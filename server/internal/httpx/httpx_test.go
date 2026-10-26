package httpx

import (
	"net/http/httptest"
	"testing"
)

func TestRemoteIP(t *testing.T) {
	cases := []struct {
		name       string
		trust      bool
		remoteAddr string
		hdr        map[string][]string
		want       string
	}{
		{
			name:       "untrusted proxy setting ignores headers",
			trust:      false,
			remoteAddr: "127.0.0.1:1234",
			hdr:        map[string][]string{"X-Forwarded-For": {"1.1.1.1"}, "X-Real-IP": {"2.2.2.2"}},
			want:       "127.0.0.1",
		},
		{
			name:       "untrusted peer outside CIDR ranges ignores headers (AE3)",
			trust:      true,
			remoteAddr: "203.0.113.50:5678",
			hdr:        map[string][]string{"X-Forwarded-For": {"198.51.100.1"}, "X-Real-IP": {"198.51.100.1"}},
			want:       "203.0.113.50",
		},
		{
			name:       "trusted loopback peer honors rightmost XFF",
			trust:      true,
			remoteAddr: "127.0.0.1:1234",
			hdr:        map[string][]string{"X-Forwarded-For": {"9.9.9.9, 3.3.3.3"}},
			want:       "3.3.3.3",
		},
		{
			name:       "trusted private 10.x peer honors last XFF line",
			trust:      true,
			remoteAddr: "10.0.1.5:1234",
			hdr:        map[string][]string{"X-Forwarded-For": {"9.9.9.9", "4.4.4.4"}},
			want:       "4.4.4.4",
		},
		{
			name:       "trusted private 172.16.x peer XFF beats client-sent X-Real-IP",
			trust:      true,
			remoteAddr: "172.16.0.2:1234",
			hdr:        map[string][]string{"X-Real-IP": {"6.6.6.6"}, "X-Forwarded-For": {"6.6.6.6, 5.5.5.5"}},
			want:       "5.5.5.5",
		},
		{
			name:       "trusted private 192.168.x peer X-Real-IP when no XFF",
			trust:      true,
			remoteAddr: "192.168.1.50:1234",
			hdr:        map[string][]string{"X-Real-IP": {"7.7.7.7"}},
			want:       "7.7.7.7",
		},
		{
			name:       "trusted ipv6 loopback peer honors XFF",
			trust:      true,
			remoteAddr: "[::1]:1234",
			hdr:        map[string][]string{"X-Forwarded-For": {"8.8.8.8"}},
			want:       "8.8.8.8",
		},
		{
			name:       "no headers falls back to peer",
			trust:      true,
			remoteAddr: "10.0.0.1:1234",
			hdr:        nil,
			want:       "10.0.0.1",
		},
	}

	defer func() {
		TrustProxy = false
		_ = SetTrustedProxies(nil)
	}()

	for _, c := range cases {
		TrustProxy = c.trust
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = c.remoteAddr
		for k, vs := range c.hdr {
			for _, v := range vs {
				r.Header.Add(k, v)
			}
		}
		if got := RemoteIP(r); got != c.want {
			t.Errorf("%s: RemoteIP = %q; want %q", c.name, got, c.want)
		}
	}
}

func TestSetTrustedProxies(t *testing.T) {
	defer func() {
		TrustProxy = false
		_ = SetTrustedProxies(nil)
	}()

	TrustProxy = true

	// Custom CIDR allows previously untrusted public IP range
	err := SetTrustedProxies([]string{"203.0.113.0/24"})
	if err != nil {
		t.Fatalf("SetTrustedProxies: %v", err)
	}

	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "203.0.113.50:1234"
	r.Header.Set("X-Forwarded-For", "198.51.100.1")

	if got := RemoteIP(r); got != "198.51.100.1" {
		t.Errorf("Custom CIDR: RemoteIP = %q, want 198.51.100.1", got)
	}

	// Malformed CIDR fails
	if err := SetTrustedProxies([]string{"invalid-cidr"}); err == nil {
		t.Errorf("SetTrustedProxies(invalid) want error, got nil")
	}

	// Reset reverts to defaults: 203.0.113.50 is untrusted again and increments XFFIgnored
	_ = SetTrustedProxies(nil)
	if XFFIgnored() != 0 || LastXFFPeer() != "" {
		t.Errorf("After SetTrustedProxies(nil): XFFIgnored=%d LastXFFPeer=%q; want 0 and empty", XFFIgnored(), LastXFFPeer())
	}
	if got := RemoteIP(r); got != "203.0.113.50" {
		t.Errorf("After SetTrustedProxies(nil): RemoteIP = %q, want socket peer 203.0.113.50", got)
	}
	if XFFIgnored() != 1 || LastXFFPeer() != "203.0.113.50" {
		t.Errorf("After untrusted XFF: XFFIgnored=%d LastXFFPeer=%q; want 1 and 203.0.113.50", XFFIgnored(), LastXFFPeer())
	}
	if err := SetTrustedProxies([]string{"10.0.0.0/8"}); err != nil {
		t.Fatalf("SetTrustedProxies: %v", err)
	}
	if XFFIgnored() != 0 || LastXFFPeer() != "" {
		t.Errorf("After SetTrustedProxies re-baseline: XFFIgnored=%d LastXFFPeer=%q; want 0 and empty", XFFIgnored(), LastXFFPeer())
	}
}
