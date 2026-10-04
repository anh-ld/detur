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
