package webhook

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var (
	ErrProhibitedDestination = errors.New("destination address is prohibited")
	ErrInvalidScheme         = errors.New("scheme must be https, or http for loopback")
	ErrMissingHost           = errors.New("host is required")
)

// isLoopbackHost checks whether a hostname refers to local loopback.
func isLoopbackHost(host string) bool {
	h := strings.ToLower(strings.Trim(host, "[]"))
	return h == "localhost" || h == "127.0.0.1" || h == "::1"
}

// isProhibitedIP returns true if the IP belongs to a private network, link-local,
// cloud instance metadata (169.254.169.254), or loopback (unless allowLoopback is true).
func isProhibitedIP(ip net.IP, allowLoopback bool) bool {
	if ip == nil {
		return true
	}
	// Normalize IPv4-mapped IPv6 (::ffff:192.0.2.1 -> 192.0.2.1)
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}

	if ip.IsLoopback() {
		return !allowLoopback
	}

	if ip.IsPrivate() { // RFC 1918 (10/8, 172.16/12, 192.168/16) and RFC 4193 (fc00::/7)
		return true
	}

	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return true
	}

	// Explicit check for cloud metadata address 169.254.169.254
	if ip.Equal(net.ParseIP("169.254.169.254")) {
		return true
	}

	return false
}

// ValidateWebhookURL verifies that the URL has an acceptable scheme and is not a prohibited private/metadata IP.
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
		if !isLoopbackHost(hostname) {
			return ErrInvalidScheme
		}
	} else if scheme != "https" {
		return ErrInvalidScheme
	}

	// If hostname is an IP literal, validate immediately
	if ip := net.ParseIP(hostname); ip != nil {
		if isProhibitedIP(ip, isLoopbackHost(hostname)) {
			return ErrProhibitedDestination
		}
	}

	return nil
}

// NewSafeHTTPClient returns an http.Client configured with:
// 1. Dial-time IP validation (SSRF defense against RFC 1918, RFC 4193, link-local, cloud metadata).
// 2. Disabled automatic redirect following (prevents SSRF via 3xx to internal resources).
// 3. 10-second request timeout.
func NewSafeHTTPClient() *http.Client {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}

			allowLoopback := isLoopbackHost(host)

			ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
			if err != nil {
				return nil, fmt.Errorf("resolve %s: %w", host, err)
			}
			if len(ips) == 0 {
				return nil, fmt.Errorf("no IP address found for %s", host)
			}

			// Validate every resolved IP
			for _, ip := range ips {
				if isProhibitedIP(ip, allowLoopback) {
					return nil, fmt.Errorf("%w: %s resolves to prohibited IP %s", ErrProhibitedDestination, host, ip)
				}
			}

			// Pin connection to the first validated IP to prevent DNS rebinding
			dialer := &net.Dialer{
				Timeout:   5 * time.Second,
				KeepAlive: 30 * time.Second,
			}
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
			// Disables redirect-following
			return http.ErrUseLastResponse
		},
	}
}
