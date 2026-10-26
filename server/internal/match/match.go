// Package match: deterministic clickId lookup, else probabilistic fingerprint scoring against documented Detour weights.
package match

import (
	"errors"
	"fmt"
	"time"

	"detur.dev/server/internal/fraud"
	"detur.dev/server/internal/store"
)

// Matching bounds: threshold 700..1200, window 5..180 min.
const (
	MinThreshold = 700
	MaxThreshold = 1200
	MinWindow    = 5
	MaxWindow    = 180
)

// Out-of-range matching param errors.
var (
	ErrThresholdOutOfRange = errors.New("threshold out of range 700..1200")
	ErrWindowOutOfRange    = errors.New("window out of range 5..180 minutes")
)

// Fingerprint: first-launch device fingerprint from SDK match-link (full payload, no clickId). No Platform: scoring derives it from click UA (one-device-signal rule).
type Fingerprint struct {
	Model, Manufacturer, SystemVersion      string
	ScreenWidth, ScreenHeight               int
	Scale                                   float64
	Locale, Timezone, UserAgent, PastedLink string
	CapturedAt                              time.Time // SDK capture time; zero = unknown
}

// Request: match-link request: exact ClickID (Android Play referrer) or Fingerprint, never both.
type Request struct {
	ClickID     string
	IP          string
	DeviceHash  string
	Fingerprint *Fingerprint
}

// Result: match outcome. Matched=false = no-match (404, organic install).
// Method/Score/RunnerUp: receipt (store.Method*). Score kept on no-match; -1 = no candidate.
type Result struct {
	Matched     bool
	Click       store.Click
	Destination string
	Method      string
	Score       int
	RunnerUp    int
	// Fraud labels (KTD7, store.Install): Fraud = fired signals, comma list; FraudAction = store.FraudAction*; FraudLinkID = excluded best click's link.
	Fraud, FraudAction, FraudLinkID string
}

// ValidateSettings: match settings within Detour's documented ranges.
func ValidateSettings(threshold, windowMinutes int) error {
	if windowMinutes < MinWindow || windowMinutes > MaxWindow {
		return fmt.Errorf("%w: got %d", ErrWindowOutOfRange, windowMinutes)
	}
	if threshold < MinThreshold || threshold > MaxThreshold {
		return fmt.Errorf("%w: got %d", ErrThresholdOutOfRange, threshold)
	}
	return nil
}

// Match: ClickID -> deterministic, else probabilistic (app threshold/window). Fraud settings apply to both; active-flagged clicks excluded (KTD4).
func Match(st *store.Store, appID string, req Request) (Result, error) {
	// same-device retry: already attributed to retained click -> same answer, no new consumption
	if req.DeviceHash != "" {
		// lookup error -> normal matching: backend trouble never denies a link
		if c, err := st.PriorMatch(appID, req.DeviceHash); err == nil {
			return Result{Matched: true, Click: c, Destination: c.Destination, Method: store.MethodPrior, Score: -1, RunnerUp: -1}, nil
		}
	}
	// settings read error returns Defaults (all tagged) -> clickId match never blocked (KTD4); error surfaces on next portal settings read
	set, _ := st.FraudSettings(appID)
	now := time.Now()
	// deterministic: no window; unknown clickId = no-match, no probabilistic fallback
	if req.ClickID != "" {
		c, err := st.ClickByClickID(appID, req.ClickID, req.DeviceHash)
		if errors.Is(err, store.ErrNotFound) {
			return label(noMatch(-1, -1), req), nil
		}
		if err != nil {
			return Result{}, err
		}
		f := fired(c, set, now, true)
		if set.Excluded(f) { // flagged clickId: organic, click unconsumed, no fingerprint fallback (R7)
			r := noMatch(-1, -1)
			r.FraudAction, r.FraudLinkID = store.FraudActionExcluded, c.LinkID
			return label(r, req, f), nil
		}
		r, err := claim(st, c, req.DeviceHash, Result{Method: store.MethodClickID, Score: -1, RunnerUp: -1})
		if err != nil {
			return Result{}, err
		}
		return label(r, req, f), nil
	}

	// paste token (iOS copy page): exact, no window. Miss or lost claim -> scoring below, never organic (stale clipboard, forged token).
	if req.Fingerprint != nil {
		if key, tok := store.PasteToken(req.Fingerprint.PastedLink); tok != "" {
			c, err := st.ClickByPasteToken(appID, key, tok, req.DeviceHash)
			if err != nil && !errors.Is(err, store.ErrNotFound) {
				return Result{}, err
			}
			if err == nil {
				f := fired(c, set, now, true)
				if set.Excluded(f) { // flagged: organic like a flagged clickId (F3)
					r := noMatch(-1, -1)
					r.FraudAction, r.FraudLinkID = store.FraudActionExcluded, c.LinkID
					return label(r, req, f), nil
				}
				r, err := claim(st, c, req.DeviceHash, Result{Method: store.MethodPasteboard, Score: -1, RunnerUp: -1})
				if err != nil {
					return Result{}, err
				}
				if r.Matched {
					return label(r, req, f), nil
				}
			}
		}
	}

	// probabilistic: window scan per app (fingerprint carries no link identity)
	app, err := st.GetApp(appID)
	if err != nil {
		return Result{}, err
	}
	if err := ValidateSettings(app.MatchThreshold, app.MatchWindowMinutes); err != nil {
		return Result{}, err
	}
	fp := Fingerprint{}
	if req.Fingerprint != nil {
		fp = *req.Fingerprint
	}
	// ref: Detour measures window from fingerprint time. Client clocks drift: use it only within MaxWindow of server time, else server now.
	ref := now
	if t := fp.CapturedAt; !t.IsZero() && t.After(now.Add(-MaxWindow*time.Minute)) && t.Before(now.Add(time.Minute)) {
		ref = t
	}
	clicks, err := st.ClicksSince(appID, ref.Add(-time.Duration(app.MatchWindowMinutes)*time.Minute))
	if err != nil {
		return Result{}, err
	}
	// score all candidates, not just >= threshold: near misses feed what-if. top = overall best; best/second = valid (non-excluded) set, in receipt (KTD4).
	top, topClick := -1, store.Click{}
	best, second, bestClick := -1, -1, store.Click{}
	// timing to fingerprint time, capped at server now: fast client clock must not stretch "too soon" away
	tref := ref
	if tref.After(now) {
		tref = now
	}
	anyActive := set.AnyActive() // all tagged: skip per-candidate checks
	for _, c := range clicks {   // newest first; strict > keeps newer click on ties
		s := Score(c, fp, req.IP)
		if s > top {
			top, topClick = s, c
		}
		if anyActive && set.Excluded(fired(c, set, tref, false)) {
			continue
		}
		if s > best {
			best, second, bestClick = s, best, c
		} else if s > second {
			second = s
		}
	}
	r := noMatch(best, second)
	var f []map[string]bool
	// excluded overall best that would have won: credit moved or lost (R7)
	if tf := fired(topClick, set, tref, false); top >= app.MatchThreshold && set.Excluded(tf) {
		r.FraudAction, r.FraudLinkID, f = store.FraudActionExcluded, topClick.LinkID, append(f, tf)
	}
	if best < app.MatchThreshold {
		return label(r, req, f...), nil
	}
	f = append(f, fired(bestClick, set, tref, false))
	action, linkID := r.FraudAction, r.FraudLinkID
	r, err = claim(st, bestClick, req.DeviceHash, Result{Method: store.MethodProbabilistic, Score: best, RunnerUp: second})
	if err != nil {
		return Result{}, err
	}
	if !r.Matched { // lost claim race: organic, but excluded best click still took credit
		r.FraudAction, r.FraudLinkID = action, linkID
	} else {
		if action != "" {
			r.FraudAction, r.FraudLinkID = store.FraudActionReattributed, linkID
		}
		// fingerprint concentration: install-level, tag-only; count failure = no tag (R5, R7)
		if req.DeviceHash != "" {
			n, err := st.CountFingerprintInstalls(appID, req.DeviceHash, r.Click.LinkID, now.AddDate(0, 0, -set.FingerprintWindowDays))
			if err == nil && n+1 >= set.FingerprintMax {
				f = append(f, map[string]bool{fraud.SignalFingerprint: true})
			}
		}
	}
	return label(r, req, f...), nil
}

// fired: store.Click -> fraud.Click.
func fired(c store.Click, set fraud.Settings, ref time.Time, clickID bool) map[string]bool {
	return set.Fired(fraud.Click{HitsIP: c.HitsIP, HitsLink: c.HitsLink, FirstSeenAt: c.FirstSeenAt, Open: c.Kind == store.KindOpen,
		UASuspect: c.UASuspect, IPHosting: c.IPHosting}, ref, clickID)
}

// label: sets r.Fraud.
func label(r Result, req Request, sets ...map[string]bool) Result {
	r.Fraud = fraud.Label(req.IP, sets...)
	return r
}

func noMatch(score, runnerUp int) Result {
	return Result{Method: store.MethodOrganic, Score: score, RunnerUp: runnerUp}
}

// claim: mark matched by deviceHash, fill r; lost race to other device = no-match.
func claim(st *store.Store, c store.Click, deviceHash string, r Result) (Result, error) {
	ok, err := st.MarkClickMatched(c.ID, deviceHash)
	if err != nil {
		return Result{}, err
	}
	if !ok {
		// lost claim race (2 first-opens, same click, ms apart): organic
		return noMatch(r.Score, r.RunnerUp), nil
	}
	r.Matched, r.Click, r.Destination = true, c, c.Destination
	return r, nil
}
