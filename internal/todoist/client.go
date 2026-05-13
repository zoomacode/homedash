// Package todoist polls the Todoist REST v2 API for a project's active
// tasks and toggles them complete/uncomplete on click.
//
// Auth is a simple bearer token from the user's Todoist integrations
// settings — no OAuth dance. The REST endpoint we hit returns only
// active tasks; the source-bucketed Reminders store in package state
// preserves recently-checked items through the dashboard's 5-min
// grace window so the strike-through stays visible across one poll.
package todoist

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/zoomacode/homedash/internal/state"
)

const apiBase = "https://api.todoist.com/api/v1"

type Client struct {
	hc          *http.Client
	token       string
	projectName string // human-readable, matched case-insensitively
	store       *state.Store

	logOnce sync.Once
}

func New(token, projectName string, store *state.Store) *Client {
	return &Client{
		hc:          &http.Client{Timeout: 15 * time.Second},
		token:       token,
		projectName: projectName,
		store:       store,
	}
}

// Run polls immediately and then every `every` until ctx is cancelled.
func (c *Client) Run(ctx context.Context, every time.Duration) {
	if every <= 0 {
		every = 6 * time.Minute
	}
	poll := func() {
		if err := c.PollOnce(ctx); err != nil {
			log.Printf("todoist: poll: %v", err)
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

type project struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type taskDue struct {
	Date string `json:"date"`
}

type task struct {
	ID          string  `json:"id"`
	Content     string  `json:"content"`
	Description string  `json:"description"`
	ProjectID   string  `json:"project_id"`
	IsCompleted bool    `json:"is_completed"`
	Due         *taskDue `json:"due"`
	Labels      []string `json:"labels"`
}

func (c *Client) PollOnce(ctx context.Context) error {
	projects, err := c.listProjects(ctx)
	if err != nil {
		return err
	}

	c.logOnce.Do(func() {
		names := make([]string, len(projects))
		for i, p := range projects {
			names[i] = p.Name
		}
		log.Printf("todoist: %d projects discovered: %q", len(names), names)
		log.Printf("todoist: configured project: %q", c.projectName)
	})

	var projectID string
	for _, p := range projects {
		if strings.EqualFold(p.Name, c.projectName) {
			projectID = p.ID
			break
		}
	}
	if projectID == "" {
		return fmt.Errorf("project %q not found", c.projectName)
	}

	tasks, err := c.listTasks(ctx, projectID)
	if err != nil {
		return err
	}

	out := make([]state.Reminder, 0, len(tasks))
	for _, t := range tasks {
		var due time.Time
		if t.Due != nil && t.Due.Date != "" {
			// Date-only ("2026-05-15") or RFC3339; try both.
			if d, err := time.ParseInLocation("2006-01-02", t.Due.Date, time.Local); err == nil {
				due = d
			} else if d, err := time.Parse(time.RFC3339, t.Due.Date); err == nil {
				due = d
			}
		}
		out = append(out, state.Reminder{
			UID:    t.ID,
			Title:  t.Content,
			Notes:  t.Description,
			Done:   t.IsCompleted,
			Due:    due,
			Path:   t.ProjectID, // recorded for symmetry; not needed by Close/Reopen
			Source: "todoist",
		})
	}
	c.store.SetRemindersFromSource("todoist", out)
	return nil
}

// ToggleReminder implements web.ReminderToggler. The Todoist REST API
// uses separate close/reopen endpoints rather than a status field.
func (c *Client) ToggleReminder(ctx context.Context, uid string, done bool) error {
	if uid == "" {
		return errors.New("todoist toggle: empty uid")
	}
	endpoint := "close"
	if !done {
		endpoint = "reopen"
	}
	url := fmt.Sprintf("%s/tasks/%s/%s", apiBase, uid, endpoint)
	req, err := http.NewRequestWithContext(ctx, "POST", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 204 && resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("todoist %s task %s: %s: %s", endpoint, uid, resp.Status, truncate(string(body), 200))
	}
	return nil
}

func (c *Client) listProjects(ctx context.Context) ([]project, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", apiBase+"/projects", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("projects: %s: %s", resp.Status, truncate(string(body), 200))
	}
	var out []project
	// Todoist v1 returns {"results": [...], "next_cursor": ...} for paged
	// endpoints, but /projects historically returned a bare array. Accept both.
	var wrap struct {
		Results []project `json:"results"`
	}
	if err := json.Unmarshal(body, &wrap); err == nil && wrap.Results != nil {
		return wrap.Results, nil
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("parse projects: %w", err)
	}
	return out, nil
}

func (c *Client) listTasks(ctx context.Context, projectID string) ([]task, error) {
	url := apiBase + "/tasks?project_id=" + projectID
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("tasks: %s: %s", resp.Status, truncate(string(body), 200))
	}
	var out []task
	var wrap struct {
		Results []task `json:"results"`
	}
	if err := json.Unmarshal(body, &wrap); err == nil && wrap.Results != nil {
		return wrap.Results, nil
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("parse tasks: %w", err)
	}
	return out, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
