package weather

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/zoomacode/homedash/internal/state"
)

func TestPoller_FetchAndStore(t *testing.T) {
	body := map[string]any{
		"current": map[string]any{
			"temperature_2m":       21.5,
			"apparent_temperature": 20.8,
			"weather_code":         2,
		},
		"daily": map[string]any{
			"time":               []string{"2026-05-09", "2026-05-10"},
			"temperature_2m_max": []float64{23, 22},
			"temperature_2m_min": []float64{12, 11},
			"weather_code":       []int{2, 3},
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(body)
	}))
	defer srv.Close()

	st := state.New()
	p := &Poller{Lat: 50, Lon: 14, Store: st, BaseURL: srv.URL, HTTP: srv.Client()}
	if err := p.Once(context.Background()); err != nil {
		t.Fatal(err)
	}

	w := st.Snapshot().Weather
	if w.TempC != 21.5 || w.FeelsC != 20.8 || w.Code != 2 {
		t.Errorf("now = %+v", w)
	}
	if len(w.Forecast) != 2 || w.Forecast[1].HighC != 22 {
		t.Errorf("forecast = %+v", w.Forecast)
	}
	if time.Since(w.UpdatedAt) > time.Second {
		t.Errorf("UpdatedAt not set")
	}
}
