package main

import (
	"context"
	"flag"
	"fmt"
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
	"github.com/zoomacode/homedash/internal/gcal"
	googleauth "github.com/zoomacode/homedash/internal/google"
	"github.com/zoomacode/homedash/internal/gpicker"
	"github.com/zoomacode/homedash/internal/gtasks"
	"github.com/zoomacode/homedash/internal/mqttsub"
	"github.com/zoomacode/homedash/internal/photos"
	"github.com/zoomacode/homedash/internal/rss"
	"github.com/zoomacode/homedash/internal/state"
	"github.com/zoomacode/homedash/internal/store"
	"github.com/zoomacode/homedash/internal/todoist"
	"github.com/zoomacode/homedash/internal/weather"
	"github.com/zoomacode/homedash/internal/web"
)

// reminderDispatcher routes a toggle request to the right reminders
// backend by looking up the source recorded on the matching Reminder
// in state. CalDAV/Google/Todoist all implement web.ReminderToggler.
type reminderDispatcher struct {
	store    *state.Store
	bySource map[string]web.ReminderToggler
}

func (d *reminderDispatcher) ToggleReminder(ctx context.Context, uid string, done bool) error {
	for _, r := range d.store.Snapshot().Reminders {
		if r.UID == uid {
			if t, ok := d.bySource[r.Source]; ok && t != nil {
				return t.ToggleReminder(ctx, uid, done)
			}
			break
		}
	}
	if t, ok := d.bySource["icloud"]; ok && t != nil {
		return t.ToggleReminder(ctx, uid, done)
	}
	return fmt.Errorf("no reminders backend for uid %s", uid)
}

func main() {
	// Subcommand dispatch. `homedash auth-google` runs the OAuth flow,
	// `homedash pick-photos` runs the interactive Google Photos picker.
	// Both exit without starting the dashboard.
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "auth-google":
			os.Args = append(os.Args[:1], os.Args[2:]...)
			runAuthGoogle()
			return
		case "pick-photos":
			os.Args = append(os.Args[:1], os.Args[2:]...)
			runPickPhotos()
			return
		}
	}

	cfgPath := flag.String("config", "/etc/homedash/config.yaml", "path to config.yaml")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	st := state.New()

	cd := caldav.New(cfg.ICloud.User, cfg.ICloud.AppPassword, cfg.Calendars.Include, cfg.Reminders.ListName, st)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Default reminder toggler is the CalDAV client. If Google Tasks is
	// configured below, it will replace this — the iPad's "tick the box"
	// hits whatever source actually populated state.Reminders.
	var toggler web.ReminderToggler = cd

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

	// Calendar sources: iCloud + Google merge into Snapshot.Events when
	// both are configured. Each source writes into its own bucket via
	// state.SetEventsFromSource so they don't overwrite each other.
	useGoogleCal := false
	if cfg.Google.ClientID != "" && cfg.Google.ClientSecret != "" && cfg.Google.TokenFile != "" && len(cfg.Google.CalendarsInclude) > 0 {
		useGoogleCal = true
	}
	if len(cfg.Calendars.Include) > 0 || cfg.Reminders.ListName != "" {
		go cd.RunEvents(ctx, time.Duration(cfg.Calendars.PollMinutes)*time.Minute)
	}

	db, err := store.Open(filepath.Join(stateDir(), "homedash.db"))
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer db.Close()

	rp := rss.New(cfg.RSS.Feeds, time.Duration(cfg.RSS.PollMinutes)*time.Minute, st, db)
	go rp.Run(ctx)

	// Google integrations: when configured, supersede the iCloud paths
	// for calendar / reminders / photos. One HTTP client shared across
	// all three so they refresh the OAuth token in one place.
	var googleHC *http.Client
	useGoogle := cfg.Google.ClientID != "" && cfg.Google.ClientSecret != "" && cfg.Google.TokenFile != ""
	if useGoogle {
		gcfg := googleauth.Config{
			ClientID:     cfg.Google.ClientID,
			ClientSecret: cfg.Google.ClientSecret,
			TokenFile:    cfg.Google.TokenFile,
		}
		googleHC, err = gcfg.Client(ctx)
		if err != nil {
			log.Printf("google: %v (run `homedash auth-google` to authorize)", err)
			googleHC = nil
		}
	}

	if useGoogleCal && googleHC != nil {
		gc := gcal.New(googleHC, cfg.Google.CalendarsInclude, st)
		go gc.Run(ctx, time.Duration(cfg.Calendars.PollMinutes)*time.Minute)
	}

	// Reminders may come from multiple sources at once. Build a
	// dispatcher that routes the iPad's "tick the box" toggle to
	// whichever backend owns that particular reminder UID.
	dispatch := &reminderDispatcher{store: st, bySource: map[string]web.ReminderToggler{}}
	dispatch.bySource["icloud"] = cd

	if cfg.Google.TasksListName != "" && googleHC != nil {
		gt := gtasks.New(googleHC, cfg.Google.TasksListName, st)
		dispatch.bySource["google"] = gt
		go gt.Run(ctx, 5*time.Minute)
	}

	if cfg.Todoist.Token != "" && cfg.Todoist.Project != "" {
		td := todoist.New(cfg.Todoist.Token, cfg.Todoist.Project, st)
		dispatch.bySource["todoist"] = td
		go td.Run(ctx, 6*time.Minute)
	}
	toggler = dispatch

	// Photos slideshow always reads from the local cache dir. The dir is
	// filled by either `homedash pick-photos` (Google Photos picker), or
	// the legacy iCloud-webstream poller below, or manual file drop.
	pp := &photos.Poller{
		AlbumURL: cfg.Photos.SharedAlbumURL,
		CacheDir: cfg.Photos.CacheDir,
		Refresh:  time.Duration(cfg.Photos.RefreshHours) * time.Hour,
		Store:    st, DB: db,
	}
	go pp.Run(ctx)

	srv := web.New(st, toggler, cfg.Photos.SlideshowSeconds, cfg.Photos.CacheDir, pp)

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

// runAuthGoogle performs the one-time interactive OAuth flow for Google
// APIs. Reads OAuth client credentials from the env and writes the
// resulting token to the path configured in config.yaml.
func runAuthGoogle() {
	cfgPath := flag.String("config", "/etc/homedash/config.yaml", "path to config.yaml")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	if cfg.Google.TokenFile == "" {
		log.Fatal("google.token_file is not set in config.yaml")
	}
	gc := googleauth.Config{
		ClientID:     cfg.Google.ClientID,
		ClientSecret: cfg.Google.ClientSecret,
		TokenFile:    cfg.Google.TokenFile,
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := gc.Authorize(ctx); err != nil {
		log.Fatalf("auth-google: %v", err)
	}
	fmt.Printf("Google token saved to %s\n", cfg.Google.TokenFile)
}

// runPickPhotos runs the interactive Google Photos picker session.
// Opens the user's browser to Google's picker UI, waits while they
// select photos / an album, then downloads each picked item into the
// configured photos cache directory. The dashboard's slideshow reads
// from that directory regardless of where the files came from.
func runPickPhotos() {
	cfgPath := flag.String("config", "/etc/homedash/config.yaml", "path to config.yaml")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	if cfg.Photos.CacheDir == "" {
		log.Fatal("photos.cache_dir is not set in config.yaml")
	}
	if cfg.Google.TokenFile == "" {
		log.Fatal("google.token_file is not set in config.yaml")
	}

	gc := googleauth.Config{
		ClientID:     cfg.Google.ClientID,
		ClientSecret: cfg.Google.ClientSecret,
		TokenFile:    cfg.Google.TokenFile,
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	hc, err := gc.Client(ctx)
	if err != nil {
		log.Fatalf("google client: %v (run `homedash auth-google` first)", err)
	}
	if err := gpicker.Pick(ctx, hc, cfg.Photos.CacheDir); err != nil {
		log.Fatalf("pick-photos: %v", err)
	}
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
