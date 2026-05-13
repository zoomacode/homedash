// Package web serves the dashboard UI.
package web

import (
	"context"
	"embed"
	"io/fs"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/zoomacode/homedash/internal/state"
	"github.com/zoomacode/homedash/internal/web/templates"
)

//go:embed static
var staticFS embed.FS

// PhotoRescanner is the subset of the photos.Poller used by the upload
// handler to refresh the slideshow after files land in the cache dir.
type PhotoRescanner interface {
	Once(ctx context.Context) error
}

type Server struct {
	store            *state.Store
	cal              ReminderToggler
	slideshowSeconds int
	photosCacheDir   string
	photoRescanner   PhotoRescanner
	router           *chi.Mux
}

func New(store *state.Store, cal ReminderToggler, slideshowSeconds int, photosCacheDir string, photoRescanner PhotoRescanner) *Server {
	s := &Server{
		store:            store,
		cal:              cal,
		slideshowSeconds: slideshowSeconds,
		photosCacheDir:   photosCacheDir,
		photoRescanner:   photoRescanner,
		router:           chi.NewRouter(),
	}
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler { return s.router }

// noCache wraps a handler with response headers that prevent browsers
// from caching the response. Used for /static/* so dashboards always
// pick up the latest CSS / JS after a homedash redeploy.
func noCache(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store, must-revalidate")
		h.ServeHTTP(w, r)
	})
}

func (s *Server) routes() {
	r := s.router
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })

	sub, _ := fs.Sub(staticFS, "static")
	// Disable caching for static assets so dashboards (especially iPad
	// Safari, which caches very aggressively) never get stuck on a
	// stale layout/CSS/JS after a homedash redeploy.
	r.Handle("/static/*", http.StripPrefix("/static/", noCache(http.FileServer(http.FS(sub)))))

	r.Get("/", s.handleIndex)
	r.Get("/events", s.handleEvents)
	r.Get("/fragment/weather", s.handleWeatherFragment)
	r.Get("/fragment/sensors", s.handleSensorsFragment)
	r.Get("/fragment/events", s.handleEventsFragment)
	r.Get("/fragment/reminders", s.handleRemindersFragment)
	r.Get("/photo/{id}", s.handlePhoto)
	r.Get("/fragment/photos", s.handlePhotosFragment)
	r.Post("/photos/upload", s.handlePhotosUpload)
	r.Post("/reminders/{uid}/toggle", s.handleToggleReminder)
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = templates.Page(s.store.Snapshot(), time.Now(), s.slideshowSeconds).Render(r.Context(), w)
}
