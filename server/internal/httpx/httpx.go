// Package httpx: small HTTP helpers shared across API and pipeline packages (remote IP extraction, JSON responses).
package httpx

import (
	"encoding/json"
	"net"
	"net/http"
	"strings"
)

// TrustProxy gates X-Forwarded-For path: only set when TLS terminates at trusted reverse proxy (TRUST_PROXY). Off by default, client-supplied header cannot spoof IP match signal.
var TrustProxy bool

// RemoteIP: client IP. Trusted proxy (TrustProxy) prefers X-Real-IP (proxy-set, edge header Dub's ipAddress reads), else LAST X-Forwarded-For entry — proxies append peer they saw, earlier entries client-controlled.
func RemoteIP(r *http.Request) string {
	if TrustProxy {
		if ip := strings.TrimSpace(r.Header.Get("X-Real-IP")); ip != "" {
			return ip
		}
		if xff := r.Header.Values("X-Forwarded-For"); len(xff) > 0 {
			last := xff[len(xff)-1]
			if i := strings.LastIndexByte(last, ','); i >= 0 {
				last = last[i+1:]
			}
			if ip := strings.TrimSpace(last); ip != "" {
				return ip
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// WriteJSON: v as JSON response with given status.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
