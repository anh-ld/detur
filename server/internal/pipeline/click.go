// Package pipeline serves browser side of deferred deep-link funnel: GET
// /{key} short links resolve globally, record click + fingerprint, 302 to
// platform store or fallback, cloning Dub's link.ts middleware semantics.
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

// Interstitial-appended query params (internal, never forwarded).
const (
	paramDone     = "_dt"
	paramScreen   = "screen"
	paramTimezone = "tz"
	paramPasted   = "pasted_link"
	paramNoTrack  = "detur-no-track" // Dub's dub-no-track
	paramTap      = "_tap"           // safety-net reload: hop 2 answers with the tap page
	paramTouch    = "_touch"         // interstitial saw a touch screen (iPadOS Safari behind a Mac UA)
)

type pipelineServer struct {
	st             *store.Store
	log            *log.Logger
	retentionHours int // click retention floor, threaded from config
}

// Register attaches browser pipeline to mux: GET /{key} short-link serving; /health and /api/* more specific, keep winning (stdlib routing).
func Register(mux *http.ServeMux, st *store.Store, retentionHours int) {
	p := &pipelineServer{st: st, log: log.Default(), retentionHours: retentionHours}
	mux.HandleFunc("GET /{key}", p.handleShort)
}

// handleShort: short link — resolve key globally, record click (bot-filtered),
// 302 to store or fallback. Mobile browsers first get one-hop interstitial
// reading screen + timezone (Dub's deeplink preview does same). Record failure logged, never blocks redirect.
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
	// iPadOS Safari sends a Mac UA. Interstitial reported touch -> rewrite UA to iPad for everything after
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
	track := !ua.IsBot(r) && !noTrackQ && !noTrackH // presence, not value (Dub record-click.ts has())
	mobile := ua.IsIOS(agent) || ua.IsAndroid(agent)
	trackedMobile := track && mobile
	source := "" // in-app browser that sent the tap (ua.InApp)
	if trackedMobile {
		source = ua.InApp(agent)
	}
	named := source != "" && source != ua.SourceUnknownInApp
	if track && (mobile || ua.MaybeIPad(agent)) && q.Get(paramDone) == "" {
		// recognized in-app: auto reload, never the copy page — hop 2 serves the tap page
		serveInterstitial(w, q, ua.IsIOS(agent) && isAppStoreURL(effectiveLink.IOS) && !named, safetyNetDelay(source))
		return
	}
	// clickId minted first, embedded in redirect (Dub link.ts). Click stores link.URL + params as deferred destination, not store URL. Dedup hit returns earlier click's id
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
			Source:  source,
			Variant: route.Variant,
		}, p.retentionHours)
		if err != nil {
			p.log.Printf("click record failed (redirect continues): %v", err)
			dest = redirectTarget(effectiveLink, agent, "", q) // no recorded click -> no referrer
		} else if rec.ID != clickID {
			dest = redirectTarget(effectiveLink, agent, rec.ID, q)
		}
	}
	// in-app browsers block the automatic hand-off: recognized ones, and the safety-net reload, get a link to tap instead
	if _, tap := q[paramTap]; trackedMobile && (named || tap) && hasStoreTarget(effectiveLink, agent) {
		p.serveTap(w, r, effectiveLink, dest, source, q)
		return
	}
	http.Redirect(w, r, dest, http.StatusFound)
}

// interstitialTmpl: reloads short link once with screen + timezone (+ _touch on touch screens) appended (no-JS browsers fall through with neither); Accept-CH: Chromium sends device model + OS version on reload.
// Copy mode (iOS + App Store target): waits for a tap, copies short link to pasteboard (needs user gesture), reloads with pasted_link in the same tap. SDK reads pasteboard on first launch: pasteboard signal (350/175).
// Safety net: page still visible after delay ms = the browser blocked the hand-off -> reload with _tap, hop 2 serves the tap page. Hidden/pagehide or a late timer (frozen in background) cancels; 0 = off.
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
  var link = location.origin + location.pathname;
  go(copy(link) ? link : "");
};{{else}}go("");{{end}}
</script>`))

// safetyNetDelay: ms before the hop-1 page assumes a blocked hand-off. Generic webviews 1.5s; real-browser UAs 4s so a slow hop 2 is not mistaken for a block; recognized in-app 0 (hop 2 serves the tap page).
func safetyNetDelay(source string) int {
	switch {
	case source == ua.SourceUnknownInApp:
		return 1500
	case source != "":
		return 0
	}
	return 4000
}

// isAppStoreURL: s parses as an App Store URL
func isAppStoreURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && isAppStore(u)
}

// isPlayStoreURL: s parses as a Play Store URL
func isPlayStoreURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && isPlayStore(u)
}

// serveInterstitial: one-hop page; copy = tap-to-copy page (iOS App Store targets), else auto reload. delay: safety-net ms (0 = off).
func serveInterstitial(w http.ResponseWriter, q url.Values, copy bool, delay int) {
	next := url.Values{}
	for k, v := range q {
		next[k] = v
	}
	next.Set(paramDone, "1")
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("Accept-CH", "Sec-CH-UA-Model, Sec-CH-UA-Platform-Version")
	_ = interstitialTmpl.Execute(w, struct {
		Next  string
		Copy  bool
		Delay int
	}{"?" + next.Encode(), copy, delay})
}

// fingerprint: click-time device signals — IP, device model (client hint, else UA), OS version hint, first Accept-Language tag, user-agent, screen/timezone/pasted_link from interstitial
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

// pastedFor: interstitial's pasted_link counts only when it is the clicked short link itself (Detour compares with the click URL). Anything else dropped.
func pastedFor(raw string, r *http.Request, key string) string {
	u, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(u.Host, r.Host) || !strings.EqualFold(strings.Trim(u.Path, "/"), key) {
		return ""
	}
	return raw
}

// hint: structured-header string client hint ("\"Pixel 7\"").
func hint(r *http.Request, name string) string {
	v := strings.TrimSpace(r.Header.Get(name))
	if s, err := strconv.Unquote(v); err == nil {
		return strings.TrimSpace(s)
	}
	return v
}
