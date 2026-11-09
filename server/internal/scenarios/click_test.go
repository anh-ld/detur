package scenarios

import (
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"detur.dev/server/internal/store"
)

const (
	clickAppStore = "https://apps.apple.com/app/id1234567890"
	clickPlay     = "https://play.google.com/store/apps/details?id=com.example.app"
	clickFallback = "https://example.com/download"
	clickPPID     = "8b1c2f2e-1d3a-4c5b-9e6f-0a1b2c3d4e5f"
	// Instagram on a real Pixel writes manufacturer/brand as "Google/google" (Dub's UA false-positive sample).
	clickUAInstagramPixel = "Mozilla/5.0 (Linux; Android 14; Pixel 7 Build/UP1A.231005.007; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/125.0.0.0 Mobile Safari/537.36 Instagram 334.0.0.42.95 Android (34/14; 420dpi; 1080x2400; Google/google; Pixel 7; panther; panther; en_US; 612345678)"
)

func clickList(e *env, a store.App) []store.Click {
	e.t.Helper()
	cs, err := e.St.ClicksSince(a.ID, "", time.Now().Add(-time.Hour))
	if err != nil {
		e.t.Fatalf("ClicksSince: %v", err)
	}
	return cs
}

func clickOne(e *env, a store.App) store.Click {
	e.t.Helper()
	cs := clickList(e, a)
	if len(cs) != 1 {
		e.t.Fatalf("clicks = %d, want 1: %+v", len(cs), cs)
	}
	return cs[0]
}

func clickNone(e *env, a store.App) {
	e.t.Helper()
	if cs := clickList(e, a); len(cs) != 0 {
		e.t.Fatalf("clicks = %d, want 0: %+v", len(cs), cs)
	}
}

func clickQuery(t *testing.T, loc string) url.Values {
	t.Helper()
	u, err := url.Parse(loc)
	if err != nil {
		t.Fatalf("bad Location %q: %v", loc, err)
	}
	return u.Query()
}

func clickAttr(t *testing.T, body, attr string) string {
	t.Helper()
	m := regexp.MustCompile(`<a [^>]*id="detur-cta"[^>]*\s` + attr + `="([^"]*)"`).FindStringSubmatch(body)
	if m == nil {
		return ""
	}
	return html.UnescapeString(m[1])
}

func clickMobileTapH(e *env, path, ua string, hdr map[string]string) resp {
	e.t.Helper()
	if r := e.tapH(path, ua, hdr); r.Code != http.StatusOK {
		e.t.Fatalf("hop 1 %s = %d, want 200 interstitial (Location %q)", path, r.Code, r.Loc())
	}
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	return e.tapH(path+sep+"_dt=1&screen=393x852%403&tz=Asia%2FHo_Chi_Minh", ua, hdr)
}

func clickOGLink(e *env, a store.App, key string) store.Link {
	e.t.Helper()
	return e.linkWith(store.Link{AppID: a.ID, Key: key, URL: "/promo/summer",
		IOS: clickAppStore, Android: clickPlay, FallbackURL: clickFallback,
		OGTitle: "Summer sale", OGDescription: "50% off everything", OGImage: "https://cdn.example.com/summer.png"})
}

func clickHasPrefix(t *testing.T, r resp, code int, prefix string) {
	t.Helper()
	if r.Code != code || !strings.HasPrefix(r.Loc(), prefix) {
		t.Fatalf("got %d Location %q, want %d to %s…\n%s", r.Code, r.Loc(), code, prefix, r.Body)
	}
}

func TestS_CLICK_01_IOSSafariCopyPageThenAppStore(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "summer", "/promo/summer")
	r := e.tap("/summer", uaIOSSafari)
	must(t, r.Code == 200 && strings.Contains(r.Body, "Open in App Store"), "hop 1 = %d, want copy page:\n%s", r.Code, r.Body)
	r = e.mobileTap("/summer", uaIOSSafari)
	clickHasPrefix(t, r, 302, clickAppStore)
	c := clickOne(e, a)
	must(t, c.Fingerprint.Screen == "393x852@3" && c.Fingerprint.Timezone == "Asia/Ho_Chi_Minh", "fingerprint = %+v", c.Fingerprint)
	must(t, c.Source == "", "Safari source = %q, want none", c.Source)
}

func TestS_CLICK_02_IOSCopyTapRecordsPastedLink(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "copyme", "/p")
	pasted := url.QueryEscape(e.Pub.URL + "/copyme?_dc=AbCdEfGh12345678")
	r := e.tap("/copyme?_dt=1&screen=393x852%403&tz=Asia%2FHo_Chi_Minh&pasted_link="+pasted, uaIOSSafari)
	clickHasPrefix(t, r, 302, clickAppStore)
	must(t, !strings.Contains(r.Loc(), "pasted_link") && !strings.Contains(r.Loc(), "_dc"), "Location leaks paste: %q", r.Loc())
	c := clickOne(e, a)
	must(t, c.Fingerprint.PastedLink != "", "pasted link not recorded")
}

func TestS_CLICK_03_IOSChromeAppStore(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "crios", "/p")
	clickHasPrefix(t, e.mobileTap("/crios", uaIOSChrome), 302, clickAppStore)
	must(t, clickOne(e, a).Source == "", "iOS Chrome tagged as in-app")
}

func TestS_CLICK_04_IPadOSTouchGoesToAppStore(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "ipad", "/p")
	r := e.tap("/ipad", uaIPadOS)
	must(t, r.Code == 200, "hop 1 = %d, want interstitial (iPad unknown yet)", r.Code)
	r = e.tap("/ipad?_dt=1&screen=820x1180%402&tz=Asia%2FHo_Chi_Minh&_touch=1", uaIPadOS)
	clickHasPrefix(t, r, 302, clickAppStore)
	c := clickOne(e, a)
	must(t, strings.Contains(c.Fingerprint.UserAgent, "iPad"), "click UA = %q, want rewritten to iPad", c.Fingerprint.UserAgent)
}

func TestS_CLICK_05_MacSafariGetsWebFallback(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "mac", "/p")
	r := e.mobileTap("/mac", uaIPadOS)
	clickHasPrefix(t, r, 302, clickFallback)
}

func TestS_CLICK_06_AndroidChromePlayReferrerClickID(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "andr", "/p")
	r := e.mobileTap("/andr", uaAndroidChrome)
	clickHasPrefix(t, r, 302, "https://play.google.com/store/apps/details")
	ref, _ := url.ParseQuery(clickQuery(t, r.Loc()).Get("referrer"))
	c := clickOne(e, a)
	must(t, ref.Get("click_id") != "" && ref.Get("click_id") == c.ClickID, "referrer click_id = %q, click = %q", ref.Get("click_id"), c.ClickID)
	must(t, clickQuery(t, r.Loc()).Get("id") == "com.example.app", "Play id changed: %q", r.Loc())
}

func TestS_CLICK_07_SamsungInternetPlayStore(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "sam", "/p")
	r := e.mobileTap("/sam", uaSamsung)
	clickHasPrefix(t, r, 302, "https://play.google.com/")
	must(t, strings.Contains(r.Loc(), "click_id"), "no click_id: %q", r.Loc())
	must(t, clickOne(e, a).Source == "", "Samsung Internet tagged in-app")
}

func TestS_CLICK_08_DesktopChromeWebFallback(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "desk", "/p")
	r := e.tap("/desk", uaDesktopChrome)
	must(t, r.Code == 302 && r.Loc() == clickFallback, "got %d %q, want 302 %s", r.Code, r.Loc(), clickFallback)
	must(t, clickOne(e, a).Kind == store.KindWeb, "kind != web")
}

func TestS_CLICK_09_MessengerIOSTapPage(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "msg", "/p")
	r := e.tap("/msg", uaMessengerIOS)
	must(t, r.Code == 200 && !strings.Contains(r.Body, "Open in App Store"), "hop 1 = %d, want auto reload (no copy button)", r.Code)
	r = e.mobileTap("/msg", uaMessengerIOS)
	must(t, r.Code == 200 && strings.Contains(r.Body, "Nothing happens?"), "hop 2 = %d %q, want tap page", r.Code, r.Loc())
	must(t, strings.HasPrefix(clickAttr(t, r.Body, "href"), clickAppStore), "button href = %q", clickAttr(t, r.Body, "href"))
	must(t, clickOne(e, a).Source == "messenger", "source != messenger")
}

func TestS_CLICK_10_FacebookIOSTapPage(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "fb", "/p")
	r := e.mobileTap("/fb", uaFacebookIOS)
	must(t, r.Code == 200 && strings.Contains(r.Body, "Nothing happens?"), "hop 2 = %d, want tap page", r.Code)
	must(t, clickOne(e, a).Source == "facebook", "source != facebook")
}

func TestS_CLICK_11_InstagramAndroidIntentButton(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "ig", "/p")
	r := clickMobileTapH(e, "/ig?utm_source=instagram", clickUAInstagramPixel, map[string]string{"Host": linkDomain})
	must(t, r.Code == 200, "hop 2 = %d, want tap page", r.Code)
	href := clickAttr(t, r.Body, "href")
	must(t, strings.HasPrefix(href, "intent://"+linkDomain+"/ig?utm_source=instagram#Intent;scheme=https;package=com.example.app;"), "intent = %q", href)
	_, fb, _ := strings.Cut(href, "S.browser_fallback_url=")
	fb, _ = url.QueryUnescape(strings.TrimSuffix(fb, ";end"))
	c := clickOne(e, a)
	must(t, strings.HasPrefix(fb, "https://play.google.com/") && strings.Contains(fb, c.ClickID), "fallback = %q, want Play with click %s", fb, c.ClickID)
	must(t, strings.Contains(r.Body, "Open Play Store") && c.Source == "instagram", "missing Play link or source %q", c.Source)
}

func TestS_CLICK_12_TikTokIOSTapPage(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "tt", "/p")
	r := e.mobileTap("/tt", uaTikTokIOS)
	must(t, r.Code == 200 && strings.Contains(r.Body, "Nothing happens?"), "hop 2 = %d, want tap page", r.Code)
	must(t, clickOne(e, a).Source == "tiktok", "source != tiktok")
}

func TestS_CLICK_13_ZaloAndroidTapPage(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "zl", "/p")
	r := e.mobileTap("/zl", uaZaloAndroid)
	must(t, r.Code == 200 && strings.HasPrefix(clickAttr(t, r.Body, "href"), "intent://"), "hop 2 = %d, want tap page with intent", r.Code)
	must(t, clickOne(e, a).Source == "zalo", "source != zalo")
}

func TestS_CLICK_14_LineIOSTapPage(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "line", "/p")
	r := e.mobileTap("/line", uaLineIOS)
	must(t, r.Code == 200 && strings.Contains(r.Body, "Nothing happens?"), "hop 2 = %d, want tap page", r.Code)
	must(t, clickOne(e, a).Source == "line", "source != line")
}

func TestS_CLICK_15_WeChatIOSTapPage(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "wx", "/p")
	r := e.mobileTap("/wx", uaWeChatIOS)
	must(t, r.Code == 200 && strings.Contains(r.Body, "Nothing happens?"), "hop 2 = %d, want tap page", r.Code)
	must(t, clickOne(e, a).Source == "wechat", "source != wechat")
}

func TestS_CLICK_16_GenericAndroidWebViewRedirects(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "wv", "/p")
	clickHasPrefix(t, e.mobileTap("/wv", uaAndroidWebView), 302, "https://play.google.com/")
	must(t, clickOne(e, a).Source == "unknown-inapp", "source != unknown-inapp")
}

func TestS_CLICK_17_SafetyNetReloadServesTapPage(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "net", "/p")
	e.mobileTap("/net", uaAndroidWebView)
	r := e.tap("/net?_dt=1&screen=393x852%403&tz=Asia%2FHo_Chi_Minh&_tap=1", uaAndroidWebView)
	must(t, r.Code == 200 && strings.Contains(r.Body, "Nothing happens?"), "safety net = %d %q, want tap page", r.Code, r.Loc())
	clickOne(e, a)
}

func TestS_CLICK_18_TapPageIOSSchemeOpensApp(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	if err := e.St.UpdateAppDetails(a.ID, a.IOSAppID, a.AndroidPackage, a.AndroidCertFingerprint, "myapp"); err != nil {
		t.Fatal(err)
	}
	e.link(a, "sch", "/product/42")
	r := e.mobileTap("/sch?ref=friend", uaMessengerIOS)
	open := clickAttr(t, r.Body, "data-open")
	must(t, open == "myapp://product/42?ref=friend", "data-open = %q, want myapp://product/42?ref=friend", open)
	must(t, strings.HasPrefix(clickAttr(t, r.Body, "href"), clickAppStore), "href must stay App Store")
}

func TestS_CLICK_19_FacebookBotGetsOGPage(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	clickOGLink(e, a, "og")
	r := e.tapH("/og?utm_source=fb&_dt=1", uaFacebookBot, map[string]string{"Host": linkDomain})
	must(t, r.Code == 200, "code = %d, want OG page", r.Code)
	for _, s := range []string{`property="og:title" content="Summer sale"`, `property="og:url" content="https://lnk.example/og?utm_source=fb"`, `og:image`} {
		must(t, strings.Contains(r.Body, s), "OG page missing %s:\n%s", s, r.Body)
	}
	clickNone(e, a)
}

func TestS_CLICK_20_TwitterBotLargeImageCard(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	clickOGLink(e, a, "tw")
	r := e.tap("/tw", uaTwitterBot)
	must(t, r.Code == 200 && strings.Contains(r.Body, `name="twitter:card" content="summary_large_image"`), "got %d:\n%s", r.Code, r.Body)
	clickNone(e, a)
}

func TestS_CLICK_21_WhatsAppPreviewNoClick(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	clickOGLink(e, a, "wa")
	r := e.tap("/wa", uaWhatsApp)
	must(t, r.Code == 200 && strings.Contains(r.Body, "og:description"), "got %d, want OG page", r.Code)
	clickNone(e, a)
}

func TestS_CLICK_22_TelegramPreviewNoClick(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	clickOGLink(e, a, "tg")
	r := e.tap("/tg", uaTelegramBot)
	must(t, r.Code == 200 && strings.Contains(r.Body, "og:title"), "got %d, want OG page", r.Code)
	clickNone(e, a)
}

func TestS_CLICK_23_IMessagePreviewOG(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	clickOGLink(e, a, "imsg")
	r := e.tap("/imsg", uaIMessage)
	must(t, r.Code == 200 && strings.Contains(r.Body, "og:title") && !strings.Contains(r.Body, "_dt"), "got %d, want OG page:\n%s", r.Code, r.Body)
	clickNone(e, a)
}

func TestS_CLICK_24_SlackWithoutOGFollowsRedirect(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "slk", "/p")
	r := e.tap("/slk", uaSlackBot)
	must(t, r.Code == 302 && r.Loc() == clickFallback, "got %d %q", r.Code, r.Loc())
	clickNone(e, a)
}

func TestS_CLICK_25_DiscordUnfurlNoClick(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "dc", "/p")
	r := e.tap("/dc", uaDiscordBot)
	must(t, r.Code == 302, "got %d, want 302", r.Code)
	clickNone(e, a)
}

func TestS_CLICK_26_GooglebotNoindexNoClick(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "gb", "/p")
	r := e.tap("/gb", uaGoogleBot)
	must(t, strings.Contains(r.H.Get("X-Robots-Tag"), "noindex"), "X-Robots-Tag = %q", r.H.Get("X-Robots-Tag"))
	clickNone(e, a)
}

func TestS_CLICK_27_CurlRedirectNoClick(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "cu", "/p")
	r := e.tap("/cu", uaCurl)
	must(t, r.Code == 302 && r.Loc() == clickFallback, "got %d %q", r.Code, r.Loc())
	clickNone(e, a)
}

func TestS_CLICK_28_HeadRequestNoClick(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "hd", "/p")
	r := do(t, "HEAD", e.Pub.URL+"/hd", "", map[string]string{"User-Agent": uaDesktopChrome, "X-Forwarded-For": clientIP})
	must(t, r.Code == 302 && r.Loc() == clickFallback, "HEAD got %d %q", r.Code, r.Loc())
	clickNone(e, a)
}

func TestS_CLICK_29_DesktopQRPageForAppOnlyLink(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.linkWith(store.Link{AppID: a.ID, Key: "qr", URL: "/p", IOS: clickAppStore, Android: clickPlay})
	r := e.tapH("/qr?utm_source=poster&_dt=1", uaDesktopMac, map[string]string{"Host": linkDomain})
	must(t, r.Code == 200, "got %d %q, want QR page", r.Code, r.Loc())
	for _, s := range []string{"data:image/png;base64,", ">https://lnk.example/qr?utm_source=poster<", clickAppStore, "Google Play"} {
		must(t, strings.Contains(r.Body, s), "QR page missing %q:\n%s", s, r.Body)
	}
}

func TestS_CLICK_30_UnknownKey404(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "real", "/p")
	for _, ua := range []string{uaIOSSafari, uaFacebookBot} {
		r := e.tap("/nope", ua)
		must(t, r.Code == 404, "%s: got %d, want 404", ua, r.Code)
	}
	clickNone(e, a)
}

func TestS_CLICK_31_KeyCaseInsensitive(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "promo", "/p")
	r := e.tap("/PROMO", uaDesktopChrome)
	must(t, r.Code == 302 && r.Loc() == clickFallback, "got %d %q", r.Code, r.Loc())
}

func TestS_CLICK_32_TrailingSlashResolves(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "promo", "/p")
	r := e.tap("/promo/", uaDesktopChrome)
	ok := r.Code >= 300 && r.Code < 400 && (r.Loc() == clickFallback || strings.HasSuffix(strings.SplitN(r.Loc(), "?", 2)[0], "/promo"))
	must(t, ok, "got %d %q, want redirect to fallback or /promo", r.Code, r.Loc())
}

func TestS_CLICK_33_PercentEncodedKey(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "promo", "/p")
	r := e.tap("/%70romo", uaDesktopChrome)
	must(t, r.Code == 302 && r.Loc() == clickFallback, "got %d %q", r.Code, r.Loc())
}

func TestS_CLICK_34_PercentEncodedQueryForwarded(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "enc", "/p")
	r := e.tap("/enc?utm_campaign=summer%20sale%26more&utm_term=%E1%BB%87", uaDesktopChrome)
	q := clickQuery(t, r.Loc())
	must(t, q.Get("utm_campaign") == "summer sale&more" && q.Get("utm_term") == "ệ", "forwarded = %v", q)
}

func TestS_CLICK_35_VeryLongQueryForwarded(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "long", "/p")
	blob := strings.Repeat("Ab0_-", 800)
	r := e.tap("/long?fbclid="+blob, uaDesktopChrome)
	must(t, r.Code == 302 && clickQuery(t, r.Loc()).Get("fbclid") == blob, "got %d, fbclid lost", r.Code)
	clickOne(e, a)
}

func TestS_CLICK_36_DuplicateQueryKeysForwardOnce(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "dup", "/p")
	r := e.tap("/dup?utm_source=a&utm_source=b", uaDesktopChrome)
	vs := clickQuery(t, r.Loc())["utm_source"]
	must(t, r.Code == 302 && len(vs) == 1, "got %d utm_source = %v", r.Code, vs)
}

func TestS_CLICK_37_InternalParamsNeverLeakToFallback(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "leak", "/p")
	r := e.tap("/leak?ref=friend&_dt=1&_tap=1&_dc=AbCdEfGh12345678&screen=1x1%401&tz=UTC&pasted_link=x&_touch=1", uaDesktopChrome)
	q := clickQuery(t, r.Loc())
	must(t, q.Get("ref") == "friend", "ref not forwarded: %q", r.Loc())
	for _, k := range []string{"_dt", "_tap", "_dc", "screen", "tz", "pasted_link", "_touch"} {
		must(t, !q.Has(k), "%s leaked: %q", k, r.Loc())
	}
}

func TestS_CLICK_38_InternalParamsNeverLeakIntoIntent(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "lki", "/p")
	r := clickMobileTapH(e, "/lki?ref=friend", uaMessengerAndroid, map[string]string{"Host": linkDomain})
	short, _, _ := strings.Cut(strings.TrimPrefix(clickAttr(t, r.Body, "href"), "intent://"), "#Intent")
	q := clickQuery(t, "https://"+short)
	must(t, q.Get("ref") == "friend", "intent = %q", short)
	for _, k := range []string{"_dt", "screen", "tz", "_tap"} {
		must(t, !q.Has(k), "%s leaked into intent %q", k, short)
	}
}

func TestS_CLICK_39_AppStoreOnlyGetsStoreParams(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "asa", "/p")
	r := e.mobileTap("/asa?pt=118&ct=spring&mt=8&ppid="+clickPPID+"&utm_source=fb", uaIOSSafari)
	q := clickQuery(t, r.Loc())
	must(t, q.Get("pt") == "118" && q.Get("ct") == "spring" && q.Get("mt") == "8" && q.Get("ppid") == clickPPID, "store params = %v", q)
	must(t, !q.Has("utm_source"), "utm leaked to App Store: %q", r.Loc())
	r = e.tapH("/asa?_dt=1&ppid=not-a-uuid", uaIOSSafari, map[string]string{"X-Forwarded-For": "198.51.100.9"})
	must(t, !clickQuery(t, r.Loc()).Has("ppid"), "bad ppid forwarded: %q", r.Loc())
}

func TestS_CLICK_40_PlayStoreUTMInsideReferrer(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "nl", "/p")
	r := e.mobileTap("/nl?utm_source=newsletter&utm_campaign=spring&id=evil.app&ref=x", uaAndroidChrome)
	q := clickQuery(t, r.Loc())
	ref, _ := url.ParseQuery(q.Get("referrer"))
	must(t, q.Get("id") == "com.example.app" && !q.Has("ref") && !q.Has("utm_source"), "top-level query = %v", q)
	must(t, ref.Get("utm_source") == "newsletter" && ref.Get("utm_campaign") == "spring" && ref.Get("click_id") != "", "referrer = %v", ref)
}

func TestS_CLICK_41_FallbackKeepsOperatorParams(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.linkWith(store.Link{AppID: a.ID, Key: "op", URL: "/p", FallbackURL: "https://example.com/download?utm_source=site"})
	q := clickQuery(t, e.tap("/op?utm_source=ads&utm_medium=cpc", uaDesktopChrome).Loc())
	must(t, q.Get("utm_source") == "site" && q.Get("utm_medium") == "cpc", "fallback query = %v", q)
}

func TestS_CLICK_42_NoTrackQuerySkipsEverything(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "nt", "/p")
	r := e.tap("/nt?detur-no-track=1", uaIOSSafari)
	clickHasPrefix(t, r, 302, clickAppStore)
	clickNone(e, a)
}

func TestS_CLICK_43_NoTrackHeader(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "nth", "/p")
	r := e.tapH("/nth?detur-no-track=1&utm_source=qa", uaDesktopChrome, map[string]string{"detur-no-track": "1"})
	must(t, r.Code == 302 && !strings.Contains(r.Loc(), "detur-no-track") && clickQuery(t, r.Loc()).Get("utm_source") == "qa", "got %d %q", r.Code, r.Loc())
	clickNone(e, a)
}

func TestS_CLICK_44_ExpiredLinkRedirectsExpiredURL(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	past := time.Now().Add(-time.Hour)
	e.linkWith(store.Link{AppID: a.ID, Key: "old", URL: "/p", IOS: clickAppStore, FallbackURL: clickFallback,
		ExpiresAt: &past, ExpiredURL: "https://example.com/promo-ended"})
	r := e.tap("/old", uaIOSSafari)
	must(t, r.Code == 302 && r.Loc() == "https://example.com/promo-ended", "got %d %q", r.Code, r.Loc())
	r = e.tap("/old", uaDesktopChrome)
	must(t, r.Code == 302 && r.Loc() == "https://example.com/promo-ended", "desktop got %d %q", r.Code, r.Loc())
}

func TestS_CLICK_45_ExpiredLinkGone(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	past := time.Now().Add(-time.Minute)
	e.linkWith(store.Link{AppID: a.ID, Key: "gone", URL: "/p", FallbackURL: clickFallback, ExpiresAt: &past})
	must(t, e.tap("/gone", uaAndroidChrome).Code == 410, "want 410")
}

func TestS_CLICK_46_RulePlatformIOS(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	l := e.link(a, "rios", "/p")
	cpp := "https://apps.apple.com/app/id1234567890?ppid=" + clickPPID
	must(t, e.St.SetLinkRules(l.ID, []store.LinkRule{{Name: "ios-cpp", Cond: store.RuleCond{Platform: "ios"},
		Action: store.RuleAction{Targets: &store.RuleTargets{IOS: cpp}}}}) == nil, "SetLinkRules")
	clickHasPrefix(t, e.mobileTap("/rios", uaIOSSafari), 302, cpp)
	must(t, clickOne(e, a).Variant == "ios-cpp", "variant != ios-cpp")
	clickHasPrefix(t, e.tap("/rios", uaDesktopChrome), 302, clickFallback)
}

func TestS_CLICK_47_RuleLanguageGerman(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	l := e.link(a, "rde", "/p")
	must(t, e.St.SetLinkRules(l.ID, []store.LinkRule{{Name: "german", Cond: store.RuleCond{Lang: "de"},
		Action: store.RuleAction{Targets: &store.RuleTargets{FallbackURL: "https://example.com/de"}}}}) == nil, "SetLinkRules")
	r := e.tapH("/rde", uaDesktopChrome, map[string]string{"Accept-Language": "de-DE,de;q=0.9,en;q=0.8"})
	clickHasPrefix(t, r, 302, "https://example.com/de")
	clickHasPrefix(t, e.tapH("/rde", uaFirefox, map[string]string{"Accept-Language": "en-US"}), 302, clickFallback)
}

func TestS_CLICK_48_RuleQueryParam(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	l := e.link(a, "rp", "/p")
	must(t, e.St.SetLinkRules(l.ID, []store.LinkRule{{Name: "tiktok", Cond: store.RuleCond{ParamKey: "ref", ParamVal: "tiktok"},
		Action: store.RuleAction{Targets: &store.RuleTargets{FallbackURL: "https://example.com/tiktok"}}}}) == nil, "SetLinkRules")
	clickHasPrefix(t, e.tap("/rp?ref=tiktok", uaDesktopChrome), 302, "https://example.com/tiktok")
}

func TestS_CLICK_49_ABSplitSticky(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	l := e.link(a, "ab", "/p")
	must(t, e.St.SetLinkRules(l.ID, []store.LinkRule{{Name: "split", Action: store.RuleAction{Split: []store.SplitVariant{
		{Name: "a", Weight: 50, Targets: store.RuleTargets{FallbackURL: "https://example.com/a"}},
		{Name: "b", Weight: 50, Targets: store.RuleTargets{FallbackURL: "https://example.com/b"}},
	}}}}) == nil, "SetLinkRules")
	r1, r2 := e.tap("/ab", uaDesktopChrome), e.tap("/ab", uaDesktopChrome)
	must(t, r1.Loc() == r2.Loc(), "not sticky: %q vs %q", r1.Loc(), r2.Loc())
	v := clickOne(e, a).Variant
	must(t, (v == "a" || v == "b") && r1.Loc() == "https://example.com/"+v, "variant %q Location %q", v, r1.Loc())
}

func TestS_CLICK_50_IPadMatchesIOSRule(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	l := e.link(a, "ripad", "/p")
	must(t, e.St.SetLinkRules(l.ID, []store.LinkRule{{Name: "ios", Cond: store.RuleCond{Platform: "ios"},
		Action: store.RuleAction{Targets: &store.RuleTargets{IOS: "https://apps.apple.com/app/id555"}}}}) == nil, "SetLinkRules")
	r := e.tap("/ripad?_dt=1&screen=820x1180%402&_touch=1", uaIPadOS)
	clickHasPrefix(t, r, 302, "https://apps.apple.com/app/id555")
}

func TestS_CLICK_51_DoubleTapDedup(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "dd", "/p")
	r1 := e.mobileTap("/dd", uaAndroidChrome)
	r2 := e.mobileTap("/dd", uaAndroidChrome)
	ref1, _ := url.ParseQuery(clickQuery(t, r1.Loc()).Get("referrer"))
	ref2, _ := url.ParseQuery(clickQuery(t, r2.Loc()).Get("referrer"))
	must(t, ref1.Get("click_id") != "" && ref1.Get("click_id") == ref2.Get("click_id"), "click_id %q vs %q", ref1.Get("click_id"), ref2.Get("click_id"))
	clickOne(e, a)
}

func TestS_CLICK_52_DifferentVisitorsSeparateClicks(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "two", "/p")
	e.tap("/two", uaDesktopChrome)
	e.tapH("/two", uaDesktopChrome, map[string]string{"X-Forwarded-For": "198.51.100.20"})
	must(t, len(clickList(e, a)) == 2, "clicks = %d, want 2", len(clickList(e, a)))
}

func TestS_CLICK_53_InAppToBrowserMerge(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "mg", "/p")
	e.mobileTap("/mg", clickUAInstagramPixel)
	first := clickOne(e, a)
	r := e.mobileTap("/mg", uaAndroidChrome)
	ref, _ := url.ParseQuery(clickQuery(t, r.Loc()).Get("referrer"))
	c := clickOne(e, a)
	must(t, ref.Get("click_id") == first.ClickID && c.Source == "instagram", "referrer click_id %q, first %q, source %q", ref.Get("click_id"), first.ClickID, c.Source)
}

func TestS_CLICK_54_NoMergeAcrossPlatforms(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "xp", "/p")
	e.mobileTap("/xp", uaInstagramIOS)
	e.mobileTap("/xp", uaAndroidChrome)
	must(t, len(clickList(e, a)) == 2, "clicks = %d, want 2", len(clickList(e, a)))
}

func TestS_CLICK_55_IPhoneWithoutIOSTargetGetsWebFallback(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.linkWith(store.Link{AppID: a.ID, Key: "andonly", URL: "/p", Android: clickPlay, FallbackURL: clickFallback})
	r := e.mobileTap("/andonly", uaIOSSafari)
	must(t, r.Code == 302 && r.Loc() == clickFallback, "got %d %q, want 302 %s (page offers Google Play to an iPhone: %v)",
		r.Code, r.Loc(), clickFallback, strings.Contains(r.Body, "Google Play"))
}

func TestS_CLICK_56_Hop1KeepsQueryThroughReload(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "h1", "/p")
	r := e.tap("/h1?utm_source=ig", uaAndroidChrome)
	must(t, r.Code == 200 && r.H.Get("Cache-Control") == "no-store", "hop 1 %d cache %q", r.Code, r.H.Get("Cache-Control"))
	must(t, r.H.Get("X-Frame-Options") == "DENY" && r.H.Get("X-Content-Type-Options") == "nosniff", "hop 1 frame %q sniff %q", r.H.Get("X-Frame-Options"), r.H.Get("X-Content-Type-Options"))
	must(t, strings.Contains(r.Body, "_dt=1") && strings.Contains(r.Body, "utm_source=ig"), "noscript reload lost params:\n%s", r.Body)
	must(t, strings.Contains(r.H.Get("Accept-CH"), "Sec-CH-UA-Model"), "Accept-CH = %q", r.H.Get("Accept-CH"))
	clickNone(e, a)
}

func TestS_CLICK_57_VisitorTagDroppedAndRowBounded(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "tagged", "/p")
	e.tap("/tagged?detur_link=other&utm_source=ig", uaDesktopChrome)
	c := clickOne(e, a)
	must(t, !strings.Contains(c.Destination, "detur_link") && strings.Contains(c.Destination, "utm_source=ig"), "destination = %q", c.Destination)

	e.link(a, "big", "/q")
	e.tap("/big?utm_source="+strings.Repeat("a", store.MaxClickURL), uaDesktopChrome+strings.Repeat("x", 20000))
	cs, err := e.St.ClicksSince(a.ID, "", time.Now().Add(-time.Hour))
	must(t, err == nil, "ClicksSince: %v", err)
	for _, c := range cs {
		must(t, len(c.Destination) <= store.MaxClickURL && len(c.Fingerprint.UserAgent) <= 1024, "row not bounded: dest %d, ua %d", len(c.Destination), len(c.Fingerprint.UserAgent))
	}
}
