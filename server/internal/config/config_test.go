package config

import (
	"strings"
	"testing"
)

func clearEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{"DOMAIN", "DB_PATH", "RETENTION_HOURS", "TRUST_PROXY", "LOGOUT_URL"} {
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
	if cfg.DBPath != "detur.db" {
		t.Errorf("DBPath default = %q, want detur.db", cfg.DBPath)
	}
	if cfg.RetentionHours != 24 {
		t.Errorf("RetentionHours default = %d, want 24", cfg.RetentionHours)
	}
}

func TestLoadEnvOverride(t *testing.T) {
	clearEnv(t)
	t.Setenv("DOMAIN", "links.example.com")
	t.Setenv("TRUST_PROXY", "1")
	t.Setenv("DB_PATH", "/data/detur.db")
	t.Setenv("RETENTION_HOURS", "48")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Domain != "links.example.com" || !cfg.TrustProxy || cfg.DBPath != "/data/detur.db" || cfg.RetentionHours != 48 {
		t.Errorf("override not applied: %+v", cfg)
	}
}

func TestLoadInvalidValues(t *testing.T) {
	clearEnv(t)
	for _, tc := range []struct {
		key, val, wantErr string
	}{
		{"RETENTION_HOURS", "23", "out of range"},
		{"RETENTION_HOURS", "not-a-number", "must be an integer"},
		{"LOGOUT_URL", "javascript:alert(1)", "LOGOUT_URL"},
		{"LOGOUT_URL", "//evil.example/logout", "LOGOUT_URL"},
		{"LOGOUT_URL", "ftp://example.com/logout", "LOGOUT_URL"},
	} {
		clearEnv(t)
		t.Setenv(tc.key, tc.val)
		_, err := Load()
		if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
			t.Errorf("%s=%s: err = %v, want containing %q", tc.key, tc.val, err, tc.wantErr)
		}
	}
}

func TestLoadLogoutURL(t *testing.T) {
	for _, v := range []string{"", "/cdn-cgi/access/logout", "/?gcp-iap-mode=CLEAR_LOGIN_COOKIE", "https://auth.example.com/logout"} {
		clearEnv(t)
		t.Setenv("LOGOUT_URL", v)
		cfg, err := Load()
		if err != nil {
			t.Errorf("LOGOUT_URL=%q: %v", v, err)
			continue
		}
		if cfg.LogoutURL != v {
			t.Errorf("LogoutURL = %q, want %q", cfg.LogoutURL, v)
		}
	}
}
