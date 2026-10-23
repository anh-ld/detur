package main

import (
	"flag"
	"log"
	"net/http"
	"strings"
	"time"

	"detur.dev/server/internal/api"
	"detur.dev/server/internal/config"
	"detur.dev/server/internal/httpx"
	"detur.dev/server/internal/pipeline"
	"detur.dev/server/internal/store"
	"detur.dev/server/internal/webhook"
)

// main: config -> store -> HTTP listeners. Public: SDK, browser pipeline, well-known. Loopback: portal (API + static UI), origin/host-guarded. Clicks/events purged at startup, then hourly.
func main() {
	// Not env: Docker passes 0.0.0.0:8081 to publish it; bare runs keep unauthenticated portal on loopback.
	portalAddr := flag.String("portal-addr", "127.0.0.1:8081", "portal listen address")
	flag.Parse()
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	httpx.TrustProxy = cfg.TrustProxy // XFF honored only behind a trusted proxy
	if cfg.TrustedProxies != "" {
		var cidrs []string
		for _, c := range strings.Split(cfg.TrustedProxies, ",") {
			if c = strings.TrimSpace(c); c != "" {
				cidrs = append(cidrs, c)
			}
		}
		if err := httpx.SetTrustedProxies(cidrs); err != nil {
			log.Fatalf("trusted proxies: %v", err)
		}
	}
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("store: %v", err)
	}
	defer st.Close()
	st.ClickIDHours = cfg.ClickIDDays * 24
	purgeLoop(st, cfg.RetentionHours)

	webhookWorker := webhook.NewWorker(st, 15*time.Second)
	webhookWorker.Start()
	defer webhookWorker.Stop()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		// probes store: wedged DB = unhealthy, no silent fail-open
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
	// Well-known: AASA + assetlinks for configured domains; unknown host = 404.
	pipeline.RegisterWellKnown(mux, st, config.DomainSet(cfg))

	// Portal: own listener; static UI from ./portal/dist (missing = API only).
	go func() {
		portal := api.RegisterPortal(st, "portal/dist", append([]string{*portalAddr}, cfg.PortalHosts...), cfg.LogoutURL, cfg.AdminPassword, cfg.AdminSessionHours)
		if cfg.AdminPassword != "" {
			log.Printf("portal admin gating: ON")
		} else {
			log.Printf("portal admin gating: OFF (ADMIN_PASSWORD unset)")
		}
		log.Printf("portal on %s", *portalAddr)
		log.Fatal(serve(*portalAddr, portal))
	}()

	log.Printf("detur :8080 (domain %s, db %s)", cfg.Domain, cfg.DBPath)
	log.Fatal(serve(":8080", mux))
}

// serve: explicit timeouts so slow clients can't pin the single-writer SQLite conn or listener goroutines.
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

// purgeLoop: startup, then hourly.
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
