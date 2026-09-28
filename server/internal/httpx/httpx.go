// Package httpx shares small HTTP helpers across the API and pipeline
// packages (remote IP extraction, JSON responses).
package httpx

import (
	"encoding/json"
	"net"
	"net/http"
	"strings"
)

// TrustProxy gates the X-Forwarded-For path: only set when TLS terminates
// at a trusted reverse proxy (TRUST_PROXY). Off by default so a
// client-supplied header cannot spoof the IP match signal.
var TrustProxy bool

// RemoteIP returns the request connection IP. X-Forwarded-For (first entry)
// is honored only when TrustProxy is set: the proxy must overwrite the
// header so a direct client cannot forge it.
func RemoteIP(r *http.Request) string {
	if TrustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			if i := strings.IndexByte(xff, ','); i >= 0 {
				xff = xff[:i]
			}
			return strings.TrimSpace(xff)
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// WriteJSON writes v as a JSON response with the given status.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
