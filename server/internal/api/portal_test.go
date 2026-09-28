package api

// U6 portal tests (docs/plans/2026-09-28-1507-feat-detour-selfhost-server-
// plan.md): portal API CRUD, settings driving the matching engine, readout
// (R15), the no-auth contract (R19), the origin/host guard (KTD5), static
// serving, and show-once API-key semantics (R14). Real store + real mux via
// httptest; the SDK endpoints run on a second listener sharing the store
// (production topology: separate listeners, one store).

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"detur.dev/server/internal/httpx"
	"detur.dev/server/internal/store"
)

// portalAddr mirrors cfg.PortalAddr's default (config.Load).
const portalAddr = "127.0.0.1:8081"

// newPortalEnv spins up the portal listener (RegisterPortal with a temp
// static dir containing an index.html) and a second SDK listener sharing the
// same store.
func newPortalEnv(t *testing.T) (portal, sdk *httptest.Server, st *store.Store) {
	httpx.TrustProxy = true // api tests simulate the trusted-proxy deployment via X-Forwarded-For
	t.Cleanup(func() { httpx.TrustProxy = false })
	t.Helper()
	path := filepath.Join(t.TempDir(), "detur-portal.db")
	st, err := store.Open(path)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	staticDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(staticDir, "index.html"),
		[]byte("<!doctype html><title>detur portal</title>"), 0o644); err != nil {
		t.Fatalf("write index.html: %v", err)
	}
	portal = httptest.NewServer(RegisterPortal(st, staticDir, []string{portalAddr}))
	t.Cleanup(portal.Close)

	sdkMux := http.NewServeMux()
	RegisterSDK(sdkMux, st, 24)
	sdk = httptest.NewServer(sdkMux)
	t.Cleanup(sdk.Close)
	return portal, sdk, st
}

// portalReq issues a request to a test server with optional headers; the
// Host header defaults to the server's own (loopback) address.
func portalReq(t *testing.T, ts *httptest.Server, method, path, body string, hdr map[string]string) (*http.Response, []byte) {
	t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, ts.URL+path, rd)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp, b
}

func mustJSON(t *testing.T, b []byte, v any) {
	t.Helper()
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatalf("bad JSON %q: %v", b, err)
	}
}

// Scenario 1: full app + link CRUD round trip through the portal API.
func TestPortalAppLinkCRUD(t *testing.T) {
	portal, _, _ := newPortalEnv(t)
	key := "portal-crud-key-1234567890abcdef"

	resp, b := portalReq(t, portal, "POST", "/api/apps", `{"name":"crud app","apiKey":"`+key+`"}`, nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create app: %d %s", resp.StatusCode, b)
	}
	var created struct {
		ID         string `json:"id"`
		APIKey     string `json:"apiKey"`
		APIKeyHash string `json:"apiKeyHash"`
	}
	mustJSON(t, b, &created)
	if created.APIKey != key || created.APIKeyHash != store.HashKey(key) {
		t.Fatalf("create response must carry plaintext key + hash: %s", b)
	}

	resp, b = portalReq(t, portal, "PATCH", "/api/apps/"+created.ID,
		`{"iosAppId":"ABCDE12345.com.example.app","androidPackage":"com.example.app","androidCertFingerprint":"AA:BB:CC"}`, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("update app: %d %s", resp.StatusCode, b)
	}
	if !strings.Contains(string(b), `"iosAppId":"ABCDE12345.com.example.app"`) {
		t.Fatalf("app details not persisted: %s", b)
	}

	resp, b = portalReq(t, portal, "POST", "/api/apps/"+created.ID+"/links",
		`{"key":"c1","url":"https://example.com/p","ios":"https://apps.apple.com/app/id1",`+
			`"android":"https://play.google.com/store/apps/details?id=com.example","threshold":1000,"windowMinutes":30}`, nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create link: %d %s", resp.StatusCode, b)
	}
	var link struct {
		ID            string `json:"id"`
		Key           string `json:"key"`
		Threshold     int    `json:"threshold"`
		WindowMinutes int    `json:"windowMinutes"`
	}
	mustJSON(t, b, &link)
	if link.Threshold != 1000 || link.WindowMinutes != 30 {
		t.Fatalf("link settings not persisted: %+v", link)
	}

	resp, b = portalReq(t, portal, "GET", "/api/apps/"+created.ID+"/links", "", nil)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(b), `"key":"c1"`) {
		t.Fatalf("list links: %d %s", resp.StatusCode, b)
	}

	// Update link: zero threshold/window means "keep stored values".
	resp, b = portalReq(t, portal, "PATCH", "/api/links/"+link.ID,
		`{"url":"https://example.com/p2","ios":"","android":"","fallbackUrl":"https://example.com/fb","threshold":0,"windowMinutes":0}`, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("update link: %d %s", resp.StatusCode, b)
	}
	var updated struct {
		URL         string `json:"url"`
		FallbackURL string `json:"fallbackUrl"`
		Threshold   int    `json:"threshold"`
	}
	mustJSON(t, b, &updated)
	if updated.URL != "https://example.com/p2" || updated.FallbackURL != "https://example.com/fb" || updated.Threshold != 1000 {
		t.Fatalf("link update not applied as expected: %+v", updated)
	}

	resp, _ = portalReq(t, portal, "DELETE", "/api/links/"+link.ID, "", nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete link: %d", resp.StatusCode)
	}
	resp, b = portalReq(t, portal, "GET", "/api/apps/"+created.ID+"/links", "", nil)
	if resp.StatusCode != http.StatusOK || strings.Contains(string(b), "c1") {
		t.Fatalf("link should be gone: %d %s", resp.StatusCode, b)
	}

	resp, _ = portalReq(t, portal, "DELETE", "/api/apps/"+created.ID, "", nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete app: %d", resp.StatusCode)
	}
	resp, b = portalReq(t, portal, "GET", "/api/apps", "", nil)
	var apps []map[string]json.RawMessage
	mustJSON(t, b, &apps)
	if resp.StatusCode != http.StatusOK || len(apps) != 0 {
		t.Fatalf("app should be gone: %d %s", resp.StatusCode, b)
	}
}

// Scenario 2: matching settings persisted via PATCH /api/settings drive the
// matching engine (U2) — the same fingerprint matches or 404s purely per the
// configured threshold (950-score candidate: matches at 850, not at 1200).
func TestPortalSettingsDriveMatching(t *testing.T) {
	portal, sdk, st := newPortalEnv(t)
	app, link := setup(t, st)
	recordAndroidClick(t, st, app, link)

	resp, b := portalReq(t, portal, "PATCH", "/api/settings", `{"threshold":1200,"windowMinutes":15}`, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("set settings: %d %s", resp.StatusCode, b)
	}

	// Score 950 (IP differs: 127.0.0.1 vs click IP): below the raised threshold.
	resp, b = portalReq(t, sdk, "POST", "/api/link/match-link", androidFingerprintJSON(), authHeaders(app.ID))
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("at threshold 1200 the 950-score match must 404, got %d %s", resp.StatusCode, b)
	}
	// Same fingerprint, IP matching via X-Forwarded-For: score 1450 >= 1200.
	hdr := authHeaders(app.ID)
	hdr["X-Forwarded-For"] = testIP
	resp, b = portalReq(t, sdk, "POST", "/api/link/match-link", androidFingerprintJSON(), hdr)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("at threshold 1200 the 1450-score match must match, got %d %s", resp.StatusCode, b)
	}

	// Lower the threshold back to default: the 950-score candidate now matches.
	resp, b = portalReq(t, portal, "PATCH", "/api/settings", `{"threshold":850,"windowMinutes":15}`, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("reset settings: %d %s", resp.StatusCode, b)
	}
	resp, b = portalReq(t, sdk, "POST", "/api/link/match-link", androidFingerprintJSON(), authHeaders(app.ID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("at threshold 850 the 950-score match must match, got %d %s", resp.StatusCode, b)
	}

	resp, b = portalReq(t, portal, "GET", "/api/settings", "", nil)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(b), `"threshold":850`) ||
		!strings.Contains(string(b), `"windowMinutes":15`) {
		t.Fatalf("get settings: %d %s", resp.StatusCode, b)
	}
}

// Scenario 3: readout numbers (R15). Recorded click + matched launch →
// non-organic; fresh no-match launch → organic (covers AE1, AE2 through the
// readout).
func TestPortalReadout(t *testing.T) {
	portal, sdk, st := newPortalEnv(t)
	app, link := setup(t, st)
	recordAndroidClick(t, st, app, link)

	hdr := authHeaders(app.ID)
	hdr["X-Forwarded-For"] = testIP
	resp, b := portalReq(t, sdk, "POST", "/api/link/match-link", androidFingerprintJSON(), hdr)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("matched launch must return the link: %d %s", resp.StatusCode, b)
	}
	resp, b = portalReq(t, sdk, "POST", "/api/link/match-link", mismatchedFingerprintJSON(), authHeaders(app.ID))
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("fresh no-match launch must 404: %d %s", resp.StatusCode, b)
	}

	resp, b = portalReq(t, portal, "GET", "/api/apps/"+app.ID+"/readout", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("readout: %d %s", resp.StatusCode, b)
	}
	var ro struct {
		Clicks     int64 `json:"clicks"`
		Organic    int64 `json:"organic"`
		NonOrganic int64 `json:"nonOrganic"`
	}
	mustJSON(t, b, &ro)
	if ro.Clicks != 1 || ro.Organic != 1 || ro.NonOrganic != 1 {
		t.Fatalf("readout mismatch: %+v", ro)
	}
}

// Scenario 4: portal serves without auth — no identity checks anywhere (R19).
func TestPortalNoAuth(t *testing.T) {
	portal, _, _ := newPortalEnv(t)
	// No Authorization / X-App-ID / X-SDK headers on any request.
	resp, b := portalReq(t, portal, "POST", "/api/apps", `{"name":"no-auth","apiKey":"k-no-auth-0123456789abcdef-XYZ-000"}`, nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("portal must serve without identity checks: %d %s", resp.StatusCode, b)
	}
}

// Scenario 5: cross-origin requests rejected by the guard (KTD5 CSRF).
func TestPortalRejectsCrossOrigin(t *testing.T) {
	portal, _, _ := newPortalEnv(t)
	resp, b := portalReq(t, portal, "POST", "/api/apps", `{"name":"x","apiKey":"k-0123456789abcdef-XYZ-000"}`,
		map[string]string{"Origin": "http://evil.example"})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin POST must be 403, got %d %s", resp.StatusCode, b)
	}
	// Same-origin (loopback) Origin passes.
	resp, b = portalReq(t, portal, "POST", "/api/apps", `{"name":"ok","apiKey":"k-0123456789abcdef-XYZ-000"}`,
		map[string]string{"Origin": "http://localhost:8081"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("loopback-origin POST must pass, got %d %s", resp.StatusCode, b)
	}
}

// Scenario 6: unknown Host header rejected by the guard (DNS rebinding).
func TestPortalRejectsForeignHost(t *testing.T) {
	portal, _, _ := newPortalEnv(t)
	req, err := http.NewRequest("GET", portal.URL+"/api/apps", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Host = "evil.example"
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("foreign Host must be 403, got %d", resp.StatusCode)
	}
}

// Scenario 7: static serving — GET / returns the built index.html; a missing
// file 404s as plain text; a missing static dir never crashes the API.
func TestPortalStaticServing(t *testing.T) {
	portal, _, _ := newPortalEnv(t)
	resp, b := portalReq(t, portal, "GET", "/", "", nil)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(b), "detur portal") {
		t.Fatalf("GET / must serve index.html: %d %s", resp.StatusCode, b)
	}
	resp, b = portalReq(t, portal, "GET", "/does-not-exist.js", "", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("missing asset must 404: %d %s", resp.StatusCode, b)
	}
}

func TestPortalStaticMissingDirKeepsAPIAlive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "detur-portal.db")
	st, err := store.Open(path)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	ts := httptest.NewServer(RegisterPortal(st, filepath.Join(t.TempDir(), "no-such-dir"), []string{portalAddr}))
	t.Cleanup(ts.Close)

	resp, b := portalReq(t, ts, "POST", "/api/apps", `{"name":"still-works","apiKey":"k-0123456789abcdef-XYZ-000"}`, nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("API must keep working without the static dir: %d %s", resp.StatusCode, b)
	}
	resp, b = portalReq(t, ts, "GET", "/", "", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("GET / without a static dir must 404, got %d %s", resp.StatusCode, b)
	}
}

// Scenario 8: show-once key semantics — the create response carries the
// plaintext key; every later GET carries only the hash.
func TestPortalKeyShownOnce(t *testing.T) {
	portal, _, _ := newPortalEnv(t)
	key := "super-secret-visible-once"
	resp, b := portalReq(t, portal, "POST", "/api/apps", `{"name":"once","apiKey":"`+key+`"}`, nil)
	if resp.StatusCode != http.StatusCreated || !strings.Contains(string(b), key) {
		t.Fatalf("create response must contain the plaintext key: %d %s", resp.StatusCode, b)
	}
	resp, b = portalReq(t, portal, "GET", "/api/apps", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list apps: %d %s", resp.StatusCode, b)
	}
	if strings.Contains(string(b), key) {
		t.Fatalf("plaintext key leaked in list: %s", b)
	}
	if !strings.Contains(string(b), store.HashKey(key)) {
		t.Fatalf("list must carry the hash: %s", b)
	}
}
