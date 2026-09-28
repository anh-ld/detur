// Package config loads runtime configuration from the environment.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config holds runtime configuration. All fields come from environment
// variables; unset fields fall back to defaults (see Load).
type Config struct {
	Domain         string   // public base domain for links, well-known, redirects
	HTTPAddr       string   // SDK + pipeline listen address
	PortalAddr     string   // portal listen address (separate listener, loopback default)
	PortalDir      string   // portal static files dir (built UI)
	PortalHosts    []string // extra Host values the portal guard accepts (zero-trust tunnels)
	ExtraDomains   []string // extra domains served for links/well-known
	DBPath         string
	RetentionHours int  // click + event retention floor (KTD4)
	TrustProxy     bool // honor X-Forwarded-For (set when TLS terminates at a trusted proxy)
}

// Load reads configuration from the environment with sensible defaults.
func Load() (*Config, error) {
	cfg := &Config{
		Domain:         envOr("DETUR_DOMAIN", "localhost"),
		HTTPAddr:       envOr("DETUR_ADDR", ":8080"),
		PortalAddr:     envOr("DETUR_PORTAL_ADDR", "127.0.0.1:8081"),
		PortalDir:      envOr("DETUR_PORTAL_DIR", "portal/dist"),
		PortalHosts:    envList("DETUR_PORTAL_HOSTS"),
		ExtraDomains:   envList("DETUR_EXTRA_DOMAINS"),
		DBPath:         envOr("DETUR_DB_PATH", "detur.db"),
		RetentionHours: 24,
		TrustProxy:     os.Getenv("DETUR_TRUST_PROXY") == "1",
	}
	var err error
	if cfg.RetentionHours, err = envIntRange("DETUR_RETENTION_HOURS", cfg.RetentionHours, 1, 8760); err != nil {
		return nil, err
	}
	return cfg, nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// envList parses a comma-separated env var into a trimmed, de-duplicated,
// non-empty slice.
func envList(key string) []string {
	var out []string
	seen := make(map[string]bool)
	for _, v := range strings.Split(os.Getenv(key), ",") {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

// envIntRange parses an int env var; out-of-range or malformed values fail
// fast with a clear error so a misconfigured deploy never runs silently.
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
