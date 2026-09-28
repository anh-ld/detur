// Package pipeline serves the browser side of the deferred deep-link funnel
// (U4): GET /{key} short links resolve globally, record a click + fingerprint
// (R9), and 302 to the platform store or fallback (R10), cloning Dub's link.ts
// middleware semantics (KD4).
package pipeline

import (
	"errors"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"

	"detur.dev/server/internal/store"
)

// retentionFloorHours is the click retention floor (KTD4): a recorded click
// never expires before 24h, so deterministic clickId lookups survive beyond
// the probabilistic window.
const retentionFloorHours = 24

// botMarkers is the cheap Dub-style UA bot filter (cloned from record-click,
// KD4): any UA containing one of these markers skips click recording.
var botMarkers = []string{"bot", "spider", "crawler", "preview", "facebookexternalhit", "slackbot", "twitterbot", "whatsapp"}

type pipelineServer struct {
	st  *store.Store
	log *log.Logger
}

// Register attaches the browser pipeline to mux: GET /{key} short-link
// serving. /health and /api/* are more specific patterns and keep winning
// (KTD2 stdlib routing).
func Register(mux *http.ServeMux, st *store.Store) {
	p := &pipelineServer{st: st, log: log.Default()}
	mux.HandleFunc("GET /{key}", p.handleShort)
}

// handleShort serves a short link: resolve the key globally, record the click
// (R8/R9, bot-filtered), then 302 to the store or fallback (R10). The click
// row is written BEFORE the redirect; a record failure is logged and never
// blocks the redirect.
func (p *pipelineServer) handleShort(w http.ResponseWriter, r *http.Request) {
	link, err := p.st.GetLinkByKeyGlobal(r.PathValue("key"))
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		p.log.Printf("short-link lookup failed for %q: %v", r.PathValue("key"), err)
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	q := r.URL.Query()
	clickID := ""
	ua := r.UserAgent()
	if !isBotUA(ua) {
		rec, err := p.st.RecordClick(store.Click{
			AppID: link.AppID, LinkID: link.ID, Destination: link.URL,
			Fingerprint: fingerprint(r, q),
		}, link.WindowMinutes, retentionFloorHours)
		if err != nil {
			p.log.Printf("click record failed (redirect continues): %v", err)
		} else {
			clickID = rec.ID // Android Play install referrer (R10)
		}
	}
	http.Redirect(w, r, redirectTarget(link, ua, clickID, q), http.StatusFound)
}

// fingerprint captures the click-time device signals (R8): IP, UA-derived
// device, first Accept-Language tag, user-agent, and screen/pasted_link when
// the link page appended them. Timezone is not derivable server-side; it is
// simply absent (scored 0 by the matching engine).
func fingerprint(r *http.Request, q url.Values) store.Fingerprint {
	locale := ""
	if al := r.Header.Get("Accept-Language"); al != "" {
		if i := strings.IndexByte(al, ','); i >= 0 {
			al = al[:i]
		}
		locale = strings.TrimSpace(al)
	}
	return store.Fingerprint{
		IP:         remoteIP(r),
		Device:     deviceFromUA(r.UserAgent()),
		Locale:     locale,
		UserAgent:  r.UserAgent(),
		Screen:     q.Get("screen"),
		PastedLink: q.Get("pasted_link"),
	}
}

// remoteIP returns the request connection IP, honoring X-Forwarded-For (first
// entry) for the reverse-proxy deployment (TLS termination requirement).
func remoteIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.IndexByte(xff, ','); i >= 0 {
			xff = xff[:i]
		}
		return strings.TrimSpace(xff)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// isBotUA applies the cheap Dub bot filter (KD4): a UA containing any known
// bot marker skips click recording but still gets the redirect.
func isBotUA(ua string) bool {
	ua = strings.ToLower(ua)
	for _, m := range botMarkers {
		if strings.Contains(ua, m) {
			return true
		}
	}
	return false
}

// deviceFromUA derives a device label using the same parsing the match
// package uses (replicated here; its helpers are unexported): Android model
// "Pixel 7", iOS "iPhone"/"iPad"/"iPod".
func deviceFromUA(ua string) string {
	switch {
	case strings.Contains(ua, "Android"):
		return androidModel(ua)
	case strings.Contains(ua, "iPhone"):
		return "iPhone"
	case strings.Contains(ua, "iPad"):
		return "iPad"
	case strings.Contains(ua, "iPod"):
		return "iPod"
	}
	return ""
}

// androidModel mirrors the match package's parser: the model token after the
// Android version in "(Linux; Android 14; Pixel 7 Build/TQ3A.230805.001)".
func androidModel(ua string) string {
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
