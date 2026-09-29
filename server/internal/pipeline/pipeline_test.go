package pipeline

// Test scenarios 1-10 for browser pipeline; real store + real mux via httptest, no store-layer mocks.

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite" // SQLite driver: tests drop tables to inject backend errors

	"detur.dev/server/internal/api"
	"detur.dev/server/internal/httpx"
	"detur.dev/server/internal/store"
)

const (
	testAPIKey = "sekrit-key-123"
	testIP     = "203.0.113.7"
)

const (
	iosUA     = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_2 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.2 Mobile/15E148 Safari/604.1"
	androidUA = "Mozilla/5.0 (Linux; Android 14; Pixel 7 Build/TQ3A.230805.001) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Mobile Safari/537.36"
	desktopUA = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36"
)

// newPipelineServer wires real store + browser pipeline + SDK routes on one mux (SDK routes more specific; GET /{key} serves rest); integration test (scenario 10) exercises both halves
func newPipelineServer(t *testing.T) (*httptest.Server, *store.Store, string) {
	t.Helper()
	path := t.TempDir() + "/detur-pipeline.db"
	st, err := store.Open(path)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	mux := http.NewServeMux()
	Register(mux, st, 24)
	api.RegisterSDK(mux, st, 24)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts, st, path
}

func setupPipeline(t *testing.T, s *store.Store) (store.App, store.Link) {
	t.Helper()
	app, err := s.CreateApp("pipeline test app", testAPIKey)
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	link, err := s.CreateLink(store.Link{
		AppID: app.ID, Key: "abc", URL: "https://example.com/product",
		IOS:         "https://apps.apple.com/app/id123",
		Android:     "https://play.google.com/store/apps/details?id=com.example",
		FallbackURL: "https://example.com/web/landing",
	})
	if err != nil {
		t.Fatalf("CreateLink: %v", err)
	}
	return app, link
}

// doGET issues GET without following redirects (asserts on 302 + Location)
func doGET(t *testing.T, ts *httptest.Server, path string, hdr map[string]string) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, ts.URL+path, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp, b
}

// mobileClick: one-hop interstitial — first GET serves screen/timezone page (200, no click); reload with _dt=1 records click, returns 302
func mobileClick(t *testing.T, ts *httptest.Server, path string, hdr map[string]string) (*http.Response, []byte) {
	t.Helper()
	first, _ := doGET(t, ts, path, hdr)
	if first.StatusCode != http.StatusOK {
		t.Fatalf("interstitial status = %d; want 200 (mobile click first hop)", first.StatusCode)
	}
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	return doGET(t, ts, path+sep+paramDone+"=1", hdr)
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

// dropTable deletes table through second connection (WAL allows it), injecting backend failure, rest of store intact
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

func latestClicks(t *testing.T, s *store.Store, appID string) []store.Click {
	t.Helper()
	clicks, err := s.ClicksSince(appID, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("ClicksSince: %v", err)
	}
	return clicks
}

// Scenario 1: iOS click -> 302 to App Store URL + click row recorded with fingerprint
func TestIOSClickRedirectsAppStoreAndRecordsClick(t *testing.T) {
	ts, s, _ := newPipelineServer(t)
	app, link := setupPipeline(t, s)
	screen := url.Values{"screen": {"393x852@3"}}.Encode()
	resp, _ := mobileClick(t, ts, "/"+link.Key+"?"+screen, map[string]string{
		"User-Agent":      iosUA,
		"Accept-Language": "en-US,en;q=0.9",
		"X-Forwarded-For": testIP,
	})
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d; want 302", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != link.IOS {
		t.Fatalf("Location = %q; want %q (App Store)", loc, link.IOS)
	}
	clicks := latestClicks(t, s, app.ID)
	if len(clicks) != 1 {
		t.Fatalf("clicks = %d; want 1 recorded", len(clicks))
	}
	if clicks[0].Destination != link.IOS || clicks[0].LinkID != link.ID {
		t.Errorf("click destination/link = %q/%q; want %q/%q (the redirect target)", clicks[0].Destination, clicks[0].LinkID, link.IOS, link.ID)
	}
	if clicks[0].Fingerprint.Screen != "393x852@3" {
		t.Errorf("click screen = %q; want 393x852@3", clicks[0].Fingerprint.Screen)
	}
}

// Scenario 2: Android click -> 302 to Play URL, referrer param carries recorded clickId (Play install referrer -> match-link)
func TestAndroidClickRedirectsPlayWithClickIDReferrer(t *testing.T) {
	ts, s, _ := newPipelineServer(t)
	app, link := setupPipeline(t, s)
	resp, _ := mobileClick(t, ts, "/"+link.Key, map[string]string{
		"User-Agent":      androidUA,
		"X-Forwarded-For": testIP,
	})
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d; want 302", resp.StatusCode)
	}
	u, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("Location %q: %v", resp.Header.Get("Location"), err)
	}
	if u.Host != "play.google.com" {
		t.Fatalf("Location = %q; want Play host", resp.Header.Get("Location"))
	}
	ref := u.Query().Get("referrer")
	if ref == "" {
		t.Fatalf("referrer param missing in %q", u)
	}
	clicks := latestClicks(t, s, app.ID)
	if len(clicks) != 1 {
		t.Fatalf("clicks = %d; want 1 recorded", len(clicks))
	}
	// Parse nested Play referrer as query string — checks server's output contract, not SDK's parser (not in this repository)
	values, err := url.ParseQuery(ref)
	if err != nil || values.Get("click_id") != clicks[0].ID {
		t.Fatalf("referrer %q has click_id %q (%v); want %s", ref, values.Get("click_id"), err, clicks[0].ID)
	}
	if got, err := s.ClickByClickID(app.ID, clicks[0].ID); err != nil || got.ID != clicks[0].ID {
		t.Fatalf("ClickByClickID(%s) = %+v, %v; want the recorded click", clicks[0].ID, got, err)
	}
}

// Scenario 2b: deterministic chain end-to-end: browser click (Android UA) -> referrer click_id -> match-link {clickId} -> 200 {link} + non-organic install (verifies server-side referrer contract)
func TestAndroidDeterministicChainEndToEnd(t *testing.T) {
	ts, s, _ := newPipelineServer(t)
	app, link := setupPipeline(t, s)
	resp, _ := mobileClick(t, ts, "/"+link.Key, map[string]string{"User-Agent": androidUA})
	loc, _ := url.Parse(resp.Header.Get("Location"))
	values, err := url.ParseQuery(loc.Query().Get("referrer"))
	if err != nil || values.Get("click_id") == "" {
		t.Fatalf("referrer has no parseable click_id: %v", err)
	}
	// match-link with extracted clickId (SDK headers + payload shape)
	body := fmt.Sprintf(`{"clickId":%q}`, values.Get("click_id"))
	req, _ := http.NewRequest("POST", ts.URL+"/api/link/match-link", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testAPIKey)
	req.Header.Set("X-App-ID", app.ID)
	req.Header.Set("X-SDK", "react-native/2.3.1")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("match-link: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(res.Body)
		t.Fatalf("match-link status = %d body=%s; want 200", res.StatusCode, b)
	}
	var out struct {
		Link string `json:"link"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatalf("match-link body: %v", err)
	}
	// Click recorded actual redirect target: Play URL carrying same clickId SDK just used
	ul, err := url.Parse(out.Link)
	if err != nil || ul.Host != "play.google.com" {
		t.Fatalf("match-link link = %q (%v); want Play host", out.Link, err)
	}
	ref, _ := url.ParseQuery(ul.Query().Get("referrer"))
	if ref.Get("click_id") != values.Get("click_id") {
		t.Fatalf("returned link referrer click_id = %q; want %q", ref.Get("click_id"), values.Get("click_id"))
	}
	organic, nonOrganic, err := s.CountInstalls(app.ID)
	if err != nil || organic != 0 || nonOrganic != 1 {
		t.Fatalf("installs = organic %d non-organic %d (%v); want 0/1", organic, nonOrganic, err)
	}
}

// Scenario 3: desktop click -> 302 to fallback URL
func TestDesktopClickRedirectsFallback(t *testing.T) {
	ts, s, _ := newPipelineServer(t)
	_, link := setupPipeline(t, s)
	resp, _ := doGET(t, ts, "/"+link.Key, map[string]string{"User-Agent": desktopUA})
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d; want 302", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != link.FallbackURL {
		t.Fatalf("Location = %q; want fallback %q", loc, link.FallbackURL)
	}
}

// Scenario 4: click-time fingerprint persisted verbatim: IP, UA-derived device, locale, user-agent, readable via store.GetClick
func TestClickFingerprintPersisted(t *testing.T) {
	httpx.TrustProxy = true // XFF header stands in for trusted-proxy deployment
	t.Cleanup(func() { httpx.TrustProxy = false })
	ts, s, _ := newPipelineServer(t)
	app, link := setupPipeline(t, s)
	mobileClick(t, ts, "/"+link.Key, map[string]string{
		"User-Agent":      iosUA,
		"Accept-Language": "fr-FR,fr;q=0.9,en;q=0.8",
		"X-Forwarded-For": testIP,
	})
	clicks := latestClicks(t, s, app.ID)
	if len(clicks) != 1 {
		t.Fatalf("clicks = %d; want 1", len(clicks))
	}
	got, err := s.GetClick(clicks[0].ID)
	if err != nil {
		t.Fatalf("GetClick: %v", err)
	}
	fp := got.Fingerprint
	if fp.IP != testIP {
		t.Errorf("IP = %q; want %q", fp.IP, testIP)
	}
	if fp.Device != "iPhone" {
		t.Errorf("device = %q; want iPhone (UA-derived)", fp.Device)
	}
	if fp.Locale != "fr-FR" {
		t.Errorf("locale = %q; want fr-FR (first Accept-Language tag)", fp.Locale)
	}
	if fp.UserAgent != iosUA {
		t.Errorf("user_agent = %q; want request UA", fp.UserAgent)
	}
}

// Scenario 5: bot UA -> no click row, redirect still happens (bot filter).
func TestBotUASkipsRecordingStillRedirects(t *testing.T) {
	ts, s, _ := newPipelineServer(t)
	_, link := setupPipeline(t, s)
	resp, _ := doGET(t, ts, "/"+link.Key, map[string]string{
		"User-Agent": "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)",
	})
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d; want 302 (bot still redirected)", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc == "" {
		t.Fatal("bot redirect has empty Location")
	}
	if n, _ := s.CountClicks(link.AppID); n != 0 {
		t.Fatalf("bot click recorded: %d clicks, want 0", n)
	}
}

// Scenario 6: unknown key -> 404 plain text, no crash (server keeps serving).
func TestUnknownKeyReturns404(t *testing.T) {
	ts, _, _ := newPipelineServer(t)
	resp, b := doGET(t, ts, "/definitely-not-a-key", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, body %q; want 404", resp.StatusCode, b)
	}
	if !strings.Contains(string(b), "not found") {
		t.Errorf("body = %q; want plain text not found", b)
	}
}

// Scenario 7: reserved params ppid and dtb pass through to redirect
func TestPpidAndDtbParamsPassThrough(t *testing.T) {
	ts, s, _ := newPipelineServer(t)
	_, link := setupPipeline(t, s)
	resp, _ := doGET(t, ts, "/"+link.Key+"?ppid=ppid-1&dtb=dtb-2", map[string]string{"User-Agent": desktopUA})
	u, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("Location %q: %v", resp.Header.Get("Location"), err)
	}
	q := u.Query()
	if q.Get("ppid") != "ppid-1" {
		t.Errorf("ppid = %q; want ppid-1", q.Get("ppid"))
	}
	if q.Get("dtb") != "dtb-2" {
		t.Errorf("dtb = %q; want dtb-2", q.Get("dtb"))
	}
}

// Scenario 8: link without ios/android overrides falls through to link.URL on mobile; missing ios -> URL; clickId referrer merged only into Play Store targets (Dub get-final-url.ts)
func TestMobileWithoutOverridesFallsThroughToURL(t *testing.T) {
	ts, s, _ := newPipelineServer(t)
	app, _ := setupPipeline(t, s)
	plain, err := s.CreateLink(store.Link{AppID: app.ID, Key: "plain", URL: "https://example.com/direct"})
	if err != nil {
		t.Fatalf("CreateLink: %v", err)
	}
	resp, _ := mobileClick(t, ts, "/plain", map[string]string{"User-Agent": iosUA})
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("iOS: status = %d; want 302", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != plain.URL {
		t.Errorf("iOS: Location = %q; want %q (falls through to URL)", loc, plain.URL)
	}
	resp, _ = mobileClick(t, ts, "/plain", map[string]string{"User-Agent": androidUA})
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("Android: status = %d; want 302", resp.StatusCode)
	}
	u, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("Android Location %q: %v", resp.Header.Get("Location"), err)
	}
	if u.Scheme+"://"+u.Host+u.Path != plain.URL {
		t.Errorf("Android: Location = %q; want target %q (falls through to URL)", u, plain.URL)
	}
	if u.Query().Get("referrer") != "" {
		t.Errorf("Android: referrer = %q; want absent (clickId referrer only on Play Store targets)", u.Query().Get("referrer"))
	}
}

// Scenario 9: click-record failure must NOT block redirect; 302 still goes out ("record before redirect"). Injection: drop clicks table; link lookup keeps working, redirect target known
func TestRecordFailureDoesNotBlockRedirect(t *testing.T) {
	ts, s, path := newPipelineServer(t)
	_, link := setupPipeline(t, s)
	dropTable(t, path, "clicks")
	// Interstitial hop works (reads links only); _dt reload hits dropped clicks table, 302 must still go out
	resp, _ := mobileClick(t, ts, "/"+link.Key, map[string]string{"User-Agent": androidUA})
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d; want 302 even when click recording fails", resp.StatusCode)
	}
	u, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("Location %q: %v", resp.Header.Get("Location"), err)
	}
	if u.Host != "play.google.com" {
		t.Fatalf("Location = %q; want Play host", resp.Header.Get("Location"))
	}
	if got := u.Query().Get("referrer"); got != "" {
		t.Errorf("referrer = %q; want empty (no recorded click to carry)", got)
	}
}

// Scenario 10: full chain: browser click via GET /{key}, then match-link call with matching fingerprint (same IP/UA/locale) returns destination, records non-organic install
func TestIntegrationBrowserClickThenMatchLinkNonOrganic(t *testing.T) {
	ts, s, _ := newPipelineServer(t)
	app, link := setupPipeline(t, s)
	mobileClick(t, ts, "/"+link.Key, map[string]string{
		"User-Agent":      androidUA,
		"Accept-Language": "en-US,en;q=0.9",
		"X-Forwarded-For": testIP,
	})
	body := `{"platform":"android","model":"Pixel 7","manufacturer":"Google",` +
		`"systemVersion":"14","screenWidth":393,"screenHeight":852,"scale":3,` +
		`"locale":[{"languageTag":"en-US"}],"timezone":"Europe/Warsaw",` +
		`"userAgent":"` + androidUA + `","timestamp":1720000000000}`
	hdr := map[string]string{
		"Authorization":   "Bearer " + testAPIKey,
		"X-App-ID":        app.ID,
		"X-SDK":           "react-native/2.3.1",
		"X-Forwarded-For": testIP,
		"Content-Type":    "application/json",
	}
	resp, b := doPost(t, ts, "/api/link/match-link", body, hdr)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("match-link status = %d, body %s; want 200", resp.StatusCode, b)
	}
	var out struct {
		Link string `json:"link"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("match-link body %s: %v", b, err)
	}
	ul, err := url.Parse(out.Link)
	if err != nil || ul.Host != "play.google.com" {
		t.Fatalf("match-link link = %q (%v); want the click's redirect target (Play host)", out.Link, err)
	}
	organic, nonOrganic, err := s.CountInstalls(app.ID)
	if err != nil || organic != 0 || nonOrganic != 1 {
		t.Fatalf("installs = organic %d non-organic %d (%v); want 0/1 (F1)", organic, nonOrganic, err)
	}
}
