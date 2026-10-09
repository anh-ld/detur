package api

// Portal tests: portal API CRUD, app matching settings driving matching engine, analytics,
// no-auth contract, origin/host guard, static serving, show-once API-key semantics.
// Real store + real mux via httptest; SDK endpoints on second listener sharing store (separate listeners, one store).

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"detur.dev/server/internal/fraud"
	"detur.dev/server/internal/httpx"
	"detur.dev/server/internal/store"
)

// portalAddr mirrors cfg.PortalAddr default (config.Load).
const portalAddr = "127.0.0.1:8081"

// newPortalEnv: portal listener (RegisterPortal with temp static dir containing index.html) + second SDK listener sharing same store.
func newPortalEnv(t *testing.T) (portal, sdk *httptest.Server, st *store.Store) {
	t.Helper()
	return newPortalEnvAt(t, filepath.Join(t.TempDir(), "detur-portal.db"))
}

// newPortalEnvAt: newPortalEnv on a caller-chosen DB path (tests that edit rows directly).
func newPortalEnvAt(t *testing.T, path string) (portal, sdk *httptest.Server, st *store.Store) {
	t.Helper()
	return newPortalEnvWith(t, path, "", 12)
}

// newPortalEnvWith: newPortalEnv with admin gating configured (adminPassword "" = AE1, no gating).
func newPortalEnvWith(t *testing.T, path, adminPassword string, adminHours int) (portal, sdk *httptest.Server, st *store.Store) {
	t.Helper()
	httpx.TrustProxy = true // api tests simulate the trusted-proxy deployment via X-Forwarded-For
	t.Cleanup(func() { httpx.TrustProxy = false })
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
	portal = httptest.NewServer(RegisterPortal(st, staticDir, []string{portalAddr}, "/cdn-cgi/access/logout", adminPassword, adminHours))
	t.Cleanup(portal.Close)

	sdkMux := http.NewServeMux()
	RegisterSDK(sdkMux, st, 24)
	sdk = httptest.NewServer(sdkMux)
	t.Cleanup(sdk.Close)
	return portal, sdk, st
}

// newPortalEnvAdmin: portal env with admin gating ON for the given password and session TTL.
func newPortalEnvAdmin(t *testing.T, password string, hours int) (portal, sdk *httptest.Server, st *store.Store) {
	t.Helper()
	return newPortalEnvWith(t, filepath.Join(t.TempDir(), "detur-portal.db"), password, hours)
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
	if !strings.Contains(string(b), `"sources":[]`) {
		t.Fatalf("analytics sources: want empty list for browser-only traffic, got %s", b)
	}

	for _, d := range []int{7, 14, 30, 60, 90, 180, 365} {
		resp, b = portalReq(t, portal, "GET", fmt.Sprintf("/api/apps/%s/analytics?days=%d", app.ID, d), "", nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("analytics days=%d: %d %s", d, resp.StatusCode, b)
		}
		var ra store.Analytics
		mustJSON(t, b, &ra)
		if len(ra.Days) != d {
			t.Fatalf("analytics len(Days) = %d; want %d", len(ra.Days), d)
		}
	}

	// platform filter over HTTP: the matched install is Android, nothing is iOS
	for _, tc := range []struct {
		platform           string
		clicks, nonOrganic int64
	}{{"android", 1, 1}, {"ios", 0, 0}} {
		resp, b := portalReq(t, portal, "GET", "/api/apps/"+app.ID+"/analytics?platform="+tc.platform, "", nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("analytics platform=%s: %d %s", tc.platform, resp.StatusCode, b)
		}
		var f store.Analytics
		mustJSON(t, b, &f)
		if d := f.Days[len(f.Days)-1]; d.Clicks != tc.clicks || d.NonOrganic != tc.nonOrganic {
			t.Errorf("platform=%s today = %+v; want %d clicks, %d non-organic", tc.platform, d, tc.clicks, tc.nonOrganic)
		}
	}

	// 1 probabilistic (bucketed), 1 organic (no candidate, unbucketed)
	resp, b = portalReq(t, portal, "GET", "/api/apps/"+app.ID+"/match-quality", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("match-quality: %d %s", resp.StatusCode, b)
	}
	var mq store.MatchQuality
	mustJSON(t, b, &mq)
	if mq.Methods[store.MethodProbabilistic] != 1 || mq.Methods[store.MethodOrganic] != 1 ||
		len(mq.Buckets) != 1 || mq.Buckets[0].Matched != 1 || mq.Buckets[0].From < 850 {
		t.Errorf("match-quality = %+v; want 1 probabilistic in a bucket >= 850, 1 organic", mq)
	}

	// custom range: 8 days
	resp, b = portalReq(t, portal, "GET", "/api/apps/"+app.ID+"/analytics?from=2026-10-01&to=2026-10-08", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("analytics custom range: %d %s", resp.StatusCode, b)
	}
	var customA store.Analytics
	mustJSON(t, b, &customA)
	if len(customA.Days) != 8 {
		t.Fatalf("analytics custom range len(Days) = %d; want 8", len(customA.Days))
	}

	for q, want := range map[string]int{
		"/api/apps/" + app.ID + "/analytics?days=5":                    http.StatusBadRequest,
		"/api/apps/" + app.ID + "/analytics?platform=web":              http.StatusBadRequest,
		"/api/apps/" + app.ID + "/analytics?from=2026-10-01":          http.StatusBadRequest,
		"/api/apps/" + app.ID + "/analytics?to=2026-10-08":            http.StatusBadRequest,
		"/api/apps/" + app.ID + "/analytics?from=bad&to=2026-10-08":   http.StatusBadRequest,
		"/api/apps/" + app.ID + "/analytics?from=2026-10-08&to=2026-10-01": http.StatusBadRequest,
		"/api/apps/missing/analytics":                                  http.StatusNotFound,
		"/api/apps/" + app.ID + "/match-quality?days=5":                http.StatusBadRequest,
		"/api/apps/missing/match-quality":                              http.StatusNotFound,
	} {
		if resp, b := portalReq(t, portal, "GET", q, "", nil); resp.StatusCode != want {
			t.Errorf("GET %s: %d %s; want %d", q, resp.StatusCode, b, want)
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
	ts := httptest.NewServer(RegisterPortal(st, filepath.Join(t.TempDir(), "no-such-dir"), []string{portalAddr}, "", "", 12))
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

func TestPortalConfig(t *testing.T) {
	portal, _, _ := newPortalEnv(t)
	resp, body := portalReq(t, portal, "GET", "/api/config", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body %s", resp.StatusCode, body)
	}
	var got struct {
		LogoutURL string `json:"logoutUrl"`
	}
	if err := json.Unmarshal(body, &got); err != nil || got.LogoutURL != "/cdn-cgi/access/logout" {
		t.Errorf("config = %s (err %v), want logoutUrl /cdn-cgi/access/logout", body, err)
	}
}

// fraudSettingsWire: GET/PATCH /fraud/settings wire shape.
type fraudSettingsWire struct {
	VelocityMode          string `json:"velocityMode"`
	TimingMode            string `json:"timingMode"`
	UserAgentMode         string `json:"userAgentMode"`
	IPMode                string `json:"ipMode"`
	VelocityIPMax         int    `json:"velocityIpMax"`
	VelocityLinkMax       int    `json:"velocityLinkMax"`
	VelocityWindowMinutes int    `json:"velocityWindowMinutes"`
	TimingShortSeconds    int    `json:"timingShortSeconds"`
	TimingLongHours       int    `json:"timingLongHours"`
	FingerprintMax        int    `json:"fingerprintMax"`
	FingerprintWindowDays int    `json:"fingerprintWindowDays"`
}

const validFraudBody = `{"velocityMode":"active","timingMode":"tagged","userAgentMode":"active","ipMode":"tagged",` +
	`"velocityIpMax":30,"velocityLinkMax":600,"velocityWindowMinutes":90,"timingShortSeconds":5,"timingLongHours":48,` +
	`"fingerprintMax":6,"fingerprintWindowDays":14}`

// U5: fraud settings GET defaults, PATCH round trip, validation, unknown app.
func TestPortalFraudSettings(t *testing.T) {
	portal, _, st := newPortalEnv(t)
	app, _ := setup(t, st)
	path := "/api/apps/" + app.ID + "/fraud/settings"

	resp, b := portalReq(t, portal, "GET", path, "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET settings: %d %s", resp.StatusCode, b)
	}
	var got fraudSettingsWire
	mustJSON(t, b, &got)
	d := fraud.Defaults
	want := fraudSettingsWire{d.VelocityMode, d.TimingMode, d.UserAgentMode, d.IPMode, d.VelocityIPMax, d.VelocityLinkMax,
		d.VelocityWindowMinutes, d.TimingShortSeconds, d.TimingLongHours, d.FingerprintMax, d.FingerprintWindowDays}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("fresh app settings = %+v; want %+v", got, want)
	}

	resp, b = portalReq(t, portal, "PATCH", path, validFraudBody, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PATCH settings: %d %s", resp.StatusCode, b)
	}
	var patched fraudSettingsWire
	mustJSON(t, b, &patched)
	want = fraudSettingsWire{"active", "tagged", "active", "tagged", 30, 600, 90, 5, 48, 6, 14}
	if !reflect.DeepEqual(patched, want) {
		t.Fatalf("PATCH response = %+v; want %+v", patched, want)
	}
	_, b = portalReq(t, portal, "GET", path, "", nil)
	mustJSON(t, b, &got)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("GET after PATCH = %+v; want %+v", got, want)
	}

	for _, tc := range []struct {
		name, id, body string
		want           int
		msg            string // "" = any non-empty message
	}{
		{"missing field", app.ID, strings.Replace(validFraudBody, `"fingerprintWindowDays":14`, `"x":1`, 1), http.StatusBadRequest, ""},
		{"missing mode", app.ID, strings.Replace(validFraudBody, `"ipMode":"tagged",`, ``, 1), http.StatusBadRequest, ""},
		{"bad mode", app.ID, strings.Replace(validFraudBody, `"velocityMode":"active"`, `"velocityMode":"block"`, 1), http.StatusBadRequest, ""},
		{"out of range", app.ID, strings.Replace(validFraudBody, `"velocityIpMax":30`, `"velocityIpMax":1`, 1), http.StatusBadRequest, ""},
		{"not json", app.ID, `nope`, http.StatusBadRequest, "invalid request body"},
		{"unknown app", "missing", validFraudBody, http.StatusNotFound, ""},
	} {
		resp, b := portalReq(t, portal, "PATCH", "/api/apps/"+tc.id+"/fraud/settings", tc.body, nil)
		if resp.StatusCode != tc.want || (tc.want == http.StatusBadRequest && strings.TrimSpace(string(b)) == "") || !strings.Contains(string(b), tc.msg) {
			t.Errorf("%s: %d %q; want %d with a message %q", tc.name, resp.StatusCode, b, tc.want, tc.msg)
		}
	}
	if s, _ := st.FraudSettings(app.ID); s.VelocityIPMax != 30 || s.FingerprintWindowDays != 14 {
		t.Errorf("rejected PATCH changed stored settings: %+v", s)
	}
	if resp, _ := portalReq(t, portal, "GET", "/api/apps/missing/fraud/settings", "", nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("GET unknown app settings: %d; want 404", resp.StatusCode)
	}
}

// U5 end to end: velocity active via PATCH, only candidate velocity-flagged -> organic; GET /fraud lists it as excluded with link key; range filter.
func TestPortalFraudDrivesMatching(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "detur-portal.db")
	portal, sdk, st := newPortalEnvAt(t, dbPath)
	app, link := setup(t, st)

	body := strings.Replace(validFraudBody, `"velocityIpMax":30`, `"velocityIpMax":2`, 1)
	body = strings.Replace(body, `"userAgentMode":"active"`, `"userAgentMode":"tagged"`, 1)
	if resp, b := portalReq(t, portal, "PATCH", "/api/apps/"+app.ID+"/fraud/settings", body, nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("PATCH settings: %d %s", resp.StatusCode, b)
	}
	recordAndroidClick(t, st, app, link)
	recordAndroidClick(t, st, app, link) // same device: one click row, 2 IP hits >= max 2

	hdr := authHeaders(app.ID)
	hdr["X-Forwarded-For"] = testIP
	if resp, b := portalReq(t, sdk, "POST", "/api/link/match-link", androidFingerprintJSON(), hdr); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("velocity-flagged only candidate must 404 (organic): %d %s", resp.StatusCode, b)
	}

	resp, b := portalReq(t, portal, "GET", "/api/apps/"+app.ID+"/fraud?days=7", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET fraud: %d %s", resp.StatusCode, b)
	}
	var f struct {
		Signals  map[string]int64 `json:"signals"`
		Installs []map[string]any `json:"installs"`
	}
	mustJSON(t, b, &f)
	if len(f.Installs) != 1 || f.Signals[fraud.SignalVelocity] != 1 {
		t.Fatalf("fraud = %s; want 1 velocity install", b)
	}
	in := f.Installs[0]
	if in["attribution"] != store.AttributionOrganic || in["fraudAction"] != store.FraudActionExcluded || in["fraudLinkKey"] != link.Key || in["linkKey"] != "" {
		t.Errorf("install row = %v; want organic, no linkKey, excluded, fraudLinkKey %q", in, link.Key)
	}
	if _, ok := in["deviceHash"]; ok {
		t.Errorf("install row exposes deviceHash: %v", in)
	}
	// fresh click: short timing fires too (tagged)
	if got := in["fraud"]; !reflect.DeepEqual(got, []any{fraud.SignalVelocity, fraud.SignalTiming}) {
		t.Errorf("install fraud = %v; want [velocity timing]", got)
	}

	// flagged 10 days ago: outside the 7-day range, inside 30
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE installs SET created_at = ?`, time.Now().UTC().AddDate(0, 0, -10).Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	db.Close()
	for q, n := range map[string]int{"7": 0, "30": 1} {
		_, b := portalReq(t, portal, "GET", "/api/apps/"+app.ID+"/fraud?days="+q, "", nil)
		mustJSON(t, b, &f)
		if len(f.Installs) != n || f.Signals[fraud.SignalVelocity] != int64(n) {
			t.Errorf("days=%s: %s; want %d", q, b, n)
		}
	}
	for q, want := range map[string]int{
		"/api/apps/" + app.ID + "/fraud?days=5": http.StatusBadRequest,
		"/api/apps/missing/fraud":               http.StatusNotFound,
	} {
		if resp, b := portalReq(t, portal, "GET", q, "", nil); resp.StatusCode != want {
			t.Errorf("GET %s: %d %s; want %d", q, resp.StatusCode, b, want)
		}
	}
}

// Backend failures on the fraud endpoints answer 500: report when the installs or links read fails, settings GET when fraud_settings is
// gone, PATCH when the upsert is aborted (a trigger; reads still work).
func TestPortalFraudBackendErrors(t *testing.T) {
	for _, tc := range []struct {
		name, fault, method, path, body string
	}{
		{"report installs", `DROP TABLE installs`, "GET", "/fraud", ""},
		{"report links", `DROP TABLE links`, "GET", "/fraud", ""},
		{"settings read", `DROP TABLE fraud_settings`, "GET", "/fraud/settings", ""},
		{"settings write", `CREATE TRIGGER no_write BEFORE INSERT ON fraud_settings BEGIN SELECT RAISE(ABORT, 'write failed'); END`, "PATCH", "/fraud/settings", validFraudBody},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dbPath := filepath.Join(t.TempDir(), "detur-portal.db")
			portal, _, st := newPortalEnvAt(t, dbPath)
			app, _ := setup(t, st)
			execSQL(t, dbPath, tc.fault)
			if resp, b := portalReq(t, portal, tc.method, "/api/apps/"+app.ID+tc.path, tc.body, nil); resp.StatusCode != http.StatusInternalServerError {
				t.Errorf("%s %s: %d %s; want 500", tc.method, tc.path, resp.StatusCode, b)
			}
		})
	}
}

// Default ranges without ?days=: fraud report and analytics cover 7 days, match quality 30; a flagged organic install 10 days old shows
// only in match quality.
func TestPortalDefaultRanges(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "detur-portal.db")
	portal, _, st := newPortalEnvAt(t, dbPath)
	app, _ := setup(t, st)
	if _, err := st.RecordInstall(store.Install{AppID: app.ID, DeviceHash: "d", Attribution: store.AttributionOrganic, Method: store.MethodOrganic, Fraud: fraud.SignalTiming}); err != nil {
		t.Fatal(err)
	}
	execSQL(t, dbPath, `UPDATE installs SET created_at = ?`, time.Now().UTC().AddDate(0, 0, -10).Format(time.RFC3339Nano))
	base := "/api/apps/" + app.ID
	var f struct {
		Signals  map[string]int64 `json:"signals"`
		Installs []map[string]any `json:"installs"`
	}
	_, b := portalReq(t, portal, "GET", base+"/fraud", "", nil)
	mustJSON(t, b, &f)
	if len(f.Installs) != 0 || f.Signals[fraud.SignalTiming] != 0 {
		t.Errorf("fraud (default range) = %s; want the 10-day-old install out", b)
	}
	var mq store.MatchQuality
	_, b = portalReq(t, portal, "GET", base+"/match-quality", "", nil)
	mustJSON(t, b, &mq)
	if mq.Methods[store.MethodOrganic] != 1 {
		t.Errorf("match-quality (default range) = %s; want the 10-day-old organic install in", b)
	}
	var a store.Analytics
	_, b = portalReq(t, portal, "GET", base+"/analytics", "", nil)
	mustJSON(t, b, &a)
	if len(a.Days) != 7 {
		t.Errorf("analytics (default range) days = %d; want 7", len(a.Days))
	}
}

// U2: elevate. Wrong password → 401 after >= 1s; right password → cookie with HttpOnly/SameSite=Lax/Path=/ and Max-Age = TTL, response carries expiresAt.
func TestPortalAdminSessionElevate(t *testing.T) {
	portal, _, _ := newPortalEnvAdmin(t, "s3cret-pw", 12)

	start := time.Now()
	resp, b := portalReq(t, portal, "POST", "/api/admin/session", `{"password":"wrong"}`, nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong password: %d %s; want 401", resp.StatusCode, b)
	}
	if d := time.Since(start); d < time.Second {
		t.Errorf("wrong-password response after %v; want >= 1s delay", d)
	}

	resp, b = portalReq(t, portal, "POST", "/api/admin/session", `{"password":"s3cret-pw"}`, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("right password: %d %s; want 200", resp.StatusCode, b)
	}
	var got struct {
		Admin     bool   `json:"admin"`
		ExpiresAt string `json:"expiresAt"`
	}
	mustJSON(t, b, &got)
	if !got.Admin {
		t.Errorf("login must respond {admin:true}, got %q", b)
	}
	if _, err := time.Parse(time.RFC3339, got.ExpiresAt); err != nil {
		t.Fatalf("login must respond {expiresAt} RFC3339, got %q", b)
	}
	cks := resp.Cookies()
	if len(cks) != 1 || cks[0].Name != adminCookie {
		t.Fatalf("login must set %s cookie, got %+v", adminCookie, cks)
	}
	c := cks[0]
	if !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.Path != "/" {
		t.Errorf("cookie flags = %+v; want HttpOnly, SameSite=Lax, Path=/", c)
	}
	if c.MaxAge != 12*3600 {
		t.Errorf("cookie MaxAge = %d; want %d (12h TTL)", c.MaxAge, 12*3600)
	}
}

// U2: session status. GET reflects active with a valid cookie, inactive without or with a tampered cookie.
func TestPortalAdminSessionStatus(t *testing.T) {
	portal, _, _ := newPortalEnvAdmin(t, "s3cret-pw", 12)

	resp, b := portalReq(t, portal, "GET", "/api/admin/session", "", nil)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(b), `"admin":false`) {
		t.Fatalf("no cookie: %d %s; want admin false", resp.StatusCode, b)
	}

	resp, b = portalReq(t, portal, "POST", "/api/admin/session", `{"password":"s3cret-pw"}`, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login: %d %s", resp.StatusCode, b)
	}
	token := resp.Cookies()[0].Value

	resp, b = portalReq(t, portal, "GET", "/api/admin/session", "", map[string]string{"Cookie": adminCookie + "=" + token})
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(b), `"admin":true`) {
		t.Fatalf("with cookie: %d %s; want admin true", resp.StatusCode, b)
	}

	// flip one hex char after the dot: signature no longer matches
	i := strings.IndexByte(token, '.') + 1
	alt := token[i]
	if alt == '0' {
		alt = '1'
	} else {
		alt = '0'
	}
	tampered := token[:i] + string(alt) + token[i+1:]
	resp, b = portalReq(t, portal, "GET", "/api/admin/session", "", map[string]string{"Cookie": adminCookie + "=" + tampered})
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(b), `"admin":false`) {
		t.Fatalf("tampered cookie: %d %s; want admin false", resp.StatusCode, b)
	}
}

// U2: exit. DELETE clears the cookie (Max-Age=0); a cookie-less GET afterwards reports inactive.
func TestPortalAdminSessionExit(t *testing.T) {
	portal, _, _ := newPortalEnvAdmin(t, "s3cret-pw", 12)
	resp, _ := portalReq(t, portal, "POST", "/api/admin/session", `{"password":"s3cret-pw"}`, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login: %d", resp.StatusCode)
	}

	resp, b := portalReq(t, portal, "DELETE", "/api/admin/session", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("exit: %d %s; want 200", resp.StatusCode, b)
	}
	if sc := resp.Header.Get("Set-Cookie"); !strings.Contains(sc, "Max-Age=0") {
		t.Errorf("exit must clear the cookie (Max-Age=0), got %q", sc)
	}
	resp, b = portalReq(t, portal, "GET", "/api/admin/session", "", nil)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(b), `"admin":false`) {
		t.Fatalf("after exit: %d %s; want admin false", resp.StatusCode, b)
	}
}

// U2/AE1: ADMIN_PASSWORD unset → POST 403 (nothing to elevate from), GET inactive, config.adminSet false; configured → adminSet true.
func TestPortalAdminSessionUnset(t *testing.T) {
	portal, _, _ := newPortalEnv(t)
	resp, b := portalReq(t, portal, "POST", "/api/admin/session", `{"password":"anything"}`, nil)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("POST with unset password: %d %s; want 403", resp.StatusCode, b)
	}
	resp, b = portalReq(t, portal, "GET", "/api/admin/session", "", nil)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(b), `"admin":false`) {
		t.Fatalf("GET with unset password: %d %s; want admin false", resp.StatusCode, b)
	}
	var cfg struct {
		AdminSet bool `json:"adminSet"`
	}
	resp, b = portalReq(t, portal, "GET", "/api/config", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("config: %d %s", resp.StatusCode, b)
	}
	mustJSON(t, b, &cfg)
	if cfg.AdminSet {
		t.Errorf("config with unset password must report adminSet false: %s", b)
	}

	portalAdmin, _, _ := newPortalEnvAdmin(t, "s3cret-pw", 12)
	resp, b = portalReq(t, portalAdmin, "GET", "/api/config", "", nil)
	mustJSON(t, b, &cfg)
	if resp.StatusCode != http.StatusOK || !cfg.AdminSet {
		t.Fatalf("config with password must report adminSet true: %s", b)
	}
}

// U3: admin route gate (KTD3). Gating ON: every admin route 403 without a cookie, succeeds with a valid session; viewer routes stay open. DELETE last: it removes the shared app.
func TestPortalAdminRouteGate(t *testing.T) {
	portal, _, _ := newPortalEnvAdmin(t, "s3cret-pw", 12)
	resp, b := portalReq(t, portal, "POST", "/api/admin/session", `{"password":"s3cret-pw"}`, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("elevate: %d %s", resp.StatusCode, b)
	}
	admin := map[string]string{"Cookie": adminCookie + "=" + resp.Cookies()[0].Value}

	resp, b = portalReq(t, portal, "POST", "/api/apps", `{"name":"gated"}`, admin)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create app as admin: %d %s", resp.StatusCode, b)
	}
	var app struct {
		ID string `json:"id"`
	}
	mustJSON(t, b, &app)

	cases := []struct {
		method, path, body string
		want               int
	}{
		{"POST", "/api/apps", `{"name":"gated2"}`, http.StatusCreated},
		{"POST", "/api/apps/" + app.ID + "/rotate-key", "", http.StatusOK},
		{"DELETE", "/api/apps/" + app.ID + "/key", "", http.StatusNoContent},
		{"PATCH", "/api/apps/" + app.ID, `{"iosAppId":"ABC123.com.example"}`, http.StatusOK},
		{"PATCH", "/api/apps/" + app.ID + "/matching", `{"threshold":850,"windowMinutes":15}`, http.StatusOK},
		{"PATCH", "/api/apps/" + app.ID + "/tagging", `{"tagLinks":true}`, http.StatusOK},
		{"GET", "/api/apps/" + app.ID + "/fraud/settings", "", http.StatusOK},
		{"PATCH", "/api/apps/" + app.ID + "/fraud/settings", validFraudBody, http.StatusOK},
		{"DELETE", "/api/apps/" + app.ID, "", http.StatusNoContent},
	}
	// gate first: every admin route 403 without a cookie
	for _, tc := range cases {
		resp, b := portalReq(t, portal, tc.method, tc.path, tc.body, nil)
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s %s without cookie: %d %s; want 403", tc.method, tc.path, resp.StatusCode, b)
		}
	}
	// then the same routes with a valid session behave as today
	for _, tc := range cases {
		resp, b := portalReq(t, portal, tc.method, tc.path, tc.body, admin)
		if resp.StatusCode != tc.want {
			t.Errorf("%s %s with cookie: %d %s; want %d", tc.method, tc.path, resp.StatusCode, b, tc.want)
		}
	}
}

// U3: viewer routes keep working without a cookie while gating is ON (link + monitoring workflow, zero 403s).
func TestPortalAdminGateViewerRoutesOpen(t *testing.T) {
	portal, _, _ := newPortalEnvAdmin(t, "s3cret-pw", 12)
	resp, b := portalReq(t, portal, "POST", "/api/admin/session", `{"password":"s3cret-pw"}`, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("elevate: %d %s", resp.StatusCode, b)
	}
	admin := map[string]string{"Cookie": adminCookie + "=" + resp.Cookies()[0].Value}
	resp, b = portalReq(t, portal, "POST", "/api/apps", `{"name":"viewer"}`, admin)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create app: %d %s", resp.StatusCode, b)
	}
	var app struct {
		ID string `json:"id"`
	}
	mustJSON(t, b, &app)
	resp, b = portalReq(t, portal, "POST", "/api/apps/"+app.ID+"/links", `{"key":"v1","url":"https://example.com"}`, nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create link as viewer: %d %s", resp.StatusCode, b)
	}
	var link struct {
		ID string `json:"id"`
	}
	mustJSON(t, b, &link)

	cases := []struct {
		method, path, body string
		want               int
	}{
		{"GET", "/api/config", "", http.StatusOK},
		{"GET", "/api/apps", "", http.StatusOK},
		{"GET", "/api/apps/" + app.ID, "", http.StatusOK},
		{"GET", "/api/apps/" + app.ID + "/links", "", http.StatusOK},
		{"POST", "/api/apps/" + app.ID + "/links", `{"key":"v2","url":"https://example.com/x"}`, http.StatusCreated},
		{"PATCH", "/api/links/" + link.ID, `{"url":"https://example.com/y"}`, http.StatusOK},
		{"DELETE", "/api/links/" + link.ID, "", http.StatusNoContent},
		{"GET", "/api/apps/" + app.ID + "/analytics", "", http.StatusOK},
		{"GET", "/api/apps/" + app.ID + "/match-quality", "", http.StatusOK},
		{"GET", "/api/apps/" + app.ID + "/health", "", http.StatusOK},
		{"GET", "/api/apps/" + app.ID + "/fraud", "", http.StatusOK},
	}
	for _, tc := range cases {
		resp, b := portalReq(t, portal, tc.method, tc.path, tc.body, nil)
		if resp.StatusCode != tc.want {
			t.Errorf("%s %s as viewer: %d %s; want %d", tc.method, tc.path, resp.StatusCode, b, tc.want)
		}
	}
}

// U3: expired cookie on an admin route → 403.
func TestPortalAdminGateExpiredCookie(t *testing.T) {
	portal, _, _ := newPortalEnvAdmin(t, "s3cret-pw", 12)
	expired, _ := mintToken("s3cret-pw", 1, time.Now().Add(-2*time.Hour)) // expired an hour ago
	resp, b := portalReq(t, portal, "POST", "/api/apps", `{"name":"x"}`,
		map[string]string{"Cookie": adminCookie + "=" + expired})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expired cookie: %d %s; want 403", resp.StatusCode, b)
	}
}

// U3/AE1: ADMIN_PASSWORD unset → admin routes succeed without a cookie (today's behavior).
func TestPortalAdminGateUnsetPassword(t *testing.T) {
	portal, _, _ := newPortalEnv(t)
	resp, b := portalReq(t, portal, "POST", "/api/apps", `{"name":"open"}`, nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST /api/apps without cookie: %d %s; want 201", resp.StatusCode, b)
	}
	var app struct {
		ID string `json:"id"`
	}
	mustJSON(t, b, &app)
	resp, b = portalReq(t, portal, "POST", "/api/apps/"+app.ID+"/rotate-key", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("rotate-key without cookie: %d %s; want 200", resp.StatusCode, b)
	}
	resp, _ = portalReq(t, portal, "DELETE", "/api/apps/"+app.ID, "", nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete app without cookie: %d; want 204", resp.StatusCode)
	}
}

// Tag deferred links: off = destinations as stored; on = match-link and resolve-short add detur_link=<key>, never over an existing one.
func TestPortalTaggingDrivesDestinations(t *testing.T) {
	portal, sdk, st := newPortalEnv(t)
	app, link := setup(t, st)
	kept, _ := st.CreateLink(store.Link{AppID: app.ID, Key: "kept", URL: "https://example.com/p?detur_link=mine"})
	destinations := func() (match, resolve, resolveKept string) {
		t.Helper()
		recordAndroidClick(t, st, app, link)
		hdr := authHeaders(app.ID)
		hdr["X-Forwarded-For"] = testIP
		var out struct {
			Link string `json:"link"`
		}
		_, b := portalReq(t, sdk, "POST", "/api/link/match-link", androidFingerprintJSON(), hdr)
		mustJSON(t, b, &out)
		match = out.Link
		_, b = portalReq(t, sdk, "POST", "/api/link/resolve-short", `{"url":"https://lnk.example/abc"}`, authHeaders(app.ID))
		mustJSON(t, b, &out)
		resolve = out.Link
		_, b = portalReq(t, sdk, "POST", "/api/link/resolve-short", `{"url":"https://lnk.example/kept"}`, authHeaders(app.ID))
		mustJSON(t, b, &out)
		return match, resolve, out.Link
	}
	if m, r, k := destinations(); m != link.URL || r != link.URL || k != kept.URL {
		t.Fatalf("tagging off: %s, %s, %s; want stored URLs", m, r, k)
	}

	resp, b := portalReq(t, portal, "PATCH", "/api/apps/"+app.ID+"/tagging", `{"tagLinks":true}`, nil)
	var got struct {
		TagLinks bool `json:"tagLinks"`
	}
	mustJSON(t, b, &got)
	if resp.StatusCode != http.StatusOK || !got.TagLinks {
		t.Fatalf("PATCH tagging: %d %s; want 200 tagLinks true", resp.StatusCode, b)
	}
	want := link.URL + "?detur_link=abc"
	if m, r, k := destinations(); m != want || r != want || k != kept.URL {
		t.Errorf("tagging on: %s, %s, %s; want %s twice and %s", m, r, k, want, kept.URL)
	}
}
