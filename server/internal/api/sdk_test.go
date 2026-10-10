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

	"detur.dev/server/internal/fraud"
	"detur.dev/server/internal/httpx"
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
		Platform: "android", Kind: store.KindApp, // as the browser redirect records it
	}, 24)
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

// One click attributes at most one install: second device with same signals gets no match.
func TestMatchLinkTwoDevicesOneClick(t *testing.T) {
	ts, s, _ := newTestServer(t)
	app, link := setup(t, s)
	recordAndroidClick(t, s, app, link)
	hdr := authHeaders(app.ID)
	hdr["X-Forwarded-For"] = testIP
	r1, b1 := doPost(t, ts, "/api/link/match-link", androidFingerprintJSON(), hdr)
	if r1.StatusCode != http.StatusOK {
		t.Fatalf("first device status = %d; want 200 (%s)", r1.StatusCode, b1)
	}
	other := strings.Replace(androidFingerprintJSON(), `"manufacturer":"Google"`, `"manufacturer":"Other"`, 1)
	r2, b2 := doPost(t, ts, "/api/link/match-link", other, hdr)
	if r2.StatusCode != http.StatusNotFound {
		t.Fatalf("second device status = %d; want 404 (%s)", r2.StatusCode, b2)
	}
	organic, nonOrganic, err := s.CountInstalls(app.ID)
	if err != nil || organic != 1 || nonOrganic != 1 {
		t.Errorf("installs = organic %d non-organic %d (%v); want 1/1", organic, nonOrganic, err)
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
		Allowed bool   `json:"allowed"`
		ClickID string `json:"clickId"`
	}
	if err := json.Unmarshal(b, &out); err != nil || !out.Allowed {
		t.Fatalf("body = %s; want allowed true", b)
	}
	if len(out.ClickID) != 16 {
		t.Errorf("clickId = %q; want 16-char fresh id", out.ClickID)
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

// installPlatform: Apple manufacturer -> ios, other manufacturer -> android, else app UA markers.
func TestInstallPlatform(t *testing.T) {
	for _, c := range []struct {
		body matchLinkBody
		want string
	}{
		{matchLinkBody{Manufacturer: "Apple"}, "ios"},
		{matchLinkBody{Manufacturer: "apple"}, "ios"},
		{matchLinkBody{Manufacturer: "samsung"}, "android"},
		{matchLinkBody{UserAgent: "MyApp/1 CFNetwork/1490 Darwin/23.5.0"}, "ios"},
		{matchLinkBody{ClickID: "abc"}, ""},
	} {
		if got := installPlatform(c.body); got != c.want {
			t.Errorf("installPlatform(%+v) = %q; want %q", c.body, got, c.want)
		}
	}
}

// Analytics labels from real SDK paths: universal-link open counts as an iOS open; a clickId-only match takes the matched click's platform.
func TestSDKAnalyticsLabels(t *testing.T) {
	ts, s, _ := newTestServer(t)
	app, link := setup(t, s)
	hdr := authHeaders(app.ID)
	hdr["User-Agent"] = "MyApp/1 CFNetwork/1490 Darwin/23.5.0"
	if resp, b := doPost(t, ts, "/api/link/universal-link-click", `{"url":"https://lnk.example/abc"}`, hdr); resp.StatusCode != http.StatusOK {
		t.Fatalf("universal-link-click: %d %s", resp.StatusCode, b)
	}
	c := recordAndroidClick(t, s, app, link)
	if resp, b := doPost(t, ts, "/api/link/match-link", `{"clickId":"`+c.ID+`"}`, authHeaders(app.ID)); resp.StatusCode != http.StatusOK {
		t.Fatalf("clickId match: %d %s", resp.StatusCode, b)
	}

	ios, err := s.Analytics(app.ID, 7, "ios", time.Now())
	if err != nil {
		t.Fatalf("Analytics ios: %v", err)
	}
	if got := ios.Days[6]; got.Opens != 1 || got.NonOrganic != 0 {
		t.Errorf("ios today = %+v; want 1 open, 0 installs", got)
	}
	android, err := s.Analytics(app.ID, 7, "android", time.Now())
	if err != nil {
		t.Fatalf("Analytics android: %v", err)
	}
	if got := android.Days[6]; got.NonOrganic != 1 || got.Opens != 0 {
		t.Errorf("android today = %+v; want 1 non-organic install (platform from matched click), 0 opens", got)
	}
	if len(android.Links) != 1 || android.Links[0].Matches != 1 {
		t.Errorf("android links = %+v; want the link with 1 match", android.Links)
	}
}

// Fraud UA signal skips opens (KTD3): native okhttp UA on a universal-link open is not flagged; kind persisted.
func TestUniversalLinkOpenNotUASuspect(t *testing.T) {
	ts, s, _ := newTestServer(t)
	app, _ := setup(t, s)
	hdr := authHeaders(app.ID)
	hdr["User-Agent"] = "okhttp/4.12.0"
	resp, b := doPost(t, ts, "/api/link/universal-link-click",
		`{"url":"https://lnk.example/abc","platform":"android"}`, hdr)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body %s; want 200", resp.StatusCode, b)
	}
	clicks, err := s.ClicksSince(app.ID, time.Now().Add(-time.Hour))
	if err != nil || len(clicks) != 1 {
		t.Fatalf("clicks = %v, %v; want 1", clicks, err)
	}
	if clicks[0].UASuspect || clicks[0].Kind != store.KindOpen {
		t.Errorf("click = suspect %v kind %q; want false, %q", clicks[0].UASuspect, clicks[0].Kind, store.KindOpen)
	}
}

// installRows: every install row for app as "attribution|fraud|fraud_action|fraud_link_id".
func installRows(t *testing.T, path, appID string) []string {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()
	rows, err := db.Query(`SELECT attribution || '|' || COALESCE(fraud, '') || '|' || COALESCE(fraud_action, '') || '|' || COALESCE(fraud_link_id, '') FROM installs WHERE app_id = ?`, appID)
	if err != nil {
		t.Fatalf("query installs: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var r string
		if err := rows.Scan(&r); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

func activeFraud(t *testing.T, s *store.Store, appID string, mut func(*fraud.Settings)) {
	t.Helper()
	f := fraud.Defaults
	mut(&f)
	if err := s.UpdateFraudSettings(appID, f); err != nil {
		t.Fatalf("UpdateFraudSettings: %v", err)
	}
}

// AE5 end to end: active-flagged clickId -> 404, one organic install carrying labels.
func TestMatchLinkFlaggedClickIDOrganic(t *testing.T) {
	ts, s, path := newTestServer(t)
	app, link := setup(t, s)
	activeFraud(t, s, app.ID, func(f *fraud.Settings) { f.UserAgentMode = fraud.ModeActive })
	c, err := s.RecordClick(store.Click{AppID: app.ID, LinkID: link.ID, Destination: link.URL, ClickID: "play-bot", UASuspect: true,
		Fingerprint: store.Fingerprint{IP: testIP, UserAgent: "curl/8.0"}}, 24)
	if err != nil {
		t.Fatal(err)
	}
	resp, b := doPost(t, ts, "/api/link/match-link", `{"clickId":"play-bot"}`, authHeaders(app.ID))
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, body %s; want 404", resp.StatusCode, b)
	}
	// clickId is fresh: short timing fires too
	want := "organic|timing,user_agent|excluded|" + link.ID
	if got := installRows(t, path, app.ID); len(got) != 1 || got[0] != want {
		t.Errorf("installs = %v; want [%s]", got, want)
	}
	if _, err := s.ClickByClickID(app.ID, c.ClickID, ""); err != nil {
		t.Errorf("excluded click consumed: %v", err)
	}
}

// Same-device retry after a re-attributed match: PriorMatch returns the same click, labels kept, one row.
func TestMatchLinkRetryAfterReattributed(t *testing.T) {
	ts, s, path := newTestServer(t)
	app, link := setup(t, s)
	activeFraud(t, s, app.ID, func(f *fraud.Settings) { f.VelocityMode = fraud.ModeActive })
	clean, err := s.RecordClick(store.Click{AppID: app.ID, LinkID: link.ID, Destination: "https://example.com/clean",
		Fingerprint: store.Fingerprint{IP: otherIP, Device: "Pixel 7", Locale: "en", Timezone: "Europe/Warsaw", Screen: "393x852@3",
			UserAgent: "Mozilla/5.0 (Linux; Android 14; Pixel 7 Build/TQ3A.230805.001)"}}, 24)
	if err != nil {
		t.Fatal(err)
	}
	flagged := recordAndroidClick(t, s, app, link) // newer: wins the 950 tie (TRUST_PROXY off, no IP match), so it is the excluded overall best
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano)
	if _, err := db.Exec(`UPDATE clicks SET first_seen_at = ?`, old); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE clicks SET hits_ip = 50 WHERE id = ?`, flagged.ID); err != nil {
		t.Fatal(err)
	}
	db.Close()
	hdr := authHeaders(app.ID)
	for i := range 2 {
		resp, b := doPost(t, ts, "/api/link/match-link", androidFingerprintJSON(), hdr)
		if resp.StatusCode != http.StatusOK || !strings.Contains(string(b), clean.Destination) {
			t.Fatalf("attempt %d: status %d body %s; want clean destination", i, resp.StatusCode, b)
		}
	}
	want := "non_organic|velocity|reattributed|" + link.ID
	if got := installRows(t, path, app.ID); len(got) != 1 || got[0] != want {
		t.Errorf("installs = %v; want [%s]", got, want)
	}
}

// Same-device retry after an excluded match: PriorMatch misses (organic), matching re-runs to organic again, click unconsumed, one row.
func TestMatchLinkRetryAfterExcluded(t *testing.T) {
	ts, s, path := newTestServer(t)
	app, link := setup(t, s)
	activeFraud(t, s, app.ID, func(f *fraud.Settings) { f.VelocityMode = fraud.ModeActive })
	flagged := recordAndroidClick(t, s, app, link)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE clicks SET hits_ip = 50, first_seen_at = ? WHERE id = ?`, time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano), flagged.ID); err != nil {
		t.Fatal(err)
	}
	db.Close()
	hdr := authHeaders(app.ID)
	for i := range 2 {
		if resp, b := doPost(t, ts, "/api/link/match-link", androidFingerprintJSON(), hdr); resp.StatusCode != http.StatusNotFound {
			t.Fatalf("attempt %d: status %d body %s; want 404", i, resp.StatusCode, b)
		}
	}
	want := "organic|velocity|excluded|" + link.ID
	if got := installRows(t, path, app.ID); len(got) != 1 || got[0] != want {
		t.Errorf("installs = %v; want [%s]", got, want)
	}
	if _, err := s.ClickByClickID(app.ID, flagged.ClickID, ""); err != nil {
		t.Errorf("excluded click consumed: %v", err)
	}
}

// execSQL: raw statement via second connection (WAL allows it).
func execSQL(t *testing.T, path, q string, args ...any) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatalf("exec %q: %v", q, err)
	}
}

// Fingerprint concentration end to end, FingerprintMax 5: one device configuration installs five times, each from a real click whose
// retention then lapses (so PriorMatch no longer answers). Installs 1-4 are clean; the 5th (4 priors on the link) is labeled, still attributed.
func TestMatchLinkFingerprintConcentration(t *testing.T) {
	ts, s, path := newTestServer(t)
	app, link := setup(t, s)
	activeFraud(t, s, app.ID, func(f *fraud.Settings) { f.FingerprintMax = 5 })
	for round := 1; round <= 5; round++ {
		c := recordAndroidClick(t, s, app, link)
		execSQL(t, path, `UPDATE clicks SET first_seen_at = ? WHERE id = ?`, time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano), c.ID)
		resp, b := doPost(t, ts, "/api/link/match-link", androidFingerprintJSON(), authHeaders(app.ID))
		if resp.StatusCode != http.StatusOK || !strings.Contains(string(b), link.URL) {
			t.Fatalf("round %d: status %d body %s; want 200 with link", round, resp.StatusCode, b)
		}
		execSQL(t, path, `UPDATE clicks SET expires_at = ? WHERE id = ?`, time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano), c.ID)

		rows := installRows(t, path, app.ID)
		labeled := 0
		for _, r := range rows {
			switch r {
			case "non_organic|fingerprint||":
				labeled++
			case "non_organic|||":
			default:
				t.Errorf("round %d: install row %q; want non_organic, clean or fingerprint", round, r)
			}
		}
		want := 0
		if round == 5 {
			want = 1
		}
		if len(rows) != round || labeled != want {
			t.Errorf("round %d: %d installs, %d labeled; want %d, %d", round, len(rows), labeled, round, want)
		}
	}
}

// Universal-link open from a hosting range records ip_hosting (KTD3: IP still judged on opens); a documentation-range IP does not.
func TestUniversalLinkOpenHostingIP(t *testing.T) {
	httpx.TrustProxy = true
	t.Cleanup(func() { httpx.TrustProxy = false })
	ts, s, _ := newTestServer(t)
	app, _ := setup(t, s)
	for _, ip := range []string{"3.5.140.1", testIP} {
		hdr := authHeaders(app.ID)
		hdr["X-Forwarded-For"] = ip
		if resp, b := doPost(t, ts, "/api/link/universal-link-click", `{"url":"https://lnk.example/abc"}`, hdr); resp.StatusCode != http.StatusOK {
			t.Fatalf("%s: status = %d, body %s; want 200", ip, resp.StatusCode, b)
		}
	}
	clicks, err := s.ClicksSince(app.ID, time.Now().Add(-time.Hour))
	if err != nil || len(clicks) != 2 {
		t.Fatalf("clicks = %v, %v; want 2", clicks, err)
	}
	got := map[string]bool{}
	for _, c := range clicks {
		got[c.Fingerprint.IP] = c.IPHosting
	}
	if !got["3.5.140.1"] || got[testIP] {
		t.Errorf("ip_hosting by IP = %v; want 3.5.140.1 true, %s false", got, testIP)
	}
}

// Hit counter table gone: universal-link-click still answers the allow shape with a clickId (fail-open); no click is recorded.
func TestUniversalLinkClickFailOpenOnHitsError(t *testing.T) {
	ts, s, path := newTestServer(t)
	app, _ := setup(t, s)
	dropTable(t, path, "click_hits")
	resp, b := doPost(t, ts, "/api/link/universal-link-click", `{"url":"https://lnk.example/abc"}`, authHeaders(app.ID))
	var out struct {
		Allowed bool   `json:"allowed"`
		ClickID string `json:"clickId"`
	}
	if resp.StatusCode != http.StatusOK || json.Unmarshal(b, &out) != nil || !out.Allowed || out.ClickID == "" {
		t.Fatalf("status %d body %s; want 200 allowed with a clickId", resp.StatusCode, b)
	}
	if n, err := s.CountClicks(app.ID); err != nil || n != 0 {
		t.Errorf("clicks = %d, %v; want 0", n, err)
	}
}

// Event with data.link (short URL) tags the device and counts; a retention call one day after the cohort counts D1, never a conversion.
func TestAnalyticsLinkTagging(t *testing.T) {
	ts, s, path := newTestServer(t)
	app, _ := setup(t, s)
	post := func(endpoint, body string) {
		t.Helper()
		if resp, b := doPost(t, ts, endpoint, body, authHeaders(app.ID)); resp.StatusCode != http.StatusOK {
			t.Fatalf("%s: %d %s", endpoint, resp.StatusCode, b)
		}
	}
	post("/api/analytics/event", `{"event_name":"signup","data":{"link":"https://lnk.example/abc"},"device_id":"dev-1"}`)
	post("/api/analytics/event", `{"event_name":"purchase","data":{"value":3},"device_id":"dev-1"}`)
	post("/api/analytics/event", `{"event_name":"purchase","data":{"link":"abc"}}`) // no device: not counted

	// handlers use time.Now(): move the cohort back one day, mapping and rollup key together
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	yesterday := time.Now().UTC().AddDate(0, 0, -1)
	if _, err := db.Exec(`UPDATE device_links SET first_seen = ?`, yesterday.Format("2006-01-02T15:04:05.000Z")); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE link_cohorts SET day = ?`, yesterday.Format("2006-01-02")); err != nil {
		t.Fatal(err)
	}
	post("/api/analytics/retention", `{"event_name":"app_open","platform":"ios","device_id":"dev-1"}`)

	a, err := s.Analytics(app.ID, 7, "", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	conv := map[string]int64{}
	for _, c := range a.Conversions {
		conv[c.Key+"/"+c.Event] = c.Count
	}
	if len(conv) != 2 || conv["abc/signup"] != 1 || conv["abc/purchase"] != 1 {
		t.Errorf("conversions = %v; want abc signup 1, purchase 1", conv)
	}
	if len(a.Retention) != 1 || a.Retention[0].Devices != 1 || a.Retention[0].D1 != (store.Mark{Returned: 1, Devices: 1}) {
		t.Errorf("retention = %+v; want abc 1 device, d1 1/1", a.Retention)
	}
}

// Link rollups are best-effort: a broken device_links table never fails the SDK call or loses the raw event.
func TestAnalyticsLinkRollupFailureStillOK(t *testing.T) {
	ts, s, path := newTestServer(t)
	app, _ := setup(t, s)
	dropTable(t, path, "device_links")
	for _, ep := range []string{"/api/analytics/event", "/api/analytics/retention"} {
		resp, b := doPost(t, ts, ep, `{"event_name":"app_open","data":{"link":"abc"},"device_id":"dev-1"}`, authHeaders(app.ID))
		if resp.StatusCode != http.StatusOK || !strings.Contains(string(b), `"success":true`) {
			t.Errorf("%s with broken rollup: %d %s; want 200 success", ep, resp.StatusCode, b)
		}
	}
	if n, err := s.CountEvents(app.ID); err != nil || n != 2 {
		t.Errorf("events = %d (%v); want 2 raw events kept", n, err)
	}
}

// SDK match-link call matches a click tagged with Variant B, stores install with Variant B, and returns Variant B's deep link destination.
func TestMatchLinkVariantAttribution(t *testing.T) {
	ts, s, path := newTestServer(t)
	app, link := setup(t, s)

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Seed click with variant and custom destination
	c, err := s.RecordClick(store.Click{
		AppID:       app.ID,
		LinkID:      link.ID,
		Variant:     "variant-b",
		Destination: "myapp://promo/variant-b",
		Fingerprint: store.Fingerprint{
			IP:        testIP,
			UserAgent: "Mozilla/5.0 (iPhone; CPU iPhone OS 16_0 like Mac OS X)",
			Locale:    "en_US",
			Timezone:  "America/New_York",
			Screen:    "390x844@3",
		},
	}, 24)
	if err != nil {
		t.Fatalf("RecordClick: %v", err)
	}

	body := `{"clickId":"` + c.ID + `"}`
	resp, respBody := doPost(t, ts, "/api/link/match-link", body, authHeaders(app.ID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("match-link status = %d, body = %s", resp.StatusCode, respBody)
	}

	var res map[string]string
	if err := json.Unmarshal(respBody, &res); err != nil {
		t.Fatalf("unmarshal match-link response: %v", err)
	}
	if res["link"] != "myapp://promo/variant-b" {
		t.Errorf("expected link destination %q, got %q", "myapp://promo/variant-b", res["link"])
	}

	// Verify install recorded with variant-b
	var instVariant string
	err = db.QueryRow(`SELECT COALESCE(variant, '') FROM installs WHERE app_id = ? AND click_id = ?`, app.ID, c.ID).Scan(&instVariant)
	if err != nil {
		t.Fatalf("query install variant: %v", err)
	}
	if instVariant != "variant-b" {
		t.Errorf("expected install variant 'variant-b', got %q", instVariant)
	}

	// Verify variant_days rollup
	stats, err := s.GetLinkVariantStats(app.ID, link.ID, time.Now().AddDate(0, 0, -1), time.Now().AddDate(0, 0, 1))
	if err != nil {
		t.Fatalf("GetLinkVariantStats: %v", err)
	}
	if len(stats) != 1 || stats[0].Variant != "variant-b" || stats[0].Clicks != 1 || stats[0].Installs != 1 {
		t.Fatalf("unexpected variant stats: %+v", stats)
	}
}
