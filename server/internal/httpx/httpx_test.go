package httpx

import (
	"net/http/httptest"
	"testing"
)

func TestRemoteIP(t *testing.T) {
	cases := []struct {
		name  string
		trust bool
		hdr   map[string][]string
		want  string
	}{
		{"untrusted ignores headers", false, map[string][]string{"X-Forwarded-For": {"1.1.1.1"}, "X-Real-IP": {"2.2.2.2"}}, "192.0.2.1"},
		{"rightmost XFF entry", true, map[string][]string{"X-Forwarded-For": {"9.9.9.9, 3.3.3.3"}}, "3.3.3.3"},
		{"last XFF header line", true, map[string][]string{"X-Forwarded-For": {"9.9.9.9", "4.4.4.4"}}, "4.4.4.4"},
		{"XFF beats client-sent X-Real-IP", true, map[string][]string{"X-Real-IP": {"6.6.6.6"}, "X-Forwarded-For": {"6.6.6.6, 5.5.5.5"}}, "5.5.5.5"},
		{"X-Real-IP when no XFF", true, map[string][]string{"X-Real-IP": {"7.7.7.7"}}, "7.7.7.7"},
		{"no headers falls back to peer", true, nil, "192.0.2.1"},
	}
	defer func() { TrustProxy = false }()
	for _, c := range cases {
		TrustProxy = c.trust
		r := httptest.NewRequest("GET", "/", nil) // RemoteAddr 192.0.2.1:1234
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
