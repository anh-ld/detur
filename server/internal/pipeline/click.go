// Package pipeline: browser side of deferred deep-link funnel. GET /{key}
// resolves globally, records click + fingerprint, 302s to store or fallback
package pipeline

import (
	"errors"
	"html/template"
	"log"
	"maps"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"detur.dev/server/internal/fraud"
	"detur.dev/server/internal/httpx"
	"detur.dev/server/internal/page"
	"detur.dev/server/internal/store"
	"detur.dev/server/internal/ua"
)

type pipelineServer struct {
	st             *store.Store
	log            *log.Logger
	retentionHours int
}

func Register(mux *http.ServeMux, st *store.Store, retentionHours int) {
	p := &pipelineServer{st: st, log: log.Default(), retentionHours: retentionHours}
	mux.HandleFunc("GET /{key}", p.handleShort)
	mux.HandleFunc("GET /{key}/{$}", func(w http.ResponseWriter, r *http.Request) {
		target := "/" + url.PathEscape(r.PathValue("key"))
		if r.URL.RawQuery != "" {
			target += "?" + r.URL.RawQuery
		}
		http.Redirect(w, r, target, http.StatusMovedPermanently)
	})
}

// stripOpenApp: Detour's #detour_open_app=true flag. Instagram iOS percent-encodes it into the path or a query value
// (/key%23detour_open_app%3Dtrue?utm_…) → 404 / polluted param. Drop it; the app never sees it (Detour does the same).
func stripOpenApp(r *http.Request) string {
	key, _, _ := strings.Cut(r.PathValue("key"), "#")
	if strings.Contains(r.URL.RawQuery, "%23detour_open_app") {
		q := r.URL.Query()
		delete(q, "#detour_open_app")
		for _, vs := range q {
			for i, v := range vs {
				vs[i] = strings.Replace(v, "#detour_open_app=true", "", 1)
			}
		}
		r.URL.RawQuery = q.Encode()
	}
	return key
}

func (p *pipelineServer) handleShort(w http.ResponseWriter, r *http.Request) {
	key := stripOpenApp(r)
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
	q := r.URL.Query()
	if q.Get(store.ParamTouch) == "1" && ua.MaybeIPad(r.UserAgent()) {
		r.Header.Set("User-Agent", strings.Replace(r.UserAgent(), "Macintosh", "iPad", 1))
	}
	rules, err := p.st.GetLinkRules(link.ID)
	if err != nil {
		p.log.Printf("get link rules failed for %q (%s): %v", key, link.ID, err)
		rules = nil
	}
	route := evaluateRules(rules, link, r, q, time.Now())
	effectiveLink := link
	effectiveLink.URL = route.Destination
	effectiveLink.IOS = route.IOS
	effectiveLink.Android = route.Android
	effectiveLink.FallbackURL = route.FallbackURL

	agent := r.UserAgent()
	_, noTrackQ := q[store.ParamNoTrack]
	_, noTrackH := r.Header[http.CanonicalHeaderKey(store.ParamNoTrack)]
	bot := ua.IsBotQuery(r, q)
	track := !bot && !noTrackQ && !noTrackH
	platform := ua.ClickPlatform(agent)
	mobile := platform != ua.PlatformDesktop
	trackedMobile := track && mobile
	source := ""
	if trackedMobile {
		source = ua.InApp(agent)
	}
	named := source != "" && source != ua.SourceUnknownInApp
	if r.Method == http.MethodGet && bot && link.OGTitle+link.OGDescription+link.OGImage != "" {
		serveOG(w, r, link, redirectTarget(effectiveLink, platform, "", q), q)
		return
	}
	if track && (mobile || ua.MaybeIPad(agent)) && q.Get(store.ParamDone) == "" {
		if platform == ua.PlatformIOS && isAppStoreURL(effectiveLink.IOS) && !named {
			p.serveCopyPage(w, effectiveLink, q, safetyNetDelay(source))
		} else {
			serveInterstitial(w, q, safetyNetDelay(source))
		}
		return
	}
	clickID := ""
	if track {
		clickID = store.Nanoid(16)
	}
	dest := redirectTarget(effectiveLink, platform, clickID, q)
	if track {
		fp := fingerprint(r, q, link)
		rec, err := p.st.RecordClick(store.Click{
			ID: clickID, AppID: link.AppID, LinkID: link.ID, Destination: deepLinkURL(effectiveLink, q),
			Fingerprint: fp, Platform: platform, Kind: clickKind(dest),
			UASuspect: fraud.SuspectUA(agent), IPHosting: fraud.Hosting(fp.IP),
			Source:     source,
			Variant:    route.Variant,
			PasteToken: pasteToken(fp.PastedLink),
		}, p.retentionHours)
		if err != nil {
			p.log.Printf("click record failed (redirect continues): %v", err)
			dest = redirectTarget(effectiveLink, platform, "", q)
		} else if rec.ID != clickID {
			dest = redirectTarget(effectiveLink, platform, rec.ID, q)
		}
	}
	// in-app browsers block auto hand-off: recognized ones + safety-net reload get a tap link instead
	if _, tap := q[store.ParamTap]; trackedMobile && (named || tap) && hasStoreTarget(effectiveLink, platform) {
		p.serveTap(w, r, effectiveLink, dest, source, q)
		return
	}
	if httpx.IsRoute(dest) || (!mobile && !bot && !isWebURL(dest)) {
		p.serveDesktop(w, r, effectiveLink, q, mobile)
		return
	}
	http.Redirect(w, r, dest, http.StatusFound)
}

var interstitialTmpl = template.Must(template.New("i").Parse(`<!doctype html>
<meta charset="utf-8"><meta name="robots" content="noindex"><meta name="viewport" content="width=device-width,initial-scale=1">
<noscript><meta http-equiv="refresh" content="0;url={{.Next}}"></noscript>
<script>
var delay = {{.Delay}}, left = false;
` + reloadJS + `
go("");
</script>`))

const reloadJS = `document.addEventListener("visibilitychange", function () { if (document.visibilityState === "hidden") left = true; });
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
}`

func safetyNetDelay(source string) int {
	switch {
	case source == ua.SourceUnknownInApp:
		return 1500
	case source != "":
		return 0
	}
	return 4000
}

func isAppStoreURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && isAppStore(u)
}

func isPlayStoreURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && isPlayStore(u)
}

func nextQuery(q url.Values) string {
	next := maps.Clone(q)
	next.Set(store.ParamDone, "1")
	return "?" + next.Encode()
}

func serveInterstitial(w http.ResponseWriter, q url.Values, delay int) {
	htmlNoStore(w)
	w.Header().Set("Accept-CH", "Sec-CH-UA-Model, Sec-CH-UA-Platform-Version")
	_ = interstitialTmpl.Execute(w, struct {
		Next  string
		Delay int
	}{nextQuery(q), delay})
}

func (p *pipelineServer) serveCopyPage(w http.ResponseWriter, link store.Link, q url.Values, delay int) {
	app := p.appOrDefault(link, "copy page")
	servePage(w, app.PageHTML, page.Input{
		Copy: true, IOS: true, Href: nextQuery(q), Delay: delay, Web: webTarget(link, q),
		Paste: store.PasteQuery(),
		Icon:  appIcons.first(iconKeys(link.IOS, link.Android, app.AndroidPackage)), AppName: app.Name,
	})
}

func htmlNoStore(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Frame-Options", "DENY")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
}

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
		Timezone:   validTZ(q.Get(store.ParamTimezone)),
		Screen:     validScreen(q.Get(store.ParamScreen)),
		UserAgent:  r.UserAgent(),
		OSVersion:  hint(r, "Sec-CH-UA-Platform-Version"),
		PastedLink: pastedFor(q.Get(store.ParamPasted), r, link.Key),
	}
}

var (
	reTimezone = regexp.MustCompile(`^[A-Za-z0-9_+\-/]+$`)
	reScreen   = regexp.MustCompile(`^\d{1,5}x\d{1,5}@\d{1,2}(\.\d{1,4})?$`)
)

func validTZ(v string) string {
	if len(v) > 64 || !reTimezone.MatchString(v) {
		return ""
	}
	return v
}

func validScreen(v string) string {
	if !reScreen.MatchString(v) {
		return ""
	}
	return v
}

func pastedFor(raw string, r *http.Request, key string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	path, _, _ := strings.Cut(strings.Trim(u.Path, "/"), "#") // flagged path, see stripOpenApp
	if !strings.EqualFold(u.Host, r.Host) || !strings.EqualFold(path, key) {
		return ""
	}
	return raw
}

func pasteToken(pasted string) string {
	_, t := store.PasteToken(pasted)
	return t
}

func hint(r *http.Request, name string) string {
	v := strings.TrimSpace(r.Header.Get(name))
	if s, err := strconv.Unquote(v); err == nil {
		return strings.TrimSpace(s)
	}
	return v
}
