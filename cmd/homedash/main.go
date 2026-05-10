package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/zoomacode/homedash/internal/caldav"
	"github.com/zoomacode/homedash/internal/config"
	"github.com/zoomacode/homedash/internal/mqttsub"
	"github.com/zoomacode/homedash/internal/photos"
	"github.com/zoomacode/homedash/internal/rss"
	"github.com/zoomacode/homedash/internal/state"
	"github.com/zoomacode/homedash/internal/store"
	"github.com/zoomacode/homedash/internal/weather"
	"github.com/zoomacode/homedash/internal/web"
)

func main() {
	cfgPath := flag.String("config", "/etc/homedash/config.yaml", "path to config.yaml")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	st := state.New()

	cd := caldav.New(cfg.ICloud.User, cfg.ICloud.AppPassword, cfg.Calendars.Include, cfg.Reminders.ListName, st)

	srv := web.New(st, cd, cfg.Photos.SlideshowSeconds)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wp := &weather.Poller{
		Lat: cfg.Location.Lat, Lon: cfg.Location.Lon,
		Store: st, Interval: time.Duration(cfg.Weather.PollMinutes) * time.Minute,
	}
	go wp.Run(ctx)

	mc := mqttsub.New(mqttsub.Config{
		Broker: cfg.MQTT.Broker, ClientID: cfg.MQTT.ClientID, Topics: cfg.MQTT.Topics,
	}, st)
	if err := mc.Start(ctx); err != nil {
		log.Printf("mqtt: %v", err)
	}

	go cd.RunEvents(ctx, time.Duration(cfg.Calendars.PollMinutes)*time.Minute)

	db, err := store.Open(filepath.Join(stateDir(), "homedash.db"))
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer db.Close()

	rp := rss.New(cfg.RSS.Feeds, time.Duration(cfg.RSS.PollMinutes)*time.Minute, st, db)
	go rp.Run(ctx)

	pp := &photos.Poller{
		AlbumURL: cfg.Photos.SharedAlbumURL,
		CacheDir: cfg.Photos.CacheDir,
		Refresh:  time.Duration(cfg.Photos.RefreshHours) * time.Hour,
		Store:    st, DB: db,
	}
	go pp.Run(ctx)

	log.Printf("homedash listening on %s", cfg.HTTP.Listen)
	if err := http.ListenAndServe(cfg.HTTP.Listen, srv.Handler()); err != nil {
		log.Fatal(err)
	}
}

func stateDir() string {
	if d := os.Getenv("STATE_DIRECTORY"); d != "" {
		return d
	}
	return "./var"
}
