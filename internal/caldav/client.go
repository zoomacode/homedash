// Package caldav polls iCloud CalDAV for events and reminders and pushes them
// into the dashboard state store.
package caldav

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
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
			Name:     ical.CompCalendar,
			AllProps: false,
			Comps: []caldavlib.CalendarCompRequest{
				{
					Name:     ical.CompEvent,
					AllProps: true,
				},
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

// fetchReminders queries a calendar path for all VTODO components.
func (c *Client) fetchReminders(ctx context.Context, cli *caldavlib.Client, path string) ([]state.Reminder, error) {
	query := &caldavlib.CalendarQuery{
		CompRequest: caldavlib.CalendarCompRequest{
			Name:     ical.CompCalendar,
			AllProps: false,
			Comps: []caldavlib.CalendarCompRequest{
				{
					Name:     ical.CompToDo,
					AllProps: true,
				},
			},
		},
		CompFilter: caldavlib.CompFilter{
			Name: ical.CompCalendar,
			Comps: []caldavlib.CompFilter{
				{
					Name: ical.CompToDo,
				},
			},
		},
	}

	objects, err := cli.QueryCalendar(ctx, path, query)
	if err != nil {
		return nil, err
	}

	var reminders []state.Reminder
	for _, obj := range objects {
		if obj.Data == nil {
			continue
		}
		for _, child := range obj.Data.Children {
			if child.Name != ical.CompToDo {
				continue
			}
			uid := getProp(child, ical.PropUID)
			title := getProp(child, ical.PropSummary)
			statusVal := getProp(child, ical.PropStatus)
			done := strings.EqualFold(statusVal, "COMPLETED")
			reminders = append(reminders, state.Reminder{
				UID:   uid,
				Title: title,
				Done:  done,
				Path:  obj.Path,
			})
		}
	}
	return reminders, nil
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
