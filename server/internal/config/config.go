// Package config loads runtime configuration from the environment.
package config

import (
	"fmt"
	"os"
	"strconv"
)

// Config holds runtime configuration. All fields come from environment
// variables; unset fields fall back to defaults (see Load).
type Config struct {
	Domain         string // public base domain for links, well-known, redirects
	HTTPAddr       string // SDK + pipeline listen address
	PortalAddr     string // portal listen address (separate listener, loopback default)
	DBPath         string
	RetentionHours int // click retention floor (deterministic lookups)
	WindowMinutes  int // default probabilistic match window
	Threshold      int // default match threshold
}

// Load reads configuration from the environment with sensible defaults.
func Load() (*Config, error) {
	cfg := &Config{
		Domain:         envOr("DETUR_DOMAIN", "localhost"),
		HTTPAddr:       envOr("DETUR_ADDR", ":8080"),
		PortalAddr:     envOr("DETUR_PORTAL_ADDR", "127.0.0.1:8081"),
		DBPath:         envOr("DETUR_DB_PATH", "detur.db"),
		RetentionHours: 24,
		WindowMinutes:  15,
		Threshold:      850,
	}
	var err error
	if cfg.RetentionHours, err = envIntRange("DETUR_RETENTION_HOURS", cfg.RetentionHours, 1, 8760); err != nil {
		return nil, err
	}
	if cfg.WindowMinutes, err = envIntRange("DETUR_WINDOW_MINUTES", cfg.WindowMinutes, 5, 180); err != nil {
		return nil, err
	}
	if cfg.Threshold, err = envIntRange("DETUR_THRESHOLD", cfg.Threshold, 700, 1200); err != nil {
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
