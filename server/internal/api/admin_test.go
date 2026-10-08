package api

// Admin elevation token tests: stateless signed session cookie primitives
// (mintToken/verifyToken). Wire behavior of the session endpoints and route
// gating lives in portal_test.go.

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestTokenRoundTrip(t *testing.T) {
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	token, expiresAt := mintToken("s3cret", 12, now)
	want := now.Add(12 * time.Hour)
	if !expiresAt.Equal(want) {
		t.Fatalf("expiresAt = %v, want %v", expiresAt, want)
	}
	// shape: <expiry-unix>.<64 hex chars>
	exp, sig, ok := strings.Cut(token, ".")
	if !ok || len(sig) != 64 {
		t.Fatalf("token shape = %q, want <expiry-unix>.<64 hex>", token)
	}
	if exp != fmt.Sprintf("%d", want.Unix()) {
		t.Fatalf("embedded expiry = %q, want %d", exp, want.Unix())
	}
	got, ok := verifyToken(token, "s3cret", now)
	if !ok {
		t.Fatal("verifyToken must accept a freshly minted token")
	}
	if !got.Equal(want) {
		t.Fatalf("verifyToken expiry = %v, want %v", got, want)
	}
}

func TestTokenTamperedFails(t *testing.T) {
	now := time.Now()
	token, _ := mintToken("s3cret", 12, now)
	// flip one hex char: first digit 0->1, a->b
	flip := map[byte]byte{'0': '1', '1': '0', 'a': 'b', 'b': 'a'}
	for i := range token {
		if token[i] == '.' {
			continue
		}
		c := token[i]
		if alt, ok := flip[c]; ok {
			tampered := token[:i] + string(alt) + token[i+1:]
			if _, ok := verifyToken(tampered, "s3cret", now); ok {
				t.Fatalf("tampered token %q must fail verification", tampered)
			}
			// wrong secret also fails on the untouched token
			if _, ok := verifyToken(token, "other-secret", now); ok {
				t.Fatal("verifyToken with a different secret must fail")
			}
			return
		}
	}
	t.Fatal("no hex char to flip in token " + token)
}

func TestTokenExpiry(t *testing.T) {
	now := time.Now()
	token, expiresAt := mintToken("s3cret", 12, now)
	if _, ok := verifyToken(token, "s3cret", now); !ok {
		t.Fatal("token must pass at mint time")
	}
	if _, ok := verifyToken(token, "s3cret", now.Add(time.Hour)); !ok {
		t.Fatal("token must pass mid-session")
	}
	if _, ok := verifyToken(token, "s3cret", expiresAt.Add(time.Second)); ok {
		t.Fatal("token must fail after expiry")
	}
}

func TestTokenMalformed(t *testing.T) {
	now := time.Now()
	for _, tok := range []string{"", "not-a-token", "12345", "abc.def", "1760426400.", ".abcdef"} {
		if _, ok := verifyToken(tok, "s3cret", now); ok {
			t.Errorf("malformed token %q must fail", tok)
		}
	}
}

// Login lock is per socket peer: a wrong-password burst from one peer does not queue another peer's
// login, two wrong guesses from the same peer still serialize, and idle lock entries are dropped.
func TestElevateLockPerPeer(t *testing.T) {
	p := &portalServer{adminPassword: "s3cret", adminHours: 12, adminLocks: map[string]*peerLock{}}
	post := func(peer, pw string) int {
		r := httptest.NewRequest("POST", "/api/admin/session", strings.NewReader(`{"password":"`+pw+`"}`))
		r.RemoteAddr = peer
		w := httptest.NewRecorder()
		p.elevate(w, r)
		return w.Code
	}

	start := time.Now()
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); post("10.0.0.1:1000", "wrong") }()
	}
	time.Sleep(50 * time.Millisecond) // attacker's guesses hold peer 10.0.0.1's lock
	if code := post("10.0.0.2:2000", "s3cret"); code != http.StatusOK {
		t.Fatalf("other peer login = %d; want 200", code)
	}
	if d := time.Since(start); d > 500*time.Millisecond {
		t.Errorf("other peer waited %v behind a failed burst; want no queueing", d)
	}
	wg.Wait()
	if d := time.Since(start); d < 2*time.Second {
		t.Errorf("two wrong guesses from one peer took %v; want >= 2s (serialized)", d)
	}
	if n := len(p.adminLocks); n != 0 {
		t.Errorf("adminLocks holds %d idle entries; want 0", n)
	}
}
