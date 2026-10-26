package match

import (
	"testing"
	"time"

	"detur.dev/server/internal/fraud"
	"detur.dev/server/internal/store"
)

const pasteTok = "PPPPPPPPPPPPPPPP"

// tokenClick: iOS click whose copy page put pasteTok on the clipboard.
func tokenClick(link store.Link) store.Click {
	c := iosClick(link)
	c.Fingerprint.PastedLink = "https://lnk.example/abc?_dc=" + pasteTok
	c.PasteToken = pasteTok
	return c
}

// tokenFP: install that pasted pasteTok, on another network (no IP signal).
func tokenFP() *Fingerprint {
	f := iosFP()
	f.PastedLink = "https://lnk.example/abc?_dc=" + pasteTok
	return &f
}

// Token: exact match an hour after the click, far outside the 15 min window.
func TestPasteTokenBeyondWindow(t *testing.T) {
	s, path := newTestStore(t)
	app, link := setupApp(t, s)
	c := recordClickBackdated(t, s, path, tokenClick(link), time.Hour)
	res, err := Match(s, app.ID, Request{IP: otherIP, DeviceHash: "devA", Fingerprint: tokenFP()})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Matched || res.Method != store.MethodPasteboard || res.Click.ID != c.ID {
		t.Errorf("token match = %+v; want pasteboard on %s", res, c.ID)
	}
	// another device with the same clipboard: claimed click is gone, nothing in window -> organic
	if res, _ := Match(s, app.ID, Request{IP: otherIP, DeviceHash: "devB", Fingerprint: tokenFP()}); res.Matched {
		t.Errorf("second device matched: %+v", res)
	}
}

// Unknown token (stale clipboard, forged): falls through to scoring, not organic.
func TestPasteTokenUnknownFallsThrough(t *testing.T) {
	s, _ := newTestStore(t)
	app, link := setupApp(t, s)
	recordClick(t, s, iosClick(link))
	res, err := Match(s, app.ID, Request{IP: testIP, DeviceHash: "devA", Fingerprint: tokenFP()})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Matched || res.Method != store.MethodProbabilistic {
		t.Errorf("unknown token = %+v; want probabilistic", res)
	}
}

// Token on another link of the same app never claims this link's click.
func TestPasteTokenBoundToLink(t *testing.T) {
	s, path := newTestStore(t)
	app, link := setupApp(t, s)
	recordClickBackdated(t, s, path, tokenClick(link), time.Hour)
	fp := tokenFP()
	fp.PastedLink = "https://lnk.example/def?_dc=" + pasteTok
	if res, _ := Match(s, app.ID, Request{IP: otherIP, DeviceHash: "devA", Fingerprint: fp}); res.Matched {
		t.Errorf("token from other link matched: %+v", res)
	}
}

// Flagged token click under an active signal: organic, excluded (F3).
func TestPasteTokenFlagged(t *testing.T) {
	s, path := newTestStore(t)
	app, link := setupApp(t, s)
	setFraud(t, s, app.ID, func(f *fraud.Settings) { f.UserAgentMode = fraud.ModeActive })
	c := tokenClick(link)
	c.UASuspect = true
	recordClickBackdated(t, s, path, c, time.Hour)
	res, err := Match(s, app.ID, Request{IP: otherIP, DeviceHash: "devA", Fingerprint: tokenFP()})
	if err != nil {
		t.Fatal(err)
	}
	if res.Matched || res.FraudAction != store.FraudActionExcluded {
		t.Errorf("flagged token = %+v; want organic, excluded", res)
	}
}

// Token miss (click kept another copy's token): pasteboard still scores like the bare link did.
func TestPasteboardIgnoresTokenDifference(t *testing.T) {
	link := testLink()
	c := iosClick(link)
	c.Fingerprint.PastedLink = "https://lnk.example/abc123?_dc=AAAAAAAAAAAAAAAA"
	fp := iosFP()
	fp.PastedLink = "https://lnk.example/abc123?_dc=BBBBBBBBBBBBBBBB"
	if got := pasteboard(c, fp); got != weightPasteboardToken {
		t.Errorf("pasteboard with different tokens = %d; want %d", got, weightPasteboardToken)
	}
}
