package match

import (
	"math"
	"strconv"
	"strings"

	"detur.dev/server/internal/store"
	"detur.dev/server/internal/ua"
)

// Documented scoring weights (detour.swmansion.com/docs/platform/architecture/matching/). Only ONE device signal scored per candidate: Android model+system version (450) when click supplies both, else iOS system version (350), else UA device signature (350), never two. Keeps documented maxima 1700 iOS / 1450 Android.
const (
	weightIPExact           = 500
	weightModelSystemVer    = 450
	weightiOSSystemVersion  = 350
	weightUADeviceSignature = 350
	weightPasteboardToken   = 350 // token + URL prefix
	weightPasteboardPrefix  = 175 // URL prefix only
	weightTimezone          = 200
	weightScreen            = 200
	weightLanguage          = 100
)

// Score: probabilistic score of one candidate click vs first-launch fingerprint fp and request connection IP. Unavailable click-side signals skip weight; nothing fabricated. Chosen device-signal branch mismatch scores 0; UA fallback never stacks on top.
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

// deviceSignal: one-device-signal ladder — Android model+system version when click supplies both, else iOS system version from click UA, else UA device signature fallback.
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
		// UA device signature fallback (Android only, per Detour weights table)
		if platform == "android" && cm != "" && fm != "" && normModel(cm) == normModel(fm) {
			return weightUADeviceSignature
		}
		return 0
	}
}

// clickSignals: click-side device signals. Empty = not derivable from click fingerprint (weight skipped).
func clickSignals(click store.Click) (platform, model, sysVer string) {
	raw := click.Fingerprint.UserAgent
	if model = click.Fingerprint.Device; model == "" {
		model = ua.AndroidModel(raw)
	}
	switch ua.Platform(raw) {
	case "android":
		platform = "android"
		// Client hint carries real version. Reduced UA freezes "Android 10; K" and drops model: UA version trusted only when UA still names model.
		sysVer = click.Fingerprint.OSVersion
		if sysVer == "" && ua.AndroidModel(raw) != "" {
			sysVer = ua.AndroidVersion(raw)
		}
	case "ios":
		platform = "ios"
		sysVer = ua.IOSVersion(raw)
	}
	return platform, model, sysVer
}

// pasteboard: iOS pasteboard two-tier match — 350 when short-link token matches AND first-launch pasted URL starts with click's pasted URL; 175 for URL prefix alone.
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

// linkToken: short-link token (last non-empty path segment).
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

// screen: screen match, tolerance ±1 width/height, ±0.01 scale.
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

// langMatch: click's browser language (first tag) shares primary subtag with any device locale (comma-separated SDK tags), case-insensitive: en-GB matches en-US; browser en matches device vi-VN,en-US.
func langMatch(click, device string) bool {
	c := primaryLang(click)
	if c == "" {
		return false
	}
	for _, tag := range strings.Split(device, ",") {
		if primaryLang(tag) == c {
			return true
		}
	}
	return false
}

func primaryLang(tag string) string {
	tag = strings.ToLower(strings.TrimSpace(tag))
	if i := strings.IndexAny(tag, "-_"); i >= 0 {
		tag = tag[:i]
	}
	return tag
}

// parseScreen: clicks-table screen string "WxH@scale" (e.g. "393x852@3").
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

// normVersion: system version for comparison — underscores -> dots, trailing ".0" dropped ("14.0.0" client hint == "14" SDK).
func normVersion(v string) string {
	v = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(v), "_", "."))
	for strings.HasSuffix(v, ".0") {
		v = strings.TrimSuffix(v, ".0")
	}
	return v
}

// normModel: device model for comparison (case + whitespace).
func normModel(m string) string {
	return strings.ToLower(strings.Join(strings.Fields(m), " "))
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
