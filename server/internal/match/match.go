// Package match implements the matching engine (U2): a deterministic clickId
// lookup (R5) plus probabilistic fingerprint scoring against the documented
// Detour weights (R6).
package match

import (
	"errors"
	"fmt"
	"time"

	"detur.dev/server/internal/store"
)

// Configurable matching bounds (R6, R14): threshold 700..1200, window
// 5..180 minutes.
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

// Fingerprint is the first-launch device fingerprint the SDK sends on
// match-link (full payload — no clickId). Platform is not stored: the
// scoring engine derives it from the click's UA (R6 one-device-signal rule).
type Fingerprint struct {
	Model, Manufacturer, SystemVersion      string
	ScreenWidth, ScreenHeight               int
	Scale                                   float64
	Locale, Timezone, UserAgent, PastedLink string
}

// Request is a match-link request: either an exact ClickID (Android Play
// referrer) or a Fingerprint payload, never both.
type Request struct {
	ClickID     string
	IP          string
	Fingerprint *Fingerprint
}

// Result carries the match outcome. Matched=false is a no-match (R2: 404,
// organic install).
type Result struct {
	Matched     bool
	Click       store.Click
	Destination string
}

// Match resolves a match-link request deterministically when ClickID is
// present (R5), else probabilistically against the app's clicks in the
// window (R6). windowMinutes and threshold are validated against the
// configurable ranges.
func Match(st *store.Store, appID string, req Request, windowMinutes, threshold int) (Result, error) {
	if windowMinutes < MinWindow || windowMinutes > MaxWindow {
		return Result{}, fmt.Errorf("%w: got %d", ErrWindowOutOfRange, windowMinutes)
	}
	if threshold < MinThreshold || threshold > MaxThreshold {
		return Result{}, fmt.Errorf("%w: got %d", ErrThresholdOutOfRange, threshold)
	}

	// R5: deterministic lookup has no window; an unknown clickId is a
	// no-match — never a probabilistic fallback.
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

	// R6: probabilistic — window scan per app (the match-link fingerprint
	// carries no link identity, verified from SDK source).
	since := time.Now().Add(-time.Duration(windowMinutes) * time.Minute)
	clicks, err := st.ClicksSince(appID, since)
	if err != nil {
		return Result{}, err
	}
	// R14: per-link thresholds override the global default for clicks on
	// that link; links without one use the global setting.
	thresholdByLink, err := st.LinkThresholds(appID)
	if err != nil {
		return Result{}, err
	}
	fp := Fingerprint{}
	if req.Fingerprint != nil {
		fp = *req.Fingerprint
	}
	best, bestClick := -1, store.Click{}
	for _, c := range clicks { // newest first; strict > keeps the newer click on ties
		if s := Score(c, fp, req.IP); s > best {
			best, bestClick = s, c
		}
	}
	if best >= thresholdFor(bestClick, thresholdByLink, threshold) {
		return Result{Matched: true, Click: bestClick, Destination: bestClick.Destination}, nil
	}
	return Result{}, nil
}

// thresholdFor returns the per-link threshold when the matched click's link
// sets one, else the global default.
func thresholdFor(c store.Click, byLink map[string]int, global int) int {
	if t, ok := byLink[c.LinkID]; ok && t > 0 {
		return t
	}
	return global
}
