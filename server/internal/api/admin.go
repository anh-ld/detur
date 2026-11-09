package api

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

func sign(secret, msg string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(msg))
	return hex.EncodeToString(mac.Sum(nil))
}

func mintToken(secret string, hours int, now time.Time) (token string, expiresAt time.Time) {
	expiresAt = now.Add(time.Duration(hours) * time.Hour)
	exp := strconv.FormatInt(expiresAt.Unix(), 10)
	return exp + "." + sign(secret, exp), expiresAt
}

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

type peerLock struct {
	mu sync.Mutex
	n  int
}

// lockPeer: keyed on RemoteAddr, never XFF: client-chosen key = unlimited parallel guesses.
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
	writeElevated(w, expiresAt)
}

func writeElevated(w http.ResponseWriter, expiresAt time.Time) {
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"admin": true, "expiresAt": fmtTime(expiresAt)})
}

func (p *portalServer) sessionStatus(w http.ResponseWriter, r *http.Request) {
	if p.adminPassword != "" {
		if at, ok := p.session(r); ok {
			writeElevated(w, at)
			return
		}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"admin": false})
}

func (p *portalServer) session(r *http.Request) (expiresAt time.Time, ok bool) {
	c, err := r.Cookie(adminCookie)
	if err != nil {
		return time.Time{}, false
	}
	now := time.Now()
	expiresAt, ok = verifyToken(c.Value, p.adminPassword, now)
	if ok && expiresAt.Sub(now) > time.Duration(p.adminHours)*time.Hour {
		return time.Time{}, false
	}
	return expiresAt, ok
}

func (p *portalServer) clearSession(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, sessionCookie(r, "", -1))
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"admin": false})
}

func (p *portalServer) requireAdminSet(msg string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if p.adminPassword == "" {
			http.Error(w, msg, http.StatusForbidden)
			return
		}
		p.requireAdmin(next)(w, r)
	}
}

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
