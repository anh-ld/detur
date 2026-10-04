// Package api serves Detour SDK endpoints: match-link, resolve-short,
// universal-link-click, two analytics calls, with godetour.dev compatible
// shapes, SDK header auth, fail-open behavior.
package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"detur.dev/server/internal/httpx"
	"detur.dev/server/internal/match"
	"detur.dev/server/internal/store"
	"detur.dev/server/internal/ua"
)

// RegisterSDK attaches five SDK endpoints to mux (stdlib method patterns). Auth wrapper validates
// Bearer key + X-App-ID + X-SDK presence; universal-link-click fails open when auth uncheckable,
// backend failure never blocks link. retentionHours: click retention floor threaded from config.
func RegisterSDK(mux *http.ServeMux, st *store.Store, retentionHours int) {
	s := &sdkServer{st: st, log: log.Default(), retentionHours: retentionHours}
	mux.HandleFunc("POST /api/link/match-link", s.requireAuth(s.matchLink, false))
	mux.HandleFunc("POST /api/link/resolve-short", s.requireAuth(s.resolveShort, false))
	mux.HandleFunc("POST /api/link/universal-link-click", s.requireAuth(s.universalLinkClick, true))
	mux.HandleFunc("POST /api/analytics/event", s.requireAuth(s.analyticsEvent, false))
	mux.HandleFunc("POST /api/analytics/retention", s.requireAuth(s.analyticsRetention, false))
}

type sdkServer struct {
	st             *store.Store
	log            *log.Logger
	retentionHours int
}

// requireAuth enforces SDK header contract: Authorization Bearer + X-App-ID + X-SDK present, apiKey valid for app. failOpen endpoints (universal-link-click) proceed when store unqueryable.
func (s *sdkServer) requireAuth(next http.HandlerFunc, failOpen bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key, ok := bearerKey(r.Header.Get("Authorization"))
		if !ok || r.Header.Get("X-App-ID") == "" || r.Header.Get("X-SDK") == "" {
			httpx.WriteJSON(w, http.StatusUnauthorized, errorBody("unauthorized"))
			return
		}
		valid, err := s.st.ValidateAPIKey(r.Header.Get("X-App-ID"), key)
		if err != nil {
			if failOpen {
				s.log.Printf("auth check failed, failing open: %v", err)
				writeAllow(w, store.Nanoid(16)) // app id unverified: answer, never write
				return
			}
			if r.URL.Path == "/api/link/match-link" {
				// match-link backend error = no-match, never deny. Body unread here, no device hash for unknown row.
				s.log.Printf("match-link auth backend error, returning no-match: %v", err)
				w.WriteHeader(http.StatusNotFound)
				return
			}
			s.log.Printf("auth check backend error: %v", err)
			httpx.WriteJSON(w, http.StatusInternalServerError, errorBody("internal error"))
			return
		}
		if !valid {
			httpx.WriteJSON(w, http.StatusUnauthorized, errorBody("unauthorized"))
			return
		}
		next(w, r)
	}
}

// matchLinkBody: union of deterministic (clickId) + probabilistic (fingerprint) match-link payloads (getDeferredLink.ts). locale = SDK's [{languageTag}...] array; timestamp (Unix ms) sets the match window reference, excluded from device hash (not a device characteristic, would break its stability).
type matchLinkBody struct {
	ClickID       string      `json:"clickId"`
	Model         string      `json:"model"`
	Manufacturer  string      `json:"manufacturer"`
	SystemVersion string      `json:"systemVersion"`
	ScreenWidth   int         `json:"screenWidth"`
	ScreenHeight  int         `json:"screenHeight"`
	Scale         float64     `json:"scale"`
	Locale        []localeTag `json:"locale"`
	Timezone      *string     `json:"timezone"`
	UserAgent     string      `json:"userAgent"`
	PastedLink    string      `json:"pastedLink"`
	Timestamp     int64       `json:"timestamp"`
}

type localeTag struct {
	LanguageTag string `json:"languageTag"`
}

// matchLink serves POST /api/link/match-link: deterministic clickId lookup or probabilistic fingerprint scoring; 200 {"link": destination} on match, 404 on no-match (SDK reads organic -> null), fail-open 404 + unknown-attribution row on backend error.
func (s *sdkServer) matchLink(w http.ResponseWriter, r *http.Request) {
	appID := r.Header.Get("X-App-ID")
	var body matchLinkBody
	if err := decodeJSON(w, r, &body); err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, errorBody("invalid request body"))
		return
	}
	dh := deviceHash(body)

	req := match.Request{IP: httpx.RemoteIP(r), DeviceHash: dh}
	if body.ClickID != "" {
		req.ClickID = body.ClickID
	} else {
		req.Fingerprint = &match.Fingerprint{
			Model: body.Model, Manufacturer: body.Manufacturer,
			SystemVersion: body.SystemVersion, ScreenWidth: body.ScreenWidth, ScreenHeight: body.ScreenHeight,
			Scale: body.Scale, Locale: localeString(body.Locale), Timezone: strOr(body.Timezone),
			UserAgent: body.UserAgent, PastedLink: body.PastedLink,
		}
		if body.Timestamp > 0 {
			req.Fingerprint.CapturedAt = time.UnixMilli(body.Timestamp)
		}
	}

	res, err := match.Match(s.st, appID, req)
	if err != nil {
		s.backendError(appID, dh, err)
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if !res.Matched {
		if _, err := s.st.RecordInstall(store.Install{AppID: appID, DeviceHash: dh, Attribution: store.AttributionOrganic, Platform: installPlatform(body)}); err != nil {
			s.backendError(appID, dh, err)
		}
		w.WriteHeader(http.StatusNotFound)
		return
	}
	// failed install write: analytics row lost, link still returned (backend errors never deny link)
	inst := store.Install{AppID: appID, DeviceHash: dh, ClickID: res.Click.ID, Attribution: store.AttributionNonOrganic, LinkID: res.Click.LinkID, Platform: installPlatform(body)}
	if inst.Platform == "" { // clickId-only payload: matched click's browser tells the platform
		inst.Platform = ua.Platform(res.Click.Fingerprint.UserAgent)
	}
	if _, err := s.st.RecordInstall(inst); err != nil {
		s.log.Printf("match-link install record failed (link still returned): %v", err)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"link": res.Destination})
}

// resolveShort serves POST /api/link/resolve-short: map short URL to destination. SDK casts raw body to {link, route, parameters}, reads only link; 404 -> null (client falls back to original URL).
func (s *sdkServer) resolveShort(w http.ResponseWriter, r *http.Request) {
	appID := r.Header.Get("X-App-ID")
	var body struct {
		URL string `json:"url"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, errorBody("invalid request body"))
		return
	}
	u, err := url.Parse(body.URL)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	key := strings.Trim(u.Path, "/")
	if key == "" {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	link, err := s.st.GetLinkByKey(appID, key)
	if errors.Is(err, store.ErrNotFound) {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if err != nil {
		s.log.Printf("resolve-short backend error: %v", err)
		w.WriteHeader(http.StatusNotFound)
		return
	}
	dest := link.URL
	if link.Expired(time.Now()) {
		if link.ExpiredURL == "" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		dest = link.ExpiredURL
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]string{
		"link":       dest,
		"route":      u.Path,
		"parameters": u.RawQuery,
	})
}

// universalLinkClick: POST /api/link/universal-link-click. v1 answers "no limit": deny shape never sent, allow shape returned even when click recording fails. URLs with no derivable link key not recorded; response always carries clickId.
func (s *sdkServer) universalLinkClick(w http.ResponseWriter, r *http.Request) {
	appID := r.Header.Get("X-App-ID")
	var body struct {
		URL string `json:"url"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, errorBody("invalid request body"))
		return
	}
	writeAllow(w, s.recordClickID(appID, body.URL, r))
}

// writeAllow: universal-link-click allow shape (v1 has no limit).
func writeAllow(w http.ResponseWriter, clickID string) {
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"allowed": true, "clicksInPeriod": 0, "effectiveLimit": -1, "clickId": clickID,
	})
}

// recordClickID: universal-link click (fail-open), returns clickId. No bot filter — caller is app (Dub skips bot checks for deeplink opens). Repeat opens within hour reuse click (store dedup). Nothing recorded -> fresh id minted, SDK always sees one.
func (s *sdkServer) recordClickID(appID, rawURL string, r *http.Request) string {
	key := linkKey(rawURL)
	if key == "" {
		return store.Nanoid(16)
	}
	link, err := s.st.GetLinkByKey(appID, key)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			s.log.Printf("universal-link-click link lookup failed: %v", err)
		}
		return store.Nanoid(16)
	}
	rec, err := s.st.RecordClick(store.Click{
		AppID: appID, LinkID: link.ID, Destination: link.URL,
		Fingerprint: store.Fingerprint{IP: httpx.RemoteIP(r), UserAgent: r.UserAgent()},
		Platform:    ua.AppPlatform(r.UserAgent()), Kind: store.KindOpen,
	}, s.retentionHours)
	if err != nil {
		s.log.Printf("universal-link-click backend error (click not recorded): %v", err)
		return store.Nanoid(16)
	}
	return rec.ID
}

func (s *sdkServer) analyticsEvent(w http.ResponseWriter, r *http.Request) {
	s.recordAnalytics(w, r, "")
}

func (s *sdkServer) analyticsRetention(w http.ResponseWriter, r *http.Request) {
	s.recordAnalytics(w, r, "retention")
}

// recordAnalytics persists analytics body: event name from event_name (SDK verbatim), task-spec "event" key or endpoint default as fallback; full body JSON stored as metadata.
func (s *sdkServer) recordAnalytics(w http.ResponseWriter, r *http.Request, defaultName string) {
	appID := r.Header.Get("X-App-ID")
	var body map[string]json.RawMessage
	if err := decodeJSON(w, r, &body); err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, errorBody("invalid request body"))
		return
	}
	name := firstString(body, "event_name", "event")
	if name == "" {
		name = defaultName
	}
	md, err := json.Marshal(body)
	if err != nil {
		httpx.WriteJSON(w, http.StatusInternalServerError, errorBody("internal error"))
		return
	}
	if err := s.st.RecordEvent(appID, name, string(md)); err != nil {
		s.log.Printf("%s backend error: %v", r.URL.Path, err)
		httpx.WriteJSON(w, http.StatusInternalServerError, errorBody("internal error"))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// backendError logs match-link backend failure, best-effort records unknown-attribution row: logged, never surfaced in analytics.
func (s *sdkServer) backendError(appID, deviceHash string, err error) {
	s.log.Printf("match-link backend error: %v", err)
	if _, rerr := s.st.RecordInstall(store.Install{AppID: appID, DeviceHash: deviceHash, Attribution: store.AttributionUnknown}); rerr != nil {
		s.log.Printf("match-link unknown-attribution record failed: %v", rerr)
	}
}

// installPlatform: install's platform from the SDK fingerprint: Apple manufacturer -> ios, any other manufacturer -> android, else app UA markers.
func installPlatform(b matchLinkBody) string {
	switch {
	case strings.EqualFold(b.Manufacturer, "apple"):
		return "ios"
	case b.Manufacturer != "":
		return "android"
	}
	return ua.AppPlatform(b.UserAgent)
}

// deviceHash: stable per-device identifier, SHA-256 hex of canonical fingerprint fields in fixed order. Timestamp excluded (not device characteristic); stability keeps duplicate match-link calls idempotent on (device_hash, click_id). clickId-only payload hashes clickId instead.
func deviceHash(b matchLinkBody) string {
	if b.ClickID != "" {
		return store.HashKey("clickId=" + b.ClickID)
	}
	canonical := fmt.Sprintf("model=%s|manufacturer=%s|systemVersion=%s|screenWidth=%d|screenHeight=%d|scale=%g|locale=%s|timezone=%s|userAgent=%s|pastedLink=%s",
		b.Model, b.Manufacturer, b.SystemVersion, b.ScreenWidth, b.ScreenHeight, b.Scale,
		localeString(b.Locale), strOr(b.Timezone), b.UserAgent, b.PastedLink)
	return store.HashKey(canonical)
}

// bearerKey extracts the token from "Bearer <token>".
func bearerKey(h string) (string, bool) {
	if len(h) > 7 && strings.EqualFold(h[:7], "Bearer ") {
		return strings.TrimSpace(h[7:]), true
	}
	return "", false
}

// linkKey: short-link key from URL path (single path segment in v1; multi-segment paths won't match any stored key).
func linkKey(u string) string {
	p, err := url.Parse(u)
	if err != nil {
		return ""
	}
	return strings.Trim(p.Path, "/")
}

func localeString(tags []localeTag) string {
	parts := make([]string, 0, len(tags))
	for _, t := range tags {
		parts = append(parts, t.LanguageTag)
	}
	return strings.Join(parts, ",")
}

func strOr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func firstString(body map[string]json.RawMessage, keys ...string) string {
	for _, k := range keys {
		var v string
		if raw, ok := body[k]; ok && json.Unmarshal(raw, &v) == nil {
			return v
		}
	}
	return ""
}

func errorBody(msg string) map[string]any {
	return map[string]any{"error": map[string]any{"message": msg}}
}

// decodeJSON reads bounded request body into v (1MB cap keeps hostile client from pinning single-writer connection with giant upload).
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, r.Body)
	return nil
}
