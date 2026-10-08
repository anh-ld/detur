package api

// Admin elevation: stateless signed session cookie. Token =
// <expiry-unix>.<hex hmac_sha256(secret, expiry-unix)>, verified with a
// constant-time compare; expiry enforced on the server clock only. Session
// endpoints and the requireAdmin route gate live in this file.

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"detur.dev/server/internal/httpx"
)

// adminCookie: elevation session cookie (HttpOnly, SameSite=Lax; the origin/host guard covers CSRF).
const adminCookie = "detur_admin"

// sessionCookie: the elevation cookie carrying value; maxAge -1 clears it (wire: Max-Age=0).
func sessionCookie(value string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name: adminCookie, Value: value, HttpOnly: true,
		SameSite: http.SameSiteLaxMode, Path: "/",
		MaxAge: maxAge,
	}
}

// sign: HMAC-SHA256 hex over msg, keyed by secret (the admin password).
func sign(secret, msg string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(msg))
	return hex.EncodeToString(mac.Sum(nil))
}

// mintToken: token valid until now+hours, signed with secret (the admin password).
func mintToken(secret string, hours int, now time.Time) (token string, expiresAt time.Time) {
	expiresAt = now.Add(time.Duration(hours) * time.Hour)
	exp := strconv.FormatInt(expiresAt.Unix(), 10)
	return exp + "." + sign(secret, exp), expiresAt
}

// verifyToken: recompute the HMAC over the token's embedded expiry, constant-time compare, then check expiry against now. Malformed/tampered/expired tokens fail.
func verifyToken(token, secret string, now time.Time) (expiresAt time.Time, ok bool) {
	exp, sig, ok := strings.Cut(token, ".")
	if !ok {
		return time.Time{}, false
	}
	secs, err := strconv.ParseInt(exp, 10, 64)
	if err != nil {
		return time.Time{}, false
	}
	if subtle.ConstantTimeCompare([]byte(sig), []byte(sign(secret, exp))) != 1 {
		return time.Time{}, false
	}
	expiresAt = time.Unix(secs, 0)
	if !now.Before(expiresAt) {
		return time.Time{}, false
	}
	return expiresAt, true
}

// peerLock: one socket peer's login lock; n counts holders + waiters so idle entries are dropped.
type peerLock struct {
	mu sync.Mutex
	n  int
}

// lockPeer: serialize logins from the request's socket peer; returns the unlock. Keyed on RemoteAddr,
// never X-Forwarded-For: a client-chosen key would let one attacker run unlimited parallel guesses.
// Behind a gateway every request shares one peer, so this is one global lock there.
func (p *portalServer) lockPeer(r *http.Request) func() {
	peer, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		peer = r.RemoteAddr
	}
	p.adminMu.Lock()
	l := p.adminLocks[peer]
	if l == nil {
		l = &peerLock{}
		p.adminLocks[peer] = l
	}
	l.n++
	p.adminMu.Unlock()
	l.mu.Lock()
	return func() {
		l.mu.Unlock()
		p.adminMu.Lock()
		if l.n--; l.n == 0 {
			delete(p.adminLocks, peer)
		}
		p.adminMu.Unlock()
	}
}

// elevate: POST /api/admin/session. Unset password → 403 (nothing to elevate from). Wrong password → 401 after a 1s delay; compare + delay serialized per socket peer so parallel connections cannot multiply one source's guess rate, and one source's failed burst does not queue another's login. Right → signed HttpOnly cookie for the session TTL.
func (p *portalServer) elevate(w http.ResponseWriter, r *http.Request) {
	if p.adminPassword == "" {
		http.Error(w, "admin not configured", http.StatusForbidden)
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer p.lockPeer(r)()
	if subtle.ConstantTimeCompare([]byte(body.Password), []byte(p.adminPassword)) != 1 {
		time.Sleep(time.Second)
		http.Error(w, "wrong password", http.StatusUnauthorized)
		return
	}
	token, expiresAt := mintToken(p.adminPassword, p.adminHours, time.Now())
	http.SetCookie(w, sessionCookie(token, p.adminHours*3600))
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"admin": true, "expiresAt": expiresAt.UTC().Format(time.RFC3339)})
}

// sessionStatus: GET /api/admin/session. Valid cookie → {admin:true, expiresAt}; else {admin:false}. Unset password → always inactive (nothing to be active in).
func (p *portalServer) sessionStatus(w http.ResponseWriter, r *http.Request) {
	if p.adminPassword != "" {
		if at, ok := p.session(r); ok {
			httpx.WriteJSON(w, http.StatusOK, map[string]any{"admin": true, "expiresAt": at.UTC().Format(time.RFC3339)})
			return
		}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"admin": false})
}

// session: the request's valid, unexpired elevation cookie, if any.
func (p *portalServer) session(r *http.Request) (expiresAt time.Time, ok bool) {
	c, err := r.Cookie(adminCookie)
	if err != nil {
		return time.Time{}, false
	}
	return verifyToken(c.Value, p.adminPassword, time.Now())
}

// clearSession: DELETE /api/admin/session. Clears the cookie (Max-Age=0); the stateless token itself dies at its embedded expiry.
func (p *portalServer) clearSession(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, sessionCookie("", -1))
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"admin": false})
}

// requireAdmin: route gate. AdminPassword "" → pass through (no gating). Else valid unexpired detur_admin cookie required; absent/invalid/expired → 403.
func (p *portalServer) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if p.adminPassword == "" {
			next(w, r)
			return
		}
		if _, ok := p.session(r); !ok {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}
