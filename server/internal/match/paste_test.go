package match

import (
	"testing"
	"time"

	"detur.dev/server/internal/fraud"
	"detur.dev/server/internal/store"
)

const pasteTok = "PPPPPPPPPPPPPPPP"

func tokenClick(link store.Link) store.Click {
	c := iosClick(link)
	c.Fingerprint.PastedLink = "https://lnk.example/abc?_dc=" + pasteTok
	c.PasteToken = pasteTok
	return c
}

func tokenFP() *Fingerprint {
	f := iosFP()
	f.PastedLink = "https://lnk.example/abc?_dc=" + pasteTok
	return &f
}

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
	if res, _ := Match(s, app.ID, Request{IP: otherIP, DeviceHash: "devB", Fingerprint: tokenFP()}); res.Matched {
		t.Errorf("second device matched: %+v", res)
	}
}

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
