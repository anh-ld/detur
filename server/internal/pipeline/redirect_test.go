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

// clickPlatform/clickKind: analytics labels follow redirectTarget's platform split; store targets are "app", anything else is a web fallback.
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
