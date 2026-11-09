// Package fraud: signals, settings, evaluation, hosting-IP and bot-UA data. No network.
package fraud

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	SignalVelocity    = "velocity"
	SignalTiming      = "timing"
	SignalUserAgent   = "user_agent"
	SignalIP          = "ip"
	SignalInstallIP   = "install_ip"
	SignalFingerprint = "fingerprint"
)

var Signals = []string{SignalVelocity, SignalTiming, SignalUserAgent, SignalIP, SignalInstallIP, SignalFingerprint}

const (
	ModeTagged = "tagged"
	ModeActive = "active"
)

const (
	MinVelocityIPMax, MaxVelocityIPMax                 = 2, 10000
	MinVelocityLinkMax, MaxVelocityLinkMax             = 2, 100000
	MinVelocityWindowMinutes, MaxVelocityWindowMinutes = 5, 1440
	MinTimingShortSeconds, MaxTimingShortSeconds       = 1, 300
	MinTimingLongHours, MaxTimingLongHours             = 1, 720
	MinFingerprintMax, MaxFingerprintMax               = 2, 1000
	MinFingerprintWindowDays, MaxFingerprintWindowDays = 1, 30
)

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

type Settings struct {
	VelocityMode, TimingMode, UserAgentMode, IPMode string

	VelocityIPMax, VelocityLinkMax, VelocityWindowMinutes int
	TimingShortSeconds, TimingLongHours                   int
	FingerprintMax, FingerprintWindowDays                 int
}

var Defaults = Settings{
	VelocityMode: ModeTagged, TimingMode: ModeTagged, UserAgentMode: ModeTagged, IPMode: ModeTagged,
	VelocityIPMax: 20, VelocityLinkMax: 500, VelocityWindowMinutes: 60,
	TimingShortSeconds: 10, TimingLongHours: 24,
	FingerprintMax: 5, FingerprintWindowDays: 7,
}

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

func (s Settings) AnyActive() bool {
	for _, sig := range Signals {
		if s.Active(sig) {
			return true
		}
	}
	return false
}

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

type Click struct {
	HitsIP, HitsLink     int
	FirstSeenAt          time.Time
	Open                 bool
	UASuspect, IPHosting bool
}

type Flags struct{ Velocity, Timing, UserAgent, IP, Fingerprint bool }

func (f Flags) has(sig string) bool {
	switch sig {
	case SignalVelocity:
		return f.Velocity
	case SignalTiming:
		return f.Timing
	case SignalUserAgent:
		return f.UserAgent
	case SignalIP:
		return f.IP
	case SignalFingerprint:
		return f.Fingerprint
	}
	return false
}

func (s Settings) Fired(c Click, ref time.Time, clickID bool) Flags {
	d := ref.Sub(c.FirstSeenAt)
	return Flags{
		Velocity: c.HitsIP >= s.VelocityIPMax || c.HitsLink >= s.VelocityLinkMax,
		Timing: (!c.Open && d < time.Duration(s.TimingShortSeconds)*time.Second) ||
			(clickID && d > time.Duration(s.TimingLongHours)*time.Hour),
		UserAgent: c.UASuspect,
		IP:        c.IPHosting,
	}
}

func (s Settings) Excluded(f Flags) bool {
	for _, sig := range Signals {
		if f.has(sig) && s.Active(sig) {
			return true
		}
	}
	return false
}

func Label(installIP string, sets ...Flags) string {
	var out []string
	for _, sig := range Signals {
		on := sig == SignalInstallIP && Hosting(installIP)
		for _, f := range sets {
			on = on || f.has(sig)
		}
		if on {
			out = append(out, sig)
		}
	}
	return strings.Join(out, ",")
}
