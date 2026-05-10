package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"time"

	"github.com/zoomacode/homedash/internal/caldav"
	"github.com/zoomacode/homedash/internal/config"
	"github.com/zoomacode/homedash/internal/mqttsub"
	"github.com/zoomacode/homedash/internal/state"
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

	srv := web.New(st, cd)

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

	log.Printf("homedash listening on %s", cfg.HTTP.Listen)
	if err := http.ListenAndServe(cfg.HTTP.Listen, srv.Handler()); err != nil {
		log.Fatal(err)
	}
}
