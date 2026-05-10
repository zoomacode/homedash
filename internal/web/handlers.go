package web

import (
	"net/http"

	"github.com/zoomacode/homedash/internal/web/templates"
)

func (s *Server) handleWeatherFragment(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = templates.Weather(s.store.Snapshot().Weather).Render(r.Context(), w)
}
