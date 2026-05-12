package main

import (
	"context"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
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

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	wp := &weather.Poller{
		Lat: cfg.Location.Lat, Lon: cfg.Location.Lon,
		Store: st, Interval: time.Duration(cfg.Weather.PollMinutes) * time.Minute,
	}
	go wp.Run(ctx)

	mc := mqttsub.New(mqttsub.Config{
		Broker:   cfg.MQTT.Broker,
		ClientID: cfg.MQTT.ClientID,
		Username: cfg.MQTT.Username,
		Password: cfg.MQTT.Password,
		Topics:   cfg.MQTT.Topics,
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
	for _, u := range listenURLs(cfg.HTTP.Listen) {
		log.Printf("  %s", u)
	}
	httpSrv := &http.Server{Addr: cfg.HTTP.Listen, Handler: srv.Handler()}
	go func() {
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelShutdown()
	_ = httpSrv.Shutdown(shutdownCtx)
}

// listenURLs returns clickable URLs derived from an HTTP listen string like
// ":8080" or "0.0.0.0:8080" — always localhost, plus the LAN IP when the
// listener is bound to a wildcard address.
func listenURLs(listen string) []string {
	host, port, err := net.SplitHostPort(strings.TrimSpace(listen))
	if err != nil {
		return []string{"http://" + listen}
	}
	urls := []string{"http://localhost:" + port}
	bindAll := host == "" || host == "0.0.0.0" || host == "::"
	if bindAll {
		if ip := outboundIP(); ip != "" {
			urls = append(urls, "http://"+ip+":"+port)
		}
	} else if host != "127.0.0.1" && host != "localhost" && host != "::1" {
		urls = append(urls, "http://"+host+":"+port)
	}
	return urls
}

// outboundIP returns the IPv4 the kernel would use to reach the public
// internet, without actually sending any packets. Empty string if no
// route is available.
func outboundIP() string {
	conn, err := net.Dial("udp4", "8.8.8.8:80")
	if err != nil {
		return ""
	}
	defer conn.Close()
	return conn.LocalAddr().(*net.UDPAddr).IP.String()
}

func stateDir() string {
	if d := os.Getenv("STATE_DIRECTORY"); d != "" {
		return d
	}
	return "./var"
}
