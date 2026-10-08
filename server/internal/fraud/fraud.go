// Package fraud: signal names, per-app thresholds and validation, plus embedded hosting-IP and bot-UA datasets shared by store, match and portal API. No outbound calls.
package fraud

import (
	"errors"
	"fmt"
)

// Signal names: stored in install labels and settings, shown in the portal.
const (
	SignalVelocity    = "velocity"
	SignalTiming      = "timing"
	SignalUserAgent   = "user_agent"
	SignalIP          = "ip"         // click came from a hosting network
	SignalInstallIP   = "install_ip" // install request came from a hosting network; tag-only (R7)
	SignalFingerprint = "fingerprint"
)

// Signals: every label, in label and report order.
var Signals = []string{SignalVelocity, SignalTiming, SignalUserAgent, SignalIP, SignalInstallIP, SignalFingerprint}

// Modes: tagged labels only; active also excludes flagged clicks from attribution.
const (
	ModeTagged = "tagged"
	ModeActive = "active"
)

// Threshold bounds (inclusive).
const (
	MinVelocityIPMax, MaxVelocityIPMax                 = 2, 10000
	MinVelocityLinkMax, MaxVelocityLinkMax             = 2, 100000
	MinVelocityWindowMinutes, MaxVelocityWindowMinutes = 5, 1440
	MinTimingShortSeconds, MaxTimingShortSeconds       = 1, 300
	MinTimingLongHours, MaxTimingLongHours             = 1, 720
	MinFingerprintMax, MaxFingerprintMax               = 2, 1000
	MinFingerprintWindowDays, MaxFingerprintWindowDays = 1, 30
)

// Errors returned for invalid fraud settings.
var (
	ErrModeInvalid                 = errors.New("mode must be tagged or active")
	ErrVelocityIPMaxOutOfRange     = errors.New("velocity IP max out of range 2..10000")
	ErrVelocityLinkMaxOutOfRange   = errors.New("velocity link max out of range 2..100000")
	ErrVelocityWindowOutOfRange    = errors.New("velocity window out of range 5..1440 minutes")
	ErrTimingShortOutOfRange       = errors.New("timing short out of range 1..300 seconds")
	ErrTimingLongOutOfRange        = errors.New("timing long out of range 1..720 hours")
	ErrFingerprintMaxOutOfRange    = errors.New("fingerprint max out of range 2..1000")
	ErrFingerprintWindowOutOfRange = errors.New("fingerprint window out of range 1..30 days")
)

// Settings: per-app fraud config. Fingerprint has no mode: install-level, tag-only (R7).
type Settings struct {
	VelocityMode, TimingMode, UserAgentMode, IPMode string

	VelocityIPMax, VelocityLinkMax, VelocityWindowMinutes int
	TimingShortSeconds, TimingLongHours                   int
	FingerprintMax, FingerprintWindowDays                 int
}

// Defaults: settings for an app with no stored row; every signal tagged.
var Defaults = Settings{
	VelocityMode: ModeTagged, TimingMode: ModeTagged, UserAgentMode: ModeTagged, IPMode: ModeTagged,
	VelocityIPMax: 20, VelocityLinkMax: 500, VelocityWindowMinutes: 60,
	TimingShortSeconds: 10, TimingLongHours: 24,
	FingerprintMax: 5, FingerprintWindowDays: 7,
}

// Active: sig is in active mode, so the clicks it flags are excluded. Signals without a mode (install IP, fingerprint) never are.
func (s Settings) Active(sig string) bool {
	switch sig {
	case SignalVelocity:
		return s.VelocityMode == ModeActive
	case SignalTiming:
		return s.TimingMode == ModeActive
	case SignalUserAgent:
		return s.UserAgentMode == ModeActive
	case SignalIP:
		return s.IPMode == ModeActive
	}
	return false
}

// AnyActive: some signal can exclude a click; false lets matching skip per-candidate checks.
func (s Settings) AnyActive() bool {
	for _, sig := range Signals {
		if s.Active(sig) {
			return true
		}
	}
	return false
}

// Validate: modes known and thresholds within bounds; errors wrap the typed Err* values.
func (s Settings) Validate() error {
	for _, m := range []string{s.VelocityMode, s.TimingMode, s.UserAgentMode, s.IPMode} {
		if m != ModeTagged && m != ModeActive {
			return fmt.Errorf("%w: got %q", ErrModeInvalid, m)
		}
	}
	for _, r := range []struct {
		v, min, max int
		err         error
	}{
		{s.VelocityIPMax, MinVelocityIPMax, MaxVelocityIPMax, ErrVelocityIPMaxOutOfRange},
		{s.VelocityLinkMax, MinVelocityLinkMax, MaxVelocityLinkMax, ErrVelocityLinkMaxOutOfRange},
		{s.VelocityWindowMinutes, MinVelocityWindowMinutes, MaxVelocityWindowMinutes, ErrVelocityWindowOutOfRange},
		{s.TimingShortSeconds, MinTimingShortSeconds, MaxTimingShortSeconds, ErrTimingShortOutOfRange},
		{s.TimingLongHours, MinTimingLongHours, MaxTimingLongHours, ErrTimingLongOutOfRange},
		{s.FingerprintMax, MinFingerprintMax, MaxFingerprintMax, ErrFingerprintMaxOutOfRange},
		{s.FingerprintWindowDays, MinFingerprintWindowDays, MaxFingerprintWindowDays, ErrFingerprintWindowOutOfRange},
	} {
		if r.v < r.min || r.v > r.max {
			return fmt.Errorf("%w: got %d", r.err, r.v)
		}
	}
	return nil
}
