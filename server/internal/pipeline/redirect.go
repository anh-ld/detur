package pipeline

import (
	"net/url"

	"detur.dev/server/internal/store"
	"detur.dev/server/internal/ua"
)

// redirectTarget picks the 302 destination by platform, falling back to link.URL:
// iOS -> link.IOS, Android -> link.Android + clickId as Play install referrer,
// desktop -> link.FallbackURL. ppid and dtb pass through everywhere.
func redirectTarget(link store.Link, agent string, clickID string, q url.Values) string {
	target := link.URL
	platform := ""
	switch {
	case ua.IsIOS(agent):
		platform = "ios"
		if link.IOS != "" {
			target = link.IOS
		}
	case ua.IsAndroid(agent):
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
		// SDK reads click_id= from the referrer (/(?:^|&)click_id=([^&]+)/ after
		// decodeURIComponent); Encode's escaping round-trips.
		p.Set("referrer", "click_id="+clickID)
	}
	u.RawQuery = p.Encode()
	return u.String()
}
