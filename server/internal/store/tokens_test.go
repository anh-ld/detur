package store

import (
	"errors"
	"slices"
	"testing"
	"time"
)

// Token lifecycle: create, lookup, update, cascade on app delete, revoke, throttled last-used.
func TestTokens(t *testing.T) {
	s := newTestStore(t)
	a, _ := s.CreateApp("A", "k1")
	b, _ := s.CreateApp("B", "k2")

	tok, plain, err := s.CreateToken("agent", []string{a.ID, b.ID})
	if err != nil {
		t.Fatal(err)
	}
	if plain[:4] != "dtr_" || tok.Prefix != plain[:8] {
		t.Fatalf("token %q prefix %q", plain, tok.Prefix)
	}
	var hash string
	if err := s.db.QueryRow(`SELECT token_hash FROM api_tokens WHERE id = ?`, tok.ID).Scan(&hash); err != nil || hash == plain || hash != HashKey(plain) {
		t.Fatalf("stored hash %q err %v", hash, err)
	}
	got, err := s.TokenByPlain(plain)
	if err != nil || !slices.Equal(got.AppIDs, []string{a.ID, b.ID}) && !slices.Equal(got.AppIDs, []string{b.ID, a.ID}) {
		t.Fatalf("lookup = %+v, %v", got, err)
	}
	if _, err := s.TokenByPlain("dtr_nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("bad token: %v", err)
	}

	if _, _, err := s.CreateToken("bad", []string{a.ID, "nope"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown app: %v", err)
	}
	if ts, _ := s.ListTokens(); len(ts) != 1 {
		t.Fatalf("failed create left a row: %+v", ts)
	}
	tok2, _, err := s.CreateToken("second", []string{a.ID})
	if err != nil {
		t.Fatal(err)
	}
	if ts, _ := s.ListTokens(); len(ts) != 2 || len(ts[0].AppIDs) != 2 || len(ts[1].AppIDs) != 1 || ts[1].AppIDs[0] != a.ID {
		t.Fatalf("batched app lookup mixed tokens: %+v", ts)
	}
	if err := s.DeleteToken(tok2.ID); err != nil {
		t.Fatal(err)
	}

	if err := s.UpdateToken(tok.ID, "", []string{b.ID}); err != nil {
		t.Fatal(err)
	}
	if got, _ = s.TokenByPlain(plain); !slices.Equal(got.AppIDs, []string{b.ID}) || got.Name != "agent" {
		t.Fatalf("after update = %+v", got)
	}

	if err := s.DeleteApp(b.ID); err != nil {
		t.Fatal(err)
	}
	if got, err = s.TokenByPlain(plain); err != nil || len(got.AppIDs) != 0 {
		t.Fatalf("after app delete = %+v, %v", got, err)
	}

	// touch: 30s later no write, 61s later write
	t0 := time.Now().UTC().Truncate(time.Millisecond)
	for _, c := range []struct {
		at   time.Duration
		want time.Duration
	}{{0, 0}, {30 * time.Second, 0}, {61 * time.Second, 61 * time.Second}} {
		if err := s.TouchToken(tok.ID, t0.Add(c.at)); err != nil {
			t.Fatal(err)
		}
		got, _ = s.TokenByPlain(plain)
		if got.LastUsedAt == nil || !got.LastUsedAt.Equal(t0.Add(c.want)) {
			t.Fatalf("touch at +%v: last_used = %v, want +%v", c.at, got.LastUsedAt, c.want)
		}
	}

	if err := s.DeleteToken(tok.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.TokenByPlain(plain); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoked: %v", err)
	}
	if err := s.DeleteToken(tok.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("double delete: %v", err)
	}
}
