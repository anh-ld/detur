// Package pipeline: browser side of deferred deep-link funnel. GET /{key}
// resolves globally, records click + fingerprint, 302s to store or fallback
// (clones Dub's link.ts middleware).
package pipeline

import (
	"errors"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"detur.dev/server/internal/fraud"
	"detur.dev/server/internal/httpx"
	"detur.dev/server/internal/store"
	"detur.dev/server/internal/ua"
)

// Interstitial-appended query params: internal, never forwarded (store.InternalParams).
const (
	paramDone     = store.ParamDone
	paramScreen   = store.ParamScreen
	paramTimezone = store.ParamTimezone
	paramPasted   = store.ParamPasted
	paramNoTrack  = store.ParamNoTrack
	paramTap      = store.ParamTap
	paramTouch    = store.ParamTouch
)

type pipelineServer struct {
	st             *store.Store
	log            *log.Logger
	retentionHours int // click retention floor, threaded from config
}

// Register: GET /{key} short links on mux; /health and /api/* more specific, still win (stdlib routing).
func Register(mux *http.ServeMux, st *store.Store, retentionHours int) {
	p := &pipelineServer{st: st, log: log.Default(), retentionHours: retentionHours}
	mux.HandleFunc("GET /{key}", p.handleShort)
	// "lnk.example/promo/" (chat apps, hand-typed): 301 to the key, query kept
	mux.HandleFunc("GET /{key}/{$}", func(w http.ResponseWriter, r *http.Request) {
		target := "/" + url.PathEscape(r.PathValue("key"))
		if r.URL.RawQuery != "" {
			target += "?" + r.URL.RawQuery
		}
		http.Redirect(w, r, target, http.StatusMovedPermanently)
	})
}

// handleShort: resolve key globally, record click (bot-filtered), 302 to store
// or fallback. Mobile browsers first get one-hop interstitial reading screen +
// timezone (like Dub's deeplink preview). Record failure logged, never blocks redirect.
func (p *pipelineServer) handleShort(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	link, err := p.st.GetLinkByKeyGlobal(key)
	if errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrAmbiguousKey) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		p.log.Printf("short-link lookup failed for %q: %v", key, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("X-Robots-Tag", "googlebot: noindex")
	if link.Expired(time.Now()) {
		if link.ExpiredURL != "" {
			http.Redirect(w, r, link.ExpiredURL, http.StatusFound)
			return
		}
		http.Error(w, "link expired", http.StatusGone)
		return
	}
	// iPadOS Safari sends Mac UA. Interstitial saw touch -> UA = iPad for everything after
	// (rules, store redirect, stored click, scoring).
	if r.URL.Query().Get(paramTouch) == "1" && ua.MaybeIPad(r.UserAgent()) {
		r.Header.Set("User-Agent", strings.Replace(r.UserAgent(), "Macintosh", "iPad", 1))
	}
	rules, err := p.st.GetLinkRules(link.ID)
	if err != nil {
		p.log.Printf("get link rules failed for %q (%s): %v", key, link.ID, err)
		rules = nil
	}
	route := EvaluateRules(rules, link, r, time.Now())
	effectiveLink := link
	effectiveLink.URL = route.Destination
	effectiveLink.IOS = route.IOS
	effectiveLink.Android = route.Android
	effectiveLink.FallbackURL = route.FallbackURL

	q := r.URL.Query()
	agent := r.UserAgent()
	_, noTrackQ := q[paramNoTrack]
	_, noTrackH := r.Header[http.CanonicalHeaderKey(paramNoTrack)]
	bot := ua.IsBot(r)
	track := !bot && !noTrackQ && !noTrackH // presence, not value (Dub record-click.ts has())
	mobile := ua.IsIOS(agent) || ua.IsAndroid(agent)
	trackedMobile := track && mobile
	source := "" // in-app browser that sent the tap (ua.InApp)
	if trackedMobile {
		source = ua.InApp(agent)
	}
	named := source != "" && source != ua.SourceUnknownInApp
	// link-preview crawler + preview set: OG page instead of the 302 (base link's preview; rules only pick the onward target)
	if r.Method == http.MethodGet && bot && link.OGTitle+link.OGDescription+link.OGImage != "" {
		serveOG(w, r, link, redirectTarget(effectiveLink, agent, "", q), q)
		return
	}
	if track && (mobile || ua.MaybeIPad(agent)) && q.Get(paramDone) == "" {
		// recognized in-app: auto reload, never copy page; hop 2 serves tap page
		serveInterstitial(w, q, ua.IsIOS(agent) && isAppStoreURL(effectiveLink.IOS) && !named, safetyNetDelay(source))
		return
	}
	// clickId minted first, embedded in redirect (Dub link.ts). Click stores link.URL + params as deferred destination, not store URL. Dedup hit -> earlier click's id
	clickID := ""
	if track {
		clickID = store.Nanoid(16)
	}
	dest := redirectTarget(effectiveLink, agent, clickID, q)
	if track {
		fp := fingerprint(r, q, link)
		rec, err := p.st.RecordClick(store.Click{
			ID: clickID, AppID: link.AppID, LinkID: link.ID, Destination: deepLinkURL(effectiveLink, q),
			Fingerprint: fp, Platform: clickPlatform(agent), Kind: clickKind(dest),
			UASuspect: fraud.SuspectUA(agent), IPHosting: fraud.Hosting(fp.IP), // raw facts, judged at match (KTD1)
			Source:     source,
			Variant:    route.Variant,
			PasteToken: pasteToken(fp.PastedLink),
		}, p.retentionHours)
		if err != nil {
			p.log.Printf("click record failed (redirect continues): %v", err)
			dest = redirectTarget(effectiveLink, agent, "", q) // no recorded click -> no referrer
		} else if rec.ID != clickID {
			dest = redirectTarget(effectiveLink, agent, rec.ID, q)
		}
	}
	// in-app browsers block auto hand-off: recognized ones + safety-net reload get a tap link instead
	if _, tap := q[paramTap]; trackedMobile && (named || tap) && hasStoreTarget(effectiveLink, agent) {
		p.serveTap(w, r, effectiveLink, dest, source, q)
		return
	}
	// never 302 to an app route (relative: lands on another short link or 404s); desktop + app-scheme target: a 302 opens nothing.
	// Both -> app page: QR on desktop, store buttons on a phone.
	if httpx.IsRoute(dest) || (!mobile && !bot && !isWebURL(dest)) {
		p.serveDesktop(w, r, effectiveLink, q, mobile)
		return
	}
	http.Redirect(w, r, dest, http.StatusFound)
}

// interstitialTmpl: reloads short link once with screen + timezone (+ _touch on touch screens); no-JS browsers fall through with neither. Accept-CH: Chromium sends device model + OS version on reload.
// Copy mode (iOS + App Store target): waits for tap, copies short link to pasteboard (needs user gesture), reloads with pasted_link in same tap. SDK reads pasteboard on first launch: pasteboard signal (350/175).
// Safety net: page still visible after delay ms = hand-off blocked -> reload with _tap, hop 2 serves tap page. Hidden/pagehide or late timer (frozen in background) cancels; 0 = off.
var interstitialTmpl = template.Must(template.New("i").Parse(`<!doctype html>
<meta charset="utf-8"><meta name="robots" content="noindex"><meta name="viewport" content="width=device-width,initial-scale=1">
<noscript><meta http-equiv="refresh" content="0;url={{.Next}}"></noscript>
{{if .Copy}}<style>body{font:17px -apple-system,system-ui,sans-serif;margin:0;min-height:100vh;display:flex;align-items:center;justify-content:center}button{font:inherit;font-weight:600;color:#fff;background:#0a66ff;border:0;border-radius:12px;padding:16px 28px}</style>
<button id="go" type="button">Open in App Store</button>{{end}}
<script>
var delay = {{.Delay}}, left = false;
document.addEventListener("visibilitychange", function () { if (document.visibilityState === "hidden") left = true; });
window.addEventListener("pagehide", function () { left = true; });
function buildQuery(pasted) {
  var q = new URLSearchParams(location.search);
  q.set("_dt", "1");
  q.set("screen", screen.width + "x" + screen.height + "@" + (window.devicePixelRatio || 1));
  try { q.set("tz", Intl.DateTimeFormat().resolvedOptions().timeZone || ""); } catch (e) {}
  if (pasted) q.set("pasted_link", pasted);
  if (navigator.maxTouchPoints > 1) q.set("_touch", "1");
  return q;
}
function go(pasted) {
  var q = buildQuery(pasted), t0 = Date.now();
  if (delay) setTimeout(function () {
    if (left || document.visibilityState !== "visible" || Date.now() - t0 > delay + 1000) return;
    q.set("_tap", "1");
    location.replace(location.pathname + "?" + q.toString());
  }, delay);
  location.replace(location.pathname + "?" + q.toString());
}
` + copyJS + `
{{if .Copy}}document.getElementById("go").onclick = function () {
  var link = location.origin + location.pathname + {{.Paste}};
  go(copy(link) ? link : "");
};{{else}}go("");{{end}}
</script>`))

// safetyNetDelay: ms before hop-1 page assumes blocked hand-off. Generic webviews 1.5s; real-browser UAs 4s (slow hop 2 != block); recognized in-app 0 (hop 2 serves tap page).
func safetyNetDelay(source string) int {
	switch {
	case source == ua.SourceUnknownInApp:
		return 1500
	case source != "":
		return 0
	}
	return 4000
}

// isAppStoreURL: s parses as App Store URL.
func isAppStoreURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && isAppStore(u)
}

// isPlayStoreURL: s parses as Play Store URL.
func isPlayStoreURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && isPlayStore(u)
}

// serveInterstitial: one-hop page; copy = tap-to-copy (iOS App Store targets), else auto reload. delay: safety-net ms (0 = off).
func serveInterstitial(w http.ResponseWriter, q url.Values, copy bool, delay int) {
	next := url.Values{}
	for k, v := range q {
		next[k] = v
	}
	next.Set(paramDone, "1")
	htmlNoStore(w)
	w.Header().Set("Accept-CH", "Sec-CH-UA-Model, Sec-CH-UA-Platform-Version")
	paste := ""
	if copy {
		paste = store.PasteQuery() // exact match when the app reads this copy (pasteToken)
	}
	_ = interstitialTmpl.Execute(w, struct {
		Next  string
		Copy  bool
		Delay int
		Paste string
	}{"?" + next.Encode(), copy, delay, paste})
}

// htmlNoStore: headers shared by every detur HTML page (never cached: each hop is per visitor).
func htmlNoStore(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Frame-Options", "DENY") // no clickjacking overlay on the tap/copy pages
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
}

// fingerprint: click-time device signals: IP, device model (client hint, else UA), OS version hint, first Accept-Language tag, UA, screen/timezone/pasted_link from interstitial.
func fingerprint(r *http.Request, q url.Values, link store.Link) store.Fingerprint {
	locale := primaryLang(r.Header.Get("Accept-Language"))
	device := hint(r, "Sec-CH-UA-Model")
	if device == "" {
		device = ua.DeviceLabel(r.UserAgent())
	}
	return store.Fingerprint{
		IP:         httpx.RemoteIP(r),
		Device:     device,
		Locale:     locale,
		Timezone:   validTZ(q.Get(paramTimezone)),
		Screen:     validScreen(q.Get(paramScreen)),
		UserAgent:  r.UserAgent(),
		OSVersion:  hint(r, "Sec-CH-UA-Platform-Version"),
		PastedLink: pastedFor(q.Get(paramPasted), r, link.Key),
	}
}

var (
	reTimezone = regexp.MustCompile(`^[A-Za-z0-9_+\-/]+$`)
	reScreen   = regexp.MustCompile(`^\d{1,5}x\d{1,5}@\d{1,2}(\.\d{1,4})?$`)
)

// validTZ: IANA-style timezone id, else dropped.
func validTZ(v string) string {
	if len(v) > 64 || !reTimezone.MatchString(v) {
		return ""
	}
	return v
}

// validScreen: "WxH@scale", else dropped.
func validScreen(v string) string {
	if !reScreen.MatchString(v) {
		return ""
	}
	return v
}

// pastedFor: pasted_link counts only if it is the clicked short link (Detour compares with click URL); else dropped.
func pastedFor(raw string, r *http.Request, key string) string {
	u, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(u.Host, r.Host) || !strings.EqualFold(strings.Trim(u.Path, "/"), key) {
		return ""
	}
	return raw
}

// pasteToken: `_dc` token of a validated pasted link (pastedFor), "" when none.
func pasteToken(pasted string) string {
	_, t := store.PasteToken(pasted)
	return t
}

// hint: structured-header string client hint ("\"Pixel 7\"").
func hint(r *http.Request, name string) string {
	v := strings.TrimSpace(r.Header.Get(name))
	if s, err := strconv.Unquote(v); err == nil {
		return strings.TrimSpace(s)
	}
	return v
}
