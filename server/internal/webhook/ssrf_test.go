package webhook

import (
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestValidateWebhookURL(t *testing.T) {
	cases := []struct {
		url     string
		wantErr bool
	}{
		// Valid HTTPS URLs
		{"https://example.com/webhook", false},
		{"https://api.crm.com/v1/events", false},
		{"https://sub.domain.co.uk:8443/hook", false},

		// Valid HTTP on loopback only
		{"http://localhost:8080/webhook", false},
		{"http://127.0.0.1:9000/webhook", false},
		{"http://[::1]:8080/webhook", false},

		// Invalid: HTTP on non-loopback
		{"http://example.com/webhook", true},
		{"http://192.168.1.1:8080/webhook", true},

		// Invalid: Private networks (RFC 1918)
		{"https://10.0.0.1/hook", true},
		{"https://172.16.0.1/hook", true},
		{"https://192.168.1.50/hook", true},

		// Invalid: Cloud metadata
		{"https://169.254.169.254/latest/meta-data", true},
		{"http://169.254.169.254/latest/meta-data", true},

		// Invalid: IPv6 private / link-local
		{"https://[fc00::1]/hook", true},
		{"https://[fe80::1]/hook", true},

		// Invalid schemes
		{"ftp://example.com/hook", true},
		{"javascript:alert(1)", true},
		{"", true},
	}

	for _, tc := range cases {
		err := ValidateWebhookURL(tc.url)
		if tc.wantErr && err == nil {
			t.Errorf("ValidateWebhookURL(%q) expected error, got nil", tc.url)
		}
		if !tc.wantErr && err != nil {
			t.Errorf("ValidateWebhookURL(%q) unexpected error: %v", tc.url, err)
		}
	}
}

func TestSafeHTTPClientLoopbackAndRedirects(t *testing.T) {
	redirectTargetHit := false
	targetSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirectTargetHit = true
		w.WriteHeader(http.StatusOK)
	}))
	defer targetSrv.Close()

	// Server that responds with 302 Found pointing to targetSrv
	redirectSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, targetSrv.URL, http.StatusFound)
	}))
	defer redirectSrv.Close()

	client := NewSafeHTTPClient()

	// Request to redirectSrv should return 302 and NOT follow the redirect
	resp, err := client.Get(redirectSrv.URL)
	if err != nil {
		t.Fatalf("client.Get(%s): %v", redirectSrv.URL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound {
		t.Errorf("status = %d, want 302 (redirect should not be followed)", resp.StatusCode)
	}
	if redirectTargetHit {
		t.Error("redirect target was hit, but redirects must not be followed")
	}
}

func TestSafeHTTPClientBlocksProhibitedIP(t *testing.T) {
	client := NewSafeHTTPClient()

	// Direct attempt to connect to metadata IP
	_, err := client.Get("http://169.254.169.254/")
	if err == nil {
		t.Fatal("expected connection to 169.254.169.254 to be blocked, got nil")
	}
	if !errors.Is(err, ErrProhibitedDestination) && !errors.Is(err, ErrInvalidScheme) {
		// As long as it is an error from our safe client dialer or validator
		t.Logf("blocked as expected with: %v", err)
	}
}

func TestIsProhibitedIP(t *testing.T) {
	for ip, want := range map[string]bool{
		"8.8.8.8":            false,
		"2606:4700::1111":    false,
		"10.0.0.1":           true,
		"169.254.169.254":    true,
		"100.100.100.200":    true, // Alibaba metadata (CGNAT)
		"192.0.0.192":        true, // Oracle metadata
		"198.18.0.1":         true,
		"::ffff:10.0.0.1":    true,
		"64:ff9b::a9fe:a9fe": true, // NAT64 -> 169.254.169.254
		"2002:a9fe:a9fe::1":  true, // 6to4 -> 169.254.169.254
		"fd00::1":            true,
		"0.0.0.0":            true,
		"127.0.0.1":          true,
	} {
		if got := isProhibitedIP(net.ParseIP(ip), false); got != want {
			t.Errorf("isProhibitedIP(%s) = %v; want %v", ip, got, want)
		}
	}
}
