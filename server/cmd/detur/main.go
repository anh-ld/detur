package main

import (
	"log"
	"net/http"

	"detur.dev/server/internal/api"
	"detur.dev/server/internal/config"
	"detur.dev/server/internal/pipeline"
	"detur.dev/server/internal/store"
)

// main wires config → store → HTTP listeners. SDK endpoints (U3) and the
// browser pipeline (U4) are registered here; well-known hosting and the
// portal API attach in later units (U5–U6).
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

	log.Printf("detur %s (portal %s, domain %s, db %s)", cfg.HTTPAddr, cfg.PortalAddr, cfg.Domain, cfg.DBPath)
	log.Fatal(http.ListenAndServe(cfg.HTTPAddr, mux))
}
