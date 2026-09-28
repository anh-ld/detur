package main

import (
	"log"
	"net/http"
	"time"

	"detur.dev/server/internal/api"
	"detur.dev/server/internal/config"
	"detur.dev/server/internal/httpx"
	"detur.dev/server/internal/pipeline"
	"detur.dev/server/internal/store"
)

// main wires config → store → HTTP listeners. SDK endpoints (U3), the
// browser pipeline (U4) and well-known hosting (U5) run on the public
// listener; the portal (U6, API + static UI) runs on its own loopback
// listener, origin/host-guarded (KTD5). Expired clicks and events are purged
// at startup and hourly (System-Wide Impact retention).
func main() {
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
		// The healthcheck probes the store: a wedged DB is visible as
		// unhealthy instead of silently failing open (R3 fail-open paths
		// keep serving while the funnel dies).
		if err := st.Ping(); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("db unavailable"))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	api.RegisterSDK(mux, st, cfg.RetentionHours)
	pipeline.Register(mux, st, cfg.RetentionHours) // GET /{key}: short links, click recording, store redirects (U4)
	// Well-known hosting (U5, R11/R12): AASA + assetlinks under the
	// configured domains; unknown hosts get 404.
	pipeline.RegisterWellKnown(mux, st, config.DomainSet(cfg))

	// Portal (U6): separate listener (loopback default, KTD5). Extra Host
	// values from PORTAL_HOSTS let a zero-trust tunnel in front pass
	// the guard; a missing static dir logs a warning but the portal API
	// still works.
	portalHosts := append(cfg.PortalHosts, cfg.PortalAddr)
	go func() {
		portal := api.RegisterPortal(st, cfg.PortalDir, portalHosts)
		log.Printf("portal on %s (static %s)", cfg.PortalAddr, cfg.PortalDir)
		log.Fatal(serve(cfg.PortalAddr, portal))
	}()

	log.Printf("detur %s (domain %s, db %s)", cfg.HTTPAddr, cfg.Domain, cfg.DBPath)
	log.Fatal(serve(cfg.HTTPAddr, mux))
}

// serve runs an HTTP server with explicit timeouts so a slow client cannot
// pin the single-writer SQLite connection or the listener goroutines.
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

// purgeLoop purges expired clicks and old events at startup, then hourly.
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
