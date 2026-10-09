// Package httpx: small HTTP helpers shared across API and pipeline packages (remote IP extraction, JSON responses).
package httpx

import (
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
)

// TrustProxy gates X-Forwarded-For path: only set when TLS terminates at trusted reverse proxy (TRUST_PROXY). Off by default, client-supplied header cannot spoof IP match signal.
var TrustProxy bool

// RemoteIP: client IP. TrustProxy: last X-Forwarded-For entry (proxy appends its peer; earlier entries client-set), else X-Real-IP. Caddy/ALB/Cloudflare pass client X-Real-IP through; Dub trusts it only because Vercel sets it.
func RemoteIP(r *http.Request) string {
	if TrustProxy {
		if xff := r.Header.Values("X-Forwarded-For"); len(xff) > 0 {
			last := xff[len(xff)-1]
			if i := strings.LastIndexByte(last, ','); i >= 0 {
				last = last[i+1:]
			}
			if ip := strings.TrimSpace(last); ip != "" {
				return ip
			}
		}
		if ip := strings.TrimSpace(r.Header.Get("X-Real-IP")); ip != "" {
			return ip
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	if !TrustProxy {
		notePeer(host)
	}
	return host
}

// Socket peers, TRUST_PROXY off. Mostly private = proxy in front, one shared IP.
// Since start, not sliding; resets on restart.
var peers, privatePeers atomic.Int64

func notePeer(host string) {
	peers.Add(1)
	if ip := net.ParseIP(host); ip != nil && (ip.IsPrivate() || ip.IsLoopback()) {
		privatePeers.Add(1)
	}
}

// PeerStats: peers seen, private/loopback count.
func PeerStats() (total, private int64) {
	return peers.Load(), privatePeers.Load()
}

// WriteJSON: v as JSON response with given status.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// AddParams: appends allowed q keys missing from target's query; operator's query bytes untouched, nothing added -> target unchanged.
func AddParams(target string, q url.Values, allow func(k, v string) bool) string {
	u, err := url.Parse(target)
	if err != nil {
		return target
	}
	have := u.Query()
	add := url.Values{}
	for k, vs := range q {
		if len(vs) == 0 || have.Has(k) || !allow(k, vs[len(vs)-1]) {
			continue
		}
		add.Set(k, vs[len(vs)-1])
	}
	if len(add) == 0 {
		return target
	}
	if u.RawQuery != "" {
		u.RawQuery += "&"
	}
	u.RawQuery += add.Encode()
	return u.String()
}
