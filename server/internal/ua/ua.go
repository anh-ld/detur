// Package ua shares the user-agent parsing used by matching and the browser
// pipeline so the two cannot drift: one feeds click-time fingerprint
// capture, the other scoring.
package ua

import (
	"regexp"
	"strings"
)

var (
	reAndroidVersion = regexp.MustCompile(`Android ([\d.]+)`)
	reIOSVersion     = regexp.MustCompile(`(?:iPhone OS|CPU OS) ([\d_.]+)`)
)

// botMarkers is the cheap UA bot filter: any UA containing one of these
// markers skips click recording.
var botMarkers = []string{"bot", "spider", "crawler", "preview", "facebookexternalhit", "slackbot", "twitterbot", "whatsapp"}

// IsBotUA reports whether a UA contains a known bot marker.
func IsBotUA(ua string) bool {
	ua = strings.ToLower(ua)
	for _, m := range botMarkers {
		if strings.Contains(ua, m) {
			return true
		}
	}
	return false
}

// Platform classifies the browser UA for scoring: "android"/"ios"/"".
// The Android check is case-sensitive (documented contract); redirect
// routing uses the case-insensitive IsAndroid instead.
func Platform(ua string) string {
	switch {
	case strings.Contains(ua, "Android"):
		return "android"
	case strings.Contains(ua, "iPhone") || strings.Contains(ua, "iPad") || strings.Contains(ua, "iPod"):
		return "ios"
	}
	return ""
}

// IsAndroid reports an Android browser UA (case-insensitive; redirect routing).
func IsAndroid(ua string) bool {
	return strings.Contains(strings.ToLower(ua), "android")
}

// IsIOS reports an iOS browser UA (iPhone/iPad/iOS, case-insensitive).
func IsIOS(ua string) bool {
	ua = strings.ToLower(ua)
	return strings.Contains(ua, "iphone") || strings.Contains(ua, "ipad") || strings.Contains(ua, "ios")
}

// AndroidVersion extracts the Android OS version from a browser UA.
func AndroidVersion(ua string) string {
	m := reAndroidVersion.FindStringSubmatch(ua)
	if m == nil {
		return ""
	}
	return m[1]
}

// IOSVersion extracts the iOS system version from a click UA, normalizing
// "17_2" to "17.2".
func IOSVersion(ua string) string {
	m := reIOSVersion.FindStringSubmatch(ua)
	if m == nil {
		return ""
	}
	return strings.ReplaceAll(m[1], "_", ".")
}

// AndroidModel extracts the device model token from an Android browser UA:
// modern "(Linux; Android 14; Pixel 7 Build/...)" -> "Pixel 7"; legacy
// "(Linux; U; Android 4.4; en-us; GT-I9300 Build/...)" -> "GT-I9300".
func AndroidModel(ua string) string {
	i := strings.Index(ua, "Android ")
	if i < 0 {
		return ""
	}
	rest := strings.TrimSpace(ua[i+len("Android "):])
	j := strings.Index(rest, ";")
	if j < 0 {
		return ""
	}
	rest = strings.TrimSpace(rest[j+1:])
	if k := strings.Index(rest, ";"); k >= 0 { // legacy locale slot
		rest = strings.TrimSpace(rest[k+1:])
	}
	rest = strings.TrimSuffix(strings.TrimSpace(rest), ")")
	if b := strings.Index(rest, " Build"); b >= 0 {
		rest = rest[:b]
	}
	return strings.TrimSpace(rest)
}

// DeviceLabel derives the click-time device label (pipeline fingerprint):
// Android model, else iPhone/iPad/iPod.
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
