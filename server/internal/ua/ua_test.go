package ua

// Real-world UA strings: modern Android (Chrome), reduced UA (Chrome's "Android 10; K"), legacy Android, Android WebView, iPhone Safari, Instagram's Pixel webview ("Google/google" false positive).
import (
	"net/http/httptest"
	"testing"
)

const (
	modernAndroid  = "Mozilla/5.0 (Linux; Android 14; Pixel 7 Build/TQ3A.230805.001) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Mobile Safari/537.36"
	reducedAndroid = "Mozilla/5.0 (Linux; Android 10; K) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36"
	legacyAndroid  = "Mozilla/5.0 (Linux; U; Android 4.4; en-us; GT-I9300 Build/KOT49H) AppleWebKit/534.30 (KHTML, like Gecko) Version/4.0 Mobile Safari/534.30"
	webviewAndroid = "Mozilla/5.0 (Linux; Android 14; Pixel 7 Build/UP1A.231005.007; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/125.0.0.0 Mobile Safari/537.36"
	iphoneSafari   = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_2 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.2 Mobile/15E148 Safari/604.1"
	instagramPixel = "Mozilla/5.0 (Linux; Android 14; Pixel 7 Build/UP1A.231005.007; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/125.0.0.0 Mobile Safari/537.36 Instagram 300.0.0.32.100 Google/google"
)

func TestAndroidModelRealUAs(t *testing.T) {
	cases := []struct {
		ua   string
		want string
	}{
		{modernAndroid, "Pixel 7"},
		{reducedAndroid, ""}, // reduced UA "K" carries no model
		{legacyAndroid, "GT-I9300"},
		{webviewAndroid, "Pixel 7"},
		{iphoneSafari, ""},
	}
	for _, c := range cases {
		if got := AndroidModel(c.ua); got != c.want {
			t.Errorf("AndroidModel(%q) = %q; want %q", c.ua, got, c.want)
		}
	}
}

func TestIsBotUADubList(t *testing.T) {
	for _, ua := range []string{
		"Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)",
		"Slackbot-LinkExpanding 1.0 (+https://api.slack.com/robots)",
		"facebookexternalhit/1.1 (+http://www.facebook.com/externalhit_uatext.php)",
		"curl/8.4.0",
		"python-requests/2.31.0",
	} {
		if !IsBotUA(ua) {
			t.Errorf("IsBotUA(%q) = false; want true", ua)
		}
	}
	// Dub's UA_FALSE_POSITIVES: Instagram's Pixel webview appends "Google/google", must not trip "google" entry.
	if IsBotUA(instagramPixel) {
		t.Errorf("IsBotUA(instagram Pixel webview) = true; want false (Google/google exception)")
	}
	// Real browsers are never bots, and "preview" is not a marker anymore.
	for _, ua := range []string{modernAndroid, reducedAndroid, iphoneSafari, "SomePreviewApp/1.0 Preview/2"} {
		if IsBotUA(ua) {
			t.Errorf("IsBotUA(%q) = true; want false", ua)
		}
	}
}

func TestIsBotRequest(t *testing.T) {
	if r := httptest.NewRequest("GET", "/abc?bot=1", nil); !IsBot(r) {
		t.Error("IsBot(?bot=1) = false; want true")
	}
	if r := httptest.NewRequest("HEAD", "/abc", nil); !IsBot(r) {
		t.Error("IsBot(HEAD) = false; want true")
	}
	r := httptest.NewRequest("GET", "/abc", nil)
	r.Header.Set("User-Agent", "Googlebot/2.1")
	if !IsBot(r) {
		t.Error("IsBot(Googlebot UA) = false; want true")
	}
	r = httptest.NewRequest("GET", "/abc", nil)
	r.Header.Set("User-Agent", modernAndroid)
	if IsBot(r) {
		t.Error("IsBot(real mobile UA) = true; want false")
	}
}

func TestIOSVersion(t *testing.T) {
	cases := []struct{ ua, want string }{
		{"Mozilla/5.0 (iPhone; CPU iPhone OS 18_6 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/26.0 Mobile/15E148 Safari/604.1", "26.0"},
		{"Mozilla/5.0 (iPhone; CPU iPhone OS 17_2_1 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.2 Mobile/15E148 Safari/604.1", "17.2.1"},
		{"Mozilla/5.0 (iPhone; CPU iPhone OS 18_6 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) CriOS/140.0 Mobile/15E148 Safari/604.1", "18.6"},
	}
	for _, c := range cases {
		if got := IOSVersion(c.ua); got != c.want {
			t.Errorf("IOSVersion(%q) = %q; want %q", c.ua, got, c.want)
		}
	}
}

// AppPlatform: browser markers first, then native HTTP client markers of in-app SDK requests.
func TestAppPlatform(t *testing.T) {
	for _, c := range []struct{ ua, want string }{
		{iphoneSafari, "ios"},
		{modernAndroid, "android"},
		{"MyApp/1 CFNetwork/1490.0.4 Darwin/23.5.0", "ios"},
		{"okhttp/4.12.0", "android"},
		{"Dalvik/2.1.0 (Linux; U; Android 14; Pixel 7)", "android"},
		{"Mozilla/5.0 (Windows NT 10.0; Win64; x64)", ""},
		{"", ""},
	} {
		if got := AppPlatform(c.ua); got != c.want {
			t.Errorf("AppPlatform(%q) = %q; want %q", c.ua, got, c.want)
		}
	}
}

// In-app browser UAs: published app tokens on real browser bases, not device captures.
const (
	messengerIOS     = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_2 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Mobile/15E148 [FBAN/MessengerForiOS;FBAV/442.0.0.42.110;FBBV/556001370;FBDV/iPhone15,2;FBMD/iPhone;FBSN/iOS;FBSV/17.2;FBSS/3;FBID/phone;FBLC/en_US;FBOP/5]"
	messengerAndroid = "Mozilla/5.0 (Linux; Android 14; Pixel 7 Build/UP1A.231005.007; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/125.0.0.0 Mobile Safari/537.36 [FB_IAB/MESSENGER;FBAV/450.0.0.42.109;]"
	orcaAndroid      = "Mozilla/5.0 (Linux; Android 13; SM-G991B Build/TP1A.220624.014; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/124.0.0.0 Mobile Safari/537.36 [FB_IAB/Orca-Android;FBAV/440.0.0.31.105;]"
	facebookIOS      = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_2 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Mobile/15E148 [FBAN/FBIOS;FBAV/450.0.0.38.108;FBBV/567289435;FBDV/iPhone15,2;FBMD/iPhone;FBSN/iOS;FBSV/17.2;FBSS/3;FBCR/;FBID/phone;FBLC/en_US;FBOP/80]"
	facebookAndroid  = "Mozilla/5.0 (Linux; Android 14; Pixel 7 Build/UP1A.231005.007; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/125.0.0.0 Mobile Safari/537.36 [FB_IAB/FB4A;FBAV/450.0.0.39.109;]"
	threadsIOS       = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_2 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Mobile/15E148 Barcelona 289.0.0.77.109 (iPhone15,2; iOS 17_2; en_US; en; scale=3.00; 1179x2556; 489720908)"
	instagramIOS     = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_2 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Mobile/15E148 Instagram 300.0.0.32.100 (iPhone15,2; iOS 17_2; en_US; en; scale=3.00; 1179x2556; 516348021)"
	zaloAndroid      = "Mozilla/5.0 (Linux; Android 13; SM-A536E Build/TP1A.220624.014; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/124.0.0.0 Mobile Safari/537.36 Zalo android/12100656 ZaloTheme/light ZaloLanguage/vi"
	tiktokAndroid    = "Mozilla/5.0 (Linux; Android 14; Pixel 7 Build/UP1A.231005.007; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/125.0.0.0 Mobile Safari/537.36 trill_340002 JsSdk/1.0 NetType/WIFI Channel/googleplay AppName/musical_ly app_version/34.0.2 ByteLocale/en ByteFullLocale/en Region/US BytedanceWebview/d8a21c6"
	tiktokIOS        = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_2 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Mobile/15E148 musical_ly_34.0.0 JsSdk/2.0 NetType/WIFI Channel/App Store ByteLocale/en Region/US"
	linkedinIOS      = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_2 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Mobile/15E148 [LinkedInApp]/9.29.6160"
	snapchatIOS      = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_2 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Mobile/15E148 Snapchat/12.80.0.40 (like Safari/8617.1.17.10.12, panda)"
	lineIOS          = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_2 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Mobile/15E148 Safari Line/13.20.0"
	lineAndroid      = "Mozilla/5.0 (Linux; Android 14; Pixel 7 Build/UP1A.231005.007; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/125.0.0.0 Mobile Safari/537.36 Line/13.20.0/IAB"
	xIOS             = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_2 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Mobile/15E148 Twitter for iPhone/10.50"
	xAndroid         = "Mozilla/5.0 (Linux; Android 14; Pixel 7 Build/UP1A.231005.007; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/125.0.0.0 Mobile Safari/537.36 TwitterAndroid"
	telegramAndroid  = "Mozilla/5.0 (Linux; Android 14; Pixel 7 Build/UP1A.231005.007; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/125.0.0.0 Mobile Safari/537.36 Telegram-Android/10.14.5"
	wechatIOS        = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_2 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Mobile/15E148 MicroMessenger/8.0.47(0x18002f2c) NetType/WIFI Language/zh_CN"
	webviewIOS       = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_2 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Mobile/15E148"
	chromeIOS        = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_2 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) CriOS/125.0.6422.80 Mobile/15E148 Safari/604.1"
	firefoxIOS       = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_2 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) FxiOS/125.0 Mobile/15E148 Safari/605.1.15"
)

func TestInApp(t *testing.T) {
	for _, c := range []struct{ ua, want string }{
		{messengerIOS, "messenger"},
		{messengerAndroid, "messenger"},
		{orcaAndroid, "messenger"},
		{facebookIOS, "facebook"},
		{facebookAndroid, "facebook"},
		{threadsIOS, "threads"},
		{instagramIOS, "instagram"},
		{instagramPixel, "instagram"},
		{zaloAndroid, "zalo"},
		{tiktokAndroid, "tiktok"},
		{tiktokIOS, "tiktok"},
		{linkedinIOS, "linkedin"},
		{snapchatIOS, "snapchat"},
		{lineIOS, "line"},
		{lineAndroid, "line"},
		{xIOS, "x"},
		{xAndroid, "x"},
		{telegramAndroid, "telegram"},
		{wechatIOS, "wechat"},
		{webviewAndroid, SourceUnknownInApp},
		{webviewIOS, SourceUnknownInApp},
		{iphoneSafari, ""},
		{modernAndroid, ""},
		{chromeIOS, ""},
		{firefoxIOS, ""},
		{"Mozilla/5.0 (Windows NT 10.0; Win64; x64) OnlineShop/1.0 Line/2", ""}, // desktop: never in-app
		{"Twitterbot/1.0", ""},
		{"", ""},
	} {
		if got := InApp(c.ua); got != c.want {
			t.Errorf("InApp(%q) = %q; want %q", c.ua, got, c.want)
		}
	}
}

// In-app UAs carrying "google" (TikTok's Channel/googleplay) are real visitors, not bots.
func TestIsBotUAInAppFalsePositives(t *testing.T) {
	for _, ua := range []string{tiktokAndroid, messengerAndroid, zaloAndroid, wechatIOS, lineAndroid} {
		if IsBotUA(ua) {
			t.Errorf("IsBotUA(%q) = true; want false", ua)
		}
	}
}
