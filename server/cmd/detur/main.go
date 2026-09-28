package main

import (
	"log"
	"net/http"
	"os"

	"detur.dev/server/internal/api"
	"detur.dev/server/internal/config"
	"detur.dev/server/internal/pipeline"
	"detur.dev/server/internal/store"
)

// main wires config → store → HTTP listeners. SDK endpoints (U3), the
// browser pipeline (U4) and well-known hosting (U5) run on the public
// listener; the portal (U6, API + static UI) runs on its own loopback
// listener, origin/host-guarded (KTD5).
func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("store: %v", err)
	}
	defer st.Close()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	api.RegisterSDK(mux, st)
	pipeline.Register(mux, st) // GET /{key}: short links, click recording, store redirects (U4)
	// Well-known hosting (U5, R11/R12): AASA + assetlinks under the
	// configured domains; unknown hosts get 404.
	pipeline.RegisterWellKnown(mux, st, config.DomainSet(cfg))

	// Portal (U6): separate listener (loopback default, KTD5). The static
	// dir defaults to portal/dist; a missing dir logs a warning but the
	// portal API still works.
	portalDir := os.Getenv("DETUR_PORTAL_DIR")
	if portalDir == "" {
		portalDir = "portal/dist"
	}
	go func() {
		portal := api.RegisterPortal(st, portalDir, cfg.PortalAddr)
		log.Printf("portal on %s (static %s)", cfg.PortalAddr, portalDir)
		log.Fatal(http.ListenAndServe(cfg.PortalAddr, portal))
	}()

	log.Printf("detur %s (domain %s, db %s)", cfg.HTTPAddr, cfg.Domain, cfg.DBPath)
	log.Fatal(http.ListenAndServe(cfg.HTTPAddr, mux))
}
