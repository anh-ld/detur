package api

// Admin elevation: stateless signed session cookie. Token = <expiry-unix>.<hex hmac_sha256(secret, expiry-unix)>,
// constant-time compare, expiry on server clock only. Session endpoints + requireAdmin gate live here.

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

// adminCookie: elevation cookie (HttpOnly, SameSite=Lax; origin/host guard covers CSRF).
const adminCookie = "detur_admin"

// sessionCookie: elevation cookie with value; maxAge -1 clears (wire: Max-Age=0). Secure when the browser reached us over HTTPS
// (TLS here or at the gateway: X-Forwarded-Proto). A spoofed header only makes the cookie stricter; plain-HTTP loopback keeps working.
func sessionCookie(r *http.Request, value string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name: adminCookie, Value: value, HttpOnly: true,
		SameSite: http.SameSiteLaxMode, Path: "/",
		MaxAge: maxAge,
		Secure: r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https"),
	}
}

// sign: HMAC-SHA256 hex of msg, key = admin password.
func sign(secret, msg string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(msg))
	return hex.EncodeToString(mac.Sum(nil))
}

// mintToken: token valid until now+hours, signed with admin password.
func mintToken(secret string, hours int, now time.Time) (token string, expiresAt time.Time) {
	expiresAt = now.Add(time.Duration(hours) * time.Hour)
	exp := strconv.FormatInt(expiresAt.Unix(), 10)
	return exp + "." + sign(secret, exp), expiresAt
}

// verifyToken: recompute HMAC over embedded expiry, constant-time compare, check expiry vs now. Malformed/tampered/expired = fail.
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

// peerLock: one socket peer's login lock; n = holders + waiters, idle entries dropped.
type peerLock struct {
	mu sync.Mutex
	n  int
}

// lockPeer: serialize logins per socket peer; returns unlock. Keyed on RemoteAddr, never XFF: client-chosen key
// = unlimited parallel guesses. Behind a gateway all requests share one peer = one global lock.
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

// elevate: POST /api/admin/session. No password -> 403. Wrong -> 401 after 1s; compare + delay serialized per socket peer: parallel conns can't multiply guess rate, one peer's failed burst doesn't queue another's login. Right -> signed HttpOnly cookie for session TTL.
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
	http.SetCookie(w, sessionCookie(r, token, p.adminHours*3600))
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"admin": true, "expiresAt": expiresAt.UTC().Format(time.RFC3339)})
}

// sessionStatus: GET /api/admin/session. Valid cookie -> {admin:true, expiresAt}, else {admin:false}. No password -> always inactive.
func (p *portalServer) sessionStatus(w http.ResponseWriter, r *http.Request) {
	if p.adminPassword != "" {
		if at, ok := p.session(r); ok {
			httpx.WriteJSON(w, http.StatusOK, map[string]any{"admin": true, "expiresAt": at.UTC().Format(time.RFC3339)})
			return
		}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"admin": false})
}

// session: request's valid, unexpired elevation cookie, if any.
func (p *portalServer) session(r *http.Request) (expiresAt time.Time, ok bool) {
	c, err := r.Cookie(adminCookie)
	if err != nil {
		return time.Time{}, false
	}
	return verifyToken(c.Value, p.adminPassword, time.Now())
}

// clearSession: DELETE /api/admin/session. Clears cookie (Max-Age=0); stateless token itself dies at embedded expiry.
func (p *portalServer) clearSession(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, sessionCookie(r, "", -1))
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"admin": false})
}

// requireAdmin: route gate. AdminPassword "" -> pass. Else valid unexpired detur_admin cookie required, else 403.
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
