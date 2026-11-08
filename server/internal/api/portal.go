package api

// Portal: apps/links CRUD, matching settings, analytics, static kinu-built UI. No identity checks;
// every route behind origin/host guard (DNS-rebinding + CSRF; listener loopback by default).

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
	"unicode/utf8"

	"detur.dev/server/internal/fraud"
	"detur.dev/server/internal/httpx"
	"detur.dev/server/internal/match"
	"detur.dev/server/internal/page"
	"detur.dev/server/internal/store"
)

// Version: release tag without "v", set at startup from the -ldflags build value; "dev" for source runs.
var Version = "dev"

// matching defaults from store's exported defaults; settings table overrides at runtime.
type portalServer struct {
	linkBase string // short-link origin shown in the portal (https://DOMAIN)
	st       *store.Store
	log      *log.Logger
	dir      string // portal static dir (built UI)
	logout   string // LOGOUT_URL, served to the UI via /api/config
	// adminPassword "" = no gating.
	adminPassword string
	adminHours    int
	adminMu       sync.Mutex           // guards adminLocks
	adminLocks    map[string]*peerLock // per socket peer: serializes password compare + wrong-password delay
}

// RegisterPortal: apps/links CRUD, matching, analytics routes + static UI from staticDir (missing file/dir
// = plain 404, never crash). Origin/host guard on mux; allowedHosts = listener address + PORTAL_HOSTS, loopback
// always ok, others 403. Viewer routes open to anyone the gateway admits; app management behind requireAdmin.
// adminPassword "" = no gating; adminSessionHours = elevated session TTL (1-72).
func RegisterPortal(st *store.Store, staticDir string, allowedHosts []string, logoutURL string, adminPassword string, adminSessionHours int, linkBase string) http.Handler {
	p := &portalServer{st: st, log: log.Default(), dir: staticDir, logout: logoutURL, linkBase: linkBase,
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
	mux.HandleFunc("GET /api/apps/{id}/page", p.requireAdmin(p.getPage))
	mux.HandleFunc("PUT /api/apps/{id}/page", p.requireAdmin(p.putPage))
	mux.HandleFunc("POST /api/apps/{id}/page/preview", p.requireAdmin(p.previewPage))
	mux.HandleFunc("PATCH /api/apps/{id}/matching", p.requireAdmin(p.updateMatching))
	mux.HandleFunc("PATCH /api/apps/{id}/tagging", p.requireAdmin(p.updateTagging))
	mux.HandleFunc("GET /api/apps/{id}", p.getApp)
	mux.HandleFunc("DELETE /api/apps/{id}", p.requireAdmin(p.deleteApp))
	mux.HandleFunc("GET /api/apps/{id}/links", p.listLinks)
	mux.HandleFunc("POST /api/apps/{id}/links", p.createLink)
	mux.HandleFunc("PATCH /api/links/{id}", p.updateLink)
	mux.HandleFunc("DELETE /api/links/{id}", p.deleteLink)
	mux.HandleFunc("GET /api/apps/{app_id}/links/{link_id}/rules", p.getLinkRules)
	mux.HandleFunc("PUT /api/apps/{app_id}/links/{link_id}/rules", p.setLinkRules)
	mux.HandleFunc("GET /api/apps/{app_id}/links/{link_id}/variants", p.getLinkVariants)
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
	mux.HandleFunc("GET /api/tokens", p.requireTokenAdmin(p.listTokens))
	mux.HandleFunc("POST /api/tokens", p.requireTokenAdmin(p.createToken))
	mux.HandleFunc("PATCH /api/tokens/{id}", p.requireTokenAdmin(p.updateToken))
	mux.HandleFunc("DELETE /api/tokens/{id}", p.requireTokenAdmin(p.deleteToken))
	mux.HandleFunc("GET /api/admin/session", p.sessionStatus)
	mux.HandleFunc("POST /api/admin/session", p.elevate)
	mux.HandleFunc("DELETE /api/admin/session", p.clearSession)
	mux.HandleFunc("GET /", p.static) // SPA shell + assets (catch-all)
	return guard(mux, allowedHosts)
}

// guard: 403 on unallowed Host or cross-origin Origin (DNS-rebinding + CSRF), anti-framing headers. No identity check.
func guard(next http.Handler, allowedHosts []string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !hostAllowed(r.Host, allowedHosts) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if o := r.Header.Get("Origin"); o != "" && !originAllowed(o, r.Host, allowedHosts) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		// never framed: a cross-site iframe would make the SPA's own same-origin fetches pass the Origin check (clickjacking)
		h := w.Header()
		h.Set("X-Frame-Options", "DENY")
		h.Set("Content-Security-Policy", "frame-ancestors 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

// hostAllowed: Host is loopback, listener, or PORTAL_HOSTS. Port ignored: gateway/published port in front still passes.
func hostAllowed(host string, allowedHosts []string) bool {
	if isLoopbackHost(hostnameOf(host)) {
		return true
	}
	return inHosts(hostnameOf(host), allowedHosts)
}

// originAllowed: Origin = request's own host:port, or non-loopback allowed host (gateway rewrote Host, e.g. nginx
// default). Loopback needs exact port: cookies ignore ports, so any localhost page would carry the admin cookie.
// No Origin (curl, same-origin GET) passes.
func originAllowed(origin, reqHost string, allowedHosts []string) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	if strings.EqualFold(u.Host, reqHost) {
		return true
	}
	h := hostnameOf(u.Host)
	if isLoopbackHost(h) {
		return false
	}
	if ip := net.ParseIP(h); ip != nil && ip.IsUnspecified() {
		return false
	}
	return inHosts(h, allowedHosts)
}

func isLoopbackHost(h string) bool { return h == "127.0.0.1" || h == "::1" || h == "localhost" }

func inHosts(h string, hosts []string) bool {
	for _, a := range hosts {
		if hostnameOf(a) == h {
			return true
		}
	}
	return false
}

// hostnameOf: lowercase hostname from "host[:port]" (IPv6 brackets ok; bare IPv6 like "::1" has no port).
func hostnameOf(host string) string {
	h := strings.ToLower(strings.TrimSpace(host))
	if host, _, err := net.SplitHostPort(h); err == nil {
		return strings.Trim(host, "[]")
	}
	return strings.Trim(h, "[]")
}

// appJSON: app wire shape. Only key hash, never key; plaintext only in create response (show-once).
type appJSON struct {
	ID                     string                  `json:"id"`
	Name                   string                  `json:"name"`
	APIKeyHash             string                  `json:"apiKeyHash"`
	IOSAppID               string                  `json:"iosAppId"`
	AndroidPackage         string                  `json:"androidPackage"`
	AndroidCertFingerprint string                  `json:"androidCertFingerprint"`
	IOSScheme              string                  `json:"iosScheme"`
	MatchThreshold         int                     `json:"matchThreshold"`
	MatchWindowMinutes     int                     `json:"matchWindowMinutes"`
	TagLinks               bool                    `json:"tagLinks"`
	RedirectProfiles       []store.RedirectProfile `json:"redirectProfiles"`
	SocialPresets          []store.SocialPreset    `json:"socialPresets"`
}

func toAppJSON(a store.App) appJSON {
	profiles := a.RedirectProfiles
	if profiles == nil {
		profiles = []store.RedirectProfile{}
	}
	social := a.SocialPresets
	if social == nil {
		social = []store.SocialPreset{}
	}
	return appJSON{ID: a.ID, Name: a.Name, APIKeyHash: a.APIKeyHash,
		IOSAppID: a.IOSAppID, AndroidPackage: a.AndroidPackage, AndroidCertFingerprint: a.AndroidCertFingerprint, IOSScheme: a.IOSScheme,
		MatchThreshold: a.MatchThreshold, MatchWindowMinutes: a.MatchWindowMinutes, TagLinks: a.TagLinks,
		RedirectProfiles: profiles, SocialPresets: social}
}

// linkJSON: link wire shape.
type linkJSON struct {
	ID            string   `json:"id"`
	AppID         string   `json:"appId"`
	Key           string   `json:"key"`
	URL           string   `json:"url"`
	Title         string   `json:"title"`
	IOS           string   `json:"ios"`
	Android       string   `json:"android"`
	FallbackURL   string   `json:"fallbackUrl"`
	ExpiresAt     string   `json:"expiresAt"` // RFC3339; empty = never
	ExpiredURL    string   `json:"expiredUrl"`
	OGTitle       string   `json:"ogTitle"`
	OGDescription string   `json:"ogDescription"`
	OGImage       string   `json:"ogImage"`
	Tags          []string `json:"tags"`
	RuleSummary   string   `json:"ruleSummary"`
	HasSplit      bool     `json:"hasSplit"`
}

func toLinkJSON(l store.Link) linkJSON {
	j := linkJSON{ID: l.ID, AppID: l.AppID, Key: l.Key, URL: l.URL, Title: l.Title, IOS: l.IOS,
		Android: l.Android, FallbackURL: l.FallbackURL,
		ExpiredURL: l.ExpiredURL, OGTitle: l.OGTitle, OGDescription: l.OGDescription, OGImage: l.OGImage,
		Tags: l.Tags, RuleSummary: l.RuleSummary, HasSplit: l.HasSplit}
	if j.Tags == nil {
		j.Tags = []string{}
	}
	if l.ExpiresAt != nil {
		j.ExpiresAt = l.ExpiresAt.UTC().Format(time.RFC3339)
	}
	return j
}

// linkBody: link payload (create + update). Omitted update fields unchanged.
type linkBody struct {
	Key           string    `json:"key"`
	URL           string    `json:"url"`
	Title         *string   `json:"title"`
	Note          *string   `json:"note"` // alias for title
	IOS           *string   `json:"ios"`
	Android       *string   `json:"android"`
	FallbackURL   *string   `json:"fallbackUrl"`
	ExpiresAt     *string   `json:"expiresAt"` // RFC3339; "" clears
	ExpiredURL    *string   `json:"expiredUrl"`
	OGTitle       *string   `json:"ogTitle"`
	OGDescription *string   `json:"ogDescription"`
	OGImage       *string   `json:"ogImage"`
	Tags          *[]string `json:"tags"` // omitted = unchanged; [] = clear
}

// Tag limits: short labels the portal lists and filters on; case-insensitive dedupe.
const (
	maxTags   = 10
	maxTagLen = 24
)

// normalizeTags: trim, drop empties, dedupe case-insensitively, keep first spelling.
func normalizeTags(in []string) ([]string, error) {
	out := make([]string, 0, len(in))
	seen := make(map[string]bool, len(in))
	for _, t := range in {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if utf8.RuneCountInString(t) > maxTagLen {
			return nil, fmt.Errorf("tag %q longer than %d characters", t, maxTagLen)
		}
		if strings.ContainsRune(t, ',') {
			return nil, errors.New("tags cannot contain commas")
		}
		k := strings.ToLower(t)
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, t)
	}
	if len(out) > maxTags {
		return nil, fmt.Errorf("at most %d tags per link", maxTags)
	}
	return out, nil
}

// maxTitleLen: link title cap, in characters.
const maxTitleLen = 100

// Social preview limits: title / description in characters; image absolute https (crawlers and chat apps fetch it).
const (
	maxOGTitle = 100
	maxOGDesc  = 300
)

// validOG: preview fields within limits.
func validOG(l store.Link) error {
	if utf8.RuneCountInString(l.OGTitle) > maxOGTitle {
		return fmt.Errorf("ogTitle must be at most %d characters", maxOGTitle)
	}
	if utf8.RuneCountInString(l.OGDescription) > maxOGDesc {
		return fmt.Errorf("ogDescription must be at most %d characters", maxOGDesc)
	}
	if l.OGImage != "" {
		if !httpsURL(l.OGImage) {
			return errors.New("ogImage must be an absolute https URL")
		}
	}
	return nil
}

// httpsURL: absolute https URL with a host.
func httpsURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Host != ""
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

// config: UI settings from env. logoutUrl "" = no Log out link; adminSet = ADMIN_PASSWORD configured.
func (p *portalServer) config(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"logoutUrl": p.logout, "adminSet": p.adminPassword != "", "linkBase": p.linkBase, "version": Version})
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
	// "dk_" + 22 base62 (~131 bits) = 25 chars.
	apiKey := "dk_" + store.Nanoid(22)
	a, err := p.st.CreateApp(body.Name, apiKey)
	if err != nil {
		p.internal(w, err)
		return
	}
	// show-once: plaintext only in this response; later reads return hash
	httpx.WriteJSON(w, http.StatusCreated, map[string]string{
		"id": a.ID, "name": a.Name, "apiKey": apiKey, "apiKeyHash": a.APIKeyHash,
	})
}

// rotateAppKey: new key, swap stored hash; old key dies now. Plaintext in this one response, like create.
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

// revokeAppKey: clear stored hash; SDK stops accepting key.
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
		IOSAppID               *string                  `json:"iosAppId"`               // omitted = keep existing
		AndroidPackage         *string                  `json:"androidPackage"`         // omitted = keep existing
		AndroidCertFingerprint *string                  `json:"androidCertFingerprint"` // omitted = keep existing
		IOSScheme              *string                  `json:"iosScheme"`              // omitted = keep existing
		RedirectProfiles       *[]store.RedirectProfile `json:"redirectProfiles"`       // omitted = keep existing
		SocialPresets          *[]store.SocialPreset    `json:"socialPresets"`          // omitted = keep existing
	}
	if err := decodeJSON(w, r, &body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	id := r.PathValue("id")
	cur, err := p.st.GetApp(id)
	if err != nil {
		p.storeErr(w, err)
		return
	}
	d := store.AppDetails{IOSAppID: cur.IOSAppID, AndroidPackage: cur.AndroidPackage, AndroidCertFingerprint: cur.AndroidCertFingerprint, IOSScheme: cur.IOSScheme}
	// pasted from App Store Connect / Play Console: stray spaces or newlines would land verbatim in AASA / assetlinks and break verification
	if body.IOSAppID != nil {
		d.IOSAppID = strings.TrimSpace(*body.IOSAppID)
	}
	if body.AndroidPackage != nil {
		d.AndroidPackage = strings.TrimSpace(*body.AndroidPackage)
	}
	if body.AndroidCertFingerprint != nil {
		d.AndroidCertFingerprint = strings.TrimSpace(*body.AndroidCertFingerprint)
	}
	if body.IOSScheme != nil {
		d.IOSScheme = strings.TrimSuffix(strings.TrimSpace(*body.IOSScheme), "://")
		if d.IOSScheme != "" && !validScheme(d.IOSScheme) {
			http.Error(w, "iosScheme must be an app URL scheme like myapp (not http, https, javascript, data, file)", http.StatusBadRequest)
			return
		}
	}
	profiles, social := cur.RedirectProfiles, cur.SocialPresets
	if body.RedirectProfiles != nil {
		if profiles, err = cleanProfiles(*body.RedirectProfiles); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	if body.SocialPresets != nil {
		if social, err = cleanSocialPresets(*body.SocialPresets); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	if err := p.st.UpdateApp(id, d, profiles, social); err != nil {
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

// getPage: custom get-app page, default + placeholders for the editor (kept out of appJSON: up to 50 KB).
func (p *portalServer) getPage(w http.ResponseWriter, r *http.Request) {
	a, err := p.st.GetApp(r.PathValue("id"))
	if err != nil {
		p.storeErr(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"html": a.PageHTML, "defaultHtml": page.DefaultHTML, "placeholders": page.Placeholders})
}

// putPage: save custom page; "" resets to default. Admin: the HTML is served on the link domain and can break the install flow.
func (p *portalServer) putPage(w http.ResponseWriter, r *http.Request) {
	var body struct {
		HTML string `json:"html"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := page.Validate(body.HTML); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := p.st.UpdateAppPage(r.PathValue("id"), body.HTML); err != nil {
		p.storeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// previewPage: real renderer on sample links, no scripts (the portal frame is sandboxed). Admin: same as save.
func (p *portalServer) previewPage(w http.ResponseWriter, r *http.Request) {
	var body struct {
		HTML     string `json:"html"`
		Page     string `json:"page"`     // copy | tap
		Platform string `json:"platform"` // ios | android
	}
	if err := decodeJSON(w, r, &body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	a, err := p.st.GetApp(r.PathValue("id"))
	if err != nil {
		p.storeErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(page.Preview(body.HTML, body.Page, body.Platform, a.Name)))
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

// validTarget: absolute URL with scheme; http(s) needs host. Empty ok (optional fields). Scripting schemes rejected.
// maxTargetLen: per link target; values land in every redirect Location and click row.
const maxTargetLen = 2048

func validTarget(field, v string) error {
	if v == "" {
		return nil
	}
	if len(v) > maxTargetLen {
		return fmt.Errorf("%s longer than %d bytes", field, maxTargetLen)
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

// validDestination: app route only ("/product/42"). Full URLs lose their first segment in the SDK, app schemes get dropped (R10).
func validDestination(field, v string) error {
	if len(v) > maxTargetLen {
		return fmt.Errorf("%s longer than %d bytes", field, maxTargetLen)
	}
	if !httpx.IsRoute(v) {
		return fmt.Errorf("%s must be an app route starting with /, e.g. /product/42", field)
	}
	return nil
}

// validTargets: first bad destination field wins.
func validTargets(l store.Link) error {
	for _, f := range []struct{ name, v string }{
		{"ios", l.IOS}, {"android", l.Android}, {"fallbackUrl", l.FallbackURL}, {"expiredUrl", l.ExpiredURL},
	} {
		if err := validTarget(f.name, f.v); err != nil {
			return err
		}
	}
	return validDestination("destination", l.URL)
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
	// key lives in URL path: path-safe chars only, never shadows a route or breaks pipeline
	if !linkKeyRe.MatchString(body.Key) || isReservedKey(body.Key) {
		http.Error(w, "key must be 1-64 chars of [A-Za-z0-9_-], not starting with _", http.StatusBadRequest)
		return
	}
	expiresAt, err := parseExpiry(body.ExpiresAt)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	title := ""
	if body.Title != nil {
		title = strings.TrimSpace(*body.Title)
	} else if body.Note != nil {
		title = strings.TrimSpace(*body.Note)
	}
	if utf8.RuneCountInString(title) > maxTitleLen {
		http.Error(w, "title must be at most 100 characters", http.StatusBadRequest)
		return
	}
	nl := store.Link{
		AppID: appID, Key: body.Key, URL: body.URL, Title: title,
		IOS: strings.TrimSpace(stringValue(body.IOS)), Android: strings.TrimSpace(stringValue(body.Android)),
		FallbackURL: strings.TrimSpace(stringValue(body.FallbackURL)),
		ExpiresAt:   expiresAt, ExpiredURL: strings.TrimSpace(stringValue(body.ExpiredURL)),
		OGTitle: strings.TrimSpace(stringValue(body.OGTitle)), OGDescription: strings.TrimSpace(stringValue(body.OGDescription)),
		OGImage: strings.TrimSpace(stringValue(body.OGImage)),
	}
	if body.Tags != nil {
		tags, err := normalizeTags(*body.Tags)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		nl.Tags = tags
	}
	if err := validTargets(nl); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := validOG(nl); err != nil {
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
	if body.Title != nil {
		l.Title = strings.TrimSpace(*body.Title)
	} else if body.Note != nil {
		l.Title = strings.TrimSpace(*body.Note)
	}
	if utf8.RuneCountInString(l.Title) > maxTitleLen {
		http.Error(w, "title must be at most 100 characters", http.StatusBadRequest)
		return
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
	if body.OGTitle != nil {
		l.OGTitle = strings.TrimSpace(*body.OGTitle)
	}
	if body.OGDescription != nil {
		l.OGDescription = strings.TrimSpace(*body.OGDescription)
	}
	if body.OGImage != nil {
		l.OGImage = strings.TrimSpace(*body.OGImage)
	}
	if body.Tags != nil {
		tags, err := normalizeTags(*body.Tags)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		l.Tags = tags
	}
	if err := validTargets(l); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := validOG(l); err != nil {
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

// updateMatching: app match threshold + window (Detour: app-level). Both required.
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

// updateTagging: PATCH {tagLinks}: SDK destinations get detur_link=<key> so app can tag its device (per-link conversions + retention).
func (p *portalServer) updateTagging(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		TagLinks *bool `json:"tagLinks"`
	}
	if err := decodeJSON(w, r, &body); err != nil || body.TagLinks == nil {
		http.Error(w, "tagLinks is required", http.StatusBadRequest)
		return
	}
	if err := p.st.UpdateAppTagLinks(id, *body.TagLinks); err != nil {
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
var analyticsRanges = map[string]int{
	"7":   7,
	"14":  14,
	"30":  30,
	"60":  60,
	"90":  90,
	"180": 180,
	"365": 365,
}

// analytics: daily click/install/event stats. ?days= or ?from=YYYY-MM-DD&to=YYYY-MM-DD (default 7 days); ?platform=ios|android|desktop (default all); ?window=15m|1h|3h|12h; ?link_id=...
func (p *portalServer) analytics(w http.ResponseWriter, r *http.Request) {
	appID := r.PathValue("id")
	if _, err := p.st.GetApp(appID); err != nil {
		p.storeErr(w, err)
		return
	}
	platform := r.URL.Query().Get("platform")
	if platform != "" && platform != "ios" && platform != "android" && platform != "desktop" {
		http.Error(w, "platform must be ios, android or desktop", http.StatusBadRequest)
		return
	}
	linkParam := r.URL.Query().Get("link_id")
	if linkParam == "" {
		linkParam = r.URL.Query().Get("link")
	}
	var linkID string
	if linkParam != "" {
		if l, err := p.st.GetLink(linkParam); err == nil && l.AppID == appID {
			linkID = l.ID
		} else if l, err := p.st.GetLinkByKey(appID, linkParam); err == nil {
			linkID = l.ID
		} else {
			http.Error(w, "link not found", http.StatusNotFound)
			return
		}
	}

	window := r.URL.Query().Get("window")
	if window == "" {
		window = r.URL.Query().Get("period")
	}
	if window != "" {
		if window != "15m" && window != "1h" && window != "3h" && window != "12h" {
			http.Error(w, "window must be 15m, 1h, 3h or 12h", http.StatusBadRequest)
			return
		}
		a, err := p.st.AnalyticsSubDay(appID, window, platform, linkID, time.Now())
		if err != nil {
			p.internal(w, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, a)
		return
	}

	from, to, ok := parseDateRange(w, r, 7)
	if !ok {
		return
	}
	a, err := p.st.AnalyticsRange(appID, from, to, platform, linkID)
	if err != nil {
		p.internal(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, a)
}

// matchQuality: GET /api/apps/{id}/match-quality?days=... or ?from=...&to=... (default 30).
func (p *portalServer) matchQuality(w http.ResponseWriter, r *http.Request) {
	appID := r.PathValue("id")
	if _, err := p.st.GetApp(appID); err != nil {
		p.storeErr(w, err)
		return
	}
	from, to, ok := parseDateRange(w, r, 30)
	if !ok {
		return
	}
	days := int(to.Sub(from).Hours()/24) + 1
	q, err := p.st.MatchQuality(appID, days, to)
	if err != nil {
		p.internal(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, q)
}

// fraudInstallJSON: flagged install row; link ids -> keys ("" = link deleted). No device hash.
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

// fraud: GET /api/apps/{id}/fraud?days=... or ?from=...&to=... (default 7): per-signal counts + latest flagged installs.
func (p *portalServer) fraud(w http.ResponseWriter, r *http.Request) {
	appID := r.PathValue("id")
	if _, err := p.st.GetApp(appID); err != nil {
		p.storeErr(w, err)
		return
	}
	from, to, ok := parseDateRange(w, r, 7)
	if !ok {
		return
	}
	f, err := p.st.FraudRange(appID, from, to)
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

// fraudSettingsJSON: fraud settings wire shape. Pointers: PATCH requires every field.
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

// parseDateRange: ?from=YYYY-MM-DD&to=YYYY-MM-DD if either present, else ?days= via parseDays. Returns from, to, ok.
func parseDateRange(w http.ResponseWriter, r *http.Request, defDays int) (time.Time, time.Time, bool) {
	q := r.URL.Query()
	if q.Get("window") != "" {
		http.Error(w, "window not supported here", http.StatusBadRequest)
		return time.Time{}, time.Time{}, false
	}
	fromStr := q.Get("from")
	toStr := q.Get("to")
	if fromStr != "" || toStr != "" {
		if fromStr == "" || toStr == "" {
			http.Error(w, "both from and to are required for custom range", http.StatusBadRequest)
			return time.Time{}, time.Time{}, false
		}
		from, err := time.Parse("2006-01-02", fromStr)
		if err != nil {
			http.Error(w, "from must be in YYYY-MM-DD format", http.StatusBadRequest)
			return time.Time{}, time.Time{}, false
		}
		to, err := time.Parse("2006-01-02", toStr)
		if err != nil {
			http.Error(w, "to must be in YYYY-MM-DD format", http.StatusBadRequest)
			return time.Time{}, time.Time{}, false
		}
		if from.After(to) {
			http.Error(w, "from must be before or equal to to", http.StatusBadRequest)
			return time.Time{}, time.Time{}, false
		}
		if to.Sub(from) > 730*24*time.Hour {
			http.Error(w, "date range cannot exceed 730 days", http.StatusBadRequest)
			return time.Time{}, time.Time{}, false
		}
		return from.UTC(), to.UTC(), true
	}
	days, ok := parseDays(w, r, defDays)
	if !ok {
		return time.Time{}, time.Time{}, false
	}
	now := time.Now().UTC()
	to := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	from := to.AddDate(0, 0, -(days - 1))
	return from, to, true
}

// parseDays: ?days= in analyticsRanges (def if absent); else writes 400, ok false.
func parseDays(w http.ResponseWriter, r *http.Request, def int) (int, bool) {
	v := r.URL.Query().Get("days")
	if v == "" {
		return def, true
	}
	days := analyticsRanges[v]
	if days == 0 {
		http.Error(w, "days must be 7, 14, 30, 60, 90, 180 or 365", http.StatusBadRequest)
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

// static: built portal from dir. Missing dir/file = plain 404 (FileServer); server keeps running.
func (p *portalServer) static(w http.ResponseWriter, r *http.Request) {
	http.FileServer(http.Dir(p.dir)).ServeHTTP(w, r)
}

// storeErr: store error -> HTTP response.
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

// schemeRe: RFC 3986 scheme. Ends up in the tap page href, so browser-handled schemes are refused (validScheme).
var schemeRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]{0,63}$`)

// validScheme: app URL scheme only; web and script schemes would turn the tap button into a web page or script.
func validScheme(v string) bool {
	switch strings.ToLower(v) {
	case "http", "https", "javascript", "data", "file", "vbscript", "blob", "about", "ftp", "ws", "wss", "intent":
		return false
	}
	return schemeRe.MatchString(v)
}

// isReservedKey: keys shadowing registered routes (case-insensitive) or "_" prefix Dub reserves (_root).
func isReservedKey(key string) bool {
	key = strings.ToLower(key)
	switch key {
	case "health", "api", "favicon.ico":
		return true
	}
	return strings.HasPrefix(key, ".well-known") || strings.HasPrefix(key, "_")
}
