package scenarios

// Real-world usage scenarios: the whole server wired like cmd/detur (public mux + portal handler, one SQLite store),
// driven over HTTP the way phones, browsers, link-preview bots, the Detour SDK and the portal drive it.
// One test = one scenario: TestS_<AREA>_<NN>_<Slug>. scripts/scenarios.sh scores pass rate per test.

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"detur.dev/server/internal/api"
	"detur.dev/server/internal/httpx"
	"detur.dev/server/internal/pipeline"
	"detur.dev/server/internal/store"
)

const (
	apiKey     = "dk_scenario-key-0123456789"
	linkDomain = "lnk.example" // DOMAIN; public listener answers well-known for it + localhost
	sdkHeader  = "react-native/2.3.1"
	clientIP   = "203.0.113.7" // public client IP behind a trusted proxy
)

// Real User-Agents seen on short-link taps.
const (
	uaIOSSafari     = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Mobile/15E148 Safari/604.1"
	uaIOSChrome     = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) CriOS/126.0.6478.54 Mobile/15E148 Safari/604.1"
	uaIPadOS        = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Safari/605.1.15" // iPadOS Safari requests desktop site
	uaAndroidChrome = "Mozilla/5.0 (Linux; Android 14; Pixel 7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Mobile Safari/537.36"
	uaSamsung       = "Mozilla/5.0 (Linux; Android 14; SAMSUNG SM-S918B) AppleWebKit/537.36 (KHTML, like Gecko) SamsungBrowser/25.0 Chrome/121.0.0.0 Mobile Safari/537.36"
	uaDesktopChrome = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"
	uaDesktopMac    = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"
	uaFirefox       = "Mozilla/5.0 (X11; Linux x86_64; rv:127.0) Gecko/20100101 Firefox/127.0"

	uaMessengerIOS     = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Mobile/15E148 [FBAN/MessengerForiOS;FBAV/442.0.0.42.110;FBDV/iPhone15,2;FBSN/iOS;FBSV/17.5]"
	uaMessengerAndroid = "Mozilla/5.0 (Linux; Android 14; Pixel 7 Build/UP1A.231005.007; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/125.0.0.0 Mobile Safari/537.36 [FB_IAB/MESSENGER;FBAV/450.0.0.42.109;]"
	uaFacebookIOS      = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Mobile/15E148 [FBAN/FBIOS;FBAV/470.0.0.40.98;FBBV/123;FBDV/iPhone15,2;FBMD/iPhone;FBSN/iOS;FBSV/17.5;FBSS/3;FBCR/;FBID/phone;FBLC/en_US;FBOP/80]"
	uaInstagramIOS     = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Mobile/15E148 Instagram 334.0.4.32.98 (iPhone15,2; iOS 17_5; en_US; en; scale=3.00; 1179x2556; 612345678)"
	uaInstagramAndroid = "Mozilla/5.0 (Linux; Android 14; Pixel 7 Build/UP1A.231005.007; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/125.0.0.0 Mobile Safari/537.36 Instagram 334.0.0.42.95 Android (34/14; 420dpi; 1080x2400; Google/google; Pixel 7; panther; panther; en_US; 612345678)"
	uaTikTokIOS        = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Mobile/15E148 musical_ly_35.1.0 JsSdk/2.0 NetType/WIFI Channel/App Store ByteLocale/en Region/US"
	uaZaloIOS          = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Mobile/15E148 Zalo iOS/24.06.01 ZaloTheme/light ZaloLanguage/vi"
	uaZaloAndroid      = "Mozilla/5.0 (Linux; Android 14; SM-S918B Build/UP1A.231005.007; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/125.0.0.0 Mobile Safari/537.36 Zalo android/24060100 ZaloTheme/light ZaloLanguage/vi"
	uaLineIOS          = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Mobile/15E148 Safari Line/14.9.0"
	uaWeChatIOS        = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Mobile/15E148 MicroMessenger/8.0.49(0x18003130) NetType/WIFI Language/zh_CN"
	uaAndroidWebView   = "Mozilla/5.0 (Linux; Android 14; Pixel 7 Build/UP1A.231005.007; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/125.0.0.0 Mobile Safari/537.36"

	uaFacebookBot = "facebookexternalhit/1.1 (+http://www.facebook.com/externalhit_uatext.php)"
	uaTwitterBot  = "Twitterbot/1.0"
	uaSlackBot    = "Slackbot-LinkExpanding 1.0 (+https://api.slack.com/robots)"
	uaWhatsApp    = "WhatsApp/2.24.12.78 A"
	uaDiscordBot  = "Mozilla/5.0 (compatible; Discordbot/2.0; +https://discordapp.com)"
	uaTelegramBot = "TelegramBot (like TwitterBot)"
	uaGoogleBot   = "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)"
	uaIMessage    = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_11_1) AppleWebKit/601.2.4 (KHTML, like Gecko) Version/9.0.1 Safari/601.2.4 facebookexternalhit/1.1 Facebot Twitterbot/1.0"
	uaCurl        = "curl/8.6.0"
)

// env: one deployment. Pub = public listener (:8080), Portal = portal listener, St = its store.
type env struct {
	t      *testing.T
	Pub    *httptest.Server
	Portal *httptest.Server
	St     *store.Store
	DBPath string
}

type envOpts struct {
	AdminPassword string
	TrustProxy    bool // default deploy: behind a trusted proxy on loopback (httptest peer is 127.0.0.1)
}

// newEnv: TRUST_PROXY=1 (Caddy/nginx in front), no admin password.
func newEnv(t *testing.T) *env { return newEnvWith(t, envOpts{TrustProxy: true}) }

func newEnvWith(t *testing.T, o envOpts) *env {
	t.Helper()
	httpx.TrustProxy = o.TrustProxy
	_ = httpx.SetTrustedProxies(nil)
	t.Cleanup(func() { httpx.TrustProxy = false; _ = httpx.SetTrustedProxies(nil) })
	path := filepath.Join(t.TempDir(), "detur.db")
	st, err := store.Open(path)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		if err := st.Ping(); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("ok"))
	})
	api.RegisterSDK(mux, st, 24*90)
	pipeline.Register(mux, st, 24*90)
	pipeline.RegisterWellKnown(mux, st, []string{linkDomain, "localhost"})
	pub := httptest.NewServer(mux)
	t.Cleanup(pub.Close)

	static := t.TempDir()
	if err := os.WriteFile(filepath.Join(static, "index.html"), []byte("<!doctype html><title>detur portal</title>"), 0o644); err != nil {
		t.Fatal(err)
	}
	portal := httptest.NewServer(api.RegisterPortal(st, static, []string{"127.0.0.1:8081"}, "/logout", o.AdminPassword, 12, "https://"+linkDomain))
	t.Cleanup(portal.Close)
	return &env{t: t, Pub: pub, Portal: portal, St: st, DBPath: path}
}

// app: app with API key, iOS + Android identity set (as after onboarding).
func (e *env) app() store.App {
	e.t.Helper()
	a, err := e.St.CreateApp("Scenario App", apiKey)
	if err != nil {
		e.t.Fatalf("CreateApp: %v", err)
	}
	if err := e.St.UpdateAppDetails(a.ID, "ABCDE12345.com.example.app", "com.example.app",
		strings.TrimSuffix(strings.Repeat("AB:", 32), ":"), ""); err != nil {
		e.t.Fatalf("UpdateAppDetails: %v", err)
	}
	a, _ = e.St.GetApp(a.ID)
	return a
}

// link: route link with both store targets + web fallback (the common production shape).
func (e *env) link(a store.App, key, route string) store.Link {
	e.t.Helper()
	return e.linkWith(store.Link{AppID: a.ID, Key: key, URL: route,
		IOS:         "https://apps.apple.com/app/id1234567890",
		Android:     "https://play.google.com/store/apps/details?id=com.example.app",
		FallbackURL: "https://example.com/download"})
}

func (e *env) linkWith(l store.Link) store.Link {
	e.t.Helper()
	got, err := e.St.CreateLink(l)
	if err != nil {
		e.t.Fatalf("CreateLink: %v", err)
	}
	return got
}

// resp: status, headers, body.
type resp struct {
	Code int
	H    http.Header
	Body string
}

func (r resp) Loc() string { return r.H.Get("Location") }

// JSON decodes the body into v (test fails on bad JSON).
func (r resp) JSON(t *testing.T, v any) {
	t.Helper()
	if err := json.Unmarshal([]byte(r.Body), v); err != nil {
		t.Fatalf("bad JSON (%d) %q: %v", r.Code, r.Body, err)
	}
}

// do: one request, redirects not followed. hdr "Host" sets the Host header.
func do(t *testing.T, method, url, body string, hdr map[string]string) resp {
	t.Helper()
	var rd io.Reader
	if body != "" {
		rd = bytes.NewBufferString(body)
	}
	req, err := http.NewRequest(method, url, rd)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	for k, v := range hdr {
		if strings.EqualFold(k, "Host") {
			req.Host = v
			continue
		}
		req.Header.Set(k, v)
	}
	c := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := c.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return resp{Code: res.StatusCode, H: res.Header, Body: string(b)}
}

// tap: browser GET on the public listener from clientIP (behind the proxy) with the given UA.
func (e *env) tap(path, ua string) resp {
	e.t.Helper()
	return do(e.t, "GET", e.Pub.URL+path, "", map[string]string{"User-Agent": ua, "X-Forwarded-For": clientIP})
}

// tapH: tap with extra headers.
func (e *env) tapH(path, ua string, hdr map[string]string) resp {
	e.t.Helper()
	h := map[string]string{"User-Agent": ua, "X-Forwarded-For": clientIP}
	for k, v := range hdr {
		h[k] = v
	}
	return do(e.t, "GET", e.Pub.URL+path, "", h)
}

// mobileTap: what a phone browser does: hop 1 (interstitial reads screen + tz, reloads) then hop 2 with _dt=1&screen&tz.
func (e *env) mobileTap(path, ua string) resp {
	e.t.Helper()
	if r := e.tap(path, ua); r.Code != http.StatusOK {
		e.t.Fatalf("hop 1 %s = %d, want 200 interstitial (Location %q)", path, r.Code, r.Loc())
	}
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	return e.tap(path+sep+"_dt=1&screen=393x852%403&tz=Asia%2FHo_Chi_Minh", ua)
}

// sdk: SDK call with valid auth from the device's IP.
func (e *env) sdk(a store.App, path, body string) resp {
	e.t.Helper()
	return e.sdkH(a, path, body, nil)
}

func (e *env) sdkH(a store.App, path, body string, extra map[string]string) resp {
	e.t.Helper()
	h := map[string]string{
		"Authorization": "Bearer " + apiKey, "X-App-ID": a.ID, "X-SDK": sdkHeader,
		"Content-Type": "application/json", "X-Forwarded-For": clientIP,
	}
	for k, v := range extra {
		if v == "" {
			delete(h, k)
			continue
		}
		h[k] = v
	}
	return do(e.t, "POST", e.Pub.URL+path, body, h)
}

// portal: portal API call (no admin password unless env has one; then pass a session cookie via hdr).
func (e *env) portal(method, path, body string, hdr map[string]string) resp {
	e.t.Helper()
	h := map[string]string{"Content-Type": "application/json"}
	for k, v := range hdr {
		h[k] = v
	}
	return do(e.t, method, e.Portal.URL+path, body, h)
}

// SDK 2.3.1 match-link bodies (links/utils/fingerprint.js). userAgent = the app's own WebView UA (Constants.getWebViewUserAgentAsync),
// not the browser's: iOS WKWebView drops "Version/x Safari/x", Android WebView adds "; wv" + "Version/4.0".
const (
	uaIOSAppWebView     = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Mobile/15E148"
	uaAndroidAppWebView = "Mozilla/5.0 (Linux; Android 14; Pixel 7 Build/UP1A.231005.007; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/126.0.0.0 Mobile Safari/537.36"
)

// iosFingerprint: probabilistic body from an iPhone 15 (393x852@3, vi-VN, Asia/Ho_Chi_Minh). extra = `,"pastedLink":"..."` etc.
func iosFingerprint(extra string) string {
	return `{"platform":"ios","model":"iPhone15,2","manufacturer":"Apple","systemVersion":"17.5","screenWidth":393,"screenHeight":852,"scale":3,` +
		`"locale":[{"languageTag":"vi-VN"},{"languageTag":"en-US"}],"timezone":"Asia/Ho_Chi_Minh","userAgent":"` + uaIOSAppWebView + `",` +
		`"timestamp":` + nowMS() + extra + `}`
}

// androidFingerprint: probabilistic body from a Pixel 7 (no install referrer click_id).
func androidFingerprint(extra string) string {
	return `{"platform":"android","model":"Pixel 7","manufacturer":"Google","systemVersion":"14","screenWidth":393,"screenHeight":852,"scale":3,` +
		`"locale":[{"languageTag":"vi-VN"}],"timezone":"Asia/Ho_Chi_Minh","userAgent":"` + uaAndroidAppWebView + `",` +
		`"timestamp":` + nowMS() + extra + `}`
}

func nowMS() string { return strconv.FormatInt(time.Now().UnixMilli(), 10) }

func must(t *testing.T, cond bool, format string, args ...any) {
	t.Helper()
	if !cond {
		t.Fatalf(format, args...)
	}
}
