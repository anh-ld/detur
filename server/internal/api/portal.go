package api

// Portal: apps/links CRUD, matching settings, click/install readout, and
// static hosting of the kinu-built portal UI. No identity checks; every
// route is wrapped in the origin/host guard instead (DNS-rebinding + CSRF
// protection; the portal listener is loopback by default).

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"

	"detur.dev/server/internal/httpx"
	"detur.dev/server/internal/match"
	"detur.dev/server/internal/store"
)

// Portal matching defaults come from the store's exported defaults; the
// settings table overrides them at runtime.
type portalServer struct {
	st  *store.Store
	log *log.Logger
	dir string // portal static dir (built UI)
}

// RegisterPortal builds the portal handler: apps/links CRUD, settings, and
// readout routes, plus the static UI served from staticDir (missing files or
// a missing dir 404 plain text, never a crash). The whole mux is wrapped in
// the origin/host guard. allowedHosts holds the portal listener's own
// listen address;
// loopback is always accepted. A Host or Origin outside those is rejected
// with 403. No auth: access control is delegated to a zero-trust boundary
// in front of this listener.
func RegisterPortal(st *store.Store, staticDir string, allowedHosts []string) http.Handler {
	p := &portalServer{st: st, log: log.Default(), dir: staticDir}
	if fi, err := os.Stat(staticDir); err != nil || !fi.IsDir() {
		p.log.Printf("portal static dir %q missing: portal API only, UI will 404", staticDir)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/apps", p.listApps)
	mux.HandleFunc("POST /api/apps", p.createApp)
	mux.HandleFunc("PATCH /api/apps/{id}", p.updateApp)
	mux.HandleFunc("DELETE /api/apps/{id}", p.deleteApp)
	mux.HandleFunc("GET /api/apps/{id}/links", p.listLinks)
	mux.HandleFunc("POST /api/apps/{id}/links", p.createLink)
	mux.HandleFunc("PATCH /api/links/{id}", p.updateLink)
	mux.HandleFunc("DELETE /api/links/{id}", p.deleteLink)
	mux.HandleFunc("GET /api/settings", p.getSettings)
	mux.HandleFunc("PATCH /api/settings", p.updateSettings)
	mux.HandleFunc("GET /api/apps/{id}/readout", p.readout)
	mux.HandleFunc("GET /", p.static) // SPA shell + assets (catch-all)
	return guard(mux, allowedHosts)
}

// guard wraps the portal mux with the origin/host check: requests whose
// Host is not loopback or an allowed host, and requests carrying an Origin
// header that names a different host, get 403. This is the DNS-rebinding +
// CSRF guard; there are no identity checks.
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

// hostAllowed reports whether the request Host is loopback or one of the
// allowed hosts (the portal listener's own address + configured tunnel
// hosts). Hostnames are compared without the port so a published/zero-trust
// port in front of the portal still passes.
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

// originAllowed reports whether an Origin header (scheme://host[:port])
// names loopback or an allowed host. Missing Origins (curl, same-origin GETs
// without CORS preflight) pass the guard untouched.
func originAllowed(origin string, allowedHosts []string) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	return hostAllowed(u.Host, allowedHosts)
}

// hostnameOf extracts the lowercase hostname from "host[:port]" (bracket-
// tolerant for IPv6; a bare IPv6 like "::1" has no port to strip).
func hostnameOf(host string) string {
	h := strings.ToLower(strings.TrimSpace(host))
	if host, _, err := net.SplitHostPort(h); err == nil {
		return strings.Trim(host, "[]")
	}
	return strings.Trim(h, "[]")
}

// appJSON is the wire shape for apps. API keys are never included, only the
// hash; the plaintext key appears in exactly one response: the create call
// (show-once semantics).
type appJSON struct {
	ID                     string `json:"id"`
	Name                   string `json:"name"`
	APIKeyHash             string `json:"apiKeyHash"`
	IOSAppID               string `json:"iosAppId"`
	AndroidPackage         string `json:"androidPackage"`
	AndroidCertFingerprint string `json:"androidCertFingerprint"`
}

func toAppJSON(a store.App) appJSON {
	return appJSON{ID: a.ID, Name: a.Name, APIKeyHash: a.APIKeyHash,
		IOSAppID: a.IOSAppID, AndroidPackage: a.AndroidPackage, AndroidCertFingerprint: a.AndroidCertFingerprint}
}

// linkJSON is the wire shape for links.
type linkJSON struct {
	ID            string `json:"id"`
	AppID         string `json:"appId"`
	Key           string `json:"key"`
	URL           string `json:"url"`
	IOS           string `json:"ios"`
	Android       string `json:"android"`
	FallbackURL   string `json:"fallbackUrl"`
	Threshold     int    `json:"threshold"`
	WindowMinutes int    `json:"windowMinutes"`
}

func toLinkJSON(l store.Link) linkJSON {
	return linkJSON{ID: l.ID, AppID: l.AppID, Key: l.Key, URL: l.URL, IOS: l.IOS,
		Android: l.Android, FallbackURL: l.FallbackURL, Threshold: l.Threshold, WindowMinutes: l.WindowMinutes}
}

// linkBody is the portal link payload (create and update share it). Omitted
// update fields stay unchanged; zero numeric values restore global matching.
type linkBody struct {
	Key           string  `json:"key"`
	URL           string  `json:"url"`
	IOS           *string `json:"ios"`
	Android       *string `json:"android"`
	FallbackURL   *string `json:"fallbackUrl"`
	Threshold     *int    `json:"threshold"`
	WindowMinutes *int    `json:"windowMinutes"`
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

func (p *portalServer) createApp(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name   string `json:"name"`
		APIKey string `json:"apiKey"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	body.APIKey = strings.TrimSpace(body.APIKey)
	if body.Name == "" || body.APIKey == "" {
		http.Error(w, "name and apiKey are required", http.StatusBadRequest)
		return
	}
	// Entropy floor: keys below this are brute-forceable even hashed at
	// rest; the UI generates 128-bit keys by default.
	if len(body.APIKey) < 24 {
		http.Error(w, "apiKey must be at least 24 characters", http.StatusBadRequest)
		return
	}
	a, err := p.st.CreateApp(body.Name, body.APIKey)
	if err != nil {
		p.internal(w, err)
		return
	}
	// Show-once semantics: the plaintext key rides this one response; every
	// later read returns only the hash.
	httpx.WriteJSON(w, http.StatusCreated, map[string]string{
		"id": a.ID, "name": a.Name, "apiKey": body.APIKey, "apiKeyHash": a.APIKeyHash,
	})
}

func (p *portalServer) updateApp(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IOSAppID               string `json:"iosAppId"`
		AndroidPackage         string `json:"androidPackage"`
		AndroidCertFingerprint string `json:"androidCertFingerprint"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
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

func (p *portalServer) createLink(w http.ResponseWriter, r *http.Request) {
	appID := r.PathValue("id")
	var body linkBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	body.Key = strings.TrimSpace(body.Key)
	body.URL = strings.TrimSpace(body.URL)
	if body.Key == "" || body.URL == "" {
		http.Error(w, "key and url are required", http.StatusBadRequest)
		return
	}
	// Short keys live in the URL path: restrict to path-safe characters so
	// a key can never shadow a registered route or break the pipeline.
	if !linkKeyRe.MatchString(body.Key) || isReservedKey(body.Key) {
		http.Error(w, "key must be 1-64 chars of [A-Za-z0-9_-]", http.StatusBadRequest)
		return
	}
	threshold, window := 0, 0
	if body.Threshold != nil {
		threshold = *body.Threshold
	}
	if body.WindowMinutes != nil {
		window = *body.WindowMinutes
	}
	if err := validateMatch(threshold, window); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	l, err := p.st.CreateLink(store.Link{
		AppID: appID, Key: body.Key, URL: body.URL,
		IOS: stringValue(body.IOS), Android: stringValue(body.Android), FallbackURL: stringValue(body.FallbackURL),
		Threshold: threshold, WindowMinutes: window,
	})
	if err != nil {
		if errors.Is(err, store.ErrKeyConflict) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		// The links.app_id FK rejects unknown apps; an absent app reads as
		// not-found so the portal can tell the difference (no pre-check: the
		// FK is the single source of truth).
		if strings.Contains(err.Error(), "FOREIGN KEY") {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		p.internal(w, err)
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
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	l := cur
	if body.URL = strings.TrimSpace(body.URL); body.URL != "" {
		l.URL = body.URL
	}
	if body.IOS != nil {
		l.IOS = *body.IOS
	}
	if body.Android != nil {
		l.Android = *body.Android
	}
	if body.FallbackURL != nil {
		l.FallbackURL = *body.FallbackURL
	}
	if body.Threshold != nil {
		l.Threshold = *body.Threshold
	}
	if body.WindowMinutes != nil {
		l.WindowMinutes = *body.WindowMinutes
	}
	if err := validateMatch(l.Threshold, l.WindowMinutes); err != nil {
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

// getSettings reads the matching defaults from the settings table, falling
// back to the store defaults.
func (p *portalServer) getSettings(w http.ResponseWriter, r *http.Request) {
	threshold, err := p.st.IntSetting(settingThreshold, store.DefaultThreshold)
	if err != nil {
		p.internal(w, err)
		return
	}
	window, err := p.st.IntSetting(settingWindow, store.DefaultWindowMinutes)
	if err != nil {
		p.internal(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]int{"threshold": threshold, "windowMinutes": window})
}

// updateSettings persists the matching defaults; absent/zero fields keep the
// stored value. The response reflects the effective (stored) settings.
func (p *portalServer) updateSettings(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Threshold     int `json:"threshold"`
		WindowMinutes int `json:"windowMinutes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := validateMatch(body.Threshold, body.WindowMinutes); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if body.Threshold != 0 || body.WindowMinutes != 0 {
		var threshold, window *string
		if body.Threshold != 0 {
			v := strconv.Itoa(body.Threshold)
			threshold = &v
		}
		if body.WindowMinutes != 0 {
			v := strconv.Itoa(body.WindowMinutes)
			window = &v
		}
		if err := p.st.SetSettings(threshold, window); err != nil {
			p.internal(w, err)
			return
		}
	}
	p.getSettings(w, r)
}

// readout serves the app-level click/install counts: clicks, organic and
// non-organic installs. Unknown-attribution rows (backend errors) are
// excluded by the store's counts.
func (p *portalServer) readout(w http.ResponseWriter, r *http.Request) {
	appID := r.PathValue("id")
	if _, err := p.st.GetApp(appID); err != nil {
		p.storeErr(w, err)
		return
	}
	clicks, err := p.st.CountClicks(appID)
	if err != nil {
		p.internal(w, err)
		return
	}
	organic, nonOrganic, err := p.st.CountInstalls(appID)
	if err != nil {
		p.internal(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]int64{
		"clicks": clicks, "organic": organic, "nonOrganic": nonOrganic,
	})
}

// static serves the built portal from the configured directory. A missing
// dir or file 404s as plain text (FileServer); the server keeps running.
func (p *portalServer) static(w http.ResponseWriter, r *http.Request) {
	http.FileServer(http.Dir(p.dir)).ServeHTTP(w, r)
}

// validateMatch rejects out-of-range matching settings; zero values mean
// "use the default/stored value" and pass.
func validateMatch(threshold, windowMinutes int) error {
	if threshold != 0 && (threshold < match.MinThreshold || threshold > match.MaxThreshold) {
		return fmt.Errorf("threshold out of range %d..%d", match.MinThreshold, match.MaxThreshold)
	}
	if windowMinutes != 0 && (windowMinutes < match.MinWindow || windowMinutes > match.MaxWindow) {
		return fmt.Errorf("windowMinutes out of range %d..%d", match.MinWindow, match.MaxWindow)
	}
	return nil
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

// linkKeyRe is the short-link key charset (URL-path-safe, single segment).
var linkKeyRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// isReservedKey rejects keys that shadow registered routes.
func isReservedKey(key string) bool {
	switch key {
	case "health", "api", "favicon.ico":
		return true
	}
	return strings.HasPrefix(key, ".well-known")
}
