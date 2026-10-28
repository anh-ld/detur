package pipeline

import (
	"net/url"
	"strings"
	"testing"

	"detur.dev/server/internal/store"
)

func TestWithParams(t *testing.T) {
	const play = "https://play.google.com/store/apps/details?id=com.example"
	ppid := "1a2b3c4d-5e6f-7890-abcd-ef1234567890"
	tests := []struct {
		name, target, q, clickID string
		check                    func(t *testing.T, got string)
	}{
		{"appstore only pt/ct/mt", "https://apps.apple.com/app/id1", "pt=1&ct=c&mt=8&id=evil&utm_source=x", "", exact("https://apps.apple.com/app/id1?ct=c&mt=8&pt=1")},
		{"appstore bad ppid dropped", "https://apps.apple.com/app/id1", "ppid=not-a-uuid", "", exact("https://apps.apple.com/app/id1")},
		{"appstore good ppid", "https://apps.apple.com/app/id1", "ppid=" + ppid, "", func(t *testing.T, got string) {
			if !strings.Contains(got, "ppid="+ppid) {
				t.Fatal(got)
			}
		}},
		{"play referrer", play, "id=evil&utm_source=nl&utm_campaign=", "c1", func(t *testing.T, got string) {
			u, _ := url.Parse(got)
			if u.Query().Get("id") != "com.example" || u.Query().Has("utm_source") {
				t.Fatal(got)
			}
			if r := u.Query().Get("referrer"); r != "click_id=c1&utm_source=nl" {
				t.Fatal(r)
			}
		}},
		{"play operator referrer wins", play + "&referrer=utm_source%3Dop", "utm_source=nl", "c1", func(t *testing.T, got string) {
			u, _ := url.Parse(got)
			if r := u.Query().Get("referrer"); r != "click_id=c1&utm_source=op" {
				t.Fatal(r)
			}
		}},
		{"operator query untouched", "https://example.com/web?a=1;b=2", "", "", exact("https://example.com/web?a=1;b=2")},
		{"add-only, non-empty, non-internal", "https://example.com/web?lang=pl", "lang=en&ref=tw&_dt=1&x=", "", exact("https://example.com/web?lang=pl&ref=tw")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			q, _ := url.ParseQuery(tc.q)
			tc.check(t, withParams(tc.target, tc.clickID, q))
		})
	}
}

func exact(want string) func(*testing.T, string) {
	return func(t *testing.T, got string) {
		if got != want {
			t.Fatalf("got %s want %s", got, want)
		}
	}
}

// clickPlatform/clickKind: follow redirectTarget platform split; store target = "app", else web fallback.
func TestClickLabels(t *testing.T) {
	for _, tc := range []struct{ agent, platform string }{
		{"Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X)", "ios"},
		{"Mozilla/5.0 (Linux; Android 14; Pixel 7)", "android"},
		{"Mozilla/5.0 (Macintosh; Intel Mac OS X 14_5)", "desktop"},
	} {
		if got := clickPlatform(tc.agent); got != tc.platform {
			t.Errorf("clickPlatform(%q) = %q; want %q", tc.agent, got, tc.platform)
		}
	}
	for _, tc := range []struct{ dest, kind string }{
		{"https://apps.apple.com/app/id123", store.KindApp},
		{"https://play.google.com/store/apps/details?id=com.example&referrer=x", store.KindApp},
		{"market://details?id=com.example", store.KindApp},
		{"https://example.com/product", store.KindWeb},
	} {
		if got := clickKind(tc.dest); got != tc.kind {
			t.Errorf("clickKind(%q) = %q; want %q", tc.dest, got, tc.kind)
		}
	}
}

// redirectTarget: own store target wins; else fallback; else link.URL (app page). Desktop never gets a store page.
func TestRedirectTargetMatrix(t *testing.T) {
	const ios, android, fb, route = "https://apps.apple.com/app/id1", "https://play.google.com/store/apps/details?id=x", "https://example.com/web", "/p"
	iphone := "Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Mobile/15E148 Safari/604.1"
	pixel := "Mozilla/5.0 (Linux; Android 14; Pixel 7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Mobile Safari/537.36"
	desktop := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"
	tests := []struct {
		name       string
		link       store.Link
		agent, out string
	}{
		{"iphone own target", store.Link{URL: route, IOS: ios, Android: android, FallbackURL: fb}, iphone, ios},
		{"iphone android-only with fallback", store.Link{URL: route, Android: android, FallbackURL: fb}, iphone, fb},
		{"iphone android-only no fallback", store.Link{URL: route, Android: android}, iphone, route},
		{"android ios-only with fallback", store.Link{URL: route, IOS: ios, FallbackURL: fb}, pixel, fb},
		{"android own target", store.Link{URL: route, IOS: ios, Android: android, FallbackURL: fb}, pixel, android},
		{"desktop with fallback", store.Link{URL: route, IOS: ios, Android: android, FallbackURL: fb}, desktop, fb},
		{"desktop no fallback", store.Link{URL: route, IOS: ios, Android: android}, desktop, route},
	}
	for _, tc := range tests {
		if got := redirectTarget(tc.link, tc.agent, "", url.Values{}); got != tc.out {
			t.Errorf("%s: redirectTarget = %q; want %q", tc.name, got, tc.out)
		}
	}
}
