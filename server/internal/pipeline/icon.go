package pipeline

import (
	"encoding/json"
	"html"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"detur.dev/server/internal/store"
)

// Store icon for the tap page: App Store via the iTunes Lookup API, Play via the listing's og:image (Play has no public API).

var (
	reAppStorePath = regexp.MustCompile(`^/(?:([A-Za-z]{2})/)?app/(?:[^/]+/)?id(\d+)/?$`) // [/<country>]/app[/<slug>]/id<n>
	reOGImage      = regexp.MustCompile(`<meta[^>]+property="og:image"[^>]+content="([^"]+)"`)
)

// icon cache key prefixes: "a:<country>:<id>", "g:<package>"
const (
	keyAppStore = "a:"
	keyPlay     = "g:"
)

// iconKeys: cache keys for the link's store targets, App Store first; storefront from the URL (Lookup defaults to US); app package covers a link without a Play URL.
func iconKeys(iosURL, androidURL, androidPkg string) []string {
	var keys []string
	if u, err := url.Parse(iosURL); err == nil && isAppStore(u) {
		if m := reAppStorePath.FindStringSubmatch(u.Path); m != nil {
			country := strings.ToLower(m[1])
			if country == "" {
				country = "us"
			}
			keys = append(keys, keyAppStore+country+":"+m[2])
		}
	}
	pkg := androidPkg
	if u, err := url.Parse(androidURL); err == nil && isPlayStore(u) {
		if id := u.Query().Get("id"); id != "" {
			pkg = id
		}
	}
	if store.AndroidPackageRe.MatchString(pkg) {
		keys = append(keys, keyPlay+pkg)
	}
	return keys
}

// iconCache: icon URL per store key. Page never waits: miss -> background fetch, later visits get the icon.
// In-memory, per process, lost on restart; persist on the app row if lookups ever matter.
type iconCache struct {
	appStore, play string // endpoints
	client         *http.Client
	mu             sync.Mutex
	m              map[string]iconEntry
}

type iconEntry struct {
	url string
	exp time.Time // refetch after
}

const (
	iconTTL     = 24 * time.Hour
	iconFailTTL = time.Hour
)

var appIcons = newIconCache("https://itunes.apple.com/lookup", "https://play.google.com/store/apps/details")

func newIconCache(appStore, play string) *iconCache {
	// nil Transport = http.DefaultTransport, which the scenarios tests stub (scenarios/stores_test.go)
	return &iconCache{appStore: appStore, play: play, client: &http.Client{Timeout: 5 * time.Second}, m: map[string]iconEntry{}}
}

// first: first known icon among keys ("" = none yet -> fallback icon). Keys up to the hit start a background fetch when stale; later ones aren't refreshed.
func (c *iconCache) first(keys []string) string {
	for _, k := range keys {
		if u := c.get(k); u != "" {
			return u
		}
	}
	return ""
}

// get: cached icon for key ("" while unknown); a miss or expiry claims one background fetch.
func (c *iconCache) get(key string) string {
	c.mu.Lock()
	e, ok := c.m[key]
	stale := !ok || time.Now().After(e.exp)
	if stale {
		c.m[key] = iconEntry{url: e.url, exp: time.Now().Add(iconFailTTL)}
	}
	c.mu.Unlock()
	if stale {
		go c.fetch(key)
	}
	return e.url
}

func (c *iconCache) fetch(key string) {
	var icon string
	if rest, ok := strings.CutPrefix(key, keyAppStore); ok {
		country, id, _ := strings.Cut(rest, ":")
		icon = c.appStoreIcon(country, id)
	} else if pkg, ok := strings.CutPrefix(key, keyPlay); ok {
		icon = c.playIcon(pkg)
	}
	if !strings.HasPrefix(icon, "https://") {
		log.Printf("store icon lookup failed for %s (fallback icon, retry in %s)", key, iconFailTTL)
		return
	}
	c.mu.Lock()
	c.m[key] = iconEntry{url: icon, exp: time.Now().Add(iconTTL)}
	c.mu.Unlock()
}

// body: GET u, at most 2 MB of a 200 response; nil on any failure.
func (c *iconCache) body(u string) []byte {
	resp, err := c.client.Get(u)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	return b
}

// appStoreIcon: largest artwork from the iTunes Lookup API, in the link's storefront.
func (c *iconCache) appStoreIcon(country, id string) string {
	var r struct {
		Results []struct {
			Art512 string `json:"artworkUrl512"`
			Art100 string `json:"artworkUrl100"`
		} `json:"results"`
	}
	if json.Unmarshal(c.body(c.appStore+"?country="+url.QueryEscape(country)+"&id="+url.QueryEscape(id)), &r) != nil || len(r.Results) == 0 {
		return ""
	}
	if r.Results[0].Art512 != "" {
		return r.Results[0].Art512
	}
	return r.Results[0].Art100
}

// playIcon: og:image of the Play listing (the app icon).
func (c *iconCache) playIcon(pkg string) string {
	m := reOGImage.FindSubmatch(c.body(c.play + "?hl=en&id=" + url.QueryEscape(pkg)))
	if m == nil {
		return ""
	}
	return html.UnescapeString(string(m[1]))
}
