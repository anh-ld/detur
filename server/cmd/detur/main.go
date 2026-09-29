package main

import (
	"flag"
	"log"
	"net/http"
	"time"

	"detur.dev/server/internal/api"
	"detur.dev/server/internal/config"
	"detur.dev/server/internal/httpx"
	"detur.dev/server/internal/pipeline"
	"detur.dev/server/internal/store"
)

// main wires config -> store -> HTTP listeners. SDK endpoints, browser pipeline, well-known hosting on public listener; portal (API + static UI) on loopback listener, origin/host-guarded. Clicks/events purged at startup, then hourly.
func main() {
	// Not an env var: Docker image passes 0.0.0.0:8081 so host can publish it; bare runs keep unauthenticated portal on loopback.
	portalAddr := flag.String("portal-addr", "127.0.0.1:8081", "portal listen address")
	flag.Parse()
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	httpx.TrustProxy = cfg.TrustProxy // XFF honored only behind a trusted proxy
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("store: %v", err)
	}
	defer st.Close()
	purgeLoop(st, cfg.RetentionHours)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		// healthcheck probes store: wedged DB reads unhealthy, no silent fail-open
		if err := st.Ping(); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("db unavailable"))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	api.RegisterSDK(mux, st, cfg.RetentionHours)
	pipeline.Register(mux, st, cfg.RetentionHours) // GET /{key}: short links, click recording, store redirects
	// Well-known hosting: AASA + assetlinks under configured domains; unknown hosts get 404.
	pipeline.RegisterWellKnown(mux, st, config.DomainSet(cfg))

	// Portal: own listener; static UI from ./portal/dist (missing dir = API only).
	go func() {
		portal := api.RegisterPortal(st, "portal/dist", []string{*portalAddr})
		log.Printf("portal on %s", *portalAddr)
		log.Fatal(serve(*portalAddr, portal))
	}()

	log.Printf("detur :8080 (domain %s, db %s)", cfg.Domain, cfg.DBPath)
	log.Fatal(serve(":8080", mux))
}

// serve: HTTP server with explicit timeouts, slow client cannot pin single-writer SQLite connection or listener goroutines.
func serve(addr string, h http.Handler) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	return srv.ListenAndServe()
}

// purgeLoop: purge expired clicks and old events at startup, then hourly.
func purgeLoop(st *store.Store, retentionHours int) {
	if n, err := st.PurgeExpired(time.Now(), retentionHours); err != nil {
		log.Printf("initial purge failed: %v", err)
	} else if n > 0 {
		log.Printf("purged %d expired rows", n)
	}
	go func() {
		for range time.Tick(time.Hour) {
			if _, err := st.PurgeExpired(time.Now(), retentionHours); err != nil {
				log.Printf("purge failed: %v", err)
			}
		}
	}()
}
