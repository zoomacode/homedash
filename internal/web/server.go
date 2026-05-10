// Package web serves the dashboard UI.
package web

import (
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

type Server struct {
	store            *state.Store
	cal              ReminderToggler
	slideshowSeconds int
	router           *chi.Mux
}

func New(store *state.Store, cal ReminderToggler, slideshowSeconds int) *Server {
	s := &Server{store: store, cal: cal, slideshowSeconds: slideshowSeconds, router: chi.NewRouter()}
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
	r.Get("/fragment/news", s.handleNewsFragment)
	r.Get("/photo/{id}", s.handlePhoto)
	r.Get("/fragment/photos", s.handlePhotosFragment)
	r.Post("/reminders/{uid}/toggle", s.handleToggleReminder)
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = templates.Page(s.store.Snapshot(), time.Now(), s.slideshowSeconds).Render(r.Context(), w)
}
