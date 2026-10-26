package pipeline

import (
	"net/http"
	"strings"
	"testing"

	"detur.dev/server/internal/store"
)

// Desktop + app-scheme target, no fallback: page with QR, alt text, short link; store buttons only when set.
func TestDesktopSchemeTargetGetsPage(t *testing.T) {
	ts, s, _ := newPipelineServer(t)
	app, _ := setupPipeline(t, s)
	if _, err := s.CreateLink(store.Link{AppID: app.ID, Key: "app", URL: "myapp://product/1", IOS: "https://apps.apple.com/app/id123"}); err != nil {
		t.Fatal(err)
	}
	resp, body := doGET(t, ts, "/app?ref=mail&_dt=1", map[string]string{"User-Agent": desktopUA})
	page := string(body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d; want 200 page", resp.StatusCode)
	}
	short := ts.URL + "/app?ref=mail"
	for _, want := range []string{`src="data:image/png;base64,`, `alt="QR code for ` + short + `"`, `<code id="short">` + short + `</code>`, "App Store", "pipeline test app"} {
		if !strings.Contains(page, want) {
			t.Errorf("page missing %q:\n%s", want, page)
		}
	}
	if strings.Contains(page, "Google Play") {
		t.Error("Play button without an Android target")
	}
	if n, _ := s.CountClicks(app.ID); n != 1 {
		t.Errorf("clicks = %d; want 1 recorded", n)
	}
	// market: is a store target but not a web page a desktop can open
	if _, err := s.CreateLink(store.Link{AppID: app.ID, Key: "mkt", URL: "myapp://p", Android: "market://details?id=x"}); err != nil {
		t.Fatal(err)
	}
	if _, body := doGET(t, ts, "/mkt", map[string]string{"User-Agent": desktopUA}); strings.Contains(string(body), "Google Play") {
		t.Error("Play button for a market: target")
	}
}

// Web fallback keeps the 302; bots never get the page.
func TestDesktopPageOnlyForSchemeTargets(t *testing.T) {
	ts, s, _ := newPipelineServer(t)
	app, link := setupPipeline(t, s)
	if resp, _ := doGET(t, ts, "/"+link.Key, map[string]string{"User-Agent": desktopUA}); resp.StatusCode != http.StatusFound {
		t.Errorf("web fallback: status = %d; want 302", resp.StatusCode)
	}
	if _, err := s.CreateLink(store.Link{AppID: app.ID, Key: "app", URL: "myapp://product/1"}); err != nil {
		t.Fatal(err)
	}
	if resp, _ := doGET(t, ts, "/app", map[string]string{"User-Agent": "Googlebot/2.1"}); resp.StatusCode != http.StatusFound {
		t.Errorf("bot: status = %d; want 302", resp.StatusCode)
	}
}
