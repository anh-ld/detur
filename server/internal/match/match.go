// Package match: deterministic clickId lookup, else probabilistic fingerprint scoring against documented Detour weights.
package match

import (
	"errors"
	"fmt"
	"time"

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
type Result struct {
	Matched     bool
	Click       store.Click
	Destination string
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

// Match: deterministic when ClickID present (no settings read: a settings failure never blocks a clickId match), else probabilistic with the app's threshold/window.
func Match(st *store.Store, appID string, req Request) (Result, error) {
	// Same-device retry: already attributed to a retained click, same answer, no new consumption.
	if req.DeviceHash != "" {
		// lookup error falls through to normal matching: backend trouble never denies a link
		if c, err := st.PriorMatch(appID, req.DeviceHash); err == nil {
			return Result{Matched: true, Click: c, Destination: c.Destination}, nil
		}
	}
	// Deterministic lookup has no window; unknown clickId is no-match, never probabilistic fallback.
	if req.ClickID != "" {
		c, err := st.ClickByClickID(appID, req.ClickID)
		if errors.Is(err, store.ErrNotFound) {
			return Result{}, nil
		}
		if err != nil {
			return Result{}, err
		}
		return claim(st, c)
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
	now := time.Now()
	ref := now
	if t := fp.CapturedAt; !t.IsZero() && t.After(now.Add(-MaxWindow*time.Minute)) && t.Before(now.Add(time.Minute)) {
		ref = t
	}
	clicks, err := st.ClicksSince(appID, ref.Add(-time.Duration(app.MatchWindowMinutes)*time.Minute))
	if err != nil {
		return Result{}, err
	}
	best, bestClick := -1, store.Click{}
	for _, c := range clicks { // newest first; strict > keeps newer click on ties
		if s := Score(c, fp, req.IP); s >= app.MatchThreshold && s > best {
			best, bestClick = s, c
		}
	}
	if best >= 0 {
		return claim(st, bestClick)
	}
	return Result{}, nil
}

// claim: mark click matched; losing the race is no-match.
func claim(st *store.Store, c store.Click) (Result, error) {
	ok, err := st.MarkClickMatched(c.ID)
	if err != nil {
		return Result{}, err
	}
	if !ok {
		// lost the claim race: rare (two first-opens on one click within ms), falls to organic
		return Result{}, nil
	}
	return Result{Matched: true, Click: c, Destination: c.Destination}, nil
}
