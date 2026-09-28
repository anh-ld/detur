package config

import (
	"strings"
	"testing"
)

func clearEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{"DETUR_DOMAIN", "DETUR_ADDR", "DETUR_PORTAL_ADDR", "DETUR_DB_PATH", "DETUR_RETENTION_HOURS", "DETUR_WINDOW_MINUTES", "DETUR_THRESHOLD"} {
		t.Setenv(k, "")
	}
}

func TestLoadDefaults(t *testing.T) {
	clearEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Domain != "localhost" {
		t.Errorf("Domain default = %q, want localhost", cfg.Domain)
	}
	if cfg.HTTPAddr != ":8080" {
		t.Errorf("HTTPAddr default = %q, want :8080", cfg.HTTPAddr)
	}
	if cfg.PortalAddr != "127.0.0.1:8081" {
		t.Errorf("PortalAddr default = %q, want 127.0.0.1:8081 (loopback)", cfg.PortalAddr)
	}
	if cfg.DBPath != "detur.db" {
		t.Errorf("DBPath default = %q, want detur.db", cfg.DBPath)
	}
	if cfg.RetentionHours != 24 || cfg.WindowMinutes != 15 || cfg.Threshold != 850 {
		t.Errorf("defaults = %d/%d/%d, want 24/15/850", cfg.RetentionHours, cfg.WindowMinutes, cfg.Threshold)
	}
}

func TestLoadEnvOverride(t *testing.T) {
	clearEnv(t)
	t.Setenv("DETUR_DOMAIN", "links.example.com")
	t.Setenv("DETUR_ADDR", ":9000")
	t.Setenv("DETUR_PORTAL_ADDR", "0.0.0.0:9001")
	t.Setenv("DETUR_DB_PATH", "/data/detur.db")
	t.Setenv("DETUR_THRESHOLD", "1000")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Domain != "links.example.com" || cfg.HTTPAddr != ":9000" || cfg.PortalAddr != "0.0.0.0:9001" || cfg.DBPath != "/data/detur.db" || cfg.Threshold != 1000 {
		t.Errorf("override not applied: %+v", cfg)
	}
}

func TestLoadInvalidValues(t *testing.T) {
	clearEnv(t)
	for _, tc := range []struct {
		key, val, wantErr string
	}{
		{"DETUR_THRESHOLD", "500", "out of range"},
		{"DETUR_THRESHOLD", "not-a-number", "must be an integer"},
		{"DETUR_WINDOW_MINUTES", "200", "out of range"},
		{"DETUR_RETENTION_HOURS", "0", "out of range"},
	} {
		clearEnv(t)
		t.Setenv(tc.key, tc.val)
		_, err := Load()
		if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
			t.Errorf("%s=%s: err = %v, want containing %q", tc.key, tc.val, err, tc.wantErr)
		}
	}
}
