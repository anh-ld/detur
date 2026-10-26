// Package config: runtime config from env.
package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
)

// Config: runtime config from env vars: DOMAIN, DB_PATH, RETENTION_HOURS, CLICK_ID_DAYS, TRUST_PROXY, TRUSTED_PROXIES, LOGOUT_URL, ADMIN_PASSWORD, ADMIN_SESSION_HOURS, PORTAL_HOSTS.
type Config struct {
	Domain            string // public domain for links, well-known, redirects
	DBPath            string
	RetentionHours    int      // click + event retention floor
	ClickIDDays       int      // unmatched click_id lifetime
	TrustProxy        bool     // honor X-Forwarded-For (only behind a trusted proxy)
	TrustedProxies    []string // TRUSTED_PROXIES: CIDR ranges, comma-separated; empty = default RFC 1918 + loopback
	LogoutURL         string   // gateway sign-out URL for the Log out link; "" hides it
	AdminPassword     string   // master password for admin elevation; "" = no gating
	AdminSessionHours int      // elevated session TTL (1-72h, default 12)
	PortalHosts       []string // PORTAL_HOSTS: gateway hostnames, comma-separated
}

// Load: config from env, with defaults.
func Load() (*Config, error) {
	cfg := &Config{
		Domain:            envOr("DOMAIN", "localhost"),
		DBPath:            envOr("DB_PATH", "detur.db"),
		RetentionHours:    24,
		ClickIDDays:       30,
		TrustProxy:        os.Getenv("TRUST_PROXY") == "1",
		LogoutURL:         os.Getenv("LOGOUT_URL"),
		AdminPassword:     os.Getenv("ADMIN_PASSWORD"),
		AdminSessionHours: 12,
	}
	for _, h := range strings.Split(os.Getenv("PORTAL_HOSTS"), ",") {
		if h = strings.TrimSpace(h); h != "" {
			cfg.PortalHosts = append(cfg.PortalHosts, h)
		}
	}
	var err error
	if cfg.RetentionHours, err = envIntRange("RETENTION_HOURS", cfg.RetentionHours, 24, 8760); err != nil {
		return nil, err
	}
	// 90 = Play Install Referrer limit
	if cfg.ClickIDDays, err = envIntRange("CLICK_ID_DAYS", cfg.ClickIDDays, 1, 90); err != nil {
		return nil, err
	}
	if cfg.AdminSessionHours, err = envIntRange("ADMIN_SESSION_HOURS", cfg.AdminSessionHours, 1, 72); err != nil {
		return nil, err
	}
	if err := checkLogoutURL(cfg.LogoutURL); err != nil {
		return nil, err
	}
	if cfg.TrustedProxies, err = parseTrustedProxies(os.Getenv("TRUSTED_PROXIES")); err != nil {
		return nil, err
	}
	return cfg, nil
}

// parseTrustedProxies: comma-separated list of CIDR strings.
func parseTrustedProxies(v string) ([]string, error) {
	var out []string
	for _, raw := range strings.Split(v, ",") {
		s := strings.TrimSpace(raw)
		if s == "" {
			continue
		}
		if _, _, err := net.ParseCIDR(s); err != nil {
			return nil, fmt.Errorf("TRUSTED_PROXIES: invalid CIDR %q: %w", s, err)
		}
		out = append(out, s)
	}
	return out, nil
}

// checkLogoutURL: "", http(s) URL, or "/path". Rejects the rest (javascript:, "//host"): it lands in an href.
func checkLogoutURL(v string) error {
	if v == "" {
		return nil
	}
	if strings.HasPrefix(v, "/") && !strings.HasPrefix(v, "//") {
		return nil
	}
	u, err := url.Parse(v)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("LOGOUT_URL: must be an http(s) URL or a path starting with /, got %q", v)
	}
	return nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// envIntRange: int env var; malformed/out-of-range = fail fast, no silent misconfig.
func envIntRange(key string, def, min, max int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("%s: must be an integer, got %q", key, v)
	}
	if n < min || n > max {
		return 0, fmt.Errorf("%s: %d out of range [%d, %d]", key, n, min, max)
	}
	return n, nil
}
