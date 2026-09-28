// Package pipeline serves the browser side of the deferred deep-link funnel
// (U4): GET /{key} short links resolve globally, record a click + fingerprint
// (R9), and 302 to the platform store or fallback (R10), cloning Dub's link.ts
// middleware semantics (KD4).
package pipeline

import (
	"errors"
	"log"
	"net/http"
	"net/url"
	"strings"

	"detur.dev/server/internal/httpx"
	"detur.dev/server/internal/store"
	"detur.dev/server/internal/ua"
)

type pipelineServer struct {
	st             *store.Store
	log            *log.Logger
	retentionHours int // click retention floor (KTD4), threaded from config
}

// Register attaches the browser pipeline to mux: GET /{key} short-link
// serving. /health and /api/* are more specific patterns and keep winning
// (KTD2 stdlib routing).
func Register(mux *http.ServeMux, st *store.Store, retentionHours int) {
	p := &pipelineServer{st: st, log: log.Default(), retentionHours: retentionHours}
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
	if agent := r.UserAgent(); !ua.IsBotUA(agent) {
		rec, err := p.st.RecordClick(store.Click{
			AppID: link.AppID, LinkID: link.ID, Destination: link.URL,
			Fingerprint: fingerprint(r, q),
		}, link.WindowMinutes, p.retentionHours)
		if err != nil {
			p.log.Printf("click record failed (redirect continues): %v", err)
		} else {
			clickID = rec.ID // Android Play install referrer (R10)
		}
	}
	http.Redirect(w, r, redirectTarget(link, r.UserAgent(), clickID, q), http.StatusFound)
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
		IP:         httpx.RemoteIP(r),
		Device:     ua.DeviceLabel(r.UserAgent()),
		Locale:     locale,
		UserAgent:  r.UserAgent(),
		Screen:     q.Get("screen"),
		PastedLink: q.Get("pasted_link"),
	}
}
