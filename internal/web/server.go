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

func (s *Server) routes() {
	r := s.router
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })

	sub, _ := fs.Sub(staticFS, "static")
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(sub))))

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
