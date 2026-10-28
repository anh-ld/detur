package scenarios

// CLICK: people tapping or opening short links (GET /{key}) on the public listener.

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

// clickList: recorded (non-bot, unmatched) clicks of the app in the last hour.
func clickList(e *env, a store.App) []store.Click {
	e.t.Helper()
	cs, err := e.St.ClicksSince(a.ID, time.Now().Add(-time.Hour))
	if err != nil {
		e.t.Fatalf("ClicksSince: %v", err)
	}
	return cs
}

// clickOne: exactly one recorded click.
func clickOne(e *env, a store.App) store.Click {
	e.t.Helper()
	cs := clickList(e, a)
	if len(cs) != 1 {
		e.t.Fatalf("clicks = %d, want 1: %+v", len(cs), cs)
	}
	return cs[0]
}

// clickNone: no click recorded.
func clickNone(e *env, a store.App) {
	e.t.Helper()
	if cs := clickList(e, a); len(cs) != 0 {
		e.t.Fatalf("clicks = %d, want 0: %+v", len(cs), cs)
	}
}

// clickQuery: parsed query of a redirect Location.
func clickQuery(t *testing.T, loc string) url.Values {
	t.Helper()
	u, err := url.Parse(loc)
	if err != nil {
		t.Fatalf("bad Location %q: %v", loc, err)
	}
	return u.Query()
}

// clickAttr: unescaped attribute value on the tap page's primary button (id="go").
func clickAttr(t *testing.T, body, attr string) string {
	t.Helper()
	m := regexp.MustCompile(`<a [^>]*id="go"[^>]*\s` + attr + `="([^"]*)"`).FindStringSubmatch(body)
	if m == nil {
		return ""
	}
	return html.UnescapeString(m[1])
}

// clickMobileTapH: the phone's two hops with extra headers (Host etc.).
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

// clickOGLink: link with a social preview set.
func clickOGLink(e *env, a store.App, key string) store.Link {
	e.t.Helper()
	return e.linkWith(store.Link{AppID: a.ID, Key: key, URL: "/promo/summer",
		IOS: clickAppStore, Android: clickPlay, FallbackURL: clickFallback,
		OGTitle: "Summer sale", OGDescription: "50% off everything", OGImage: "https://cdn.example.com/summer.png"})
}

// clickIsStore: Location is the App Store / Play Store target.
func clickHasPrefix(t *testing.T, r resp, code int, prefix string) {
	t.Helper()
	if r.Code != code || !strings.HasPrefix(r.Loc(), prefix) {
		t.Fatalf("got %d Location %q, want %d to %s…\n%s", r.Code, r.Loc(), code, prefix, r.Body)
	}
}

// Real world: an iPhone user taps a campaign link in Safari. Expect: hop 1 tap-to-copy page, hop 2 302 to the App Store, click recorded with screen + timezone.
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

// Real world: an iPhone user taps "Open in App Store" on the copy page; the page copies the short link and reloads. Expect: App Store 302 without pasted_link, click keeps the pasted link.
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

// Real world: an iPhone user with Chrome as browser taps a link. Expect: App Store 302, not tagged as an in-app browser.
func TestS_CLICK_03_IOSChromeAppStore(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "crios", "/p")
	clickHasPrefix(t, e.mobileTap("/crios", uaIOSChrome), 302, clickAppStore)
	must(t, clickOne(e, a).Source == "", "iOS Chrome tagged as in-app")
}

// Real world: an iPad user (Safari sends a Mac UA) taps a link; the interstitial sees a touch screen. Expect: App Store, click platform iPad/iOS.
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

// Real world: a Mac Safari user (no touch screen) opens a link. Expect: one reload, then the web fallback.
func TestS_CLICK_05_MacSafariGetsWebFallback(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "mac", "/p")
	r := e.mobileTap("/mac", uaIPadOS)
	clickHasPrefix(t, r, 302, clickFallback)
}

// Real world: an Android user taps a link in Chrome. Expect: Play Store 302, referrer carries the recorded click's id (deterministic match).
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

// Real world: a Galaxy user taps a link in Samsung Internet. Expect: treated as a real Android browser, Play Store 302 with click_id.
func TestS_CLICK_07_SamsungInternetPlayStore(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "sam", "/p")
	r := e.mobileTap("/sam", uaSamsung)
	clickHasPrefix(t, r, 302, "https://play.google.com/")
	must(t, strings.Contains(r.Loc(), "click_id"), "no click_id: %q", r.Loc())
	must(t, clickOne(e, a).Source == "", "Samsung Internet tagged in-app")
}

// Real world: someone opens the link on a Windows laptop in Chrome. Expect: straight 302 to the web fallback (no interstitial), web click recorded.
func TestS_CLICK_08_DesktopChromeWebFallback(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "desk", "/p")
	r := e.tap("/desk", uaDesktopChrome)
	must(t, r.Code == 302 && r.Loc() == clickFallback, "got %d %q, want 302 %s", r.Code, r.Loc(), clickFallback)
	must(t, clickOne(e, a).Kind == store.KindWeb, "kind != web")
}

// Real world: someone taps a link in Messenger on iPhone. Expect: hop 1 reloads without a copy button, hop 2 tap page whose button goes to the App Store, source messenger.
func TestS_CLICK_09_MessengerIOSTapPage(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "msg", "/p")
	r := e.tap("/msg", uaMessengerIOS)
	must(t, r.Code == 200 && !strings.Contains(r.Body, "Open in App Store"), "hop 1 = %d, want auto reload (no copy button)", r.Code)
	r = e.mobileTap("/msg", uaMessengerIOS)
	must(t, r.Code == 200 && strings.Contains(r.Body, "Continue to the app"), "hop 2 = %d %q, want tap page", r.Code, r.Loc())
	must(t, strings.HasPrefix(clickAttr(t, r.Body, "href"), clickAppStore), "button href = %q", clickAttr(t, r.Body, "href"))
	must(t, clickOne(e, a).Source == "messenger", "source != messenger")
}

// Real world: someone taps a link in the Facebook app on iPhone. Expect: tap page, source facebook.
func TestS_CLICK_10_FacebookIOSTapPage(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "fb", "/p")
	r := e.mobileTap("/fb", uaFacebookIOS)
	must(t, r.Code == 200 && strings.Contains(r.Body, "Continue to the app"), "hop 2 = %d, want tap page", r.Code)
	must(t, clickOne(e, a).Source == "facebook", "source != facebook")
}

// Real world: an Android user taps a link in Instagram. Expect: tap page with intent:// back to the short link (app package) and Play fallback carrying click_id.
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

// Real world: someone taps a TikTok bio link on iPhone. Expect: tap page, source tiktok.
func TestS_CLICK_12_TikTokIOSTapPage(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "tt", "/p")
	r := e.mobileTap("/tt", uaTikTokIOS)
	must(t, r.Code == 200 && strings.Contains(r.Body, "Continue to the app"), "hop 2 = %d, want tap page", r.Code)
	must(t, clickOne(e, a).Source == "tiktok", "source != tiktok")
}

// Real world: a Vietnamese user taps a link in a Zalo chat on Android. Expect: tap page with intent button, source zalo.
func TestS_CLICK_13_ZaloAndroidTapPage(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "zl", "/p")
	r := e.mobileTap("/zl", uaZaloAndroid)
	must(t, r.Code == 200 && strings.HasPrefix(clickAttr(t, r.Body, "href"), "intent://"), "hop 2 = %d, want tap page with intent", r.Code)
	must(t, clickOne(e, a).Source == "zalo", "source != zalo")
}

// Real world: someone taps a link in LINE on iPhone. Expect: tap page, source line.
func TestS_CLICK_14_LineIOSTapPage(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "line", "/p")
	r := e.mobileTap("/line", uaLineIOS)
	must(t, r.Code == 200 && strings.Contains(r.Body, "Continue to the app"), "hop 2 = %d, want tap page", r.Code)
	must(t, clickOne(e, a).Source == "line", "source != line")
}

// Real world: someone taps a link in WeChat on iPhone. Expect: tap page, source wechat.
func TestS_CLICK_15_WeChatIOSTapPage(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "wx", "/p")
	r := e.mobileTap("/wx", uaWeChatIOS)
	must(t, r.Code == 200 && strings.Contains(r.Body, "Continue to the app"), "hop 2 = %d, want tap page", r.Code)
	must(t, clickOne(e, a).Source == "wechat", "source != wechat")
}

// Real world: a link opens inside some unbranded Android app's webview. Expect: hop 2 302 to Play (handoff may work), click tagged unknown-inapp.
func TestS_CLICK_16_GenericAndroidWebViewRedirects(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "wv", "/p")
	clickHasPrefix(t, e.mobileTap("/wv", uaAndroidWebView), 302, "https://play.google.com/")
	must(t, clickOne(e, a).Source == "unknown-inapp", "source != unknown-inapp")
}

// Real world: the webview blocked the Play hand-off, so the safety net reloads with _tap. Expect: tap page, still one click.
func TestS_CLICK_17_SafetyNetReloadServesTapPage(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "net", "/p")
	e.mobileTap("/net", uaAndroidWebView)
	r := e.tap("/net?_dt=1&screen=393x852%403&tz=Asia%2FHo_Chi_Minh&_tap=1", uaAndroidWebView)
	must(t, r.Code == 200 && strings.Contains(r.Body, "Continue to the app"), "safety net = %d %q, want tap page", r.Code, r.Loc())
	clickOne(e, a)
}

// Real world: the app has an iOS URL scheme; an installed-app user taps a link in Messenger. Expect: tap button tries myapp://<route>?<query> first, no internal params.
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

// Real world: someone shares the link on Facebook; facebookexternalhit fetches it. Expect: OG page with title + canonical short URL, no click.
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

// Real world: a tweet with the link; Twitterbot builds the card. Expect: large image card.
func TestS_CLICK_20_TwitterBotLargeImageCard(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	clickOGLink(e, a, "tw")
	r := e.tap("/tw", uaTwitterBot)
	must(t, r.Code == 200 && strings.Contains(r.Body, `name="twitter:card" content="summary_large_image"`), "got %d:\n%s", r.Code, r.Body)
	clickNone(e, a)
}

// Real world: the link is pasted into a WhatsApp chat; WhatsApp fetches the preview. Expect: OG page, no click.
func TestS_CLICK_21_WhatsAppPreviewNoClick(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	clickOGLink(e, a, "wa")
	r := e.tap("/wa", uaWhatsApp)
	must(t, r.Code == 200 && strings.Contains(r.Body, "og:description"), "got %d, want OG page", r.Code)
	clickNone(e, a)
}

// Real world: the link is pasted into Telegram; its bot fetches the preview. Expect: OG page, no click.
func TestS_CLICK_22_TelegramPreviewNoClick(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	clickOGLink(e, a, "tg")
	r := e.tap("/tg", uaTelegramBot)
	must(t, r.Code == 200 && strings.Contains(r.Body, "og:title"), "got %d, want OG page", r.Code)
	clickNone(e, a)
}

// Real world: the link is sent in iMessage; Apple's preview fetcher (Mac Safari-like UA) loads it. Expect: OG page, not the iPad interstitial, no click.
func TestS_CLICK_23_IMessagePreviewOG(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	clickOGLink(e, a, "imsg")
	r := e.tap("/imsg", uaIMessage)
	must(t, r.Code == 200 && strings.Contains(r.Body, "og:title") && !strings.Contains(r.Body, "_dt"), "got %d, want OG page:\n%s", r.Code, r.Body)
	clickNone(e, a)
}

// Real world: a link without a preview is pasted into Slack. Expect: Slackbot follows the 302 to the web fallback, no click.
func TestS_CLICK_24_SlackWithoutOGFollowsRedirect(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "slk", "/p")
	r := e.tap("/slk", uaSlackBot)
	must(t, r.Code == 302 && r.Loc() == clickFallback, "got %d %q", r.Code, r.Loc())
	clickNone(e, a)
}

// Real world: a link is posted in Discord; Discordbot unfurls it. Expect: no interstitial, 302, no click.
func TestS_CLICK_25_DiscordUnfurlNoClick(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "dc", "/p")
	r := e.tap("/dc", uaDiscordBot)
	must(t, r.Code == 302, "got %d, want 302", r.Code)
	clickNone(e, a)
}

// Real world: Googlebot crawls a short link found on a page. Expect: noindex header, no click.
func TestS_CLICK_26_GooglebotNoindexNoClick(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "gb", "/p")
	r := e.tap("/gb", uaGoogleBot)
	must(t, strings.Contains(r.H.Get("X-Robots-Tag"), "noindex"), "X-Robots-Tag = %q", r.H.Get("X-Robots-Tag"))
	clickNone(e, a)
}

// Real world: a developer checks the link with curl. Expect: plain 302 to the fallback, no click.
func TestS_CLICK_27_CurlRedirectNoClick(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "cu", "/p")
	r := e.tap("/cu", uaCurl)
	must(t, r.Code == 302 && r.Loc() == clickFallback, "got %d %q", r.Code, r.Loc())
	clickNone(e, a)
}

// Real world: a link checker sends HEAD with a browser UA. Expect: same 302 as GET, no click.
func TestS_CLICK_28_HeadRequestNoClick(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "hd", "/p")
	r := do(t, "HEAD", e.Pub.URL+"/hd", "", map[string]string{"User-Agent": uaDesktopChrome, "X-Forwarded-For": clientIP})
	must(t, r.Code == 302 && r.Loc() == clickFallback, "HEAD got %d %q", r.Code, r.Loc())
	clickNone(e, a)
}

// Real world: someone opens an app-only link (no web fallback) on a laptop, e.g. from a poster QR shared by email. Expect: QR page with the short link (forwarded params, no internal ones) + store buttons.
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

// Real world: someone types a wrong/deleted short link. Expect: 404, no click.
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

// Real world: a user retypes a printed link in capitals (PROMO). Expect: same link (keys are unique case-insensitively).
func TestS_CLICK_31_KeyCaseInsensitive(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "promo", "/p")
	r := e.tap("/PROMO", uaDesktopChrome)
	must(t, r.Code == 302 && r.Loc() == clickFallback, "got %d %q", r.Code, r.Loc())
}

// Real world: a chat app or user adds a trailing slash (lnk.example/promo/). Expect: the link still works (resolve or redirect to /promo), not 404.
func TestS_CLICK_32_TrailingSlashResolves(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "promo", "/p")
	r := e.tap("/promo/", uaDesktopChrome)
	ok := r.Code >= 300 && r.Code < 400 && (r.Loc() == clickFallback || strings.HasSuffix(strings.SplitN(r.Loc(), "?", 2)[0], "/promo"))
	must(t, ok, "got %d %q, want redirect to fallback or /promo", r.Code, r.Loc())
}

// Real world: a mail client percent-encodes the path (/%70romo). Expect: same link.
func TestS_CLICK_33_PercentEncodedKey(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "promo", "/p")
	r := e.tap("/%70romo", uaDesktopChrome)
	must(t, r.Code == 302 && r.Loc() == clickFallback, "got %d %q", r.Code, r.Loc())
}

// Real world: a campaign with spaces, ampersands and Vietnamese in UTM values. Expect: forwarded to the web fallback with values intact.
func TestS_CLICK_34_PercentEncodedQueryForwarded(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "enc", "/p")
	r := e.tap("/enc?utm_campaign=summer%20sale%26more&utm_term=%E1%BB%87", uaDesktopChrome)
	q := clickQuery(t, r.Loc())
	must(t, q.Get("utm_campaign") == "summer sale&more" && q.Get("utm_term") == "ệ", "forwarded = %v", q)
}

// Real world: an ad network appends a huge tracking blob (long fbclid/gclid). Expect: redirect still works and keeps the blob.
func TestS_CLICK_35_VeryLongQueryForwarded(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "long", "/p")
	blob := strings.Repeat("Ab0_-", 800) // 4000 chars
	r := e.tap("/long?fbclid="+blob, uaDesktopChrome)
	must(t, r.Code == 302 && clickQuery(t, r.Loc()).Get("fbclid") == blob, "got %d, fbclid lost", r.Code)
	clickOne(e, a)
}

// Real world: a misconfigured campaign tool appends utm_source twice. Expect: redirect works, utm_source forwarded once.
func TestS_CLICK_36_DuplicateQueryKeysForwardOnce(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "dup", "/p")
	r := e.tap("/dup?utm_source=a&utm_source=b", uaDesktopChrome)
	vs := clickQuery(t, r.Loc())["utm_source"]
	must(t, r.Code == 302 && len(vs) == 1, "got %d utm_source = %v", r.Code, vs)
}

// Real world: a user copies the hop-2 URL (with detur's own params) and opens it on a laptop. Expect: none of _dt,_tap,_dc,screen,tz,pasted_link,_touch,detur-no-track reach the fallback; ref does.
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

// Real world: a Messenger Android tap page; the intent:// re-opens the short link in the app. Expect: intent carries ref but none of detur's params.
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

// Real world: an Apple Search Ads / custom product page campaign link on iPhone. Expect: App Store gets pt/ct/mt/ppid only; utm and malformed ppid dropped.
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

// Real world: a newsletter link on Android with utm_* plus a hostile ?id=other.app. Expect: utm_* + click_id inside referrer only, id untouched.
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

// Real world: the operator's fallback already has utm_source=site; a visitor arrives with utm_source=ads. Expect: operator's value kept, extra params added.
func TestS_CLICK_41_FallbackKeepsOperatorParams(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.linkWith(store.Link{AppID: a.ID, Key: "op", URL: "/p", FallbackURL: "https://example.com/download?utm_source=site"})
	q := clickQuery(t, e.tap("/op?utm_source=ads&utm_medium=cpc", uaDesktopChrome).Loc())
	must(t, q.Get("utm_source") == "site" && q.Get("utm_medium") == "cpc", "fallback query = %v", q)
}

// Real world: a developer tests on an iPhone with ?detur-no-track=1. Expect: no interstitial, straight to the App Store, no click.
func TestS_CLICK_42_NoTrackQuerySkipsEverything(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "nt", "/p")
	r := e.tap("/nt?detur-no-track=1", uaIOSSafari)
	clickHasPrefix(t, r, 302, clickAppStore)
	clickNone(e, a)
}

// Real world: a QA script sends the detur-no-track header from a laptop. Expect: redirect, no click, header value never forwarded.
func TestS_CLICK_43_NoTrackHeader(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "nth", "/p")
	r := e.tapH("/nth?detur-no-track=1&utm_source=qa", uaDesktopChrome, map[string]string{"detur-no-track": "1"})
	must(t, r.Code == 302 && !strings.Contains(r.Loc(), "detur-no-track") && clickQuery(t, r.Loc()).Get("utm_source") == "qa", "got %d %q", r.Code, r.Loc())
	clickNone(e, a)
}

// Real world: an iPhone user taps last month's promo link after it expired. Expect: straight 302 to the expired URL, no interstitial.
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

// Real world: an expired link without an expired URL. Expect: 410 Gone.
func TestS_CLICK_45_ExpiredLinkGone(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	past := time.Now().Add(-time.Minute)
	e.linkWith(store.Link{AppID: a.ID, Key: "gone", URL: "/p", FallbackURL: clickFallback, ExpiresAt: &past})
	must(t, e.tap("/gone", uaAndroidChrome).Code == 410, "want 410")
}

// Real world: an operator routes iOS visitors to a custom App Store product page via a platform rule. Expect: iPhone gets the rule target, click carries the rule name.
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

// Real world: German visitors on desktop get a German landing page via a language rule. Expect: de-DE browser → German fallback.
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

// Real world: a TikTok campaign adds ?ref=tiktok and the operator routes it to a TikTok landing page. Expect: param rule matches.
func TestS_CLICK_48_RuleQueryParam(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	l := e.link(a, "rp", "/p")
	must(t, e.St.SetLinkRules(l.ID, []store.LinkRule{{Name: "tiktok", Cond: store.RuleCond{ParamKey: "ref", ParamVal: "tiktok"},
		Action: store.RuleAction{Targets: &store.RuleTargets{FallbackURL: "https://example.com/tiktok"}}}}) == nil, "SetLinkRules")
	clickHasPrefix(t, e.tap("/rp?ref=tiktok", uaDesktopChrome), 302, "https://example.com/tiktok")
}

// Real world: a 50/50 A/B test of two landing pages; the same visitor clicks twice. Expect: same variant both times, click records the variant.
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

// Real world: an iPad user hits a link with an iOS-only rule. Expect: touch-detected iPad matches the iOS rule.
func TestS_CLICK_50_IPadMatchesIOSRule(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	l := e.link(a, "ripad", "/p")
	must(t, e.St.SetLinkRules(l.ID, []store.LinkRule{{Name: "ios", Cond: store.RuleCond{Platform: "ios"},
		Action: store.RuleAction{Targets: &store.RuleTargets{IOS: "https://apps.apple.com/app/id555"}}}}) == nil, "SetLinkRules")
	r := e.tap("/ripad?_dt=1&screen=820x1180%402&_touch=1", uaIPadOS)
	clickHasPrefix(t, r, 302, "https://apps.apple.com/app/id555")
}

// Real world: an Android user taps the same link twice within minutes. Expect: one click, same click_id in both Play referrers.
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

// Real world: two people on different networks tap the same link. Expect: two clicks.
func TestS_CLICK_52_DifferentVisitorsSeparateClicks(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "two", "/p")
	e.tap("/two", uaDesktopChrome)
	e.tapH("/two", uaDesktopChrome, map[string]string{"X-Forwarded-For": "198.51.100.20"})
	must(t, len(clickList(e, a)) == 2, "clicks = %d, want 2", len(clickList(e, a)))
}

// Real world: a user taps in Instagram, sees the tap page, chooses "Open in external browser" (Chrome). Expect: one click (instagram source), Chrome's Play referrer reuses its click_id.
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

// Real world: on shared Wi-Fi an iPhone user taps in Instagram and an Android user opens the link in Chrome. Expect: no merge across platforms, two clicks.
func TestS_CLICK_54_NoMergeAcrossPlatforms(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "xp", "/p")
	e.mobileTap("/xp", uaInstagramIOS)
	e.mobileTap("/xp", uaAndroidChrome)
	must(t, len(clickList(e, a)) == 2, "clicks = %d, want 2", len(clickList(e, a)))
}

// Real world: an Android-only app shares a link; an iPhone user taps it in Safari. The link has a web fallback. Expect: the web fallback, not a dead-end app page.
func TestS_CLICK_55_IPhoneWithoutIOSTargetGetsWebFallback(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.linkWith(store.Link{AppID: a.ID, Key: "andonly", URL: "/p", Android: clickPlay, FallbackURL: clickFallback})
	r := e.mobileTap("/andonly", uaIOSSafari)
	must(t, r.Code == 302 && r.Loc() == clickFallback, "got %d %q, want 302 %s (page offers Google Play to an iPhone: %v)",
		r.Code, r.Loc(), clickFallback, strings.Contains(r.Body, "Google Play"))
}

// Real world: an Android user taps a link with utm params; hop 1 must carry them through the reload. Expect: no-store page, noscript reload keeps utm_source + adds _dt, Accept-CH asked.
func TestS_CLICK_56_Hop1KeepsQueryThroughReload(t *testing.T) {
	e := newEnv(t)
	a := e.app()
	e.link(a, "h1", "/p")
	r := e.tap("/h1?utm_source=ig", uaAndroidChrome)
	must(t, r.Code == 200 && r.H.Get("Cache-Control") == "no-store", "hop 1 %d cache %q", r.Code, r.H.Get("Cache-Control"))
	must(t, strings.Contains(r.Body, "_dt=1") && strings.Contains(r.Body, "utm_source=ig"), "noscript reload lost params:\n%s", r.Body)
	must(t, strings.Contains(r.H.Get("Accept-CH"), "Sec-CH-UA-Model"), "Accept-CH = %q", r.H.Get("Accept-CH"))
	clickNone(e, a)
}
