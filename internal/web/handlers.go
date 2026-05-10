package web

import (
	"net/http"

	"github.com/zoomacode/homedash/internal/web/templates"
)

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
