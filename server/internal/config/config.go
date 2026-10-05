// Package config loads runtime configuration from the environment.
package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
)

// Config: runtime config from env vars: DOMAIN, DB_PATH, RETENTION_HOURS, CLICK_ID_DAYS, TRUST_PROXY, LOGOUT_URL.
type Config struct {
	Domain         string // public domain for links, well-known, redirects
	DBPath         string
	RetentionHours int    // click + event retention floor
	ClickIDDays    int    // unmatched click_id lifetime
	TrustProxy     bool   // honor X-Forwarded-For (only behind a trusted proxy)
	LogoutURL      string // gateway sign-out URL for the Log out link; "" hides it
}

// Load: configuration from environment, sensible defaults.
func Load() (*Config, error) {
	cfg := &Config{
		Domain:         envOr("DOMAIN", "localhost"),
		DBPath:         envOr("DB_PATH", "detur.db"),
		RetentionHours: 24,
		ClickIDDays:    30,
		TrustProxy:     os.Getenv("TRUST_PROXY") == "1",
		LogoutURL:      os.Getenv("LOGOUT_URL"),
	}
	var err error
	if cfg.RetentionHours, err = envIntRange("RETENTION_HOURS", cfg.RetentionHours, 24, 8760); err != nil {
		return nil, err
	}
	// 90 = Play Install Referrer limit
	if cfg.ClickIDDays, err = envIntRange("CLICK_ID_DAYS", cfg.ClickIDDays, 1, 90); err != nil {
		return nil, err
	}
	if err := checkLogoutURL(cfg.LogoutURL); err != nil {
		return nil, err
	}
	return cfg, nil
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

// envIntRange: int env var; out-of-range or malformed values fail fast with clear error, misconfigured deploy never runs silently.
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
