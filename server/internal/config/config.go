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

type Config struct {
	Domain            string
	DBPath            string
	RetentionHours    int
	ClickIDDays       int
	TrustProxy        bool
	TrustedProxies    []string
	LogoutURL         string
	AdminPassword     string
	AdminSessionHours int
	PortalHosts       []string
}

func Load() (*Config, error) {
	cfg := &Config{
		Domain:            normalizeDomain(envOr("DOMAIN", "localhost")),
		DBPath:            envOr("DB_PATH", "detur.db"),
		RetentionHours:    24,
		ClickIDDays:       30,
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
	if cfg.TrustProxy, err = envBool("TRUST_PROXY"); err != nil {
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

func normalizeDomain(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	if i := strings.Index(v, "://"); i >= 0 {
		v = v[i+3:]
	}
	if i := strings.IndexAny(v, "/?#"); i >= 0 {
		v = v[:i]
	}
	return strings.TrimSuffix(v, ".")
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envBool(key string) (bool, error) {
	switch v := strings.ToLower(strings.TrimSpace(os.Getenv(key))); v {
	case "", "0", "false", "no", "off":
		return false, nil
	case "1", "true", "yes", "on":
		return true, nil
	default:
		return false, fmt.Errorf("%s: must be 1/true/yes/on or 0/false/no/off, got %q", key, v)
	}
}

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
