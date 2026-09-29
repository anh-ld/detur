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
}

// Request: match-link request, exact ClickID (Android Play referrer) or Fingerprint payload, never both.
type Request struct {
	ClickID     string
	IP          string
	Fingerprint *Fingerprint
}

// Result: match outcome. Matched=false is no-match (404, organic install).
type Result struct {
	Matched     bool
	Click       store.Click
	Destination string
}

// Match: deterministic resolution when ClickID present, else probabilistic against app's clicks in window. windowMinutes/threshold validated against configurable ranges.
func Match(st *store.Store, appID string, req Request, windowMinutes, threshold int) (Result, error) {
	if windowMinutes < MinWindow || windowMinutes > MaxWindow {
		return Result{}, fmt.Errorf("%w: got %d", ErrWindowOutOfRange, windowMinutes)
	}
	if threshold < MinThreshold || threshold > MaxThreshold {
		return Result{}, fmt.Errorf("%w: got %d", ErrThresholdOutOfRange, threshold)
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
		return Result{Matched: true, Click: c, Destination: c.Destination}, nil
	}

	// Probabilistic: window scan per app (match-link fingerprint carries no link identity). Per-link thresholds/windows override global defaults; links without one use global setting.
	thresholdByLink, windowByLink, err := st.LinkMatchOverrides(appID)
	if err != nil {
		return Result{}, err
	}
	lookback := windowMinutes
	for _, w := range windowByLink {
		lookback = max(lookback, w)
	}
	now := time.Now()
	clicks, err := st.ClicksSince(appID, now.Add(-time.Duration(min(lookback, MaxWindow))*time.Minute))
	if err != nil {
		return Result{}, err
	}
	fp := Fingerprint{}
	if req.Fingerprint != nil {
		fp = *req.Fingerprint
	}
	best, bestClick := -1, store.Click{}
	for _, c := range clicks { // newest first; strict > keeps newer click on ties
		linkWindow := windowByLink[c.LinkID]
		if linkWindow == 0 {
			linkWindow = windowMinutes
		}
		if c.CreatedAt.Before(now.Add(-time.Duration(linkWindow) * time.Minute)) {
			continue
		}
		if s := Score(c, fp, req.IP); s >= thresholdFor(c, thresholdByLink, threshold) && s > best {
			best, bestClick = s, c
		}
	}
	if best >= 0 {
		return Result{Matched: true, Click: bestClick, Destination: bestClick.Destination}, nil
	}
	return Result{}, nil
}

// thresholdFor: per-link threshold when matched click's link sets one, else global default.
func thresholdFor(c store.Click, byLink map[string]int, global int) int {
	if t, ok := byLink[c.LinkID]; ok && t > 0 {
		return t
	}
	return global
}
