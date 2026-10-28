package scenarios

// Infra / deployment / platform-integration scenarios: Apple + Google verification files, Host handling,
// reverse proxies and client IP, health, SQLite robustness, request limits, config from env.

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"detur.dev/server/internal/api"
	"detur.dev/server/internal/config"
	"detur.dev/server/internal/httpx"
	"detur.dev/server/internal/pipeline"
	"detur.dev/server/internal/store"
)

const infraFP2 = "CD:CD:CD:CD:CD:CD:CD:CD:CD:CD:CD:CD:CD:CD:CD:CD:CD:CD:CD:CD:CD:CD:CD:CD:CD:CD:CD:CD:CD:CD:CD:CD"

var infraFP1 = strings.TrimSuffix(strings.Repeat("AB:", 32), ":")

type infraAASA struct {
	AppLinks *struct {
		Apps    []string `json:"apps"`
		Details []struct {
			AppID      string              `json:"appID"`
			AppIDs     []string            `json:"appIDs"`
			Paths      []string            `json:"paths"`
			Components []map[string]string `json:"components"`
		} `json:"details"`
	} `json:"applinks"`
}

type infraAssetLink struct {
	Relation []string `json:"relation"`
	Target   struct {
		Namespace    string   `json:"namespace"`
		PackageName  string   `json:"package_name"`
		Fingerprints []string `json:"sha256_cert_fingerprints"`
	} `json:"target"`
}

// infraWK: well-known fetch the way Apple's CDN / Google's verifier do it (GET, given Host, no redirect follow).
func infraWK(t *testing.T, e *env, method, path, host string) resp {
	t.Helper()
	return do(t, method, e.Pub.URL+path, "", map[string]string{"Host": host, "User-Agent": "AASA-Bot/1.0"})
}

func infraAASAOf(t *testing.T, r resp) infraAASA {
	t.Helper()
	must(t, r.Code == 200, "AASA = %d %q", r.Code, r.Body)
	var a infraAASA
	r.JSON(t, &a)
	must(t, a.AppLinks != nil, "AASA missing applinks: %s", r.Body)
	return a
}

// infraApp: extra app with its own key and platform identity.
func infraApp(t *testing.T, e *env, name, iosID, pkg, fp string) store.App {
	t.Helper()
	a, err := e.St.CreateApp(name, "dk_"+name+"-0123456789")
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	if err := e.St.UpdateAppDetails(a.ID, iosID, pkg, fp, ""); err != nil {
		t.Fatalf("UpdateAppDetails: %v", err)
	}
	a, _ = e.St.GetApp(a.ID)
	return a
}

// infraDesktopTap: desktop browser tap through the proxy with the given forwarded headers (records a click, 302s to fallback).
func infraDesktopTap(t *testing.T, e *env, key string, hdr map[string]string) resp {
	t.Helper()
	h := map[string]string{"User-Agent": uaDesktopChrome}
	for k, v := range hdr {
		h[k] = v
	}
	return do(t, "GET", e.Pub.URL+"/"+key, "", h)
}

// infraClickIPs: IPs on the app's recorded clicks.
func infraClickIPs(t *testing.T, e *env, appID string) []string {
	t.Helper()
	cs, err := e.St.ClicksSince(appID, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("ClicksSince: %v", err)
	}
	var ips []string
	for _, c := range cs {
		ips = append(ips, c.Fingerprint.IP)
	}
	return ips
}

// infraOneClickIP: the single click's IP.
func infraOneClickIP(t *testing.T, e *env, appID string) string {
	t.Helper()
	ips := infraClickIPs(t, e, appID)
	must(t, len(ips) == 1, "clicks = %v, want exactly 1", ips)
	return ips[0]
}

// infraServe: public listener wired like cmd/detur on an existing store.
func infraServe(t *testing.T, st *store.Store) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	api.RegisterSDK(mux, st, 24)
	pipeline.Register(mux, st, 24)
	pipeline.RegisterWellKnown(mux, st, []string{linkDomain, "localhost"})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// infraEnvVars: blank every detur env var, then apply kv (t.Setenv restores).
func infraEnvVars(t *testing.T, kv map[string]string) {
	t.Helper()
	for _, k := range []string{"DOMAIN", "DB_PATH", "RETENTION_HOURS", "CLICK_ID_DAYS", "TRUST_PROXY", "TRUSTED_PROXIES",
		"LOGOUT_URL", "ADMIN_PASSWORD", "ADMIN_SESSION_HOURS", "PORTAL_HOSTS"} {
		t.Setenv(k, "")
	}
	for k, v := range kv {
		t.Setenv(k, v)
	}
}

// ---------- Apple / Google verification files ----------

// Real world: Apple's CDN (swcd) fetches https://lnk.example/.well-known/apple-app-site-association for a single-app deploy.
// Expect: 200, application/json, no redirect, applinks.details with appIDs + components claiming every path, apps = [].
func TestS_INFRA_01_AASAServedToAppleCDN(t *testing.T) {
	e := newEnv(t)
	e.app()
	r := infraWK(t, e, "GET", "/.well-known/apple-app-site-association", linkDomain)
	must(t, strings.HasPrefix(r.H.Get("Content-Type"), "application/json"), "Content-Type = %q", r.H.Get("Content-Type"))
	a := infraAASAOf(t, r)
	must(t, a.AppLinks.Apps != nil && len(a.AppLinks.Apps) == 0, "apps must be [] (legacy key), got %v", a.AppLinks.Apps)
	must(t, len(a.AppLinks.Details) == 1, "details = %+v", a.AppLinks.Details)
	d := a.AppLinks.Details[0]
	must(t, len(d.AppIDs) == 1 && d.AppIDs[0] == "ABCDE12345.com.example.app", "appIDs = %v", d.AppIDs)
	must(t, d.AppID == "ABCDE12345.com.example.app", "appID (iOS <=12) = %q", d.AppID)
	must(t, len(d.Components) == 1 && d.Components[0]["/"] == "/*", "components = %v", d.Components)
}

// Real world: CDNs, uptime monitors and some validators probe the AASA with HEAD first.
// Expect: 200 with application/json, same as GET.
func TestS_INFRA_02_AASAHeadRequest(t *testing.T) {
	e := newEnv(t)
	e.app()
	r := infraWK(t, e, "HEAD", "/.well-known/apple-app-site-association", linkDomain)
	must(t, r.Code == 200, "HEAD AASA = %d", r.Code)
	must(t, strings.HasPrefix(r.H.Get("Content-Type"), "application/json"), "Content-Type = %q", r.H.Get("Content-Type"))
}

// Real world: a proxy forwards Host with an explicit port and mixed case ("LNK.Example:443").
// Expect: hostnames are case-insensitive and the port is ignored; AASA and assetlinks still served.
func TestS_INFRA_03_WellKnownHostPortAndCase(t *testing.T) {
	e := newEnv(t)
	e.app()
	for _, p := range []string{"/.well-known/apple-app-site-association", "/.well-known/assetlinks.json"} {
		r := infraWK(t, e, "GET", p, "LNK.Example:443")
		must(t, r.Code == 200, "%s with Host LNK.Example:443 = %d", p, r.Code)
	}
}

// Real world: a client or resolver uses the fully-qualified form "lnk.example." (trailing dot) in Host.
// Expect: same DNS name as lnk.example, so the association file is served.
func TestS_INFRA_04_WellKnownHostTrailingDot(t *testing.T) {
	e := newEnv(t)
	e.app()
	r := infraWK(t, e, "GET", "/.well-known/apple-app-site-association", linkDomain+".")
	must(t, r.Code == 200, "AASA with Host %q = %d", linkDomain+".", r.Code)
}

// Real world: someone points their own domain at this instance (or scans by IP) and asks for its association files.
// Expect: 404 for both files: one instance never vouches for another operator's domain.
func TestS_INFRA_05_WellKnownUnknownHost404(t *testing.T) {
	e := newEnv(t)
	e.app()
	for _, p := range []string{"/.well-known/apple-app-site-association", "/.well-known/assetlinks.json"} {
		r := infraWK(t, e, "GET", p, "evil.example")
		must(t, r.Code == 404, "%s for evil.example = %d %q", p, r.Code, r.Body)
		r = infraWK(t, e, "GET", p, "lnk.example.evil.example")
		must(t, r.Code == 404, "%s for suffix host = %d", p, r.Code)
	}
}

// Real world: Android-only app, no iOS app ID yet; Apple still fetches the AASA.
// Expect: valid AASA JSON with details as an empty array (never null, which breaks parsers).
func TestS_INFRA_06_AASANoIOSApp(t *testing.T) {
	e := newEnv(t)
	infraApp(t, e, "droid", "", "com.example.droid", infraFP1)
	r := infraWK(t, e, "GET", "/.well-known/apple-app-site-association", linkDomain)
	must(t, strings.Contains(r.Body, `"details":[]`), "details must be [] not null: %s", r.Body)
	a := infraAASAOf(t, r)
	must(t, len(a.AppLinks.Details) == 0, "details = %+v", a.AppLinks.Details)
}

// Real world: two iOS apps share one link domain; iOS opens the first matching details entry.
// Expect: each entry claims only its own link keys (no "*"), so app B's links don't open app A.
func TestS_INFRA_07_AASAMultiAppPartitionsPaths(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	b := infraApp(t, e, "second", "ZZZZZ99999.com.example.second", "", "")
	e.link(a, "promo-a", "/a")
	e.link(b, "promo-b", "/b")
	r := infraWK(t, e, "GET", "/.well-known/apple-app-site-association", linkDomain)
	aasa := infraAASAOf(t, r)
	must(t, len(aasa.AppLinks.Details) == 2, "details = %s", r.Body)
	claims := map[string][]string{}
	for _, d := range aasa.AppLinks.Details {
		must(t, len(d.AppIDs) == 1, "appIDs = %v", d.AppIDs)
		for _, c := range d.Components {
			must(t, c["/"] != "/*" && c["/"] != "*", "multi-app entry %s claims wildcard: %s", d.AppIDs[0], r.Body)
			claims[d.AppIDs[0]] = append(claims[d.AppIDs[0]], c["/"])
		}
	}
	must(t, fmt.Sprint(claims["ABCDE12345.com.example.app"]) == "[/promo-a]", "app A claims %v", claims)
	must(t, fmt.Sprint(claims["ZZZZZ99999.com.example.second"]) == "[/promo-b]", "app B claims %v", claims)
}

// Real world: operator copies the Team ID + bundle ID from the Apple Developer portal with a trailing space/newline and saves it in the portal.
// Expect: the AASA appID is exactly TEAMID.bundle (trimmed); Apple matches it byte for byte.
func TestS_INFRA_08_AASAPortalTrimsPastedAppID(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	body := `{"iosAppId":"  ABCDE12345.com.example.app\n","androidPackage":" com.example.app ","androidCertFingerprint":"` + infraFP1 + `\n","iosScheme":""}`
	r := e.portal("PATCH", "/api/apps/"+a.ID, body, nil)
	must(t, r.Code == 200, "PATCH app = %d %s", r.Code, r.Body)
	aasa := infraAASAOf(t, infraWK(t, e, "GET", "/.well-known/apple-app-site-association", linkDomain))
	must(t, len(aasa.AppLinks.Details) == 1, "details = %+v", aasa.AppLinks.Details)
	must(t, aasa.AppLinks.Details[0].AppIDs[0] == "ABCDE12345.com.example.app", "appID = %q", aasa.AppLinks.Details[0].AppIDs[0])
	var links []infraAssetLink
	infraWK(t, e, "GET", "/.well-known/assetlinks.json", linkDomain).JSON(t, &links)
	must(t, len(links) == 1 && links[0].Target.PackageName == "com.example.app", "assetlinks package = %+v", links)
}

// Real world: Google's Digital Asset Links verifier fetches https://lnk.example/.well-known/assetlinks.json during App Links verification.
// Expect: 200, application/json, top-level array with handle_all_urls relation, android_app namespace, package + fingerprint.
func TestS_INFRA_09_AssetLinksServedToGoogle(t *testing.T) {
	e := newEnv(t)
	e.app()
	r := infraWK(t, e, "GET", "/.well-known/assetlinks.json", linkDomain)
	must(t, r.Code == 200, "assetlinks = %d", r.Code)
	must(t, strings.HasPrefix(r.H.Get("Content-Type"), "application/json"), "Content-Type = %q", r.H.Get("Content-Type"))
	must(t, strings.HasPrefix(strings.TrimSpace(r.Body), "["), "assetlinks must be a JSON array: %s", r.Body)
	var links []infraAssetLink
	r.JSON(t, &links)
	must(t, len(links) == 1, "entries = %s", r.Body)
	l := links[0]
	must(t, len(l.Relation) == 1 && l.Relation[0] == "delegate_permission/common.handle_all_urls", "relation = %v", l.Relation)
	must(t, l.Target.Namespace == "android_app" && l.Target.PackageName == "com.example.app", "target = %+v", l.Target)
	must(t, len(l.Target.Fingerprints) == 1 && l.Target.Fingerprints[0] == infraFP1, "fingerprints = %v", l.Target.Fingerprints)
}

// Real world: app on Play App Signing: operator lists the upload key and the Play signing key, comma-separated with spaces.
// Expect: both fingerprints in sha256_cert_fingerprints, trimmed, so debug and Play builds both verify.
func TestS_INFRA_10_AssetLinksMultipleFingerprints(t *testing.T) {
	e := newEnv(t)
	infraApp(t, e, "play", "", "com.example.play", infraFP1+" , "+infraFP2+" ,")
	var links []infraAssetLink
	infraWK(t, e, "GET", "/.well-known/assetlinks.json", linkDomain).JSON(t, &links)
	must(t, len(links) == 1, "entries = %+v", links)
	fps := links[0].Target.Fingerprints
	must(t, len(fps) == 2 && fps[0] == infraFP1 && fps[1] == infraFP2, "fingerprints = %q", fps)
}

// Real world: one instance hosts an Android app, a second Android app, and an iOS-only app.
// Expect: assetlinks lists both Android apps, skips the iOS-only one; AASA lists only the iOS-capable apps.
func TestS_INFRA_11_WellKnownMixedApps(t *testing.T) {
	e := newEnv(t)
	e.app() // iOS + Android
	infraApp(t, e, "droid2", "", "com.example.droid2", infraFP2)
	ios := infraApp(t, e, "iosonly", "QQQQQ11111.com.example.iosonly", "", "")
	e.link(ios, "ios-only", "/x")
	var links []infraAssetLink
	infraWK(t, e, "GET", "/.well-known/assetlinks.json", linkDomain).JSON(t, &links)
	pk := map[string]bool{}
	for _, l := range links {
		pk[l.Target.PackageName] = true
	}
	must(t, len(links) == 2 && pk["com.example.app"] && pk["com.example.droid2"], "assetlinks packages = %v", pk)
	aasa := infraAASAOf(t, infraWK(t, e, "GET", "/.well-known/apple-app-site-association", linkDomain))
	for _, d := range aasa.AppLinks.Details {
		must(t, !strings.Contains(d.AppIDs[0], "droid2"), "android-only app in AASA: %+v", d)
	}
}

// Real world: developer runs `docker compose up` with DOMAIN=localhost and opens http://localhost:8080/.well-known/... to check setup.
// Expect: localhost (with port) is always served.
func TestS_INFRA_12_WellKnownLocalhostDev(t *testing.T) {
	e := newEnv(t)
	e.app()
	r := infraWK(t, e, "GET", "/.well-known/assetlinks.json", "localhost:8080")
	must(t, r.Code == 200, "assetlinks on localhost:8080 = %d", r.Code)
	r = infraWK(t, e, "GET", "/.well-known/apple-app-site-association", "localhost:8080")
	must(t, r.Code == 200, "AASA on localhost:8080 = %d", r.Code)
}

// ---------- Proxies and client IP ----------

// Real world: Caddy on the same host (loopback peer) appends the visitor's IP to X-Forwarded-For; TRUST_PROXY=1.
// Expect: the recorded click carries the visitor's public IP, not 127.0.0.1.
func TestS_INFRA_13_ProxyTrustedLoopbackXFF(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "ip1", "/p")
	r := infraDesktopTap(t, e, "ip1", map[string]string{"X-Forwarded-For": clientIP})
	must(t, r.Code == 302, "tap = %d", r.Code)
	ip := infraOneClickIP(t, e, a.ID)
	must(t, ip == clientIP, "click IP = %q, want %q", ip, clientIP)
}

// Real world: detur exposed directly on :8080 (TRUST_PROXY=0); an attacker sends a forged X-Forwarded-For to game IP matching.
// Expect: forwarded headers ignored; click IP = socket peer.
func TestS_INFRA_14_ProxyOffIgnoresSpoofedXFF(t *testing.T) {
	e := newEnvWith(t, envOpts{TrustProxy: false})
	a := e.app()
	e.link(a, "ip2", "/p")
	infraDesktopTap(t, e, "ip2", map[string]string{"X-Forwarded-For": "198.51.100.66", "X-Real-IP": "198.51.100.67"})
	ip := infraOneClickIP(t, e, a.ID)
	must(t, ip == "127.0.0.1", "click IP = %q, want socket peer 127.0.0.1", ip)
}

// Real world: Cloudflare -> Zeabur ingress -> detur. TRUSTED_PROXIES lists the ingress range and Cloudflare's ranges.
// Client-forged leftmost entry, then the real visitor, then the Cloudflare edge.
// Expect: click IP = the real visitor (rightmost untrusted hop), never the forged entry or the CF edge.
func TestS_INFRA_15_ProxyCloudflareIngressChain(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "ip3", "/p")
	must(t, httpx.SetTrustedProxies([]string{"127.0.0.0/8", "10.0.0.0/8", "173.245.48.0/20", "2400:cb00::/32"}) == nil, "SetTrustedProxies")
	infraDesktopTap(t, e, "ip3", map[string]string{"X-Forwarded-For": "6.6.6.6, 198.51.100.7, 173.245.48.10"})
	ip := infraOneClickIP(t, e, a.ID)
	must(t, ip == "198.51.100.7", "click IP = %q, want 198.51.100.7", ip)
}

// Real world: TRUSTED_PROXIES is set to the ingress range only; a request arrives from a peer outside it carrying XFF.
// Expect: headers ignored (click IP = peer) and the ignore is counted for the Settings Health warning.
func TestS_INFRA_16_ProxyUntrustedPeerSpoof(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "ip4", "/p")
	must(t, httpx.SetTrustedProxies([]string{"10.0.0.0/8"}) == nil, "SetTrustedProxies")
	infraDesktopTap(t, e, "ip4", map[string]string{"X-Forwarded-For": "198.51.100.9"})
	ip := infraOneClickIP(t, e, a.ID)
	must(t, ip == "127.0.0.1", "click IP = %q, want peer 127.0.0.1", ip)
	must(t, httpx.XFFIgnored() >= 1 && httpx.LastXFFPeer() == "127.0.0.1", "XFFIgnored=%d LastXFFPeer=%q", httpx.XFFIgnored(), httpx.LastXFFPeer())
}

// Real world: nginx configured with `proxy_set_header X-Real-IP $remote_addr;` and no X-Forwarded-For.
// Expect: click IP = X-Real-IP.
func TestS_INFRA_17_ProxyNginxXRealIP(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "ip5", "/p")
	infraDesktopTap(t, e, "ip5", map[string]string{"X-Real-IP": "198.51.100.23"})
	ip := infraOneClickIP(t, e, a.ID)
	must(t, ip == "198.51.100.23", "click IP = %q", ip)
}

// Real world: visitor on an IPv6 mobile network behind Caddy.
// Expect: click IP = the IPv6 address.
func TestS_INFRA_18_ProxyIPv6Client(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "ip6", "/p")
	infraDesktopTap(t, e, "ip6", map[string]string{"X-Forwarded-For": "2001:db8:85a3::8a2e:370:7334"})
	ip := infraOneClickIP(t, e, a.ID)
	must(t, ip == "2001:db8:85a3::8a2e:370:7334", "click IP = %q", ip)
}

// Real world: Azure Application Gateway / App Service append the client as "ip:port" in X-Forwarded-For.
// Expect: the port is stripped; click IP = bare IP (else tap and SDK call never share an IP).
func TestS_INFRA_19_ProxyXFFWithPort(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "ip7", "/p")
	infraDesktopTap(t, e, "ip7", map[string]string{"X-Forwarded-For": "203.0.113.7:51234"})
	ip := infraOneClickIP(t, e, a.ID)
	must(t, ip == "203.0.113.7", "click IP = %q, want 203.0.113.7", ip)
}

// Real world: iPhone on IPv6 taps a link in Safari, installs, opens the app; SDK calls match-link from the same IPv6.
// Expect: deferred link matched (200 with the link).
func TestS_INFRA_20_ProxyIPv6EndToEndMatch(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "v6", "/product/6")
	v6 := map[string]string{"X-Forwarded-For": "2001:db8::abcd"}
	must(t, e.tapH("/v6", uaIOSSafari, v6).Code == 200, "hop 1 not interstitial")
	r := e.tapH("/v6?_dt=1&screen=393x852%403&tz=Asia%2FHo_Chi_Minh", uaIOSSafari, v6)
	must(t, r.Code == 302 || r.Code == 200, "hop 2 = %d", r.Code)
	m := e.sdkH(a, "/api/link/match-link", iosFingerprint(""), v6)
	must(t, m.Code == 200 && strings.Contains(m.Body, "product/6"), "match-link = %d %s", m.Code, m.Body)
}

// ---------- Health ----------

// Real world: Docker HEALTHCHECK (wget GET) and uptime monitors (HEAD) probe /health on a working instance.
// Expect: 200 "ok" for GET, 200 for HEAD.
func TestS_INFRA_21_HealthOK(t *testing.T) {
	e := newEnv(t)
	r := do(t, "GET", e.Pub.URL+"/health", "", nil)
	must(t, r.Code == 200 && strings.TrimSpace(r.Body) == "ok", "GET /health = %d %q", r.Code, r.Body)
	r = do(t, "HEAD", e.Pub.URL+"/health", "", nil)
	must(t, r.Code == 200, "HEAD /health = %d", r.Code)
}

// Real world: the SQLite handle is gone (volume unmounted, DB closed); orchestrator must restart the container.
// Expect: /health = 503.
func TestS_INFRA_22_HealthDBDown(t *testing.T) {
	e := newEnv(t)
	e.St.Close()
	r := do(t, "GET", e.Pub.URL+"/health", "", nil)
	must(t, r.Code == http.StatusServiceUnavailable, "/health with closed DB = %d", r.Code)
}

// ---------- Robustness ----------

// Real world: an influencer posts a link; 50 different people tap it within the same second.
// Expect: every tap redirects, every click is recorded (no SQLITE_BUSY drops).
func TestS_INFRA_23_ConcurrentClicksOneLink(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "viral", "/p")
	const n = 50
	codes := make([]int, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			codes[i] = infraDesktopTap(t, e, "viral", map[string]string{"X-Forwarded-For": fmt.Sprintf("198.51.100.%d", i+1)}).Code
		}(i)
	}
	wg.Wait()
	for i, c := range codes {
		must(t, c == 302, "tap %d = %d", i, c)
	}
	got := len(infraClickIPs(t, e, a.ID))
	must(t, got == n, "recorded clicks = %d, want %d", got, n)
}

// Real world: a user double-taps and the browser prefetches: 20 identical requests from one device hit at once.
// Expect: all redirect, exactly one click (dedup holds under concurrency).
func TestS_INFRA_24_ConcurrentSameDeviceDedup(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "dbl", "/p")
	var wg sync.WaitGroup
	codes := make([]int, 20)
	for i := range codes {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			codes[i] = infraDesktopTap(t, e, "dbl", map[string]string{"X-Forwarded-For": clientIP}).Code
		}(i)
	}
	wg.Wait()
	for i, c := range codes {
		must(t, c == 302, "tap %d = %d", i, c)
	}
	got := len(infraClickIPs(t, e, a.ID))
	must(t, got == 1, "clicks = %d, want 1", got)
}

// Real world: a release goes out; 30 new installs open the app at once and the SDK calls match-link concurrently.
// Expect: no 5xx; every device gets an answer and an organic install row.
func TestS_INFRA_25_ConcurrentMatchLink(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	const n = 30
	codes := make([]int, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			body := strings.Replace(androidFingerprint(""), `"model":"Pixel 7"`, fmt.Sprintf(`"model":"Pixel %d"`, 100+i), 1)
			codes[i] = e.sdkH(a, "/api/link/match-link", body, map[string]string{"X-Forwarded-For": fmt.Sprintf("198.51.100.%d", i+1)}).Code
		}(i)
	}
	wg.Wait()
	for i, c := range codes {
		must(t, c == 200 || c == 404, "match-link %d = %d", i, c)
	}
	organic, _, err := e.St.CountInstalls(a.ID)
	must(t, err == nil && organic == n, "organic installs = %d (%v), want %d", organic, err, n)
}

// Real world: `docker compose pull && up -d` restarts the container on the same /data volume.
// Expect: store.Open on the same path keeps apps, links and clicks; links still resolve.
func TestS_INFRA_26_RestartKeepsData(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "keep", "/p")
	infraDesktopTap(t, e, "keep", map[string]string{"X-Forwarded-For": clientIP})
	e.Pub.Close()
	must(t, e.St.Close() == nil, "close")
	st, err := store.Open(e.DBPath)
	must(t, err == nil, "reopen: %v", err)
	defer st.Close()
	n, _ := st.CountClicks(a.ID)
	must(t, n == 1, "clicks after restart = %d", n)
	srv := infraServe(t, st)
	r := do(t, "GET", srv.URL+"/keep", "", map[string]string{"User-Agent": uaDesktopChrome})
	must(t, r.Code == 302, "link after restart = %d", r.Code)
	r = do(t, "GET", srv.URL+"/.well-known/apple-app-site-association", "", map[string]string{"Host": linkDomain})
	must(t, r.Code == 200 && strings.Contains(r.Body, "ABCDE12345.com.example.app"), "AASA after restart = %d %s", r.Code, r.Body)
}

// Real world: operator upgrades from v0.1 (old tables missing later columns) to the current image on the same DB.
// Expect: store.Open migrates in place; old app's well-known files, old link and new click recording all work.
func TestS_INFRA_27_UpgradeFromV01Database(t *testing.T) {
	path := filepath.Join(t.TempDir(), "detur.db")
	db, err := sql.Open("sqlite", path)
	must(t, err == nil, "sql.Open: %v", err)
	_, err = db.Exec(`
CREATE TABLE apps (id TEXT PRIMARY KEY, name TEXT NOT NULL, api_key_hash TEXT NOT NULL,
  ios_app_id TEXT, android_package TEXT, android_cert_fingerprint TEXT,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')));
CREATE TABLE links (id TEXT PRIMARY KEY, app_id TEXT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
  key TEXT NOT NULL, url TEXT NOT NULL, ios TEXT, android TEXT, fallback_url TEXT,
  threshold INTEGER NOT NULL DEFAULT 850, window_minutes INTEGER NOT NULL DEFAULT 15,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')), UNIQUE (app_id, key));
CREATE TABLE clicks (id TEXT PRIMARY KEY, app_id TEXT NOT NULL, link_id TEXT NOT NULL, ip TEXT,
  device TEXT, locale TEXT, timezone TEXT, screen TEXT, user_agent TEXT, pasted_link TEXT,
  destination TEXT NOT NULL, click_id TEXT, is_bot INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')), expires_at TEXT NOT NULL);
CREATE TABLE installs (id TEXT PRIMARY KEY, app_id TEXT NOT NULL, device_hash TEXT NOT NULL, click_id TEXT, attribution TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')), UNIQUE (app_id, device_hash, click_id));
INSERT INTO apps (id, name, api_key_hash, ios_app_id, android_package, android_cert_fingerprint)
  VALUES ('old-app', 'Old', 'h', 'ABCDE12345.com.example.old', 'com.example.old', '` + infraFP1 + `');
INSERT INTO links (id, app_id, key, url, fallback_url) VALUES ('old-link', 'old-app', 'oldpromo', '/promo', 'https://example.com/old');`)
	must(t, err == nil, "legacy schema: %v", err)
	must(t, db.Close() == nil, "close legacy")

	httpx.TrustProxy = true
	t.Cleanup(func() { httpx.TrustProxy = false })
	st, err := store.Open(path)
	must(t, err == nil, "Open legacy DB: %v", err)
	defer st.Close()
	srv := infraServe(t, st)
	r := do(t, "GET", srv.URL+"/.well-known/apple-app-site-association", "", map[string]string{"Host": linkDomain})
	must(t, r.Code == 200 && strings.Contains(r.Body, "ABCDE12345.com.example.old"), "AASA = %d %s", r.Code, r.Body)
	r = do(t, "GET", srv.URL+"/oldpromo", "", map[string]string{"User-Agent": uaDesktopChrome, "X-Forwarded-For": clientIP})
	must(t, r.Code == 302 && r.Loc() == "https://example.com/old", "old link = %d %q", r.Code, r.Loc())
	n, err := st.CountClicks("old-app")
	must(t, err == nil && n == 1, "clicks on upgraded DB = %d (%v)", n, err)
}

// Real world: DB_PATH points at a file that isn't SQLite (wrong mount, stray file) or into a missing directory.
// Expect: store.Open returns an error (container exits with a clear log), never panics or silently starts.
func TestS_INFRA_28_OpenBadDBPath(t *testing.T) {
	dir := t.TempDir()
	junk := filepath.Join(dir, "detur.db")
	must(t, os.WriteFile(junk, []byte(strings.Repeat("not a sqlite database\n", 200)), 0o644) == nil, "write junk")
	if st, err := store.Open(junk); err == nil {
		st.Close()
		t.Fatalf("store.Open on a non-SQLite file succeeded")
	}
	if st, err := store.Open(filepath.Join(dir, "missing", "sub", "detur.db")); err == nil {
		st.Close()
		t.Fatalf("store.Open in a missing directory succeeded")
	}
}

// Real world: RETENTION_HOURS=24; the hourly purge runs a day later.
// Expect: the click's fingerprint (IP, UA) is scrubbed, while the app and link are untouched and still redirect.
func TestS_INFRA_29_RetentionPurgeScrubsClicks(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "ret", "/p")
	srv := infraServe(t, e.St) // RETENTION_HOURS=24
	tap := func() resp {
		return do(t, "GET", srv.URL+"/ret", "", map[string]string{"User-Agent": uaDesktopChrome, "X-Forwarded-For": clientIP})
	}
	must(t, tap().Code == 302, "tap")
	_, err := e.St.PurgeExpired(time.Now(), 24)
	must(t, err == nil, "purge now: %v", err)
	ips := infraClickIPs(t, e, a.ID)
	must(t, len(ips) == 1 && ips[0] == clientIP, "live click scrubbed early: %v", ips)
	_, err = e.St.PurgeExpired(time.Now().Add(25*time.Hour), 24)
	must(t, err == nil, "purge: %v", err)
	cs, err := e.St.ClicksSince(a.ID, time.Now().Add(-time.Hour))
	must(t, err == nil, "ClicksSince: %v", err)
	for _, c := range cs {
		must(t, c.Fingerprint.IP == "" && c.Fingerprint.UserAgent == "", "fingerprint kept past retention: %+v", c.Fingerprint)
	}
	must(t, tap().Code == 302, "link after purge")
}

// Real world: Android user taps, installs 2 days later; Play Install Referrer brings click_id back after the 24h purge scrubbed the fingerprint (CLICK_ID_DAYS=30).
// Expect: match-link by clickId still returns the link.
func TestS_INFRA_30_PurgeKeepsLateClickID(t *testing.T) {
	e := newEnv(t)
	e.St.ClickIDHours = 30 * 24
	a := e.app()
	e.link(a, "late", "/late/1")
	srv := infraServe(t, e.St) // RETENTION_HOURS=24
	h := map[string]string{"User-Agent": uaAndroidChrome, "X-Forwarded-For": clientIP}
	must(t, do(t, "GET", srv.URL+"/late", "", h).Code == 200, "hop 1")
	do(t, "GET", srv.URL+"/late?_dt=1&screen=393x852%403&tz=Asia%2FHo_Chi_Minh", "", h)
	cs, err := e.St.ClicksSince(a.ID, time.Now().Add(-time.Hour))
	must(t, err == nil && len(cs) == 1 && cs[0].ClickID != "", "clicks = %+v (%v)", cs, err)
	_, err = e.St.PurgeExpired(time.Now().Add(48*time.Hour), 24)
	must(t, err == nil, "purge: %v", err)
	r := do(t, "POST", srv.URL+"/api/link/match-link", `{"clickId":"`+cs[0].ClickID+`"}`, map[string]string{
		"Authorization": "Bearer " + apiKey, "X-App-ID": a.ID, "X-SDK": sdkHeader, "Content-Type": "application/json", "X-Forwarded-For": clientIP})
	must(t, r.Code == 200 && strings.Contains(r.Body, "late/1"), "match by late clickId = %d %s", r.Code, r.Body)
}

// Real world: a hostile client POSTs a 2 MB body to match-link to pin the single SQLite writer.
// Expect: rejected with 413 (or 400), never 5xx, and the next normal SDK call still works.
func TestS_INFRA_31_OversizedSDKBody(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	big := `{"model":"` + strings.Repeat("a", 2<<20) + `"}`
	r := e.sdk(a, "/api/link/match-link", big)
	must(t, r.Code == http.StatusRequestEntityTooLarge || r.Code == http.StatusBadRequest, "2MB body = %d", r.Code)
	r = e.sdk(a, "/api/link/match-link", androidFingerprint(""))
	must(t, r.Code == 200 || r.Code == 404, "normal call after oversized = %d", r.Code)
}

// Real world: scanners hit very long random paths; ad networks append huge click IDs (fbclid/gclid) to a real link.
// Expect: long unknown key -> 404 (not 5xx); real link with a 6 KB query still redirects and records the click.
func TestS_INFRA_32_VeryLongURLs(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "ad", "/p")
	r := infraDesktopTap(t, e, strings.Repeat("k", 4000), nil)
	must(t, r.Code == 404, "4000-char key = %d", r.Code)
	r = infraDesktopTap(t, e, "ad?fbclid="+strings.Repeat("Iw", 3000)+"&utm_source=fb", map[string]string{"X-Forwarded-For": clientIP})
	must(t, r.Code == 302, "long query tap = %d", r.Code)
	must(t, len(infraClickIPs(t, e, a.ID)) == 1, "click not recorded for long query")
}

// Real world: a marketer tries an emoji / Vietnamese key ("🎉sale", "khuyến-mãi"); people also open percent-encoded emoji paths.
// Expect: portal rejects non path-safe keys with 400; public listener answers 404 for such paths, never 5xx.
func TestS_INFRA_33_UnicodeKeys(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	for _, k := range []string{"🎉sale", "khuyến-mãi"} {
		r := e.portal("POST", "/api/apps/"+a.ID+"/links", `{"key":"`+k+`","url":"/p"}`, nil)
		must(t, r.Code == 400, "portal key %q = %d %s", k, r.Code, r.Body)
	}
	for _, p := range []string{"/%F0%9F%8E%89sale", "/khuy%E1%BA%BFn-m%C3%A3i", "/%FF%FE"} {
		r := infraDesktopTap(t, e, strings.TrimPrefix(p, "/"), nil)
		must(t, r.Code == 404, "GET %s = %d", p, r.Code)
	}
}

// Real world: an email client or QR generator percent-encodes plain characters in the path ("/ab%63" for "/abc").
// Expect: RFC 3986 equivalent path -> same link redirects.
func TestS_INFRA_34_PercentEncodedKey(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "abc", "/p")
	r := infraDesktopTap(t, e, "ab%63", nil)
	must(t, r.Code == 302, "/ab%%63 = %d", r.Code)
}

// Real world: a form-submitting crawler POSTs to a short link; a misconfigured client PUTs to an SDK route.
// Expect: 405 with Allow header; no click recorded.
func TestS_INFRA_35_MethodNotAllowed(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "m", "/p")
	r := do(t, "POST", e.Pub.URL+"/m", "x=1", map[string]string{"User-Agent": uaDesktopChrome, "X-Forwarded-For": clientIP})
	must(t, r.Code == 405 && strings.Contains(r.H.Get("Allow"), "GET"), "POST /m = %d Allow=%q", r.Code, r.H.Get("Allow"))
	r = do(t, "PUT", e.Pub.URL+"/api/link/match-link", "{}", nil)
	must(t, r.Code == 405, "PUT match-link = %d", r.Code)
	must(t, len(infraClickIPs(t, e, a.ID)) == 0, "POST recorded a click")
}

// Real world: :8080 is public; a scanner probes /api/apps, /api/apps/x/links, /api/admin/session and GETs SDK routes.
// Expect: public listener never exposes portal API (404) and SDK routes are not read as short-link keys (405 for GET).
func TestS_INFRA_36_PublicListenerNoPortalAPI(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "x", "/p")
	for _, p := range []string{"/api/apps", "/api/apps/" + a.ID + "/links", "/api/admin/session", "/api/config", "/api/nope/deeper"} {
		r := do(t, "GET", e.Pub.URL+p, "", nil)
		must(t, r.Code == 404, "public GET %s = %d %s", p, r.Code, r.Body)
		must(t, !strings.Contains(r.Body, a.ID), "public %s leaks app data", p)
	}
	r := do(t, "GET", e.Pub.URL+"/api/link/match-link", "", nil)
	must(t, r.Code == 405, "GET match-link = %d", r.Code)
}

// Real world: operator tries to create a link keyed "health" or "API" (would be shadowed by /health and /api routes).
// Expect: portal refuses reserved keys, case-insensitively.
func TestS_INFRA_37_ReservedKeysRefused(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	for _, k := range []string{"health", "Health", "API", "_root"} {
		r := e.portal("POST", "/api/apps/"+a.ID+"/links", `{"key":"`+k+`","url":"/p"}`, nil)
		must(t, r.Code == 400, "key %q = %d %s", k, r.Code, r.Body)
	}
}

// Real world: an attacker on the portal port requests encoded traversal paths for /etc/passwd or the SQLite file.
// Expect: never serves files outside the static dir.
func TestS_INFRA_38_PortalStaticTraversal(t *testing.T) {
	e := newEnv(t)
	for _, p := range []string{"/../../../../etc/passwd", "/%2e%2e/%2e%2e/%2e%2e/etc/passwd", "/..%2f..%2f..%2fetc%2fpasswd", "/%2e%2e%2f" + filepath.Base(e.DBPath)} {
		r := do(t, "GET", e.Portal.URL+p, "", nil)
		must(t, !strings.Contains(r.Body, "root:") && !strings.Contains(r.Body, "SQLite format"), "GET %s served a file outside static dir (%d)", p, r.Code)
	}
}

// Real world: email security scanners (Outlook Safe Links, Proofpoint) send HEAD with a browser UA to every link in a message.
// Expect: HEAD gets the redirect but records no click (no fake clicks in analytics or matching).
func TestS_INFRA_39_HeadDoesNotRecordClick(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "h", "/p")
	r := do(t, "HEAD", e.Pub.URL+"/h", "", map[string]string{"User-Agent": uaDesktopChrome, "X-Forwarded-For": clientIP})
	must(t, r.Code == 302 || r.Code == 200, "HEAD /h = %d", r.Code)
	n, _ := e.St.CountClicks(a.ID)
	must(t, n == 0, "HEAD recorded %d click(s)", n)
}

// ---------- Config ----------

// Real world: `docker run` with no env except what the Dockerfile sets.
// Expect: documented defaults (DOMAIN=localhost, RETENTION_HOURS=24, CLICK_ID_DAYS=30, TRUST_PROXY off, ADMIN_SESSION_HOURS=12).
func TestS_INFRA_40_ConfigDefaults(t *testing.T) {
	infraEnvVars(t, nil)
	c, err := config.Load()
	must(t, err == nil, "Load: %v", err)
	must(t, c.Domain == "localhost" && c.RetentionHours == 24 && c.ClickIDDays == 30 && !c.TrustProxy &&
		c.AdminSessionHours == 12 && len(c.TrustedProxies) == 0 && len(c.PortalHosts) == 0, "defaults = %+v", c)
	must(t, fmt.Sprint(config.DomainSet(c)) == "[localhost]", "DomainSet = %v", config.DomainSet(c))
}

// Real world: typos in numeric env vars (RETENTION_HOURS=24h, =0, =9000, CLICK_ID_DAYS=91, ADMIN_SESSION_HOURS=0).
// Expect: config.Load fails fast for each instead of silently running with a different value.
func TestS_INFRA_41_ConfigInvalidNumbersFailFast(t *testing.T) {
	for _, kv := range [][2]string{{"RETENTION_HOURS", "24h"}, {"RETENTION_HOURS", "0"}, {"RETENTION_HOURS", "9000"},
		{"CLICK_ID_DAYS", "91"}, {"CLICK_ID_DAYS", "0"}, {"ADMIN_SESSION_HOURS", "0"}, {"ADMIN_SESSION_HOURS", "73"}} {
		infraEnvVars(t, map[string]string{kv[0]: kv[1]})
		_, err := config.Load()
		must(t, err != nil, "%s=%s accepted", kv[0], kv[1])
	}
}

// Real world: operator writes TRUST_PROXY=true (docker-compose style boolean) behind Caddy.
// Expect: either honored as on, or rejected at startup; never silently treated as off (every user would share the proxy IP).
func TestS_INFRA_42_ConfigTrustProxyTrue(t *testing.T) {
	for _, v := range []string{"true", "TRUE", "yes"} {
		infraEnvVars(t, map[string]string{"TRUST_PROXY": v})
		c, err := config.Load()
		must(t, err != nil || c.TrustProxy, "TRUST_PROXY=%s silently treated as off", v)
	}
}

// Real world: operator sets DOMAIN=https://lnk.example/ despite the .env comment.
// Expect: normalized to lnk.example or rejected at startup; never silently 404 every AASA/assetlinks fetch.
func TestS_INFRA_43_ConfigDomainWithScheme(t *testing.T) {
	infraEnvVars(t, map[string]string{"DOMAIN": "https://lnk.example/"})
	c, err := config.Load()
	if err != nil {
		return
	}
	ds := config.DomainSet(c)
	must(t, len(ds) > 0 && ds[0] == "lnk.example", "DOMAIN=https://lnk.example/ -> DomainSet %v (well-known would 404 on lnk.example)", ds)
}

// Real world: TRUSTED_PROXIES lists Cloudflare v4 + v6 ranges with spaces after commas; a typo elsewhere ("cloudflare").
// Expect: valid list parsed (trimmed, IPv6 ok); invalid entry fails startup.
func TestS_INFRA_44_ConfigTrustedProxiesList(t *testing.T) {
	infraEnvVars(t, map[string]string{"TRUST_PROXY": "1", "TRUSTED_PROXIES": "173.245.48.0/20, 103.21.244.0/22 ,2400:cb00::/32,"})
	c, err := config.Load()
	must(t, err == nil && c.TrustProxy, "Load: %v", err)
	must(t, fmt.Sprint(c.TrustedProxies) == "[173.245.48.0/20 103.21.244.0/22 2400:cb00::/32]", "TrustedProxies = %v", c.TrustedProxies)
	infraEnvVars(t, map[string]string{"TRUST_PROXY": "1", "TRUSTED_PROXIES": "10.0.0.0/8,cloudflare"})
	_, err = config.Load()
	must(t, err != nil, "invalid TRUSTED_PROXIES accepted")
}

// Real world: LOGOUT_URL set to a script URL or protocol-relative host (lands in the portal's href).
// Expect: rejected at startup; Cloudflare Access path and full https URL accepted.
func TestS_INFRA_45_ConfigLogoutURL(t *testing.T) {
	for _, v := range []string{"javascript:alert(1)", "//evil.example/x", "ftp://x.example"} {
		infraEnvVars(t, map[string]string{"LOGOUT_URL": v})
		_, err := config.Load()
		must(t, err != nil, "LOGOUT_URL=%q accepted", v)
	}
	for _, v := range []string{"/cdn-cgi/access/logout", "https://auth.example.com/logout"} {
		infraEnvVars(t, map[string]string{"LOGOUT_URL": v})
		_, err := config.Load()
		must(t, err == nil, "LOGOUT_URL=%q rejected: %v", v, err)
	}
}
