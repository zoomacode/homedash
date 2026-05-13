package web

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/zoomacode/homedash/internal/state"
	"github.com/zoomacode/homedash/internal/web/templates"
)

// maxUploadBytes caps a single multipart request to keep iPad uploads
// from blowing out the kitchen Pi's memory. Per-file limit is the
// in-memory threshold for ParseMultipartForm; everything beyond goes to
// a temp file on disk.
const maxUploadBytes = 200 << 20 // 200 MiB

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

func (s *Server) handlePhoto(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	for _, p := range s.store.Snapshot().Photos {
		if p.ID == id {
			http.ServeFile(w, r, p.LocalPath)
			return
		}
	}
	http.NotFound(w, r)
}

func (s *Server) handlePhotosFragment(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = templates.Photos(s.store.Snapshot().Photos, s.slideshowSeconds).Render(r.Context(), w)
}

// handlePhotosUpload accepts multipart/form-data with one or more image
// files, writes each into the configured photos cache dir using an
// "upload_" prefix (so re-running the Google Photos picker won't wipe
// them), then triggers a rescan. The SSE notifier delivers the new
// slideshow to all open dashboards.
func (s *Server) handlePhotosUpload(w http.ResponseWriter, r *http.Request) {
	if s.photosCacheDir == "" {
		http.Error(w, "photos cache dir not configured", http.StatusServiceUnavailable)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, "parse multipart: "+err.Error(), http.StatusBadRequest)
		return
	}
	if r.MultipartForm == nil || len(r.MultipartForm.File["files"]) == 0 {
		http.Error(w, "no files in field 'files'", http.StatusBadRequest)
		return
	}
	if err := os.MkdirAll(s.photosCacheDir, 0o755); err != nil {
		http.Error(w, "mkdir cache: "+err.Error(), http.StatusInternalServerError)
		return
	}

	saved := 0
	for _, h := range r.MultipartForm.File["files"] {
		ext := strings.ToLower(filepath.Ext(h.Filename))
		switch ext {
		case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".heic":
		default:
			continue
		}
		f, err := h.Open()
		if err != nil {
			continue
		}
		nameRand := make([]byte, 4)
		_, _ = rand.Read(nameRand)
		dest := filepath.Join(
			s.photosCacheDir,
			fmt.Sprintf("upload_%d_%s%s", time.Now().Unix(), hex.EncodeToString(nameRand), ext),
		)
		out, err := os.Create(dest + ".tmp")
		if err != nil {
			f.Close()
			continue
		}
		if _, err := io.Copy(out, f); err != nil {
			out.Close()
			f.Close()
			os.Remove(dest + ".tmp")
			continue
		}
		out.Close()
		f.Close()
		if err := os.Rename(dest+".tmp", dest); err != nil {
			os.Remove(dest + ".tmp")
			continue
		}
		saved++
	}

	if s.photoRescanner != nil {
		_ = s.photoRescanner.Once(r.Context())
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = templates.Photos(s.store.Snapshot().Photos, s.slideshowSeconds).Render(r.Context(), w)
}

func (s *Server) handleToggleReminder(w http.ResponseWriter, r *http.Request) {
	if s.cal == nil {
		http.Error(w, "calendar not configured", http.StatusServiceUnavailable)
		return
	}
	uid := chi.URLParam(r, "uid")
	_ = r.ParseForm()
	done := r.Form.Get("done") == "on"

	// Optimistic UI: flip the local copy first. Stamp Completed=now
	// when checking off so the grace-period filter sees the right
	// timestamp even before the next remote poll catches up.
	snap := s.store.Snapshot()
	for i, rem := range snap.Reminders {
		if rem.UID == uid {
			snap.Reminders[i].Done = done
			if done {
				snap.Reminders[i].Completed = time.Now()
			} else {
				snap.Reminders[i].Completed = time.Time{}
			}
			s.store.SetReminders(snap.Reminders)
			break
		}
	}

	if err := s.cal.ToggleReminder(r.Context(), uid, done); err != nil {
		log.Printf("reminders: toggle %s done=%v: %v", uid, done, err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	log.Printf("reminders: toggle %s done=%v: ok", uid, done)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	full := findReminder(snap.Reminders, uid)
	full.UID = uid
	full.Done = done
	_ = templates.ReminderItem(full).Render(r.Context(), w)
}

func findReminder(rs []state.Reminder, uid string) state.Reminder {
	for _, r := range rs {
		if r.UID == uid {
			return r
		}
	}
	return state.Reminder{}
}
