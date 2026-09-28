// Package config loads runtime configuration from the environment.
package config

import (
	"fmt"
	"os"
	"strconv"
)

// Config holds runtime configuration, read from exactly four env vars:
// DOMAIN, DB_PATH, RETENTION_HOURS, TRUST_PROXY.
type Config struct {
	Domain         string // public domain for links, well-known, redirects
	DBPath         string
	RetentionHours int  // click + event retention floor
	TrustProxy     bool // honor X-Forwarded-For (only behind a trusted proxy)
}

// Load reads configuration from the environment with sensible defaults.
func Load() (*Config, error) {
	cfg := &Config{
		Domain:         envOr("DOMAIN", "localhost"),
		DBPath:         envOr("DB_PATH", "detur.db"),
		RetentionHours: 24,
		TrustProxy:     os.Getenv("TRUST_PROXY") == "1",
	}
	var err error
	if cfg.RetentionHours, err = envIntRange("RETENTION_HOURS", cfg.RetentionHours, 24, 8760); err != nil {
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
