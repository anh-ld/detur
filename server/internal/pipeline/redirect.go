package pipeline

import (
	"net/url"

	"detur.dev/server/internal/store"
	"detur.dev/server/internal/ua"
)

// internalParams: detur's own query params — consumed here, never forwarded to destination (Dub skips dub-no-track and redir_url)
var internalParams = map[string]bool{
	paramDone: true, paramScreen: true, paramTimezone: true, paramPasted: true, paramNoTrack: true,
}

// redirectTarget: 302 destination by platform, fallback link.URL: iOS -> link.IOS,
// Android -> link.Android, desktop -> link.FallbackURL. Every incoming query param
// except detur's own forwarded, overriding same-name keys (Dub get-final-url.ts);
// Play Store targets: clickId merged into install referrer.
func redirectTarget(link store.Link, agent string, clickID string, q url.Values) string {
	target := link.URL
	switch {
	case ua.IsIOS(agent):
		if link.IOS != "" {
			target = link.IOS
		}
	case ua.IsAndroid(agent):
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
	for k, vs := range q {
		if !internalParams[k] && len(vs) > 0 {
			p.Set(k, vs[len(vs)-1])
		}
	}
	if clickID != "" && isPlayStore(u) {
		// Merge into operator's referrer (utm_* etc.), like Dub; SDK reads click_id= from decoded referrer via /(?:^|&)click_id=([^&]+)/, stays its own key=value pair
		ref, _ := url.ParseQuery(p.Get("referrer"))
		ref.Set("click_id", clickID)
		p.Set("referrer", ref.Encode())
	}
	u.RawQuery = p.Encode()
	return u.String()
}

// isPlayStore: Play Store URL — only target passing install referrer to app (Dub is-google-play-store-url.ts, plus market://)
func isPlayStore(u *url.URL) bool {
	return u.Hostname() == "play.google.com" || u.Scheme == "market"
}
