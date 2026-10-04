package store

import (
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"
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
	got, err := s.ClickByClickID(app.ID, "play-click-42")
	if err != nil || got.ID != c.ID {
		t.Fatalf("ClickByClickID = %+v, %v", got, err)
	}
	if _, err := s.ClickByClickID(app.ID, "missing"); !errors.Is(err, ErrNotFound) {
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
	if ok, err := s.MarkClickMatched(c.ID); !ok || err != nil {
		t.Fatalf("first MarkClickMatched = %v, %v; want true", ok, err)
	}
	if ok, err := s.MarkClickMatched(c.ID); ok || err != nil {
		t.Fatalf("second MarkClickMatched = %v, %v; want false", ok, err)
	}
	if cs, err := s.ClicksSince(app.ID, time.Now().Add(-time.Hour)); err != nil || len(cs) != 0 {
		t.Errorf("ClicksSince = %d, %v; want 0 after match", len(cs), err)
	}
	if _, err := s.ClickByClickID(app.ID, "k1"); !errors.Is(err, ErrNotFound) {
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
INSERT INTO links (id, app_id, key, url) VALUES ('legacy-link', 'legacy-app', 'old', 'https://example.com/old');`)
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
	if !columnExists(s.db, "clicks", "matched_at") {
		t.Error("clicks.matched_at missing after Open")
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
