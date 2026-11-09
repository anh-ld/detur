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

// RemoteIP: client IP. TrustProxy: verifies peer IP against trusted CIDRs, then walks XFF
// right to left (each proxy appends its peer; leftmost entries client-set), skipping trusted
// proxies (CDN -> ingress chains), else X-Real-IP.
// Untrusted peers ignore forwarded headers to prevent IP spoofing.
func RemoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if TrustProxy {
		if isTrustedPeer(host) {
			if ip := forwardedClient(r.Header.Values("X-Forwarded-For")); ip != "" {
				return ip
			}
			if ip := hostOnly(strings.TrimSpace(r.Header.Get("X-Real-IP"))); ip != "" {
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

// forwardedClient: rightmost XFF entry outside TRUSTED_PROXIES; all trusted -> leftmost. "" when empty.
func forwardedClient(xff []string) string {
	entries := strings.Split(strings.Join(xff, ","), ",")
	ip := ""
	for i := len(entries) - 1; i >= 0; i-- {
		e := hostOnly(strings.TrimSpace(entries[i]))
		if e == "" {
			continue
		}
		ip = e
		if !isTrustedPeer(e) {
			break
		}
	}
	return ip
}

// hostOnly: forwarded entry without a port (Azure gateways send "ip:port", IPv6 as "[v6]:port"); bare IPs unchanged.
func hostOnly(e string) string {
	if net.ParseIP(e) != nil {
		return e
	}
	if h, _, err := net.SplitHostPort(e); err == nil {
		return h
	}
	return strings.Trim(e, "[]")
}

var peers, privatePeers atomic.Int64

func notePeer(host string) {
	peers.Add(1)
	if ip := net.ParseIP(host); ip != nil && (ip.IsPrivate() || ip.IsLoopback()) {
		privatePeers.Add(1)
	}
}

func PeerStats() (total, private int64) {
	return peers.Load(), privatePeers.Load()
}

var xffIgnored atomic.Int64

var lastXFFPeer atomic.Value

func XFFIgnored() int64 { return xffIgnored.Load() }

func LastXFFPeer() string {
	if v, ok := lastXFFPeer.Load().(string); ok {
		return v
	}
	return ""
}

func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

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

func Origin(r *http.Request) string {
	if r.TLS == nil && strings.Contains(r.Host, ":") {
		return "http://" + r.Host
	}
	return "https://" + r.Host
}

func IsRoute(s string) bool {
	return strings.HasPrefix(s, "/") && !strings.HasPrefix(s, "//")
}
