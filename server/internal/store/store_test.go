package store

import (
	"database/sql"
	"errors"
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
	if links[0].Threshold != 0 || links[0].WindowMinutes != 0 {
		t.Errorf("unset link settings = %d/%d, want 0/0 (global applies)", links[0].Threshold, links[0].WindowMinutes)
	}

	l, err := s.GetLinkByKey(app.ID, "abc")
	if err != nil || l.URL != "https://example.com/product" {
		t.Fatalf("GetLinkByKey = %+v, %v", l, err)
	}

	l.URL = "https://example.com/new"
	l.Threshold = 1000
	if err := s.UpdateLink(l); err != nil {
		t.Fatalf("UpdateLink: %v", err)
	}
	got, err := s.GetLink(l.ID)
	if err != nil || got.URL != "https://example.com/new" || got.Threshold != 1000 {
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
	c, err := s.RecordClick(Click{AppID: app.ID, LinkID: link.ID, Fingerprint: fp, Destination: link.URL}, 15, 24)
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

func TestClickExpiryRespectsWindowAndFloor(t *testing.T) {
	s := newTestStore(t)
	app, link := setupApp(t, s)
	c, err := s.RecordClick(Click{AppID: app.ID, LinkID: link.ID, Destination: link.URL}, 180, 24)
	if err != nil {
		t.Fatalf("RecordClick: %v", err)
	}
	got, _ := s.GetClick(c.ID)
	if got.ExpiresAt.Sub(got.CreatedAt) < 179*time.Minute {
		t.Errorf("window 180 not respected: %v", got.ExpiresAt.Sub(got.CreatedAt))
	}
	// window smaller than floor → floor wins
	c2, _ := s.RecordClick(Click{AppID: app.ID, LinkID: link.ID, Destination: link.URL}, 5, 24)
	got2, _ := s.GetClick(c2.ID)
	if got2.ExpiresAt.Sub(got2.CreatedAt) < 23*time.Hour {
		t.Errorf("floor 24h not respected for 5-min window: %v", got2.ExpiresAt.Sub(got2.CreatedAt))
	}
}

// RecordClick: reuses previous hour's click for same link + device (IP + user agent), like Dub's click cache; different device records new row.
func TestRecordClickDedupSameDeviceWithinHour(t *testing.T) {
	s := newTestStore(t)
	app, link := setupApp(t, s)
	fp := Fingerprint{IP: "203.0.113.7", UserAgent: "Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X)"}
	first, err := s.RecordClick(Click{AppID: app.ID, LinkID: link.ID, Fingerprint: fp, Destination: link.URL}, 15, 24)
	if err != nil {
		t.Fatalf("RecordClick: %v", err)
	}
	second, err := s.RecordClick(Click{AppID: app.ID, LinkID: link.ID, Fingerprint: fp, Destination: link.URL}, 15, 24)
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
	third, err := s.RecordClick(Click{AppID: app.ID, LinkID: link.ID, Fingerprint: other, Destination: link.URL}, 15, 24)
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

func TestDeterministicLookupByClickID(t *testing.T) {
	s := newTestStore(t)
	app, link := setupApp(t, s)
	c, err := s.RecordClick(Click{AppID: app.ID, LinkID: link.ID, Destination: link.URL, ClickID: "play-click-42"}, 15, 24)
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

func TestPurgeExpired(t *testing.T) {
	s := newTestStore(t)
	app, link := setupApp(t, s)
	c, _ := s.RecordClick(Click{AppID: app.ID, LinkID: link.ID, Destination: link.URL}, 15, 24)
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
	var preserved int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM installs WHERE id = ? AND app_id = ?`, "legacy", "app-a").Scan(&preserved); err != nil || preserved != 1 {
		t.Fatalf("legacy row preserved = %d, %v; want 1", preserved, err)
	}
}

func TestRecordEventAndSettings(t *testing.T) {
	s := newTestStore(t)
	app, _ := setupApp(t, s)
	if err := s.RecordEvent(app.ID, "install", `{"source":"play"}`); err != nil {
		t.Fatalf("RecordEvent: %v", err)
	}
	if v, ok, err := s.GetSetting("threshold"); err != nil || ok {
		t.Fatalf("GetSetting(unset) = %q, %v, %v", v, ok, err)
	}
	if err := s.SetSetting("threshold", "950"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	if v, ok, _ := s.GetSetting("threshold"); !ok || v != "950" {
		t.Errorf("GetSetting = %q, %v; want 950,true", v, ok)
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
