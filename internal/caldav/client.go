// Package caldav polls iCloud CalDAV for events and reminders and pushes them
// into the dashboard state store.
package caldav

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-ical"
	"github.com/emersion/go-webdav"
	caldavlib "github.com/emersion/go-webdav/caldav"
	"github.com/zoomacode/homedash/internal/state"
)

// is401 reports whether err represents an HTTP 401 Unauthorized response.
// go-webdav's internal.HTTPError is not exported, so we detect it via the
// error string which is formatted as "401 Unauthorized[: ...]".
func is401(err error) bool {
	if err == nil {
		return false
	}
	return strings.HasPrefix(err.Error(), "401 ")
}

const defaultEndpoint = "https://caldav.icloud.com/"

// Client polls a CalDAV server for events and to-dos.
type Client struct {
	user, password string
	calNames       []string // calendar display names to pull events from
	listName       string   // calendar display name to pull reminders (VTODO) from
	store          *state.Store
	httpClient     webdav.HTTPClient
	endpoint       string
	logOnce        sync.Once
}

// New constructs a CalDAV client that will poll the given calendars and
// reminder list and write results into store.
func New(user, password string, calendars []string, listName string, store *state.Store) *Client {
	hc := webdav.HTTPClientWithBasicAuth(http.DefaultClient, user, password)
	return &Client{
		user:       user,
		password:   password,
		calNames:   calendars,
		listName:   listName,
		store:      store,
		httpClient: hc,
		endpoint:   defaultEndpoint,
	}
}

// PollOnce performs a single CalDAV fetch and updates the store.
// It detects HTTP 401 responses and sets the ICloudAuthError flag accordingly.
func (c *Client) PollOnce(ctx context.Context) error {
	err := c.pollOnce(ctx)
	if is401(err) {
		c.store.SetICloudAuthError(true)
		return err
	}
	if err == nil {
		c.store.SetICloudAuthError(false)
	}
	return err
}

// pollOnce is the internal implementation of PollOnce.
func (c *Client) pollOnce(ctx context.Context) error {
	cli, err := caldavlib.NewClient(c.httpClient, c.endpoint)
	if err != nil {
		return err
	}

	principal, err := cli.FindCurrentUserPrincipal(ctx)
	if err != nil {
		return err
	}

	homeSet, err := cli.FindCalendarHomeSet(ctx, principal)
	if err != nil {
		return err
	}

	calendars, err := cli.FindCalendars(ctx, homeSet)
	if err != nil {
		return err
	}

	c.logCalendars(calendars)

	now := time.Now()
	horizon := now.Add(14 * 24 * time.Hour)

	var events []state.Event
	var reminders []state.Reminder

	for _, cal := range calendars {
		isEventCal := c.inCalNames(cal.Name)
		isListCal := strings.EqualFold(cal.Name, c.listName)

		if !isEventCal && !isListCal {
			continue
		}

		if isEventCal && c.supportsComp(cal, ical.CompEvent) {
			evs, err := c.fetchEvents(ctx, cli, cal.Path, now, horizon)
			if err != nil {
				log.Printf("caldav: events from %q: %v", cal.Name, err)
				continue
			}
			events = append(events, evs...)
		}

		if isListCal && c.supportsComp(cal, ical.CompToDo) {
			rems, err := c.fetchReminders(ctx, cli, cal.Path)
			if err != nil {
				log.Printf("caldav: reminders from %q: %v", cal.Name, err)
				continue
			}
			reminders = append(reminders, rems...)
		}
	}

	c.store.SetEvents(events)
	c.store.SetReminders(reminders)
	return nil
}

// RunEvents polls immediately and then on every tick of the given interval.
func (c *Client) RunEvents(ctx context.Context, every time.Duration) {
	if every <= 0 {
		every = 15 * time.Minute
	}

	poll := func() {
		if err := c.PollOnce(ctx); err != nil {
			log.Printf("caldav: poll error: %v", err)
		}
	}

	poll()
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			poll()
		}
	}
}

// fetchEvents queries a calendar path for VEVENT components in [start, end).
func (c *Client) fetchEvents(ctx context.Context, cli *caldavlib.Client, path string, start, end time.Time) ([]state.Event, error) {
	query := &caldavlib.CalendarQuery{
		CompRequest: caldavlib.CalendarCompRequest{
			Name:  ical.CompCalendar,
			Props: []string{"VERSION", "PRODID"},
			// iCloud quirk: <allprop/> returns empty VEVENTs, so list
			// the props we need explicitly.
			Comps: []caldavlib.CalendarCompRequest{
				{
					Name: ical.CompEvent,
					Props: []string{
						"UID", "SUMMARY", "DESCRIPTION", "LOCATION",
						"DTSTART", "DTEND", "DURATION",
						"RECURRENCE-ID", "STATUS", "CATEGORIES", "TRANSP",
					},
				},
			},
			// Ask the server to expand RRULEs into individual instances
			// within [start, end) so we get one event per occurrence.
			Expand: &caldavlib.CalendarExpandRequest{
				Start: start,
				End:   end,
			},
		},
		CompFilter: caldavlib.CompFilter{
			Name: ical.CompCalendar,
			Comps: []caldavlib.CompFilter{
				{
					Name:  ical.CompEvent,
					Start: start,
					End:   end,
				},
			},
		},
	}

	objects, err := cli.QueryCalendar(ctx, path, query)
	if err != nil {
		return nil, err
	}

	var events []state.Event
	for _, obj := range objects {
		if obj.Data == nil {
			continue
		}
		for _, ev := range obj.Data.Events() {
			uid := getProp(ev.Component, ical.PropUID)
			title := getProp(ev.Component, ical.PropSummary)
			evStart, _ := getTime(ev.Component, ical.PropDateTimeStart)
			evEnd, _ := getTime(ev.Component, ical.PropDateTimeEnd)
			events = append(events, state.Event{
				UID:   uid,
				Title: title,
				Start: evStart,
				End:   evEnd,
			})
		}
	}
	return events, nil
}

// reminderQuery is the REPORT body sent to fetch every VTODO in a calendar.
// We ask only for the props we use and avoid <allprop/> because of the
// same iCloud quirk that affects events.
const reminderQuery = `<?xml version="1.0" encoding="UTF-8"?>
<calendar-query xmlns="urn:ietf:params:xml:ns:caldav" xmlns:d="DAV:">
  <d:prop>
    <d:getetag/>
    <calendar-data>
      <comp name="VCALENDAR">
        <prop name="VERSION"/>
        <prop name="PRODID"/>
        <comp name="VTODO">
          <prop name="UID"/>
          <prop name="SUMMARY"/>
          <prop name="STATUS"/>
        </comp>
      </comp>
    </calendar-data>
  </d:prop>
  <filter>
    <comp-filter name="VCALENDAR">
      <comp-filter name="VTODO"/>
    </comp-filter>
  </filter>
</calendar-query>`

// multistatusResp is a minimal subset of the WebDAV REPORT response we need
// to extract calendar-data CDATA. Local-name matching lets us ignore the
// DAV: / caldav: namespace split.
type multistatusResp struct {
	XMLName   xml.Name       `xml:"multistatus"`
	Responses []responseElem `xml:"response"`
}
type responseElem struct {
	Href      string         `xml:"href"`
	Propstats []propstatElem `xml:"propstat"`
}
type propstatElem struct {
	Status string `xml:"status"`
	Prop   struct {
		CalData []byte `xml:"calendar-data"`
	} `xml:"prop"`
}

// fetchReminders queries a calendar path for all VTODO components using a
// raw REPORT so we can tolerate iCloud including the collection itself in
// the multistatus with a 404 on calendar-data (go-webdav errors out on
// that, which would mask all the real VTODO entries).
func (c *Client) fetchReminders(ctx context.Context, _ *caldavlib.Client, path string) ([]state.Reminder, error) {
	url := strings.TrimRight(c.endpoint, "/") + path
	req, err := http.NewRequestWithContext(ctx, "REPORT", url, strings.NewReader(reminderQuery))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/xml; charset=utf-8")
	req.Header.Set("Depth", "1")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMultiStatus && resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("REPORT %s: %s", path, resp.Status)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var ms multistatusResp
	if err := xml.Unmarshal(raw, &ms); err != nil {
		return nil, fmt.Errorf("parse multistatus: %w", err)
	}

	var reminders []state.Reminder
	for _, r := range ms.Responses {
		for _, ps := range r.Propstats {
			if !strings.Contains(ps.Status, " 200 ") {
				continue
			}
			if len(ps.Prop.CalData) == 0 {
				continue
			}
			cal, err := ical.NewDecoder(bytes.NewReader(ps.Prop.CalData)).Decode()
			if err != nil {
				continue
			}
			for _, child := range cal.Children {
				if child.Name != ical.CompToDo {
					continue
				}
				uid := getProp(child, ical.PropUID)
				title := getProp(child, ical.PropSummary)
				if title == "" || isApplePlaceholderReminder(title) {
					continue
				}
				statusVal := getProp(child, ical.PropStatus)
				done := strings.EqualFold(statusVal, "COMPLETED")
				reminders = append(reminders, state.Reminder{
					UID:   uid,
					Title: title,
					Done:  done,
					Path:  r.Href,
				})
			}
		}
	}
	return reminders, nil
}

// logCalendars emits a one-shot diagnostic line listing every calendar
// the server returned and which of those matched the configured event
// and reminder names. Useful for the very common "I named the calendar
// wrong in config.yaml" problem.
func (c *Client) logCalendars(calendars []caldavlib.Calendar) {
	c.logOnce.Do(func() {
		log.Printf("caldav: %d calendars discovered:", len(calendars))
		for _, cal := range calendars {
			comps := cal.SupportedComponentSet
			if len(comps) == 0 {
				comps = []string{"(any)"}
			}
			log.Printf("caldav:   %-30q  supports=%v", cal.Name, comps)
		}
		log.Printf("caldav: event matches for %q", c.calNames)
		log.Printf("caldav: reminder matches for %q", c.listName)
	})
}

// isApplePlaceholderReminder reports whether the VTODO is one of Apple's
// stub items that appear in legacy CalDAV reminder lists after the user
// upgraded to the modern (non-CalDAV) Reminders sync. Filtering these out
// prevents the dashboard from displaying "Where are my reminders?" etc.
func isApplePlaceholderReminder(title string) bool {
	t := strings.ToLower(title)
	return strings.Contains(t, "upgraded these reminders") ||
		strings.Contains(t, "where are my reminders")
}

// inCalNames reports whether name matches any configured calendar name
// (case-insensitive).
func (c *Client) inCalNames(name string) bool {
	for _, n := range c.calNames {
		if strings.EqualFold(n, name) {
			return true
		}
	}
	return false
}

// supportsComp reports whether the calendar declares support for the given
// component type (e.g. "VEVENT" or "VTODO"). If the SupportedComponentSet is
// empty we assume all types are supported.
func (c *Client) supportsComp(cal caldavlib.Calendar, compName string) bool {
	if len(cal.SupportedComponentSet) == 0 {
		return true
	}
	for _, s := range cal.SupportedComponentSet {
		if strings.EqualFold(s, compName) {
			return true
		}
	}
	return false
}

// ToggleReminder sets the STATUS of the VTODO with the given UID to COMPLETED
// or NEEDS-ACTION depending on done, then writes it back to the CalDAV server.
// It detects HTTP 401 responses and sets the ICloudAuthError flag accordingly.
func (c *Client) ToggleReminder(ctx context.Context, uid string, done bool) error {
	err := c.toggleReminder(ctx, uid, done)
	if is401(err) {
		c.store.SetICloudAuthError(true)
		return err
	}
	if err == nil {
		c.store.SetICloudAuthError(false)
	}
	return err
}

// toggleReminder is the internal implementation of ToggleReminder.
func (c *Client) toggleReminder(ctx context.Context, uid string, done bool) error {
	cli, err := caldavlib.NewClient(c.httpClient, c.endpoint)
	if err != nil {
		return err
	}

	principal, err := cli.FindCurrentUserPrincipal(ctx)
	if err != nil {
		return err
	}

	homeSet, err := cli.FindCalendarHomeSet(ctx, principal)
	if err != nil {
		return err
	}

	calendars, err := cli.FindCalendars(ctx, homeSet)
	if err != nil {
		return err
	}

	for _, cal := range calendars {
		if !strings.EqualFold(cal.Name, c.listName) {
			continue
		}
		objs, err := cli.QueryCalendar(ctx, cal.Path, &caldavlib.CalendarQuery{
			CompFilter: caldavlib.CompFilter{
				Name: ical.CompCalendar,
				Comps: []caldavlib.CompFilter{
					{Name: ical.CompToDo},
				},
			},
		})
		if err != nil {
			return err
		}
		for _, o := range objs {
			if o.Data == nil {
				continue
			}
			for _, comp := range o.Data.Children {
				if comp.Name != ical.CompToDo || getProp(comp, ical.PropUID) != uid {
					continue
				}
				comp.Props.SetText(ical.PropStatus, statusFor(done))
				_, err = cli.PutCalendarObject(ctx, o.Path, o.Data)
				return err
			}
		}
	}
	return fmt.Errorf("reminder %s not found", uid)
}

func statusFor(done bool) string {
	if done {
		return "COMPLETED"
	}
	return "NEEDS-ACTION"
}
