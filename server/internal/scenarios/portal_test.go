package scenarios

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"detur.dev/server/internal/api"
	"detur.dev/server/internal/store"
	"detur.dev/server/internal/webhook"
)

const portalAdminPW = "correct-horse-battery-staple-42"

func portalAdminEnv(t *testing.T) (*env, map[string]string) {
	t.Helper()
	e := newEnvWith(t, envOpts{AdminPassword: portalAdminPW, TrustProxy: true})
	return e, portalElevate(t, e)
}

func portalElevate(t *testing.T, e *env) map[string]string {
	t.Helper()
	r := e.portal("POST", "/api/admin/session", `{"password":"`+portalAdminPW+`"}`, nil)
	must(t, r.Code == 200, "elevate = %d %s", r.Code, r.Body)
	for _, c := range portalCookies(r) {
		if c.Name == "detur_admin" {
			return map[string]string{"Cookie": c.Name + "=" + c.Value}
		}
	}
	t.Fatalf("no detur_admin cookie in %v", r.H["Set-Cookie"])
	return nil
}

func portalCookies(r resp) []*http.Cookie { return (&http.Response{Header: r.H}).Cookies() }

func portalCreateApp(t *testing.T, e *env, name string, hdr map[string]string) (id, key string) {
	t.Helper()
	r := e.portal("POST", "/api/apps", `{"name":"`+name+`"}`, hdr)
	must(t, r.Code == 201, "create app = %d %s", r.Code, r.Body)
	var out struct{ ID, APIKey string }
	r.JSON(t, &out)
	return out.ID, out.APIKey
}

func portalSDK(e *env, appID, key, path, body string) resp {
	e.t.Helper()
	return do(e.t, "POST", e.Pub.URL+path, body, map[string]string{
		"Authorization": "Bearer " + key, "X-App-ID": appID, "X-SDK": sdkHeader,
		"Content-Type": "application/json", "X-Forwarded-For": clientIP,
	})
}

func portalLinkBody(key, url, fallback string) string {
	b, _ := json.Marshal(map[string]string{
		"key": key, "url": url,
		"ios":         "https://apps.apple.com/app/id1234567890",
		"android":     "https://play.google.com/store/apps/details?id=com.example.app",
		"fallbackUrl": fallback, "ogTitle": "", "ogDescription": "", "ogImage": "", "expiresAt": "", "expiredUrl": "",
	})
	return string(b)
}

func linkScalars(t *testing.T, r resp) map[string]string {
	t.Helper()
	var raw map[string]json.RawMessage
	r.JSON(t, &raw)
	m := make(map[string]string, len(raw))
	for k, v := range raw {
		var s string
		if json.Unmarshal(v, &s) == nil {
			m[k] = s
		}
	}
	return m
}

func portalCreateLink(t *testing.T, e *env, appID, body string) map[string]string {
	t.Helper()
	r := e.portal("POST", "/api/apps/"+appID+"/links", body, nil)
	must(t, r.Code == 201, "create link = %d %s", r.Code, r.Body)
	return linkScalars(t, r)
}

func portalInstall(t *testing.T, e *env, a store.App, key string) {
	t.Helper()
	r := e.mobileTap("/"+key, uaAndroidChrome)
	must(t, r.Code == 302, "tap = %d", r.Code)
	r = e.sdk(a, "/api/link/match-link", androidFingerprint(""))
	must(t, r.Code == 200, "match-link = %d %s", r.Code, r.Body)
}

type portalHit struct {
	H    http.Header
	Body []byte
}

type portalReceiver struct {
	*httptest.Server
	mu   sync.Mutex
	hits []portalHit
}

func newPortalReceiver(t *testing.T, status func(body []byte) int) *portalReceiver {
	t.Helper()
	rc := &portalReceiver{}
	rc.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		rc.mu.Lock()
		rc.hits = append(rc.hits, portalHit{r.Header.Clone(), b})
		rc.mu.Unlock()
		w.WriteHeader(status(b))
	}))
	t.Cleanup(rc.Close)
	return rc
}

func (rc *portalReceiver) Hits() []portalHit {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	return append([]portalHit(nil), rc.hits...)
}

func ok200([]byte) int { return 200 }

type portalWebhook struct {
	ID              string   `json:"id"`
	URL             string   `json:"url"`
	Secret          string   `json:"secret"`
	Types           []string `json:"types"`
	Enabled         bool     `json:"enabled"`
	CursorInstalls  int64    `json:"cursorInstalls"`
	FailsInstalls   int      `json:"failsInstalls"`
	FailsClicks     int      `json:"failsClicks"`
	BackoffInstalls *string  `json:"backoffInstalls"`
}

func portalCreateWebhook(t *testing.T, e *env, adm map[string]string, appID, url, types string) portalWebhook {
	t.Helper()
	r := e.portal("POST", "/api/apps/"+appID+"/webhooks", `{"url":"`+url+`","types":`+types+`}`, adm)
	must(t, r.Code == 201, "create webhook = %d %s", r.Code, r.Body)
	var w portalWebhook
	r.JSON(t, &w)
	return w
}

func portalWebhooks(t *testing.T, e *env, adm map[string]string, appID string) []portalWebhook {
	t.Helper()
	r := e.portal("GET", "/api/apps/"+appID+"/webhooks", "", adm)
	must(t, r.Code == 200, "list webhooks = %d %s", r.Code, r.Body)
	var ws []portalWebhook
	r.JSON(t, &ws)
	return ws
}

func portalDeliver(e *env) { webhook.NewWorker(e.St, time.Hour).ProcessOnce(context.Background()) }

var sigRe = regexp.MustCompile(`^t=(\d+),v1=([0-9a-f]{64})$`)

func portalVerify(h http.Header, body []byte, secret string) bool {
	m := sigRe.FindStringSubmatch(h.Get("Detur-Signature"))
	if m == nil {
		return false
	}
	ts, _ := strconv.ParseInt(m[1], 10, 64)
	if d := time.Since(time.Unix(ts, 0)); d > 5*time.Minute || d < -5*time.Minute {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("t." + m[1] + "."))
	mac.Write(body)
	return hmac.Equal([]byte(hex.EncodeToString(mac.Sum(nil))), []byte(m[2]))
}

func TestS_PORTAL_01_CreateAppKeyShownOnce(t *testing.T) {
	e := newEnv(t)
	id, key := portalCreateApp(t, e, "Shop", nil)
	must(t, strings.HasPrefix(key, "dk_") && len(key) >= 20, "key %q", key)
	for _, p := range []string{"/api/apps", "/api/apps/" + id} {
		r := e.portal("GET", p, "", nil)
		must(t, r.Code == 200, "GET %s = %d", p, r.Code)
		must(t, !strings.Contains(r.Body, key), "GET %s leaks plaintext key: %s", p, r.Body)
	}
	r := portalSDK(e, id, key, "/api/link/resolve-short", `{"url":"https://lnk.example/none"}`)
	must(t, r.Code != 401, "fresh key rejected by SDK: %d", r.Code)
}

func TestS_PORTAL_02_AppDetailsPublishWellKnown(t *testing.T) {
	e := newEnv(t)
	id, _ := portalCreateApp(t, e, "Shop", nil)
	fp := strings.TrimSuffix(strings.Repeat("1F:", 32), ":")
	r := e.portal("PATCH", "/api/apps/"+id, `{"iosAppId":"TEAM123456.com.shop.app","androidPackage":"com.shop.app","androidCertFingerprint":"`+fp+`","iosScheme":""}`, nil)
	must(t, r.Code == 200, "PATCH = %d %s", r.Code, r.Body)
	e.linkWith(store.Link{AppID: id, Key: "promo", URL: "/p"})
	aasa := do(t, "GET", e.Pub.URL+"/.well-known/apple-app-site-association", "", map[string]string{"Host": linkDomain})
	must(t, aasa.Code == 200 && strings.Contains(aasa.Body, "TEAM123456.com.shop.app"), "AASA = %d %s", aasa.Code, aasa.Body)
	al := do(t, "GET", e.Pub.URL+"/.well-known/assetlinks.json", "", map[string]string{"Host": linkDomain})
	must(t, al.Code == 200 && strings.Contains(al.Body, "com.shop.app") && strings.Contains(al.Body, fp), "assetlinks = %d %s", al.Code, al.Body)
}

func TestS_PORTAL_03_IOSSchemeNormalizedUnsafeRejected(t *testing.T) {
	e := newEnv(t)
	id, _ := portalCreateApp(t, e, "Shop", nil)
	body := func(s string) string {
		return `{"iosAppId":"","androidPackage":"","androidCertFingerprint":"","iosScheme":"` + s + `"}`
	}
	r := e.portal("PATCH", "/api/apps/"+id, body("myapp://"), nil)
	must(t, r.Code == 200, "PATCH = %d %s", r.Code, r.Body)
	var a struct{ IOSScheme string }
	r.JSON(t, &a)
	must(t, a.IOSScheme == "myapp", "iosScheme = %q", a.IOSScheme)
	for _, bad := range []string{"https", "javascript", "data", "intent", "my app"} {
		r := e.portal("PATCH", "/api/apps/"+id, body(bad), nil)
		must(t, r.Code == 400, "scheme %q = %d", bad, r.Code)
	}
}

func TestS_PORTAL_04_RotateKeyKillsOldKeyNow(t *testing.T) {
	e := newEnv(t)
	id, oldKey := portalCreateApp(t, e, "Shop", nil)
	r := e.portal("POST", "/api/apps/"+id+"/rotate-key", "", nil)
	must(t, r.Code == 200, "rotate = %d %s", r.Code, r.Body)
	var out struct{ APIKey string }
	r.JSON(t, &out)
	must(t, out.APIKey != "" && out.APIKey != oldKey, "new key %q", out.APIKey)
	body := `{"url":"https://lnk.example/none"}`
	must(t, portalSDK(e, id, oldKey, "/api/link/resolve-short", body).Code == 401, "old key still accepted")
	must(t, portalSDK(e, id, out.APIKey, "/api/link/resolve-short", body).Code != 401, "new key rejected")
}

func TestS_PORTAL_05_RevokeKeyStopsSDK(t *testing.T) {
	e := newEnv(t)
	id, key := portalCreateApp(t, e, "Shop", nil)
	r := e.portal("DELETE", "/api/apps/"+id+"/key", "", nil)
	must(t, r.Code == 204, "revoke = %d %s", r.Code, r.Body)
	for _, p := range []string{"/api/link/resolve-short", "/api/analytics/event", "/api/link/match-link"} {
		got := portalSDK(e, id, key, p, `{"url":"https://lnk.example/x","event_name":"x"}`)
		must(t, got.Code == 401, "%s with revoked key = %d", p, got.Code)
	}
}

func TestS_PORTAL_06_DeleteAppStopsLinks(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "promo", "/p")
	r := e.portal("DELETE", "/api/apps/"+a.ID, "", nil)
	must(t, r.Code == 204, "delete = %d %s", r.Code, r.Body)
	tap := e.tap("/promo", uaDesktopChrome)
	must(t, tap.Code == 404, "deleted app's link = %d (Location %q)", tap.Code, tap.Loc())
	must(t, e.sdk(a, "/api/link/resolve-short", `{"url":"https://lnk.example/promo"}`).Code == 401, "deleted app key still works")
	must(t, e.portal("GET", "/api/apps/"+a.ID, "", nil).Code == 404, "deleted app still readable")
}

func TestS_PORTAL_07_CreateLinkOnDeletedApp404(t *testing.T) {
	e := newEnv(t)
	r := e.portal("POST", "/api/apps/no-such-app/links", portalLinkBody("promo", "/p", ""), nil)
	must(t, r.Code == 404, "create link on missing app = %d %s", r.Code, r.Body)
}

func TestS_PORTAL_08_CreateLinkRoundTrip(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	l := portalCreateLink(t, e, a.ID, portalLinkBody("summer", "/product/42?ref=ig", "https://example.com/summer"))
	must(t, l["key"] == "summer" && l["url"] == "/product/42?ref=ig" && l["fallbackUrl"] == "https://example.com/summer" && l["id"] != "", "link %v", l)
	r := e.portal("GET", "/api/apps/"+a.ID+"/links", "", nil)
	must(t, r.Code == 200 && strings.Contains(r.Body, `"summer"`), "list = %d %s", r.Code, r.Body)
	tap := e.mobileTap("/summer", uaAndroidChrome)
	must(t, tap.Code == 302 && strings.HasPrefix(tap.Loc(), "https://play.google.com/"), "tap = %d %q", tap.Code, tap.Loc())
}

func TestS_PORTAL_09_DestinationMustBeAppRoute(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	for _, d := range []string{"https://example.com/product/42", "myapp://product/42", "product/42", "//evil.example/x"} {
		r := e.portal("POST", "/api/apps/"+a.ID+"/links", portalLinkBody("k1", d, ""), nil)
		must(t, r.Code == 400, "destination %q = %d %s", d, r.Code, r.Body)
	}
}

func TestS_PORTAL_10_UnsafeOrRelativeTargetsRejected(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	cases := map[string]string{
		"ios": "javascript:alert(1)", "android": "vbscript:x", "fallbackUrl": "data:text/html,<script>1</script>",
		"expiredUrl": "file:///etc/passwd",
	}
	cases2 := map[string]string{"fallbackUrl": "example.com/download"}
	for f, v := range cases {
		b, _ := json.Marshal(map[string]string{"key": "k1", "url": "/p", f: v})
		r := e.portal("POST", "/api/apps/"+a.ID+"/links", string(b), nil)
		must(t, r.Code == 400, "%s=%q = %d", f, v, r.Code)
	}
	for f, v := range cases2 {
		b, _ := json.Marshal(map[string]string{"key": "k1", "url": "/p", f: v})
		r := e.portal("POST", "/api/apps/"+a.ID+"/links", string(b), nil)
		must(t, r.Code == 400, "%s=%q = %d", f, v, r.Code)
	}
	r := e.portal("POST", "/api/apps/"+a.ID+"/links", `{"key":"k1","url":"/p","fallbackUrl":"https://"}`, nil)
	must(t, r.Code == 400, "host-less https fallback = %d", r.Code)
}

func TestS_PORTAL_11_ReservedAndMalformedKeysRejected(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	for _, k := range []string{"api", "API", "health", "Health", ".well-known", "favicon.ico", "robots.txt", "_root", "a/b", "a b", "%2e", strings.Repeat("x", 65)} {
		r := e.portal("POST", "/api/apps/"+a.ID+"/links", portalLinkBody(k, "/p", ""), nil)
		must(t, r.Code == 400, "key %q = %d", k, r.Code)
	}
	must(t, do(t, "GET", e.Pub.URL+"/health", "", nil).Body == "ok", "/health broken")
}

func TestS_PORTAL_12_DuplicateKeyConflicts(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	b, _ := portalCreateApp(t, e, "Other", nil)
	portalCreateLink(t, e, a.ID, portalLinkBody("promo", "/p", "https://example.com/a"))
	for _, c := range []struct{ app, key string }{{a.ID, "promo"}, {a.ID, "PROMO"}, {b, "promo"}} {
		r := e.portal("POST", "/api/apps/"+c.app+"/links", portalLinkBody(c.key, "/q", "https://example.com/b"), nil)
		must(t, r.Code == 409, "dup %q in %s = %d %s", c.key, c.app, r.Code, r.Body)
	}
	must(t, e.tap("/promo", uaDesktopChrome).Loc() == "https://example.com/a", "first link hijacked")
}

func TestS_PORTAL_13_EditLinkChangesRedirect(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	l := portalCreateLink(t, e, a.ID, portalLinkBody("promo", "/p", "https://example.com/old"))
	r := e.portal("PATCH", "/api/links/"+l["id"], `{"url":"/p","fallbackUrl":"https://example.com/new"}`, nil)
	must(t, r.Code == 200, "PATCH = %d %s", r.Code, r.Body)
	got := linkScalars(t, r)
	must(t, got["ios"] == l["ios"] && got["android"] == l["android"], "partial edit wiped stores: %v", got)
	tap := e.tap("/promo", uaDesktopChrome)
	must(t, tap.Code == 302 && tap.Loc() == "https://example.com/new", "tap = %d %q", tap.Code, tap.Loc())
}

func TestS_PORTAL_14_ExpiryAndExpiredURL(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	l := portalCreateLink(t, e, a.ID, portalLinkBody("sale", "/p", "https://example.com/sale"))
	past := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	r := e.portal("PATCH", "/api/links/"+l["id"], `{"url":"/p","expiresAt":"`+past+`","expiredUrl":"https://example.com/over"}`, nil)
	must(t, r.Code == 200, "PATCH = %d %s", r.Code, r.Body)
	tap := e.tap("/sale", uaDesktopChrome)
	must(t, tap.Code == 302 && tap.Loc() == "https://example.com/over", "expired tap = %d %q", tap.Code, tap.Loc())
	e.portal("PATCH", "/api/links/"+l["id"], `{"url":"/p","expiredUrl":""}`, nil)
	must(t, e.tap("/sale", uaDesktopChrome).Code == 410, "expired without URL not 410")
	e.portal("PATCH", "/api/links/"+l["id"], `{"url":"/p","expiresAt":""}`, nil)
	tap = e.tap("/sale", uaDesktopChrome)
	must(t, tap.Code == 302 && tap.Loc() == "https://example.com/sale", "cleared expiry tap = %d %q", tap.Code, tap.Loc())
}

func TestS_PORTAL_15_OGFieldsValidatedAndServed(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	bad := []string{
		`{"key":"og","url":"/p","ogImage":"http://cdn.example/x.png"}`,
		`{"key":"og","url":"/p","ogTitle":"` + strings.Repeat("t", 101) + `"}`,
		`{"key":"og","url":"/p","ogDescription":"` + strings.Repeat("d", 301) + `"}`,
	}
	for _, b := range bad {
		must(t, e.portal("POST", "/api/apps/"+a.ID+"/links", b, nil).Code == 400, "accepted %s", b[:40])
	}
	portalCreateLink(t, e, a.ID, `{"key":"og","url":"/p","ios":"https://apps.apple.com/app/id1","ogTitle":"Summer Sale 50%","ogDescription":"Half off","ogImage":"https://cdn.example/s.png"}`)
	r := e.tap("/og", uaSlackBot)
	must(t, r.Code == 200 && strings.Contains(r.Body, "Summer Sale 50%") && strings.Contains(r.Body, "https://cdn.example/s.png"), "bot = %d %s", r.Code, r.Body)
}

func TestS_PORTAL_16_DeleteLinkFreesKey(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	l := portalCreateLink(t, e, a.ID, portalLinkBody("promo", "/old", "https://example.com/old"))
	must(t, e.portal("DELETE", "/api/links/"+l["id"], "", nil).Code == 204, "delete failed")
	must(t, e.tap("/promo", uaDesktopChrome).Code == 404, "deleted link still resolves")
	portalCreateLink(t, e, a.ID, portalLinkBody("promo", "/new", "https://example.com/new"))
	must(t, e.tap("/promo", uaDesktopChrome).Loc() == "https://example.com/new", "reused key wrong target")
}

func TestS_PORTAL_17_RulesRoundTrip(t *testing.T) {
	e, adm := portalAdminEnv(t)
	a := e.app()
	l := e.link(a, "promo", "/p")
	base := "/api/apps/" + a.ID + "/links/" + l.ID + "/rules"
	r := e.portal("GET", base, "", nil)
	must(t, r.Code == 200 && strings.TrimSpace(r.Body) == "[]", "empty rules = %d %q", r.Code, r.Body)
	r = e.portal("PUT", base, `[{"name":"German","cond":{"lang":"de"},"action":{"targets":{"destination":"/de/p"}}}]`, adm)
	must(t, r.Code == 200, "PUT = %d %s", r.Code, r.Body)
	var rules []struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Position int    `json:"position"`
	}
	e.portal("GET", base, "", nil).JSON(t, &rules)
	must(t, len(rules) == 1 && rules[0].ID != "" && rules[0].Name == "German", "rules %+v", rules)
}

func TestS_PORTAL_18_SavedRuleChangesClick(t *testing.T) {
	e, adm := portalAdminEnv(t)
	a := e.app()
	l := e.link(a, "promo", "/p")
	base := "/api/apps/" + a.ID + "/links/" + l.ID + "/rules"
	r := e.portal("PUT", base, `[{"name":"Desktop LP","cond":{"platform":"desktop"},"action":{"targets":{"fallback_url":"https://example.com/desktop"}}}]`, adm)
	must(t, r.Code == 200, "PUT = %d %s", r.Code, r.Body)
	tap := e.tap("/promo", uaDesktopChrome)
	must(t, tap.Code == 302 && tap.Loc() == "https://example.com/desktop", "desktop = %d %q", tap.Code, tap.Loc())
	and := e.mobileTap("/promo", uaAndroidChrome)
	must(t, strings.HasPrefix(and.Loc(), "https://play.google.com/"), "android = %q", and.Loc())
	must(t, e.portal("PUT", base, `[]`, adm).Code == 200, "clear failed")
	tap = e.tapH("/promo", uaDesktopMac, nil)
	must(t, tap.Loc() == "https://example.com/download", "after clear desktop = %d %q", tap.Code, tap.Loc())
}

func TestS_PORTAL_19_RuleTargetsValidated(t *testing.T) {
	e, adm := portalAdminEnv(t)
	a := e.app()
	l := e.link(a, "promo", "/p")
	base := "/api/apps/" + a.ID + "/links/" + l.ID + "/rules"
	for _, tg := range []string{`{"destination":"https://example.com/p"}`, `{"fallback_url":"javascript:alert(1)"}`, `{"ios":"example.com"}`} {
		r := e.portal("PUT", base, `[{"name":"R","cond":{},"action":{"targets":`+tg+`}}]`, adm)
		must(t, r.Code == 400, "target %s = %d", tg, r.Code)
	}
}

func TestS_PORTAL_20_SplitValidated(t *testing.T) {
	e, adm := portalAdminEnv(t)
	a := e.app()
	l := e.link(a, "promo", "/p")
	base := "/api/apps/" + a.ID + "/links/" + l.ID + "/rules"
	bad := []string{
		`[{"name":"AB","cond":{},"action":{"split":[{"name":"A","weight":50,"targets":{}},{"name":"B","weight":40,"targets":{}}]}}]`,
		`[{"name":"AB","cond":{},"action":{"split":[{"name":"A","weight":100,"targets":{}}]}}]`,
		`[{"name":"AB","cond":{},"action":{"split":[{"name":"A","weight":50,"targets":{}},{"name":"A","weight":50,"targets":{}}]}}]`,
		`[{"name":"AB","cond":{},"action":{"split":[{"name":"A","weight":0,"targets":{}},{"name":"B","weight":100,"targets":{}}]}}]`,
	}
	for _, b := range bad {
		must(t, e.portal("PUT", base, b, adm).Code == 400, "accepted %s", b)
	}
	must(t, strings.TrimSpace(e.portal("GET", base, "", nil).Body) == "[]", "invalid rules saved")
}

func TestS_PORTAL_21_SplitClicksShowInVariants(t *testing.T) {
	e, adm := portalAdminEnv(t)
	a := e.app()
	l := e.link(a, "promo", "/p")
	base := "/api/apps/" + a.ID + "/links/" + l.ID
	r := e.portal("PUT", base+"/rules", `[{"name":"LP test","cond":{},"action":{"split":[`+
		`{"name":"Blue","weight":50,"targets":{"fallback_url":"https://example.com/blue"}},`+
		`{"name":"Green","weight":50,"targets":{"fallback_url":"https://example.com/green"}}]}}]`, adm)
	must(t, r.Code == 200, "PUT = %d %s", r.Code, r.Body)
	tap := e.tap("/promo", uaDesktopChrome)
	want := map[string]string{"https://example.com/blue": "Blue", "https://example.com/green": "Green"}[tap.Loc()]
	must(t, want != "", "tap went to %d %q", tap.Code, tap.Loc())
	var stats []struct {
		Variant string `json:"variant"`
		Clicks  int64  `json:"clicks"`
	}
	r = e.portal("GET", base+"/variants?days=7", "", nil)
	must(t, r.Code == 200, "variants = %d %s", r.Code, r.Body)
	r.JSON(t, &stats)
	must(t, len(stats) == 1 && stats[0].Variant == want && stats[0].Clicks == 1, "stats %+v want %s=1", stats, want)
}

func TestS_PORTAL_22_AnalyticsAfterRealTraffic(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "promo", "/p")
	portalInstall(t, e, a, "promo")
	must(t, e.sdk(a, "/api/analytics/event", `{"event_name":"purchase","data":{"value":9.99}}`).Code == 200, "event failed")
	var an struct {
		Days  []struct{ Clicks, NonOrganic int64 } `json:"days"`
		Links []struct {
			Key             string
			Clicks, Matches int64
		} `json:"links"`
		Events []struct {
			Event string
			Count int64
		} `json:"events"`
	}
	r := e.portal("GET", "/api/apps/"+a.ID+"/analytics?days=7", "", nil)
	must(t, r.Code == 200, "analytics = %d %s", r.Code, r.Body)
	r.JSON(t, &an)
	var clicks, installs int64
	for _, d := range an.Days {
		clicks += d.Clicks
		installs += d.NonOrganic
	}
	must(t, clicks == 1 && installs == 1, "days: clicks %d installs %d", clicks, installs)
	must(t, len(an.Links) == 1 && an.Links[0].Key == "promo" && an.Links[0].Clicks == 1 && an.Links[0].Matches == 1, "links %+v", an.Links)
	must(t, len(an.Events) == 1 && an.Events[0].Event == "purchase" && an.Events[0].Count == 1, "events %+v", an.Events)
	var mq struct{ Methods map[string]int64 }
	e.portal("GET", "/api/apps/"+a.ID+"/match-quality?days=30", "", nil).JSON(t, &mq)
	must(t, mq.Methods["probabilistic"] == 1, "match-quality %+v", mq.Methods)
}

func TestS_PORTAL_23_FraudPageEmptyAppShape(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	r := e.portal("GET", "/api/apps/"+a.ID+"/fraud?days=7", "", nil)
	must(t, r.Code == 200, "fraud = %d %s", r.Code, r.Body)
	var f struct {
		Signals  map[string]int64 `json:"signals"`
		Installs []any            `json:"installs"`
	}
	r.JSON(t, &f)
	must(t, f.Signals != nil && f.Installs != nil, "null field in %s", r.Body)
}

func TestS_PORTAL_24_FraudSettingsRoundTrip(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	p := "/api/apps/" + a.ID + "/fraud/settings"
	r := e.portal("GET", p, "", nil)
	must(t, r.Code == 200, "GET = %d %s", r.Code, r.Body)
	var s map[string]any
	r.JSON(t, &s)
	s["velocityMode"] = "active"
	s["velocityIpMax"] = 7
	b, _ := json.Marshal(s)
	r = e.portal("PATCH", p, string(b), nil)
	must(t, r.Code == 200, "PATCH = %d %s", r.Code, r.Body)
	var got map[string]any
	e.portal("GET", p, "", nil).JSON(t, &got)
	must(t, got["velocityMode"] == "active" && got["velocityIpMax"] == float64(7), "persisted %v", got)
	must(t, e.portal("PATCH", p, `{"velocityMode":"tagged"}`, nil).Code == 400, "partial accepted")
	s["timingMode"] = "block"
	b, _ = json.Marshal(s)
	must(t, e.portal("PATCH", p, string(b), nil).Code == 400, "unknown mode accepted")
}

func TestS_PORTAL_25_HealthReflectsSetup(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	status := func() map[string]string {
		var cs []struct{ Check, Status string }
		r := e.portal("GET", "/api/apps/"+a.ID+"/health", "", nil)
		must(t, r.Code == 200, "health = %d", r.Code)
		r.JSON(t, &cs)
		m := map[string]string{}
		for _, c := range cs {
			m[c.Check] = c.Status
		}
		return m
	}
	must(t, status()["SDK"] == "fail", "SDK before first call: %v", status())
	e.sdk(a, "/api/analytics/event", `{"event_name":"app_open"}`)
	must(t, status()["SDK"] == "ok", "SDK after call: %v", status())
	e.portal("PATCH", "/api/apps/"+a.ID, `{"iosAppId":"com.example.app","androidPackage":"com.example.app","androidCertFingerprint":"`+a.AndroidCertFingerprint+`","iosScheme":""}`, nil)
	must(t, status()["iOS app ID"] == "fail", "bad iOS ID: %v", status())
}

func TestS_PORTAL_26_MatchingSettingsValidated(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	p := "/api/apps/" + a.ID + "/matching"
	r := e.portal("PATCH", p, `{"threshold":900,"windowMinutes":30}`, nil)
	must(t, r.Code == 200, "PATCH = %d %s", r.Code, r.Body)
	var app struct{ MatchThreshold, MatchWindowMinutes int }
	e.portal("GET", "/api/apps/"+a.ID, "", nil).JSON(t, &app)
	must(t, app.MatchThreshold == 900 && app.MatchWindowMinutes == 30, "persisted %+v", app)
	for _, b := range []string{`{"threshold":650,"windowMinutes":30}`, `{"threshold":900,"windowMinutes":0}`, `{"threshold":900}`} {
		must(t, e.portal("PATCH", p, b, nil).Code == 400, "accepted %s", b)
	}
}

func TestS_PORTAL_27_ViewerReadsOpenWritesGated(t *testing.T) {
	e := newEnvWith(t, envOpts{AdminPassword: portalAdminPW, TrustProxy: true})
	a := e.app()
	l := e.link(a, "promo", "/p")
	var cfg struct{ AdminSet bool }
	e.portal("GET", "/api/config", "", nil).JSON(t, &cfg)
	must(t, cfg.AdminSet, "config adminSet false")
	for _, p := range []string{"/api/apps", "/api/apps/" + a.ID, "/api/apps/" + a.ID + "/links", "/api/apps/" + a.ID + "/analytics?days=7",
		"/api/apps/" + a.ID + "/links/" + l.ID + "/rules", "/api/apps/" + a.ID + "/links/" + l.ID + "/variants"} {
		must(t, e.portal("GET", p, "", nil).Code == 200, "viewer GET %s blocked", p)
	}
	writes := [][3]string{
		{"POST", "/api/apps", `{"name":"x"}`},
		{"PATCH", "/api/apps/" + a.ID, `{"iosAppId":"","androidPackage":"","androidCertFingerprint":"","iosScheme":""}`},
		{"POST", "/api/apps/" + a.ID + "/rotate-key", ""},
		{"DELETE", "/api/apps/" + a.ID + "/key", ""},
		{"PATCH", "/api/apps/" + a.ID + "/matching", `{"threshold":900,"windowMinutes":30}`},
		{"PATCH", "/api/apps/" + a.ID + "/tagging", `{"tagLinks":true}`},
		{"DELETE", "/api/apps/" + a.ID, ""},
	}
	for _, w := range writes {
		r := e.portal(w[0], w[1], w[2], nil)
		must(t, r.Code == 403, "viewer %s %s = %d", w[0], w[1], r.Code)
	}
	r := e.portal("PUT", "/api/apps/"+a.ID+"/links/"+l.ID+"/rules", `[]`, nil)
	must(t, r.Code == 200, "viewer PUT rules = %d %s", r.Code, r.Body)
	must(t, e.sdk(a, "/api/link/resolve-short", `{"url":"https://lnk.example/promo"}`).Code == 200, "app/key changed by viewer")
}

func TestS_PORTAL_28_ElevateUnlocksAdmin(t *testing.T) {
	e := newEnvWith(t, envOpts{AdminPassword: portalAdminPW, TrustProxy: true})
	r := e.portal("POST", "/api/admin/session", `{"password":"`+portalAdminPW+`"}`, nil)
	must(t, r.Code == 200, "elevate = %d", r.Code)
	cs := portalCookies(r)
	must(t, len(cs) == 1 && cs[0].HttpOnly && cs[0].SameSite != http.SameSiteNoneMode, "cookie %+v", cs)
	adm := map[string]string{"Cookie": cs[0].Name + "=" + cs[0].Value}
	var st struct {
		Admin     bool
		ExpiresAt time.Time
	}
	e.portal("GET", "/api/admin/session", "", adm).JSON(t, &st)
	must(t, st.Admin && time.Until(st.ExpiresAt) > 11*time.Hour && time.Until(st.ExpiresAt) <= 12*time.Hour+time.Minute, "status %+v", st)
	portalCreateApp(t, e, "Shop", adm)
}

func TestS_PORTAL_29_WrongPasswordDelayed401(t *testing.T) {
	e := newEnvWith(t, envOpts{AdminPassword: portalAdminPW, TrustProxy: true})
	start := time.Now()
	r := e.portal("POST", "/api/admin/session", `{"password":"hunter2"}`, nil)
	must(t, r.Code == 401, "wrong pw = %d", r.Code)
	must(t, time.Since(start) >= 900*time.Millisecond, "no delay: %v", time.Since(start))
	must(t, len(portalCookies(r)) == 0, "cookie set on wrong pw")
}

func TestS_PORTAL_30_ParallelGuessesSerialized(t *testing.T) {
	e := newEnvWith(t, envOpts{AdminPassword: portalAdminPW, TrustProxy: true})
	start := time.Now()
	var wg sync.WaitGroup
	for i := range 3 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req, _ := http.NewRequest("POST", e.Portal.URL+"/api/admin/session", strings.NewReader(`{"password":"guess`+strconv.Itoa(i)+`"}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Forwarded-For", "198.51.100."+strconv.Itoa(i+1))
			c := &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}
			if res, err := c.Do(req); err == nil {
				res.Body.Close()
			}
		}()
	}
	wg.Wait()
	must(t, time.Since(start) >= 2800*time.Millisecond, "3 parallel guesses took %v", time.Since(start))
}

func TestS_PORTAL_31_ExitAdminClearsCookie(t *testing.T) {
	e, adm := portalAdminEnv(t)
	r := e.portal("DELETE", "/api/admin/session", "", adm)
	must(t, r.Code == 200, "exit = %d", r.Code)
	cs := portalCookies(r)
	must(t, len(cs) == 1 && cs[0].Name == "detur_admin" && cs[0].MaxAge < 0, "clear cookie %+v", cs)
	var st struct{ Admin bool }
	e.portal("GET", "/api/admin/session", "", nil).JSON(t, &st)
	must(t, !st.Admin, "still admin")
	must(t, e.portal("POST", "/api/apps", `{"name":"x"}`, nil).Code == 403, "write allowed after exit")
}

func TestS_PORTAL_32_TamperedOrStaleCookieRejected(t *testing.T) {
	e, adm := portalAdminEnv(t)
	val := strings.TrimPrefix(adm["Cookie"], "detur_admin=")
	_, sig, _ := strings.Cut(val, ".")
	forged := map[string]string{"Cookie": "detur_admin=" + strconv.FormatInt(time.Now().Add(365*24*time.Hour).Unix(), 10) + "." + sig}
	must(t, e.portal("POST", "/api/apps", `{"name":"x"}`, forged).Code == 403, "forged expiry accepted")
	rotated := httptest.NewServer(api.RegisterPortal(e.St, t.TempDir(), nil, "", "new-password-after-rotation", 12, "https://"+linkDomain))
	defer rotated.Close()
	r := do(t, "POST", rotated.URL+"/api/apps", `{"name":"x"}`, map[string]string{"Cookie": adm["Cookie"], "Content-Type": "application/json"})
	must(t, r.Code == 403, "old-password session accepted after rotation: %d", r.Code)
}

func TestS_PORTAL_33_ForeignHostRejected(t *testing.T) {
	e := newEnv(t)
	e.app()
	for _, p := range []string{"/api/apps", "/"} {
		r := e.portal("GET", p, "", map[string]string{"Host": "attacker.example"})
		must(t, r.Code == 403, "foreign Host %s = %d", p, r.Code)
	}
	must(t, e.portal("GET", "/api/apps", "", map[string]string{"Host": "localhost:8081"}).Code == 200, "loopback Host rejected")
}

func TestS_PORTAL_34_CrossOriginWriteRejected(t *testing.T) {
	e := newEnv(t)
	for _, o := range []string{"https://evil.example", "http://localhost:3000", "null"} {
		r := e.portal("POST", "/api/apps", `{"name":"pwned"}`, map[string]string{"Origin": o})
		must(t, r.Code == 403, "Origin %s = %d", o, r.Code)
	}
	must(t, !strings.Contains(e.portal("GET", "/api/apps", "", nil).Body, "pwned"), "cross-origin app created")
	r := e.portal("POST", "/api/apps", `{"name":"ok"}`, map[string]string{"Origin": e.Portal.URL})
	must(t, r.Code == 201, "same-origin POST = %d", r.Code)
}

func TestS_PORTAL_35_SPAShellAndTraversal(t *testing.T) {
	e := newEnv(t)
	r := e.portal("GET", "/", "", nil)
	must(t, r.Code == 200 && strings.Contains(r.Body, "detur portal"), "/ = %d %s", r.Code, r.Body)
	for _, p := range []string{"/..%2f..%2f..%2f..%2f..%2fetc%2fpasswd", "/%2e%2e/%2e%2e/%2e%2e/%2e%2e/etc/passwd", "/....//....//etc/passwd"} {
		r := e.portal("GET", p, "", nil)
		must(t, !strings.Contains(r.Body, "root:"), "%s leaked /etc/passwd", p)
	}
}

func TestS_PORTAL_36_WebhooksNeedAdminPassword(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	must(t, e.portal("GET", "/api/apps/"+a.ID+"/webhooks", "", nil).Code == 403, "list open")
	must(t, e.portal("POST", "/api/apps/"+a.ID+"/webhooks", `{"url":"https://hooks.example/x","types":["installs"]}`, nil).Code == 403, "create open")
	e2 := newEnvWith(t, envOpts{AdminPassword: portalAdminPW, TrustProxy: true})
	a2 := e2.app()
	must(t, e2.portal("GET", "/api/apps/"+a2.ID+"/webhooks", "", nil).Code == 403, "viewer can list webhooks (secrets)")
}

func TestS_PORTAL_37_WebhookCRUD(t *testing.T) {
	e, adm := portalAdminEnv(t)
	a := e.app()
	w := portalCreateWebhook(t, e, adm, a.ID, "https://hooks.example/detur", `[" Installs ","installs","clicks"]`)
	must(t, len(w.Secret) >= 16 && w.Enabled && len(w.Types) == 2 && w.Types[0] == "installs", "created %+v", w)
	for _, b := range []string{`{"url":"https://hooks.example/x","types":[]}`, `{"url":"https://hooks.example/x","types":["purchases"]}`} {
		must(t, e.portal("POST", "/api/apps/"+a.ID+"/webhooks", b, adm).Code == 400, "accepted %s", b)
	}
	r := e.portal("PATCH", "/api/webhooks/"+w.ID, `{"types":["events"],"enabled":false}`, adm)
	must(t, r.Code == 200, "PATCH = %d %s", r.Code, r.Body)
	ws := portalWebhooks(t, e, adm, a.ID)
	must(t, len(ws) == 1 && !ws[0].Enabled && len(ws[0].Types) == 1 && ws[0].Types[0] == "events", "after patch %+v", ws)
	must(t, e.portal("DELETE", "/api/webhooks/"+w.ID, "", adm).Code == 204, "delete failed")
	must(t, len(portalWebhooks(t, e, adm, a.ID)) == 0, "still listed")
	must(t, e.portal("DELETE", "/api/webhooks/"+w.ID, "", adm).Code == 404, "second delete not 404")
}

func TestS_PORTAL_38_WebhookSSRFRejected(t *testing.T) {
	e, adm := portalAdminEnv(t)
	a := e.app()
	bad := []string{
		"http://hooks.example/x", "ftp://hooks.example/x", "https://169.254.169.254/latest/meta-data/", "https://10.0.0.5/",
		"https://192.168.1.1/", "https://172.16.0.1/", "https://100.100.100.200/", "https://192.0.0.192/", "https://[fd00::1]/",
		"https://[::ffff:169.254.169.254]/", "https://[64:ff9b::a9fe:a9fe]/", "https://[fe80::1]/", "https://0.0.0.0/",
	}
	for _, u := range bad {
		r := e.portal("POST", "/api/apps/"+a.ID+"/webhooks", `{"url":"`+u+`","types":["installs"]}`, adm)
		must(t, r.Code == 400, "create %s = %d", u, r.Code)
	}
	w := portalCreateWebhook(t, e, adm, a.ID, "https://hooks.example/ok", `["installs"]`)
	r := e.portal("PATCH", "/api/webhooks/"+w.ID, `{"url":"https://169.254.169.254/"}`, adm)
	must(t, r.Code == 400, "edit to metadata = %d", r.Code)
}

func TestS_PORTAL_39_WebhookDeliversSignedInstall(t *testing.T) {
	e, adm := portalAdminEnv(t)
	a := e.app()
	e.link(a, "promo", "/p")
	rc := newPortalReceiver(t, ok200)
	w := portalCreateWebhook(t, e, adm, a.ID, rc.URL+"/hook", `["installs"]`)
	portalInstall(t, e, a, "promo")
	portalDeliver(e)
	hits := rc.Hits()
	must(t, len(hits) == 1, "deliveries = %d", len(hits))
	h := hits[0]
	must(t, strings.HasPrefix(h.H.Get("Content-Type"), "application/json"), "content-type %q", h.H.Get("Content-Type"))
	must(t, portalVerify(h.H, h.Body, w.Secret), "signature %q does not verify", h.H.Get("Detur-Signature"))
	must(t, h.H.Get("Detur-Replay") == "", "live delivery marked replay")
	var batch []map[string]any
	must(t, json.Unmarshal(h.Body, &batch) == nil && len(batch) == 1, "body %s", h.Body)
	p := batch[0]
	must(t, p["type"] == "install" && p["linkKey"] == "promo" && p["attribution"] == "non_organic" && p["appId"] == a.ID, "payload %v", p)
	must(t, !strings.Contains(string(h.Body), clientIP) && !strings.Contains(strings.ToLower(string(h.Body)), "devicehash") &&
		!strings.Contains(string(h.Body), "device_hash"), "PII in payload %s", h.Body)
	portalDeliver(e)
	must(t, len(rc.Hits()) == 1, "redelivered after 2xx")
}

func TestS_PORTAL_40_WebhookClicksAndEvents(t *testing.T) {
	e, adm := portalAdminEnv(t)
	a := e.app()
	e.link(a, "promo", "/p")
	rc := newPortalReceiver(t, ok200)
	portalCreateWebhook(t, e, adm, a.ID, rc.URL, `["clicks","events"]`)
	e.mobileTap("/promo", uaAndroidChrome)
	e.sdk(a, "/api/analytics/event", `{"event_name":"purchase"}`)
	portalDeliver(e)
	var click, event map[string]any
	for _, h := range rc.Hits() {
		var batch []map[string]any
		_ = json.Unmarshal(h.Body, &batch)
		for _, p := range batch {
			switch p["type"] {
			case "click":
				click = p
				must(t, !strings.Contains(string(h.Body), clientIP) && !strings.Contains(string(h.Body), "Mozilla/"), "PII in click %s", h.Body)
			case "event":
				event = p
			}
		}
	}
	must(t, click != nil && click["linkKey"] == "promo" && click["platform"] == "android", "click %v", click)
	must(t, event != nil && event["event"] == "purchase", "event %v", event)
}

func TestS_PORTAL_41_WebhookRotateSecret(t *testing.T) {
	e, adm := portalAdminEnv(t)
	a := e.app()
	rc := newPortalReceiver(t, ok200)
	w := portalCreateWebhook(t, e, adm, a.ID, rc.URL, `["events"]`)
	r := e.portal("POST", "/api/webhooks/"+w.ID+"/rotate-secret", "", adm)
	must(t, r.Code == 200, "rotate = %d %s", r.Code, r.Body)
	var out struct{ Secret string }
	r.JSON(t, &out)
	must(t, out.Secret != "" && out.Secret != w.Secret, "secret %q", out.Secret)
	e.sdk(a, "/api/analytics/event", `{"event_name":"signup"}`)
	portalDeliver(e)
	hits := rc.Hits()
	must(t, len(hits) == 1, "deliveries %d", len(hits))
	must(t, portalVerify(hits[0].H, hits[0].Body, out.Secret), "new secret does not verify")
	must(t, !portalVerify(hits[0].H, hits[0].Body, w.Secret), "old secret still verifies")
}

func TestS_PORTAL_42_WebhookReceiverDownBacksOff(t *testing.T) {
	e, adm := portalAdminEnv(t)
	a := e.app()
	e.link(a, "promo", "/p")
	rc := newPortalReceiver(t, func([]byte) int { return 500 })
	portalCreateWebhook(t, e, adm, a.ID, rc.URL, `["installs"]`)
	portalInstall(t, e, a, "promo")
	portalDeliver(e)
	ws := portalWebhooks(t, e, adm, a.ID)
	must(t, len(rc.Hits()) == 1, "attempts %d", len(rc.Hits()))
	must(t, ws[0].FailsInstalls == 1 && ws[0].BackoffInstalls != nil, "after failure %+v", ws[0])
	bo, _ := time.Parse(time.RFC3339, *ws[0].BackoffInstalls)
	must(t, time.Until(bo) > 20*time.Second && time.Until(bo) <= time.Hour, "backoff until %v", bo)
	portalDeliver(e)
	must(t, len(rc.Hits()) == 1, "retried during backoff")
}

func TestS_PORTAL_43_FailingClicksDontBlockInstalls(t *testing.T) {
	e, adm := portalAdminEnv(t)
	a := e.app()
	e.link(a, "promo", "/p")
	rc := newPortalReceiver(t, func(b []byte) int {
		if strings.Contains(string(b), `"type":"click"`) {
			return 503
		}
		return 200
	})
	portalCreateWebhook(t, e, adm, a.ID, rc.URL, `["clicks","installs"]`)
	portalInstall(t, e, a, "promo")
	portalDeliver(e)
	gotInstall := false
	for _, h := range rc.Hits() {
		gotInstall = gotInstall || strings.Contains(string(h.Body), `"type":"install"`)
	}
	must(t, gotInstall, "install blocked by failing clicks")
	ws := portalWebhooks(t, e, adm, a.ID)
	must(t, ws[0].FailsClicks == 1 && ws[0].FailsInstalls == 0, "fails %+v", ws[0])
}

func TestS_PORTAL_44_WebhookReplay(t *testing.T) {
	e, adm := portalAdminEnv(t)
	a := e.app()
	e.link(a, "promo", "/p")
	rc := newPortalReceiver(t, ok200)
	w := portalCreateWebhook(t, e, adm, a.ID, rc.URL, `["installs"]`)
	portalInstall(t, e, a, "promo")
	portalDeliver(e)
	must(t, len(rc.Hits()) == 1, "initial deliveries %d", len(rc.Hits()))
	must(t, e.portal("POST", "/api/webhooks/"+w.ID+"/replay", `{"from":"yesterday"}`, adm).Code == 400, "bad from accepted")
	from := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	r := e.portal("POST", "/api/webhooks/"+w.ID+"/replay", `{"from":"`+from+`"}`, adm)
	must(t, r.Code == 200, "replay = %d %s", r.Code, r.Body)
	portalDeliver(e)
	hits := rc.Hits()
	must(t, len(hits) == 2, "deliveries after replay %d", len(hits))
	must(t, hits[1].H.Get("Detur-Replay") == "true" && portalVerify(hits[1].H, hits[1].Body, w.Secret), "replay headers %v", hits[1].H)
	must(t, string(hits[1].Body) == string(hits[0].Body), "replayed body differs:\n%s\n%s", hits[0].Body, hits[1].Body)
}

func TestS_PORTAL_45_DeleteAppRemovesWebhooks(t *testing.T) {
	e, adm := portalAdminEnv(t)
	a := e.app()
	rc := newPortalReceiver(t, ok200)
	w := portalCreateWebhook(t, e, adm, a.ID, rc.URL, `["events"]`)
	e.sdk(a, "/api/analytics/event", `{"event_name":"x"}`)
	must(t, e.portal("DELETE", "/api/apps/"+a.ID, "", adm).Code == 204, "delete app failed")
	must(t, e.portal("PATCH", "/api/webhooks/"+w.ID, `{"enabled":true}`, adm).Code == 404, "webhook survived app delete")
	portalDeliver(e)
	must(t, len(rc.Hits()) == 0, "delivered for deleted app")
}

func TestS_PORTAL_46_WebhookRedirectNotFollowed(t *testing.T) {
	e, adm := portalAdminEnv(t)
	a := e.app()
	inner := newPortalReceiver(t, ok200)
	outer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, inner.URL+"/internal", http.StatusTemporaryRedirect)
	}))
	defer outer.Close()
	portalCreateWebhook(t, e, adm, a.ID, outer.URL, `["events"]`)
	e.sdk(a, "/api/analytics/event", `{"event_name":"x"}`)
	portalDeliver(e)
	must(t, len(inner.Hits()) == 0, "redirect followed")
	ws := portalWebhooks(t, e, adm, a.ID)
	must(t, ws[0].CursorInstalls == 0, "cursor moved")
	r := e.portal("GET", "/api/apps/"+a.ID+"/webhooks", "", adm)
	must(t, strings.Contains(r.Body, `"failsEvents":1`), "3xx not counted as failure: %s", r.Body)
}

func TestS_PORTAL_47_FastInstallOnFraudPage(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "promo", "/p")
	portalInstall(t, e, a, "promo")
	var f struct {
		Signals  map[string]int64 `json:"signals"`
		Installs []struct {
			LinkKey     string   `json:"linkKey"`
			Attribution string   `json:"attribution"`
			Fraud       []string `json:"fraud"`
			FraudAction string   `json:"fraudAction"`
		} `json:"installs"`
	}
	e.portal("GET", "/api/apps/"+a.ID+"/fraud?days=7", "", nil).JSON(t, &f)
	must(t, f.Signals["timing"] == 1, "signals %v", f.Signals)
	must(t, len(f.Installs) == 1 && f.Installs[0].LinkKey == "promo" && f.Installs[0].Attribution == "non_organic" && f.Installs[0].FraudAction == "", "installs %+v", f.Installs)
}

func TestS_PORTAL_48_ActiveTimingExcludesInstall(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "promo", "/p")
	p := "/api/apps/" + a.ID + "/fraud/settings"
	var s map[string]any
	e.portal("GET", p, "", nil).JSON(t, &s)
	s["timingMode"] = "active"
	b, _ := json.Marshal(s)
	must(t, e.portal("PATCH", p, string(b), nil).Code == 200, "save failed")
	e.mobileTap("/promo", uaAndroidChrome)
	r := e.sdk(a, "/api/link/match-link", androidFingerprint(""))
	must(t, r.Code == 404, "hijacked install credited: %d %s", r.Code, r.Body)
	var f struct {
		Installs []struct {
			FraudAction  string `json:"fraudAction"`
			FraudLinkKey string `json:"fraudLinkKey"`
		} `json:"installs"`
	}
	e.portal("GET", "/api/apps/"+a.ID+"/fraud?days=7", "", nil).JSON(t, &f)
	must(t, len(f.Installs) == 1 && f.Installs[0].FraudAction == "excluded" && f.Installs[0].FraudLinkKey == "promo", "fraud %+v", f.Installs)
}

func TestS_PORTAL_49_SplitInstallsShowInVariants(t *testing.T) {
	e, adm := portalAdminEnv(t)
	a := e.app()
	l := e.link(a, "promo", "/p")
	base := "/api/apps/" + a.ID + "/links/" + l.ID
	r := e.portal("PUT", base+"/rules", `[{"name":"Store test","cond":{"platform":"android"},"action":{"split":[`+
		`{"name":"Old","weight":50,"targets":{}},{"name":"New","weight":50,"targets":{"destination":"/p?v=new"}}]}}]`, adm)
	must(t, r.Code == 200, "PUT = %d %s", r.Code, r.Body)
	portalInstall(t, e, a, "promo")
	var stats []struct {
		Variant        string  `json:"variant"`
		Clicks         int64   `json:"clicks"`
		Installs       int64   `json:"installs"`
		ConversionRate float64 `json:"conversion_rate"`
	}
	e.portal("GET", base+"/variants?days=7", "", nil).JSON(t, &stats)
	must(t, len(stats) == 1 && stats[0].Clicks == 1 && stats[0].Installs == 1 && stats[0].ConversionRate == 1, "stats %+v", stats)
}

func TestS_PORTAL_50_RuleChangesDeferredDestination(t *testing.T) {
	e, adm := portalAdminEnv(t)
	a := e.app()
	l := e.link(a, "promo", "/p")
	r := e.portal("PUT", "/api/apps/"+a.ID+"/links/"+l.ID+"/rules",
		`[{"name":"Android onboarding","cond":{"platform":"android"},"action":{"targets":{"destination":"/onboard/android"}}}]`, adm)
	must(t, r.Code == 200, "PUT = %d %s", r.Code, r.Body)
	e.mobileTap("/promo", uaAndroidChrome)
	r = e.sdk(a, "/api/link/match-link", androidFingerprint(""))
	must(t, r.Code == 200, "match = %d", r.Code)
	var out struct{ Link string }
	r.JSON(t, &out)
	must(t, strings.Contains(out.Link, "/onboard/android"), "deferred link %q", out.Link)
}

func TestS_PORTAL_51_WebhookInstallCarriesVariant(t *testing.T) {
	e, adm := portalAdminEnv(t)
	a := e.app()
	l := e.link(a, "promo", "/p")
	e.portal("PUT", "/api/apps/"+a.ID+"/links/"+l.ID+"/rules",
		`[{"name":"Android direct","cond":{"platform":"android"},"action":{"targets":{"destination":"/a"}}}]`, adm)
	rc := newPortalReceiver(t, ok200)
	portalCreateWebhook(t, e, adm, a.ID, rc.URL, `["installs"]`)
	portalInstall(t, e, a, "promo")
	portalDeliver(e)
	hits := rc.Hits()
	must(t, len(hits) == 1 && strings.Contains(string(hits[0].Body), `"variant":"Android direct"`), "payload %v", hits)
}

func TestS_PORTAL_52_AnalyticsBotsFallbacksPlatform(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "promo", "/p")
	e.tap("/promo", uaSlackBot)
	must(t, e.tap("/promo", uaDesktopChrome).Code == 302, "desktop tap")
	sum := func(q string) (clicks, web int64) {
		var an struct {
			Days []struct{ Clicks, WebFallbacks int64 } `json:"days"`
		}
		e.portal("GET", "/api/apps/"+a.ID+"/analytics?days=7"+q, "", nil).JSON(t, &an)
		for _, d := range an.Days {
			clicks += d.Clicks
			web += d.WebFallbacks
		}
		return
	}
	c, w := sum("")
	must(t, c == 1 && w == 1, "all: clicks %d web %d", c, w)
	c, _ = sum("&platform=desktop")
	must(t, c == 1, "desktop clicks %d", c)
	c, _ = sum("&platform=ios")
	must(t, c == 0, "ios clicks %d", c)
}

func TestS_PORTAL_53_InAppSourceInAnalytics(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "promo", "/p")
	e.mobileTap("/promo", uaMessengerIOS)
	var an struct {
		Sources []struct {
			Source string
			Count  int64
		} `json:"sources"`
	}
	e.portal("GET", "/api/apps/"+a.ID+"/analytics?days=7", "", nil).JSON(t, &an)
	must(t, len(an.Sources) == 1 && an.Sources[0].Source == "messenger" && an.Sources[0].Count == 1, "sources %+v", an.Sources)
}

func TestS_PORTAL_54_TaggedConversions(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "promo", "/p")
	r := e.portal("PATCH", "/api/apps/"+a.ID+"/tagging", `{"tagLinks":true}`, nil)
	must(t, r.Code == 200, "tagging = %d %s", r.Code, r.Body)
	e.mobileTap("/promo", uaAndroidChrome)
	r = e.sdk(a, "/api/link/match-link", androidFingerprint(""))
	var out struct{ Link string }
	r.JSON(t, &out)
	must(t, strings.Contains(out.Link, "detur_link=promo"), "deferred link %q", out.Link)
	e.sdk(a, "/api/analytics/event", `{"event_name":"purchase","device_id":"dev-1","data":{"link":"promo"}}`)
	var an struct {
		Conversions []struct {
			Key, Event string
			Count      int64
		} `json:"conversions"`
	}
	e.portal("GET", "/api/apps/"+a.ID+"/analytics?days=7", "", nil).JSON(t, &an)
	must(t, len(an.Conversions) == 1 && an.Conversions[0].Key == "promo" && an.Conversions[0].Event == "purchase" && an.Conversions[0].Count == 1, "conversions %+v", an.Conversions)
}

func TestS_PORTAL_55_NewWebhookStartsAtTail(t *testing.T) {
	e, adm := portalAdminEnv(t)
	a := e.app()
	e.sdk(a, "/api/analytics/event", `{"event_name":"old"}`)
	rc := newPortalReceiver(t, ok200)
	portalCreateWebhook(t, e, adm, a.ID, rc.URL, `["events"]`)
	e.sdk(a, "/api/analytics/event", `{"event_name":"new"}`)
	portalDeliver(e)
	hits := rc.Hits()
	must(t, len(hits) == 1 && strings.Contains(string(hits[0].Body), `"new"`) && !strings.Contains(string(hits[0].Body), `"old"`), "hits %v", hits)
}

func TestS_PORTAL_56_WebhookBatchesOf100(t *testing.T) {
	e, adm := portalAdminEnv(t)
	a := e.app()
	rc := newPortalReceiver(t, ok200)
	w := portalCreateWebhook(t, e, adm, a.ID, rc.URL, `["events"]`)
	for i := range 150 {
		must(t, e.St.RecordEvent(a.ID, "e"+strconv.Itoa(i), "{}") == nil, "RecordEvent")
	}
	portalDeliver(e)
	ids := map[string]bool{}
	for _, h := range rc.Hits() {
		must(t, portalVerify(h.H, h.Body, w.Secret), "unsigned batch")
		var batch []struct{ ID string }
		_ = json.Unmarshal(h.Body, &batch)
		must(t, len(batch) <= 100, "batch of %d", len(batch))
		for _, p := range batch {
			must(t, !ids[p.ID], "duplicate %s", p.ID)
			ids[p.ID] = true
		}
	}
	must(t, len(ids) == 150, "delivered %d of 150", len(ids))
}

func TestS_PORTAL_57_AppDetailsTrimmedOnSave(t *testing.T) {
	e := newEnv(t)
	id, _ := portalCreateApp(t, e, "Shop", nil)
	fp := strings.TrimSuffix(strings.Repeat("1F:", 32), ":")
	r := e.portal("PATCH", "/api/apps/"+id, `{"iosAppId":" TEAM123456.com.shop.app\n","androidPackage":"com.shop.app ","androidCertFingerprint":"  `+fp+`\n","iosScheme":""}`, nil)
	must(t, r.Code == 200, "PATCH = %d %s", r.Code, r.Body)
	var a struct{ IOSAppID, AndroidPackage, AndroidCertFingerprint string }
	r.JSON(t, &a)
	must(t, a.IOSAppID == "TEAM123456.com.shop.app" && a.AndroidPackage == "com.shop.app" && a.AndroidCertFingerprint == fp, "saved %q %q %q", a.IOSAppID, a.AndroidPackage, a.AndroidCertFingerprint)
	e.linkWith(store.Link{AppID: id, Key: "promo", URL: "/p"})
	aasa := do(t, "GET", e.Pub.URL+"/.well-known/apple-app-site-association", "", map[string]string{"Host": linkDomain})
	must(t, strings.Contains(aasa.Body, `"TEAM123456.com.shop.app"`), "AASA %s", aasa.Body)
}

func TestS_PORTAL_58_AdminCookieSecureBehindHTTPS(t *testing.T) {
	e := newEnvWith(t, envOpts{AdminPassword: portalAdminPW, TrustProxy: true})
	r := e.portal("POST", "/api/admin/session", `{"password":"`+portalAdminPW+`"}`, map[string]string{"X-Forwarded-Proto": "https"})
	must(t, r.Code == 200, "elevate = %d", r.Code)
	cs := portalCookies(r)
	must(t, len(cs) == 1 && cs[0].Secure, "cookie over https not Secure: %v", r.H["Set-Cookie"])
}
