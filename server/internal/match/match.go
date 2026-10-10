// Package match: deterministic clickId lookup, else probabilistic fingerprint scoring against documented Detour weights.
package match

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"detur.dev/server/internal/fraud"
	"detur.dev/server/internal/store"
)

// Configurable matching bounds: threshold 700..1200, window 5..180 minutes.
const (
	MinThreshold = 700
	MaxThreshold = 1200
	MinWindow    = 5
	MaxWindow    = 180
)

// Errors returned for out-of-range matching parameters.
var (
	ErrThresholdOutOfRange = errors.New("threshold out of range 700..1200")
	ErrWindowOutOfRange    = errors.New("window out of range 5..180 minutes")
)

// Fingerprint: first-launch device fingerprint SDK sends on match-link (full payload, no clickId). Platform not stored: scoring engine derives it from click's UA (one-device-signal rule).
type Fingerprint struct {
	Model, Manufacturer, SystemVersion      string
	ScreenWidth, ScreenHeight               int
	Scale                                   float64
	Locale, Timezone, UserAgent, PastedLink string
	CapturedAt                              time.Time // SDK capture time; zero = unknown
}

// Request: match-link request, exact ClickID (Android Play referrer) or Fingerprint payload, never both.
type Request struct {
	ClickID     string
	IP          string
	DeviceHash  string
	Fingerprint *Fingerprint
}

// Result: match outcome. Matched=false is no-match (404, organic install).
// Method/Score/RunnerUp: receipt (store.Method*). Score kept on no-match; -1 = no candidate.
type Result struct {
	Matched     bool
	Click       store.Click
	Destination string
	Method      string
	Score       int
	RunnerUp    int
	// Fraud labels (KTD7, store.Install): Fraud = fired signals comma list; FraudAction = store.FraudAction*; FraudLinkID = excluded best click's link.
	Fraud, FraudAction, FraudLinkID string
}

// ValidateSettings: app match settings within Detour's documented ranges.
func ValidateSettings(threshold, windowMinutes int) error {
	if windowMinutes < MinWindow || windowMinutes > MaxWindow {
		return fmt.Errorf("%w: got %d", ErrWindowOutOfRange, windowMinutes)
	}
	if threshold < MinThreshold || threshold > MaxThreshold {
		return fmt.Errorf("%w: got %d", ErrThresholdOutOfRange, threshold)
	}
	return nil
}

// Match: deterministic when ClickID present, else probabilistic with the app's threshold/window. Fraud settings apply to both; active-flagged clicks are excluded (KTD4).
func Match(st *store.Store, appID string, req Request) (Result, error) {
	// Same-device retry: already attributed to a retained click, same answer, no new consumption.
	if req.DeviceHash != "" {
		// lookup error falls through to normal matching: backend trouble never denies a link
		if c, err := st.PriorMatch(appID, req.DeviceHash); err == nil {
			return Result{Matched: true, Click: c, Destination: c.Destination, Method: store.MethodPrior, Score: -1, RunnerUp: -1}, nil
		}
	}
	// a settings read error comes back with Defaults (every signal tagged), so a clickId match is never blocked (KTD4); the error surfaces on the next portal settings read
	set, _ := st.FraudSettings(appID)
	now := time.Now()
	// Deterministic lookup has no window; unknown clickId is no-match, never probabilistic fallback.
	if req.ClickID != "" {
		c, err := st.ClickByClickID(appID, req.ClickID, req.DeviceHash)
		if errors.Is(err, store.ErrNotFound) {
			return label(noMatch(-1, -1), req), nil
		}
		if err != nil {
			return Result{}, err
		}
		f := fired(c, set, now, true)
		if excluded(f, set) { // flagged clickId: organic, click left unconsumed, no fingerprint to fall back on (R7)
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

	// Probabilistic: window scan per app (match-link fingerprint carries no link identity).
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
	// ref: Detour measures the window from the fingerprint timestamp. Client clocks drift: use it only within MaxWindow of server time, else server now.
	ref := now
	if t := fp.CapturedAt; !t.IsZero() && t.After(now.Add(-MaxWindow*time.Minute)) && t.Before(now.Add(time.Minute)) {
		ref = t
	}
	clicks, err := st.ClicksSince(appID, ref.Add(-time.Duration(app.MatchWindowMinutes)*time.Minute))
	if err != nil {
		return Result{}, err
	}
	// all candidates, not only >= threshold: near misses feed the what-if. top: overall best; best/second: valid (non-excluded) set, reported in the receipt (KTD4).
	top, topClick := -1, store.Click{}
	best, second, bestClick := -1, -1, store.Click{}
	// timing measures to the fingerprint time, never past server now: a client clock running ahead must not stretch "too soon" away
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
		if anyActive && excluded(fired(c, set, tref, false), set) {
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
	if tf := fired(topClick, set, tref, false); top >= app.MatchThreshold && excluded(tf, set) {
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
	if !r.Matched { // lost the claim race: organic, but an excluded best click still took credit away
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

// fired: click-level signals c trips at ref (KTD1, KTD3). Long timing only on the clickId path (R2); open clicks skip short timing.
func fired(c store.Click, set fraud.Settings, ref time.Time, clickID bool) map[string]bool {
	d := ref.Sub(c.FirstSeenAt)
	return map[string]bool{
		fraud.SignalVelocity: c.HitsIP >= set.VelocityIPMax || c.HitsLink >= set.VelocityLinkMax,
		fraud.SignalTiming: (c.Kind != store.KindOpen && d < time.Duration(set.TimingShortSeconds)*time.Second) ||
			(clickID && d > time.Duration(set.TimingLongHours)*time.Hour),
		fraud.SignalUserAgent: c.UASuspect,
		fraud.SignalIP:        c.IPHosting,
	}
}

// excluded: any fired signal in active mode.
func excluded(f map[string]bool, set fraud.Settings) bool {
	for sig, on := range f {
		if on && set.Active(sig) {
			return true
		}
	}
	return false
}

// label: r.Fraud = union of fired sets plus install IP hosting (own tag-only label, so the IP promotion count stays click-level), in fraud.Signals order.
func label(r Result, req Request, sets ...map[string]bool) Result {
	var out []string
	for _, sig := range fraud.Signals {
		on := sig == fraud.SignalInstallIP && fraud.Hosting(req.IP)
		for _, f := range sets {
			on = on || f[sig]
		}
		if on {
			out = append(out, sig)
		}
	}
	r.Fraud = strings.Join(out, ",")
	return r
}

func noMatch(score, runnerUp int) Result {
	return Result{Method: store.MethodOrganic, Score: score, RunnerUp: runnerUp}
}

// claim: mark matched by deviceHash, fill r; lost race to another device = no-match.
func claim(st *store.Store, c store.Click, deviceHash string, r Result) (Result, error) {
	ok, err := st.MarkClickMatched(c.ID, deviceHash)
	if err != nil {
		return Result{}, err
	}
	if !ok {
		// lost the claim race: rare (two first-opens on one click within ms), falls to organic
		return noMatch(r.Score, r.RunnerUp), nil
	}
	r.Matched, r.Click, r.Destination = true, c, c.Destination
	return r, nil
}
