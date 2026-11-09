package match

import (
	"math"
	"strconv"
	"strings"

	"detur.dev/server/internal/store"
	"detur.dev/server/internal/ua"
)

// Documented scoring weights (detour.swmansion.com/docs/platform/architecture/matching/).
// IP 400 and Android model+version 400 (rebalanced). Timezone + language count only with pasteboard or device+screen (see Score),
// so nothing reaches default 850 without pasteboard or device+screen: shared Wi-Fi peers stay organic.
// ONE device signal per candidate: Android model+system version (400) if click has both, else iOS system version (350), else UA device signature (350); never two.
const (
	weightIPExact           = 400
	weightModelSystemVer    = 400
	weightiOSSystemVersion  = 350
	weightUADeviceSignature = 350
	weightPasteboardToken   = 350
	weightPasteboardPrefix  = 175
	weightTimezone          = 200
	weightScreen            = 200
	weightLanguage          = 100
)

func Score(click store.Click, fp Fingerprint, ip string) int {
	s := 0
	if ip != "" && click.Fingerprint.IP != "" && ip == click.Fingerprint.IP {
		s += weightIPExact
	}
	d, p, sc := deviceSignal(click, fp), pasteboard(click, fp), screen(click, fp)
	s += d + p + sc
	if p == 0 && (d == 0 || sc == 0) {
		return s
	}
	if click.Fingerprint.Timezone != "" && fp.Timezone != "" && click.Fingerprint.Timezone == fp.Timezone {
		s += weightTimezone
	}
	if langMatch(click.Fingerprint.Locale, fp.Locale) {
		s += weightLanguage
	}
	return s
}

func deviceSignal(click store.Click, fp Fingerprint) int {
	platform, cm, cv := clickSignals(click)
	fm := fp.Model
	if fm == "" {
		fm = ua.AndroidModel(fp.UserAgent)
	}
	fv := fp.SystemVersion
	switch {
	case platform == ua.PlatformAndroid && cm != "" && cv != "":
		if normModel(cm) == normModel(fm) && normVersion(cv) == normVersion(fv) {
			return weightModelSystemVer
		}
		return 0
	case platform == ua.PlatformIOS && cv != "":
		// OS token carries the patch (17_4 = 17.4.0): exact. Version/ has none: compare at its precision.
		if _, approx := ua.IOSVersion(click.Fingerprint.UserAgent); (approx && iosVersionMatch(cv, fv)) || (!approx && normVersion(cv) == normVersion(fv)) {
			return weightiOSSystemVersion
		}
		return 0
	default:
		if platform == ua.PlatformAndroid && cm != "" && fm != "" && normModel(cm) == normModel(fm) {
			return weightUADeviceSignature
		}
		return 0
	}
}

func clickSignals(click store.Click) (platform, model, sysVer string) {
	raw := click.Fingerprint.UserAgent
	if model = click.Fingerprint.Device; model == "" {
		model = ua.AndroidModel(raw)
	}
	switch ua.Platform(raw) {
	case ua.PlatformAndroid:
		platform = ua.PlatformAndroid
		// Client hint = real version. Reduced UA freezes "Android 10; K", drops model: trust UA version only when UA still names model.
		sysVer = click.Fingerprint.OSVersion
		if sysVer == "" && ua.AndroidModel(raw) != "" {
			sysVer = ua.AndroidVersion(raw)
		}
	case ua.PlatformIOS:
		platform = ua.PlatformIOS
		sysVer, _ = ua.IOSVersion(raw)
	}
	return platform, model, sysVer
}

// pasteboard: iOS two-tier: 350 = short-link token match AND first-launch pasted URL starts with click's pasted URL; 175 = URL prefix only.
func pasteboard(click store.Click, fp Fingerprint) int {
	cp, pp := store.StripPasteToken(click.Fingerprint.PastedLink), store.StripPasteToken(fp.PastedLink)
	if cp == "" || pp == "" || !strings.HasPrefix(pp, cp) {
		return 0
	}
	ct, pt := linkToken(cp), linkToken(pp)
	if ct != "" && ct == pt {
		return weightPasteboardToken
	}
	return weightPasteboardPrefix
}

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

// screen: tolerance ±1 width/height, ±0.01 scale. Either orientation: SDK Dimensions follow rotation, click page may not.
func screen(click store.Click, fp Fingerprint) int {
	cw, ch, cs, ok := parseScreen(click.Fingerprint.Screen)
	if !ok || fp.ScreenWidth <= 0 || fp.ScreenHeight <= 0 || fp.Scale <= 0 || math.Abs(cs-fp.Scale) > 0.01 {
		return 0
	}
	fw, fh := fp.ScreenWidth, fp.ScreenHeight
	if (abs(cw-fw) <= 1 && abs(ch-fh) <= 1) || (abs(cw-fh) <= 1 && abs(ch-fw) <= 1) {
		return weightScreen
	}
	return 0
}

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

func normVersion(v string) string {
	v = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(v), "_", "."))
	for strings.HasSuffix(v, ".0") {
		v = strings.TrimSuffix(v, ".0")
	}
	return v
}

// iosVersionMatch: click version vs SDK systemVersion, at the click's precision. iOS 26 Safari Version/ = major.minor
// ("26.0"), SDK sends the patch ("26.0.1"). Missing SDK parts = 0 ("26" = "26.0").
func iosVersionMatch(click, sdk string) bool {
	c := strings.Split(strings.ReplaceAll(strings.TrimSpace(click), "_", "."), ".")
	f := strings.Split(strings.ReplaceAll(strings.TrimSpace(sdk), "_", "."), ".")
	if strings.TrimSpace(sdk) == "" {
		return false
	}
	for i, v := range c {
		w := "0"
		if i < len(f) {
			w = f[i]
		}
		if v != w {
			return false
		}
	}
	return true
}

func normModel(m string) string {
	return strings.ToLower(strings.Join(strings.Fields(m), " "))
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
