package match

// Test scenarios 1-20 for the matching engine.

import (
	"database/sql"
	"errors"
	"testing"
	"time"

	_ "modernc.org/sqlite" // SQLite driver: tests backdate click timestamps directly

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
	got, err := s.RecordClick(c, 15, 24)
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
	res, err := Match(s, link.AppID, Request{ClickID: "play-click-1", IP: otherIP, Fingerprint: &fp}, 15, 850)
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
	res, err := Match(s, link.AppID, Request{ClickID: "unknown-click", IP: testIP, Fingerprint: fpPtr(androidFP())}, 15, 850)
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
	res, err := Match(s, link.AppID, Request{IP: testIP, Fingerprint: fpPtr(androidFP())}, 15, 850)
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
	res, err := Match(s, link.AppID, Request{IP: otherIP, Fingerprint: &fp}, 15, 850)
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
	res, err := Match(s, link.AppID, Request{IP: testIP, Fingerprint: &fp}, 15, 850)
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
	res, err := Match(s, link.AppID, Request{IP: testIP, Fingerprint: fpPtr(androidFP())}, 15, 850)
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
	res, err := Match(s, link.AppID, Request{IP: testIP, Fingerprint: fpPtr(androidFP())}, 15, 850)
	if err != nil {
		t.Fatalf("Match: %v", err)
	}
	if res.Matched {
		t.Errorf("click 15:01 min ago matched: %+v", res)
	}
}

// Scenario 7: window bounds, 5..180 minutes enforced; out of range errors.
func TestWindowRangeValidation(t *testing.T) {
	s, _ := newTestStore(t)
	_, link := setupApp(t, s)
	for _, w := range []int{4, 181} {
		_, err := Match(s, link.AppID, Request{IP: testIP, Fingerprint: fpPtr(androidFP())}, w, 850)
		if !errors.Is(err, ErrWindowOutOfRange) {
			t.Errorf("window %d: err = %v; want ErrWindowOutOfRange", w, err)
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
	res, err := Match(s, link.AppID, Request{ClickID: "play-old-1"}, 15, 850)
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
	res, err := Match(s, link.AppID, Request{IP: testIP, Fingerprint: fpPtr(androidFP())}, 15, 850)
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
	res, err := Match(s, link.AppID, Request{IP: testIP}, 15, 850)
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

// Scenario 20: threshold clamp 700..1200, out-of-range errors, bounds inclusive, respected even on deterministic path.
func TestThresholdClamp(t *testing.T) {
	s, _ := newTestStore(t)
	_, link := setupApp(t, s)
	recordClick(t, s, androidClick(link))
	for _, th := range []int{699, 1201} {
		_, err := Match(s, link.AppID, Request{ClickID: "nope", IP: testIP, Fingerprint: fpPtr(androidFP())}, 15, th)
		if !errors.Is(err, ErrThresholdOutOfRange) {
			t.Errorf("threshold %d: err = %v; want ErrThresholdOutOfRange", th, err)
		}
	}
	// Bounds are inclusive: 700/1200 with windows 5/180 are accepted.
	res, err := Match(s, link.AppID, Request{IP: testIP, Fingerprint: fpPtr(androidFP())}, 5, 700)
	if err != nil || !res.Matched {
		t.Errorf("threshold 700 / window 5: res = %+v, err = %v; want matched", res, err)
	}
	if _, err := Match(s, link.AppID, Request{IP: testIP, Fingerprint: fpPtr(androidFP())}, 180, 1200); err != nil {
		t.Errorf("threshold 1200 / window 180 rejected: %v", err)
	}
}

// linkWith: second link on app with per-link overrides.
func linkWith(t *testing.T, s *store.Store, appID string, threshold, window int) store.Link {
	t.Helper()
	l, err := s.CreateLink(store.Link{AppID: appID, Key: "override", URL: "https://example.com/override",
		Threshold: threshold, WindowMinutes: window})
	if err != nil {
		t.Fatalf("CreateLink: %v", err)
	}
	return l
}

func TestPerLinkThresholdOverridesGlobal(t *testing.T) {
	// IP 500 + timezone 200 + screen 200 = 900: passes global 850, fails 1000.
	fp := Fingerprint{Timezone: "Europe/Warsaw", ScreenWidth: 393, ScreenHeight: 852, Scale: 3}
	for _, tc := range []struct {
		threshold int
		want      bool
	}{{0, true}, {1000, false}} {
		s, _ := newTestStore(t)
		app, _ := setupApp(t, s)
		recordClick(t, s, androidClick(linkWith(t, s, app.ID, tc.threshold, 0)))
		res, err := Match(s, app.ID, Request{IP: testIP, Fingerprint: &fp}, 15, 850)
		if err != nil {
			t.Fatalf("Match: %v", err)
		}
		if res.Matched != tc.want {
			t.Errorf("link threshold %d: matched = %v; want %v", tc.threshold, res.Matched, tc.want)
		}
	}
}

func TestPerLinkWindowOverridesGlobal(t *testing.T) {
	// Click 60 min old outside global 15-min window; 90-min link window must still reach it (lookback covers widest window), 5-min link window must reject click global window would accept.
	for _, tc := range []struct {
		window int
		age    time.Duration
		want   bool
	}{{0, 60 * time.Minute, false}, {90, 60 * time.Minute, true}, {5, 10 * time.Minute, false}} {
		s, path := newTestStore(t)
		app, _ := setupApp(t, s)
		recordClickBackdated(t, s, path, androidClick(linkWith(t, s, app.ID, 0, tc.window)), tc.age)
		res, err := Match(s, app.ID, Request{IP: testIP, Fingerprint: fpPtr(androidFP())}, 15, 850)
		if err != nil {
			t.Fatalf("Match: %v", err)
		}
		if res.Matched != tc.want {
			t.Errorf("link window %d, click age %v: matched = %v; want %v", tc.window, tc.age, res.Matched, tc.want)
		}
	}
}
