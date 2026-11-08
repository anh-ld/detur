package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"detur.dev/server/internal/api"
	"detur.dev/server/internal/config"
	"detur.dev/server/internal/httpx"
	"detur.dev/server/internal/pipeline"
	"detur.dev/server/internal/store"
	"detur.dev/server/internal/webhook"
)

// version: set by the Docker build (-ldflags -X main.version=...).
var version = "dev"

// main: config -> store -> HTTP listeners. Public: SDK, browser pipeline, well-known. Loopback: portal (API + static UI), origin/host-guarded. Clicks/events purged at startup, then hourly.
// SIGTERM/SIGINT (docker stop): stop accepting, drain in-flight requests, stop the webhook worker, close the DB.
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// Not env: Docker passes 0.0.0.0:8081 to publish it; bare runs keep unauthenticated portal on loopback.
	portalAddr := flag.String("portal-addr", "127.0.0.1:8081", "portal listen address")
	flag.Parse()
	api.Version = version
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	httpx.TrustProxy = cfg.TrustProxy // XFF honored only behind a trusted proxy
	// empty TRUSTED_PROXIES = loopback + private defaults
	if err := httpx.SetTrustedProxies(cfg.TrustedProxies); err != nil {
		log.Fatalf("trusted proxies: %v", err)
	}
	if cfg.TrustProxy && len(cfg.TrustedProxies) == 0 {
		log.Printf("TRUST_PROXY=1, TRUSTED_PROXIES unset: forwarded headers honored only from loopback + private ranges; proxy on a public IP (Cloudflare, ALB) -> add its CIDR")
	}
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("store: %v", err)
	}
	st.ClickIDHours = cfg.ClickIDDays * 24
	purgeLoop(ctx, st, cfg.RetentionHours)

	webhookWorker := webhook.NewWorker(st, 15*time.Second)
	webhookWorker.Start()

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
	api.RegisterData(mux, st, cfg.AdminPassword)   // read-only data API + MCP, bearer tokens
	pipeline.Register(mux, st, cfg.RetentionHours) // GET /{key}: short links, click recording, store redirects
	// Well-known: AASA + assetlinks for configured domains; unknown host = 404.
	pipeline.RegisterWellKnown(mux, st, config.DomainSet(cfg))

	// Portal: own listener; static UI from ./portal/dist (missing = API only).
	portal := api.RegisterPortal(st, "portal/dist", append([]string{*portalAddr}, cfg.PortalHosts...), cfg.LogoutURL, cfg.AdminPassword, cfg.AdminSessionHours, linkBase(cfg.Domain))
	if cfg.AdminPassword != "" {
		log.Printf("portal admin gating: ON")
	} else {
		log.Printf("portal admin gating: OFF (ADMIN_PASSWORD unset)")
	}
	servers := []*http.Server{server(":8080", mux), server(*portalAddr, portal)}
	errc := make(chan error, len(servers))
	for _, srv := range servers {
		go func() {
			if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
				errc <- err
			}
		}()
	}
	log.Printf("detur :8080 (domain %s, db %s), portal on %s", cfg.Domain, cfg.DBPath, *portalAddr)

	select {
	case err := <-errc:
		log.Printf("listener: %v", err) // e.g. port in use: still shut down cleanly, then exit 1
	case <-ctx.Done():
		log.Printf("shutting down")
	}
	// under docker stop's 10s grace: in-flight requests finish, no new ones
	sctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	for _, srv := range servers {
		if err := srv.Shutdown(sctx); err != nil {
			log.Printf("shutdown %s: %v", srv.Addr, err)
		}
	}
	webhookWorker.Stop()
	if err := st.Close(); err != nil {
		log.Printf("close db: %v", err)
	}
	if ctx.Err() == nil {
		os.Exit(1)
	}
}

// server: explicit timeouts so slow clients can't pin the single-writer SQLite conn or listener goroutines.
func server(addr string, h http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    32 << 10, // UA/hints/query land in click rows: bounds each row, not just the request
	}
}

// purgeLoop: startup, then hourly until ctx ends. A panicking purge is logged, never kills the process.
func purgeLoop(ctx context.Context, st *store.Store, retentionHours int) {
	purge := func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("purge panic: %v", r)
			}
		}()
		if n, err := st.PurgeExpired(time.Now(), retentionHours); err != nil {
			log.Printf("purge failed: %v", err)
		} else if n > 0 {
			log.Printf("purged %d expired rows", n)
		}
	}
	purge()
	go func() {
		t := time.NewTicker(time.Hour)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				purge()
			}
		}
	}()
}

// linkBase: short-link origin for the portal. Dev (localhost) = public listener.
func linkBase(domain string) string {
	if domain == "localhost" {
		return "http://localhost:8080"
	}
	return "https://" + domain
}
