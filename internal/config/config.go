// Package config loads homedash configuration from a YAML file plus secrets from env.
package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Version   int       `yaml:"version"`
	HTTP      HTTP      `yaml:"http"`
	Location  Location  `yaml:"location"`
	MQTT      MQTT      `yaml:"mqtt"`
	Calendars Calendars `yaml:"calendars"`
	Reminders Reminders `yaml:"reminders"`
	Weather   Weather   `yaml:"weather"`
	RSS       RSS       `yaml:"rss"`
	Photos    Photos    `yaml:"photos"`
	ICloud    ICloud    `yaml:"-"` // from env
}

type HTTP struct {
	Listen string `yaml:"listen"`
}
type Location struct {
	Lat float64 `yaml:"lat"`
	Lon float64 `yaml:"lon"`
}

type MQTT struct {
	Broker   string  `yaml:"broker"`
	ClientID string  `yaml:"client_id"`
	Username string  `yaml:"-"` // from env MQTT_USERNAME
	Password string  `yaml:"-"` // from env MQTT_PASSWORD
	Topics   []Topic `yaml:"topics"`
}

type Topic struct {
	Topic         string        `yaml:"topic"`
	Field         string        `yaml:"field"` // optional dotted JSON path, e.g. "temperature" or "particles.p25um"
	Name          string        `yaml:"name"`
	Unit          string        `yaml:"unit"`
	Group         string        `yaml:"group"`
	Decimals      int           `yaml:"decimals"`
	StaleAfterStr string        `yaml:"stale_after"`
	StaleAfter    time.Duration `yaml:"-"`
}

type Calendars struct {
	PollMinutes int      `yaml:"poll_minutes"`
	Include     []string `yaml:"include"`
}
type Reminders struct {
	ListName string `yaml:"list_name"`
}
type Weather struct {
	PollMinutes int `yaml:"poll_minutes"`
}
type RSS struct {
	PollMinutes int      `yaml:"poll_minutes"`
	Feeds       []string `yaml:"feeds"`
}
type Photos struct {
	SharedAlbumURL   string `yaml:"shared_album_url"`
	RefreshHours     int    `yaml:"refresh_hours"`
	CacheDir         string `yaml:"cache_dir"`
	SlideshowSeconds int    `yaml:"slideshow_seconds"`
}
type ICloud struct {
	User        string
	AppPassword string
}

func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	for i := range cfg.MQTT.Topics {
		t := &cfg.MQTT.Topics[i]
		if t.StaleAfterStr == "" {
			t.StaleAfter = 5 * time.Minute
			continue
		}
		d, err := time.ParseDuration(t.StaleAfterStr)
		if err != nil {
			return nil, fmt.Errorf("topic %q stale_after %q: %w", t.Topic, t.StaleAfterStr, err)
		}
		t.StaleAfter = d
	}

	cfg.ICloud.User = os.Getenv("ICLOUD_USER")
	cfg.ICloud.AppPassword = os.Getenv("ICLOUD_APP_PASSWORD")
	if cfg.ICloud.User == "" || cfg.ICloud.AppPassword == "" {
		return nil, fmt.Errorf("ICLOUD_USER and ICLOUD_APP_PASSWORD env vars are required")
	}

	cfg.MQTT.Username = os.Getenv("MQTT_USERNAME")
	cfg.MQTT.Password = os.Getenv("MQTT_PASSWORD")

	return &cfg, nil
}
