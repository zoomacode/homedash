package state

import (
	"testing"
	"time"
)

func TestStore_SetAndGet(t *testing.T) {
	s := New()
	s.SetWeather(Weather{TempC: 21.5})
	got := s.Snapshot().Weather
	if got.TempC != 21.5 {
		t.Errorf("temp = %v", got.TempC)
	}
}

func TestStore_NotifiesOnChange(t *testing.T) {
	s := New()
	ch := s.Subscribe(8)
	defer s.Unsubscribe(ch)

	s.SetWeather(Weather{TempC: 19})

	select {
	case ev := <-ch:
		if ev.Section != "weather" {
			t.Errorf("section = %q", ev.Section)
		}
	case <-time.After(time.Second):
		t.Fatal("no event received")
	}
}
