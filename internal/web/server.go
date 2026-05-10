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
	store  *state.Store
	router *chi.Mux
}

func New(store *state.Store) *Server {
	s := &Server{store: store, router: chi.NewRouter()}
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
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = templates.Page(s.store.Snapshot(), time.Now()).Render(r.Context(), w)
}
