package webhook

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

var (
	ErrProhibitedDestination = errors.New("destination address is prohibited")
	ErrInvalidScheme         = errors.New("scheme must be https, or http for loopback")
	ErrMissingHost           = errors.New("host is required")
)

func IsLoopbackHost(host string) bool {
	h := strings.ToLower(strings.Trim(host, "[]"))
	return h == "localhost" || h == "127.0.0.1" || h == "::1"
}

// prohibitedPrefixes: non-public ranges stdlib misses. CGNAT 100.64/10 -> Alibaba metadata 100.100.100.200;
// 192.0.0/24 -> Oracle 192.0.0.192; NAT64/6to4/Teredo embed an IPv4 a gateway may route to 169.254.169.254.
var prohibitedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("2001::/32"),
	netip.MustParsePrefix("::/96"),     // IPv4-compatible (deprecated): ::a9fe:a9fe -> 169.254.169.254
	netip.MustParsePrefix("fec0::/10"), // site-local (deprecated)
}

// isProhibitedIP: private, link-local, CGNAT, cloud metadata (169.254.169.254, 100.100.100.200, 192.0.0.192),
// IPv4-embedding IPv6, loopback unless allowLoopback.
func isProhibitedIP(ip net.IP, allowLoopback bool) bool {
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return true
	}
	addr = addr.Unmap()

	if addr.IsLoopback() {
		return !allowLoopback
	}
	if addr.IsPrivate() || addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() || addr.IsMulticast() || addr.IsUnspecified() {
		return true
	}
	for _, p := range prohibitedPrefixes {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

func ValidateWebhookURL(rawURL string) error {
	u, err := url.ParseRequestURI(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	scheme := strings.ToLower(u.Scheme)
	hostname := strings.ToLower(u.Hostname())
	if hostname == "" {
		return ErrMissingHost
	}

	if scheme == "http" {
		if !IsLoopbackHost(hostname) {
			return ErrInvalidScheme
		}
	} else if scheme != "https" {
		return ErrInvalidScheme
	}

	if ip := net.ParseIP(hostname); ip != nil {
		if isProhibitedIP(ip, IsLoopbackHost(hostname)) {
			return ErrProhibitedDestination
		}
	}

	return nil
}

func NewSafeHTTPClient() *http.Client {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}

			allowLoopback := IsLoopbackHost(host)

			ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
			if err != nil {
				return nil, fmt.Errorf("resolve %s: %w", host, err)
			}
			if len(ips) == 0 {
				return nil, fmt.Errorf("no IP address found for %s", host)
			}

			for _, ip := range ips {
				if isProhibitedIP(ip, allowLoopback) {
					return nil, fmt.Errorf("%w: %s resolves to prohibited IP %s", ErrProhibitedDestination, host, ip)
				}
			}

			dialer := &net.Dialer{
				Timeout:   5 * time.Second,
				KeepAlive: 30 * time.Second,
			}
			// dial the validated IP, not the name: blocks DNS rebinding
			return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
		},
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
	}

	return &http.Client{
		Timeout:   10 * time.Second,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}
