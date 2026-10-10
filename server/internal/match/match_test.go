package match

// Test scenarios 1-20 for the matching engine.

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite" // SQLite driver: tests backdate click timestamps directly

	"detur.dev/server/internal/fraud"
	"detur.dev/server/internal/store"
)

const (
	testIP  = "203.0.113.7"
	otherIP = "198.51.100.9"
)

// testLink: bare link for pure Score tests (no store required).
func testLink() store.Link {
	return store.Link{ID: "l1", AppID: "a1", URL: "https://example.com/product"}
}

// androidClick: click with every Android-scorable signal: IP, model+system version in UA, locale, timezone, screen.
func androidClick(link store.Link) store.Click {
	return store.Click{
		AppID: link.AppID, LinkID: link.ID, Destination: link.URL,
		Fingerprint: store.Fingerprint{
			IP: testIP, Device: "Pixel 7", Locale: "en", Timezone: "Europe/Warsaw",
			Screen:    "393x852@3",
			UserAgent: "Mozilla/5.0 (Linux; Android 14; Pixel 7 Build/TQ3A.230805.001) AppleWebKit/537.36",
		},
	}
}

// iosClick: click with every iOS-scorable signal: IP, iOS system version in UA, locale, timezone, screen, pasted link.
func iosClick(link store.Link) store.Click {
	return store.Click{
		AppID: link.AppID, LinkID: link.ID, Destination: link.URL,
		Fingerprint: store.Fingerprint{
			IP: testIP, Device: "iPhone 15", Locale: "en", Timezone: "Europe/Warsaw",
			Screen:     "393x852@3",
			UserAgent:  "Mozilla/5.0 (iPhone; CPU iPhone OS 17_2 like Mac OS X) AppleWebKit/605.1.15",
			PastedLink: "https://lnk.example/abc123",
		},
	}
}

func androidFP() Fingerprint {
	return Fingerprint{
		Model: "Pixel 7", SystemVersion: "14",
		ScreenWidth: 393, ScreenHeight: 852, Scale: 3,
		Locale: "en-US", Timezone: "Europe/Warsaw",
		UserAgent: "Mozilla/5.0 (Linux; Android 14; Pixel 7 Build/TQ3A.230805.001)",
	}
}

func iosFP() Fingerprint {
	return Fingerprint{
		Model: "iPhone 15", SystemVersion: "17.2",
		ScreenWidth: 393, ScreenHeight: 852, Scale: 3,
		Locale: "en-US", Timezone: "Europe/Warsaw",
		UserAgent:  "Mozilla/5.0 (iPhone; CPU iPhone OS 17.2 like Mac OS X)",
		PastedLink: "https://lnk.example/abc123",
	}
}

func fpPtr(f Fingerprint) *Fingerprint { return &f }

func newTestStore(t *testing.T) (*store.Store, string) {
	t.Helper()
	path := t.TempDir() + "/detur-test.db"
	s, err := store.Open(path)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s, path
}

func setupApp(t *testing.T, s *store.Store) (store.App, store.Link) {
	t.Helper()
	app, err := s.CreateApp("match test app", "key-123")
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

// recordClick: persist click, created_at = now.
func recordClick(t *testing.T, s *store.Store, c store.Click) store.Click {
	t.Helper()
	got, err := s.RecordClick(c, 24)
	if err != nil {
		t.Fatalf("RecordClick: %v", err)
	}
	return got
}

// recordClickBackdated: persist click, then rewrite created_at into past. RecordClick always stamps now, so window-edge tests adjust row via second connection (WAL allows it).
func recordClickBackdated(t *testing.T, s *store.Store, dbPath string, c store.Click, d time.Duration) store.Click {
	t.Helper()
	got := recordClick(t, s, c)
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()
	ts := time.Now().UTC().Add(-d).Format(time.RFC3339Nano)
	if _, err := db.Exec(`UPDATE clicks SET created_at = ? WHERE id = ?`, ts, got.ID); err != nil {
		t.Fatalf("backdate click %s: %v", got.ID, err)
	}
	return got
}

// Scenario 1: exact clickId resolves regardless of score.
func TestDeterministicClickIDWinsRegardlessOfScore(t *testing.T) {
	s, _ := newTestStore(t)
	_, link := setupApp(t, s)
	c := androidClick(link)
	c.ClickID = "play-click-1"
	recordClick(t, s, c)
	// A fingerprint that would score 0 (no IP match, every signal mismatched).
	fp := Fingerprint{Model: "Pixel 9", SystemVersion: "99", Locale: "fr",
		Timezone: "UTC", UserAgent: "Mozilla/5.0 (X11; Linux x86_64)"}
	res, err := Match(s, link.AppID, Request{ClickID: "play-click-1", IP: otherIP, Fingerprint: &fp})
	if err != nil {
		t.Fatalf("Match: %v", err)
	}
	if !res.Matched || res.Destination != link.URL {
		t.Errorf("deterministic match = %+v; want matched with destination %s", res, link.URL)
	}
}

// Scenario 2: clickId present but unknown -> no-match, no probabilistic fallback.
func TestUnknownClickIDNoProbabilisticFallback(t *testing.T) {
	s, _ := newTestStore(t)
	_, link := setupApp(t, s)
	// A recent, strongly matching click sits inside the window...
	recordClick(t, s, androidClick(link))
	// ...but the request carries an unknown clickId: deterministic path only.
	res, err := Match(s, link.AppID, Request{ClickID: "unknown-click", IP: testIP, Fingerprint: fpPtr(androidFP())})
	if err != nil {
		t.Fatalf("Match: %v", err)
	}
	if res.Matched {
		t.Errorf("unknown clickId fell through to probabilistic match: %+v", res)
	}
}

// Scenario 3: score above threshold -> match; returns the click destination.
func TestScoreAboveThresholdMatches(t *testing.T) {
	s, _ := newTestStore(t)
	_, link := setupApp(t, s)
	recordClick(t, s, androidClick(link))
	res, err := Match(s, link.AppID, Request{IP: testIP, Fingerprint: fpPtr(androidFP())})
	if err != nil {
		t.Fatalf("Match: %v", err)
	}
	if !res.Matched || res.Destination != link.URL || res.Click.ID == "" {
		t.Errorf("match = %+v; want matched click with destination %s", res, link.URL)
	}
}

// Scenario 4: score below threshold -> no match. Mirrors docs' rejection example: timezone+language+screen = 200+100+200 = 500.
func TestScoreBelowThresholdNoMatch(t *testing.T) {
	s, _ := newTestStore(t)
	_, link := setupApp(t, s)
	recordClick(t, s, androidClick(link))
	fp := Fingerprint{Timezone: "Europe/Warsaw", Locale: "en-US",
		ScreenWidth: 393, ScreenHeight: 852, Scale: 3}
	res, err := Match(s, link.AppID, Request{IP: otherIP, Fingerprint: &fp})
	if err != nil {
		t.Fatalf("Match: %v", err)
	}
	if res.Matched {
		t.Errorf("score 500 matched at threshold 850: %+v", res)
	}
}

// Scenario 5: score exactly at threshold -> match. Mirrors docs' example "iOS: IP + exact pasteboard token and URL, 500 + 350 = 850".
func TestScoreAtThresholdMatches(t *testing.T) {
	s, _ := newTestStore(t)
	_, link := setupApp(t, s)
	recordClick(t, s, iosClick(link))
	fp := Fingerprint{PastedLink: "https://lnk.example/abc123"}
	res, err := Match(s, link.AppID, Request{IP: testIP, Fingerprint: &fp})
	if err != nil {
		t.Fatalf("Match: %v", err)
	}
	if !res.Matched {
		t.Errorf("score 850 (IP+pasteboard) rejected at threshold 850: %+v", res)
	}
}

// Scenario 6a: window edge, click 14:59 min ago is inside the 15-min window.
func TestWindowEdgeInsideMatches(t *testing.T) {
	s, path := newTestStore(t)
	_, link := setupApp(t, s)
	recordClickBackdated(t, s, path, androidClick(link), 14*time.Minute+59*time.Second)
	res, err := Match(s, link.AppID, Request{IP: testIP, Fingerprint: fpPtr(androidFP())})
	if err != nil {
		t.Fatalf("Match: %v", err)
	}
	if !res.Matched {
		t.Errorf("click 14:59 min ago not matched: %+v", res)
	}
}

// Scenario 6b: window edge, click 15:01 min ago is outside the 15-min window.
func TestWindowEdgeOutsideNoMatch(t *testing.T) {
	s, path := newTestStore(t)
	_, link := setupApp(t, s)
	recordClickBackdated(t, s, path, androidClick(link), 15*time.Minute+1*time.Second)
	res, err := Match(s, link.AppID, Request{IP: testIP, Fingerprint: fpPtr(androidFP())})
	if err != nil {
		t.Fatalf("Match: %v", err)
	}
	if res.Matched {
		t.Errorf("click 15:01 min ago matched: %+v", res)
	}
}

// Scenario 7: stored out-of-range window errors at read time; deterministic path never reads settings.
func TestWindowRangeValidation(t *testing.T) {
	s, _ := newTestStore(t)
	app, link := setupApp(t, s)
	for _, w := range []int{4, 181} {
		if err := s.UpdateAppMatchSettings(app.ID, 850, w); err != nil {
			t.Fatalf("UpdateAppMatchSettings: %v", err)
		}
		_, err := Match(s, link.AppID, Request{IP: testIP, Fingerprint: fpPtr(androidFP())})
		if !errors.Is(err, ErrWindowOutOfRange) {
			t.Errorf("window %d: err = %v; want ErrWindowOutOfRange", w, err)
		}
		if _, err := Match(s, link.AppID, Request{ClickID: "nope"}); err != nil {
			t.Errorf("window %d: clickId path err = %v; want nil", w, err)
		}
	}
}

// Scenario 9: deterministic clickId lookup succeeds beyond window (24h retention floor; deterministic matching has no window).
func TestDeterministicClickIDBeyondWindow(t *testing.T) {
	s, path := newTestStore(t)
	_, link := setupApp(t, s)
	c := androidClick(link)
	c.ClickID = "play-old-1"
	recordClickBackdated(t, s, path, c, 30*time.Minute) // beyond the 15-min window
	res, err := Match(s, link.AppID, Request{ClickID: "play-old-1"})
	if err != nil {
		t.Fatalf("Match: %v", err)
	}
	if !res.Matched {
		t.Errorf("deterministic lookup outside window failed: %+v", res)
	}
}

// Scenario 10: tie (equal score) -> newer click wins.
func TestTieBreaksToNewerClick(t *testing.T) {
	s, path := newTestStore(t)
	_, link := setupApp(t, s)
	old := recordClickBackdated(t, s, path, androidClick(link), 2*time.Minute)
	rec := recordClick(t, s, androidClick(link)) // identical fingerprint, newer
	res, err := Match(s, link.AppID, Request{IP: testIP, Fingerprint: fpPtr(androidFP())})
	if err != nil {
		t.Fatalf("Match: %v", err)
	}
	if !res.Matched || res.Click.ID != rec.ID {
		t.Errorf("tie winner = %s (matched %v); want newer click %s (old %s)",
			res.Click.ID, res.Matched, rec.ID, old.ID)
	}
}

// Scenario 11: Android model+systemVersion 450 applied; UA device signature NOT added too (one-device-signal rule).
func TestAndroidModelSystemVersionWeight(t *testing.T) {
	link := testLink()
	if s := Score(androidClick(link), androidFP(), testIP); s != 1450 {
		t.Errorf("android full score = %d; want 1450 (450 device signal, no UA double-count)", s)
	}
	// Model mismatch: 450 branch chosen but fails -> 0 device, UA signature not stacked on top.
	bad := androidFP()
	bad.Model = "Pixel 8"
	if s := Score(androidClick(link), bad, testIP); s != 1000 {
		t.Errorf("android model mismatch = %d; want 1000 (500 IP + 200 tz + 200 screen + 100 lang)", s)
	}
}

// Scenario 12: iOS systemVersion 350 applied; UA signature not double-counted.
func TestIOSSystemVersionWeight(t *testing.T) {
	link := testLink()
	if s := Score(iosClick(link), iosFP(), testIP); s != 1700 {
		t.Errorf("ios full score = %d; want 1700 (350 sysver, no UA double-count)", s)
	}
	bad := iosFP()
	bad.SystemVersion = "16.6"
	if s := Score(iosClick(link), bad, testIP); s != 1350 {
		t.Errorf("ios sysver mismatch = %d; want 1350 (350 sysver dropped)", s)
	}
}

// Scenario 13: UA device signature 350 used only as fallback when platform device signal unavailable (here: Android UA without derivable OS version).
func TestUADeviceSignatureFallback(t *testing.T) {
	link := testLink()
	c := androidClick(link)
	c.Fingerprint.UserAgent = "Mozilla/5.0 (Linux; Android; Pixel 7 Build/TQ3A.230805.001)"
	if s := Score(c, androidFP(), testIP); s != 1350 {
		t.Errorf("UA fallback score = %d; want 1350 (500 IP + 350 UA sig + 200 tz + 200 screen + 100 lang)", s)
	}
	bad := androidFP()
	bad.Model = "Pixel 8"
	if s := Score(c, bad, testIP); s != 1000 {
		t.Errorf("UA fallback mismatch = %d; want 1000", s)
	}
}

// Scenario 14: pasteboard tiers, 350 token+prefix, 175 prefix-only.
func TestPasteboardTiers(t *testing.T) {
	click := store.Click{Fingerprint: store.Fingerprint{PastedLink: "https://lnk.example/abc123"}}
	if s := Score(click, Fingerprint{PastedLink: "https://lnk.example/abc123"}, ""); s != 350 {
		t.Errorf("token+prefix = %d; want 350", s)
	}
	if s := Score(click, Fingerprint{PastedLink: "https://lnk.example/abc123xyz"}, ""); s != 175 {
		t.Errorf("prefix-only = %d; want 175", s)
	}
	if s := Score(click, Fingerprint{PastedLink: "https://other.example/abc123"}, ""); s != 0 {
		t.Errorf("token-only (no prefix) = %d; want 0", s)
	}
	if s := Score(store.Click{}, Fingerprint{PastedLink: "https://lnk.example/abc123"}, ""); s != 0 {
		t.Errorf("empty click pasted link = %d; want 0", s)
	}
}

// Scenario 15: screen tolerance ±1 width/height, ±0.01 scale.
func TestScreenTolerance(t *testing.T) {
	base := androidClick(testLink())
	fp := Fingerprint{ScreenWidth: 393, ScreenHeight: 852, Scale: 3}
	c := base
	c.Fingerprint.Screen = "394x851@3.005"
	if s := Score(c, fp, ""); s != 200 {
		t.Errorf("within tolerance = %d; want 200", s)
	}
	c.Fingerprint.Screen = "393x854@3" // height +2
	if s := Score(c, fp, ""); s != 0 {
		t.Errorf("height +2 = %d; want 0", s)
	}
	c.Fingerprint.Screen = "393x852@3.02" // scale +0.02
	if s := Score(c, fp, ""); s != 0 {
		t.Errorf("scale +0.02 = %d; want 0", s)
	}
	c.Fingerprint.Screen = "not-a-screen"
	if s := Score(c, fp, ""); s != 0 {
		t.Errorf("unparseable screen = %d; want 0", s)
	}
}

// Scenario 16: IP exact 500.
func TestIPExactWeight(t *testing.T) {
	c := store.Click{Fingerprint: store.Fingerprint{IP: testIP}}
	if s := Score(c, Fingerprint{}, testIP); s != 500 {
		t.Errorf("ip exact = %d; want 500", s)
	}
	if s := Score(c, Fingerprint{}, otherIP); s != 0 {
		t.Errorf("ip mismatch = %d; want 0", s)
	}
}

// Scenario 17: timezone exact 200; language (locale prefix) 100.
func TestTimezoneAndLanguageWeights(t *testing.T) {
	c := store.Click{Fingerprint: store.Fingerprint{Timezone: "Europe/Warsaw", Locale: "en"}}
	if s := Score(c, Fingerprint{Timezone: "Europe/Warsaw", Locale: "en-US"}, ""); s != 300 {
		t.Errorf("tz+lang = %d; want 300", s)
	}
	if s := Score(c, Fingerprint{Timezone: "UTC", Locale: "en-US"}, ""); s != 100 {
		t.Errorf("tz mismatch = %d; want 100 (language only)", s)
	}
	if s := Score(c, Fingerprint{Timezone: "UTC", Locale: "fr-FR"}, ""); s != 0 {
		t.Errorf("no signal match = %d; want 0", s)
	}
}

// Scenario 18: missing signals -> partial score, no panic.
func TestMissingSignalsNoPanic(t *testing.T) {
	if s := Score(store.Click{Fingerprint: store.Fingerprint{}}, androidFP(), testIP); s != 0 {
		t.Errorf("empty click = %d; want 0", s)
	}
	if s := Score(androidClick(testLink()), Fingerprint{Timezone: "Europe/Warsaw"}, ""); s != 200 {
		t.Errorf("partial fingerprint = %d; want 200 (timezone only)", s)
	}
	// Nil fingerprint through Match must not panic either.
	s, _ := newTestStore(t)
	_, link := setupApp(t, s)
	recordClick(t, s, androidClick(link))
	res, err := Match(s, link.AppID, Request{IP: testIP})
	if err != nil {
		t.Fatalf("Match(nil fingerprint): %v", err)
	}
	if res.Matched {
		t.Errorf("nil fingerprint matched: %+v", res)
	}
}

// Scenario 19: documented maxima, iOS full match 1700, Android 1450 (asserts one-device-signal rule).
func TestMaxTotalsSanity(t *testing.T) {
	link := testLink()
	if s := Score(androidClick(link), androidFP(), testIP); s != 1450 {
		t.Errorf("android max = %d; want 1450", s)
	}
	if s := Score(iosClick(link), iosFP(), testIP); s != 1700 {
		t.Errorf("ios max = %d; want 1700", s)
	}
}

// Scenario 20: settings bounds 700..1200 / 5..180 inclusive, out-of-range errors.
func TestValidateSettings(t *testing.T) {
	for _, tc := range []struct {
		threshold, window int
		want              error
	}{
		{699, 15, ErrThresholdOutOfRange}, {1201, 15, ErrThresholdOutOfRange},
		{700, 5, nil}, {1200, 180, nil},
		{850, 4, ErrWindowOutOfRange}, {850, 181, ErrWindowOutOfRange},
	} {
		if err := ValidateSettings(tc.threshold, tc.window); !errors.Is(err, tc.want) {
			t.Errorf("ValidateSettings(%d, %d) = %v; want %v", tc.threshold, tc.window, err, tc.want)
		}
	}
}

// TestAppThresholdApplies: IP 500 + timezone 200 + screen 200 = 900 passes app threshold 850, fails 1000.
func TestAppThresholdApplies(t *testing.T) {
	fp := Fingerprint{Timezone: "Europe/Warsaw", ScreenWidth: 393, ScreenHeight: 852, Scale: 3}
	for _, tc := range []struct {
		threshold int
		want      bool
	}{{850, true}, {1000, false}} {
		s, _ := newTestStore(t)
		app, link := setupApp(t, s)
		if err := s.UpdateAppMatchSettings(app.ID, tc.threshold, 15); err != nil {
			t.Fatalf("UpdateAppMatchSettings: %v", err)
		}
		recordClick(t, s, androidClick(link))
		res, err := Match(s, app.ID, Request{IP: testIP, Fingerprint: &fp})
		if err != nil {
			t.Fatalf("Match: %v", err)
		}
		if res.Matched != tc.want {
			t.Errorf("app threshold %d: matched = %v; want %v", tc.threshold, res.Matched, tc.want)
		}
	}
}

// TestAppWindowApplies: app window, not a default, decides which clicks are candidates.
func TestAppWindowApplies(t *testing.T) {
	for _, tc := range []struct {
		window int
		age    time.Duration
		want   bool
	}{{15, 60 * time.Minute, false}, {90, 60 * time.Minute, true}, {5, 10 * time.Minute, false}} {
		s, path := newTestStore(t)
		app, link := setupApp(t, s)
		if err := s.UpdateAppMatchSettings(app.ID, 850, tc.window); err != nil {
			t.Fatalf("UpdateAppMatchSettings: %v", err)
		}
		recordClickBackdated(t, s, path, androidClick(link), tc.age)
		res, err := Match(s, app.ID, Request{IP: testIP, Fingerprint: fpPtr(androidFP())})
		if err != nil {
			t.Fatalf("Match: %v", err)
		}
		if res.Matched != tc.want {
			t.Errorf("app window %d, click age %v: matched = %v; want %v", tc.window, tc.age, res.Matched, tc.want)
		}
	}
}

func TestClickConsumedByFirstDevice(t *testing.T) {
	s, _ := newTestStore(t)
	_, link := setupApp(t, s)
	recordClick(t, s, androidClick(link))
	res, err := Match(s, link.AppID, Request{IP: testIP, DeviceHash: "d1", Fingerprint: fpPtr(androidFP())})
	if err != nil || !res.Matched {
		t.Fatalf("d1 Match = %+v, %v; want matched", res, err)
	}
	res, err = Match(s, link.AppID, Request{IP: testIP, DeviceHash: "d2", Fingerprint: fpPtr(androidFP())})
	if err != nil || res.Matched {
		t.Errorf("d2 Match = %+v, %v; want no match (click consumed)", res, err)
	}
}

func TestSameDeviceRetryStillMatches(t *testing.T) {
	s, _ := newTestStore(t)
	_, link := setupApp(t, s)
	recordClick(t, s, androidClick(link))
	req := Request{IP: testIP, DeviceHash: "d1", Fingerprint: fpPtr(androidFP())}
	first, err := Match(s, link.AppID, req)
	if err != nil || !first.Matched {
		t.Fatalf("first Match = %+v, %v; want matched", first, err)
	}
	// sdk handler records the install after a match
	if _, err := s.RecordInstall(store.Install{AppID: link.AppID, DeviceHash: "d1", ClickID: first.Click.ID, Attribution: store.AttributionNonOrganic}); err != nil {
		t.Fatalf("RecordInstall: %v", err)
	}
	again, err := Match(s, link.AppID, req)
	if err != nil || !again.Matched || again.Click.ID != first.Click.ID {
		t.Errorf("retry Match = %+v, %v; want matched click %s", again, err, first.Click.ID)
	}
}

func TestDeterministicAfterProbabilisticConsumed(t *testing.T) {
	s, _ := newTestStore(t)
	_, link := setupApp(t, s)
	c := recordClick(t, s, androidClick(link))
	res, err := Match(s, link.AppID, Request{IP: testIP, DeviceHash: "d1", Fingerprint: fpPtr(androidFP())})
	if err != nil || !res.Matched {
		t.Fatalf("probabilistic Match = %+v, %v; want matched", res, err)
	}
	res, err = Match(s, link.AppID, Request{ClickID: c.ClickID, DeviceHash: "clickhash"})
	if err != nil || res.Matched {
		t.Errorf("deterministic Match = %+v, %v; want no match (click consumed)", res, err)
	}
}

func TestNewerUnmatchedClickStillCandidate(t *testing.T) {
	s, path := newTestStore(t)
	_, link := setupApp(t, s)
	old := recordClickBackdated(t, s, path, androidClick(link), 2*time.Minute)
	newer := androidClick(link)
	newer.Fingerprint.UserAgent += " v2" // distinct UA: same-signal clicks otherwise dedup into one row
	rec := recordClick(t, s, newer)
	if rec.ID == old.ID {
		t.Fatal("clicks deduped into one row")
	}
	req := Request{IP: testIP, DeviceHash: "d1", Fingerprint: fpPtr(androidFP())}
	r1, err := Match(s, link.AppID, req)
	if err != nil || !r1.Matched || r1.Click.ID != rec.ID {
		t.Fatalf("d1 Match = %+v, %v; want newer click %s", r1, err, rec.ID)
	}
	req.DeviceHash = "d2"
	r2, err := Match(s, link.AppID, req)
	if err != nil || !r2.Matched || r2.Click.ID != old.ID {
		t.Errorf("d2 Match = %+v, %v; want older click %s", r2, err, old.ID)
	}
}

// Window measured from SDK capture time: click 20 min old, window 15.
func TestWindowFromCaptureTime(t *testing.T) {
	cases := []struct {
		name string
		at   time.Time
		want bool
	}{
		{"capture 10m ago", time.Now().Add(-10 * time.Minute), true},
		{"zero uses server now", time.Time{}, false},
		{"stale 2024 ignored", time.Date(2024, 7, 3, 0, 0, 0, 0, time.UTC), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, path := newTestStore(t)
			_, link := setupApp(t, s)
			recordClickBackdated(t, s, path, androidClick(link), 20*time.Minute)
			fp := androidFP()
			fp.CapturedAt = tc.at
			res, err := Match(s, link.AppID, Request{IP: testIP, Fingerprint: fpPtr(fp)})
			if err != nil {
				t.Fatalf("Match: %v", err)
			}
			if res.Matched != tc.want {
				t.Errorf("matched = %v; want %v", res.Matched, tc.want)
			}
		})
	}
}

// iOS 26 Safari freezes the OS token at 18_6; Version/ token carries the real version.
func TestIOS26SafariScoresDeviceSignal(t *testing.T) {
	c := iosClick(testLink())
	c.Fingerprint.UserAgent = "Mozilla/5.0 (iPhone; CPU iPhone OS 18_6 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/26.0 Mobile/15E148 Safari/604.1"
	fp := iosFP()
	fp.SystemVersion = "26.0"
	got := Score(c, fp, testIP)
	fp.SystemVersion = "25.0"
	if diff := got - Score(c, fp, testIP); diff != 350 {
		t.Errorf("device signal diff = %d; want 350", diff)
	}
	// Version/ is major.minor; the SDK sends the patch.
	for v, want := range map[string]bool{"26.0.1": true, "26": true, "26.1": false, "26.1.2": false} {
		fp.SystemVersion = v
		if hit := Score(c, fp, testIP) == got; hit != want {
			t.Errorf("Version/26.0 vs SDK %s: match %v; want %v", v, hit, want)
		}
	}
	// Pre-26 OS token carries the patch: 17_4 is 17.4.0, never 17.4.1.
	c.Fingerprint.UserAgent = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_4 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.4 Mobile/15E148 Safari/604.1"
	fp.SystemVersion = "17.4"
	exact := Score(c, fp, testIP)
	fp.SystemVersion = "17.4.1"
	if diff := exact - Score(c, fp, testIP); diff != 350 {
		t.Errorf("OS token 17_4 vs SDK 17.4.1: diff = %d; want 350 (no version credit)", diff)
	}
}

// UA device signature fallback is Android-only.
func TestUADeviceSignatureAndroidOnly(t *testing.T) {
	ios := store.Click{Fingerprint: store.Fingerprint{Device: "iPhone", UserAgent: "Mozilla/5.0 (iPhone)"}}
	if s := Score(ios, Fingerprint{Model: "iPhone"}, ""); s != 0 {
		t.Errorf("iOS UA signature = %d; want 0", s)
	}
	and := store.Click{Fingerprint: store.Fingerprint{Device: "Pixel 7", UserAgent: "Mozilla/5.0 (Linux; Android 10; K)"}}
	if s := Score(and, Fingerprint{Model: "Pixel 7"}, ""); s != 350 {
		t.Errorf("Android UA signature = %d; want 350", s)
	}
}

// Receipts: method per path, best + runner-up, near-miss score.
func TestMatchReceipts(t *testing.T) {
	s, _ := newTestStore(t)
	_, link := setupApp(t, s)
	fp := androidFP()
	best := androidClick(link) // IP match
	second := androidClick(link)
	second.Fingerprint.IP = otherIP
	third := androidClick(link)
	third.Fingerprint.IP, third.Fingerprint.Timezone = "192.0.2.1", "Asia/Tokyo"
	var scores []int
	for _, c := range []store.Click{best, second, third} {
		scores = append(scores, Score(recordClick(t, s, c), fp, testIP))
	}
	if !(scores[0] > scores[1] && scores[1] > scores[2]) {
		t.Fatalf("scores %v; want strictly descending", scores)
	}
	res, err := Match(s, link.AppID, Request{IP: testIP, Fingerprint: &fp})
	if err != nil {
		t.Fatalf("Match: %v", err)
	}
	if !res.Matched || res.Method != store.MethodProbabilistic || res.Score != scores[0] || res.RunnerUp != scores[1] {
		t.Errorf("receipt = %s %d/%d; want probabilistic %d/%d", res.Method, res.Score, res.RunnerUp, scores[0], scores[1])
	}

	// docs' rejection example: 500 < 850
	miss := Fingerprint{Timezone: "Europe/Warsaw", Locale: "en-US", ScreenWidth: 393, ScreenHeight: 852, Scale: 3}
	res, _ = Match(s, link.AppID, Request{IP: "192.0.2.99", Fingerprint: &miss})
	if res.Matched || res.Method != store.MethodOrganic || res.Score != 500 {
		t.Errorf("near miss = %+v; want organic, score 500", res)
	}

	c := androidClick(link)
	c.Fingerprint.IP, c.ClickID = "192.0.2.50", "play-7"
	recordClick(t, s, c)
	if res, _ := Match(s, link.AppID, Request{ClickID: "play-7"}); res.Method != store.MethodClickID || res.Score != -1 {
		t.Errorf("clickId receipt = %s %d; want click_id, -1", res.Method, res.Score)
	}
	if res, _ := Match(s, link.AppID, Request{ClickID: "nope"}); res.Method != store.MethodOrganic {
		t.Errorf("unknown clickId method = %s; want organic", res.Method)
	}
}

// execDB: raw statement via second connection (WAL allows it).
func execDB(t *testing.T, dbPath, q string, args ...any) {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatalf("exec %q: %v", q, err)
	}
}

// setClick: raw click column edit; facts RecordClick derives itself (hits, first_seen_at) can't be passed in.
func setClick(t *testing.T, dbPath, id, set string, args ...any) {
	t.Helper()
	execDB(t, dbPath, `UPDATE clicks SET `+set+` WHERE id = ?`, append(args, id)...)
}

// ago: stored timestamp d before now.
func ago(d time.Duration) string { return time.Now().UTC().Add(-d).Format(time.RFC3339Nano) }

// recordAged: click first seen a minute ago, clear of the 10 s short-timing cutoff.
func recordAged(t *testing.T, s *store.Store, dbPath string, c store.Click) store.Click {
	t.Helper()
	got := recordClick(t, s, c)
	setClick(t, dbPath, got.ID, `first_seen_at = ?`, ago(time.Minute))
	return got
}

// setFraud: defaults with mut applied.
func setFraud(t *testing.T, s *store.Store, appID string, mut func(*fraud.Settings)) {
	t.Helper()
	f := fraud.Defaults
	mut(&f)
	if err := s.UpdateFraudSettings(appID, f); err != nil {
		t.Fatalf("UpdateFraudSettings: %v", err)
	}
}

func secondLink(t *testing.T, s *store.Store, appID string) store.Link {
	t.Helper()
	l, err := s.CreateLink(store.Link{AppID: appID, Key: "def", URL: "https://example.com/other"})
	if err != nil {
		t.Fatalf("CreateLink: %v", err)
	}
	return l
}

func unconsumed(t *testing.T, s *store.Store, appID, clickID string) bool {
	t.Helper()
	_, err := s.ClickByClickID(appID, clickID, "")
	return err == nil
}

// modeOf: the settings field holding sig's mode.
func modeOf(f *fraud.Settings, sig string) *string {
	switch sig {
	case fraud.SignalVelocity:
		return &f.VelocityMode
	case fraud.SignalTiming:
		return &f.TimingMode
	case fraud.SignalUserAgent:
		return &f.UserAgentMode
	case fraud.SignalIP:
		return &f.IPMode
	}
	panic("no mode for " + sig)
}

// flaggedAndClean: best click (IP match, 1450) on link plus a clean candidate (950) on a second link, both first seen a minute ago.
func flaggedAndClean(t *testing.T, s *store.Store, path string, link store.Link) (flagged, clean store.Click) {
	t.Helper()
	flagged = recordAged(t, s, path, androidClick(link))
	c := androidClick(secondLink(t, s, link.AppID))
	c.Fingerprint.IP = otherIP
	clean = recordAged(t, s, path, c)
	fp := androidFP()
	if a, b := Score(flagged, fp, testIP), Score(clean, fp, testIP); a != 1450 || b != 950 {
		t.Fatalf("scores %d/%d; want 1450/950", a, b)
	}
	return flagged, clean
}

// Per signal path: active mode moves credit to the clean candidate (reattributed, flagged click left unconsumed); tagged mode, with every
// other signal active or under defaults, leaves the flagged click winning with the same receipt and only a label (AE1-AE3).
func TestFraudSignalModes(t *testing.T) {
	cases := []struct {
		name, sig, set string
		arg            any
	}{
		{"velocity via ip hits", fraud.SignalVelocity, `hits_ip = ?`, fraud.Defaults.VelocityIPMax},
		{"velocity via link hits", fraud.SignalVelocity, `hits_link = ?`, fraud.Defaults.VelocityLinkMax},
		{"timing short", fraud.SignalTiming, `first_seen_at = ?`, ago(3 * time.Second)},
		{"user agent", fraud.SignalUserAgent, `ua_suspect = ?`, 1},
		{"ip hosting", fraud.SignalIP, `ip_hosting = ?`, 1},
	}
	for _, tc := range cases {
		for _, mode := range []string{"active", "tagged", "defaults"} {
			t.Run(tc.name+"/"+mode, func(t *testing.T) {
				s, path := newTestStore(t)
				app, link := setupApp(t, s)
				flagged, clean := flaggedAndClean(t, s, path, link)
				setClick(t, path, flagged.ID, tc.set, tc.arg)
				switch mode {
				case "active":
					setFraud(t, s, app.ID, func(f *fraud.Settings) { *modeOf(f, tc.sig) = fraud.ModeActive })
				case "tagged":
					setFraud(t, s, app.ID, func(f *fraud.Settings) {
						f.VelocityMode, f.TimingMode, f.UserAgentMode, f.IPMode = fraud.ModeActive, fraud.ModeActive, fraud.ModeActive, fraud.ModeActive
						*modeOf(f, tc.sig) = fraud.ModeTagged
					})
				}
				res, err := Match(s, app.ID, Request{IP: testIP, Fingerprint: fpPtr(androidFP())})
				if err != nil {
					t.Fatalf("Match: %v", err)
				}
				if res.Fraud != tc.sig {
					t.Errorf("fraud = %q; want %q", res.Fraud, tc.sig)
				}
				if mode != "active" {
					if !res.Matched || res.Click.ID != flagged.ID || res.Score != 1450 || res.RunnerUp != 950 || res.FraudAction != "" || res.FraudLinkID != "" {
						t.Errorf("got %v %s %d/%d action %q link %q; want flagged click 1450/950, no action", res.Matched, res.Click.ID, res.Score, res.RunnerUp, res.FraudAction, res.FraudLinkID)
					}
					return
				}
				if !res.Matched || res.Click.ID != clean.ID || res.Score != 950 || res.RunnerUp != -1 {
					t.Errorf("got %v %s %d/%d; want clean click %s 950/-1", res.Matched, res.Click.ID, res.Score, res.RunnerUp, clean.ID)
				}
				if res.FraudAction != store.FraudActionReattributed || res.FraudLinkID != link.ID {
					t.Errorf("action %q link %q; want reattributed, %s", res.FraudAction, res.FraudLinkID, link.ID)
				}
				if !unconsumed(t, s, app.ID, flagged.ClickID) || unconsumed(t, s, app.ID, clean.ClickID) {
					t.Error("want flagged click unconsumed, clean click consumed")
				}
			})
		}
	}
}

// Velocity fires at the threshold, not one below, for both counters.
func TestFraudVelocityThresholds(t *testing.T) {
	for _, tc := range []struct {
		set  string
		n    int
		want string
	}{
		{`hits_ip = ?`, fraud.Defaults.VelocityIPMax - 1, ""},
		{`hits_ip = ?`, fraud.Defaults.VelocityIPMax, fraud.SignalVelocity},
		{`hits_link = ?`, fraud.Defaults.VelocityLinkMax - 1, ""},
		{`hits_link = ?`, fraud.Defaults.VelocityLinkMax, fraud.SignalVelocity},
	} {
		s, path := newTestStore(t)
		_, link := setupApp(t, s)
		c := recordAged(t, s, path, androidClick(link))
		setClick(t, path, c.ID, tc.set, tc.n)
		res, err := Match(s, link.AppID, Request{IP: testIP, Fingerprint: fpPtr(androidFP())})
		if err != nil || !res.Matched || res.Fraud != tc.want {
			t.Errorf("%s %d: matched %v fraud %q, %v; want matched, fraud %q", tc.set, tc.n, res.Matched, res.Fraud, err, tc.want)
		}
	}
}

// Active-flagged clickId -> organic, excluded, click not consumed; a clean clickId under the same mode still matches (AE5).
func TestFraudClickIDExcluded(t *testing.T) {
	s, _ := newTestStore(t)
	app, link := setupApp(t, s)
	setFraud(t, s, app.ID, func(f *fraud.Settings) { f.UserAgentMode = fraud.ModeActive })
	c := androidClick(link)
	c.ClickID, c.UASuspect = "play-bot", true
	recordClick(t, s, c)
	res, err := Match(s, app.ID, Request{ClickID: "play-bot"})
	if err != nil {
		t.Fatalf("Match: %v", err)
	}
	// fresh click: short timing fires too (tagged)
	if res.Matched || res.Method != store.MethodOrganic || res.FraudAction != store.FraudActionExcluded || res.FraudLinkID != link.ID ||
		res.Fraud != "timing,user_agent" {
		t.Errorf("flagged clickId = %+v; want organic, excluded, timing,user_agent", res)
	}
	if !unconsumed(t, s, app.ID, "play-bot") {
		t.Error("excluded click consumed")
	}

	ok := androidClick(link)
	ok.Fingerprint.IP, ok.ClickID = otherIP, "play-ok"
	recordClick(t, s, ok)
	if res, _ := Match(s, app.ID, Request{ClickID: "play-ok"}); !res.Matched || res.Method != store.MethodClickID || res.FraudAction != "" {
		t.Errorf("clean clickId = %+v; want click_id match", res)
	}
}

// Velocity active, only the flagged click -> organic, excluded, no valid score (AE4).
func TestFraudActiveOnlyFlaggedOrganic(t *testing.T) {
	s, path := newTestStore(t)
	app, link := setupApp(t, s)
	setFraud(t, s, app.ID, func(f *fraud.Settings) { f.VelocityMode = fraud.ModeActive })
	c := recordAged(t, s, path, androidClick(link))
	setClick(t, path, c.ID, `hits_ip = 50`)
	res, _ := Match(s, app.ID, Request{IP: testIP, Fingerprint: fpPtr(androidFP())})
	if res.Matched || res.FraudAction != store.FraudActionExcluded || res.FraudLinkID != link.ID || res.Fraud != fraud.SignalVelocity || res.Score != -1 {
		t.Errorf("only flagged = %+v; want organic, excluded, velocity, score -1", res)
	}
	if !unconsumed(t, s, app.ID, c.ClickID) {
		t.Error("excluded click consumed")
	}
}

// Short timing: fires just under the cutoff, not at it; open clicks exempt; a re-tap counts from the first tap; a client clock running
// ahead (capture 50 s in the future) is clamped to server now, so it cannot stretch "too soon" away.
func TestFraudTimingShort(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	capture := now.Add(-time.Second)
	cutoff := time.Duration(fraud.Defaults.TimingShortSeconds) * time.Second
	for _, tc := range []struct {
		name, kind string
		capture    time.Time
		firstSeen  time.Time
		retap      bool
		want       bool
	}{
		{"just under cutoff", store.KindApp, capture, capture.Add(-cutoff + time.Millisecond), false, true},
		{"at cutoff", store.KindApp, capture, capture.Add(-cutoff), false, false},
		{"open kind exempt", store.KindOpen, capture, capture.Add(-3 * time.Second), false, false},
		{"retap measured from first tap", store.KindApp, now, now.Add(-time.Minute), true, false},
		{"future capture clamped", store.KindApp, now.Add(50 * time.Second), now.Add(-3 * time.Second), false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, path := newTestStore(t)
			_, link := setupApp(t, s)
			c := androidClick(link)
			c.Kind = tc.kind
			got := recordClick(t, s, c)
			first := tc.firstSeen.Format(time.RFC3339Nano)
			setClick(t, path, got.ID, `first_seen_at = ?, created_at = ?`, first, first)
			if tc.retap { // dedup refresh: created_at -> now, first_seen_at kept
				if again := recordClick(t, s, c); again.ID != got.ID {
					t.Fatal("re-tap made a new click")
				}
			}
			fp := androidFP()
			fp.CapturedAt = tc.capture
			res, err := Match(s, link.AppID, Request{IP: testIP, Fingerprint: &fp})
			if err != nil || !res.Matched {
				t.Fatalf("Match = %+v, %v; want matched", res, err)
			}
			if fired := strings.Contains(res.Fraud, fraud.SignalTiming); fired != tc.want {
				t.Errorf("fraud %q; want timing fired = %v", res.Fraud, tc.want)
			}
		})
	}
}

// Long timing: clickId click first tapped past TimingLongHours fires, under it does not; long never fires on the probabilistic path.
func TestFraudTimingLongClickIDOnly(t *testing.T) {
	long := time.Duration(fraud.Defaults.TimingLongHours) * time.Hour
	for _, tc := range []struct {
		name    string
		clickID bool
		age     time.Duration
		want    bool
	}{
		{"clickId past long", true, long + time.Minute, true},
		{"clickId under long", true, long - time.Minute, false},
		{"probabilistic past long", false, long + time.Minute, false},
	} {
		s, path := newTestStore(t)
		_, link := setupApp(t, s)
		c := recordClick(t, s, androidClick(link)) // created now (re-tapped), first tap age ago
		setClick(t, path, c.ID, `first_seen_at = ?`, ago(tc.age))
		req := Request{IP: testIP, Fingerprint: fpPtr(androidFP())}
		if tc.clickID {
			req = Request{ClickID: c.ClickID}
		}
		res, err := Match(s, link.AppID, req)
		if err != nil || !res.Matched {
			t.Fatalf("%s: Match = %+v, %v; want matched", tc.name, res, err)
		}
		if fired := strings.Contains(res.Fraud, fraud.SignalTiming); fired != tc.want {
			t.Errorf("%s: fraud %q; want timing fired = %v", tc.name, res.Fraud, tc.want)
		}
	}
}

// Active IP mode, best candidate clean, flagged one below it -> attribution unchanged, no action; runner-up comes from the valid set only.
func TestFraudActiveCleanBestUnchanged(t *testing.T) {
	s, path := newTestStore(t)
	app, link := setupApp(t, s)
	setFraud(t, s, app.ID, func(f *fraud.Settings) { f.IPMode = fraud.ModeActive })
	best := recordAged(t, s, path, androidClick(link))
	h := androidClick(link)
	h.Fingerprint.IP, h.IPHosting = otherIP, true
	recordAged(t, s, path, h)
	res, _ := Match(s, app.ID, Request{IP: testIP, Fingerprint: fpPtr(androidFP())})
	if !res.Matched || res.Click.ID != best.ID || res.Score != 1450 || res.RunnerUp != -1 || res.FraudAction != "" || res.Fraud != "" {
		t.Errorf("clean best = %+v; want best click 1450/-1, no labels", res)
	}
}

// Flagged best and clean candidate both below threshold -> organic, no action (nothing was taken away).
func TestFraudBelowThresholdNoAction(t *testing.T) {
	s, path := newTestStore(t)
	app, link := setupApp(t, s)
	if err := s.UpdateAppMatchSettings(app.ID, 1200, 15); err != nil {
		t.Fatal(err)
	}
	setFraud(t, s, app.ID, func(f *fraud.Settings) { f.VelocityMode = fraud.ModeActive })
	clean := androidClick(link)
	clean.Fingerprint.IP = otherIP
	recordAged(t, s, path, clean)
	flagged := recordAged(t, s, path, androidClick(link)) // 1450 at IP match, but request IP matches neither
	setClick(t, path, flagged.ID, `hits_ip = 50`)
	res, _ := Match(s, app.ID, Request{IP: "192.0.2.99", Fingerprint: fpPtr(androidFP())})
	if res.Matched || res.FraudAction != "" || res.FraudLinkID != "" || res.Fraud != "" || res.Score != 950 {
		t.Errorf("below threshold = %+v; want organic 950, no labels", res)
	}
}

// Claim race after an exclusion: the clean winner is taken between scan and claim (a trigger turns the claim UPDATE into a no-op, the
// same zero-row result another device's claim produces) -> organic, but the excluded click still took credit away, so excluded + its link.
func TestFraudClaimRaceKeepsExcluded(t *testing.T) {
	s, path := newTestStore(t)
	app, link := setupApp(t, s)
	setFraud(t, s, app.ID, func(f *fraud.Settings) { f.VelocityMode = fraud.ModeActive })
	flagged, clean := flaggedAndClean(t, s, path, link)
	setClick(t, path, flagged.ID, `hits_ip = 50`)
	execDB(t, path, `CREATE TRIGGER lost_race BEFORE UPDATE OF matched_at ON clicks WHEN OLD.id = '`+clean.ID+`' BEGIN SELECT RAISE(IGNORE); END`)
	res, err := Match(s, app.ID, Request{IP: testIP, Fingerprint: fpPtr(androidFP())})
	if err != nil {
		t.Fatalf("Match: %v", err)
	}
	if res.Matched || res.Method != store.MethodOrganic || res.Score != 950 || res.RunnerUp != -1 {
		t.Errorf("got %v %s %d/%d; want organic 950/-1", res.Matched, res.Method, res.Score, res.RunnerUp)
	}
	if res.FraudAction != store.FraudActionExcluded || res.FraudLinkID != link.ID || res.Fraud != fraud.SignalVelocity {
		t.Errorf("labels %q %q %q; want velocity, excluded, %s", res.Fraud, res.FraudAction, res.FraudLinkID, link.ID)
	}
	if !unconsumed(t, s, app.ID, flagged.ClickID) {
		t.Error("excluded click consumed")
	}
}

// Install request from a hosting range gets its own tag-only label: install_ip, never ip, never excludes even with IP mode active;
// a hosting click plus a hosting install carries both labels.
func TestFraudInstallIPTagOnly(t *testing.T) {
	const aws = "3.5.140.1"
	s, path := newTestStore(t)
	app, link := setupApp(t, s)
	setFraud(t, s, app.ID, func(f *fraud.Settings) { f.IPMode = fraud.ModeActive })
	c := recordAged(t, s, path, androidClick(link))
	res, _ := Match(s, app.ID, Request{IP: aws, Fingerprint: fpPtr(androidFP())})
	if !res.Matched || res.Click.ID != c.ID || res.Fraud != fraud.SignalInstallIP || res.FraudAction != "" {
		t.Errorf("hosting install IP, IP mode active = %+v; want matched, install_ip label only", res)
	}

	s2, path2 := newTestStore(t)
	_, link2 := setupApp(t, s2)
	h := androidClick(link2)
	h.IPHosting = true
	recordAged(t, s2, path2, h)
	res, _ = Match(s2, link2.AppID, Request{IP: aws, Fingerprint: fpPtr(androidFP())})
	if !res.Matched || res.Fraud != "ip,install_ip" {
		t.Errorf("hosting click + install = %q matched %v; want ip,install_ip", res.Fraud, res.Matched)
	}
}

// Fingerprint concentration, FingerprintMax 5: this install plus 4 prior probabilistic installs (same hash, same link) labels it, 3 do not;
// priors older than FingerprintWindowDays do not count. Tag only: still attributed.
func TestFraudFingerprintConcentration(t *testing.T) {
	for _, tc := range []struct {
		name          string
		recent, stale int
		want          string
	}{
		{"3 priors", 3, 0, ""},
		{"4 priors", 4, 0, fraud.SignalFingerprint},
		{"3 priors + 1 outside window", 3, 1, ""},
	} {
		s, path := newTestStore(t)
		app, link := setupApp(t, s)
		setFraud(t, s, app.ID, func(f *fraud.Settings) { f.FingerprintMax, f.FingerprintWindowDays = 5, 7 })
		for i := range tc.recent + tc.stale {
			if _, err := s.RecordInstall(store.Install{AppID: app.ID, DeviceHash: "h", ClickID: fmt.Sprint("old", i), LinkID: link.ID,
				Attribution: store.AttributionNonOrganic, Method: store.MethodProbabilistic}); err != nil {
				t.Fatal(err)
			}
			if i >= tc.recent {
				execDB(t, path, `UPDATE installs SET created_at = ? WHERE click_id = ?`, ago(8*24*time.Hour), fmt.Sprint("old", i))
			}
		}
		recordAged(t, s, path, androidClick(link))
		res, err := Match(s, app.ID, Request{IP: testIP, DeviceHash: "h", Fingerprint: fpPtr(androidFP())})
		if err != nil || !res.Matched || res.Fraud != tc.want || res.FraudAction != "" {
			t.Errorf("%s: matched %v fraud %q action %q, %v; want matched, fraud %q", tc.name, res.Matched, res.Fraud, res.FraudAction, err, tc.want)
		}
	}
}

// Settings read failure (table dropped) -> every signal tagged; a flagged clickId still matches (KTD4).
func TestFraudSettingsErrorClickIDMatches(t *testing.T) {
	s, path := newTestStore(t)
	_, link := setupApp(t, s)
	c := androidClick(link)
	c.ClickID, c.UASuspect = "play-1", true
	recordClick(t, s, c)
	execDB(t, path, `DROP TABLE fraud_settings`)
	res, err := Match(s, link.AppID, Request{ClickID: "play-1"})
	if err != nil || !res.Matched || res.Method != store.MethodClickID || res.FraudAction != "" {
		t.Errorf("settings error: %+v, %v; want clickId match, no action", res, err)
	}
}

// Backend errors surface as errors, never as a match or an organic answer: clickId lookup, unknown app, window scan, and the claim
// UPDATE on both paths (a trigger aborts it).
func TestMatchBackendErrors(t *testing.T) {
	const abortClaim = `CREATE TRIGGER no_claim BEFORE UPDATE OF matched_at ON clicks BEGIN SELECT RAISE(ABORT, 'claim failed'); END`
	for _, tc := range []struct {
		name  string
		fault string
		req   func(store.Click) Request
		appID string // "" = the test app
	}{
		{"clickId lookup", `DROP TABLE clicks`, func(c store.Click) Request { return Request{ClickID: c.ClickID, DeviceHash: "h"} }, ""},
		{"unknown app", "", func(store.Click) Request { return Request{IP: testIP, Fingerprint: fpPtr(androidFP())} }, "missing"},
		{"window scan", `ALTER TABLE clicks DROP COLUMN hits_link`, func(store.Click) Request { return Request{IP: testIP, Fingerprint: fpPtr(androidFP())} }, ""},
		{"clickId claim", abortClaim, func(c store.Click) Request { return Request{ClickID: c.ClickID} }, ""},
		{"probabilistic claim", abortClaim, func(store.Click) Request { return Request{IP: testIP, Fingerprint: fpPtr(androidFP())} }, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, path := newTestStore(t)
			app, link := setupApp(t, s)
			c := recordAged(t, s, path, androidClick(link))
			if tc.fault != "" {
				execDB(t, path, tc.fault)
			}
			id := app.ID
			if tc.appID != "" {
				id = tc.appID
			}
			res, err := Match(s, id, tc.req(c))
			if err == nil || res.Matched || res.Method != "" {
				t.Errorf("Match = %+v, %v; want zero result and an error", res, err)
			}
		})
	}
}

// Fingerprint count failure (installs table gone): the match still succeeds, just without the fingerprint label (R5, R7). Stored max 1
// (below Validate's floor, written directly) so a failed count (0) would label if the error were ignored.
func TestFraudFingerprintCountErrorStillMatches(t *testing.T) {
	s, path := newTestStore(t)
	app, link := setupApp(t, s)
	setFraud(t, s, app.ID, func(*fraud.Settings) {})
	execDB(t, path, `UPDATE fraud_settings SET fingerprint_max = 1`)
	c := recordAged(t, s, path, androidClick(link))
	execDB(t, path, `DROP TABLE installs`)
	res, err := Match(s, app.ID, Request{IP: testIP, DeviceHash: "h", Fingerprint: fpPtr(androidFP())})
	if err != nil || !res.Matched || res.Click.ID != c.ID || res.Method != store.MethodProbabilistic || res.Fraud != "" {
		t.Errorf("Match = %+v, %v; want probabilistic match on %s, no labels", res, err, c.ID)
	}
}

// Threshold is inclusive on both sides of an exclusion: a valid winner scoring exactly the threshold matches, and an excluded top click
// scoring exactly the threshold counts as credit taken away (organic, excluded + its link).
func TestFraudThresholdInclusive(t *testing.T) {
	for _, tc := range []struct {
		name   string
		hits   int
		action string
	}{
		{"clean at threshold matches", 0, ""},
		{"excluded at threshold", 50, store.FraudActionExcluded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, path := newTestStore(t)
			app, link := setupApp(t, s)
			if err := s.UpdateAppMatchSettings(app.ID, 950, 15); err != nil {
				t.Fatal(err)
			}
			setFraud(t, s, app.ID, func(f *fraud.Settings) { f.VelocityMode = fraud.ModeActive })
			c := recordAged(t, s, path, androidClick(link))
			setClick(t, path, c.ID, `hits_ip = ?`, tc.hits)
			res, err := Match(s, app.ID, Request{IP: otherIP, Fingerprint: fpPtr(androidFP())}) // no IP match: 950
			if err != nil {
				t.Fatalf("Match: %v", err)
			}
			if tc.action == "" {
				if !res.Matched || res.Score != 950 || res.FraudAction != "" {
					t.Errorf("got %+v; want matched at 950, no action", res)
				}
				return
			}
			if res.Matched || res.FraudAction != tc.action || res.FraudLinkID != link.ID || res.Fraud != fraud.SignalVelocity {
				t.Errorf("got %+v; want organic, excluded, velocity, link %s", res, link.ID)
			}
		})
	}
}

// Runner-up comes from the valid set in any scan order: newest clean 1450 wins, the older active-excluded 950 is skipped, the oldest
// clean 750 is the runner-up.
func TestFraudRunnerUpFromValidSet(t *testing.T) {
	s, path := newTestStore(t)
	app, link := setupApp(t, s)
	setFraud(t, s, app.ID, func(f *fraud.Settings) { f.IPMode = fraud.ModeActive })
	low := androidClick(link)
	low.Fingerprint.IP, low.Fingerprint.Timezone = otherIP, "Asia/Tokyo"
	recordAged(t, s, path, low)
	flagged := androidClick(link)
	flagged.Fingerprint.IP, flagged.IPHosting = otherIP, true
	flagged.Fingerprint.UserAgent += " flagged" // separate device: no dedup into low's row
	recordAged(t, s, path, flagged)
	best := recordAged(t, s, path, androidClick(link))
	fp := androidFP()
	if a, b, c := Score(best, fp, testIP), Score(flagged, fp, testIP), Score(low, fp, testIP); a != 1450 || b != 950 || c != 750 {
		t.Fatalf("scores %d/%d/%d; want 1450/950/750", a, b, c)
	}
	res, err := Match(s, app.ID, Request{IP: testIP, Fingerprint: &fp})
	if err != nil || !res.Matched || res.Click.ID != best.ID || res.Score != 1450 || res.RunnerUp != 750 || res.FraudAction != "" {
		t.Errorf("Match = %+v, %v; want %s at 1450, runner-up 750, no action", res, err, best.ID)
	}
}

// No device hash on the request: no fingerprint count, so no label even when hash-less probabilistic installs on the link pass the max.
func TestFraudFingerprintNeedsDeviceHash(t *testing.T) {
	s, path := newTestStore(t)
	app, link := setupApp(t, s)
	for i := range fraud.Defaults.FingerprintMax {
		if _, err := s.RecordInstall(store.Install{AppID: app.ID, ClickID: fmt.Sprint("old", i), LinkID: link.ID,
			Attribution: store.AttributionNonOrganic, Method: store.MethodProbabilistic}); err != nil {
			t.Fatal(err)
		}
	}
	recordAged(t, s, path, androidClick(link))
	res, err := Match(s, app.ID, Request{IP: testIP, Fingerprint: fpPtr(androidFP())})
	if err != nil || !res.Matched || res.Fraud != "" {
		t.Errorf("Match = %+v, %v; want matched, no labels", res, err)
	}
}
