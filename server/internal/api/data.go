package api

// Data API: read-only, bearer-token JSON on the public port. Reuses portal read handlers behind a token gate;
// app-scoped routes check the token's explicit app list first (not allowed = same 404 as unknown app).
// Handlers whose portal shape leaks (key hash, per-install rows, server-wide health) get their own.

import (
	"context"
	"log"
	"net/http"
	"slices"
	"time"

	"detur.dev/server/internal/httpx"
	"detur.dev/server/internal/store"
)

type dataServer struct {
	p   *portalServer
	st  *store.Store
	log *log.Logger
	v1  *http.ServeMux // also served in-process to MCP tool calls
	// adminPassword "" = every token refused: removing the password must not leave unrevocable credentials live.
	adminPassword string
}

type allowedKey struct{}

// RegisterData: GET /api/v1/... reads + POST /api/mcp on the public mux.
func RegisterData(mux *http.ServeMux, st *store.Store, adminPassword string) {
	d := &dataServer{st: st, log: log.Default(), adminPassword: adminPassword, v1: http.NewServeMux()}
	d.p = &portalServer{st: st, log: d.log}
	get := func(pattern string, h http.HandlerFunc, scoped bool) {
		if scoped {
			h = d.scoped(h)
		}
		d.v1.HandleFunc("GET "+pattern, d.auth(h))
	}
	get("/api/v1/apps", d.listApps, false)
	get("/api/v1/apps/{id}", d.getApp, true)
	get("/api/v1/apps/{id}/links", d.p.listLinks, true)
	get("/api/v1/apps/{app_id}/links/{link_id}/rules", d.p.getLinkRules, true)
	get("/api/v1/apps/{app_id}/links/{link_id}/variants", d.p.getLinkVariants, true)
	get("/api/v1/apps/{id}/analytics", d.p.analytics, true)
	get("/api/v1/apps/{id}/match-quality", d.p.matchQuality, true)
	get("/api/v1/apps/{id}/fraud", d.fraud, true)
	get("/api/v1/apps/{id}/health", d.health, true)
	mux.Handle("/api/v1/", d.v1) // non-GET -> 405 from v1: no write routes exist
	mux.HandleFunc("POST /api/mcp", d.auth(d.mcp))
}

// auth: no browser origins (DNS rebinding), password set, valid bearer token. Allowed app set goes in the context.
func (d *dataServer) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Origin") != "" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		plain, ok := bearerKey(r.Header.Get("Authorization"))
		if d.adminPassword == "" || !ok || plain == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		t, err := d.st.TokenByPlain(plain)
		if err != nil {
			if err != store.ErrNotFound {
				d.log.Printf("data api token lookup: %v", err)
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if err := d.st.TouchToken(t.ID, time.Now()); err != nil {
			d.log.Printf("data api touch: %v", err) // never blocks the read
		}
		next(w, r.WithContext(context.WithValue(r.Context(), allowedKey{}, t.AppIDs)))
	}
}

func allowed(r *http.Request) []string {
	ids, _ := r.Context().Value(allowedKey{}).([]string)
	return ids
}

// scoped: app in path must be in the token's list, checked before the handler runs.
func (d *dataServer) scoped(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" {
			id = r.PathValue("app_id")
		}
		if !slices.Contains(allowed(r), id) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		next(w, r)
	}
}

// dataAppJSON: non-secret app info only (no key hash, cert fingerprint, page HTML).
type dataAppJSON struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	IOSAppID       string `json:"iosAppId"`
	AndroidPackage string `json:"androidPackage"`
	IOSScheme      string `json:"iosScheme"`
}

func toDataApp(a store.App) dataAppJSON {
	return dataAppJSON{ID: a.ID, Name: a.Name, IOSAppID: a.IOSAppID, AndroidPackage: a.AndroidPackage, IOSScheme: a.IOSScheme}
}

func (d *dataServer) listApps(w http.ResponseWriter, r *http.Request) {
	apps, err := d.st.ListApps()
	if err != nil {
		d.p.internal(w, err)
		return
	}
	ids := allowed(r)
	out := []dataAppJSON{}
	for _, a := range apps {
		if slices.Contains(ids, a.ID) {
			out = append(out, toDataApp(a))
		}
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (d *dataServer) getApp(w http.ResponseWriter, r *http.Request) {
	a, err := d.st.GetApp(r.PathValue("id"))
	if err != nil {
		d.p.storeErr(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toDataApp(a))
}

// fraud: per-signal counts only; flagged install rows stay in the portal.
func (d *dataServer) fraud(w http.ResponseWriter, r *http.Request) {
	appID := r.PathValue("id")
	if _, err := d.st.GetApp(appID); err != nil {
		d.p.storeErr(w, err)
		return
	}
	from, to, ok := parseDateRange(w, r, 7)
	if !ok {
		return
	}
	sig, err := d.st.FraudSignalsRange(appID, from, to)
	if err != nil {
		d.p.internal(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"signals": sig})
}

// health: app-scoped checks only; server-wide Proxy / Forwarded headers checks (peer IPs) dropped.
func (d *dataServer) health(w http.ResponseWriter, r *http.Request) {
	app, err := d.st.GetApp(r.PathValue("id"))
	if err != nil {
		d.p.storeErr(w, err)
		return
	}
	checks, err := d.p.healthChecks(app, time.Now())
	if err != nil {
		d.p.internal(w, err)
		return
	}
	out := make([]healthCheck, 0, len(checks))
	for _, c := range checks {
		if c.Check != "Proxy" && c.Check != "Forwarded headers" {
			out = append(out, c)
		}
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}
