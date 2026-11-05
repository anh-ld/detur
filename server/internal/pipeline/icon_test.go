package pipeline

// Store icon on the tap page: App Store lookup or Play og:image, generic glyph until one is known.

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"detur.dev/server/internal/store"
)

func TestMain(m *testing.M) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/lookup":
			q := r.URL.Query()
			if q.Get("id") == "123" || (q.Get("id") == "456" && q.Get("country") == "vn") {
				fmt.Fprint(w, `{"resultCount":1,"results":[{"artworkUrl512":"https://is1.example/icon512.png"}]}`)
				return
			}
			fmt.Fprint(w, `{"resultCount":0,"results":[]}`)
		case "/details":
			if r.URL.Query().Get("id") == "com.playonly" {
				fmt.Fprint(w, `<html><head><meta property="og:image" content="https://play-lh.example/icon?a=1&amp;b=2"></head></html>`)
				return
			}
			http.NotFound(w, r)
		}
	}))
	appIcons = newIconCache(ts.URL+"/lookup", ts.URL+"/details")
	code := m.Run()
	ts.Close()
	os.Exit(code)
}

func TestIconKeys(t *testing.T) {
	for _, c := range []struct {
		ios, android, pkg string
		want              string
	}{
		{"https://apps.apple.com/vn/app/demo/id123", "https://play.google.com/store/apps/details?id=com.demo", "", "a:vn:123 g:com.demo"},
		{"https://apps.apple.com/app/id123", "", "", "a:us:123"},
		{"https://apps.apple.com/us/app/id3d-game/id999?mt=8", "", "", "a:us:999"},
		{"", "https://play.google.com/store/apps/details?id=com.link", "com.app", "g:com.link"},
		{"", "", "com.app", "g:com.app"},
		{"https://example.com/id123", "https://example.com", "", ""},
		{"", "https://play.google.com/store/apps/details?id=bad pkg", "", ""},
	} {
		if got := strings.Join(iconKeys(c.ios, c.android, c.pkg), " "); got != c.want {
			t.Errorf("iconKeys(%q, %q, %q) = %q; want %q", c.ios, c.android, c.pkg, got, c.want)
		}
	}
}

// waitIcon: icon for keys once the background fetch lands.
func waitIcon(t *testing.T, keys []string) string {
	t.Helper()
	for i := 0; i < 100; i++ {
		if u := appIcons.first(keys); u != "" {
			return u
		}
		time.Sleep(10 * time.Millisecond)
	}
	return ""
}

func TestIconFetch(t *testing.T) {
	if got := waitIcon(t, []string{"a:us:123"}); got != "https://is1.example/icon512.png" {
		t.Errorf("App Store icon = %q", got)
	}
	if got := waitIcon(t, []string{"a:us:999", "g:com.playonly"}); got != "https://play-lh.example/icon?a=1&b=2" {
		t.Errorf("Play fallback icon = %q", got)
	}
	appIcons.fetch("a:us:404")
	if got := cachedIcon(appIcons, "a:us:404"); got != "" {
		t.Errorf("unknown app icon = %q; want none", got)
	}
}

// cachedIcon: cached URL for key, without starting a fetch.
func cachedIcon(c *iconCache, key string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.m[key].url
}

// stubCache: cache whose App Store lookup answers with artwork (empty = no result) and counts requests.
func stubCache(t *testing.T, artwork string) (*iconCache, *atomic.Int32) {
	t.Helper()
	hits := &atomic.Int32{}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if artwork == "" {
			fmt.Fprint(w, `{"resultCount":0,"results":[]}`)
			return
		}
		fmt.Fprintf(w, `{"resultCount":1,"results":[{"artworkUrl512":%q}]}`, artwork)
	}))
	t.Cleanup(ts.Close)
	return newIconCache(ts.URL, ts.URL), hits
}

// only https artwork reaches <img src>: http and javascript URLs are dropped.
func TestIconRejectsNonHTTPS(t *testing.T) {
	for _, art := range []string{"http://is1.example/icon.png", "javascript:alert(1)"} {
		c, _ := stubCache(t, art)
		c.fetch("a:us:1")
		if got := cachedIcon(c, "a:us:1"); got != "" {
			t.Errorf("artwork %q cached as %q; want none", art, got)
		}
	}
}

// visitors arriving together start one fetch, not one each.
func TestIconOneFetchPerKey(t *testing.T) {
	c, hits := stubCache(t, "")
	for i := 0; i < 5; i++ {
		c.first([]string{"a:us:1"})
	}
	for i := 0; i < 100 && hits.Load() == 0; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond)
	if n := hits.Load(); n != 1 {
		t.Errorf("store hits = %d; want 1", n)
	}
}

// a failed refresh keeps serving the old icon.
func TestIconFailedRefreshKeepsOld(t *testing.T) {
	c, _ := stubCache(t, "")
	c.m["a:us:1"] = iconEntry{url: "https://old.example/icon.png", exp: time.Now().Add(-time.Minute)}
	if got := c.first([]string{"a:us:1"}); got != "https://old.example/icon.png" {
		t.Fatalf("expired entry = %q; want old icon", got)
	}
	c.fetch("a:us:1")
	if got := cachedIcon(c, "a:us:1"); got != "https://old.example/icon.png" {
		t.Errorf("after failed refresh = %q; want old icon", got)
	}
}

// tap page: fallback glyph before the icon is known, store icon after.
func TestTapPageIcon(t *testing.T) {
	ts, s, _ := newPipelineServer(t)
	_, link := setupPipeline(t, s)
	hdr := map[string]string{"User-Agent": messengerIOSUA}
	appIcons.mu.Lock()
	delete(appIcons.m, "a:us:123")
	appIcons.mu.Unlock()
	_, body := doGET(t, ts, "/"+link.Key+"?_dt=1", hdr)
	if !strings.Contains(string(body), `<img class="icon" src="data:image/svg+xml,`) {
		t.Errorf("first visit: want fallback icon:\n%s", body)
	}
	waitIcon(t, []string{"a:us:123"})
	_, body = doGET(t, ts, "/"+link.Key+"?_dt=1", hdr)
	if !strings.Contains(string(body), `<img class="icon" src="https://is1.example/icon512.png"`) {
		t.Errorf("later visit: want store icon:\n%s", body)
	}
}

// a successful lookup is kept for the long TTL, not the failure one.
func TestIconSuccessCachedLong(t *testing.T) {
	c, _ := stubCache(t, "https://is1.example/icon.png")
	c.fetch("a:us:1")
	c.mu.Lock()
	e := c.m["a:us:1"]
	c.mu.Unlock()
	if e.url != "https://is1.example/icon.png" || time.Until(e.exp) <= iconFailTTL {
		t.Errorf("entry = %+v; want icon cached ~%s", e, iconTTL)
	}
}

// lookup uses the link's storefront: an app only in the VN store gets its icon.
func TestIconStorefront(t *testing.T) {
	if got := waitIcon(t, []string{"a:vn:456"}); got != "https://is1.example/icon512.png" {
		t.Errorf("VN storefront icon = %q", got)
	}
}

// App Store miss -> icon from the app's Android package (link has no Play URL), through the tap page.
func TestTapPageIconFromAppPackage(t *testing.T) {
	ts, s, _ := newPipelineServer(t)
	app, err := s.CreateApp("pkg app", testAPIKey+"2")
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	if err := s.UpdateAppDetails(app.ID, "", "com.playonly", "", ""); err != nil {
		t.Fatalf("UpdateAppDetails: %v", err)
	}
	link, err := s.CreateLink(store.Link{AppID: app.ID, Key: "pkgicon", URL: "https://example.com/p", IOS: "https://apps.apple.com/app/id777"})
	if err != nil {
		t.Fatalf("CreateLink: %v", err)
	}
	hdr := map[string]string{"User-Agent": messengerIOSUA}
	doGET(t, ts, "/"+link.Key+"?_dt=1", hdr)
	waitIcon(t, []string{"a:us:777", "g:com.playonly"})
	_, body := doGET(t, ts, "/"+link.Key+"?_dt=1", hdr)
	if !strings.Contains(string(body), `<img class="icon" src="https://play-lh.example/icon?a=1&amp;b=2"`) {
		t.Errorf("want Play icon from app package:\n%s", body)
	}
}
