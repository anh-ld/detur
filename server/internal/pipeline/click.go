// Package pipeline serves the browser side of the deferred deep-link funnel:
// GET /{key} short links resolve globally, record a click + fingerprint, and
// 302 to the platform store or fallback, cloning Dub's link.ts middleware
// semantics.
package pipeline

import (
	"errors"
	"html/template"
	"log"
	"net/http"
	"net/url"
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

// Register attaches the browser pipeline to mux: GET /{key} short-link
// serving. /health and /api/* are more specific patterns and keep winning
// (stdlib routing).
func Register(mux *http.ServeMux, st *store.Store, retentionHours int) {
	p := &pipelineServer{st: st, log: log.Default(), retentionHours: retentionHours}
	mux.HandleFunc("GET /{key}", p.handleShort)
}

// handleShort: short link — resolve key globally, record click
// (bot-filtered), 302 to store or fallback. Mobile browsers first get a
// one-hop interstitial reading screen + timezone (Dub's deeplink preview
// does the same). Record failure logged, never blocks the redirect.
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
	track := !ua.IsBot(r) && q.Get(paramNoTrack) == "" && r.Header.Get(paramNoTrack) == ""
	mobile := ua.IsIOS(agent) || ua.IsAndroid(agent)
	if track && mobile && q.Get(paramDone) == "" {
		serveInterstitial(w, q)
		return
	}
	// Dub mints clickId first, embeds in final URL: click's destination IS
	// the redirect target (link.ts getFinalUrl). Dedup hit hands back an
	// earlier click with its own id.
	clickID := ""
	if track {
		clickID = store.Nanoid(16)
	}
	dest := redirectTarget(link, agent, clickID, q)
	if track {
		rec, err := p.st.RecordClick(store.Click{
			ID: clickID, AppID: link.AppID, LinkID: link.ID, Destination: dest,
			Fingerprint: fingerprint(r, q),
		}, link.WindowMinutes, p.retentionHours)
		if err != nil {
			p.log.Printf("click record failed (redirect continues): %v", err)
			dest = redirectTarget(link, agent, "", q) // no recorded click -> no referrer
		} else if rec.ID != clickID {
			dest = redirectTarget(link, agent, rec.ID, q)
		}
	}
	http.Redirect(w, r, dest, http.StatusFound)
}

// interstitialTmpl: reloads the short link once with screen + timezone
// appended (no-JS browsers fall through with neither). Accept-CH: Chromium
// sends device model + OS version on the reload.
var interstitialTmpl = template.Must(template.New("i").Parse(`<!doctype html>
<meta charset="utf-8"><meta name="robots" content="noindex"><meta name="viewport" content="width=device-width">
<noscript><meta http-equiv="refresh" content="0;url={{.}}"></noscript>
<script>
var q = new URLSearchParams(location.search);
q.set("_dt", "1");
q.set("screen", screen.width + "x" + screen.height + "@" + (window.devicePixelRatio || 1));
try { q.set("tz", Intl.DateTimeFormat().resolvedOptions().timeZone || ""); } catch (e) {}
location.replace(location.pathname + "?" + q.toString());
</script>`))

func serveInterstitial(w http.ResponseWriter, q url.Values) {
	next := url.Values{}
	for k, v := range q {
		next[k] = v
	}
	next.Set(paramDone, "1")
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("Accept-CH", "Sec-CH-UA-Model, Sec-CH-UA-Platform-Version")
	_ = interstitialTmpl.Execute(w, "?"+next.Encode())
}

// fingerprint: click-time device signals — IP, device model (client hint,
// else UA), OS version hint, first Accept-Language tag, user-agent,
// screen/timezone/pasted_link from the interstitial.
func fingerprint(r *http.Request, q url.Values) store.Fingerprint {
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
		Timezone:   q.Get(paramTimezone),
		Screen:     q.Get(paramScreen),
		UserAgent:  r.UserAgent(),
		OSVersion:  hint(r, "Sec-CH-UA-Platform-Version"),
		PastedLink: q.Get(paramPasted),
	}
}

// hint: structured-header string client hint ("\"Pixel 7\"").
func hint(r *http.Request, name string) string {
	v := strings.TrimSpace(r.Header.Get(name))
	if s, err := strconv.Unquote(v); err == nil {
		return strings.TrimSpace(s)
	}
	return v
}
