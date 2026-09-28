package pipeline

import (
	"net/url"
	"strings"

	"detur.dev/server/internal/store"
)

// redirectTarget picks the 302 destination per the platform contract (R10):
// iOS -> App Store (link.IOS, else link.URL); Android -> Play (link.Android
// with the recorded clickId as the install referrer, else link.URL); desktop
// -> fallback (link.FallbackURL, else link.URL). Reserved params ppid and dtb
// pass through on every platform (KD4, link.ts reserved-param behavior). The
// referrer is url.Values.Encode()-escaped, so the SDK reads the clickId from
// the Play install referrer and sends it to match-link (AE1 leg).
func redirectTarget(link store.Link, ua string, clickID string, q url.Values) string {
	target := link.URL
	platform := ""
	switch {
	case isIOS(ua):
		platform = "ios"
		if link.IOS != "" {
			target = link.IOS
		}
	case isAndroid(ua):
		platform = "android"
		if link.Android != "" {
			target = link.Android
		}
	default:
		if link.FallbackURL != "" {
			target = link.FallbackURL
		}
	}
	u, err := url.Parse(target)
	if err != nil {
		return target
	}
	p := u.Query()
	if v := q.Get("ppid"); v != "" {
		p.Set("ppid", v)
	}
	if v := q.Get("dtb"); v != "" {
		p.Set("dtb", v)
	}
	if platform == "android" && clickID != "" {
		p.Set("referrer", clickID)
	}
	u.RawQuery = p.Encode()
	return u.String()
}

// isIOS reports an iOS browser UA (iPhone/iPad/iOS).
func isIOS(ua string) bool {
	ua = strings.ToLower(ua)
	return strings.Contains(ua, "iphone") || strings.Contains(ua, "ipad") || strings.Contains(ua, "ios")
}

// isAndroid reports an Android browser UA.
func isAndroid(ua string) bool {
	return strings.Contains(strings.ToLower(ua), "android")
}
