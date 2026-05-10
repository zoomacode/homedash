// Package state holds the in-memory dashboard snapshot and notifies subscribers of section changes.
package state

import (
	"sync"
	"sync/atomic"
	"time"
)

type Snapshot struct {
	Weather         Weather
	Sensors         map[string]Sensor // key: topic
	Events          []Event
	Reminders       []Reminder
	News            []NewsItem
	Photos          []Photo
	ICloudAuthError bool
}

type Weather struct {
	TempC, FeelsC float64
	Code          int
	Forecast      []DayForecast
	UpdatedAt     time.Time
}
type DayForecast struct {
	Date          time.Time
	HighC, LowC   float64
	Code          int
}

type Sensor struct {
	Topic, Name, Unit, Group string
	Value                    string
	UpdatedAt                time.Time
	StaleAfter               time.Duration
}

type Event struct {
	UID, Title string
	Start, End time.Time
}
type Reminder struct {
	UID, Title string
	Done       bool
	Path       string
}
type NewsItem struct {
	GUID, Feed, Title, Link string
	Published               time.Time
}
type Photo struct {
	ID, LocalPath string
}

type Event2 struct{ Section string }

type Store struct {
	cur atomic.Pointer[Snapshot]

	mu   sync.Mutex
	subs []chan Event2
}

func New() *Store {
	s := &Store{}
	empty := &Snapshot{Sensors: map[string]Sensor{}}
	s.cur.Store(empty)
	return s
}

func (s *Store) Snapshot() Snapshot { return *s.cur.Load() }

func (s *Store) Subscribe(buf int) chan Event2 {
	ch := make(chan Event2, buf)
	s.mu.Lock()
	s.subs = append(s.subs, ch)
	s.mu.Unlock()
	return ch
}

func (s *Store) Unsubscribe(ch chan Event2) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, c := range s.subs {
		if c == ch {
			s.subs = append(s.subs[:i], s.subs[i+1:]...)
			close(ch)
			return
		}
	}
}

func (s *Store) notify(section string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, ch := range s.subs {
		select {
		case ch <- Event2{Section: section}:
		default: // drop on slow consumer
		}
	}
}

func (s *Store) update(mut func(*Snapshot)) {
	old := s.cur.Load()
	cp := *old
	if cp.Sensors != nil {
		m := make(map[string]Sensor, len(cp.Sensors))
		for k, v := range cp.Sensors {
			m[k] = v
		}
		cp.Sensors = m
	}
	mut(&cp)
	s.cur.Store(&cp)
}

func (s *Store) SetWeather(w Weather) {
	w.UpdatedAt = time.Now()
	s.update(func(sn *Snapshot) { sn.Weather = w })
	s.notify("weather")
}

func (s *Store) SetSensor(sensor Sensor) {
	sensor.UpdatedAt = time.Now()
	s.update(func(sn *Snapshot) {
		if sn.Sensors == nil {
			sn.Sensors = map[string]Sensor{}
		}
		sn.Sensors[sensor.Topic] = sensor
	})
	s.notify("sensors")
}

func (s *Store) SetEvents(ev []Event) {
	s.update(func(sn *Snapshot) { sn.Events = ev })
	s.notify("events")
}

func (s *Store) SetReminders(r []Reminder) {
	s.update(func(sn *Snapshot) { sn.Reminders = r })
	s.notify("reminders")
}

func (s *Store) SetNews(n []NewsItem) {
	s.update(func(sn *Snapshot) { sn.News = n })
	s.notify("news")
}

func (s *Store) SetPhotos(p []Photo) {
	s.update(func(sn *Snapshot) { sn.Photos = p })
	s.notify("photos")
}

func (s *Store) SetICloudAuthError(b bool) {
	s.update(func(sn *Snapshot) { sn.ICloudAuthError = b })
	s.notify("auth")
}
