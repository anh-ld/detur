package pipeline

// In-app browser tap page: recognized in-app visitors get a tapped "Get the app" link instead of the blocked 302.

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"detur.dev/server/internal/store"
	"detur.dev/server/internal/ua"
)

const (
	messengerIOSUA     = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_2 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Mobile/15E148 [FBAN/MessengerForiOS;FBAV/442.0.0.42.110;FBDV/iPhone15,2;FBSN/iOS;FBSV/17.2]"
	messengerAndroidUA = "Mozilla/5.0 (Linux; Android 14; Pixel 7 Build/UP1A.231005.007; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/125.0.0.0 Mobile Safari/537.36 [FB_IAB/MESSENGER;FBAV/450.0.0.42.109;]"
)

// Messenger iOS, App Store target: auto first hop (no copy page), tap page with App Store link + Messenger steps, one click tagged messenger.
func TestInAppIOSGetsTapPage(t *testing.T) {
	ts, s, _ := newPipelineServer(t)
	app, link := setupPipeline(t, s)
	hdr := map[string]string{"User-Agent": messengerIOSUA}
	resp, body := doGET(t, ts, "/"+link.Key, hdr)
	if resp.StatusCode != http.StatusOK || strings.Contains(string(body), "Open in App Store") {
		t.Fatalf("first hop = %d, copy page %v; want 200 auto page", resp.StatusCode, strings.Contains(string(body), "Open in App Store"))
	}
	resp, body = doGET(t, ts, "/"+link.Key+"?_dt=1", hdr)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("hop 2 = %d; want 200 tap page", resp.StatusCode)
	}
	page := string(body)
	for _, want := range []string{"Get the app", `href="https://apps.apple.com/app/id123"`, "Safari"} {
		if !strings.Contains(page, want) {
			t.Errorf("tap page missing %q:\n%s", want, page)
		}
	}
	if strings.Contains(page, "Open Play Store") {
		t.Error("iOS tap page shows the Android Play link")
	}
	clicks := latestClicks(t, s, app.ID)
	if len(clicks) != 1 || clicks[0].Source != "messenger" || clicks[0].Kind != store.KindApp {
		t.Fatalf("clicks = %+v; want 1 app click tagged messenger", clicks)
	}
}

// Messenger Android with the package set: intent:// button back to the short link, Play fallback carrying click_id, plain Play link as backup.
func TestInAppAndroidIntentButton(t *testing.T) {
	ts, s, _ := newPipelineServer(t)
	app, link := setupPipeline(t, s)
	if err := s.UpdateAppDetails(app.ID, "", "com.example", "AA:BB"); err != nil {
		t.Fatalf("UpdateAppDetails: %v", err)
	}
	resp, body := mobileClick(t, ts, "/"+link.Key, map[string]string{"User-Agent": messengerAndroidUA})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("hop 2 = %d; want 200 tap page", resp.StatusCode)
	}
	clicks := latestClicks(t, s, app.ID)
	if len(clicks) != 1 {
		t.Fatalf("clicks = %d; want 1", len(clicks))
	}
	page := string(body)
	host := strings.TrimPrefix(ts.URL, "http://")
	fallback := url.QueryEscape("https://play.google.com/store/apps/details?id=com.example&referrer=click_id%3D" + clicks[0].ID)
	for _, want := range []string{
		"intent://" + host + "/" + link.Key + "#Intent;scheme=https;package=com.example;S.browser_fallback_url=" + fallback + ";end",
		"Open Play Store", "Chrome",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("tap page missing %q:\n%s", want, page)
		}
	}
	if strings.Contains(page, "_tap=") || strings.Contains(page, "_dt=") {
		t.Errorf("tap page leaks internal params:\n%s", page)
	}
}

// No Android package: primary button is the Play URL itself (no intent).
func TestInAppAndroidWithoutPackageUsesPlay(t *testing.T) {
	ts, s, _ := newPipelineServer(t)
	_, link := setupPipeline(t, s)
	_, body := mobileClick(t, ts, "/"+link.Key, map[string]string{"User-Agent": messengerAndroidUA})
	page := string(body)
	if strings.Contains(page, "intent://") || !strings.Contains(page, `href="https://play.google.com/store/apps/details?id=com.example&amp;referrer=click_id`) {
		t.Errorf("want plain Play button, got:\n%s", page)
	}
}

// No store target for the platform -> today's redirect, even in-app or with _tap; _tap never forwarded.
func TestInAppWithoutStoreTargetRedirects(t *testing.T) {
	ts, s, _ := newPipelineServer(t)
	app, _ := setupPipeline(t, s)
	if _, err := s.CreateLink(store.Link{AppID: app.ID, Key: "web", URL: "https://example.com/x"}); err != nil {
		t.Fatalf("CreateLink: %v", err)
	}
	for _, path := range []string{"/web?_dt=1", "/web?_dt=1&_tap=1"} {
		resp, _ := doGET(t, ts, path, map[string]string{"User-Agent": messengerIOSUA})
		loc := resp.Header.Get("Location")
		if resp.StatusCode != http.StatusFound || !strings.HasPrefix(loc, "https://example.com/x") || strings.Contains(loc, "_tap") {
			t.Errorf("%s = %d %q; want 302 to link URL without _tap", path, resp.StatusCode, loc)
		}
	}
}

// Safety-net reload (_tap) from a plain browser: tap page, same click refreshed (no second count).
func TestTapParamServesTapPageWithoutSecondClick(t *testing.T) {
	ts, s, _ := newPipelineServer(t)
	app, link := setupPipeline(t, s)
	hdr := map[string]string{"User-Agent": iosUA}
	mobileClick(t, ts, "/"+link.Key, hdr)
	resp, body := doGET(t, ts, "/"+link.Key+"?_dt=1&_tap=1", hdr)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "Get the app") {
		t.Fatalf("_tap = %d; want 200 tap page", resp.StatusCode)
	}
	if n := len(latestClicks(t, s, app.ID)); n != 1 {
		t.Errorf("clicks = %d; want 1", n)
	}
	a, err := s.Analytics(app.ID, 1, "", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if got := a.Days[0]; got.Clicks != 1 || got.Web != 0 {
		t.Errorf("today = %+v; want 1 store click", got)
	}
}

// A generic webview is recorded unknown-inapp but keeps the redirect (safety net only).
func TestGenericWebviewTaggedKeepsRedirect(t *testing.T) {
	ts, s, _ := newPipelineServer(t)
	app, link := setupPipeline(t, s)
	webview := "Mozilla/5.0 (iPhone; CPU iPhone OS 17_2 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Mobile/15E148"
	resp, _ := mobileClick(t, ts, "/"+link.Key, map[string]string{"User-Agent": webview})
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("hop 2 = %d; want 302", resp.StatusCode)
	}
	if c := latestClicks(t, s, app.ID); len(c) != 1 || c[0].Source != ua.SourceUnknownInApp {
		t.Fatalf("clicks = %+v; want 1 tagged unknown-inapp", c)
	}
}

// Bots never get the tap page.
func TestBotNeverGetsTapPage(t *testing.T) {
	ts, s, _ := newPipelineServer(t)
	_, link := setupPipeline(t, s)
	resp, _ := doGET(t, ts, "/"+link.Key+"?bot=1&_dt=1", map[string]string{"User-Agent": messengerIOSUA})
	if resp.StatusCode != http.StatusFound {
		t.Errorf("bot = %d; want 302", resp.StatusCode)
	}
}

// Safety net delay: short for generic webviews, longer for real browsers, off for recognized in-app (hop 2 already serves the tap page).
func TestSafetyNetDelay(t *testing.T) {
	for _, c := range []struct {
		source string
		want   int
	}{{ua.SourceUnknownInApp, 1500}, {"", 4000}, {"messenger", 0}} {
		if got := safetyNetDelay(c.source); got != c.want {
			t.Errorf("safetyNetDelay(%q) = %d; want %d", c.source, got, c.want)
		}
	}
	// the hop-1 page carries the delay chosen for its UA
	ts, s, _ := newPipelineServer(t)
	_, link := setupPipeline(t, s)
	webview := "Mozilla/5.0 (Linux; Android 14; Pixel 7 Build/UP1A.231005.007; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/125.0.0.0 Mobile Safari/537.36"
	for _, c := range []struct {
		agent string
		ms    int
	}{{webview, 1500}, {androidUA, 4000}, {messengerAndroidUA, 0}} {
		_, body := doGET(t, ts, "/"+link.Key, map[string]string{"User-Agent": c.agent})
		if !regexp.MustCompile(fmt.Sprintf(`var delay =\s*%d\s*,`, c.ms)).Match(body) {
			t.Errorf("%s: hop-1 page lacks delay %d", c.agent, c.ms)
		}
	}
}

// "Open in browser" wording: the app's own menu item, generic for unknown webviews, else the platform browser; platform menu icon.
func TestTapSteps(t *testing.T) {
	for _, c := range []struct {
		source string
		ios    bool
		icon   string
		label  string
	}{
		{"instagram", true, "···", "Open in external browser"},
		{"zalo", false, "⋮", "Open in browser"},
		{"messenger", true, "···", "Open in Safari"},
		{"messenger", false, "⋮", "Open in Chrome"},
		{ua.SourceUnknownInApp, true, "···", "Open in browser"},
		{"", false, "⋮", "Open in browser"},
	} {
		got := tapSteps(c.source, c.ios)
		if len(got) != 2 || !strings.Contains(got[0], c.icon) || !strings.Contains(got[1], "“"+c.label+"”") {
			t.Errorf("tapSteps(%q, ios=%v) = %q; want icon %s, label %q", c.source, c.ios, got, c.icon, c.label)
		}
	}
}

// Copy page: copy and navigate in the same tap handler (no promise before navigation), noscript still reaches hop 2.
func TestCopyPageNavigatesInSameTap(t *testing.T) {
	ts, s, _ := newPipelineServer(t)
	_, link := setupPipeline(t, s)
	_, body := doGET(t, ts, "/"+link.Key, map[string]string{"User-Agent": iosUA})
	page := string(body)
	if !strings.Contains(page, "Open in App Store") || strings.Contains(page, ".then(") {
		t.Errorf("copy page: want tap handler without promise chain:\n%s", page)
	}
	if !strings.Contains(page, `<noscript><meta http-equiv="refresh" content="0;url=?_dt=1">`) {
		t.Errorf("noscript fallback missing:\n%s", page)
	}
}

// iOS tap page copies + reports pasted_link (keepalive refresh, same click); Android page has no script.
func TestInAppIOSTapReportsPastedLink(t *testing.T) {
	ts, s, _ := newPipelineServer(t)
	app, link := setupPipeline(t, s)
	hdr := map[string]string{"User-Agent": messengerIOSUA}
	_, body := doGET(t, ts, "/"+link.Key+"?_dt=1", hdr)
	if page := string(body); !strings.Contains(page, "pasted_link") || !strings.Contains(page, `execCommand("copy")`) {
		t.Fatalf("iOS tap page missing copy/report script:\n%s", page)
	}
	pasted := ts.URL + "/" + link.Key
	doGET(t, ts, "/"+link.Key+"?_dt=1&pasted_link="+url.QueryEscape(pasted), hdr)
	c := latestClicks(t, s, app.ID)
	if len(c) != 1 || c[0].Fingerprint.PastedLink != pasted || c[0].Source != "messenger" {
		t.Fatalf("clicks = %+v; want 1 messenger click with pasted_link", c)
	}
	_, body = mobileClick(t, ts, "/"+link.Key, map[string]string{"User-Agent": messengerAndroidUA})
	if strings.Contains(string(body), "pasted_link") {
		t.Error("Android tap page carries the iOS copy script")
	}
}

// The tap page drops _tap from its own URL, so "Open in browser" reopens without it (real browser -> 302).
func TestTapPageStripsTapParam(t *testing.T) {
	ts, s, _ := newPipelineServer(t)
	_, link := setupPipeline(t, s)
	_, body := doGET(t, ts, "/"+link.Key+"?_dt=1&_tap=1", map[string]string{"User-Agent": iosUA})
	if !strings.Contains(string(body), `history.replaceState`) {
		t.Errorf("tap page does not strip _tap:\n%s", body)
	}
}

// market:// Android target: the Play backup link keeps its scheme (no #ZgotmplZ).
func TestTapPageMarketBackupLink(t *testing.T) {
	ts, s, _ := newPipelineServer(t)
	app, _ := setupPipeline(t, s)
	if err := s.UpdateAppDetails(app.ID, "", "com.example", "AA:BB"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateLink(store.Link{AppID: app.ID, Key: "mkt", URL: "https://example.com/m", Android: "market://details?id=com.example"}); err != nil {
		t.Fatal(err)
	}
	_, body := mobileClick(t, ts, "/mkt", map[string]string{"User-Agent": messengerAndroidUA})
	if page := string(body); strings.Contains(page, "ZgotmplZ") || !strings.Contains(page, `href="market://details?id=com.example`) {
		t.Errorf("market backup link broken:\n%s", page)
	}
}

// Android in-app visitor on a link whose Android target is not a Play URL: plain redirect.
func TestInAppAndroidWebTargetRedirects(t *testing.T) {
	ts, s, _ := newPipelineServer(t)
	app, _ := setupPipeline(t, s)
	if _, err := s.CreateLink(store.Link{AppID: app.ID, Key: "aw", URL: "https://example.com/aw", Android: "https://example.com/android-web"}); err != nil {
		t.Fatal(err)
	}
	resp, _ := mobileClick(t, ts, "/aw", map[string]string{"User-Agent": messengerAndroidUA})
	if resp.StatusCode != http.StatusFound || !strings.HasPrefix(resp.Header.Get("Location"), "https://example.com/android-web") {
		t.Errorf("= %d %q; want 302 to the Android web target", resp.StatusCode, resp.Header.Get("Location"))
	}
}
