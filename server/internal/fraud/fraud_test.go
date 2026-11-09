package fraud

import (
	"errors"
	"net/netip"
	"testing"
)

func TestHosting(t *testing.T) {
	cases := []struct {
		ip   string
		want bool
	}{
		{"3.5.140.1", true},
		{"34.64.0.1", true},
		{"73.15.1.1", false},
		{"2600:1f18::1", true},
		{"::ffff:3.5.140.1", true},
		{"10.0.0.1", false},
		{"127.0.0.1", false},
		{"::1", false},
		{"", false},
		{"not-an-ip", false},
		{"172.224.226.1", false},
		{"104.28.28.1", false},
		{"2a02:26f7:b3c0:4000::1", false},
	}
	for _, c := range cases {
		if got := Hosting(c.ip); got != c.want {
			t.Errorf("Hosting(%q) = %v; want %v", c.ip, got, c.want)
		}
	}
}

func TestHostingBoundaries(t *testing.T) {
	for _, tab := range []struct {
		data []byte
		size int
	}{{hosting4, 4}, {hosting6, 16}} {
		if len(tab.data) == 0 || len(tab.data)%(2*tab.size) != 0 {
			t.Fatalf("bad table length %d", len(tab.data))
		}
		rec := tab.data[:2*tab.size]
		lo, _ := netip.AddrFromSlice(rec[:tab.size])
		hi, _ := netip.AddrFromSlice(rec[tab.size:])
		next, _ := netip.AddrFromSlice(tab.data[2*tab.size : 3*tab.size])
		last := tab.data[len(tab.data)-2*tab.size:]
		lastLo, _ := netip.AddrFromSlice(last[:tab.size])
		lastHi, _ := netip.AddrFromSlice(last[tab.size:])
		if !lastHi.Next().IsValid() {
			t.Fatalf("last interval ends at the top of the address space; no address above it to test")
		}
		for ip, want := range map[netip.Addr]bool{lo: true, hi: true, hi.Next(): hi.Next() == next, lo.Prev(): false,
			lastLo: true, lastHi: true, lastHi.Next(): false} {
			if got := Hosting(ip.String()); got != want {
				t.Errorf("Hosting(%s) = %v; want %v", ip, got, want)
			}
		}
	}
}

func TestSuspectUA(t *testing.T) {
	cases := []struct {
		ua   string
		want bool
	}{
		{"", true},
		{"   ", true},
		{"python-requests/2.31", true},
		{"Mozilla/5.0 (compatible; Googlebot/2.1)", true},
		{"mozilla/5.0 (compatible; GOOGLEBOT/2.1)", true},
		{"Sogou web spider/4.0", true},
		{"Mozilla/5.0 (iPhone; CPU iPhone OS 17_2 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.2 Mobile/15E148 Safari/604.1", false},
		{"Mozilla/5.0 (Linux; Android 14; Pixel 7 Build/TQ3A.230805.001) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Mobile Safari/537.36", false},
		{"Mozilla/5.0 (iPhone; CPU iPhone OS 17_2 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Mobile/15E148 [Pinterest/iOS]", false},
	}
	for _, c := range cases {
		if got := SuspectUA(c.ua); got != c.want {
			t.Errorf("SuspectUA(%q) = %v; want %v", c.ua, got, c.want)
		}
	}
}

func TestValidate(t *testing.T) {
	if err := Defaults.Validate(); err != nil {
		t.Fatalf("defaults: %v", err)
	}
	cases := []struct {
		name string
		edit func(*Settings)
		want error
	}{
		{"velocity mode", func(s *Settings) { s.VelocityMode = "off" }, ErrModeInvalid},
		{"timing mode", func(s *Settings) { s.TimingMode = "" }, ErrModeInvalid},
		{"ua mode", func(s *Settings) { s.UserAgentMode = "Active" }, ErrModeInvalid},
		{"ip mode", func(s *Settings) { s.IPMode = "x" }, ErrModeInvalid},
		{"ip max low", func(s *Settings) { s.VelocityIPMax = 1 }, ErrVelocityIPMaxOutOfRange},
		{"ip max high", func(s *Settings) { s.VelocityIPMax = 10001 }, ErrVelocityIPMaxOutOfRange},
		{"link max low", func(s *Settings) { s.VelocityLinkMax = 1 }, ErrVelocityLinkMaxOutOfRange},
		{"link max high", func(s *Settings) { s.VelocityLinkMax = 100001 }, ErrVelocityLinkMaxOutOfRange},
		{"window low", func(s *Settings) { s.VelocityWindowMinutes = 4 }, ErrVelocityWindowOutOfRange},
		{"window high", func(s *Settings) { s.VelocityWindowMinutes = 1441 }, ErrVelocityWindowOutOfRange},
		{"short low", func(s *Settings) { s.TimingShortSeconds = 0 }, ErrTimingShortOutOfRange},
		{"short high", func(s *Settings) { s.TimingShortSeconds = 301 }, ErrTimingShortOutOfRange},
		{"long low", func(s *Settings) { s.TimingLongHours = 0 }, ErrTimingLongOutOfRange},
		{"long high", func(s *Settings) { s.TimingLongHours = 721 }, ErrTimingLongOutOfRange},
		{"fp max low", func(s *Settings) { s.FingerprintMax = 1 }, ErrFingerprintMaxOutOfRange},
		{"fp max high", func(s *Settings) { s.FingerprintMax = 1001 }, ErrFingerprintMaxOutOfRange},
		{"fp window low", func(s *Settings) { s.FingerprintWindowDays = 0 }, ErrFingerprintWindowOutOfRange},
		{"fp window high", func(s *Settings) { s.FingerprintWindowDays = 31 }, ErrFingerprintWindowOutOfRange},
	}
	for _, c := range cases {
		s := Defaults
		c.edit(&s)
		if err := s.Validate(); !errors.Is(err, c.want) {
			t.Errorf("%s: Validate() = %v; want %v", c.name, err, c.want)
		}
	}
	s := Defaults
	s.VelocityMode, s.IPMode = ModeActive, ModeActive
	s.VelocityIPMax, s.VelocityLinkMax, s.VelocityWindowMinutes = 10000, 2, 1440
	s.TimingShortSeconds, s.TimingLongHours = 1, 720
	s.FingerprintMax, s.FingerprintWindowDays = 1000, 1
	if err := s.Validate(); err != nil {
		t.Errorf("edges: %v", err)
	}
}

func TestRoutable(t *testing.T) {
	cases := []struct {
		ip   string
		want string
	}{
		{"203.0.113.7", "203.0.113.7"},
		{"::ffff:203.0.113.7", "203.0.113.7"},
		{"2001:db8:1:2::1", "2001:db8:1:2::1"},
		{"10.0.0.1", ""},
		{"192.168.1.1", ""},
		{"fd00::1", ""},
		{"169.254.10.1", ""},
		{"fe80::1", ""},
		{"127.0.0.1", ""},
		{"::1", ""},
		{"0.0.0.0", ""},
		{"::", ""},
		{"", ""},
		{"203.0.113", ""},
	}
	for _, c := range cases {
		a, ok := Routable(c.ip)
		if ok != (c.want != "") || (ok && a.String() != c.want) {
			t.Errorf("Routable(%q) = %v, %v; want %q", c.ip, a, ok, c.want)
		}
	}
}

func TestSettingsActive(t *testing.T) {
	all := Defaults
	all.VelocityMode, all.TimingMode, all.UserAgentMode, all.IPMode = ModeActive, ModeActive, ModeActive, ModeActive
	for _, c := range []struct {
		sig  string
		mode func(*Settings) *string
	}{
		{SignalVelocity, func(s *Settings) *string { return &s.VelocityMode }},
		{SignalTiming, func(s *Settings) *string { return &s.TimingMode }},
		{SignalUserAgent, func(s *Settings) *string { return &s.UserAgentMode }},
		{SignalIP, func(s *Settings) *string { return &s.IPMode }},
	} {
		one := Defaults
		*c.mode(&one) = ModeActive
		for _, sig := range Signals {
			if got := one.Active(sig); got != (sig == c.sig) {
				t.Errorf("only %s active: Active(%s) = %v; want %v", c.sig, sig, got, sig == c.sig)
			}
		}
		if !one.AnyActive() {
			t.Errorf("only %s active: AnyActive() = false; want true", c.sig)
		}
	}
	for _, sig := range []string{SignalInstallIP, SignalFingerprint} {
		if all.Active(sig) {
			t.Errorf("all modes active: Active(%s) = true; want false (no mode)", sig)
		}
	}
	if Defaults.AnyActive() {
		t.Error("Defaults.AnyActive() = true; want false")
	}
}
