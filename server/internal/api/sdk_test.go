package api

// SDK endpoint tests. Real store + real mux via httptest; no store-layer
// mocks.

import (
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite" // SQLite driver: tests drop tables to inject backend errors

	"detur.dev/server/internal/store"
)

const (
	testAPIKey = "sekrit-key-123"
	testIP     = "203.0.113.7"
	otherIP    = "198.51.100.9"
)

func newTestServer(t *testing.T) (*httptest.Server, *store.Store, string) {
	t.Helper()
	path := t.TempDir() + "/detur-api.db"
	st, err := store.Open(path)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	mux := http.NewServeMux()
	RegisterSDK(mux, st, 24)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts, st, path
}

func setup(t *testing.T, s *store.Store) (store.App, store.Link) {
	t.Helper()
	app, err := s.CreateApp("api test app", testAPIKey)
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	link, err := s.CreateLink(store.Link{
		AppID: app.ID, Key: "abc", URL: "https://example.com/product",
		IOS:     "https://apps.apple.com/app/id123",
		Android: "https://play.google.com/store/apps/details?id=com.example",
	})
	if err != nil {
		t.Fatalf("CreateLink: %v", err)
	}
	return app, link
}

// recordAndroidClick persists click carrying every Android-scorable signal, matching fingerprint scores 1450 >= 850.
func recordAndroidClick(t *testing.T, s *store.Store, app store.App, link store.Link) store.Click {
	t.Helper()
	c, err := s.RecordClick(store.Click{
		AppID: app.ID, LinkID: link.ID, Destination: link.URL,
		Fingerprint: store.Fingerprint{
			IP: testIP, Device: "Pixel 7", Locale: "en", Timezone: "Europe/Warsaw",
			Screen:    "393x852@3",
			UserAgent: "Mozilla/5.0 (Linux; Android 14; Pixel 7 Build/TQ3A.230805.001)",
		},
	}, 15, 24)
	if err != nil {
		t.Fatalf("RecordClick: %v", err)
	}
	return c
}

// androidFingerprintJSON: verbatim probabilistic fingerprint payload SDK sends on first launch (fingerprint.ts): locale = [{languageTag}].
func androidFingerprintJSON() string {
	return `{"platform":"android","model":"Pixel 7","manufacturer":"Google",` +
		`"systemVersion":"14","screenWidth":393,"screenHeight":852,"scale":3,` +
		`"locale":[{"languageTag":"en-US"}],"timezone":"Europe/Warsaw",` +
		`"userAgent":"Mozilla/5.0 (Linux; Android 14; Pixel 7 Build/TQ3A.230805.001)",` +
		`"timestamp":1720000000000}`
}

func mismatchedFingerprintJSON() string {
	return `{"platform":"android","model":"Pixel 9","manufacturer":"Google",` +
		`"systemVersion":"99","screenWidth":100,"screenHeight":100,"scale":1,` +
		`"locale":[{"languageTag":"fr"}],"timezone":"UTC",` +
		`"userAgent":"Mozilla/5.0 (Linux; Android 99; Pixel 9 Build/X)"}`
}

func authHeaders(appID string) map[string]string {
	return map[string]string{
		"Authorization": "Bearer " + testAPIKey,
		"X-App-ID":      appID,
		"X-SDK":         "react-native/2.3.1",
		"Content-Type":  "application/json",
	}
}

func doPost(t *testing.T, ts *httptest.Server, path, body string, hdr map[string]string) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, ts.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp, b
}

// dropTable deletes table via second connection (WAL allows it), injecting backend failure leaving rest of store intact.
func dropTable(t *testing.T, path, table string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec("DROP TABLE " + table); err != nil {
		t.Fatalf("DROP TABLE %s: %v", table, err)
	}
}

func countInstallsAttribution(t *testing.T, path, appID, attribution string) int64 {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()
	var n int64
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM installs WHERE app_id = ? AND attribution = ?`,
		appID, attribution,
	).Scan(&n); err != nil {
		t.Fatalf("count installs: %v", err)
	}
	return n
}

// Scenario 1: valid fingerprint above threshold -> 200 link + non-organic
// install recorded.
func TestMatchLinkFingerprintAboveThreshold(t *testing.T) {
	ts, s, _ := newTestServer(t)
	app, link := setup(t, s)
	recordAndroidClick(t, s, app, link)
	hdr := authHeaders(app.ID)
	hdr["X-Forwarded-For"] = testIP
	resp, b := doPost(t, ts, "/api/link/match-link", androidFingerprintJSON(), hdr)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body %s; want 200", resp.StatusCode, b)
	}
	var out struct {
		Link string `json:"link"`
	}
	if err := json.Unmarshal(b, &out); err != nil || out.Link != link.URL {
		t.Fatalf("body = %s; want top-level link %s", b, link.URL)
	}
	organic, nonOrganic, err := s.CountInstalls(app.ID)
	if err != nil || organic != 0 || nonOrganic != 1 {
		t.Fatalf("installs = organic %d non-organic %d (%v); want 0/1 (AE1)", organic, nonOrganic, err)
	}
}

// Scenario 1b: failed install write must not deny link — match-link still returns 200 (backend errors never deny link).
func TestMatchLinkInstallFailureStillReturnsLink(t *testing.T) {
	ts, s, path := newTestServer(t)
	app, link := setup(t, s)
	recordAndroidClick(t, s, app, link)
	dropTable(t, path, "installs")
	hdr := authHeaders(app.ID)
	hdr["X-Forwarded-For"] = testIP
	resp, b := doPost(t, ts, "/api/link/match-link", androidFingerprintJSON(), hdr)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body %s; want 200 even when the install write fails", resp.StatusCode, b)
	}
	var out struct {
		Link string `json:"link"`
	}
	if err := json.Unmarshal(b, &out); err != nil || out.Link != link.URL {
		t.Fatalf("body = %s; want top-level link %s", b, link.URL)
	}
}

// Scenario 2: no matching click -> 404 + organic install recorded.
func TestMatchLinkNoMatch404Organic(t *testing.T) {
	ts, s, _ := newTestServer(t)
	app, _ := setup(t, s)
	recordAndroidClick(t, s, app, mustLink(t, s, app))
	hdr := authHeaders(app.ID)
	hdr["X-Forwarded-For"] = otherIP
	resp, b := doPost(t, ts, "/api/link/match-link", mismatchedFingerprintJSON(), hdr)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, body %s; want 404", resp.StatusCode, b)
	}
	organic, nonOrganic, err := s.CountInstalls(app.ID)
	if err != nil || organic != 1 || nonOrganic != 0 {
		t.Fatalf("installs = organic %d non-organic %d (%v); want 1/0 (AE2)", organic, nonOrganic, err)
	}
}

func mustLink(t *testing.T, s *store.Store, app store.App) store.Link {
	t.Helper()
	l, err := s.GetLinkByKey(app.ID, "abc")
	if err != nil {
		t.Fatalf("GetLinkByKey: %v", err)
	}
	return l
}

// Scenario 3: backend error mid-request -> 404 + unknown attribution row recorded, logged distinctly.
func TestMatchLinkBackendErrorFailOpen404Unknown(t *testing.T) {
	ts, s, path := newTestServer(t)
	app, _ := setup(t, s)
	// matching reads clicks table; drop it. installs stays intact, unknown-attribution row persists.
	dropTable(t, path, "clicks")
	hdr := authHeaders(app.ID)
	resp, b := doPost(t, ts, "/api/link/match-link", androidFingerprintJSON(), hdr)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, body %s; want 404 (fail-open R3)", resp.StatusCode, b)
	}
	if n := countInstallsAttribution(t, path, app.ID, store.AttributionUnknown); n != 1 {
		t.Fatalf("unknown attribution rows = %d; want 1", n)
	}
}

// Scenario 4: duplicate match-link call -> single attribution row (idempotent).
func TestMatchLinkDuplicateIdempotent(t *testing.T) {
	ts, s, _ := newTestServer(t)
	app, link := setup(t, s)
	recordAndroidClick(t, s, app, link)
	hdr := authHeaders(app.ID)
	hdr["X-Forwarded-For"] = testIP
	body := androidFingerprintJSON()
	r1, b1 := doPost(t, ts, "/api/link/match-link", body, hdr)
	r2, b2 := doPost(t, ts, "/api/link/match-link", body, hdr)
	if r1.StatusCode != http.StatusOK || r2.StatusCode != http.StatusOK {
		t.Fatalf("statuses = %d/%d; want 200/200 (bodies %s / %s)", r1.StatusCode, r2.StatusCode, b1, b2)
	}
	if string(b1) != string(b2) {
		t.Errorf("duplicate responses differ: %s vs %s", b1, b2)
	}
	organic, nonOrganic, err := s.CountInstalls(app.ID)
	if err != nil || organic != 0 || nonOrganic != 1 {
		t.Fatalf("installs = organic %d non-organic %d (%v); want single non-organic row", organic, nonOrganic, err)
	}
}

// Scenario 5: invalid apiKey -> 401 with SDK-visible error shape.
func TestMatchLinkInvalidAPIKey401(t *testing.T) {
	ts, s, _ := newTestServer(t)
	app, _ := setup(t, s)
	hdr := authHeaders(app.ID)
	hdr["Authorization"] = "Bearer wrong-key"
	resp, b := doPost(t, ts, "/api/link/match-link", androidFingerprintJSON(), hdr)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, body %s; want 401", resp.StatusCode, b)
	}
	var out struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(b, &out); err != nil || out.Error.Message != "unauthorized" {
		t.Fatalf("body = %s; want error.message unauthorized", b)
	}
}

// Scenario 6: missing X-App-ID -> 401.
func TestMatchLinkMissingAppID401(t *testing.T) {
	ts, s, _ := newTestServer(t)
	app, _ := setup(t, s)
	hdr := authHeaders(app.ID)
	delete(hdr, "X-App-ID")
	resp, b := doPost(t, ts, "/api/link/match-link", androidFingerprintJSON(), hdr)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, body %s; want 401", resp.StatusCode, b)
	}
}

// Scenario 7: clickId present but unknown -> 404, no probabilistic fallback even with strongly matching click inside window.
func TestMatchLinkUnknownClickIDNoProbabilisticFallback(t *testing.T) {
	ts, s, _ := newTestServer(t)
	app, link := setup(t, s)
	recordAndroidClick(t, s, app, link)
	hdr := authHeaders(app.ID)
	hdr["X-Forwarded-For"] = testIP
	resp, b := doPost(t, ts, "/api/link/match-link", `{"clickId":"unknown-click"}`, hdr)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, body %s; want 404 (no fallback)", resp.StatusCode, b)
	}
	organic, nonOrganic, _ := s.CountInstalls(app.ID)
	if organic != 1 || nonOrganic != 0 {
		t.Fatalf("installs = organic %d non-organic %d; want organic 1", organic, nonOrganic)
	}
}

// Scenario 8: resolve-short with a valid URL -> link/route/parameters.
func TestResolveShortValid(t *testing.T) {
	ts, s, _ := newTestServer(t)
	app, link := setup(t, s)
	resp, b := doPost(t, ts, "/api/link/resolve-short",
		`{"url":"https://lnk.example/abc?x=1&y=2"}`, authHeaders(app.ID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body %s; want 200", resp.StatusCode, b)
	}
	var out struct {
		Link       string `json:"link"`
		Route      string `json:"route"`
		Parameters string `json:"parameters"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("body %s: %v", b, err)
	}
	if out.Link != link.URL || out.Route != "/abc" || out.Parameters != "x=1&y=2" {
		t.Fatalf("body = %s; want link %s route /abc parameters x=1&y=2", b, link.URL)
	}
}

// Scenario 9: resolve-short with an unknown key -> 404 (SDK null).
func TestResolveShortUnknown404(t *testing.T) {
	ts, s, _ := newTestServer(t)
	app, _ := setup(t, s)
	resp, b := doPost(t, ts, "/api/link/resolve-short",
		`{"url":"https://lnk.example/nope"}`, authHeaders(app.ID))
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, body %s; want 404", resp.StatusCode, b)
	}
}

// Scenario 10: universal-link-click fails open on DB error -> allowed:true with no-limit fields present.
func TestUniversalLinkClickFailOpenOnDBError(t *testing.T) {
	ts, s, path := newTestServer(t)
	app, _ := setup(t, s)
	dropTable(t, path, "clicks")
	hdr := authHeaders(app.ID)
	resp, b := doPost(t, ts, "/api/link/universal-link-click",
		`{"url":"https://lnk.example/abc","timestamp":1720000000000,"platform":"ios"}`, hdr)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body %s; want 200 allow-shape", resp.StatusCode, b)
	}
	var out struct {
		Allowed        bool   `json:"allowed"`
		EffectiveLimit int    `json:"effectiveLimit"`
		ClicksInPeriod int    `json:"clicksInPeriod"`
		ClickID        string `json:"clickId"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("body %s: %v", b, err)
	}
	if !out.Allowed || out.EffectiveLimit != -1 || out.ClicksInPeriod != 0 || out.ClickID == "" {
		t.Fatalf("body = %s; want allowed true, effectiveLimit -1, clicksInPeriod 0, clickId set", b)
	}
}

// Scenario 11: universal-link-click normal -> allowed:true, effectiveLimit -1, clickId returned, click recorded.
func TestUniversalLinkClickNormal(t *testing.T) {
	ts, s, _ := newTestServer(t)
	app, link := setup(t, s)
	resp, b := doPost(t, ts, "/api/link/universal-link-click",
		`{"url":"https://lnk.example/abc","timestamp":1720000000000,"platform":"ios"}`, authHeaders(app.ID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body %s; want 200", resp.StatusCode, b)
	}
	var out struct {
		Allowed        bool   `json:"allowed"`
		EffectiveLimit int    `json:"effectiveLimit"`
		ClickID        string `json:"clickId"`
	}
	if err := json.Unmarshal(b, &out); err != nil || !out.Allowed || out.EffectiveLimit != -1 || out.ClickID == "" {
		t.Fatalf("body = %s; want allow-shape with clickId", b)
	}
	n, err := s.CountClicks(app.ID)
	if err != nil || n != 1 {
		t.Fatalf("clicks = %d (%v); want 1 recorded", n, err)
	}
	clicks, err := s.ClicksSince(app.ID, time.Now().Add(-time.Hour))
	if err != nil || len(clicks) != 1 || clicks[0].Destination != link.URL {
		t.Fatalf("clicks since = %d (%v); want 1 with destination %s", len(clicks), err, link.URL)
	}
}

// SDK deep-link clicks not bot-filtered: caller is app (Dub skips bot checks for deeplink opens), bot-ish UA still records.
func TestUniversalLinkClickBotUARecorded(t *testing.T) {
	ts, s, _ := newTestServer(t)
	app, _ := setup(t, s)
	hdr := authHeaders(app.ID)
	hdr["User-Agent"] = "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)"
	resp, b := doPost(t, ts, "/api/link/universal-link-click",
		`{"url":"https://lnk.example/abc","platform":"unknown"}`, hdr)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body %s; want 200", resp.StatusCode, b)
	}
	var out struct {
		Allowed bool `json:"allowed"`
	}
	if err := json.Unmarshal(b, &out); err != nil || !out.Allowed {
		t.Fatalf("body = %s; want allowed true", b)
	}
	if n, _ := s.CountClicks(app.ID); n != 1 {
		t.Errorf("click recorded = %d; want 1 (SDK clicks are not bot-filtered)", n)
	}
}

// Scenario 12: analytics event accepted + persisted (event_name verbatim, task-spec "event" key as fallback).
func TestAnalyticsEventPersisted(t *testing.T) {
	ts, s, _ := newTestServer(t)
	app, _ := setup(t, s)
	resp, b := doPost(t, ts, "/api/analytics/event",
		`{"event_name":"install","data":{"foo":1},"timestamp":"2026-09-28T00:00:00Z","platform":"ios","device_id":"d1"}`,
		authHeaders(app.ID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body %s; want 200", resp.StatusCode, b)
	}
	resp, b = doPost(t, ts, "/api/analytics/event", `{"event":"install2"}`, authHeaders(app.ID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("second status = %d, body %s; want 200", resp.StatusCode, b)
	}
	n, err := s.CountEvents(app.ID)
	if err != nil || n != 2 {
		t.Fatalf("events = %d (%v); want 2 persisted", n, err)
	}
}

// Scenario 13: analytics retention accepted + persisted (task-spec body {days: N} persists as event "retention").
func TestAnalyticsRetentionPersisted(t *testing.T) {
	ts, s, path := newTestServer(t)
	app, _ := setup(t, s)
	resp, b := doPost(t, ts, "/api/analytics/retention", `{"days":7}`, authHeaders(app.ID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body %s; want 200", resp.StatusCode, b)
	}
	n, err := s.CountEvents(app.ID)
	if err != nil || n != 1 {
		t.Fatalf("events = %d (%v); want 1 persisted", n, err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()
	var name string
	if err := db.QueryRow(`SELECT event FROM events WHERE app_id = ?`, app.ID).Scan(&name); err != nil {
		t.Fatalf("read event name: %v", err)
	}
	if name != "retention" {
		t.Errorf("event name = %q; want retention", name)
	}
}

// auth wrapper applies to all five endpoints.
func TestAllSDKEndpointsRequireAuth(t *testing.T) {
	ts, s, _ := newTestServer(t)
	_, _ = setup(t, s)
	for _, path := range []string{
		"/api/link/match-link",
		"/api/link/resolve-short",
		"/api/link/universal-link-click",
		"/api/analytics/event",
		"/api/analytics/retention",
	} {
		resp, _ := doPost(t, ts, path, `{}`, nil)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s without auth: status %d; want 401", path, resp.StatusCode)
		}
	}
}

// universal-link-click fails open even when auth check cannot query store: closed DB must still yield allow shape.
func TestUniversalLinkClickAuthFailOpenOnClosedDB(t *testing.T) {
	ts, s, _ := newTestServer(t)
	app, _ := setup(t, s)
	if err := s.Close(); err != nil {
		t.Fatalf("store.Close: %v", err)
	}
	resp, b := doPost(t, ts, "/api/link/universal-link-click",
		`{"url":"https://lnk.example/abc"}`, authHeaders(app.ID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body %s; want 200 allow-shape (fail-open R3)", resp.StatusCode, b)
	}
	var out struct {
		Allowed bool `json:"allowed"`
	}
	if err := json.Unmarshal(b, &out); err != nil || !out.Allowed {
		t.Fatalf("body = %s; want allowed true", b)
	}
}

func TestMatchLinkAuthFailOpenOnClosedDB(t *testing.T) {
	ts, s, _ := newTestServer(t)
	app, _ := setup(t, s)
	if err := s.Close(); err != nil {
		t.Fatalf("store.Close: %v", err)
	}
	resp, b := doPost(t, ts, "/api/link/match-link", androidFingerprintJSON(), authHeaders(app.ID))
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, body %s; want 404 no-match (fail-open R3)", resp.StatusCode, b)
	}
}
