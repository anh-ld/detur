package store

import (
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"detur.dev/server/internal/fraud"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.TempDir() + "/detur-test.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func setupApp(t *testing.T, s *Store) (App, Link) {
	t.Helper()
	app, err := s.CreateApp("test app", "sekrit-key-123")
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	link, err := s.CreateLink(Link{AppID: app.ID, Key: "abc", URL: "https://example.com/product", IOS: "https://apps.apple.com/app/id123", Android: "https://play.google.com/store/apps/details?id=com.example"})
	if err != nil {
		t.Fatalf("CreateLink: %v", err)
	}
	return app, link
}

func TestCreateAppAndValidateKey(t *testing.T) {
	s := newTestStore(t)
	app, err := s.CreateApp("a", "key-1")
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	if app.ID == "" || app.APIKeyHash == "" {
		t.Fatalf("app fields missing: %+v", app)
	}
	if app.APIKeyHash != HashKey("key-1") {
		t.Errorf("hash mismatch: got %s", app.APIKeyHash)
	}
	ok, err := s.ValidateAPIKey(app.ID, "key-1")
	if err != nil || !ok {
		t.Errorf("ValidateAPIKey(key-1) = %v, %v; want true", ok, err)
	}
	ok, err = s.ValidateAPIKey(app.ID, "wrong")
	if err != nil || ok {
		t.Errorf("ValidateAPIKey(wrong) = %v, %v; want false", ok, err)
	}
	ok, err = s.ValidateAPIKey("nope", "key-1")
	if err != nil || ok {
		t.Errorf("ValidateAPIKey(unknown app) = %v, %v; want false", ok, err)
	}
}

func TestLinkCRUD(t *testing.T) {
	s := newTestStore(t)
	app, _ := setupApp(t, s)

	links, err := s.ListLinks(app.ID)
	if err != nil || len(links) != 1 {
		t.Fatalf("ListLinks = %d links, %v; want 1", len(links), err)
	}

	l, err := s.GetLinkByKey(app.ID, "abc")
	if err != nil || l.URL != "https://example.com/product" {
		t.Fatalf("GetLinkByKey = %+v, %v", l, err)
	}

	l.URL = "https://example.com/new"
	if err := s.UpdateLink(l); err != nil {
		t.Fatalf("UpdateLink: %v", err)
	}
	got, err := s.GetLink(l.ID)
	if err != nil || got.URL != "https://example.com/new" {
		t.Fatalf("updated link = %+v, %v", got, err)
	}

	if err := s.DeleteLink(l.ID); err != nil {
		t.Fatalf("DeleteLink: %v", err)
	}
	if _, err := s.GetLink(l.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetLink after delete: %v; want ErrNotFound", err)
	}
}

func TestRecordClickPersistsFingerprint(t *testing.T) {
	s := newTestStore(t)
	app, link := setupApp(t, s)
	fp := Fingerprint{
		IP: "203.0.113.7", Device: "iPhone 15", Locale: "en-US", Timezone: "Asia/Ho_Chi_Minh",
		Screen: "393x852@3", UserAgent: "Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X)", PastedLink: "https://lnk.example/abc",
	}
	c, err := s.RecordClick(Click{AppID: app.ID, LinkID: link.ID, Fingerprint: fp, Destination: link.URL}, 24)
	if err != nil {
		t.Fatalf("RecordClick: %v", err)
	}
	got, err := s.GetClick(c.ID)
	if err != nil {
		t.Fatalf("GetClick: %v", err)
	}
	if got.Fingerprint != fp {
		t.Errorf("fingerprint mismatch: %+v vs %+v", got.Fingerprint, fp)
	}
	if !got.ExpiresAt.After(got.CreatedAt.Add(23 * time.Hour)) {
		t.Errorf("expiry not at retention floor: created %v expires %v", got.CreatedAt, got.ExpiresAt)
	}
}

func TestClickExpiryIsRetention(t *testing.T) {
	s := newTestStore(t)
	app, link := setupApp(t, s)
	c, err := s.RecordClick(Click{AppID: app.ID, LinkID: link.ID, Destination: link.URL}, 48)
	if err != nil {
		t.Fatalf("RecordClick: %v", err)
	}
	got, _ := s.GetClick(c.ID)
	if d := got.ExpiresAt.Sub(got.CreatedAt) - 48*time.Hour; d < -time.Minute || d > time.Minute {
		t.Errorf("expiry = created + %v; want 48h", got.ExpiresAt.Sub(got.CreatedAt))
	}
}

// RecordClick: reuses previous hour's click for same link + device (IP + user agent), like Dub's click cache; different device records new row.
func TestRecordClickDedupSameDeviceWithinHour(t *testing.T) {
	s := newTestStore(t)
	app, link := setupApp(t, s)
	fp := Fingerprint{IP: "203.0.113.7", UserAgent: "Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X)"}
	first, err := s.RecordClick(Click{AppID: app.ID, LinkID: link.ID, Fingerprint: fp, Destination: link.URL}, 24)
	if err != nil {
		t.Fatalf("RecordClick: %v", err)
	}
	second, err := s.RecordClick(Click{AppID: app.ID, LinkID: link.ID, Fingerprint: fp, Destination: link.URL}, 24)
	if err != nil {
		t.Fatalf("second RecordClick: %v", err)
	}
	if second.ID != first.ID {
		t.Errorf("dedup returned %q; want the first click %q", second.ID, first.ID)
	}
	if n, _ := s.CountClicks(app.ID); n != 1 {
		t.Errorf("clicks = %d; want 1 (deduped)", n)
	}
	// Different device (different UA), same link: new click.
	other := Fingerprint{IP: "203.0.113.7", UserAgent: "Mozilla/5.0 (Linux; Android 14; Pixel 7)"}
	third, err := s.RecordClick(Click{AppID: app.ID, LinkID: link.ID, Fingerprint: other, Destination: link.URL}, 24)
	if err != nil {
		t.Fatalf("third RecordClick: %v", err)
	}
	if third.ID == first.ID {
		t.Error("different device reused the click; want a new row")
	}
	if n, _ := s.CountClicks(app.ID); n != 2 {
		t.Errorf("clicks = %d; want 2 (one per device)", n)
	}
}

// Re-tap within dedup hour refreshes click: same id, created_at = now, new signals kept (Dub rewrites deepLinkClickCache per tap).
func TestRecordClickDedupRefreshesClick(t *testing.T) {
	s := newTestStore(t)
	app, link := setupApp(t, s)
	fp := Fingerprint{IP: "203.0.113.7", UserAgent: "Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X)"}
	first, err := s.RecordClick(Click{AppID: app.ID, LinkID: link.ID, Fingerprint: fp, Destination: link.URL}, 24)
	if err != nil {
		t.Fatalf("RecordClick: %v", err)
	}
	old := time.Now().UTC().Add(-40 * time.Minute)
	if _, err := s.db.Exec(`UPDATE clicks SET created_at = ? WHERE id = ?`, rfc3339(old), first.ID); err != nil {
		t.Fatalf("backdate: %v", err)
	}
	fp.Timezone = "Europe/Warsaw"
	second, err := s.RecordClick(Click{AppID: app.ID, LinkID: link.ID, Fingerprint: fp, Destination: link.URL}, 24)
	if err != nil {
		t.Fatalf("second RecordClick: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("re-tap id = %q; want %q (same click)", second.ID, first.ID)
	}
	if time.Since(second.CreatedAt) > time.Minute {
		t.Errorf("created_at = %v; want refreshed to now", second.CreatedAt)
	}
	if second.Fingerprint.Timezone != "Europe/Warsaw" {
		t.Errorf("timezone = %q; want refreshed signal", second.Fingerprint.Timezone)
	}
	if !second.ExpiresAt.After(first.ExpiresAt.Add(-time.Second)) {
		t.Errorf("expires_at shrank: %v < %v", second.ExpiresAt, first.ExpiresAt)
	}
}

func TestDeterministicLookupByClickID(t *testing.T) {
	s := newTestStore(t)
	app, link := setupApp(t, s)
	c, err := s.RecordClick(Click{AppID: app.ID, LinkID: link.ID, Destination: link.URL, ClickID: "play-click-42"}, 24)
	if err != nil {
		t.Fatalf("RecordClick: %v", err)
	}
	got, err := s.ClickByClickID(app.ID, "play-click-42", "")
	if err != nil || got.ID != c.ID {
		t.Fatalf("ClickByClickID = %+v, %v", got, err)
	}
	if _, err := s.ClickByClickID(app.ID, "missing", ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("ClickByClickID(missing) = %v; want ErrNotFound", err)
	}
}

func TestMarkClickMatchedOnce(t *testing.T) {
	s := newTestStore(t)
	app, link := setupApp(t, s)
	c, err := s.RecordClick(Click{AppID: app.ID, LinkID: link.ID, Destination: link.URL, ClickID: "k1"}, 24)
	if err != nil {
		t.Fatalf("RecordClick: %v", err)
	}
	if ok, err := s.MarkClickMatched(c.ID, ""); !ok || err != nil {
		t.Fatalf("first MarkClickMatched = %v, %v; want true", ok, err)
	}
	if ok, err := s.MarkClickMatched(c.ID, ""); ok || err != nil {
		t.Fatalf("second MarkClickMatched = %v, %v; want false", ok, err)
	}
	if cs, err := s.ClicksSince(app.ID, time.Now().Add(-time.Hour)); err != nil || len(cs) != 0 {
		t.Errorf("ClicksSince = %d, %v; want 0 after match", len(cs), err)
	}
	if _, err := s.ClickByClickID(app.ID, "k1", ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("ClickByClickID after match = %v; want ErrNotFound", err)
	}
}

func TestPriorMatch(t *testing.T) {
	s := newTestStore(t)
	app, link := setupApp(t, s)
	c, err := s.RecordClick(Click{AppID: app.ID, LinkID: link.ID, Destination: link.URL}, 24)
	if err != nil {
		t.Fatalf("RecordClick: %v", err)
	}
	if _, err := s.RecordInstall(Install{AppID: app.ID, DeviceHash: "d1", ClickID: c.ID, Attribution: AttributionNonOrganic}); err != nil {
		t.Fatalf("RecordInstall d1: %v", err)
	}
	if _, err := s.RecordInstall(Install{AppID: app.ID, DeviceHash: "d3", Attribution: AttributionOrganic}); err != nil {
		t.Fatalf("RecordInstall d3: %v", err)
	}
	if got, err := s.PriorMatch(app.ID, "d1"); err != nil || got.ID != c.ID {
		t.Errorf("PriorMatch d1 = %+v, %v; want click %s", got, err, c.ID)
	}
	for _, d := range []string{"d2", "d3"} {
		if _, err := s.PriorMatch(app.ID, d); !errors.Is(err, ErrNotFound) {
			t.Errorf("PriorMatch %s = %v; want ErrNotFound", d, err)
		}
	}
}

func TestPurgeExpired(t *testing.T) {
	s := newTestStore(t)
	app, link := setupApp(t, s)
	c, _ := s.RecordClick(Click{AppID: app.ID, LinkID: link.ID, Destination: link.URL}, 24)
	// force expiry into the past
	if _, err := s.rawExec(`UPDATE clicks SET expires_at = ? WHERE id = ?`, rfc3339(time.Now().Add(-time.Hour)), c.ID); err != nil {
		t.Fatalf("force expiry: %v", err)
	}
	// recent event (kept) + old event past retention floor (purged)
	if err := s.RecordEvent(app.ID, "install", `{}`); err != nil {
		t.Fatalf("RecordEvent: %v", err)
	}
	old := app.ID + "-old"
	if _, err := s.rawExec(`INSERT INTO events (id, app_id, event, metadata, created_at) VALUES (?, ?, 'retention', NULL, ?)`, old, app.ID, rfc3339(time.Now().Add(-48*time.Hour))); err != nil {
		t.Fatalf("insert old event: %v", err)
	}
	n, err := s.PurgeExpired(time.Now(), 24)
	if err != nil {
		t.Fatalf("PurgeExpired: %v", err)
	}
	if n != 2 {
		t.Errorf("PurgeExpired removed %d, want 2 (expired click + old event)", n)
	}
	if _, err := s.GetClick(c.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("click survived purge: %v", err)
	}
}

func TestInstallAttributionIdempotent(t *testing.T) {
	s := newTestStore(t)
	app, _ := setupApp(t, s)
	in, err := s.RecordInstall(Install{AppID: app.ID, DeviceHash: "hash-a", ClickID: "click-1", Attribution: AttributionNonOrganic})
	if err != nil {
		t.Fatalf("RecordInstall: %v", err)
	}
	// duplicate call → same surviving row, no double count
	dup, err := s.RecordInstall(Install{AppID: app.ID, DeviceHash: "hash-a", ClickID: "click-1", Attribution: AttributionNonOrganic})
	if err != nil || dup.ID != in.ID {
		t.Fatalf("duplicate upsert = %+v (%v); want surviving row %s", dup, err, in.ID)
	}
	// organic retry with empty clickId → still one row
	org, err := s.RecordInstall(Install{AppID: app.ID, DeviceHash: "hash-b", ClickID: "", Attribution: AttributionOrganic})
	if err != nil {
		t.Fatalf("RecordInstall organic: %v", err)
	}
	org2, err := s.RecordInstall(Install{AppID: app.ID, DeviceHash: "hash-b", ClickID: "", Attribution: AttributionOrganic})
	if err != nil || org2.ID != org.ID {
		t.Fatalf("organic retry = %+v (%v); want single row", org2, err)
	}
	// unknown attribution stored, excluded from counts
	if _, err := s.RecordInstall(Install{AppID: app.ID, DeviceHash: "hash-c", ClickID: "", Attribution: AttributionUnknown}); err != nil {
		t.Fatalf("RecordInstall unknown: %v", err)
	}
	organic, nonOrganic, err := s.CountInstalls(app.ID)
	if err != nil {
		t.Fatalf("CountInstalls: %v", err)
	}
	if organic != 1 || nonOrganic != 1 {
		t.Errorf("CountInstalls = organic %d non-organic %d; want 1/1 (unknown excluded)", organic, nonOrganic)
	}
}

// backend-error unknown must not block a later real attribution; real ones stay immutable
func TestInstallUnknownUpgradedByRealAttribution(t *testing.T) {
	s := newTestStore(t)
	app, _ := setupApp(t, s)
	rec := func(dev, attr string) {
		t.Helper()
		if _, err := s.RecordInstall(Install{AppID: app.ID, DeviceHash: dev, Attribution: attr}); err != nil {
			t.Fatalf("RecordInstall %s/%s: %v", dev, attr, err)
		}
	}
	rec("d1", AttributionUnknown)
	rec("d1", AttributionOrganic)
	if o, n, _ := s.CountInstalls(app.ID); o != 1 || n != 0 {
		t.Fatalf("unknown→organic: organic %d non-organic %d; want 1/0", o, n)
	}
	rec("d2", AttributionOrganic)
	rec("d2", AttributionUnknown)
	if o, n, _ := s.CountInstalls(app.ID); o != 2 || n != 0 {
		t.Fatalf("organic→unknown: organic %d non-organic %d; want 2/0", o, n)
	}
	// unknown -> real attribution carries the analytics labels
	if _, err := s.RecordInstall(Install{AppID: app.ID, DeviceHash: "d3", ClickID: "c3", Attribution: AttributionUnknown}); err != nil {
		t.Fatalf("RecordInstall unknown: %v", err)
	}
	if _, err := s.RecordInstall(Install{AppID: app.ID, DeviceHash: "d3", ClickID: "c3", Attribution: AttributionNonOrganic, LinkID: "l3", Platform: "ios"}); err != nil {
		t.Fatalf("RecordInstall upgrade: %v", err)
	}
	var linkID, platform string
	s.db.QueryRow(`SELECT link_id, platform FROM installs WHERE device_hash = 'd3'`).Scan(&linkID, &platform)
	if linkID != "l3" || platform != "ios" {
		t.Errorf("upgraded install link/platform = %q/%q; want l3/ios", linkID, platform)
	}
}

func TestOpenMigratesLegacyInstallUniqueness(t *testing.T) {
	path := t.TempDir() + "/legacy.db"
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	_, err = db.Exec(`CREATE TABLE installs (
		id TEXT PRIMARY KEY, app_id TEXT NOT NULL, device_hash TEXT NOT NULL,
		click_id TEXT, attribution TEXT NOT NULL,
		created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
		UNIQUE (device_hash, click_id)
	)`)
	if err == nil {
		_, err = db.Exec(`INSERT INTO installs (id, app_id, device_hash, click_id, attribution) VALUES (?, ?, ?, ?, ?)`,
			"legacy", "app-a", "same-device", "same-click", AttributionNonOrganic)
	}
	closeErr := db.Close()
	if err != nil {
		t.Fatalf("prepare legacy database: %v", err)
	}
	if closeErr != nil {
		t.Fatalf("close legacy database: %v", closeErr)
	}

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open legacy database: %v", err)
	}
	defer s.Close()
	got, err := s.RecordInstall(Install{
		AppID: "app-b", DeviceHash: "same-device", ClickID: "same-click", Attribution: AttributionNonOrganic,
	})
	if err != nil || got.AppID != "app-b" || got.ID == "legacy" {
		t.Fatalf("cross-app install after migration = %+v, %v; want a separate app-b row", got, err)
	}
	// rebuild runs before addMissingColumns: analytics columns survive it
	if !columnExists(s.db, "installs", "link_id") || !columnExists(s.db, "installs", "platform") {
		t.Fatal("installs.link_id/platform missing after legacy rebuild")
	}
	var preserved int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM installs WHERE id = ? AND app_id = ?`, "legacy", "app-a").Scan(&preserved); err != nil || preserved != 1 {
		t.Fatalf("legacy row preserved = %d, %v; want 1", preserved, err)
	}
}

func TestRecordEvent(t *testing.T) {
	s := newTestStore(t)
	app, _ := setupApp(t, s)
	if err := s.RecordEvent(app.ID, "install", `{"source":"play"}`); err != nil {
		t.Fatalf("RecordEvent: %v", err)
	}
}

func TestUpdateAppMatchSettings(t *testing.T) {
	s := newTestStore(t)
	app, _ := setupApp(t, s)
	if app.MatchThreshold != 850 || app.MatchWindowMinutes != 15 {
		t.Fatalf("new app match = %d/%d; want 850/15", app.MatchThreshold, app.MatchWindowMinutes)
	}
	if err := s.UpdateAppMatchSettings(app.ID, 950, 30); err != nil {
		t.Fatalf("UpdateAppMatchSettings: %v", err)
	}
	got, err := s.GetApp(app.ID)
	if err != nil || got.MatchThreshold != 950 || got.MatchWindowMinutes != 30 {
		t.Fatalf("GetApp = %+v, %v; want 950/30", got, err)
	}
	if err := s.UpdateAppMatchSettings("nope", 950, 30); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown app: %v; want ErrNotFound", err)
	}
}

// legacyMatchDB: pre-upgrade database (apps without match columns, instance-wide settings table) with one app.
func legacyMatchDB(t *testing.T, threshold, window string) string {
	t.Helper()
	path := t.TempDir() + "/legacy.db"
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	_, err = db.Exec(`
CREATE TABLE apps (id TEXT PRIMARY KEY, name TEXT NOT NULL, api_key_hash TEXT NOT NULL,
  ios_app_id TEXT, android_package TEXT, android_cert_fingerprint TEXT,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')));
CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);
INSERT INTO apps (id, name, api_key_hash) VALUES ('legacy-app', 'old', 'h');
INSERT INTO settings (key, value) VALUES ('threshold', '` + threshold + `'), ('window_minutes', '` + window + `');`)
	closeErr := db.Close()
	if err != nil {
		t.Fatalf("prepare legacy database: %v", err)
	}
	if closeErr != nil {
		t.Fatalf("close legacy database: %v", closeErr)
	}
	return path
}

func TestOpenSeedsAppMatchFromLegacySettings(t *testing.T) {
	for _, tc := range []struct {
		threshold, window string
		wantT, wantW      int
	}{
		{"950", "30", 950, 30},
		{"9999", "30", 850, 30}, // out of range: default stays
		{"abc", "1", 850, 15},   // garbage + out of range: defaults stay
	} {
		s, err := Open(legacyMatchDB(t, tc.threshold, tc.window))
		if err != nil {
			t.Fatalf("Open legacy database: %v", err)
		}
		got, err := s.GetApp("legacy-app")
		s.Close()
		if err != nil || got.MatchThreshold != tc.wantT || got.MatchWindowMinutes != tc.wantW {
			t.Errorf("settings %s/%s: app match = %+v, %v; want %d/%d", tc.threshold, tc.window, got, err, tc.wantT, tc.wantW)
		}
	}
}

// TestGetLinkByKeyGlobal rejects cross-app duplicates rather than resolving public traffic to arbitrary app.
func TestGetLinkByKeyGlobal(t *testing.T) {
	s := newTestStore(t)
	app, link := setupApp(t, s)
	app2, err := s.CreateApp("second app", "key-2")
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	if _, err := s.CreateLink(Link{AppID: app2.ID, Key: "abc", URL: "https://second.example/"}); !errors.Is(err, ErrKeyConflict) {
		t.Fatalf("CreateLink duplicate global key = %v; want ErrKeyConflict", err)
	}
	got, err := s.GetLinkByKeyGlobal("abc")
	if err != nil {
		t.Fatalf("GetLinkByKeyGlobal: %v", err)
	}
	if got.AppID != app.ID || got.ID != link.ID {
		t.Errorf("GetLinkByKeyGlobal = %+v; want the first-created app's link %s", got, link.ID)
	}
	if _, err := s.GetLinkByKeyGlobal("nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetLinkByKeyGlobal(nope) = %v; want ErrNotFound", err)
	}
}

func TestLinkKeyConflict(t *testing.T) {
	s := newTestStore(t)
	app, _ := setupApp(t, s)
	_, err := s.CreateLink(Link{AppID: app.ID, Key: "abc", URL: "https://x"})
	if !errors.Is(err, ErrKeyConflict) {
		t.Errorf("duplicate link key err = %v; want ErrKeyConflict", err)
	}
}

func TestGetLinkByKeyGlobalAmbiguousFailsClosed(t *testing.T) {
	s := newTestStore(t)
	setupApp(t, s)
	app2, err := s.CreateApp("second app", "key-2")
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	// CreateLink blocks cross-app duplicates now; simulate a historic one.
	if _, err := s.db.Exec(`INSERT INTO links (id, app_id, key, url) VALUES ('dup', ?, 'abc', 'https://second.example/')`, app2.ID); err != nil {
		t.Fatalf("insert duplicate: %v", err)
	}
	if _, err := s.GetLinkByKeyGlobal("abc"); !errors.Is(err, ErrAmbiguousKey) {
		t.Errorf("GetLinkByKeyGlobal(dup) = %v; want ErrAmbiguousKey", err)
	}
}

// pre-v0.2 DB gains expires_at, expired_url, os_version on Open; re-Open is a no-op
// Pre-fraud database: Open adds every later column, legacy rows read with zero facts, new facts and labels round trip.
func TestOpenAddsMissingColumns(t *testing.T) {
	path := t.TempDir() + "/legacy.db"
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	_, err = db.Exec(`
CREATE TABLE apps (id TEXT PRIMARY KEY, name TEXT NOT NULL, api_key_hash TEXT NOT NULL,
  ios_app_id TEXT, android_package TEXT, android_cert_fingerprint TEXT,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')));
CREATE TABLE links (id TEXT PRIMARY KEY, app_id TEXT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
  key TEXT NOT NULL, url TEXT NOT NULL, ios TEXT, android TEXT, fallback_url TEXT,
  threshold INTEGER NOT NULL DEFAULT 850, window_minutes INTEGER NOT NULL DEFAULT 15,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')), UNIQUE (app_id, key));
CREATE TABLE clicks (id TEXT PRIMARY KEY, app_id TEXT NOT NULL, link_id TEXT NOT NULL, ip TEXT,
  device TEXT, locale TEXT, timezone TEXT, screen TEXT, user_agent TEXT, pasted_link TEXT,
  destination TEXT NOT NULL, click_id TEXT, is_bot INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')), expires_at TEXT NOT NULL);
INSERT INTO apps (id, name, api_key_hash) VALUES ('legacy-app', 'old', 'h');
INSERT INTO links (id, app_id, key, url) VALUES ('legacy-link', 'legacy-app', 'old', 'https://example.com/old');
INSERT INTO clicks (id, app_id, link_id, ip, destination, created_at, expires_at) VALUES ('legacy-click', 'legacy-app', 'legacy-link', '203.0.113.7', 'x', '2026-01-01T00:00:00.000Z', '2999-01-01T00:00:00.000Z');
CREATE TABLE installs (id TEXT PRIMARY KEY, app_id TEXT NOT NULL, device_hash TEXT NOT NULL, click_id TEXT, attribution TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')), link_id TEXT, platform TEXT, method TEXT, score INTEGER, runner_up INTEGER,
  UNIQUE (app_id, device_hash, click_id));
INSERT INTO installs (id, app_id, device_hash, click_id, attribution) VALUES ('legacy-install', 'legacy-app', 'd0', 'legacy-click', 'non_organic');`)
	closeErr := db.Close()
	if err != nil {
		t.Fatalf("prepare legacy database: %v", err)
	}
	if closeErr != nil {
		t.Fatalf("close legacy database: %v", closeErr)
	}

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open legacy database: %v", err)
	}
	defer s.Close()
	for _, c := range [][2]string{{"clicks", "matched_at"}, {"clicks", "kind"}, {"clicks", "first_seen_at"}, {"clicks", "ua_suspect"}, {"clicks", "ip_hosting"},
		{"clicks", "hits_ip"}, {"clicks", "hits_link"}, {"clicks", "source"}, {"installs", "fraud"}, {"installs", "fraud_action"}, {"installs", "fraud_link_id"}} {
		if !columnExists(s.db, c[0], c[1]) {
			t.Errorf("%s.%s missing after Open", c[0], c[1])
		}
	}
	// legacy row: zero facts, first_seen_at falls back to created_at
	if lc, err := s.GetClick("legacy-click"); err != nil || lc.UASuspect || lc.IPHosting || lc.HitsIP != 0 || lc.HitsLink != 0 || lc.Kind != "" || !lc.FirstSeenAt.Equal(lc.CreatedAt) {
		t.Errorf("legacy click = %+v, %v; want zero facts, first_seen = created", lc, err)
	}
	l, err := s.GetLink("legacy-link")
	if err != nil || l.ExpiresAt != nil || l.ExpiredURL != "" {
		t.Fatalf("GetLink = %+v, %v; want nil ExpiresAt, empty ExpiredURL", l, err)
	}
	exp := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	l.ExpiresAt, l.ExpiredURL = &exp, "https://example.com/gone"
	if err := s.UpdateLink(l); err != nil {
		t.Fatalf("UpdateLink: %v", err)
	}
	got, err := s.GetLink("legacy-link")
	if err != nil || got.ExpiresAt == nil || !got.ExpiresAt.Equal(exp) || got.ExpiredURL != "https://example.com/gone" {
		t.Errorf("GetLink after update = %+v, %v; want expiry %v + expired url", got, err, exp)
	}
	c, err := s.RecordClick(Click{AppID: "legacy-app", LinkID: "legacy-link", Destination: "x", Fingerprint: Fingerprint{OSVersion: "14.0.0"}}, 24)
	if err != nil {
		t.Fatalf("RecordClick: %v", err)
	}
	if gc, err := s.GetClick(c.ID); err != nil || gc.Fingerprint.OSVersion != "14.0.0" {
		t.Errorf("GetClick OSVersion = %q, %v; want 14.0.0", gc.Fingerprint.OSVersion, err)
	}
	// fraud facts and labels write and read back on the upgraded tables
	fc, err := s.RecordClick(Click{AppID: "legacy-app", LinkID: "legacy-link", Destination: "x", Kind: KindApp, IPHosting: true,
		Fingerprint: Fingerprint{IP: "3.5.140.1", UserAgent: "ua"}}, 24)
	if err != nil {
		t.Fatalf("RecordClick with fraud facts: %v", err)
	}
	if gc, err := s.GetClick(fc.ID); err != nil || gc.Kind != KindApp || !gc.IPHosting || gc.HitsIP != 1 || gc.HitsLink != 2 {
		t.Errorf("GetClick = kind %q hosting %v hits %d/%d, %v; want app, true, 1/2", gc.Kind, gc.IPHosting, gc.HitsIP, gc.HitsLink, err)
	}
	// in-app source writes, reads back and rolls up on the upgraded database
	sc, err := s.RecordClick(Click{AppID: "legacy-app", LinkID: "legacy-link", Destination: "x", Kind: KindApp, Source: "zalo",
		Fingerprint: Fingerprint{IP: "198.51.100.77", UserAgent: "ua-zalo"}}, 24)
	if err != nil {
		t.Fatalf("RecordClick with source: %v", err)
	}
	if gc, _ := s.GetClick(sc.ID); gc.Source != "zalo" {
		t.Errorf("GetClick Source = %q; want zalo", gc.Source)
	}
	if a, err := s.Analytics("legacy-app", 1, "", time.Now()); err != nil || len(a.Sources) != 1 || a.Sources[0].Source != "zalo" {
		t.Errorf("legacy Analytics Sources = %+v, %v; want zalo 1", a.Sources, err)
	}
	if _, err := s.RecordInstall(Install{AppID: "legacy-app", DeviceHash: "d1", Attribution: AttributionOrganic, Method: MethodOrganic,
		Fraud: "velocity,install_ip", FraudAction: FraudActionExcluded, FraudLinkID: "legacy-link"}); err != nil {
		t.Fatalf("RecordInstall with fraud labels: %v", err)
	}
	f, err := s.Fraud("legacy-app", 7, time.Now())
	if err != nil || len(f.Installs) != 1 || f.Installs[0].DeviceHash != "d1" || f.Installs[0].FraudAction != FraudActionExcluded ||
		f.Installs[0].FraudLinkID != "legacy-link" || f.Signals[fraud.SignalInstallIP] != 1 || f.Signals[fraud.SignalVelocity] != 1 {
		t.Errorf("Fraud after upgrade = %+v, %v; want the d1 install, excluded, velocity + install_ip counted", f, err)
	}
	s.Close()
	s2, err := Open(path)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	s2.Close()
}

// dedup window edges: 59m hit, 61m miss, other link miss, NULL IP hit, empty signal keeps old value
func TestRecordClickDedupBoundaries(t *testing.T) {
	cases := []struct {
		name     string
		first    Fingerprint
		second   Fingerprint
		age      time.Duration
		otherLnk bool
		wantSame bool
		wantTZ   string
	}{
		{name: "59 min old", first: Fingerprint{IP: "1.1.1.1", UserAgent: "ua"}, second: Fingerprint{IP: "1.1.1.1", UserAgent: "ua"}, age: 59 * time.Minute, wantSame: true},
		{name: "61 min old", first: Fingerprint{IP: "1.1.1.1", UserAgent: "ua"}, second: Fingerprint{IP: "1.1.1.1", UserAgent: "ua"}, age: 61 * time.Minute},
		{name: "other link", first: Fingerprint{IP: "1.1.1.1", UserAgent: "ua"}, second: Fingerprint{IP: "1.1.1.1", UserAgent: "ua"}, otherLnk: true},
		{name: "NULL IP both", first: Fingerprint{UserAgent: "ua"}, second: Fingerprint{UserAgent: "ua"}, wantSame: true},
		{name: "empty signal keeps old", first: Fingerprint{IP: "1.1.1.1", UserAgent: "ua", Timezone: "Europe/Warsaw"}, second: Fingerprint{IP: "1.1.1.1", UserAgent: "ua"}, wantSame: true, wantTZ: "Europe/Warsaw"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestStore(t)
			app, link := setupApp(t, s)
			first, err := s.RecordClick(Click{AppID: app.ID, LinkID: link.ID, Destination: link.URL, Fingerprint: tc.first}, 24)
			if err != nil {
				t.Fatalf("RecordClick first: %v", err)
			}
			if tc.age > 0 {
				if _, err := s.db.Exec(`UPDATE clicks SET created_at = ? WHERE id = ?`, rfc3339(time.Now().UTC().Add(-tc.age)), first.ID); err != nil {
					t.Fatalf("backdate: %v", err)
				}
			}
			lid := link.ID
			if tc.otherLnk {
				l2, err := s.CreateLink(Link{AppID: app.ID, Key: "abc2", URL: "https://example.com/other"})
				if err != nil {
					t.Fatalf("CreateLink: %v", err)
				}
				lid = l2.ID
			}
			second, err := s.RecordClick(Click{AppID: app.ID, LinkID: lid, Destination: link.URL, Fingerprint: tc.second}, 24)
			if err != nil {
				t.Fatalf("RecordClick second: %v", err)
			}
			if same := second.ID == first.ID; same != tc.wantSame {
				t.Errorf("same ID = %v; want %v", same, tc.wantSame)
			}
			if tc.wantTZ != "" {
				if got, err := s.GetClick(second.ID); err != nil || got.Fingerprint.Timezone != tc.wantTZ {
					t.Errorf("timezone = %q, %v; want %q", got.Fingerprint.Timezone, err, tc.wantTZ)
				}
			}
		})
	}
}

// purge drops expired click, keeps live click and recent event
func TestPurgeExpiredKeepsLive(t *testing.T) {
	s := newTestStore(t)
	app, link := setupApp(t, s)
	a, err := s.RecordClick(Click{AppID: app.ID, LinkID: link.ID, Destination: link.URL, Fingerprint: Fingerprint{UserAgent: "ua-a"}}, 24)
	if err != nil {
		t.Fatalf("RecordClick A: %v", err)
	}
	b, err := s.RecordClick(Click{AppID: app.ID, LinkID: link.ID, Destination: link.URL, Fingerprint: Fingerprint{UserAgent: "ua-b"}}, 24)
	if err != nil {
		t.Fatalf("RecordClick B: %v", err)
	}
	if _, err := s.rawExec(`UPDATE clicks SET expires_at = ? WHERE id = ?`, rfc3339(time.Now().Add(-time.Hour)), a.ID); err != nil {
		t.Fatalf("force expiry: %v", err)
	}
	if err := s.RecordEvent(app.ID, "install", `{}`); err != nil {
		t.Fatalf("RecordEvent: %v", err)
	}
	if _, err := s.PurgeExpired(time.Now(), 24); err != nil {
		t.Fatalf("PurgeExpired: %v", err)
	}
	if _, err := s.GetClick(a.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("expired click survived: %v", err)
	}
	if _, err := s.GetClick(b.ID); err != nil {
		t.Errorf("live click purged: %v", err)
	}
	if n, err := s.CountEvents(app.ID); err != nil || n != 1 {
		t.Errorf("CountEvents = %d, %v; want 1", n, err)
	}
}

// Past retention: scrubbed, click_id lives ClickIDHours, matched deleted.
func TestClickIDOutlivesRetention(t *testing.T) {
	s := newTestStore(t)
	s.ClickIDHours = 30 * 24
	app, link := setupApp(t, s)
	fp := Fingerprint{IP: "1.2.3.4", UserAgent: "ua", Timezone: "Europe/Warsaw", Screen: "390x844"}
	late, _ := s.RecordClick(Click{AppID: app.ID, LinkID: link.ID, Destination: link.URL, Fingerprint: fp, ClickID: "late"}, 24)
	gone, _ := s.RecordClick(Click{AppID: app.ID, LinkID: link.ID, Destination: link.URL, Fingerprint: Fingerprint{IP: "5.6.7.8", UserAgent: "ua"}, ClickID: "gone"}, 24)
	used, _ := s.RecordClick(Click{AppID: app.ID, LinkID: link.ID, Destination: link.URL, Fingerprint: Fingerprint{IP: "9.9.9.9", UserAgent: "ua"}}, 24)
	ago := func(d time.Duration) string { return rfc3339(time.Now().Add(-d)) }
	for id, created := range map[string]time.Duration{late.ID: 3 * 24 * time.Hour, gone.ID: 31 * 24 * time.Hour, used.ID: 3 * 24 * time.Hour} {
		if _, err := s.rawExec(`UPDATE clicks SET created_at = ?, expires_at = ? WHERE id = ?`, ago(created), ago(created-24*time.Hour), id); err != nil {
			t.Fatalf("age click: %v", err)
		}
	}
	if ok, _ := s.MarkClickMatched(used.ID, ""); !ok {
		t.Fatal("MarkClickMatched failed")
	}
	if _, err := s.PurgeExpired(time.Now(), 24); err != nil {
		t.Fatalf("PurgeExpired: %v", err)
	}
	got, err := s.ClickByClickID(app.ID, "late", "")
	if err != nil || got.Destination != link.URL {
		t.Fatalf("ClickByClickID(late) = %+v, %v; want resolved after 3 days", got, err)
	}
	if got.Fingerprint != (Fingerprint{}) {
		t.Errorf("fingerprint = %+v; want scrubbed", got.Fingerprint)
	}
	if _, err := s.ClickByClickID(app.ID, "gone", ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("ClickByClickID(gone) = %v; want ErrNotFound after 31 days", err)
	}
	for _, id := range []string{gone.ID, used.ID} {
		if _, err := s.GetClick(id); !errors.Is(err, ErrNotFound) {
			t.Errorf("click %s survived purge: %v", id, err)
		}
	}
	if n, _ := s.CountClicks(app.ID); n != 0 {
		t.Errorf("CountClicks = %d; want 0 (scrubbed rows not counted)", n)
	}
}

func TestNanoidAlphabetAndLength(t *testing.T) {
	const alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	for i := 0; i < 1000; i++ {
		id := Nanoid(22)
		if len(id) != 22 || strings.Trim(id, alphabet) != "" {
			t.Fatalf("Nanoid(22) = %q", id)
		}
	}
}

func TestDeleteLinkRemovesClicks(t *testing.T) {
	s := newTestStore(t)
	app, l1 := setupApp(t, s)
	l2, err := s.CreateLink(Link{AppID: app.ID, Key: "def", URL: "https://example.com/two"})
	if err != nil {
		t.Fatalf("CreateLink: %v", err)
	}
	c1, err := s.RecordClick(Click{AppID: app.ID, LinkID: l1.ID, Destination: l1.URL}, 24)
	if err != nil {
		t.Fatalf("RecordClick: %v", err)
	}
	c2, err := s.RecordClick(Click{AppID: app.ID, LinkID: l2.ID, Destination: l2.URL}, 24)
	if err != nil {
		t.Fatalf("RecordClick: %v", err)
	}
	if err := s.DeleteLink(l1.ID); err != nil {
		t.Fatalf("DeleteLink: %v", err)
	}
	if _, err := s.GetClick(c1.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetClick(deleted link) = %v; want ErrNotFound", err)
	}
	if _, err := s.GetClick(c2.ID); err != nil {
		t.Errorf("GetClick(other link) = %v; want present", err)
	}
	var rollups int
	s.db.QueryRow(`SELECT COUNT(*) FROM click_days WHERE link_id = ?`, l1.ID).Scan(&rollups)
	if rollups != 0 {
		t.Errorf("click_days rows for deleted link = %d; want 0", rollups)
	}
	if err := s.DeleteLink("missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("DeleteLink(missing) = %v; want ErrNotFound", err)
	}
}

func TestDeleteAppRemovesAppData(t *testing.T) {
	s := newTestStore(t)
	a, la := setupApp(t, s)
	b, err := s.CreateApp("other app", "other-key-456")
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	lb, err := s.CreateLink(Link{AppID: b.ID, Key: "xyz", URL: "https://example.com/b"})
	if err != nil {
		t.Fatalf("CreateLink: %v", err)
	}
	for _, x := range []struct {
		app App
		l   Link
	}{{a, la}, {b, lb}} {
		if _, err := s.RecordClick(Click{AppID: x.app.ID, LinkID: x.l.ID, Destination: x.l.URL}, 24); err != nil {
			t.Fatalf("RecordClick: %v", err)
		}
		if _, err := s.RecordInstall(Install{AppID: x.app.ID, DeviceHash: "h", ClickID: "c", Attribution: AttributionNonOrganic}); err != nil {
			t.Fatalf("RecordInstall: %v", err)
		}
		if err := s.RecordEvent(x.app.ID, "install", "{}"); err != nil {
			t.Fatalf("RecordEvent: %v", err)
		}
	}
	if err := s.DeleteApp(a.ID); err != nil {
		t.Fatalf("DeleteApp: %v", err)
	}
	clicks, _ := s.CountClicks(a.ID)
	org, non, _ := s.CountInstalls(a.ID)
	events, _ := s.CountEvents(a.ID)
	if clicks != 0 || org != 0 || non != 0 || events != 0 {
		t.Errorf("deleted app data = %d clicks, %d/%d installs, %d events; want all 0", clicks, org, non, events)
	}
	clicks, _ = s.CountClicks(b.ID)
	_, non, _ = s.CountInstalls(b.ID)
	events, _ = s.CountEvents(b.ID)
	if clicks != 1 || non != 1 || events != 1 {
		t.Errorf("other app data = %d clicks, %d non-organic installs, %d events; want 1 each", clicks, non, events)
	}
	if err := s.DeleteApp("missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("DeleteApp(missing) = %v; want ErrNotFound", err)
	}
}

// Analytics: rollups count new clicks once per kind/platform, outlive the retention purge, fill empty days, and leave with the app.
func TestAnalytics(t *testing.T) {
	s := newTestStore(t)
	app, link := setupApp(t, s)
	start := time.Now()
	iphone := Fingerprint{IP: "203.0.113.7", UserAgent: "Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X)"}
	desktop := Fingerprint{IP: "203.0.113.8", UserAgent: "Mozilla/5.0 (Macintosh; Intel Mac OS X 14_5)"}
	appUA := Fingerprint{IP: "203.0.113.9", UserAgent: "MyApp/1 CFNetwork/1490 Darwin/23.5.0"}
	for _, c := range []Click{
		{Fingerprint: iphone, Platform: "ios", Kind: KindApp},
		{Fingerprint: iphone, Platform: "ios", Kind: KindApp}, // dedup: same click
		{Fingerprint: desktop, Platform: "desktop", Kind: KindWeb},
		{Fingerprint: appUA, Platform: "ios", Kind: KindOpen},
	} {
		c.AppID, c.LinkID, c.Destination = app.ID, link.ID, link.URL
		if _, err := s.RecordClick(c, 24); err != nil {
			t.Fatalf("RecordClick: %v", err)
		}
	}
	long := strings.Repeat("x", 100)
	for _, e := range []string{"purchase", "signup", "purchase", long} {
		if err := s.RecordEvent(app.ID, e, ""); err != nil {
			t.Fatalf("RecordEvent: %v", err)
		}
	}
	for _, i := range []Install{
		{AppID: app.ID, DeviceHash: "d1", Attribution: AttributionOrganic, Platform: "android"},
		{AppID: app.ID, DeviceHash: "d2", ClickID: "c2", Attribution: AttributionNonOrganic, LinkID: link.ID, Platform: "ios"},
		{AppID: app.ID, DeviceHash: "d3", Attribution: AttributionUnknown},
	} {
		if _, err := s.RecordInstall(i); err != nil {
			t.Fatalf("RecordInstall: %v", err)
		}
	}

	now := time.Now()
	if day(now) != day(start) {
		t.Skip("crossed UTC midnight while recording; rollup day assertions would be off by one")
	}
	// raw clicks/events purged; rollups stay
	if _, err := s.PurgeExpired(now.Add(48*time.Hour), 24); err != nil {
		t.Fatalf("PurgeExpired: %v", err)
	}
	a, err := s.Analytics(app.ID, 7, "", now)
	if err != nil {
		t.Fatalf("Analytics: %v", err)
	}
	if len(a.Days) != 7 || a.Days[6].Day != now.UTC().Format("2006-01-02") || a.Days[0].Clicks != 0 {
		t.Fatalf("days = %+v; want 7 zero-filled days ending today", a.Days)
	}
	want := DayStat{Day: a.Days[6].Day, Clicks: 2, Web: 1, Opens: 1, Organic: 1, NonOrganic: 1}
	if a.Days[6] != want {
		t.Errorf("today = %+v; want %+v", a.Days[6], want)
	}
	if len(a.Links) != 1 || a.Links[0].Clicks != 2 || a.Links[0].Matches != 1 {
		t.Errorf("links = %+v; want 2 clicks, 1 match", a.Links)
	}
	if len(a.Events) != 3 || a.Events[0] != (EventStat{"purchase", 2}) || a.Events[2] != (EventStat{"xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx", 1}) {
		t.Errorf("events = %+v; want purchase 2, signup 1, long name cut to 64", a.Events)
	}

	ios, err := s.Analytics(app.ID, 7, "ios", now)
	if err != nil {
		t.Fatalf("Analytics ios: %v", err)
	}
	want = DayStat{Day: want.Day, Clicks: 1, Opens: 1, NonOrganic: 1}
	if ios.Days[6] != want {
		t.Errorf("ios today = %+v; want %+v", ios.Days[6], want)
	}

	if err := s.DeleteApp(app.ID); err != nil {
		t.Fatalf("DeleteApp: %v", err)
	}
	var n int
	s.db.QueryRow(`SELECT (SELECT COUNT(*) FROM click_days) + (SELECT COUNT(*) FROM event_days)`).Scan(&n)
	if n != 0 {
		t.Errorf("rollup rows after DeleteApp = %d; want 0", n)
	}
}

// hits counts raw taps per IP and link (KTD2): dedup merges rows, not hits; first_seen_at stays at the first tap.
func TestRecordClickHitCounter(t *testing.T) {
	s := newTestStore(t)
	app, link := setupApp(t, s)
	fp := Fingerprint{IP: "203.0.113.7", UserAgent: "Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X)"}
	rec := func(fp Fingerprint) Click {
		t.Helper()
		c, err := s.RecordClick(Click{AppID: app.ID, LinkID: link.ID, Fingerprint: fp, Destination: link.URL, Kind: KindApp}, 24)
		if err != nil {
			t.Fatalf("RecordClick: %v", err)
		}
		return c
	}
	first := rec(fp)
	if first.HitsIP != 1 || first.HitsLink != 1 || !first.FirstSeenAt.Equal(first.CreatedAt) || first.Kind != KindApp {
		t.Fatalf("first = hits %d/%d first_seen %v created %v kind %q; want 1/1, equal, app", first.HitsIP, first.HitsLink, first.FirstSeenAt, first.CreatedAt, first.Kind)
	}
	var stamped bool
	s.db.QueryRow(`SELECT first_seen_at IS created_at FROM clicks WHERE id = ?`, first.ID).Scan(&stamped)
	if !stamped {
		t.Fatal("stored first_seen_at differs from created_at on insert; want the first tap stamped")
	}
	old := time.Now().UTC().Add(-10 * time.Minute)
	if _, err := s.rawExec(`UPDATE clicks SET created_at = ?, first_seen_at = ? WHERE id = ?`, rfc3339(old), rfc3339(old), first.ID); err != nil {
		t.Fatal(err)
	}
	rec(fp)
	third := rec(fp)
	if third.ID != first.ID || third.HitsIP != 3 || third.HitsLink != 3 {
		t.Errorf("third = id %s hits %d/%d; want %s 3/3", third.ID, third.HitsIP, third.HitsLink, first.ID)
	}
	if !third.FirstSeenAt.Equal(parseTime(rfc3339(old))) || !third.CreatedAt.After(old) {
		t.Errorf("first_seen %v created %v; want first_seen = %v, created moved forward", third.FirstSeenAt, third.CreatedAt, old)
	}
	if n, _ := s.CountClicks(app.ID); n != 1 {
		t.Errorf("clicks = %d; want 1", n)
	}
	// second IP on the same link: link counts all, IP counts its own
	other := rec(Fingerprint{IP: "198.51.100.9", UserAgent: fp.UserAgent})
	if other.HitsIP != 1 || other.HitsLink != 4 {
		t.Errorf("other IP hits = %d/%d; want 1/4", other.HitsIP, other.HitsLink)
	}
	// IPv4-mapped form of the first IP: same counter
	if mapped := rec(Fingerprint{IP: "::ffff:203.0.113.7", UserAgent: "ua-mapped"}); mapped.HitsIP != 4 {
		t.Errorf("IPv4-mapped hits_ip = %d; want 4 (shares 203.0.113.7's counter)", mapped.HitsIP)
	}
	// IPv6 sharing a /64: one key
	a := rec(Fingerprint{IP: "2001:db8:1:2::1", UserAgent: "ua-a"})
	b := rec(Fingerprint{IP: "2001:db8:1:2:ffff::9", UserAgent: "ua-b"})
	if a.HitsIP != 1 || b.HitsIP != 2 {
		t.Errorf("v6 /64 hits = %d, %d; want 1, 2", a.HitsIP, b.HitsIP)
	}
	// private and empty IPs never count (KTD8)
	for _, ip := range []string{"10.0.0.1", "127.0.0.1", ""} {
		c := rec(Fingerprint{IP: ip, UserAgent: "ua-" + ip})
		if c.HitsIP != 0 {
			t.Errorf("hits_ip for %q = %d; want 0", ip, c.HitsIP)
		}
	}
	var keys int
	s.db.QueryRow(`SELECT COUNT(*) FROM click_hits WHERE key_type = 'ip'`).Scan(&keys)
	if keys != 3 {
		t.Errorf("ip counter keys = %d; want 3 (v4, v4, one v6 /64)", keys)
	}
	if got, _ := s.GetClick(first.ID); got.HitsIP != 3 || got.HitsLink != 3 {
		t.Errorf("GetClick hits = %d/%d; want 3/3", got.HitsIP, got.HitsLink)
	}
}

// UA-suspect clicks are stored but stay out of click_days (R3, AE1).
func TestRecordClickUASuspectSkipsRollup(t *testing.T) {
	s := newTestStore(t)
	app, link := setupApp(t, s)
	bot, err := s.RecordClick(Click{AppID: app.ID, LinkID: link.ID, Destination: link.URL, Kind: KindApp, UASuspect: true, IPHosting: true,
		Fingerprint: Fingerprint{IP: "203.0.113.7", UserAgent: "python-requests/2.31"}}, 24)
	if err != nil {
		t.Fatalf("RecordClick: %v", err)
	}
	if got, err := s.GetClick(bot.ID); err != nil || !got.UASuspect || !got.IPHosting {
		t.Errorf("GetClick = %+v, %v; want ua_suspect + ip_hosting", got, err)
	}
	var n int
	s.db.QueryRow(`SELECT COALESCE(SUM(n), 0) FROM click_days WHERE app_id = ?`, app.ID).Scan(&n)
	if n != 0 {
		t.Errorf("click_days after suspect click = %d; want 0", n)
	}
	if _, err := s.RecordClick(Click{AppID: app.ID, LinkID: link.ID, Destination: link.URL, Kind: KindApp,
		Fingerprint: Fingerprint{IP: "203.0.113.8", UserAgent: "Mozilla/5.0"}}, 24); err != nil {
		t.Fatal(err)
	}
	s.db.QueryRow(`SELECT COALESCE(SUM(n), 0) FROM click_days WHERE app_id = ?`, app.ID).Scan(&n)
	if n != 1 {
		t.Errorf("click_days after clean click = %d; want 1", n)
	}
}

// Fraud settings: no row reads as Defaults, upsert round trips, a second upsert replaces every field, unknown app is ErrNotFound.
func TestFraudSettings(t *testing.T) {
	s := newTestStore(t)
	app, _ := setupApp(t, s)
	got, err := s.FraudSettings(app.ID)
	if err != nil || got != fraud.Defaults {
		t.Fatalf("FraudSettings (no row) = %+v, %v; want defaults", got, err)
	}
	want := fraud.Defaults
	want.VelocityMode, want.IPMode, want.VelocityWindowMinutes, want.FingerprintMax = fraud.ModeActive, fraud.ModeActive, 15, 9
	if err := s.UpdateFraudSettings(app.ID, want); err != nil {
		t.Fatalf("UpdateFraudSettings: %v", err)
	}
	if got, err := s.FraudSettings(app.ID); err != nil || got != want {
		t.Errorf("round trip = %+v, %v; want %+v", got, err, want)
	}
	want = fraud.Settings{VelocityMode: fraud.ModeTagged, TimingMode: fraud.ModeActive, UserAgentMode: fraud.ModeActive, IPMode: fraud.ModeTagged,
		VelocityIPMax: 31, VelocityLinkMax: 601, VelocityWindowMinutes: 91, TimingShortSeconds: 6, TimingLongHours: 49,
		FingerprintMax: 7, FingerprintWindowDays: 15}
	if err := s.UpdateFraudSettings(app.ID, want); err != nil {
		t.Fatalf("UpdateFraudSettings again: %v", err)
	}
	if got, _ := s.FraudSettings(app.ID); got != want {
		t.Errorf("second update = %+v; want %+v", got, want)
	}
	if err := s.UpdateFraudSettings("missing", want); !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateFraudSettings(missing) = %v; want ErrNotFound", err)
	}
}

// velocity window follows the app's setting: a bucket outside it stops counting.
func TestRecordClickHitWindow(t *testing.T) {
	s := newTestStore(t)
	app, link := setupApp(t, s)
	st := fraud.Defaults
	st.VelocityWindowMinutes = 5
	if err := s.UpdateFraudSettings(app.ID, st); err != nil {
		t.Fatal(err)
	}
	old := time.Now().UTC().Add(-10 * time.Minute).Format(bucketLayout)
	if _, err := s.rawExec(`INSERT INTO click_hits (app_id, key_type, key, bucket, n) VALUES (?, 'link', ?, ?, 50)`, app.ID, link.ID, old); err != nil {
		t.Fatal(err)
	}
	c, err := s.RecordClick(Click{AppID: app.ID, LinkID: link.ID, Destination: link.URL, Fingerprint: Fingerprint{IP: "203.0.113.7"}}, 24)
	if err != nil || c.HitsLink != 1 {
		t.Errorf("hits_link = %d, %v; want 1 (old bucket outside 5m window)", c.HitsLink, err)
	}
}

// Unknown install upgraded by a real attribution takes its fraud labels; purge leaves install labels alone.
func TestInstallFraudLabelsSurviveUpgradeAndPurge(t *testing.T) {
	s := newTestStore(t)
	app, link := setupApp(t, s)
	if _, err := s.RecordInstall(Install{AppID: app.ID, DeviceHash: "d1", ClickID: "c1", Attribution: AttributionUnknown}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordInstall(Install{AppID: app.ID, DeviceHash: "d1", ClickID: "c1", Attribution: AttributionNonOrganic, LinkID: link.ID,
		Fraud: "velocity,ip", FraudAction: FraudActionReattributed, FraudLinkID: "l-bad"}); err != nil {
		t.Fatal(err)
	}
	check := func(when string) {
		t.Helper()
		var f, a, l string
		s.db.QueryRow(`SELECT COALESCE(fraud, ''), COALESCE(fraud_action, ''), COALESCE(fraud_link_id, '') FROM installs WHERE device_hash = 'd1'`).Scan(&f, &a, &l)
		if f != "velocity,ip" || a != FraudActionReattributed || l != "l-bad" {
			t.Errorf("%s labels = %q/%q/%q; want velocity,ip/reattributed/l-bad", when, f, a, l)
		}
	}
	check("upgraded")
	if _, err := s.PurgeExpired(time.Now().Add(1000*time.Hour), 24); err != nil {
		t.Fatal(err)
	}
	check("after purge")
}

// Purge drops hit buckets past the 24 h max window, keeps younger ones.
func TestPurgeExpiredClickHits(t *testing.T) {
	s := newTestStore(t)
	app, link := setupApp(t, s)
	now := time.Now().UTC()
	for _, b := range []time.Time{now.Add(-25 * time.Hour), now.Add(-23 * time.Hour)} {
		if _, err := s.rawExec(`INSERT INTO click_hits (app_id, key_type, key, bucket, n) VALUES (?, 'link', ?, ?, 1)`, app.ID, link.ID, b.Format(bucketLayout)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.PurgeExpired(now, 24); err != nil {
		t.Fatal(err)
	}
	var buckets []string
	rows, _ := s.db.Query(`SELECT bucket FROM click_hits`)
	for rows.Next() {
		var b string
		rows.Scan(&b)
		buckets = append(buckets, b)
	}
	rows.Close()
	if len(buckets) != 1 || buckets[0] != now.Add(-23*time.Hour).Format(bucketLayout) {
		t.Errorf("buckets after purge = %v; want only the 23h-old one", buckets)
	}
}

// DeleteLink drops that link's counter only; DeleteApp drops the app's counters and settings, other apps untouched.
func TestDeleteRemovesFraudRows(t *testing.T) {
	s := newTestStore(t)
	a, la := setupApp(t, s)
	b, _ := s.CreateApp("other", "k2")
	lb, _ := s.CreateLink(Link{AppID: b.ID, Key: "xyz", URL: "https://example.com/b"})
	la2, _ := s.CreateLink(Link{AppID: a.ID, Key: "two", URL: "https://example.com/two"})
	for _, x := range []struct {
		app string
		l   string
	}{{a.ID, la.ID}, {a.ID, la2.ID}, {b.ID, lb.ID}} {
		if _, err := s.RecordClick(Click{AppID: x.app, LinkID: x.l, Destination: "d", Fingerprint: Fingerprint{IP: "203.0.113.7", UserAgent: x.l}}, 24); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{a.ID, b.ID} {
		if err := s.UpdateFraudSettings(id, fraud.Defaults); err != nil {
			t.Fatal(err)
		}
	}
	count := func(q string, args ...any) int {
		var n int
		s.db.QueryRow(q, args...).Scan(&n)
		return n
	}
	if err := s.DeleteLink(la2.ID); err != nil {
		t.Fatal(err)
	}
	if n := count(`SELECT COUNT(*) FROM click_hits WHERE key_type = 'link' AND key = ?`, la2.ID); n != 0 {
		t.Errorf("link counter rows after DeleteLink = %d; want 0", n)
	}
	if n := count(`SELECT COUNT(*) FROM click_hits WHERE key_type = 'link' AND key = ?`, la.ID); n != 1 {
		t.Errorf("sibling link counter rows = %d; want 1", n)
	}
	if err := s.DeleteApp(a.ID); err != nil {
		t.Fatal(err)
	}
	if n := count(`SELECT COUNT(*) FROM click_hits WHERE app_id = ?`, a.ID) + count(`SELECT COUNT(*) FROM fraud_settings WHERE app_id = ?`, a.ID); n != 0 {
		t.Errorf("deleted app fraud rows = %d; want 0", n)
	}
	if n := count(`SELECT COUNT(*) FROM click_hits WHERE app_id = ?`, b.ID) + count(`SELECT COUNT(*) FROM fraud_settings WHERE app_id = ?`, b.ID); n != 3 {
		t.Errorf("other app fraud rows = %d; want 3 (ip + link counters, settings)", n)
	}
}

// Fingerprint count: probabilistic installs only, same hash and same link.
func TestCountFingerprintInstalls(t *testing.T) {
	s := newTestStore(t)
	app, link := setupApp(t, s)
	for i, in := range []Install{
		{DeviceHash: "h", LinkID: link.ID, Method: MethodProbabilistic, Attribution: AttributionNonOrganic},
		{DeviceHash: "h", LinkID: link.ID, Method: MethodProbabilistic, Attribution: AttributionNonOrganic},
		{DeviceHash: "h", LinkID: link.ID, Method: MethodClickID, Attribution: AttributionNonOrganic},
		{DeviceHash: "h", LinkID: "other", Method: MethodProbabilistic, Attribution: AttributionNonOrganic},
		{DeviceHash: "x", LinkID: link.ID, Method: MethodProbabilistic, Attribution: AttributionNonOrganic},
	} {
		in.AppID, in.ClickID = app.ID, fmt.Sprint("c", i)
		if _, err := s.RecordInstall(in); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := s.CountFingerprintInstalls(app.ID, "h", link.ID, time.Now().Add(-time.Hour)); err != nil || n != 2 {
		t.Errorf("CountFingerprintInstalls = %d, %v; want 2", n, err)
	}
}

// Fraud report: per-signal counts over the range (every key present, ip and install_ip apart), latest 100 flagged installs newest first.
func TestFraudAnalytics(t *testing.T) {
	s := newTestStore(t)
	app, link := setupApp(t, s)
	now := time.Now().UTC()
	for i := 0; i < 102; i++ {
		f := "velocity"
		if i%2 == 0 {
			f = "velocity,ip"
		}
		if _, err := s.RecordInstall(Install{AppID: app.ID, DeviceHash: fmt.Sprint("d", i), Attribution: AttributionOrganic, Method: MethodOrganic,
			Fraud: f, FraudAction: FraudActionExcluded, FraudLinkID: link.ID}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.rawExec(`UPDATE installs SET created_at = ? WHERE device_hash = ?`, rfc3339(now.Add(-time.Duration(102-i)*time.Second)), fmt.Sprint("d", i)); err != nil {
			t.Fatal(err)
		}
	}
	// clean install and an out-of-range flagged one: not counted
	s.RecordInstall(Install{AppID: app.ID, DeviceHash: "clean", Attribution: AttributionOrganic})
	s.RecordInstall(Install{AppID: app.ID, DeviceHash: "old", Attribution: AttributionOrganic, Fraud: "timing"})
	s.rawExec(`UPDATE installs SET created_at = ? WHERE device_hash = 'old'`, rfc3339(now.AddDate(0, 0, -30)))
	// install-IP label: counted on its own, never as ip (",ip," is not a substring of ",install_ip,"); oldest, so outside the 100 listed
	s.RecordInstall(Install{AppID: app.ID, DeviceHash: "hosted", Attribution: AttributionOrganic, Fraud: "install_ip"})
	s.rawExec(`UPDATE installs SET created_at = ? WHERE device_hash = 'hosted'`, rfc3339(now.Add(-time.Hour)))

	f, err := s.Fraud(app.ID, 7, now)
	if err != nil {
		t.Fatalf("Fraud: %v", err)
	}
	want := map[string]int64{fraud.SignalVelocity: 102, fraud.SignalTiming: 0, fraud.SignalUserAgent: 0, fraud.SignalIP: 51, fraud.SignalInstallIP: 1, fraud.SignalFingerprint: 0}
	if !reflect.DeepEqual(f.Signals, want) {
		t.Errorf("signals = %v; want %v", f.Signals, want)
	}
	if len(f.Installs) != 100 || f.Installs[0].DeviceHash != "d101" || f.Installs[99].DeviceHash != "d2" {
		t.Fatalf("installs = %d, first %+v; want 100 newest first", len(f.Installs), f.Installs[0])
	}
	if in := f.Installs[0]; len(in.Fraud) != 1 || in.Fraud[0] != "velocity" || in.FraudAction != FraudActionExcluded || in.FraudLinkID != link.ID {
		t.Errorf("install = %+v; want fraud [velocity], excluded, link", in)
	}
}

// Fraud settings table gone: read returns the error and Defaults (callers fail open on it), write returns an error that is not ErrNotFound.
func TestFraudSettingsBackendError(t *testing.T) {
	s := newTestStore(t)
	app, _ := setupApp(t, s)
	want := fraud.Defaults
	want.VelocityMode = fraud.ModeActive
	if err := s.UpdateFraudSettings(app.ID, want); err != nil {
		t.Fatal(err)
	}
	if _, err := s.rawExec(`DROP TABLE fraud_settings`); err != nil {
		t.Fatal(err)
	}
	if got, err := s.FraudSettings(app.ID); err == nil || got != fraud.Defaults {
		t.Errorf("FraudSettings = %+v, %v; want Defaults and an error", got, err)
	}
	if err := s.UpdateFraudSettings(app.ID, want); err == nil || errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateFraudSettings = %v; want a backend error, not ErrNotFound", err)
	}
}

// Installs table gone: fingerprint count, fraud report and signal counts all return errors.
func TestFraudInstallQueriesBackendError(t *testing.T) {
	s := newTestStore(t)
	app, link := setupApp(t, s)
	if _, err := s.rawExec(`DROP TABLE installs`); err != nil {
		t.Fatal(err)
	}
	if n, err := s.CountFingerprintInstalls(app.ID, "h", link.ID, time.Now().Add(-time.Hour)); err == nil || n != 0 {
		t.Errorf("CountFingerprintInstalls = %d, %v; want 0 and an error", n, err)
	}
	if _, err := s.Fraud(app.ID, 7, time.Now()); err == nil {
		t.Error("Fraud: want an error")
	}
	if m, err := s.FraudSignals(app.ID, 7, time.Now()); err == nil || m != nil {
		t.Errorf("FraudSignals = %v, %v; want nil and an error", m, err)
	}
}

// Fraud report list fails after the counts succeed: a column only the list reads is gone, then a corrupt row (NULL id) fails the scan.
func TestFraudListBackendError(t *testing.T) {
	s := newTestStore(t)
	app, _ := setupApp(t, s)
	if _, err := s.RecordInstall(Install{AppID: app.ID, DeviceHash: "d", Attribution: AttributionOrganic, Fraud: "velocity"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.rawExec(`UPDATE installs SET id = NULL`); err != nil {
		t.Fatal(err)
	}
	if m, err := s.FraudSignals(app.ID, 7, time.Now()); err != nil || m[fraud.SignalVelocity] != 1 {
		t.Fatalf("FraudSignals = %v, %v; want velocity 1", m, err)
	}
	if _, err := s.Fraud(app.ID, 7, time.Now()); err == nil {
		t.Error("Fraud with a NULL-id row: want a scan error")
	}
	if _, err := s.rawExec(`ALTER TABLE installs DROP COLUMN fraud_link_id`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.FraudSignals(app.ID, 7, time.Now()); err != nil {
		t.Fatalf("FraudSignals after column drop: %v", err)
	}
	if _, err := s.Fraud(app.ID, 7, time.Now()); err == nil {
		t.Error("Fraud without fraud_link_id: want a query error")
	}
}

// Range edge: a 7-day report starts at 00:00 UTC six days back; an install a millisecond earlier is out, in both counts and list.
func TestFraudRangeEdge(t *testing.T) {
	s := newTestStore(t)
	app, _ := setupApp(t, s)
	now := time.Now().UTC()
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -6)
	for dh, at := range map[string]time.Time{"in": start, "out": start.Add(-time.Millisecond)} {
		if _, err := s.RecordInstall(Install{AppID: app.ID, DeviceHash: dh, Attribution: AttributionOrganic, Fraud: "timing"}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.rawExec(`UPDATE installs SET created_at = ? WHERE device_hash = ?`, rfc3339(at), dh); err != nil {
			t.Fatal(err)
		}
	}
	f, err := s.Fraud(app.ID, 7, now)
	if err != nil {
		t.Fatalf("Fraud: %v", err)
	}
	if f.Signals[fraud.SignalTiming] != 1 || len(f.Installs) != 1 || f.Installs[0].DeviceHash != "in" {
		t.Errorf("7-day report = timing %d, installs %+v; want 1, only the install at %s", f.Signals[fraud.SignalTiming], f.Installs, start)
	}
}

// Hit counter write fails for one key type (a trigger aborts the IP or the link bucket insert): RecordClick returns the error and leaves
// no click and no rollup behind.
func TestRecordClickHitsBackendError(t *testing.T) {
	s := newTestStore(t)
	app, link := setupApp(t, s)
	for _, kt := range []string{"ip", "link"} {
		if _, err := s.rawExec(`CREATE TRIGGER no_hits BEFORE INSERT ON click_hits WHEN NEW.key_type = '` + kt + `' BEGIN SELECT RAISE(ABORT, 'hits down'); END`); err != nil {
			t.Fatal(err)
		}
		if _, err := s.RecordClick(Click{AppID: app.ID, LinkID: link.ID, Destination: link.URL, Fingerprint: Fingerprint{IP: "203.0.113.7"}}, 24); err == nil {
			t.Errorf("%s bucket insert aborted: RecordClick err = nil; want an error", kt)
		}
		if _, err := s.rawExec(`DROP TRIGGER no_hits`); err != nil {
			t.Fatal(err)
		}
	}
	var n int
	s.db.QueryRow(`SELECT (SELECT COUNT(*) FROM clicks) + (SELECT COUNT(*) FROM click_days)`).Scan(&n)
	if n != 0 {
		t.Errorf("click + rollup rows = %d; want 0", n)
	}
}

// Hit counter table gone: DeleteApp and DeleteLink fail and roll back (app, link, clicks kept); PurgeExpired returns the error.
func TestFraudCleanupBackendError(t *testing.T) {
	s := newTestStore(t)
	app, link := setupApp(t, s)
	if _, err := s.RecordClick(Click{AppID: app.ID, LinkID: link.ID, Destination: link.URL}, 24); err != nil {
		t.Fatal(err)
	}
	if _, err := s.rawExec(`DROP TABLE click_hits`); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteLink(link.ID); err == nil {
		t.Error("DeleteLink: want an error")
	}
	if err := s.DeleteApp(app.ID); err == nil {
		t.Error("DeleteApp: want an error")
	}
	if _, err := s.GetApp(app.ID); err != nil {
		t.Errorf("app after failed delete: %v", err)
	}
	if _, err := s.GetLink(link.ID); err != nil {
		t.Errorf("link after failed delete: %v", err)
	}
	if n, _ := s.CountClicks(app.ID); n != 1 {
		t.Errorf("clicks after failed deletes = %d; want 1", n)
	}
	if _, err := s.PurgeExpired(time.Now(), 24); err == nil {
		t.Error("PurgeExpired: want an error")
	}
}

// Refresh keeps the hit maxima: 50 taps counted, then the buckets age out (deleted), so the next tap's fresh sums are 1, but the stored
// counts stay at their peak.
func TestRecordClickRefreshKeepsHitMax(t *testing.T) {
	s := newTestStore(t)
	app, link := setupApp(t, s)
	c := Click{AppID: app.ID, LinkID: link.ID, Destination: link.URL, Fingerprint: Fingerprint{IP: "203.0.113.7", UserAgent: "ua"}}
	first, err := s.RecordClick(c, 24)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.rawExec(`UPDATE click_hits SET n = 50`); err != nil {
		t.Fatal(err)
	}
	peak, err := s.RecordClick(c, 24)
	if err != nil || peak.HitsIP != 51 || peak.HitsLink != 51 {
		t.Fatalf("peak = hits %d/%d, %v; want 51/51", peak.HitsIP, peak.HitsLink, err)
	}
	if _, err := s.rawExec(`DELETE FROM click_hits`); err != nil {
		t.Fatal(err)
	}
	again, err := s.RecordClick(c, 24)
	if err != nil || again.ID != first.ID || again.HitsIP != 51 || again.HitsLink != 51 {
		t.Errorf("refresh = id %s hits %d/%d, %v; want %s, hits kept at 51/51", again.ID, again.HitsIP, again.HitsLink, err, first.ID)
	}
}

// In-app reopen merge: a real-browser reload of an in-app click on the same link + IP within the window is the same click and keeps its destination; second in-app browsers, other platforms, IPs, links and SDK opens stay separate.
func TestRecordClickInAppReopenMerge(t *testing.T) {
	const webview, chrome, okhttp = "Mozilla/5.0 (Linux; Android 14; wv) [FB_IAB/MESSENGER;]", "Mozilla/5.0 (Linux; Android 14) Chrome/125.0", "okhttp/4.12.0"
	ip := "203.0.113.7"
	for _, tc := range []struct {
		name      string
		firstSrc  string
		second    Click
		otherLink bool
		age       time.Duration
		wantRows  int
	}{
		{name: "reopen in real browser merges", firstSrc: "messenger", second: Click{Fingerprint: Fingerprint{IP: ip, UserAgent: chrome}, Kind: KindApp}, wantRows: 1},
		{name: "sdk open never merges", firstSrc: "messenger", second: Click{Fingerprint: Fingerprint{IP: ip, UserAgent: okhttp}, Kind: KindOpen}, wantRows: 2},
		{name: "no source on first click", second: Click{Fingerprint: Fingerprint{IP: ip, UserAgent: chrome}, Kind: KindApp}, wantRows: 2},
		{name: "second in-app browser stays separate", firstSrc: "messenger", second: Click{Fingerprint: Fingerprint{IP: ip, UserAgent: "Mozilla/5.0 (Linux; Android 14; wv) Zalo"}, Kind: KindApp, Source: "zalo"}, wantRows: 2},
		{name: "other platform stays separate", firstSrc: "messenger", second: Click{Fingerprint: Fingerprint{IP: ip, UserAgent: "Mozilla/5.0 (iPhone; CPU iPhone OS 17_2 like Mac OS X) Safari/604.1"}, Kind: KindApp}, wantRows: 2},
		{name: "different ip", firstSrc: "messenger", second: Click{Fingerprint: Fingerprint{IP: "198.51.100.1", UserAgent: chrome}, Kind: KindApp}, wantRows: 2},
		{name: "different link", firstSrc: "messenger", second: Click{Fingerprint: Fingerprint{IP: ip, UserAgent: chrome}, Kind: KindApp}, otherLink: true, wantRows: 2},
		{name: "outside window", firstSrc: "messenger", second: Click{Fingerprint: Fingerprint{IP: ip, UserAgent: chrome}, Kind: KindApp}, age: 2 * time.Hour, wantRows: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestStore(t)
			app, link := setupApp(t, s)
			first, err := s.RecordClick(Click{AppID: app.ID, LinkID: link.ID, Destination: link.URL, Kind: KindApp, Platform: "android",
				Source: tc.firstSrc, Fingerprint: Fingerprint{IP: ip, UserAgent: webview}}, 24)
			if err != nil {
				t.Fatalf("RecordClick first: %v", err)
			}
			if tc.age > 0 {
				if _, err := s.db.Exec(`UPDATE clicks SET created_at = ? WHERE id = ?`, rfc3339(time.Now().Add(-tc.age)), first.ID); err != nil {
					t.Fatal(err)
				}
			}
			lid := link.ID
			if tc.otherLink {
				l2, err := s.CreateLink(Link{AppID: app.ID, Key: "other", URL: "https://example.com/o"})
				if err != nil {
					t.Fatal(err)
				}
				lid = l2.ID
			}
			c := tc.second
			c.AppID, c.LinkID, c.Destination, c.Platform = app.ID, lid, "https://evil.example/other", "android"
			c.Fingerprint.PastedLink = "https://evil.example/paste"
			second, err := s.RecordClick(c, 24)
			if err != nil {
				t.Fatalf("RecordClick second: %v", err)
			}
			if n, _ := s.CountClicks(app.ID); n != int64(tc.wantRows) {
				t.Fatalf("clicks = %d; want %d", n, tc.wantRows)
			}
			if tc.wantRows == 1 {
				if second.Destination != link.URL || second.Fingerprint.PastedLink != "" {
					t.Errorf("reopen overwrote destination/pasted link: %q %q", second.Destination, second.Fingerprint.PastedLink)
				}
				if second.ID != first.ID || second.Source != "messenger" {
					t.Errorf("merged click = %s source %q; want %s source messenger", second.ID, second.Source, first.ID)
				}
				a, err := s.Analytics(app.ID, 1, "", time.Now())
				if err != nil {
					t.Fatal(err)
				}
				if a.Days[0].Clicks != 1 || len(a.Sources) != 1 || a.Sources[0].Count != 1 {
					t.Errorf("rollups = %d clicks, sources %+v; want 1 click, messenger 1", a.Days[0].Clicks, a.Sources)
				}
			}
		})
	}
}

// Sources: per-source rollup of new in-app clicks for the range; none without in-app traffic; removed with the app and link.
func TestAnalyticsSources(t *testing.T) {
	s := newTestStore(t)
	app, link := setupApp(t, s)
	for i, src := range []string{"messenger", "messenger", "zalo", ""} {
		fp := Fingerprint{IP: fmt.Sprintf("203.0.113.%d", i), UserAgent: "ua"}
		if _, err := s.RecordClick(Click{AppID: app.ID, LinkID: link.ID, Destination: link.URL, Kind: KindApp, Source: src, Fingerprint: fp}, 24); err != nil {
			t.Fatalf("RecordClick: %v", err)
		}
	}
	a, err := s.Analytics(app.ID, 7, "", time.Now())
	if err != nil {
		t.Fatalf("Analytics: %v", err)
	}
	want := []SourceStat{{Source: "messenger", Count: 2}, {Source: "zalo", Count: 1}}
	if !reflect.DeepEqual(a.Sources, want) {
		t.Errorf("Sources = %+v; want %+v", a.Sources, want)
	}
	other, _ := s.CreateApp("quiet", "quiet-key-1")
	if q, _ := s.Analytics(other.ID, 7, "", time.Now()); q.Sources == nil || len(q.Sources) != 0 {
		t.Errorf("quiet app Sources = %#v; want empty slice", q.Sources)
	}
	if err := s.DeleteLink(link.ID); err != nil {
		t.Fatal(err)
	}
	if a, _ := s.Analytics(app.ID, 7, "", time.Now()); len(a.Sources) != 0 {
		t.Errorf("Sources after DeleteLink = %+v; want none", a.Sources)
	}
	l2, _ := s.CreateLink(Link{AppID: app.ID, Key: "again", URL: "https://example.com/a"})
	if _, err := s.RecordClick(Click{AppID: app.ID, LinkID: l2.ID, Destination: l2.URL, Kind: KindApp, Source: "zalo", Fingerprint: Fingerprint{IP: "198.51.100.9", UserAgent: "ua"}}, 24); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteApp(app.ID); err != nil {
		t.Fatal(err)
	}
	var n int
	s.db.QueryRow(`SELECT COUNT(*) FROM click_sources WHERE app_id = ?`, app.ID).Scan(&n)
	if n != 0 {
		t.Errorf("click_sources rows after DeleteApp = %d; want 0", n)
	}
}

// Reopen picks the newest in-app click on the visitor's platform, even when a newer one from the other platform shares the IP.
func TestRecordClickReopenSkipsOtherPlatform(t *testing.T) {
	s := newTestStore(t)
	app, link := setupApp(t, s)
	ip := "203.0.113.7"
	rec := func(ua, src string) Click {
		t.Helper()
		c, err := s.RecordClick(Click{AppID: app.ID, LinkID: link.ID, Destination: link.URL, Kind: KindApp, Source: src, Fingerprint: Fingerprint{IP: ip, UserAgent: ua}}, 24)
		if err != nil {
			t.Fatalf("RecordClick: %v", err)
		}
		return c
	}
	iosInApp := rec("Mozilla/5.0 (iPhone; CPU iPhone OS 17_2 like Mac OS X) Mobile/15E148 [FBAN/MessengerForiOS]", "messenger")
	rec("Mozilla/5.0 (Linux; Android 14; wv) [FB_IAB/MESSENGER;]", "messenger")
	reopen := rec("Mozilla/5.0 (iPhone; CPU iPhone OS 17_2 like Mac OS X) Version/17.2 Mobile/15E148 Safari/604.1", "")
	if reopen.ID != iosInApp.ID {
		t.Errorf("iOS reopen = %s; want merged into iOS in-app click %s", reopen.ID, iosInApp.ID)
	}
	if n, _ := s.CountClicks(app.ID); n != 2 {
		t.Errorf("clicks = %d; want 2", n)
	}
}

// Same-device retry that lost the claim race (install not written yet) gets the click; another device doesn't (M3, M4).
func TestMarkClickMatchedSameDevice(t *testing.T) {
	s := newTestStore(t)
	app, link := setupApp(t, s)
	c, err := s.RecordClick(Click{AppID: app.ID, LinkID: link.ID, Destination: link.URL, ClickID: "k-dev"}, 24)
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := s.MarkClickMatched(c.ID, "dev-a"); !ok || err != nil {
		t.Fatalf("first claim = %v, %v; want true", ok, err)
	}
	if ok, _ := s.MarkClickMatched(c.ID, "dev-a"); !ok {
		t.Error("same-device retry claim = false; want true")
	}
	if ok, _ := s.MarkClickMatched(c.ID, "dev-b"); ok {
		t.Error("other-device claim = true; want false")
	}
	if _, err := s.ClickByClickID(app.ID, "k-dev", "dev-a"); err != nil {
		t.Errorf("same-device clickId lookup after claim: %v; want the click", err)
	}
	if _, err := s.ClickByClickID(app.ID, "k-dev", "dev-b"); !errors.Is(err, ErrNotFound) {
		t.Errorf("other-device clickId lookup = %v; want ErrNotFound", err)
	}
}
