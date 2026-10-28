package pipeline

// Well-known hosting tests. Real store + real mux via httptest; hosts from
// domain set built like main.go (config.DomainSet).

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"detur.dev/server/internal/config"
	"detur.dev/server/internal/store"
)

const primaryDomain = "detur.example.com"

// testDomains: domain set, built like main.go.
func testDomains(t *testing.T) []string {
	t.Helper()
	return config.DomainSet(&config.Config{Domain: primaryDomain})
}

// newWellKnownServer: real store + short-link pipeline + well-known routes on one mux (same as main.go), over httptest
func newWellKnownServer(t *testing.T, domains []string) (*httptest.Server, *store.Store) {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/detur-wellknown.db")
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	mux := http.NewServeMux()
	Register(mux, st, 24)
	RegisterWellKnown(mux, st, domains)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts, st
}

// getWithHost: GET path with fixed Host, no redirect following (short-link tests assert 302 itself)
func getWithHost(t *testing.T, ts *httptest.Server, path, host string) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, ts.URL+path, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Host = host
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET %s (host %s): %v", path, host, err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp, b
}

// aasaResp: AASA shape.
type aasaResp struct {
	AppLinks struct {
		Apps    []string `json:"apps"`
		Details []struct {
			AppID      string              `json:"appID"`
			Paths      []string            `json:"paths"`
			AppIDs     []string            `json:"appIDs"`
			Components []map[string]string `json:"components"`
		} `json:"details"`
	} `json:"applinks"`
}

// assetlinksResp: one assetlinks.json entry.
type assetlinksResp []struct {
	Relation []string `json:"relation"`
	Target   struct {
		Namespace             string   `json:"namespace"`
		PackageName           string   `json:"package_name"`
		SHA256CertFingerprint []string `json:"sha256_cert_fingerprints"`
	} `json:"target"`
}

// setupWellKnownApp: app with iOS + Android well-known details.
func setupWellKnownApp(t *testing.T, s *store.Store) store.App {
	t.Helper()
	app, err := s.CreateApp("well-known app", "sekrit-key-123")
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	if err := s.UpdateAppDetails(app.ID, "TEAM123.com.example.app", "com.example.app", "AA:BB:CC:DD:EE:FF:00:11:22:33:44:55:66:77:88:99:AA:BB:CC:DD:EE:FF:00:11:22:33:44:55:66:77:88:99", ""); err != nil {
		t.Fatalf("UpdateAppDetails: %v", err)
	}
	return app
}

// Scenario 1: AASA valid JSON per app. Several iOS apps: each claims only own link keys (iOS routes URL to first matching entry; blanket ["*"] per app shadows all but first).
func TestAASAValidJSONPerApp(t *testing.T) {
	ts, st := newWellKnownServer(t, testDomains(t))
	iosOnly, err := st.CreateApp("ios only", "key-1")
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	if err := st.UpdateAppDetails(iosOnly.ID, "TEAM1.com.example.ios", "", "", ""); err != nil {
		t.Fatalf("UpdateAppDetails: %v", err)
	}
	if _, err := st.CreateLink(store.Link{AppID: iosOnly.ID, Key: "abc", URL: "https://example.com/abc"}); err != nil {
		t.Fatalf("CreateLink: %v", err)
	}
	sibling := setupWellKnownApp(t, st)
	if _, err := st.CreateLink(store.Link{AppID: sibling.ID, Key: "second", URL: "https://example.com/second"}); err != nil {
		t.Fatalf("CreateLink: %v", err)
	}
	zeroLink, err := st.CreateApp("zero link ios", "key-2")
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	if err := st.UpdateAppDetails(zeroLink.ID, "TEAM9.com.example.zero", "", "", ""); err != nil {
		t.Fatalf("UpdateAppDetails: %v", err)
	}

	// Multi-app: each app claims only own link keys, zero-link apps omitted, new keys appear on refetch.
	checkAASA := func(team1Want []string) {
		resp, b := getWithHost(t, ts, "/.well-known/apple-app-site-association", primaryDomain)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, body %s; want 200", resp.StatusCode, b)
		}
		if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q; want application/json", ct)
		}
		var aasa aasaResp
		if err := json.Unmarshal(b, &aasa); err != nil {
			t.Fatalf("invalid JSON: %v\n%s", err, b)
		}
		if len(aasa.AppLinks.Apps) != 0 {
			t.Errorf("apps = %v; want empty array", aasa.AppLinks.Apps)
		}
		want := map[string][]string{
			"TEAM1.com.example.ios":   team1Want,
			"TEAM123.com.example.app": {"/second"},
		}
		if len(aasa.AppLinks.Details) != len(want) {
			t.Fatalf("details = %+v; want %d entries", aasa.AppLinks.Details, len(want))
		}
		for _, d := range aasa.AppLinks.Details {
			wantPaths, ok := want[d.AppID]
			if !ok {
				t.Errorf("unexpected details appID %q", d.AppID)
				continue
			}
			if len(d.Paths) != len(wantPaths) {
				t.Errorf("appID %s paths = %v; want %v", d.AppID, d.Paths, wantPaths)
				continue
			}
			for i, p := range wantPaths {
				if d.Paths[i] != p {
					t.Errorf("appID %s paths = %v; want %v", d.AppID, d.Paths, wantPaths)
					break
				}
			}
			// iOS 13+ format, same claim (appIDs + components mirroring paths, "*" -> "/*")
			if len(d.AppIDs) != 1 || d.AppIDs[0] != d.AppID || len(d.Components) != len(d.Paths) {
				t.Errorf("appID %s appIDs/components = %v/%v; want one entry mirroring paths", d.AppID, d.AppIDs, d.Components)
				continue
			}
			for i, cp := range d.Components {
				wantCP := wantPaths[i]
				if wantCP == "*" {
					wantCP = "/*"
				}
				if cp["/"] != wantCP {
					t.Errorf("appID %s component[%d] = %v; want {\"/\": %q}", d.AppID, i, cp, wantCP)
				}
			}
		}
	}

	checkAASA([]string{"/abc"})

	// New key appears under its owning app on refetch; the sibling's claim is untouched.
	if _, err := st.CreateLink(store.Link{AppID: iosOnly.ID, Key: "later", URL: "https://example.com/later"}); err != nil {
		t.Fatalf("CreateLink: %v", err)
	}
	checkAASA([]string{"/abc", "/later"})
}

// AASA enumeration keeps the newest MaxAASAKeys per app (wellknown.go slice direction).
func TestAASAEnumerationKeepsNewestKeys(t *testing.T) {
	ts, st := newWellKnownServer(t, testDomains(t))

	app, err := st.CreateApp("cap ios", "key-3")
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	if err := st.UpdateAppDetails(app.ID, "TEAM77.com.example.cap", "", "", ""); err != nil {
		t.Fatalf("UpdateAppDetails: %v", err)
	}
	if _, err := st.CreateLink(store.Link{AppID: app.ID, Key: "first", URL: "https://example.com/first"}); err != nil {
		t.Fatalf("CreateLink: %v", err)
	}
	time.Sleep(1100 * time.Millisecond) // strictly later created_at than "first", even at second precision
	for i := 0; i < store.MaxAASAKeys; i++ {
		key := fmt.Sprintf("k%03d", i)
		if _, err := st.CreateLink(store.Link{AppID: app.ID, Key: key, URL: "https://example.com/" + key}); err != nil {
			t.Fatalf("CreateLink %s: %v", key, err)
		}
	}
	setupWellKnownApp(t, st) // second iOS app: switches the domain to per-key enumeration

	resp, b := getWithHost(t, ts, "/.well-known/apple-app-site-association", primaryDomain)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body %s; want 200", resp.StatusCode, b)
	}
	var aasa aasaResp
	if err := json.Unmarshal(b, &aasa); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, b)
	}
	for _, d := range aasa.AppLinks.Details {
		if d.AppID != "TEAM77.com.example.cap" {
			continue
		}
		if len(d.Paths) != store.MaxAASAKeys {
			t.Fatalf("paths = %d; want %d", len(d.Paths), store.MaxAASAKeys)
		}
		served := map[string]bool{}
		for _, p := range d.Paths {
			served[p] = true
		}
		if served["/first"] {
			t.Errorf("oldest key /first served; want dropped by the cap")
		}
		if !served["/k000"] || !served[fmt.Sprintf("/k%03d", store.MaxAASAKeys-1)] {
			t.Errorf("newest keys missing from served paths")
		}
		return
	}
	t.Fatalf("app TEAM77.com.example.cap missing from details: %+v", aasa.AppLinks.Details)
}

// Scenario 1b: AASA wildcard for single iOS app. Wildcard ["*"] ensures newly created links work immediately without Apple CDN cache staleness (R7).
func TestAASASingleAppWildcard(t *testing.T) {
	ts, st := newWellKnownServer(t, testDomains(t))
	app, err := st.CreateApp("single ios app", "key-single")
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	if err := st.UpdateAppDetails(app.ID, "TEAM1.com.example.single", "", "", ""); err != nil {
		t.Fatalf("UpdateAppDetails: %v", err)
	}

	resp, b := getWithHost(t, ts, "/.well-known/apple-app-site-association", primaryDomain)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body %s; want 200", resp.StatusCode, b)
	}
	var aasa aasaResp
	if err := json.Unmarshal(b, &aasa); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(aasa.AppLinks.Details) != 1 {
		t.Fatalf("details length = %d; want 1", len(aasa.AppLinks.Details))
	}
	d := aasa.AppLinks.Details[0]
	if d.AppID != "TEAM1.com.example.single" {
		t.Errorf("appID = %q; want TEAM1.com.example.single", d.AppID)
	}
	if len(d.Paths) != 1 || d.Paths[0] != "*" {
		t.Errorf("paths = %v; want [*]", d.Paths)
	}
	if len(d.Components) != 1 || d.Components[0]["/"] != "/*" {
		t.Errorf("components = %v; want [{\"/\": \"/*\"}]", d.Components)
	}

	// Verify that creating a new link in the DB does not alter AASA file (prevents Apple CDN cache invalidation need)
	if _, err := st.CreateLink(store.Link{AppID: app.ID, Key: "newkey123", URL: "https://example.com/new"}); err != nil {
		t.Fatalf("CreateLink: %v", err)
	}
	resp2, b2 := getWithHost(t, ts, "/.well-known/apple-app-site-association", primaryDomain)
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body %s; want 200", resp2.StatusCode, b2)
	}
	var aasa2 aasaResp
	if err := json.Unmarshal(b2, &aasa2); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	d2 := aasa2.AppLinks.Details[0]
	if len(d2.Paths) != 1 || d2.Paths[0] != "*" {
		t.Errorf("paths after link creation = %v; want [*]", d2.Paths)
	}
	if len(d2.Components) != 1 || d2.Components[0]["/"] != "/*" {
		t.Errorf("components after link creation = %v; want [{\"/\": \"/*\"}]", d2.Components)
	}
}

// Scenario 2: assetlinks.json, one entry per app with android package AND cert fingerprint; ios-only apps absent
func TestAssetlinksValidJSONPerApp(t *testing.T) {
	ts, st := newWellKnownServer(t, testDomains(t))
	iosOnly, err := st.CreateApp("ios only", "key-1")
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	if err := st.UpdateAppDetails(iosOnly.ID, "TEAM1.com.example.ios", "", "", ""); err != nil {
		t.Fatalf("UpdateAppDetails: %v", err)
	}
	setupWellKnownApp(t, st)

	resp, b := getWithHost(t, ts, "/.well-known/assetlinks.json", primaryDomain)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body %s; want 200", resp.StatusCode, b)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q; want application/json", ct)
	}
	var entries assetlinksResp
	if err := json.Unmarshal(b, &entries); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, b)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %d; want 1 (ios-only app omitted)", len(entries))
	}
	e := entries[0]
	if len(e.Relation) != 1 || e.Relation[0] != "delegate_permission/common.handle_all_urls" {
		t.Errorf("relation = %v; want [delegate_permission/common.handle_all_urls]", e.Relation)
	}
	if e.Target.Namespace != "android_app" {
		t.Errorf("namespace = %q; want android_app", e.Target.Namespace)
	}
	if e.Target.PackageName != "com.example.app" {
		t.Errorf("package_name = %q; want com.example.app", e.Target.PackageName)
	}
	if len(e.Target.SHA256CertFingerprint) != 1 {
		t.Fatalf("sha256_cert_fingerprints = %v; want 1 fingerprint", e.Target.SHA256CertFingerprint)
	}
}

// Scenario 3: apps without details omitted; AASA details empty (apps still []), assetlinks [], both 200 valid JSON
func TestAppsWithoutDetailsOmitted(t *testing.T) {
	ts, st := newWellKnownServer(t, testDomains(t))
	if _, err := st.CreateApp("no details", "key-1"); err != nil {
		t.Fatalf("CreateApp: %v", err)
	}

	resp, b := getWithHost(t, ts, "/.well-known/apple-app-site-association", primaryDomain)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("AASA status = %d, body %s; want 200", resp.StatusCode, b)
	}
	var aasa aasaResp
	if err := json.Unmarshal(b, &aasa); err != nil {
		t.Fatalf("AASA invalid JSON: %v\n%s", err, b)
	}
	if aasa.AppLinks.Apps == nil || aasa.AppLinks.Details == nil ||
		len(aasa.AppLinks.Apps) != 0 || len(aasa.AppLinks.Details) != 0 {
		t.Errorf("AASA = %+v; want empty arrays for apps and details", aasa.AppLinks)
	}

	resp, b = getWithHost(t, ts, "/.well-known/assetlinks.json", primaryDomain)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("assetlinks status = %d, body %s; want 200", resp.StatusCode, b)
	}
	var entries []json.RawMessage
	if err := json.Unmarshal(b, &entries); err != nil {
		t.Fatalf("assetlinks invalid JSON: %v\n%s", err, b)
	}
	if entries == nil {
		t.Errorf("assetlinks body %s; want [] not null", b)
	}
	if len(entries) != 0 {
		t.Errorf("assetlinks entries = %d; want empty array", len(entries))
	}
}

// Scenario 4: host outside domain set -> 404 (host gate).
func TestUnknownDomain404(t *testing.T) {
	ts, st := newWellKnownServer(t, testDomains(t))
	setupWellKnownApp(t, st)
	for _, path := range []string{"/.well-known/apple-app-site-association", "/.well-known/assetlinks.json"} {
		resp, _ := getWithHost(t, ts, path, "evil.example.net")
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s on unknown domain: status = %d; want 404", path, resp.StatusCode)
		}
	}
}

// Scenario 7: UpdateAppDetails persists details; GetApp/ListApps return them; unknown id -> ErrNotFound; empty strings clear
func TestUpdateAppDetailsPersistsGetAppReturnsFields(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/detur-appdetails.db")
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	app, err := st.CreateApp("details", "key-1")
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}

	iosID := "TEAM9.com.example.ios"
	pkg := "com.example.android"
	fp := "AA:BB:CC:DD:EE:FF:00:11:22:33:44:55:66:77:88:99:AA:BB:CC:DD:EE:FF:00:11:22:33:44:55:66:77:88:99"
	if err := st.UpdateAppDetails(app.ID, iosID, pkg, fp, ""); err != nil {
		t.Fatalf("UpdateAppDetails: %v", err)
	}
	got, err := st.GetApp(app.ID)
	if err != nil {
		t.Fatalf("GetApp: %v", err)
	}
	if got.IOSAppID != iosID || got.AndroidPackage != pkg || got.AndroidCertFingerprint != fp {
		t.Errorf("GetApp details = %q/%q/%q; want %q/%q/%q",
			got.IOSAppID, got.AndroidPackage, got.AndroidCertFingerprint, iosID, pkg, fp)
	}
	apps, err := st.ListApps()
	if err != nil {
		t.Fatalf("ListApps: %v", err)
	}
	if len(apps) != 1 || apps[0].IOSAppID != iosID || apps[0].AndroidPackage != pkg {
		t.Errorf("ListApps = %+v; want the persisted details", apps)
	}

	// unknown app -> ErrNotFound, both methods
	if err := st.UpdateAppDetails("nope", "x", "", "", ""); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("UpdateAppDetails(unknown) err = %v; want ErrNotFound", err)
	}
	if _, err := st.GetApp("nope"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("GetApp(unknown) err = %v; want ErrNotFound", err)
	}

	// empty strings clear (nullable columns read back "")
	if err := st.UpdateAppDetails(app.ID, "", "", "", ""); err != nil {
		t.Fatalf("UpdateAppDetails(clear): %v", err)
	}
	got, err = st.GetApp(app.ID)
	if err != nil {
		t.Fatalf("GetApp after clear: %v", err)
	}
	if got.IOSAppID != "" || got.AndroidPackage != "" || got.AndroidCertFingerprint != "" {
		t.Errorf("GetApp after clear = %q/%q/%q; want empty", got.IOSAppID, got.AndroidPackage, got.AndroidCertFingerprint)
	}
}

// Scenario 8: AASA + assetlinks + short link on same domain, same mux
func TestIntegrationWellKnownAndShortLinksOnConfiguredDomain(t *testing.T) {
	ts, st := newWellKnownServer(t, testDomains(t))
	setupWellKnownApp(t, st)
	app, err := st.CreateApp("link app", "sekrit-key-123")
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	link, err := st.CreateLink(store.Link{
		AppID: app.ID, Key: "abc", URL: "https://example.com/product",
		FallbackURL: "https://example.com/landing",
	})
	if err != nil {
		t.Fatalf("CreateLink: %v", err)
	}

	for _, path := range []string{"/.well-known/apple-app-site-association", "/.well-known/assetlinks.json"} {
		resp, b := getWithHost(t, ts, path, primaryDomain)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s status = %d, body %s; want 200", path, resp.StatusCode, b)
		}
		if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("%s Content-Type = %q; want application/json", path, ct)
		}
	}

	resp, _ := getWithHost(t, ts, "/"+link.Key, primaryDomain)
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("short link status = %d; want 302", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != link.FallbackURL {
		t.Errorf("Location = %q; want fallback %q", loc, link.FallbackURL)
	}
}
