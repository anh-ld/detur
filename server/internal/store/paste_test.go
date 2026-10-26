package store

import (
	"errors"
	"testing"
	"time"
)

func TestPasteToken(t *testing.T) {
	for _, tc := range []struct{ raw, key, token string }{
		{"https://l.example/abc?_dc=AbCdEfGh12345678", "abc", "AbCdEfGh12345678"},
		{"https://l.example/abc", "", ""},
		{"https://l.example/abc?_dc=short", "", ""},
		{"https://l.example/abc?_dc=AbCdEfGh1234567!", "", ""},
		{"", "", ""},
	} {
		if k, tok := PasteToken(tc.raw); k != tc.key || tok != tc.token {
			t.Errorf("PasteToken(%q) = %q, %q; want %q, %q", tc.raw, k, tok, tc.key, tc.token)
		}
	}
}

func TestClickByPasteToken(t *testing.T) {
	s := newTestStore(t)
	s.ClickIDHours = 30 * 24
	app, link := setupApp(t, s)
	const tok, tok2 = "AAAAAAAAAAAAAAAA", "BBBBBBBBBBBBBBBB"
	fp := Fingerprint{IP: "203.0.113.7", UserAgent: "Mozilla/5.0 (iPhone; CPU iPhone OS 17_2 like Mac OS X) Safari/604.1"}
	c, err := s.RecordClick(Click{AppID: app.ID, LinkID: link.ID, Destination: link.URL, Kind: KindApp, Fingerprint: fp, PasteToken: tok}, 24)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := s.ClickByPasteToken(app.ID, "abc", tok, "devA"); err != nil || got.ID != c.ID {
		t.Fatalf("lookup = %v, %v; want %s", got.ID, err, c.ID)
	}
	if _, err := s.ClickByPasteToken(app.ID, "other", tok, "devA"); !errors.Is(err, ErrNotFound) {
		t.Errorf("other link key: err = %v; want not found", err)
	}
	// re-tap copies again: newest token wins
	if _, err := s.RecordClick(Click{AppID: app.ID, LinkID: link.ID, Destination: link.URL, Kind: KindApp, Fingerprint: fp, PasteToken: tok2}, 24); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ClickByPasteToken(app.ID, "abc", tok2, "devA"); err != nil {
		t.Errorf("new token: %v", err)
	}
	if ok, err := s.MarkClickMatched(c.ID, "devA"); !ok || err != nil {
		t.Fatalf("claim: %v %v", ok, err)
	}
	if _, err := s.ClickByPasteToken(app.ID, "abc", tok2, "devB"); !errors.Is(err, ErrNotFound) {
		t.Errorf("claimed by another device: err = %v; want not found", err)
	}
	if _, err := s.ClickByPasteToken(app.ID, "abc", tok2, "devA"); err != nil {
		t.Errorf("same device retry: %v", err)
	}
}

func TestReopenMergePasteToken(t *testing.T) {
	const webview, safari = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_2 like Mac OS X) Instagram 300.0", "Mozilla/5.0 (iPhone; CPU iPhone OS 17_2 like Mac OS X) Safari/604.1"
	const ip, inAppTok, browserTok = "203.0.113.7", "AAAAAAAAAAAAAAAA", "CCCCCCCCCCCCCCCC"
	inAppPaste, browserPaste := "https://l.example/abc?_dc="+inAppTok, "https://l.example/abc?_dc="+browserTok
	for _, tc := range []struct {
		name                       string
		inAppPaste, inAppTok       string
		browserPaste, browserTok   string
		wantPaste, wantFound, gone string
	}{
		{"in-app without paste takes browser's", "", "", browserPaste, browserTok, browserPaste, browserTok, ""},
		{"later browser copy replaces in-app copy", inAppPaste, inAppTok, browserPaste, browserTok, browserPaste, browserTok, inAppTok},
		{"browser without copy keeps in-app copy", inAppPaste, inAppTok, "", "", inAppPaste, inAppTok, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestStore(t)
			app, link := setupApp(t, s)
			first, err := s.RecordClick(Click{AppID: app.ID, LinkID: link.ID, Destination: link.URL, Kind: KindApp, Source: "instagram",
				Fingerprint: Fingerprint{IP: ip, UserAgent: webview, PastedLink: tc.inAppPaste}, PasteToken: tc.inAppTok}, 24)
			if err != nil {
				t.Fatal(err)
			}
			second, err := s.RecordClick(Click{AppID: app.ID, LinkID: link.ID, Destination: link.URL, Kind: KindApp,
				Fingerprint: Fingerprint{IP: ip, UserAgent: safari, PastedLink: tc.browserPaste}, PasteToken: tc.browserTok}, 24)
			if err != nil {
				t.Fatal(err)
			}
			if second.ID != first.ID || second.Fingerprint.PastedLink != tc.wantPaste {
				t.Errorf("merged %s (want %s), pasted %q (want %q)", second.ID, first.ID, second.Fingerprint.PastedLink, tc.wantPaste)
			}
			if _, err := s.ClickByPasteToken(app.ID, "abc", tc.wantFound, "dev"); err != nil {
				t.Errorf("token %s: %v; want found", tc.wantFound, err)
			}
			if tc.gone != "" {
				if _, err := s.ClickByPasteToken(app.ID, "abc", tc.gone, "dev"); !errors.Is(err, ErrNotFound) {
					t.Errorf("replaced token %s: err = %v; want not found", tc.gone, err)
				}
			}
		})
	}
}

// Short keys route case-insensitively: a token copied from /ABC still finds the click on key "abc".
func TestClickByPasteTokenKeyCase(t *testing.T) {
	s := newTestStore(t)
	app, link := setupApp(t, s)
	const tok = "DDDDDDDDDDDDDDDD"
	if _, err := s.RecordClick(Click{AppID: app.ID, LinkID: link.ID, Destination: link.URL, Kind: KindApp,
		Fingerprint: Fingerprint{IP: "203.0.113.7", UserAgent: "ua"}, PasteToken: tok}, 24); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ClickByPasteToken(app.ID, "ABC", tok, "dev"); err != nil {
		t.Errorf("upper-case key: %v; want found", err)
	}
}

func TestStripPasteToken(t *testing.T) {
	for in, want := range map[string]string{
		"https://l.example/abc?_dc=AbCdEfGh12345678":     "https://l.example/abc",
		"https://l.example/abc?x=1&_dc=AbCdEfGh12345678": "https://l.example/abc?x=1",
		"https://l.example/abc":                          "https://l.example/abc",
		"https://l.example/abc?_dc=short":                "https://l.example/abc?_dc=short",
	} {
		if got := StripPasteToken(in); got != want {
			t.Errorf("StripPasteToken(%q) = %q; want %q", in, got, want)
		}
	}
}

// DB from before paste_token: Open adds the column and its index (index must not live in schema.sql).
func TestOpenAddsPasteToken(t *testing.T) {
	path := t.TempDir() + "/old.db"
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{`DROP INDEX idx_clicks_paste_token`, `ALTER TABLE clicks DROP COLUMN paste_token`} {
		if _, err := s.db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatalf("reopen old DB: %v", err)
	}
	defer s.Close()
	if !columnExists(s.db, "clicks", "paste_token") {
		t.Error("paste_token column missing")
	}
}

// Paste token outlives the fingerprint scrub, like click_id (M15).
func TestPasteTokenSurvivesScrub(t *testing.T) {
	s := newTestStore(t)
	s.ClickIDHours = 30 * 24
	app, link := setupApp(t, s)
	const tok = "EEEEEEEEEEEEEEEE"
	c, err := s.RecordClick(Click{AppID: app.ID, LinkID: link.ID, Destination: link.URL, Kind: KindApp,
		Fingerprint: Fingerprint{IP: "203.0.113.7", UserAgent: "ua"}, PasteToken: tok}, 24)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.PurgeExpired(time.Now().Add(48*time.Hour), 24); err != nil {
		t.Fatal(err)
	}
	got, err := s.ClickByPasteToken(app.ID, "abc", tok, "dev")
	if err != nil || got.ID != c.ID || got.Fingerprint.IP != "" {
		t.Fatalf("after scrub: %+v, %v; want same click, fingerprint scrubbed", got, err)
	}
}
