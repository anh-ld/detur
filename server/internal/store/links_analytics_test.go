package store

import (
	"reflect"
	"testing"
	"time"
)

var linkDay0 = time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

func recordLinkEvent(t *testing.T, s *Store, e LinkEvent, now time.Time) {
	t.Helper()
	if err := s.RecordLinkEvent(e, now); err != nil {
		t.Fatalf("RecordLinkEvent(%+v): %v", e, err)
	}
}

// linkEvents: link_event_days as key/event -> n.
func linkEvents(t *testing.T, s *Store, appID string) map[string]int64 {
	t.Helper()
	rows, err := s.db.Query(`SELECT l.key, e.event, e.n FROM link_event_days e JOIN links l ON l.id = e.link_id WHERE e.app_id = ?`, appID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var k, ev string
		var n int64
		if err := rows.Scan(&k, &ev, &n); err != nil {
			t.Fatal(err)
		}
		out[k+"/"+ev] = n
	}
	return out
}

func cohort(t *testing.T, s *Store, linkID, day string) [4]int64 {
	t.Helper()
	var c [4]int64
	s.db.QueryRow(`SELECT devices, d1, d7, d30 FROM link_cohorts WHERE link_id = ? AND day = ?`, linkID, day).Scan(&c[0], &c[1], &c[2], &c[3])
	return c
}

// Tag maps device -> its first link; tag marker isn't a conversion; later events count for that link.
func TestRecordLinkEventConversions(t *testing.T) {
	s := newTestStore(t)
	app, abc := setupApp(t, s)
	if _, err := s.CreateLink(Link{AppID: app.ID, Key: "def", URL: "https://example.com/def"}); err != nil {
		t.Fatal(err)
	}
	recordLinkEvent(t, s, LinkEvent{AppID: app.ID, Device: "dev-1", LinkKey: "ABC", Event: LinkTag}, linkDay0)
	recordLinkEvent(t, s, LinkEvent{AppID: app.ID, Device: "dev-1", Event: "purchase"}, linkDay0)
	recordLinkEvent(t, s, LinkEvent{AppID: app.ID, Device: "dev-1", LinkKey: "def", Event: "signup"}, linkDay0) // first tag wins
	recordLinkEvent(t, s, LinkEvent{AppID: app.ID, Device: "dev-2", LinkKey: "nope", Event: "purchase"}, linkDay0)
	recordLinkEvent(t, s, LinkEvent{AppID: app.ID, LinkKey: "abc", Event: "purchase"}, linkDay0)
	recordLinkEvent(t, s, LinkEvent{AppID: app.ID, Device: "dev-3", Event: "purchase"}, linkDay0)

	want := map[string]int64{"abc/purchase": 1, "abc/signup": 1}
	if got := linkEvents(t, s, app.ID); !reflect.DeepEqual(got, want) {
		t.Errorf("link_event_days = %v; want %v", got, want)
	}
	if got := cohort(t, s, abc.ID, "2026-03-01"); got != [4]int64{1, 0, 0, 0} {
		t.Errorf("cohort = %v; want 1 device", got)
	}
	var raw int
	s.db.QueryRow(`SELECT COUNT(*) FROM device_links WHERE device = 'dev-1'`).Scan(&raw)
	if raw != 0 {
		t.Error("device_links stores the raw device id; want hash")
	}
}

// Retention: device counts once per mark, on exactly day 1, 7, 30; untagged devices count nothing.
func TestRecordLinkEventRetention(t *testing.T) {
	s := newTestStore(t)
	app, abc := setupApp(t, s)
	recordLinkEvent(t, s, LinkEvent{AppID: app.ID, Device: "dev-1", LinkKey: "abc", Event: LinkTag}, linkDay0)
	for _, d := range []int{0, 1, 1, 2, 7, 30, 31} {
		recordLinkEvent(t, s, LinkEvent{AppID: app.ID, Device: "dev-1", Event: "app_open", Retention: true}, linkDay0.AddDate(0, 0, d))
		recordLinkEvent(t, s, LinkEvent{AppID: app.ID, Device: "dev-9", Event: "app_open", Retention: true}, linkDay0.AddDate(0, 0, d))
	}
	if got := cohort(t, s, abc.ID, "2026-03-01"); got != [4]int64{1, 1, 1, 1} {
		t.Errorf("cohort devices/d1/d7/d30 = %v; want [1 1 1 1]", got)
	}
	if got := linkEvents(t, s, app.ID); len(got) != 0 {
		t.Errorf("retention calls counted as conversions: %v", got)
	}
}

// Mapping purged at 90d; rollups stay. DeleteLink and DeleteApp drop all three tables.
func TestLinkAnalyticsLifecycle(t *testing.T) {
	s := newTestStore(t)
	app, abc := setupApp(t, s)
	def, _ := s.CreateLink(Link{AppID: app.ID, Key: "def", URL: "https://example.com/def"})
	recordLinkEvent(t, s, LinkEvent{AppID: app.ID, Device: "dev-1", LinkKey: "abc", Event: "signup"}, linkDay0)

	count := func(table, linkID string) (n int) {
		s.db.QueryRow(`SELECT COUNT(*) FROM `+table+` WHERE link_id = ?`, linkID).Scan(&n)
		return n
	}
	if _, err := s.PurgeExpired(linkDay0.AddDate(0, 0, 89), 24); err != nil {
		t.Fatal(err)
	}
	if count("device_links", abc.ID) != 1 {
		t.Error("mapping purged before 90 days")
	}
	if _, err := s.PurgeExpired(linkDay0.AddDate(0, 0, 91), 24); err != nil {
		t.Fatal(err)
	}
	if count("device_links", abc.ID) != 0 || count("link_cohorts", abc.ID) != 1 || count("link_event_days", abc.ID) != 1 {
		t.Error("after 91 days: want mapping gone, rollups kept")
	}

	recordLinkEvent(t, s, LinkEvent{AppID: app.ID, Device: "dev-3", LinkKey: "abc", Event: "signup"}, linkDay0)
	recordLinkEvent(t, s, LinkEvent{AppID: app.ID, Device: "dev-2", LinkKey: "def", Event: "signup"}, linkDay0)
	if err := s.DeleteLink(abc.ID); err != nil {
		t.Fatal(err)
	}
	for _, tbl := range []string{"device_links", "link_cohorts", "link_event_days"} {
		if count(tbl, abc.ID) != 0 {
			t.Errorf("%s rows left for deleted link", tbl)
		}
		if count(tbl, def.ID) != 1 {
			t.Errorf("%s rows for other link = %d; want 1", tbl, count(tbl, def.ID))
		}
	}
	if err := s.DeleteApp(app.ID); err != nil {
		t.Fatal(err)
	}
	for _, tbl := range []string{"device_links", "link_cohorts", "link_event_days"} {
		if count(tbl, def.ID) != 0 {
			t.Errorf("%s rows left after DeleteApp", tbl)
		}
	}
}

// Devices = cohorts started in range; each DN = cohorts whose mark day is in range.
func TestAnalyticsLinkRetentionAndConversions(t *testing.T) {
	s := newTestStore(t)
	app, _ := setupApp(t, s)
	now := linkDay0
	recordLinkEvent(t, s, LinkEvent{AppID: app.ID, Device: "new", LinkKey: "abc", Event: "purchase"}, now.AddDate(0, 0, -3))
	recordLinkEvent(t, s, LinkEvent{AppID: app.ID, Device: "new", Event: "purchase"}, now.AddDate(0, 0, -3))
	recordLinkEvent(t, s, LinkEvent{AppID: app.ID, Device: "new", Event: "app_open", Retention: true}, now.AddDate(0, 0, -2))
	recordLinkEvent(t, s, LinkEvent{AppID: app.ID, Device: "old", LinkKey: "abc", Event: "signup"}, now.AddDate(0, 0, -10))
	recordLinkEvent(t, s, LinkEvent{AppID: app.ID, Device: "old", Event: "app_open", Retention: true}, now.AddDate(0, 0, -3))
	stale, _ := s.CreateLink(Link{AppID: app.ID, Key: "stale", URL: "https://example.com/s"})
	recordLinkEvent(t, s, LinkEvent{AppID: app.ID, Device: "gone", LinkKey: stale.Key, Event: "signup"}, now.AddDate(0, 0, -20)) // no mark day in range

	a, err := s.Analytics(app.ID, 7, "", now)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Retention) != 1 {
		t.Fatalf("Retention = %+v; want one link", a.Retention)
	}
	r := a.Retention[0]
	if r.Key != "abc" || r.Devices != 1 || r.D1 != (Mark{1, 1}) || r.D7 != (Mark{1, 1}) || r.D30 != (Mark{}) {
		t.Errorf("Retention = %+v; want abc devices 1, d1 1/1, d7 1/1, d30 0/0", r)
	}
	want := []LinkConversion{{LinkID: r.LinkID, Key: "abc", Event: "purchase", Count: 2}}
	if !reflect.DeepEqual(a.Conversions, want) {
		t.Errorf("Conversions = %+v; want %+v (signup is 10 days old, out of range)", a.Conversions, want)
	}

	other, _ := s.CreateApp("quiet", "quiet-key-1")
	q, _ := s.Analytics(other.ID, 7, "", now)
	if q.Retention == nil || q.Conversions == nil || len(q.Retention)+len(q.Conversions) != 0 {
		t.Errorf("quiet app = %#v / %#v; want empty slices", q.Retention, q.Conversions)
	}
}

// install date filters: [from, to] both ends inclusive by UTC day; later installs never leak into an earlier range
func TestInstallDateRangeBounds(t *testing.T) {
	s := newTestStore(t)
	app, _ := setupApp(t, s)
	at := func(dev, ts string) {
		in, err := s.RecordInstall(Install{AppID: app.ID, DeviceHash: dev, Attribution: AttributionOrganic, Method: MethodOrganic})
		if err != nil {
			t.Fatalf("RecordInstall: %v", err)
		}
		if _, err := s.db.Exec(`UPDATE installs SET created_at = ? WHERE id = ?`, ts, in.ID); err != nil {
			t.Fatalf("backdate: %v", err)
		}
	}
	at("d0", "2026-01-09T23:59:59Z") // before
	at("d1", "2026-01-10T00:00:00Z")
	at("d2", "2026-01-15T23:59:59Z")
	at("d3", "2026-01-16T00:00:00Z") // after
	to := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	q, err := s.MatchQuality(app.ID, 6, to)
	if err != nil {
		t.Fatalf("MatchQuality: %v", err)
	}
	if n := q.Methods[MethodOrganic]; n != 2 {
		t.Errorf("MatchQuality organic = %d; want 2 (Jan 10–15 only)", n)
	}
	a, err := s.AnalyticsRange(app.ID, time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC), to, "")
	if err != nil {
		t.Fatalf("AnalyticsRange: %v", err)
	}
	var organic int64
	for _, d := range a.Days {
		organic += d.Organic
	}
	if organic != 2 {
		t.Errorf("AnalyticsRange organic = %d; want 2", organic)
	}
}

func TestAnalyticsSubDayAndLinkFilter(t *testing.T) {
	s := newTestStore(t)
	app, _ := setupApp(t, s)

	linkA, err := s.CreateLink(Link{AppID: app.ID, Key: "alice", URL: "/alice", Title: "Alice Promo"})
	if err != nil {
		t.Fatal(err)
	}
	linkB, err := s.CreateLink(Link{AppID: app.ID, Key: "bob", URL: "/bob", Title: "Bob Promo"})
	if err != nil {
		t.Fatal(err)
	}

	// Clicks: linkA (2 clicks with distinct IPs), linkB (1 click)
	cA1, _ := s.RecordClick(Click{AppID: app.ID, LinkID: linkA.ID, Destination: linkA.URL, Platform: "ios", Fingerprint: Fingerprint{IP: "1.1.1.1"}}, 24)
	cA2, _ := s.RecordClick(Click{AppID: app.ID, LinkID: linkA.ID, Destination: linkA.URL, Platform: "ios", Source: "messenger", Fingerprint: Fingerprint{IP: "2.2.2.2"}}, 24)
	cB, _ := s.RecordClick(Click{AppID: app.ID, LinkID: linkB.ID, Destination: linkB.URL, Platform: "android", Fingerprint: Fingerprint{IP: "3.3.3.3"}}, 24)

	// Installs: linkA (1 non-organic), 1 organic
	iA, _ := s.RecordInstall(Install{AppID: app.ID, LinkID: linkA.ID, DeviceHash: "dev-a", Platform: "ios", Attribution: AttributionNonOrganic})
	iOrg, _ := s.RecordInstall(Install{AppID: app.ID, DeviceHash: "dev-org", Attribution: AttributionOrganic})

	now := time.Now().UTC()
	// Set exact timestamps within the last 5 minutes
	s.db.Exec(`UPDATE clicks SET created_at = ? WHERE id = ?`, rfc3339(now.Add(-4*time.Minute)), cA1.ID)
	s.db.Exec(`UPDATE clicks SET created_at = ? WHERE id = ?`, rfc3339(now.Add(-2*time.Minute)), cA2.ID)
	s.db.Exec(`UPDATE clicks SET created_at = ? WHERE id = ?`, rfc3339(now.Add(-3*time.Minute)), cB.ID)
	s.db.Exec(`UPDATE installs SET created_at = ? WHERE id = ?`, rfc3339(now.Add(-2*time.Minute)), iA.ID)
	s.db.Exec(`UPDATE installs SET created_at = ? WHERE id = ?`, rfc3339(now.Add(-1*time.Minute)), iOrg.ID)

	// Events
	s.RecordEvent(app.ID, "app_open", `{}`)

	// 1. Sub-day 15m (all links)
	a15, err := s.AnalyticsSubDay(app.ID, "15m", "", "", now)
	if err != nil {
		t.Fatalf("AnalyticsSubDay 15m: %v", err)
	}
	if len(a15.Days) != 15 {
		t.Fatalf("15m len(Days) = %d; want 15", len(a15.Days))
	}
	var c15, o15, no15 int64
	for _, d := range a15.Days {
		c15 += d.Clicks
		o15 += d.Organic
		no15 += d.NonOrganic
	}
	if c15 != 3 || o15 != 1 || no15 != 1 {
		t.Errorf("15m totals: clicks=%d (want 3), organic=%d (want 1), nonOrganic=%d (want 1)", c15, o15, no15)
	}
	if len(a15.Links) != 3 || a15.Links[0].Title != "Alice Promo" || a15.Links[0].Clicks != 2 || a15.Links[0].Matches != 1 {
		t.Errorf("15m links = %+v", a15.Links)
	}
	if len(a15.Sources) != 1 || a15.Sources[0].Source != "messenger" || a15.Sources[0].Count != 1 {
		t.Errorf("15m sources = %+v", a15.Sources)
	}

	// 2. Sub-day 15m filtered by linkA
	a15A, err := s.AnalyticsSubDay(app.ID, "15m", "", linkA.ID, now)
	if err != nil {
		t.Fatalf("AnalyticsSubDay 15m linkA: %v", err)
	}
	var c15A, o15A, no15A int64
	for _, d := range a15A.Days {
		c15A += d.Clicks
		o15A += d.Organic
		no15A += d.NonOrganic
	}
	if c15A != 2 || o15A != 0 || no15A != 1 {
		t.Errorf("15m linkA totals: clicks=%d (want 2), organic=%d (want 0), nonOrganic=%d (want 1)", c15A, o15A, no15A)
	}
	if len(a15A.Links) != 1 || a15A.Links[0].Key != "alice" {
		t.Errorf("15m linkA links = %+v; want only alice", a15A.Links)
	}

	// 3. Other sub-day windows bucket counts
	for _, win := range []struct {
		w string
		b int
	}{{"1h", 12}, {"3h", 18}, {"12h", 24}} {
		aw, err := s.AnalyticsSubDay(app.ID, win.w, "", "", now)
		if err != nil || len(aw.Days) != win.b {
			t.Errorf("window %s len(Days) = %d, err = %v; want %d", win.w, len(aw.Days), err, win.b)
		}
	}

	// 4. AnalyticsRange filtered by linkA
	arA, err := s.AnalyticsRange(app.ID, now.AddDate(0, 0, -1), now, "", linkA.ID)
	if err != nil {
		t.Fatalf("AnalyticsRange linkA: %v", err)
	}
	if len(arA.Links) != 1 || arA.Links[0].Key != "alice" || arA.Links[0].Title != "Alice Promo" {
		t.Errorf("AnalyticsRange linkA links = %+v", arA.Links)
	}

	// 5. link-scoped events: range reads link_event_days (that link only); window has no link column -> empty
	recordLinkEvent(t, s, LinkEvent{AppID: app.ID, Device: "d1", LinkKey: "alice", Event: LinkTag}, now)
	recordLinkEvent(t, s, LinkEvent{AppID: app.ID, Device: "d1", Event: "buy"}, now)
	recordLinkEvent(t, s, LinkEvent{AppID: app.ID, Device: "d2", LinkKey: "bob", Event: LinkTag}, now)
	recordLinkEvent(t, s, LinkEvent{AppID: app.ID, Device: "d2", Event: "share"}, now)
	arA, err = s.AnalyticsRange(app.ID, now.AddDate(0, 0, -1), now, "", linkA.ID)
	if err != nil || len(arA.Events) != 1 || arA.Events[0].Event != "buy" {
		t.Errorf("AnalyticsRange linkA events = %+v, err = %v; want only buy", arA.Events, err)
	}
	if ev, err := s.AnalyticsSubDay(app.ID, "15m", "", "", time.Now()); err != nil || len(ev.Events) == 0 {
		t.Errorf("15m app-wide events = %+v, err = %v; want app_open", ev.Events, err)
	}
	if ev, _ := s.AnalyticsSubDay(app.ID, "15m", "", linkA.ID, time.Now()); len(ev.Events) != 0 {
		t.Errorf("15m linkA events = %+v; want none", ev.Events)
	}
	// 6. platform filter in window mode: only the android click
	aAnd, err := s.AnalyticsSubDay(app.ID, "15m", "android", "", now)
	if err != nil {
		t.Fatal(err)
	}
	var cAnd int64
	for _, d := range aAnd.Days {
		cAnd += d.Clicks
	}
	if cAnd != 1 || len(aAnd.Links) != 3 || aAnd.Links[0].Key != "bob" {
		t.Errorf("15m android: clicks=%d links=%+v; want 1 click, bob first", cAnd, aAnd.Links)
	}
}

// Refresh across a UTC day moves the click_days rollup with created_at; pre-upgrade NULL platform backfilled from UA at Open.
func TestRefreshClickMovesRollupAndBackfillsPlatform(t *testing.T) {
	path := t.TempDir() + "/p.db"
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	app, link := setupApp(t, s)
	fp := Fingerprint{IP: "9.9.9.9", UserAgent: "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0)"}
	late := time.Date(2026, 3, 1, 23, 59, 0, 0, time.UTC)
	c1 := Click{AppID: app.ID, LinkID: link.ID, Destination: link.URL, Platform: "ios", Kind: KindApp, Fingerprint: fp}
	if _, err := s.recordClickAt(c1, 24, late); err != nil {
		t.Fatal(err)
	}
	if _, err := s.recordClickAt(c1, 24, late.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	var d1, d2 int64
	s.db.QueryRow(`SELECT COALESCE(SUM(n), 0) FROM click_days WHERE day = '2026-03-01'`).Scan(&d1)
	s.db.QueryRow(`SELECT COALESCE(SUM(n), 0) FROM click_days WHERE day = '2026-03-02'`).Scan(&d2)
	if d1 != 0 || d2 != 1 {
		t.Errorf("rollup after refresh: day1=%d day2=%d; want 0, 1", d1, d2)
	}
	// legacy rows: NULL platform, UA present -> backfilled; scrubbed UA stays NULL
	s.db.Exec(`UPDATE clicks SET platform = NULL`)
	for id, c := range map[string][2]string{"desk": {"Mozilla/5.0 (Macintosh; Intel Mac OS X)", KindWeb}, "open": {"MyApp/1 CFNetwork/1494 Darwin/23", KindOpen}} {
		s.db.Exec(`INSERT INTO clicks (id, app_id, link_id, destination, user_agent, kind, expires_at) VALUES (?, ?, ?, 'x', ?, ?, '2999-01-01T00:00:00.000Z')`, id, app.ID, link.ID, c[0], c[1])
	}
	s.db.Exec(`INSERT INTO clicks (id, app_id, link_id, destination, expires_at) VALUES ('scrubbed', ?, ?, 'x', '2999-01-01T00:00:00.000Z')`, app.ID, link.ID)
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var desk, ios, nulls int
	s.db.QueryRow(`SELECT COUNT(*) FROM clicks WHERE platform = 'desktop' AND id = 'desk'`).Scan(&desk)
	s.db.QueryRow(`SELECT COUNT(*) FROM clicks WHERE platform = 'ios'`).Scan(&ios)
	s.db.QueryRow(`SELECT COUNT(*) FROM clicks WHERE platform IS NULL`).Scan(&nulls)
	if desk != 1 || ios != 2 || nulls != 1 { // ios: browser iPhone click + kind=open CFNetwork
		t.Errorf("backfill: desktop=%d ios=%d null=%d; want 1, 2, 1", desk, ios, nulls)
	}
}
