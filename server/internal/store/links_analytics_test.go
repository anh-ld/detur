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

// Tag maps a device to its first link; the tag marker is not a conversion; later events count for that link.
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

// Retention: one device counts once per mark, on exactly day 1, 7, 30; untagged devices count nothing.
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

// Devices = cohorts started in range; each DN uses cohorts whose mark day falls in range.
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
