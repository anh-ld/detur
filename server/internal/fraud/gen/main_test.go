package main

import (
	"fmt"
	"net/netip"
	"testing"
)

func spans(cidrs ...string) []span {
	var s []span
	for _, c := range cidrs {
		s = append(s, prefixSpan(netip.MustParsePrefix(c)))
	}
	return s
}

// TestMergeSubtract: adjacent prefixes coalesce, families stay apart, egress cuts holes and trims edges.
func TestMergeSubtract(t *testing.T) {
	h := merge(spans("10.0.1.0/24", "10.0.0.0/24", "10.0.2.0/25", "2001:db8::/32", "2001:db9::/32"))
	e := merge(spans("10.0.0.0/28", "10.0.1.0/30", "10.0.2.64/26", "2001:db9::/33"))
	got := ""
	for _, s := range subtract(h, e) {
		got += fmt.Sprintf("%s-%s ", s.lo, s.hi)
	}
	want := "10.0.0.16-10.0.0.255 10.0.1.4-10.0.2.63 2001:db8::-2001:db8:ffff:ffff:ffff:ffff:ffff:ffff 2001:db9:8000::-2001:db9:ffff:ffff:ffff:ffff:ffff:ffff "
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}
