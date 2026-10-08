// Package ua: user-agent parsing shared by matching and browser pipeline so the two cannot drift: one feeds click-time fingerprint capture, the other scoring.
package ua

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

var (
	reAndroidVersion = regexp.MustCompile(`Android ([\d.]+)`)
	reIOSVersion     = regexp.MustCompile(`(?:iPhone OS|CPU OS) ([\d_.]+)`)
	reSafariVersion  = regexp.MustCompile(`Version/([\d.]+)`)
)

// uaBots: Dub's UA_BOTS list (apps/web/lib/middleware/utils/bots-list.ts), matched case-insensitively as substrings.
var uaBots = regexp.MustCompile(`(?i)` + strings.Join([]string{
	"bot", "crawler", "spider", "http", "scraper", "fetch", "curl", "wget", "python", "node", "ruby",
	"chatgpt", "bluesky", "facebookexternalhit", "meta-externalagent", "meta-externalads",
	"meta-externalfetcher", "meta-webindexer", "thirdLandingPageFeInfra", "WhatsApp", "google",
	"baidu", "bing", "msn", "duckduckbot", "teoma", "slurp", "yandex", "MetaInspector", "iframely",
	"HeadlessChrome", "ia_archiver", "Sogou", "SkypeUriPreview", "vkShare", "Slackbot", "Tumblr",
	"FeedBurner", "upptime", "Hyperping", "cron-job", "InternetMeasurement", "HostTracker", "Expanse",
	"anthropic-ai", "Claude-Web", "Applebot-Extended", "perplexity", "Omigili", "timpi",
	"ShortLinkTranslate", "BingPreview", "facebookcatalog", "Embedly", "Scrapy", "axios", "Guzzle",
	"Postman", "Insomnia", "Newman", "Qwantify", "Wayback", "heritrix", "nutch", "seokicks", "sistrix",
	"searchmetrics", "linkdex", "opensiteexplorer", "spyfu", "serpstat", "cognitiveseo", "seobility",
	"seositecheckup", "woorank", "gtmetrix", "pingdom", "statuscake", "site24x7", "monitis", "gomez",
	"neustar", "catchpoint", "webpagetest", "speedcurve", "dareboost", "yellowlab", "linkchecker",
	"deadlinkchecker", "brokenlinkcheck", "xenu", "scrutiny", "powermapper", "siteimprove", "monsido",
}, "|"))

// uaFalsePositive: Dub's UA_FALSE_POSITIVES — Instagram's webview on Pixel appends "Google/google", would otherwise trip "google". Same for TikTok's webview "Channel/googleplay" (not in Dub's list).
var uaFalsePositive = regexp.MustCompile(`Google/google\b|Channel/googleplay\b`)

// IsBotUA: UA matches Dub's bot list.
func IsBotUA(ua string) bool {
	return uaBots.MatchString(uaFalsePositive.ReplaceAllString(ua, ""))
}

// IsBot: Dub's detectBot for browser clicks — ?bot= param, any HEAD request, or bot UA.
func IsBot(r *http.Request) bool {
	return r.URL.Query().Get("bot") != "" || r.Method == http.MethodHead || IsBotUA(r.UserAgent())
}

// Platform: browser UA class for scoring, "android"/"ios"/"". Android check case-sensitive (documented contract); redirect routing uses case-insensitive IsAndroid.
func Platform(ua string) string {
	switch {
	case strings.Contains(ua, "Android"):
		return "android"
	case strings.Contains(ua, "iPhone") || strings.Contains(ua, "iPad") || strings.Contains(ua, "iPod"):
		return "ios"
	}
	return ""
}

// IsAndroid: Android browser UA (case-insensitive; redirect routing).
func IsAndroid(ua string) bool {
	return strings.Contains(strings.ToLower(ua), "android")
}

// AppPlatform: platform of an in-app (SDK) request, "ios"/"android"/"": browser UA markers, then native HTTP client markers (iOS CFNetwork/Darwin, Android okhttp/Dalvik).
func AppPlatform(ua string) string {
	if p := Platform(ua); p != "" {
		return p
	}
	l := strings.ToLower(ua)
	switch {
	case strings.Contains(l, "cfnetwork") || strings.Contains(l, "darwin"):
		return "ios"
	case strings.Contains(l, "okhttp") || strings.Contains(l, "dalvik"):
		return "android"
	}
	return ""
}

// IsIOS: iOS browser UA (iPhone/iPad/iPod, case-insensitive).
func IsIOS(ua string) bool {
	ua = strings.ToLower(ua)
	return strings.Contains(ua, "iphone") || strings.Contains(ua, "ipad") || strings.Contains(ua, "ipod")
}

// AndroidVersion: Android OS version from browser UA.
func AndroidVersion(ua string) string {
	m := reAndroidVersion.FindStringSubmatch(ua)
	if m == nil {
		return ""
	}
	return m[1]
}

// IOSVersion: iOS system version from click UA, normalizing "17_2" to "17.2". iOS 26 WebKit freezes the OS token at 18_6; Safari's Version/ token carries the real version (Detour reads it too). Version/ wins only when its major is above the OS token's major: on older iOS the OS token is more precise (17_2_1 vs Version/17.2).
func IOSVersion(ua string) string {
	v := ""
	if m := reIOSVersion.FindStringSubmatch(ua); m != nil {
		v = strings.ReplaceAll(m[1], "_", ".")
	}
	if m := reSafariVersion.FindStringSubmatch(ua); m != nil && major(m[1]) > major(v) {
		return m[1]
	}
	return v
}

// major: integer before first ".", non-numeric = 0.
func major(v string) int {
	n, _ := strconv.Atoi(strings.SplitN(v, ".", 2)[0])
	return n
}

// AndroidModel: device model token from Android browser UA: modern "(Linux; Android 14; Pixel 7 Build/...)" -> "Pixel 7"; legacy "(Linux; U; Android 4.4; en-us; GT-I9300 Build/...)" -> "GT-I9300"; WebView "(Linux; Android 14; Pixel 7 Build/UP1A; wv)" -> "Pixel 7"; reduced UA "(Linux; Android 10; K)" carries no model -> "".
func AndroidModel(ua string) string {
	i := strings.Index(ua, "Android ")
	if i < 0 {
		return ""
	}
	rest := ua[i+len("Android "):]
	if j := strings.IndexByte(rest, ')'); j >= 0 {
		rest = rest[:j]
	}
	// Tokens after the version: [locale;] model [Build/...] [; wv]
	parts := strings.Split(rest, ";")[1:]
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if b := strings.Index(p, " Build"); b >= 0 {
			p = p[:b]
		} else if strings.HasPrefix(p, "Build/") {
			continue
		}
		p = strings.TrimSpace(p)
		switch {
		case p == "", p == "K", p == "U", p == "wv", reLocale.MatchString(p):
			continue
		}
		return p
	}
	return ""
}

var reLocale = regexp.MustCompile(`^[a-z]{2}(?:[-_][a-zA-Z]{2})?$`)

// DeviceLabel: click-time device label (pipeline fingerprint): Android model, else iPhone/iPad/iPod.
func DeviceLabel(ua string) string {
	switch {
	case strings.Contains(ua, "Android"):
		return AndroidModel(ua)
	case strings.Contains(ua, "iPhone"):
		return "iPhone"
	case strings.Contains(ua, "iPad"):
		return "iPad"
	case strings.Contains(ua, "iPod"):
		return "iPod"
	}
	return ""
}

// SourceUnknownInApp: click source for a generic webview no token names.
const SourceUnknownInApp = "unknown-inapp"

// inAppTokens: in-app browser UA tokens, first match wins (Messenger before Facebook: both carry FB_IAB; Threads before Instagram). Written from ua-parser-js v2 token facts + vendor UA samples, not copied (AGPL). Narrow tokens keep crawlers out ("Twitter for", not "Twitter").
var inAppTokens = []struct {
	name string
	re   *regexp.Regexp
}{
	{"messenger", regexp.MustCompile(`FBAN/Messenger|FB_IAB/MESSENGER|Orca-Android`)},
	{"threads", regexp.MustCompile(`Barcelona`)},
	{"instagram", regexp.MustCompile(`Instagram`)},
	{"facebook", regexp.MustCompile(`FBAN/|FBAV/|FB_IAB/|FBIOS`)},
	{"zalo", regexp.MustCompile(`(?i)zalo`)},
	{"tiktok", regexp.MustCompile(`musical_ly|BytedanceWebview|trill_|TikTok`)},
	{"linkedin", regexp.MustCompile(`LinkedInApp`)},
	{"snapchat", regexp.MustCompile(`Snapchat/`)},
	{"line", regexp.MustCompile(`\bLine/`)},
	{"x", regexp.MustCompile(`Twitter for|TwitterAndroid`)},
	{"telegram", regexp.MustCompile(`Telegram`)},
	{"wechat", regexp.MustCompile(`MicroMessenger`)},
}

// reIOSBrowser: real iOS browsers (not webviews), excluded from the missing-"Safari/" webview check.
var reIOSBrowser = regexp.MustCompile(`CriOS|FxiOS|EdgiOS|OPiOS`)

// InApp: in-app browser of a mobile UA — app name, SourceUnknownInApp for a generic webview (Android "; wv)", iOS without "Safari/"), "" for a real browser or non-mobile UA. SFSafariViewController / Custom Tabs send the plain browser UA: undetectable.
func InApp(ua string) string {
	ios, android := IsIOS(ua), IsAndroid(ua)
	if !ios && !android {
		return ""
	}
	for _, t := range inAppTokens {
		if t.re.MatchString(ua) {
			return t.name
		}
	}
	if android && strings.Contains(ua, "; wv)") || ios && !strings.Contains(ua, "Safari/") && !reIOSBrowser.MatchString(ua) {
		return SourceUnknownInApp
	}
	return ""
}
