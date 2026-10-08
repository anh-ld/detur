package fraud

import (
	"errors"
	"net/netip"
	"testing"
)

// Hosting: bundled datacenter ranges hit (v4, v6, IPv4-mapped); residential, non-routable and Private Relay egress miss.
func TestHosting(t *testing.T) {
	cases := []struct {
		ip   string
		want bool
	}{
		{"3.5.140.1", true},               // AWS
		{"34.64.0.1", true},               // GCP
		{"73.15.1.1", false},              // Comcast residential
		{"2600:1f18::1", true},            // AWS v6
		{"::ffff:3.5.140.1", true},        // IPv4-mapped AWS
		{"10.0.0.1", false},               // private (KTD8)
		{"127.0.0.1", false},              // loopback
		{"::1", false},                    // loopback v6
		{"", false},                       // empty
		{"not-an-ip", false},              // unparsable
		{"172.224.226.1", false},          // Private Relay egress (Akamai)
		{"104.28.28.1", false},            // Private Relay egress (Cloudflare)
		{"2a02:26f7:b3c0:4000::1", false}, // Private Relay egress v6 (Akamai)
	}
	for _, c := range cases {
		if got := Hosting(c.ip); got != c.want {
			t.Errorf("Hosting(%q) = %v; want %v", c.ip, got, c.want)
		}
	}
}

// TestHostingBoundaries: first and last address of the first and last shipped intervals hit; one below the first and one above the last miss.
func TestHostingBoundaries(t *testing.T) {
	for _, tab := range []struct {
		data []byte
		size int
	}{{hosting4, 4}, {hosting6, 16}} {
		if len(tab.data) == 0 || len(tab.data)%(2*tab.size) != 0 {
			t.Fatalf("bad table length %d", len(tab.data))
		}
		rec := tab.data[:2*tab.size] // first interval: nothing shipped below it
		lo, _ := netip.AddrFromSlice(rec[:tab.size])
		hi, _ := netip.AddrFromSlice(rec[tab.size:])
		next, _ := netip.AddrFromSlice(tab.data[2*tab.size : 3*tab.size])
		last := tab.data[len(tab.data)-2*tab.size:] // last interval: nothing shipped above it
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

// SuspectUA: empty/blank or bundled bot substring (case-insensitive, multi-word kept whole); real mobile browsers and in-app UAs pass.
func TestSuspectUA(t *testing.T) {
	cases := []struct {
		ua   string
		want bool
	}{
		{"", true},
		{"   ", true},
		{"python-requests/2.31", true},
		{"Mozilla/5.0 (compatible; Googlebot/2.1)", true},
		{"mozilla/5.0 (compatible; GOOGLEBOT/2.1)", true}, // case-insensitive
		{"Sogou web spider/4.0", true},                    // multi-word pattern kept whole
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

// Validate: defaults and inclusive range edges pass; each bad mode and each one-past-edge threshold returns its typed error.
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
	// Range edges accepted.
	s := Defaults
	s.VelocityMode, s.IPMode = ModeActive, ModeActive
	s.VelocityIPMax, s.VelocityLinkMax, s.VelocityWindowMinutes = 10000, 2, 1440
	s.TimingShortSeconds, s.TimingLongHours = 1, 720
	s.FingerprintMax, s.FingerprintWindowDays = 1000, 1
	if err := s.Validate(); err != nil {
		t.Errorf("edges: %v", err)
	}
}

// Routable: public addresses pass (IPv4-mapped unmapped to v4); empty, unparsable, private, loopback, unspecified do not (KTD8).
func TestRoutable(t *testing.T) {
	cases := []struct {
		ip   string
		want string // "" = not routable
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

// Active: each mode field drives only its own signal; install_ip and fingerprint have no mode and are never active. AnyActive: any one mode.
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
