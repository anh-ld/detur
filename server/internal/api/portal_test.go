package api

// Portal tests: portal API CRUD, app matching settings driving matching engine, analytics,
// no-auth contract, origin/host guard, static serving, show-once API-key semantics.
// Real store + real mux via httptest; SDK endpoints on second listener sharing store (separate listeners, one store).

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

// portalAddr mirrors cfg.PortalAddr default (config.Load).
const portalAddr = "127.0.0.1:8081"

// newPortalEnv: portal listener (RegisterPortal with temp static dir containing index.html) + second SDK listener sharing same store.
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

// portalReq issues request to test server with optional headers; Host defaults to server's own (loopback) address.
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

// Scenario 1: full app + link CRUD round trip through portal API.
func TestPortalAppLinkCRUD(t *testing.T) {
	portal, _, _ := newPortalEnv(t)

	resp, b := portalReq(t, portal, "POST", "/api/apps", `{"name":"crud app"}`, nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create app: %d %s", resp.StatusCode, b)
	}
	var created struct {
		ID         string `json:"id"`
		APIKey     string `json:"apiKey"`
		APIKeyHash string `json:"apiKeyHash"`
	}
	mustJSON(t, b, &created)
	if !strings.HasPrefix(created.APIKey, "dk_") || created.APIKeyHash != store.HashKey(created.APIKey) {
		t.Fatalf("server must mint the key and store its hash: %s", b)
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
			`"android":"https://play.google.com/store/apps/details?id=com.example","fallbackUrl":"https://example.com/fb0"}`, nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create link: %d %s", resp.StatusCode, b)
	}
	var link struct {
		ID  string `json:"id"`
		Key string `json:"key"`
	}
	mustJSON(t, b, &link)

	resp, b = portalReq(t, portal, "GET", "/api/apps/"+created.ID+"/links", "", nil)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(b), `"key":"c1"`) {
		t.Fatalf("list links: %d %s", resp.StatusCode, b)
	}

	// omitted destinations stay unchanged, explicit empty clears
	resp, b = portalReq(t, portal, "PATCH", "/api/links/"+link.ID,
		`{"url":"https://example.com/p2","ios":""}`, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("update link: %d %s", resp.StatusCode, b)
	}
	var updated struct {
		URL         string `json:"url"`
		IOS         string `json:"ios"`
		Android     string `json:"android"`
		FallbackURL string `json:"fallbackUrl"`
	}
	mustJSON(t, b, &updated)
	if updated.URL != "https://example.com/p2" || updated.IOS != "" ||
		updated.Android != "https://play.google.com/store/apps/details?id=com.example" ||
		updated.FallbackURL != "https://example.com/fb0" {
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

// Scenario 2: app matching settings persisted via PATCH /api/apps/{id}/matching drive matching engine: same fingerprint matches or 404s purely per configured threshold (950-score candidate: matches at 850, not 1200).
func TestPortalAppMatchingDrivesMatching(t *testing.T) {
	portal, sdk, st := newPortalEnv(t)
	app, link := setup(t, st)
	recordAndroidClick(t, st, app, link)

	resp, b := portalReq(t, portal, "PATCH", "/api/apps/"+app.ID+"/matching", `{"threshold":1200,"windowMinutes":15}`, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("set settings: %d %s", resp.StatusCode, b)
	}

	// score 950 (IP differs: 127.0.0.1 vs click IP): below raised threshold.
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

	// lower threshold back to default: 950-score candidate now matches.
	resp, b = portalReq(t, portal, "PATCH", "/api/apps/"+app.ID+"/matching", `{"threshold":850,"windowMinutes":15}`, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("reset settings: %d %s", resp.StatusCode, b)
	}
	resp, b = portalReq(t, sdk, "POST", "/api/link/match-link", androidFingerprintJSON(), authHeaders(app.ID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("at threshold 850 the 950-score match must match, got %d %s", resp.StatusCode, b)
	}

	resp, b = portalReq(t, portal, "GET", "/api/apps/"+app.ID, "", nil)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(b), `"matchThreshold":850`) ||
		!strings.Contains(string(b), `"matchWindowMinutes":15`) {
		t.Fatalf("get app: %d %s", resp.StatusCode, b)
	}

	for _, tc := range []struct {
		path, body string
		want       int
	}{
		{app.ID, `{"threshold":900}`, http.StatusBadRequest},
		{app.ID, `{"windowMinutes":15}`, http.StatusBadRequest},
		{app.ID, `{"threshold":699,"windowMinutes":15}`, http.StatusBadRequest},
		{app.ID, `{"threshold":850,"windowMinutes":181}`, http.StatusBadRequest},
		{app.ID, `not json`, http.StatusBadRequest},
		{"nope", `{"threshold":850,"windowMinutes":15}`, http.StatusNotFound},
	} {
		resp, b = portalReq(t, portal, "PATCH", "/api/apps/"+tc.path+"/matching", tc.body, nil)
		if resp.StatusCode != tc.want {
			t.Errorf("PATCH matching %s %s: %d %s; want %d", tc.path, tc.body, resp.StatusCode, b, tc.want)
		}
	}
}

// Scenario 3: analytics numbers. Recorded click + matched launch -> non-organic
// on the link; fresh no-match launch -> organic. Bad range -> 400.
func TestPortalAnalytics(t *testing.T) {
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

	resp, b = portalReq(t, portal, "GET", "/api/apps/"+app.ID+"/analytics?days=30", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("analytics: %d %s", resp.StatusCode, b)
	}
	var a store.Analytics
	mustJSON(t, b, &a)
	today := a.Days[len(a.Days)-1]
	if len(a.Days) != 30 || today.Clicks != 1 || today.Organic != 1 || today.NonOrganic != 1 {
		t.Fatalf("analytics days mismatch: %d days, today %+v", len(a.Days), today)
	}
	if len(a.Links) != 1 || a.Links[0].Key != link.Key || a.Links[0].Matches != 1 {
		t.Fatalf("analytics links mismatch: %+v", a.Links)
	}

	for _, q := range []string{"?days=5", "?platform=web"} {
		if resp, b := portalReq(t, portal, "GET", "/api/apps/"+app.ID+"/analytics"+q, "", nil); resp.StatusCode != http.StatusBadRequest {
			t.Errorf("analytics%s: %d %s; want 400", q, resp.StatusCode, b)
		}
	}
}

// Scenario 5: cross-origin requests rejected by the guard (CSRF).
func TestPortalRejectsCrossOrigin(t *testing.T) {
	portal, _, _ := newPortalEnv(t)
	resp, b := portalReq(t, portal, "POST", "/api/apps", `{"name":"x"}`,
		map[string]string{"Origin": "http://evil.example"})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin POST must be 403, got %d %s", resp.StatusCode, b)
	}
	// Same-origin (loopback) Origin passes.
	resp, b = portalReq(t, portal, "POST", "/api/apps", `{"name":"ok"}`,
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

// Scenario 7: static serving. GET / returns built index.html; missing file 404s plain text; missing static dir never crashes API.
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

	resp, b := portalReq(t, ts, "POST", "/api/apps", `{"name":"still-works"}`, nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("API must keep working without the static dir: %d %s", resp.StatusCode, b)
	}
	resp, b = portalReq(t, ts, "GET", "/", "", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("GET / without a static dir must 404, got %d %s", resp.StatusCode, b)
	}
}

// Scenario 8: show-once key semantics. Create response carries plaintext key; every later GET carries only hash.
func TestPortalKeyShownOnce(t *testing.T) {
	portal, _, _ := newPortalEnv(t)
	resp, b := portalReq(t, portal, "POST", "/api/apps", `{"name":"once"}`, nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create app: %d %s", resp.StatusCode, b)
	}
	var created struct {
		APIKey     string `json:"apiKey"`
		APIKeyHash string `json:"apiKeyHash"`
	}
	mustJSON(t, b, &created)
	if created.APIKey == "" || !strings.Contains(string(b), created.APIKey) {
		t.Fatalf("create response must contain the plaintext key: %s", b)
	}
	resp, b = portalReq(t, portal, "GET", "/api/apps", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list apps: %d %s", resp.StatusCode, b)
	}
	if strings.Contains(string(b), created.APIKey) {
		t.Fatalf("plaintext key leaked in list: %s", b)
	}
	if !strings.Contains(string(b), created.APIKeyHash) {
		t.Fatalf("list must carry the hash: %s", b)
	}
}

// Scenario 9: key management — rotation mints new key, old one dies; revoke empties hash, SDK auth fails.
func TestPortalRotateAndRevokeKey(t *testing.T) {
	portal, _, st := newPortalEnv(t)
	resp, b := portalReq(t, portal, "POST", "/api/apps", `{"name":"keys"}`, nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create app: %d %s", resp.StatusCode, b)
	}
	var created struct {
		ID         string `json:"id"`
		APIKey     string `json:"apiKey"`
		APIKeyHash string `json:"apiKeyHash"`
	}
	mustJSON(t, b, &created)

	resp, b = portalReq(t, portal, "POST", "/api/apps/"+created.ID+"/rotate-key", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("rotate key: %d %s", resp.StatusCode, b)
	}
	var rotated struct {
		APIKey     string `json:"apiKey"`
		APIKeyHash string `json:"apiKeyHash"`
	}
	mustJSON(t, b, &rotated)
	if rotated.APIKey == "" || rotated.APIKey == created.APIKey {
		t.Fatalf("rotate must mint a new key: %s", b)
	}
	if ok, _ := st.ValidateAPIKey(created.ID, created.APIKey); ok {
		t.Error("old key must stop working after rotation")
	}
	if ok, _ := st.ValidateAPIKey(created.ID, rotated.APIKey); !ok {
		t.Error("new key must authenticate after rotation")
	}

	resp, b = portalReq(t, portal, "DELETE", "/api/apps/"+created.ID+"/key", "", nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("revoke key: %d %s", resp.StatusCode, b)
	}
	if ok, _ := st.ValidateAPIKey(created.ID, rotated.APIKey); ok {
		t.Error("key must fail auth after revoke")
	}
}

func TestPortalCreateLinkDuplicateKey409(t *testing.T) {
	portal, _, _ := newPortalEnv(t)
	resp, b := portalReq(t, portal, "POST", "/api/apps", `{"name":"dup"}`, nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create app: %d %s", resp.StatusCode, b)
	}
	var app struct {
		ID string `json:"id"`
	}
	mustJSON(t, b, &app)
	for i, want := range []int{http.StatusCreated, http.StatusConflict} {
		resp, b = portalReq(t, portal, "POST", "/api/apps/"+app.ID+"/links", `{"key":"same","url":"https://example.com/p"}`, nil)
		if resp.StatusCode != want {
			t.Fatalf("create link #%d: %d %s; want %d", i+1, resp.StatusCode, b, want)
		}
	}
}

// unknown app on link create is 404, not 500
func TestPortalCreateLinkUnknownApp404(t *testing.T) {
	portal, _, _ := newPortalEnv(t)
	resp, b := portalReq(t, portal, "POST", "/api/apps/nope/links", `{"key":"k1","url":"https://example.com"}`, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown app: %d %s; want 404", resp.StatusCode, b)
	}
}

func TestPortalLinkURLValidation(t *testing.T) {
	portal, _, _ := newPortalEnv(t)
	resp, b := portalReq(t, portal, "POST", "/api/apps", `{"name":"urls"}`, nil)
	var app struct {
		ID string `json:"id"`
	}
	mustJSON(t, b, &app)
	path := "/api/apps/" + app.ID + "/links"
	for i, c := range []struct {
		body string
		want int
	}{
		{`{"key":"v1","url":"example.com/x"}`, http.StatusBadRequest},
		{`{"key":"v2","url":"javascript:alert(1)"}`, http.StatusBadRequest},
		{`{"key":"v3","url":"https://"}`, http.StatusBadRequest},
		{`{"key":"v4","url":"https://example.com","ios":"myapp://open"}`, http.StatusCreated},
		{`{"key":"v5","url":"https://example.com","android":"market://details?id=x"}`, http.StatusCreated},
	} {
		if resp, b = portalReq(t, portal, "POST", path, c.body, nil); resp.StatusCode != c.want {
			t.Errorf("case %d %s: %d %s; want %d", i, c.body, resp.StatusCode, b, c.want)
		}
	}
	var link struct {
		ID string `json:"id"`
	}
	mustJSON(t, b, &link)
	if resp, b = portalReq(t, portal, "PATCH", "/api/links/"+link.ID, `{"fallbackUrl":"nope"}`, nil); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("update bad fallbackUrl: %d %s; want 400", resp.StatusCode, b)
	}
}

// Scenario: GET /api/apps/{id} serves one app; unknown ids 404.
func TestPortalGetApp(t *testing.T) {
	portal, _, _ := newPortalEnv(t)

	resp, b := portalReq(t, portal, "POST", "/api/apps", `{"name":"single"}`, nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create app: %d %s", resp.StatusCode, b)
	}
	var created struct {
		ID string `json:"id"`
	}
	mustJSON(t, b, &created)

	resp, b = portalReq(t, portal, "GET", "/api/apps/"+created.ID, "", nil)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(b), `"name":"single"`) {
		t.Fatalf("get app: %d %s", resp.StatusCode, b)
	}

	resp, _ = portalReq(t, portal, "GET", "/api/apps/nope", "", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("get missing app: %d, want 404", resp.StatusCode)
	}
}
