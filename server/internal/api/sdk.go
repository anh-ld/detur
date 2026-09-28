// Package api serves the Detour SDK endpoints (U3): match-link, resolve-short,
// universal-link-click, and the two analytics calls, with godetour.dev
// compatible shapes (R1), SDK header auth (R4), and fail-open behavior (R3).
package api

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"detur.dev/server/internal/match"
	"detur.dev/server/internal/store"
)

// RegisterSDK attaches the five SDK endpoints to mux (KTD2: stdlib method
// patterns). The auth wrapper validates Bearer key + X-App-ID + X-SDK
// presence (R4); universal-link-click fails open when auth cannot be checked
// so a backend failure never blocks the link (R3).
func RegisterSDK(mux *http.ServeMux, st *store.Store) {
	s := &sdkServer{st: st, log: log.Default()}
	mux.HandleFunc("POST /api/link/match-link", s.requireAuth(s.matchLink, false))
	mux.HandleFunc("POST /api/link/resolve-short", s.requireAuth(s.resolveShort, false))
	mux.HandleFunc("POST /api/link/universal-link-click", s.requireAuth(s.universalLinkClick, true))
	mux.HandleFunc("POST /api/analytics/event", s.requireAuth(s.analyticsEvent, false))
	mux.HandleFunc("POST /api/analytics/retention", s.requireAuth(s.analyticsRetention, false))
}

// Matching/click defaults (R6): fall back when no settings are stored.
const (
	defaultWindow       = 15
	defaultThreshold    = 850
	retentionFloorHours = 24
)

type sdkServer struct {
	st  *store.Store
	log *log.Logger
}

// requireAuth enforces the SDK header contract (R4): Authorization Bearer +
// X-App-ID + X-SDK present, apiKey valid for the app. failOpen endpoints
// (universal-link-click) proceed when the store cannot be queried (R3).
func (s *sdkServer) requireAuth(next http.HandlerFunc, failOpen bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key, ok := bearerKey(r.Header.Get("Authorization"))
		if !ok || r.Header.Get("X-App-ID") == "" || r.Header.Get("X-SDK") == "" {
			writeJSON(w, http.StatusUnauthorized, errorBody("unauthorized"))
			return
		}
		valid, err := s.st.ValidateAPIKey(r.Header.Get("X-App-ID"), key)
		if err != nil {
			if failOpen {
				s.log.Printf("auth check failed, failing open: %v", err)
				next(w, r)
				return
			}
			s.log.Printf("auth check backend error: %v", err)
			writeJSON(w, http.StatusInternalServerError, errorBody("internal error"))
			return
		}
		if !valid {
			writeJSON(w, http.StatusUnauthorized, errorBody("unauthorized"))
			return
		}
		next(w, r)
	}
}

// matchLinkBody is the union of the deterministic (clickId) and probabilistic
// (fingerprint) match-link payloads (SDK source getDeferredLink.ts). locale
// is the SDK's [{languageTag}...] array; timestamp is ignored — it is not a
// device characteristic and would break device-hash stability.
type matchLinkBody struct {
	ClickID       string      `json:"clickId"`
	Platform      string      `json:"platform"`
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

// matchLink serves POST /api/link/match-link (R5-R7): deterministic clickId
// lookup or probabilistic fingerprint scoring; 200 {"link": destination} on
// match, 404 on no-match (SDK reads organic -> null), and fail-open 404 with
// an unknown-attribution row on backend error (R3).
func (s *sdkServer) matchLink(w http.ResponseWriter, r *http.Request) {
	appID := r.Header.Get("X-App-ID")
	var body matchLinkBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid request body"))
		return
	}

	window, threshold, err := s.matchSettings()
	if err != nil {
		s.backendError(appID, deviceHash(body), err)
		w.WriteHeader(http.StatusNotFound)
		return
	}
	req := match.Request{IP: remoteIP(r)}
	if body.ClickID != "" {
		req.ClickID = body.ClickID
	} else {
		req.Fingerprint = &match.Fingerprint{
			Platform: body.Platform, Model: body.Model, Manufacturer: body.Manufacturer,
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
	if _, err := s.st.RecordInstall(store.Install{AppID: appID, DeviceHash: dh, ClickID: res.Click.ID, Attribution: store.AttributionNonOrganic}); err != nil {
		s.backendError(appID, dh, err)
		w.WriteHeader(http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"link": res.Destination})
}

// resolveShort serves POST /api/link/resolve-short: map a short URL to its
// destination. The SDK casts the raw body to {link, route, parameters} and
// reads only link; 404 -> null (client falls back to the original URL).
func (s *sdkServer) resolveShort(w http.ResponseWriter, r *http.Request) {
	appID := r.Header.Get("X-App-ID")
	var body struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid request body"))
		return
	}
	u, err := url.Parse(body.URL)
	if err != nil || u.Path == "" || u.Path == "/" {
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
	writeJSON(w, http.StatusOK, map[string]string{
		"link":       link.URL,
		"route":      u.Path,
		"parameters": u.RawQuery,
	})
}

// universalLinkClick serves POST /api/link/universal-link-click. v1 answers
// "no limit" (KD3): the deny shape is never sent; the allow shape is returned
// even when click recording fails (R3). Bot clicks and URLs with no derivable
// link key are not recorded.
func (s *sdkServer) universalLinkClick(w http.ResponseWriter, r *http.Request) {
	appID := r.Header.Get("X-App-ID")
	var body struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid request body"))
		return
	}
	clickID := newID(16)
	if key := linkKey(body.URL); key != "" && !isBotUA(r.UserAgent()) {
		link, err := s.st.GetLinkByKey(appID, key)
		if err != nil {
			s.log.Printf("universal-link-click link lookup failed: %v", err)
		} else {
			rec, err := s.st.RecordClick(store.Click{
				AppID: appID, LinkID: link.ID, Destination: link.URL,
				Fingerprint: store.Fingerprint{IP: remoteIP(r), UserAgent: r.UserAgent()},
			}, s.windowSetting(), retentionFloorHours)
			if err != nil {
				s.log.Printf("universal-link-click backend error (click not recorded): %v", err)
			} else {
				clickID = rec.ID
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"allowed": true, "clicksInPeriod": 0, "effectiveLimit": -1, "clickId": clickID,
	})
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
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid request body"))
		return
	}
	name := firstString(body, "event_name", "event")
	if name == "" {
		name = defaultName
	}
	md, err := json.Marshal(body)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("internal error"))
		return
	}
	if err := s.st.RecordEvent(appID, name, string(md)); err != nil {
		s.log.Printf("%s backend error: %v", r.URL.Path, err)
		writeJSON(w, http.StatusInternalServerError, errorBody("internal error"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// matchSettings reads the app defaults from the settings table (R14),
// falling back to 15 minutes / 850.
func (s *sdkServer) matchSettings() (window, threshold int, err error) {
	window, threshold = defaultWindow, defaultThreshold
	if v, ok, e := s.st.GetSetting("window_minutes"); e != nil {
		return 0, 0, e
	} else if ok {
		if n, perr := strconv.Atoi(v); perr == nil {
			window = n
		}
	}
	if v, ok, e := s.st.GetSetting("threshold"); e != nil {
		return 0, 0, e
	} else if ok {
		if n, perr := strconv.Atoi(v); perr == nil {
			threshold = n
		}
	}
	return window, threshold, nil
}

// windowSetting returns the configured match window, defaulting on missing or
// invalid settings.
func (s *sdkServer) windowSetting() int {
	if v, ok, err := s.st.GetSetting("window_minutes"); err != nil {
		s.log.Printf("window_minutes read failed, using %d: %v", defaultWindow, err)
	} else if ok {
		if n, perr := strconv.Atoi(v); perr == nil {
			return n
		}
	}
	return defaultWindow
}

// backendError logs a match-link backend failure (R3) and best-effort records
// the unknown-attribution row: logged, never surfaced in readout.
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
		return sha256Hex("clickId=" + b.ClickID)
	}
	canonical := fmt.Sprintf("platform=%s|model=%s|manufacturer=%s|systemVersion=%s|screenWidth=%d|screenHeight=%d|scale=%g|locale=%s|timezone=%s|userAgent=%s|pastedLink=%s",
		b.Platform, b.Model, b.Manufacturer, b.SystemVersion, b.ScreenWidth, b.ScreenHeight, b.Scale,
		localeString(b.Locale), strOr(b.Timezone), b.UserAgent, b.PastedLink)
	return sha256Hex(canonical)
}

// bearerKey extracts the token from "Bearer <token>".
func bearerKey(h string) (string, bool) {
	if len(h) > 7 && strings.EqualFold(h[:7], "Bearer ") {
		return strings.TrimSpace(h[7:]), true
	}
	return "", false
}

// remoteIP returns the request connection IP, honoring X-Forwarded-For for
// the documented reverse-proxy deployment (TLS termination).
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

// isBotUA is the cheap Dub-style bot filter (cloned from record-click, KD4):
// any UA containing a known bot marker skips click recording.
func isBotUA(ua string) bool {
	ua = strings.ToLower(ua)
	return strings.Contains(ua, "bot") || strings.Contains(ua, "spider") || strings.Contains(ua, "crawler")
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

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// newID returns a crypto-random base62 id (SDK-visible click ids).
func newID(n int) string {
	const alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand unavailable: " + err.Error())
	}
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return string(b)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func errorBody(msg string) map[string]any {
	return map[string]any{"error": map[string]any{"message": msg}}
}
