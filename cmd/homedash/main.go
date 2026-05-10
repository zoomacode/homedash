package main

import (
	"flag"
	"log"
	"net/http"

	"github.com/zoomacode/homedash/internal/config"
	"github.com/zoomacode/homedash/internal/state"
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
	srv := web.New(st)

	log.Printf("homedash listening on %s", cfg.HTTP.Listen)
	if err := http.ListenAndServe(cfg.HTTP.Listen, srv.Handler()); err != nil {
		log.Fatal(err)
	}
}
