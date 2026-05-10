package web

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/zoomacode/homedash/internal/state"
	"github.com/zoomacode/homedash/internal/web/templates"
)

// ReminderToggler is the subset of the CalDAV client used by the toggle handler.
type ReminderToggler interface {
	ToggleReminder(ctx context.Context, uid string, done bool) error
}

func (s *Server) handleWeatherFragment(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = templates.Weather(s.store.Snapshot().Weather).Render(r.Context(), w)
}

func (s *Server) handleSensorsFragment(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = templates.Sensors(s.store.Snapshot()).Render(r.Context(), w)
}

func (s *Server) handleEventsFragment(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = templates.Events(s.store.Snapshot().Events).Render(r.Context(), w)
}

func (s *Server) handleRemindersFragment(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = templates.Reminders(s.store.Snapshot().Reminders).Render(r.Context(), w)
}

func (s *Server) handleNewsFragment(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = templates.News(s.store.Snapshot().News).Render(r.Context(), w)
}

func (s *Server) handleToggleReminder(w http.ResponseWriter, r *http.Request) {
	if s.cal == nil {
		http.Error(w, "calendar not configured", http.StatusServiceUnavailable)
		return
	}
	uid := chi.URLParam(r, "uid")
	_ = r.ParseForm()
	done := r.Form.Get("done") == "on"

	// Optimistic UI: flip the local copy first.
	snap := s.store.Snapshot()
	for i, rem := range snap.Reminders {
		if rem.UID == uid {
			snap.Reminders[i].Done = done
			s.store.SetReminders(snap.Reminders)
			break
		}
	}

	if err := s.cal.ToggleReminder(r.Context(), uid, done); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = templates.ReminderItem(state.Reminder{UID: uid, Done: done, Title: titleFor(snap.Reminders, uid)}).Render(r.Context(), w)
}

func titleFor(rs []state.Reminder, uid string) string {
	for _, r := range rs {
		if r.UID == uid {
			return r.Title
		}
	}
	return ""
}
