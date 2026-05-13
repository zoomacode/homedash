// Package gtasks polls a Google Tasks list and pushes items into the
// dashboard state as Reminders.
package gtasks

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"google.golang.org/api/option"
	"google.golang.org/api/tasks/v1"

	"github.com/zoomacode/homedash/internal/state"
)

// Client polls a specific Google Tasks list by display name.
type Client struct {
	httpClient *http.Client
	listName   string // human-readable list title, matched case-insensitively
	store      *state.Store

	logOnce sync.Once
}

func New(hc *http.Client, listName string, store *state.Store) *Client {
	return &Client{httpClient: hc, listName: listName, store: store}
}

// Run polls immediately and then every `every` until ctx is canceled.
func (c *Client) Run(ctx context.Context, every time.Duration) {
	if every <= 0 {
		every = 5 * time.Minute
	}
	poll := func() {
		if err := c.PollOnce(ctx); err != nil {
			log.Printf("gtasks: poll: %v", err)
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

// PollOnce fetches all incomplete tasks from the configured list and
// writes them to the store. A one-shot diagnostic logs every task list
// the account has so misconfigured names surface immediately.
func (c *Client) PollOnce(ctx context.Context) error {
	svc, err := tasks.NewService(ctx, option.WithHTTPClient(c.httpClient))
	if err != nil {
		return err
	}

	lists, err := svc.Tasklists.List().MaxResults(100).Do()
	if err != nil {
		return err
	}

	c.logOnce.Do(func() {
		names := make([]string, len(lists.Items))
		for i, l := range lists.Items {
			names[i] = l.Title
		}
		log.Printf("gtasks: %d task lists discovered: %q", len(names), names)
		log.Printf("gtasks: configured list_name: %q", c.listName)
	})

	var listID, listTitle string
	for _, l := range lists.Items {
		if strings.EqualFold(l.Title, c.listName) {
			listID = l.Id
			listTitle = l.Title
			break
		}
	}
	if listID == "" {
		return fmt.Errorf("task list %q not found", c.listName)
	}

	var items []state.Reminder
	pageToken := ""
	for {
		// Pull completed tasks too so the dashboard can keep showing
		// them (struck out) during the grace window before they
		// disappear. The render layer filters them by Completed time.
		call := svc.Tasks.List(listID).
			MaxResults(100).
			ShowCompleted(true).
			ShowHidden(false)
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}
		page, err := call.Do()
		if err != nil {
			return err
		}
		for _, t := range page.Items {
			if t.Title == "" {
				continue
			}
			var due time.Time
			if t.Due != "" {
				due, _ = time.Parse(time.RFC3339, t.Due)
			}
			var completed time.Time
			if t.Completed != nil && *t.Completed != "" {
				completed, _ = time.Parse(time.RFC3339, *t.Completed)
			}
			items = append(items, state.Reminder{
				UID:       t.Id,
				Title:     t.Title,
				Done:      t.Status == "completed",
				Path:      listID, // reused for ToggleReminder
				Notes:     t.Notes,
				Due:       due,
				Completed: completed,
			})
		}
		if page.NextPageToken == "" {
			break
		}
		pageToken = page.NextPageToken
	}

	_ = listTitle
	c.store.SetReminders(items)
	return nil
}

// ToggleReminder flips a task's completed state and writes it back to
// Google. Implements the web.ReminderToggler interface so the iPad UI
// can tick boxes against Tasks just like it did against CalDAV.
// The Google Tasks list ID is recovered from Reminder.Path in state.
func (c *Client) ToggleReminder(ctx context.Context, uid string, done bool) error {
	if uid == "" {
		return errors.New("gtasks toggle: empty uid")
	}
	var listID string
	for _, r := range c.store.Snapshot().Reminders {
		if r.UID == uid {
			listID = r.Path
			break
		}
	}
	if listID == "" {
		return fmt.Errorf("gtasks: reminder %s not found in state", uid)
	}

	svc, err := tasks.NewService(ctx, option.WithHTTPClient(c.httpClient))
	if err != nil {
		return err
	}
	t, err := svc.Tasks.Get(listID, uid).Do()
	if err != nil {
		return err
	}
	if done {
		t.Status = "completed"
	} else {
		t.Status = "needsAction"
		// Google requires NullFields to actually clear `completed`.
		t.NullFields = append(t.NullFields, "Completed")
	}
	_, err = svc.Tasks.Update(listID, uid, t).Do()
	return err
}
