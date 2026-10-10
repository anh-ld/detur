package pipeline

import (
	"net/url"
	"regexp"

	"detur.dev/server/internal/httpx"
	"detur.dev/server/internal/store"
	"detur.dev/server/internal/ua"
)

// internalParams: detur's own query params — consumed here, never forwarded to destination (Dub skips dub-no-track and redir_url)
var internalParams = map[string]bool{
	paramDone: true, paramScreen: true, paramTimezone: true, paramPasted: true, paramNoTrack: true, paramTap: true, paramTouch: true,
}

// redirectTarget: 302 destination by platform, fallback link.URL: iOS -> link.IOS,
// Android -> link.Android, desktop -> link.FallbackURL. Incoming query params
// forwarded per target type (see withParams); Play Store targets: clickId merged into install referrer.
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
	return withParams(target, clickID, q)
}

// deepLinkURL: link.URL + forwarded params (add-only), returned by match-link. Dub redirects to store, caches main url (link.ts cacheDeepLinkClickData).
func deepLinkURL(link store.Link, q url.Values) string {
	return withParams(link.URL, "", q)
}

// uuidRe: App Store Connect custom product page id; malformed ppid dropped (Detour click-handling)
var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// playUTM: UTM keys Detour forwards inside the Play Store referrer
var playUTM = []string{"utm_source", "utm_medium", "utm_campaign", "utm_term", "utm_content"}

// withParams: each store URL carries only its own keys, never empty values (Detour); forwarded keys only add, never overwrite operator's.
func withParams(target string, clickID string, q url.Values) string {
	u, err := url.Parse(target)
	if err != nil {
		return target
	}
	switch {
	case isAppStore(u):
		return httpx.AddParams(target, q, func(k, v string) bool {
			return v != "" && (k == "pt" || k == "ct" || k == "mt" || (k == "ppid" && uuidRe.MatchString(v)))
		})
	case isPlayStore(u):
		return playReferrer(target, q, clickID)
	}
	return httpx.AddParams(target, q, func(k, v string) bool { return v != "" && !internalParams[k] })
}

// playReferrer: operator referrer pairs + add-only q utm_* + click_id (SDK reads click_id= from decoded referrer via /(?:^|&)click_id=([^&]+)/); merged like Dub.
// Re-encodes the whole query (referrer value must be rebuilt) — Play URLs only.
func playReferrer(target string, q url.Values, clickID string) string {
	u, err := url.Parse(target)
	if err != nil {
		return target
	}
	p := u.Query()
	ref, _ := url.ParseQuery(p.Get("referrer"))
	changed := false
	for _, k := range playUTM {
		if v := q.Get(k); v != "" && !ref.Has(k) {
			ref.Set(k, v)
			changed = true
		}
	}
	if clickID != "" {
		ref.Set("click_id", clickID)
		changed = true
	}
	if !changed {
		return target
	}
	p.Set("referrer", ref.Encode())
	u.RawQuery = p.Encode()
	return u.String()
}

// clickPlatform / clickKind: analytics labels for a browser click, same platform split as redirectTarget.
func clickPlatform(agent string) string {
	switch {
	case ua.IsIOS(agent):
		return "ios"
	case ua.IsAndroid(agent):
		return "android"
	}
	return "desktop"
}

func clickKind(dest string) string {
	if u, err := url.Parse(dest); err == nil && (isAppStore(u) || isPlayStore(u)) {
		return store.KindApp
	}
	return store.KindWeb
}

// isAppStore: App Store URL (itms-apps schemes included)
func isAppStore(u *url.URL) bool {
	h := u.Hostname()
	return h == "apps.apple.com" || h == "itunes.apple.com" || u.Scheme == "itms-apps" || u.Scheme == "itms-appss"
}

// isPlayStore: Play Store URL — only target passing install referrer to app (Dub is-google-play-store-url.ts, plus market://)
func isPlayStore(u *url.URL) bool {
	return u.Hostname() == "play.google.com" || u.Scheme == "market"
}
