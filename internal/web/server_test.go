package web

import (
	"bufio"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zoomacode/homedash/internal/state"
)

func TestSSE_DeliversWeatherEvent(t *testing.T) {
	st := state.New()
	srv := New(st, nil, 8)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := ts.Client().Get(ts.URL + "/events")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	go func() { st.SetWeather(state.Weather{TempC: 1}) }()

	r := bufio.NewReader(resp.Body)
	for i := 0; i < 10; i++ {
		line, err := r.ReadString('\n')
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if strings.HasPrefix(line, "event: weather") {
			return
		}
	}
	t.Fatal("no weather event received")
}

func TestIndex_RendersClock(t *testing.T) {
	srv := New(state.New(), nil, 8)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	srv.Handler().ServeHTTP(rr, req)

	if rr.Code != 200 {
		t.Fatalf("status = %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, `id="clock"`) {
		t.Errorf("body missing clock section")
	}
}

func TestIndex_RendersSensors(t *testing.T) {
	st := state.New()
	st.SetSensor(state.Sensor{Topic: "sensors/temp", Name: "Outdoor Temp", Unit: "°C", Group: "outdoor", Value: "21.5"})
	srv := New(st, nil, 8)
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest("GET", "/", nil))

	body := rr.Body.String()
	if !strings.Contains(body, "Outdoor Temp") || !strings.Contains(body, "21.5°C") {
		t.Errorf("body missing sensor: %s", body)
	}
}
