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
	q := r.URL.Query()
	agent := r.UserAgent()
	_, noTrackQ := q[paramNoTrack]
	_, noTrackH := r.Header[http.CanonicalHeaderKey(paramNoTrack)]
	track := !ua.IsBot(r) && !noTrackQ && !noTrackH // presence, not value (Dub record-click.ts has())
	mobile := ua.IsIOS(agent) || ua.IsAndroid(agent)
	if track && mobile && q.Get(paramDone) == "" {
		serveInterstitial(w, q, ua.IsIOS(agent) && isAppStoreURL(link.IOS))
		return
	}
	// clickId minted first, embedded in redirect (Dub link.ts). Click stores link.URL + params as deferred destination, not store URL. Dedup hit returns earlier click's id
	clickID := ""
	if track {
		clickID = store.Nanoid(16)
	}
	dest := redirectTarget(link, agent, clickID, q)
	if track {
		rec, err := p.st.RecordClick(store.Click{
			ID: clickID, AppID: link.AppID, LinkID: link.ID, Destination: deepLinkURL(link, q),
			Fingerprint: fingerprint(r, q, link),
		}, p.retentionHours)
		if err != nil {
			p.log.Printf("click record failed (redirect continues): %v", err)
			dest = redirectTarget(link, agent, "", q) // no recorded click -> no referrer
		} else if rec.ID != clickID {
			dest = redirectTarget(link, agent, rec.ID, q)
		}
	}
	http.Redirect(w, r, dest, http.StatusFound)
}

// interstitialTmpl: reloads short link once with screen + timezone appended (no-JS browsers fall through with neither); Accept-CH: Chromium sends device model + OS version on reload.
// Copy mode (iOS + App Store target): waits for a tap, copies short link to pasteboard (needs user gesture), reloads with pasted_link. SDK reads pasteboard on first launch: pasteboard signal (350/175).
var interstitialTmpl = template.Must(template.New("i").Parse(`<!doctype html>
<meta charset="utf-8"><meta name="robots" content="noindex"><meta name="viewport" content="width=device-width,initial-scale=1">
<noscript><meta http-equiv="refresh" content="0;url={{.Next}}"></noscript>
{{if .Copy}}<style>body{font:17px -apple-system,system-ui,sans-serif;margin:0;min-height:100vh;display:flex;align-items:center;justify-content:center}button{font:inherit;font-weight:600;color:#fff;background:#0a66ff;border:0;border-radius:12px;padding:16px 28px}</style>
<button id="go" type="button">Open in App Store</button>{{end}}
<script>
function go(pasted) {
  var q = new URLSearchParams(location.search);
  q.set("_dt", "1");
  q.set("screen", screen.width + "x" + screen.height + "@" + (window.devicePixelRatio || 1));
  try { q.set("tz", Intl.DateTimeFormat().resolvedOptions().timeZone || ""); } catch (e) {}
  if (pasted) q.set("pasted_link", pasted);
  location.replace(location.pathname + "?" + q.toString());
}
function copy(text) {
  if (navigator.clipboard && navigator.clipboard.writeText) return navigator.clipboard.writeText(text);
  var t = document.createElement("textarea");
  t.value = text; t.setAttribute("readonly", ""); t.style.position = "fixed"; t.style.opacity = "0";
  document.body.appendChild(t); t.select();
  var ok = document.execCommand("copy");
  document.body.removeChild(t);
  return ok ? Promise.resolve() : Promise.reject();
}
{{if .Copy}}document.getElementById("go").onclick = function () {
  var link = location.origin + location.pathname;
  copy(link).then(function () { go(link); }, function () { go(""); });
};{{else}}go("");{{end}}
</script>`))

// isAppStoreURL: s parses as an App Store URL
func isAppStoreURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && isAppStore(u)
}

// serveInterstitial: one-hop page; copy = tap-to-copy page (iOS App Store targets), else auto reload.
func serveInterstitial(w http.ResponseWriter, q url.Values, copy bool) {
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
		Next string
		Copy bool
	}{"?" + next.Encode(), copy})
}

// fingerprint: click-time device signals — IP, device model (client hint, else UA), OS version hint, first Accept-Language tag, user-agent, screen/timezone/pasted_link from interstitial
func fingerprint(r *http.Request, q url.Values, link store.Link) store.Fingerprint {
	locale := ""
	if al := r.Header.Get("Accept-Language"); al != "" {
		if i := strings.IndexByte(al, ','); i >= 0 {
			al = al[:i]
		}
		if i := strings.IndexByte(al, ';'); i >= 0 {
			al = al[:i]
		}
		locale = strings.TrimSpace(al)
	}
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
