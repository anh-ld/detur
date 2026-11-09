package pipeline

import (
	"net/url"
	"regexp"

	"detur.dev/server/internal/httpx"
	"detur.dev/server/internal/store"
	"detur.dev/server/internal/ua"
)

func forwardable(k, v string) bool { return v != "" && !store.InternalParams[k] }

func redirectTarget(link store.Link, platform string, clickID string, q url.Values) string {
	target := link.URL
	if link.FallbackURL != "" {
		target = link.FallbackURL
	}
	switch platform {
	case ua.PlatformIOS:
		if link.IOS != "" {
			target = link.IOS
		}
	case ua.PlatformAndroid:
		if link.Android != "" {
			target = link.Android
		}
	}
	return withParams(target, clickID, q)
}

func deepLinkURL(link store.Link, q url.Values) string {
	if d := withParams(link.URL, "", q); len(d) <= store.MaxClickURL {
		return d
	}
	return link.URL
}

var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// playUTM: UTM keys Detour forwards inside Play Store referrer.
var playUTM = []string{"utm_source", "utm_medium", "utm_campaign", "utm_term", "utm_content"}

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
	return httpx.AddParams(target, q, forwardable)
}

// playReferrer: operator referrer pairs + add-only q utm_* + click_id (SDK reads click_id= from decoded referrer via /(?:^|&)click_id=([^&]+)/); merged like Dub.
// Re-encodes whole query (referrer value rebuilt); Play URLs only.
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

func clickKind(dest string) string {
	if httpx.IsRoute(dest) || isAppStoreURL(dest) || isPlayStoreURL(dest) {
		return store.KindApp
	}
	return store.KindWeb
}

func isAppStore(u *url.URL) bool {
	h := u.Hostname()
	return h == "apps.apple.com" || h == "itunes.apple.com" || u.Scheme == "itms-apps" || u.Scheme == "itms-appss"
}

func isPlayStore(u *url.URL) bool {
	return u.Hostname() == "play.google.com" || u.Scheme == "market"
}
