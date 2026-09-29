// Package api serves the Detour SDK endpoints: match-link, resolve-short,
// universal-link-click, and the two analytics calls, with godetour.dev
// compatible shapes, SDK header auth, and fail-open behavior.
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
)

// Settings keys: the matching defaults live in the settings table, edited
// through the portal.
const (
	settingWindow    = "window_minutes"
	settingThreshold = "threshold"
)

// RegisterSDK attaches the five SDK endpoints to mux (stdlib method
// patterns). The auth wrapper validates Bearer key + X-App-ID + X-SDK
// presence; universal-link-click fails open when auth cannot be checked so a
// backend failure never blocks the link. retentionHours is the click
// retention floor threaded from config.
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

// requireAuth enforces the SDK header contract: Authorization Bearer +
// X-App-ID + X-SDK present, apiKey valid for the app. failOpen endpoints
// (universal-link-click) proceed when the store cannot be queried.
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
				next(w, r)
				return
			}
			if r.URL.Path == "/api/link/match-link" {
				// A match-link backend error is a no-match, never a deny. The body
				// is unread here, so no device hash for an unknown row.
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

// matchLinkBody is the union of the deterministic (clickId) and probabilistic
// (fingerprint) match-link payloads (getDeferredLink.ts). locale is the SDK's
// [{languageTag}...] array; timestamp is ignored: not a device characteristic,
// would break device-hash stability.
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
}

type localeTag struct {
	LanguageTag string `json:"languageTag"`
}

// matchLink serves POST /api/link/match-link: deterministic clickId lookup
// or probabilistic fingerprint scoring; 200 {"link": destination} on match,
// 404 on no-match (SDK reads organic -> null), and fail-open 404 with an
// unknown-attribution row on backend error.
func (s *sdkServer) matchLink(w http.ResponseWriter, r *http.Request) {
	appID := r.Header.Get("X-App-ID")
	var body matchLinkBody
	if err := decodeJSON(w, r, &body); err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, errorBody("invalid request body"))
		return
	}

	window, threshold, err := s.matchSettings()
	if err != nil {
		s.backendError(appID, deviceHash(body), err)
		w.WriteHeader(http.StatusNotFound)
		return
	}
	req := match.Request{IP: httpx.RemoteIP(r)}
	if body.ClickID != "" {
		req.ClickID = body.ClickID
	} else {
		req.Fingerprint = &match.Fingerprint{
			Model: body.Model, Manufacturer: body.Manufacturer,
			SystemVersion: body.SystemVersion, ScreenWidth: body.ScreenWidth, ScreenHeight: body.ScreenHeight,
			Scale: body.Scale, Locale: localeString(body.Locale), Timezone: strOr(body.Timezone),
			UserAgent: body.UserAgent, PastedLink: body.PastedLink,
		}
	}

	res, err := match.Match(s.st, appID, req, window, threshold)
	if err != nil {
		s.backendError(appID, deviceHash(body), err)
		w.WriteHeader(http.StatusNotFound)
		return
	}
	dh := deviceHash(body)
	if !res.Matched {
		if _, err := s.st.RecordInstall(store.Install{AppID: appID, DeviceHash: dh, Attribution: store.AttributionOrganic}); err != nil {
			s.backendError(appID, dh, err)
		}
		w.WriteHeader(http.StatusNotFound)
		return
	}
	// Failed install write: readout row lost, link still returned (backend
	// errors never deny the link).
	if _, err := s.st.RecordInstall(store.Install{AppID: appID, DeviceHash: dh, ClickID: res.Click.ID, Attribution: store.AttributionNonOrganic}); err != nil {
		s.log.Printf("match-link install record failed (link still returned): %v", err)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"link": res.Destination})
}

// resolveShort serves POST /api/link/resolve-short: map a short URL to its
// destination. The SDK casts the raw body to {link, route, parameters} and
// reads only link; 404 -> null (client falls back to the original URL).
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
	if err != nil || u.Path == "" || u.Path == "/" {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	key := linkKey(body.URL)
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

// universalLinkClick: POST /api/link/universal-link-click. v1 answers "no
// limit": deny shape never sent, allow shape returned even when click
// recording fails. URLs with no derivable link key not recorded; response
// always carries a clickId.
func (s *sdkServer) universalLinkClick(w http.ResponseWriter, r *http.Request) {
	appID := r.Header.Get("X-App-ID")
	var body struct {
		URL string `json:"url"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, errorBody("invalid request body"))
		return
	}
	clickID := s.recordClickID(appID, body.URL, r)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"allowed": true, "clicksInPeriod": 0, "effectiveLimit": -1, "clickId": clickID,
	})
}

// recordClickID: universal-link click (fail-open), returns clickId. No bot
// filter — caller is the app (Dub skips bot checks for deeplink opens).
// Repeat opens within an hour reuse the click (store dedup). Nothing
// recorded -> fresh id minted, SDK always sees one.
func (s *sdkServer) recordClickID(appID, rawURL string, r *http.Request) string {
	key := linkKey(rawURL)
	if key == "" {
		return store.Nanoid(16)
	}
	link, err := s.st.GetLinkByKey(appID, key)
	if err != nil {
		s.log.Printf("universal-link-click link lookup failed: %v", err)
		return store.Nanoid(16)
	}
	window := link.WindowMinutes
	if window == 0 {
		window = s.windowSetting()
	}
	rec, err := s.st.RecordClick(store.Click{
		AppID: appID, LinkID: link.ID, Destination: link.URL,
		Fingerprint: store.Fingerprint{IP: httpx.RemoteIP(r), UserAgent: r.UserAgent()},
	}, window, s.retentionHours)
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

// recordAnalytics persists the analytics body: event name from event_name
// (SDK verbatim), with the task-spec "event" key or the endpoint default as
// fallback; the full body JSON is stored as metadata.
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

// matchSettings reads the app defaults from the settings table, falling
// back to the store defaults.
func (s *sdkServer) matchSettings() (window, threshold int, err error) {
	window, err = s.st.IntSetting(settingWindow, store.DefaultWindowMinutes)
	if err != nil {
		return 0, 0, err
	}
	threshold, err = s.st.IntSetting(settingThreshold, store.DefaultThreshold)
	if err != nil {
		return 0, 0, err
	}
	return window, threshold, nil
}

// windowSetting returns the configured match window, defaulting on missing
// or invalid settings: a bad setting must never block a click.
func (s *sdkServer) windowSetting() int {
	v, err := s.st.IntSetting(settingWindow, store.DefaultWindowMinutes)
	if err != nil {
		s.log.Printf("window_minutes read failed, using %d: %v", store.DefaultWindowMinutes, err)
		return store.DefaultWindowMinutes
	}
	return v
}

// backendError logs a match-link backend failure and best-effort records the
// unknown-attribution row: logged, never surfaced in readout.
func (s *sdkServer) backendError(appID, deviceHash string, err error) {
	s.log.Printf("match-link backend error: %v", err)
	if _, rerr := s.st.RecordInstall(store.Install{AppID: appID, DeviceHash: deviceHash, Attribution: store.AttributionUnknown}); rerr != nil {
		s.log.Printf("match-link unknown-attribution record failed: %v", rerr)
	}
}

// deviceHash derives a stable per-device identifier: SHA-256 hex of the
// canonical fingerprint fields in a fixed order. Timestamp is excluded (not a
// device characteristic); stability keeps duplicate match-link calls
// idempotent on (device_hash, click_id). A clickId-only payload hashes the
// clickId instead.
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

// linkKey extracts the short-link key from a URL path (single path segment in
// v1; multi-segment paths simply won't match any stored key).
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

// decodeJSON reads a bounded request body into v (1MB cap keeps a hostile
// client from pinning the single-writer connection with a giant upload).
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, r.Body)
	return nil
}
