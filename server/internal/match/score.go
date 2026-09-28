package match

import (
	"math"
	"strconv"
	"strings"

	"detur.dev/server/internal/store"
	"detur.dev/server/internal/ua"
)

// Documented scoring weights (R6; detour.swmansion.com/docs/platform/
// architecture/matching/). Only ONE device signal is scored per candidate:
// Android model+system version (450) when the click supplies both, else iOS
// system version (350), else the UA device signature (350) — never two. This
// keeps the documented maxima at 1700 iOS / 1450 Android.
const (
	weightIPExact           = 500
	weightModelSystemVer    = 450
	weightiOSSystemVersion  = 350
	weightUADeviceSignature = 350
	weightPasteboardToken   = 350 // pasteboard token + URL prefix
	weightPasteboardPrefix  = 175 // URL prefix only
	weightTimezone          = 200
	weightScreen            = 200
	weightLanguage          = 100
)

// Score computes the probabilistic score of one candidate click against the
// first-launch fingerprint fp and the request connection IP (R6 weights).
// Click-side signals that are unavailable skip their weight; nothing is
// fabricated. A chosen device-signal branch that does not match scores 0 —
// the UA fallback is never stacked on it.
func Score(click store.Click, fp Fingerprint, ip string) int {
	s := 0
	if ip != "" && click.Fingerprint.IP != "" && ip == click.Fingerprint.IP {
		s += weightIPExact
	}
	s += deviceSignal(click, fp)
	s += pasteboard(click, fp)
	if click.Fingerprint.Timezone != "" && fp.Timezone != "" && click.Fingerprint.Timezone == fp.Timezone {
		s += weightTimezone
	}
	s += screen(click, fp)
	if langMatch(click.Fingerprint.Locale, fp.Locale) {
		s += weightLanguage
	}
	return s
}

// deviceSignal applies the one-device-signal ladder (R6): Android
// model+system version when the click supplies both, else iOS system version
// parsed from the click UA, else the UA device signature as fallback.
func deviceSignal(click store.Click, fp Fingerprint) int {
	platform, cm, cv := clickSignals(click)
	fm := fp.Model
	if fm == "" {
		fm = ua.AndroidModel(fp.UserAgent)
	}
	fv := fp.SystemVersion
	switch {
	case platform == "android" && cm != "" && cv != "":
		if normModel(cm) == normModel(fm) && normVersion(cv) == normVersion(fv) {
			return weightModelSystemVer
		}
		return 0
	case platform == "ios" && cv != "":
		if normVersion(cv) == normVersion(fv) {
			return weightiOSSystemVersion
		}
		return 0
	default:
		// Platform device signal unavailable -> UA device signature fallback.
		if cm != "" && fm != "" && normModel(cm) == normModel(fm) {
			return weightUADeviceSignature
		}
		return 0
	}
}

// clickSignals derives the click-side device signals. Empty values mean the
// signal is not derivable from the click fingerprint (weight skipped).
func clickSignals(click store.Click) (platform, model, sysVer string) {
	raw := click.Fingerprint.UserAgent
	switch ua.Platform(raw) {
	case "android":
		platform = "android"
		sysVer = ua.AndroidVersion(raw)
	case "ios":
		platform = "ios"
		sysVer = ua.IOSVersion(raw)
	}
	if model = click.Fingerprint.Device; model == "" {
		model = ua.AndroidModel(raw)
	}
	return platform, model, sysVer
}

// pasteboard scores the iOS pasteboard two-tier match (R6): 350 when the
// short-link token matches AND the first-launch pasted URL starts with the
// click's pasted URL; 175 for the URL prefix alone.
func pasteboard(click store.Click, fp Fingerprint) int {
	cp, pp := click.Fingerprint.PastedLink, fp.PastedLink
	if cp == "" || pp == "" || !strings.HasPrefix(pp, cp) {
		return 0
	}
	ct, pt := linkToken(cp), linkToken(pp)
	if ct != "" && ct == pt {
		return weightPasteboardToken
	}
	return weightPasteboardPrefix
}

// linkToken returns the short-link token (last non-empty path segment).
func linkToken(u string) string {
	if i := strings.IndexAny(u, "?#"); i >= 0 {
		u = u[:i]
	}
	u = strings.TrimSuffix(u, "/")
	if i := strings.LastIndex(u, "/"); i >= 0 {
		return u[i+1:]
	}
	return ""
}

// screen scores the screen match with tolerance ±1 width/height, ±0.01 scale.
func screen(click store.Click, fp Fingerprint) int {
	cw, ch, cs, ok := parseScreen(click.Fingerprint.Screen)
	if !ok || fp.ScreenWidth <= 0 || fp.ScreenHeight <= 0 || fp.Scale <= 0 {
		return 0
	}
	if abs(cw-fp.ScreenWidth) <= 1 && abs(ch-fp.ScreenHeight) <= 1 && math.Abs(cs-fp.Scale) <= 0.01 {
		return weightScreen
	}
	return 0
}

// langMatch reports a locale prefix match (R6: language 100).
func langMatch(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return strings.HasPrefix(a, b) || strings.HasPrefix(b, a)
}

// parseScreen parses the clicks-table screen string "WxH@scale"
// (e.g. "393x852@3").
func parseScreen(s string) (w, h int, scale float64, ok bool) {
	parts := strings.Split(s, "@")
	if len(parts) != 2 {
		return 0, 0, 0, false
	}
	wh := strings.Split(parts[0], "x")
	if len(wh) != 2 {
		return 0, 0, 0, false
	}
	w, err1 := strconv.Atoi(wh[0])
	h, err2 := strconv.Atoi(wh[1])
	scale, err3 := strconv.ParseFloat(parts[1], 64)
	if err1 != nil || err2 != nil || err3 != nil || w <= 0 || h <= 0 || scale <= 0 {
		return 0, 0, 0, false
	}
	return w, h, scale, true
}

// normVersion normalizes a system version for comparison (underscores -> dots).
func normVersion(v string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(v), "_", "."))
}

// normModel normalizes a device model for comparison (case + whitespace).
func normModel(m string) string {
	return strings.ToLower(strings.Join(strings.Fields(m), " "))
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
