// Package gcal polls one or more Google Calendars by display name and
// pushes events into the dashboard state. Recurrences are expanded
// server-side (singleEvents=true) so each occurrence becomes its own
// state.Event with the actual instance start time.
package gcal

import (
	"context"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	calendarapi "google.golang.org/api/calendar/v3"
	"google.golang.org/api/option"

	"github.com/zoomacode/homedash/internal/state"
)

type Client struct {
	hc       *http.Client
	include  []string // calendar summaries to fetch, matched case-insensitively
	horizon  time.Duration
	store    *state.Store
	logOnce  sync.Once
}

func New(hc *http.Client, include []string, store *state.Store) *Client {
	return &Client{hc: hc, include: include, horizon: 14 * 24 * time.Hour, store: store}
}

func (c *Client) Run(ctx context.Context, every time.Duration) {
	if every <= 0 {
		every = 15 * time.Minute
	}
	poll := func() {
		if err := c.PollOnce(ctx); err != nil {
			log.Printf("gcal: poll: %v", err)
		}
	}
	poll()
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			poll()
		}
	}
}

func (c *Client) PollOnce(ctx context.Context) error {
	svc, err := calendarapi.NewService(ctx, option.WithHTTPClient(c.hc))
	if err != nil {
		return err
	}

	cl, err := svc.CalendarList.List().MaxResults(100).Do()
	if err != nil {
		return err
	}

	c.logOnce.Do(func() {
		names := make([]string, len(cl.Items))
		for i, it := range cl.Items {
			names[i] = it.Summary
		}
		log.Printf("gcal: %d calendars discovered: %q", len(names), names)
		log.Printf("gcal: configured include: %q", c.include)
	})

	includeSet := map[string]bool{}
	for _, n := range c.include {
		includeSet[strings.ToLower(n)] = true
	}

	now := time.Now()
	end := now.Add(c.horizon)
	timeMin := now.Format(time.RFC3339)
	timeMax := end.Format(time.RFC3339)

	var events []state.Event
	for _, it := range cl.Items {
		if !includeSet[strings.ToLower(it.Summary)] {
			continue
		}
		page := ""
		for {
			call := svc.Events.List(it.Id).
				TimeMin(timeMin).
				TimeMax(timeMax).
				SingleEvents(true). // expand recurrences into instances
				OrderBy("startTime").
				MaxResults(250)
			if page != "" {
				call = call.PageToken(page)
			}
			resp, err := call.Do()
			if err != nil {
				log.Printf("gcal: events from %q: %v", it.Summary, err)
				break
			}
			for _, ev := range resp.Items {
				if ev.Status == "cancelled" {
					continue
				}
				startT, endT := eventTimes(ev)
				if startT.IsZero() {
					continue
				}
				events = append(events, state.Event{
					UID:   ev.Id,
					Title: ev.Summary,
					Start: startT,
					End:   endT,
				})
			}
			if resp.NextPageToken == "" {
				break
			}
			page = resp.NextPageToken
		}
	}

	sort.Slice(events, func(i, j int) bool { return events[i].Start.Before(events[j].Start) })
	c.store.SetEventsFromSource("google", events)
	return nil
}

// eventTimes pulls Start/End from a Google Calendar event. Timed events
// use DateTime (RFC3339); all-day events use Date (YYYY-MM-DD).
func eventTimes(ev *calendarapi.Event) (time.Time, time.Time) {
	if ev.Start == nil {
		return time.Time{}, time.Time{}
	}
	var start, end time.Time
	if ev.Start.DateTime != "" {
		start, _ = time.Parse(time.RFC3339, ev.Start.DateTime)
	} else if ev.Start.Date != "" {
		start, _ = time.ParseInLocation("2006-01-02", ev.Start.Date, time.Local)
	}
	if ev.End != nil {
		if ev.End.DateTime != "" {
			end, _ = time.Parse(time.RFC3339, ev.End.DateTime)
		} else if ev.End.Date != "" {
			end, _ = time.ParseInLocation("2006-01-02", ev.End.Date, time.Local)
		}
	}
	if start.IsZero() {
		return time.Time{}, time.Time{}
	}
	return start, end
}
