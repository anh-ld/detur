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
	reAppStorePath = regexp.MustCompile(`^/(?:([A-Za-z]{2})/)?app/(?:[^/]+/)?id(\d+)/?$`)
	reOGImage      = regexp.MustCompile(`<meta[^>]+property="og:image"[^>]+content="([^"]+)"`)
)

const (
	keyAppStore = "a:"
	keyPlay     = "g:"
)

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

type iconCache struct {
	appStore, play string
	client         *http.Client
	mu             sync.Mutex
	m              map[string]iconEntry
}

type iconEntry struct {
	url string
	exp time.Time
}

const (
	iconTTL     = 24 * time.Hour
	iconFailTTL = time.Hour
)

var appIcons = newIconCache("https://itunes.apple.com/lookup", "https://play.google.com/store/apps/details")

func newIconCache(appStore, play string) *iconCache {
	return &iconCache{appStore: appStore, play: play, client: &http.Client{Timeout: 5 * time.Second}, m: map[string]iconEntry{}}
}

func (c *iconCache) first(keys []string) string {
	for _, k := range keys {
		if u := c.get(k); u != "" {
			return u
		}
	}
	return ""
}

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

func (c *iconCache) playIcon(pkg string) string {
	m := reOGImage.FindSubmatch(c.body(c.play + "?hl=en&id=" + url.QueryEscape(pkg)))
	if m == nil {
		return ""
	}
	return html.UnescapeString(string(m[1]))
}
