package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoad_Valid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.yaml")
	body := `
version: 1
http:
  listen: ":8080"
location:
  lat: 50.08
  lon: 14.43
mqtt:
  broker: "tcp://localhost:1883"
  client_id: "homedash"
  topics:
    - topic: "sensors/temp"
      name: "Temp"
      unit: "°C"
      group: "outdoor"
      decimals: 1
      stale_after: "5m"
calendars:
  poll_minutes: 15
  include: ["Personal"]
reminders:
  list_name: "Dashboard"
weather:
  poll_minutes: 30
rss:
  poll_minutes: 15
  feeds: ["https://example.com/feed.xml"]
photos:
  shared_album_url: "https://www.icloud.com/sharedalbum/#X"
  refresh_hours: 6
  cache_dir: "/tmp/photos"
  slideshow_seconds: 8
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ICLOUD_USER", "u@i.com")
	t.Setenv("ICLOUD_APP_PASSWORD", "abcd-efgh-ijkl-mnop")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.HTTP.Listen != ":8080" {
		t.Errorf("listen = %q", cfg.HTTP.Listen)
	}
	if len(cfg.MQTT.Topics) != 1 || cfg.MQTT.Topics[0].StaleAfter != 5*time.Minute {
		t.Errorf("topics = %#v", cfg.MQTT.Topics)
	}
	if cfg.ICloud.User != "u@i.com" {
		t.Errorf("user = %q", cfg.ICloud.User)
	}
}

func TestLoad_MissingSecrets(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.yaml")
	if err := os.WriteFile(path, []byte("version: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ICLOUD_USER", "")
	t.Setenv("ICLOUD_APP_PASSWORD", "")

	if _, err := Load(path); err == nil {
		t.Fatal("expected error when secrets missing")
	}
}
