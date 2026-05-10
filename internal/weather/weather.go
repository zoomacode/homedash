// Package weather polls Open-Meteo for current conditions and a forecast.
package weather

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"time"

	"github.com/zoomacode/homedash/internal/state"
)

const defaultBaseURL = "https://api.open-meteo.com/v1/forecast"

type Poller struct {
	Lat, Lon float64
	Store    *state.Store
	Interval time.Duration
	BaseURL  string
	HTTP     *http.Client
}

type apiResp struct {
	Current struct {
		TempC  float64 `json:"temperature_2m"`
		FeelsC float64 `json:"apparent_temperature"`
		Code   int     `json:"weather_code"`
	} `json:"current"`
	Daily struct {
		Time []string  `json:"time"`
		High []float64 `json:"temperature_2m_max"`
		Low  []float64 `json:"temperature_2m_min"`
		Code []int     `json:"weather_code"`
	} `json:"daily"`
}

func (p *Poller) Once(ctx context.Context) error {
	base := p.BaseURL
	if base == "" {
		base = defaultBaseURL
	}
	q := url.Values{}
	q.Set("latitude", fmt.Sprintf("%g", p.Lat))
	q.Set("longitude", fmt.Sprintf("%g", p.Lon))
	q.Set("current", "temperature_2m,apparent_temperature,weather_code")
	q.Set("daily", "temperature_2m_max,temperature_2m_min,weather_code")
	q.Set("timezone", "auto")
	q.Set("forecast_days", "5")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	client := p.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("open-meteo: status %d", resp.StatusCode)
	}
	var r apiResp
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return err
	}

	w := state.Weather{TempC: r.Current.TempC, FeelsC: r.Current.FeelsC, Code: r.Current.Code}
	for i := range r.Daily.Time {
		date, _ := time.Parse("2006-01-02", r.Daily.Time[i])
		w.Forecast = append(w.Forecast, state.DayForecast{
			Date: date, HighC: r.Daily.High[i], LowC: r.Daily.Low[i], Code: r.Daily.Code[i],
		})
	}
	p.Store.SetWeather(w)
	return nil
}

func (p *Poller) Run(ctx context.Context) {
	if p.Interval == 0 {
		p.Interval = 30 * time.Minute
	}
	if err := p.Once(ctx); err != nil {
		log.Printf("weather: poll error: %v", err)
	}
	t := time.NewTicker(p.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := p.Once(ctx); err != nil {
				log.Printf("weather: poll error: %v", err)
			}
		}
	}
}
