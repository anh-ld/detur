package api

// Portal: apps/links CRUD, matching settings, analytics, static
// hosting of kinu-built portal UI. No identity checks; every route wrapped in
// origin/host guard (DNS-rebinding + CSRF; portal listener loopback by default).

import (
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"detur.dev/server/internal/fraud"
	"detur.dev/server/internal/httpx"
	"detur.dev/server/internal/match"
	"detur.dev/server/internal/store"
)

// portal matching defaults come from store's exported defaults; settings table overrides at runtime.
type portalServer struct {
	st     *store.Store
	log    *log.Logger
	dir    string // portal static dir (built UI)
	logout string // LOGOUT_URL, served to the UI via /api/config
	// admin gating: adminPassword "" = no gating, every route behaves as before admin mode existed.
	adminPassword string
	adminHours    int
	adminMu       sync.Mutex           // guards adminLocks
	adminLocks    map[string]*peerLock // per socket peer: serializes password compare + wrong-password delay
}

// RegisterPortal builds portal handler: apps/links CRUD, app matching, analytics routes, plus static UI
// from staticDir (missing files/dir 404 plain text, never crash). Mux wrapped in origin/host guard;
// allowedHosts = listener's own address, loopback always accepted, others 403. Viewer routes open to
// anyone the gateway admits; app-management routes gated by requireAdmin. adminPassword "" = no gating
// (today's single-operator behavior); adminSessionHours bounds the elevated session TTL (config-bounded 1-72).
func RegisterPortal(st *store.Store, staticDir string, allowedHosts []string, logoutURL string, adminPassword string, adminSessionHours int) http.Handler {
	p := &portalServer{st: st, log: log.Default(), dir: staticDir, logout: logoutURL,
		adminPassword: adminPassword, adminHours: adminSessionHours, adminLocks: map[string]*peerLock{}}
	if fi, err := os.Stat(staticDir); err != nil || !fi.IsDir() {
		p.log.Printf("portal static dir %q missing: portal API only, UI will 404", staticDir)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/config", p.config)
	mux.HandleFunc("GET /api/apps", p.listApps)
	mux.HandleFunc("POST /api/apps", p.requireAdmin(p.createApp))
	mux.HandleFunc("POST /api/apps/{id}/rotate-key", p.requireAdmin(p.rotateAppKey))
	mux.HandleFunc("DELETE /api/apps/{id}/key", p.requireAdmin(p.revokeAppKey))
	mux.HandleFunc("PATCH /api/apps/{id}", p.requireAdmin(p.updateApp))
	mux.HandleFunc("PATCH /api/apps/{id}/matching", p.requireAdmin(p.updateMatching))
	mux.HandleFunc("GET /api/apps/{id}", p.getApp)
	mux.HandleFunc("DELETE /api/apps/{id}", p.requireAdmin(p.deleteApp))
	mux.HandleFunc("GET /api/apps/{id}/links", p.listLinks)
	mux.HandleFunc("POST /api/apps/{id}/links", p.createLink)
	mux.HandleFunc("PATCH /api/links/{id}", p.updateLink)
	mux.HandleFunc("DELETE /api/links/{id}", p.deleteLink)
	mux.HandleFunc("GET /api/apps/{id}/analytics", p.analytics)
	mux.HandleFunc("GET /api/apps/{id}/match-quality", p.matchQuality)
	mux.HandleFunc("GET /api/apps/{id}/health", p.health)
	mux.HandleFunc("GET /api/apps/{id}/fraud", p.fraud)
	mux.HandleFunc("GET /api/apps/{id}/fraud/settings", p.requireAdmin(p.fraudSettings))
	mux.HandleFunc("PATCH /api/apps/{id}/fraud/settings", p.requireAdmin(p.updateFraudSettings))
	mux.HandleFunc("GET /api/apps/{id}/webhooks", p.requireWebhookAdmin(p.listWebhooks))
	mux.HandleFunc("POST /api/apps/{id}/webhooks", p.requireWebhookAdmin(p.createWebhook))
	mux.HandleFunc("PATCH /api/webhooks/{id}", p.requireWebhookAdmin(p.updateWebhook))
	mux.HandleFunc("DELETE /api/webhooks/{id}", p.requireWebhookAdmin(p.deleteWebhook))
	mux.HandleFunc("POST /api/webhooks/{id}/rotate-secret", p.requireWebhookAdmin(p.rotateWebhookSecret))
	mux.HandleFunc("POST /api/webhooks/{id}/replay", p.requireWebhookAdmin(p.replayWebhook))
	mux.HandleFunc("GET /api/admin/session", p.sessionStatus)
	mux.HandleFunc("POST /api/admin/session", p.elevate)
	mux.HandleFunc("DELETE /api/admin/session", p.clearSession)
	mux.HandleFunc("GET /", p.static) // SPA shell + assets (catch-all)
	return guard(mux, allowedHosts)
}

// guard wraps portal mux with origin/host check: non-loopback/unallowed Host, or Origin naming different host, 403. DNS-rebinding + CSRF guard; no identity checks.
func guard(next http.Handler, allowedHosts []string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !hostAllowed(r.Host, allowedHosts) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if o := r.Header.Get("Origin"); o != "" && !originAllowed(o, allowedHosts) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// hostAllowed: request Host loopback or one of allowed hosts (listener's own address + tunnel hosts). Hostnames compared without port: published/zero-trust port in front still passes.
func hostAllowed(host string, allowedHosts []string) bool {
	h := hostnameOf(host)
	if h == "127.0.0.1" || h == "::1" || h == "localhost" {
		return true
	}
	for _, a := range allowedHosts {
		if hostnameOf(a) == h {
			return true
		}
	}
	return false
}

// originAllowed: Origin header (scheme://host[:port]) names loopback or allowed host. Missing Origins (curl, same-origin GETs without CORS preflight) pass guard untouched.
func originAllowed(origin string, allowedHosts []string) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	return hostAllowed(u.Host, allowedHosts)
}

// hostnameOf: lowercase hostname from "host[:port]" (bracket-tolerant for IPv6; bare IPv6 like "::1" has no port to strip).
func hostnameOf(host string) string {
	h := strings.ToLower(strings.TrimSpace(host))
	if host, _, err := net.SplitHostPort(h); err == nil {
		return strings.Trim(host, "[]")
	}
	return strings.Trim(h, "[]")
}

// appJSON: wire shape for apps. API keys never included, only hash; plaintext key appears in exactly one response: create call (show-once semantics).
type appJSON struct {
	ID                     string `json:"id"`
	Name                   string `json:"name"`
	APIKeyHash             string `json:"apiKeyHash"`
	IOSAppID               string `json:"iosAppId"`
	AndroidPackage         string `json:"androidPackage"`
	AndroidCertFingerprint string `json:"androidCertFingerprint"`
	MatchThreshold         int    `json:"matchThreshold"`
	MatchWindowMinutes     int    `json:"matchWindowMinutes"`
}

func toAppJSON(a store.App) appJSON {
	return appJSON{ID: a.ID, Name: a.Name, APIKeyHash: a.APIKeyHash,
		IOSAppID: a.IOSAppID, AndroidPackage: a.AndroidPackage, AndroidCertFingerprint: a.AndroidCertFingerprint,
		MatchThreshold: a.MatchThreshold, MatchWindowMinutes: a.MatchWindowMinutes}
}

// linkJSON: wire shape for links.
type linkJSON struct {
	ID          string `json:"id"`
	AppID       string `json:"appId"`
	Key         string `json:"key"`
	URL         string `json:"url"`
	IOS         string `json:"ios"`
	Android     string `json:"android"`
	FallbackURL string `json:"fallbackUrl"`
	ExpiresAt   string `json:"expiresAt"` // RFC3339; empty = never
	ExpiredURL  string `json:"expiredUrl"`
}

func toLinkJSON(l store.Link) linkJSON {
	j := linkJSON{ID: l.ID, AppID: l.AppID, Key: l.Key, URL: l.URL, IOS: l.IOS,
		Android: l.Android, FallbackURL: l.FallbackURL,
		ExpiredURL: l.ExpiredURL}
	if l.ExpiresAt != nil {
		j.ExpiresAt = l.ExpiresAt.UTC().Format(time.RFC3339)
	}
	return j
}

// linkBody: portal link payload (create + update share it). Omitted update fields unchanged.
type linkBody struct {
	Key         string  `json:"key"`
	URL         string  `json:"url"`
	IOS         *string `json:"ios"`
	Android     *string `json:"android"`
	FallbackURL *string `json:"fallbackUrl"`
	ExpiresAt   *string `json:"expiresAt"` // RFC3339; "" clears
	ExpiredURL  *string `json:"expiredUrl"`
}

// parseExpiry: optional RFC3339 expiry; nil or "" = never.
func parseExpiry(v *string) (*time.Time, error) {
	if v == nil || strings.TrimSpace(*v) == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(*v))
	if err != nil {
		return nil, errors.New("expiresAt must be RFC3339, e.g. 2026-12-31T23:59:59Z")
	}
	return &t, nil
}

// config: UI settings from env. logoutUrl "" = no Log out link; adminSet tells the UI whether admin elevation exists (ADMIN_PASSWORD configured).
func (p *portalServer) config(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"logoutUrl": p.logout, "adminSet": p.adminPassword != ""})
}

func (p *portalServer) listApps(w http.ResponseWriter, r *http.Request) {
	apps, err := p.st.ListApps()
	if err != nil {
		p.internal(w, err)
		return
	}
	out := make([]appJSON, 0, len(apps))
	for _, a := range apps {
		out = append(out, toAppJSON(a))
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (p *portalServer) getApp(w http.ResponseWriter, r *http.Request) {
	a, err := p.st.GetApp(r.PathValue("id"))
	if err != nil {
		p.storeErr(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toAppJSON(a))
}

func (p *portalServer) createApp(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if body.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	// Server-minted key: "dk_" + 22 base62 (~131 bits), 25 chars total.
	apiKey := "dk_" + store.Nanoid(22)
	a, err := p.st.CreateApp(body.Name, apiKey)
	if err != nil {
		p.internal(w, err)
		return
	}
	// show-once semantics: plaintext key rides this one response; later reads return only hash
	httpx.WriteJSON(w, http.StatusCreated, map[string]string{
		"id": a.ID, "name": a.Name, "apiKey": apiKey, "apiKeyHash": a.APIKeyHash,
	})
}

// rotateAppKey mints new key, swaps stored hash; old key dies immediately. Plaintext rides this one response, like create.
func (p *portalServer) rotateAppKey(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	apiKey := "dk_" + store.Nanoid(22)
	if err := p.st.UpdateAppKey(id, apiKey); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		p.internal(w, err)
		return
	}
	app, err := p.st.GetApp(id)
	if err != nil {
		p.internal(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]string{
		"id": app.ID, "name": app.Name, "apiKey": apiKey, "apiKeyHash": app.APIKeyHash,
	})
}

// revokeAppKey clears stored hash: SDK stops accepting key.
func (p *portalServer) revokeAppKey(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := p.st.ClearAppKey(id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		p.internal(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (p *portalServer) updateApp(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IOSAppID               string `json:"iosAppId"`
		AndroidPackage         string `json:"androidPackage"`
		AndroidCertFingerprint string `json:"androidCertFingerprint"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	id := r.PathValue("id")
	if err := p.st.UpdateAppDetails(id, body.IOSAppID, body.AndroidPackage, body.AndroidCertFingerprint); err != nil {
		p.storeErr(w, err)
		return
	}
	a, err := p.st.GetApp(id)
	if err != nil {
		p.storeErr(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toAppJSON(a))
}

func (p *portalServer) deleteApp(w http.ResponseWriter, r *http.Request) {
	if err := p.st.DeleteApp(r.PathValue("id")); err != nil {
		p.storeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (p *portalServer) listLinks(w http.ResponseWriter, r *http.Request) {
	links, err := p.st.ListLinks(r.PathValue("id"))
	if err != nil {
		p.internal(w, err)
		return
	}
	out := make([]linkJSON, 0, len(links))
	for _, l := range links {
		out = append(out, toLinkJSON(l))
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// validTarget: absolute URL with scheme; http(s) needs host. Empty allowed (optional fields). Scripting schemes rejected.
func validTarget(field, v string) error {
	if v == "" {
		return nil
	}
	u, err := url.Parse(v)
	if err != nil || u.Scheme == "" {
		return fmt.Errorf("%s must be an absolute URL", field)
	}
	switch strings.ToLower(u.Scheme) {
	case "javascript", "data", "vbscript", "file":
		return fmt.Errorf("%s scheme %q not allowed", field, u.Scheme)
	case "http", "https":
		if u.Host == "" {
			return fmt.Errorf("%s must be an absolute URL", field)
		}
	}
	return nil
}

// validTargets: first bad destination field wins.
func validTargets(l store.Link) error {
	for _, f := range []struct{ name, v string }{
		{"url", l.URL}, {"ios", l.IOS}, {"android", l.Android}, {"fallbackUrl", l.FallbackURL}, {"expiredUrl", l.ExpiredURL},
	} {
		if err := validTarget(f.name, f.v); err != nil {
			return err
		}
	}
	return nil
}

func (p *portalServer) createLink(w http.ResponseWriter, r *http.Request) {
	appID := r.PathValue("id")
	var body linkBody
	if err := decodeJSON(w, r, &body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	body.Key = strings.TrimSpace(body.Key)
	body.URL = strings.TrimSpace(body.URL)
	if body.Key == "" || body.URL == "" {
		http.Error(w, "key and url are required", http.StatusBadRequest)
		return
	}
	// short keys live in URL path: restrict to path-safe characters, key never shadows registered route or breaks pipeline
	if !linkKeyRe.MatchString(body.Key) || isReservedKey(body.Key) {
		http.Error(w, "key must be 1-64 chars of [A-Za-z0-9_-], not starting with _", http.StatusBadRequest)
		return
	}
	expiresAt, err := parseExpiry(body.ExpiresAt)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	nl := store.Link{
		AppID: appID, Key: body.Key, URL: body.URL,
		IOS: strings.TrimSpace(stringValue(body.IOS)), Android: strings.TrimSpace(stringValue(body.Android)),
		FallbackURL: strings.TrimSpace(stringValue(body.FallbackURL)),
		ExpiresAt:   expiresAt, ExpiredURL: strings.TrimSpace(stringValue(body.ExpiredURL)),
	}
	if err := validTargets(nl); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	l, err := p.st.CreateLink(nl)
	if err != nil {
		p.storeErr(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, toLinkJSON(l))
}

func (p *portalServer) updateLink(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	cur, err := p.st.GetLink(id)
	if err != nil {
		p.storeErr(w, err)
		return
	}
	var body linkBody
	if err := decodeJSON(w, r, &body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	l := cur
	if body.URL = strings.TrimSpace(body.URL); body.URL != "" {
		l.URL = body.URL
	}
	if body.IOS != nil {
		l.IOS = strings.TrimSpace(*body.IOS)
	}
	if body.Android != nil {
		l.Android = strings.TrimSpace(*body.Android)
	}
	if body.FallbackURL != nil {
		l.FallbackURL = strings.TrimSpace(*body.FallbackURL)
	}
	if body.ExpiresAt != nil {
		if l.ExpiresAt, err = parseExpiry(body.ExpiresAt); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	if body.ExpiredURL != nil {
		l.ExpiredURL = strings.TrimSpace(*body.ExpiredURL)
	}
	if err := validTargets(l); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := p.st.UpdateLink(l); err != nil {
		p.storeErr(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toLinkJSON(l))
}

func (p *portalServer) deleteLink(w http.ResponseWriter, r *http.Request) {
	if err := p.st.DeleteLink(r.PathValue("id")); err != nil {
		p.storeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// updateMatching: app's match threshold + window (Detour: app-level). Both required.
func (p *portalServer) updateMatching(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Threshold     *int `json:"threshold"`
		WindowMinutes *int `json:"windowMinutes"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if body.Threshold == nil || body.WindowMinutes == nil {
		http.Error(w, "threshold and windowMinutes are required", http.StatusBadRequest)
		return
	}
	if err := match.ValidateSettings(*body.Threshold, *body.WindowMinutes); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := p.st.UpdateAppMatchSettings(id, *body.Threshold, *body.WindowMinutes); err != nil {
		p.storeErr(w, err)
		return
	}
	a, err := p.st.GetApp(id)
	if err != nil {
		p.storeErr(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toAppJSON(a))
}

// analyticsRanges: allowed ?days= windows (portal range picker).
var analyticsRanges = map[string]int{"7": 7, "30": 30, "90": 90}

// analytics serves daily click/install/event stats: ?days=7|30|90 (default 7), ?platform=ios|android|desktop (default all).
func (p *portalServer) analytics(w http.ResponseWriter, r *http.Request) {
	appID := r.PathValue("id")
	if _, err := p.st.GetApp(appID); err != nil {
		p.storeErr(w, err)
		return
	}
	q := r.URL.Query()
	days, ok := parseDays(w, r, 7)
	if !ok {
		return
	}
	platform := q.Get("platform")
	if platform != "" && platform != "ios" && platform != "android" && platform != "desktop" {
		http.Error(w, "platform must be ios, android or desktop", http.StatusBadRequest)
		return
	}
	a, err := p.st.Analytics(appID, days, platform, time.Now())
	if err != nil {
		p.internal(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, a)
}

// matchQuality: GET /api/apps/{id}/match-quality?days=7|30|90 (default 30).
func (p *portalServer) matchQuality(w http.ResponseWriter, r *http.Request) {
	appID := r.PathValue("id")
	if _, err := p.st.GetApp(appID); err != nil {
		p.storeErr(w, err)
		return
	}
	days, ok := parseDays(w, r, 30)
	if !ok {
		return
	}
	q, err := p.st.MatchQuality(appID, days, time.Now())
	if err != nil {
		p.internal(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, q)
}

// fraudInstallJSON: flagged install row for the portal; link ids resolved to keys ("" = link deleted). No device hash.
type fraudInstallJSON struct {
	ID           string    `json:"id"`
	CreatedAt    time.Time `json:"createdAt"`
	Attribution  string    `json:"attribution"`
	Method       string    `json:"method"`
	LinkKey      string    `json:"linkKey"`
	Platform     string    `json:"platform"`
	Fraud        []string  `json:"fraud"`
	FraudAction  string    `json:"fraudAction"`
	FraudLinkKey string    `json:"fraudLinkKey"`
}

// fraud: GET /api/apps/{id}/fraud?days=7|30|90 (default 7): per-signal counts + latest flagged installs.
func (p *portalServer) fraud(w http.ResponseWriter, r *http.Request) {
	appID := r.PathValue("id")
	if _, err := p.st.GetApp(appID); err != nil {
		p.storeErr(w, err)
		return
	}
	days, ok := parseDays(w, r, 7)
	if !ok {
		return
	}
	f, err := p.st.Fraud(appID, days, time.Now())
	if err != nil {
		p.internal(w, err)
		return
	}
	links, err := p.st.ListLinks(appID)
	if err != nil {
		p.internal(w, err)
		return
	}
	keys := make(map[string]string, len(links))
	for _, l := range links {
		keys[l.ID] = l.Key
	}
	out := make([]fraudInstallJSON, 0, len(f.Installs))
	for _, in := range f.Installs {
		out = append(out, fraudInstallJSON{ID: in.ID, CreatedAt: in.CreatedAt, Attribution: in.Attribution, Method: in.Method,
			LinkKey: keys[in.LinkID], Platform: in.Platform, Fraud: in.Fraud, FraudAction: in.FraudAction, FraudLinkKey: keys[in.FraudLinkID]})
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"signals": f.Signals, "installs": out})
}

// fraudSettingsJSON: fraud settings wire shape. Pointers so PATCH can require every field.
type fraudSettingsJSON struct {
	VelocityMode          *string `json:"velocityMode"`
	TimingMode            *string `json:"timingMode"`
	UserAgentMode         *string `json:"userAgentMode"`
	IPMode                *string `json:"ipMode"`
	VelocityIPMax         *int    `json:"velocityIpMax"`
	VelocityLinkMax       *int    `json:"velocityLinkMax"`
	VelocityWindowMinutes *int    `json:"velocityWindowMinutes"`
	TimingShortSeconds    *int    `json:"timingShortSeconds"`
	TimingLongHours       *int    `json:"timingLongHours"`
	FingerprintMax        *int    `json:"fingerprintMax"`
	FingerprintWindowDays *int    `json:"fingerprintWindowDays"`
}

// fraudSettings: GET /api/apps/{id}/fraud/settings.
func (p *portalServer) fraudSettings(w http.ResponseWriter, r *http.Request) {
	p.writeFraudSettings(w, r.PathValue("id"))
}

// parseDays: ?days= as 7, 30 or 90 (def when absent); else 400 written, ok false.
func parseDays(w http.ResponseWriter, r *http.Request, def int) (int, bool) {
	v := r.URL.Query().Get("days")
	if v == "" {
		return def, true
	}
	days := analyticsRanges[v]
	if days == 0 {
		http.Error(w, "days must be 7, 30 or 90", http.StatusBadRequest)
		return 0, false
	}
	return days, true
}

func (p *portalServer) writeFraudSettings(w http.ResponseWriter, appID string) {
	if _, err := p.st.GetApp(appID); err != nil {
		p.storeErr(w, err)
		return
	}
	s, err := p.st.FraudSettings(appID)
	if err != nil {
		p.internal(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, fraudSettingsJSON{
		VelocityMode: &s.VelocityMode, TimingMode: &s.TimingMode, UserAgentMode: &s.UserAgentMode, IPMode: &s.IPMode,
		VelocityIPMax: &s.VelocityIPMax, VelocityLinkMax: &s.VelocityLinkMax, VelocityWindowMinutes: &s.VelocityWindowMinutes,
		TimingShortSeconds: &s.TimingShortSeconds, TimingLongHours: &s.TimingLongHours,
		FingerprintMax: &s.FingerprintMax, FingerprintWindowDays: &s.FingerprintWindowDays,
	})
}

// updateFraudSettings: PATCH /api/apps/{id}/fraud/settings: every field required, validated, upserted; responds like GET.
func (p *portalServer) updateFraudSettings(w http.ResponseWriter, r *http.Request) {
	var b fraudSettingsJSON
	if err := decodeJSON(w, r, &b); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	modes := []*string{b.VelocityMode, b.TimingMode, b.UserAgentMode, b.IPMode}
	nums := []*int{b.VelocityIPMax, b.VelocityLinkMax, b.VelocityWindowMinutes, b.TimingShortSeconds, b.TimingLongHours,
		b.FingerprintMax, b.FingerprintWindowDays}
	if slices.Contains(modes, nil) || slices.Contains(nums, nil) {
		http.Error(w, "all fraud settings fields are required", http.StatusBadRequest)
		return
	}
	s := fraud.Settings{VelocityMode: *b.VelocityMode, TimingMode: *b.TimingMode, UserAgentMode: *b.UserAgentMode, IPMode: *b.IPMode,
		VelocityIPMax: *b.VelocityIPMax, VelocityLinkMax: *b.VelocityLinkMax, VelocityWindowMinutes: *b.VelocityWindowMinutes,
		TimingShortSeconds: *b.TimingShortSeconds, TimingLongHours: *b.TimingLongHours,
		FingerprintMax: *b.FingerprintMax, FingerprintWindowDays: *b.FingerprintWindowDays}
	if err := s.Validate(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	id := r.PathValue("id")
	if err := p.st.UpdateFraudSettings(id, s); err != nil {
		p.storeErr(w, err)
		return
	}
	p.writeFraudSettings(w, id)
}

// static serves built portal from configured directory. Missing dir/file 404s plain text (FileServer); server keeps running.
func (p *portalServer) static(w http.ResponseWriter, r *http.Request) {
	http.FileServer(http.Dir(p.dir)).ServeHTTP(w, r)
}

// storeErr maps store errors to HTTP responses.
func (p *portalServer) storeErr(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if errors.Is(err, store.ErrKeyConflict) {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	p.internal(w, err)
}

func stringValue(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func (p *portalServer) internal(w http.ResponseWriter, err error) {
	p.log.Printf("portal backend error: %v", err)
	http.Error(w, "internal error", http.StatusInternalServerError)
}

// linkKeyRe: short-link key charset (URL-path-safe, single segment).
var linkKeyRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// isReservedKey: reject keys shadowing registered routes (case-insensitive match) or "_" prefix Dub reserves (_root).
func isReservedKey(key string) bool {
	key = strings.ToLower(key)
	switch key {
	case "health", "api", "favicon.ico":
		return true
	}
	return strings.HasPrefix(key, ".well-known") || strings.HasPrefix(key, "_")
}
