// Package httpx: shared HTTP helpers (remote IP, JSON responses).
package httpx

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
)

// TrustProxy: honor X-Forwarded-For (TRUST_PROXY). Set only when TLS ends at a trusted proxy; off = client header can't spoof IP match signal.
var TrustProxy bool

var defaultTrustedCIDRStrings = []string{
	"127.0.0.1/32",
	"::1/128",
	"10.0.0.0/8",
	"172.16.0.0/12",
	"192.168.0.0/16",
}

var (
	trustedMu   sync.RWMutex
	trustedNets []*net.IPNet
)

func init() {
	_ = SetTrustedProxies(nil)
}

// SetTrustedProxies parses and sets the allowed CIDR ranges for reverse proxies.
// If cidrs is empty or nil, it resets to default private/loopback CIDRs.
func SetTrustedProxies(cidrs []string) error {
	list := cidrs
	if len(list) == 0 {
		list = defaultTrustedCIDRStrings
	}
	nets := make([]*net.IPNet, 0, len(list))
	for _, c := range list {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		_, ipNet, err := net.ParseCIDR(c)
		if err != nil {
			return fmt.Errorf("invalid CIDR %q: %w", c, err)
		}
		nets = append(nets, ipNet)
	}
	trustedMu.Lock()
	trustedNets = nets
	// Re-baseline: the counters mean "since the current TRUSTED_PROXIES configuration".
	xffIgnored.Store(0)
	lastXFFPeer.Store("")
	trustedMu.Unlock()
	return nil
}

func isTrustedPeer(host string) bool {
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	trustedMu.RLock()
	defer trustedMu.RUnlock()
	for _, n := range trustedNets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// RemoteIP: client IP. TrustProxy: verifies peer IP against trusted CIDRs, then parses
// last XFF entry (proxy appends its peer; earlier entries client-set), else X-Real-IP.
// Untrusted peers ignore forwarded headers to prevent IP spoofing (R4, R6).
func RemoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if TrustProxy {
		if isTrustedPeer(host) {
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
		} else if r.Header.Get("X-Forwarded-For") != "" || r.Header.Get("X-Real-IP") != "" {
			xffIgnored.Add(1)
			lastXFFPeer.Store(host)
		}
	} else {
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

// Forwarded headers ignored from peers outside TRUSTED_PROXIES while TRUST_PROXY=1; since start.
var xffIgnored atomic.Int64

// Host of the most recent untrusted peer whose forwarded headers were ignored; for the health page.
var lastXFFPeer atomic.Value

// XFFIgnored: X-Forwarded-For/X-Real-IP ignores from untrusted peers.
func XFFIgnored() int64 { return xffIgnored.Load() }

// LastXFFPeer: most recent untrusted peer that sent ignored forwarded headers; "" if none.
func LastXFFPeer() string {
	if v, ok := lastXFFPeer.Load().(string); ok {
		return v
	}
	return ""
}

// WriteJSON: v as JSON with status.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// AddParams: append allowed q keys missing from target's query; operator's query bytes untouched; nothing added -> target unchanged.
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

// Origin: scheme + host of the request. Dev listener (host:port, plain HTTP) -> http; prod sits behind TLS on the bare domain.
func Origin(r *http.Request) string {
	if r.TLS == nil && strings.Contains(r.Host, ":") {
		return "http://" + r.Host
	}
	return "https://" + r.Host
}

// IsRoute: app route destination ("/product/42?x=1"), not a URL. "//host" is protocol-relative, not a route.
func IsRoute(s string) bool {
	return strings.HasPrefix(s, "/") && !strings.HasPrefix(s, "//")
}
