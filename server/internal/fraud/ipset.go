package fraud

import (
	"bytes"
	_ "embed"
	"net/netip"
	"sort"
)

// hosting4/hosting6: sorted, non-overlapping hosting intervals as big-endian lo,hi pairs (gen/main.go); searched in place, nothing to parse.
var (
	//go:embed data/hosting4.bin
	hosting4 []byte
	//go:embed data/hosting6.bin
	hosting6 []byte
)

// Routable: parsed public address (IPv4-in-IPv6 unmapped); false for empty, unparsable, private, loopback, link-local or unspecified. Behind a misconfigured proxy every client shows a private peer, so IP signals skip those (KTD8).
func Routable(ip string) (netip.Addr, bool) {
	a, err := netip.ParseAddr(ip)
	if err != nil {
		return netip.Addr{}, false
	}
	a = a.Unmap()
	return a, !(a.IsPrivate() || a.IsLoopback() || a.IsLinkLocalUnicast() || a.IsUnspecified())
}

// Hosting: ip falls in a bundled datacenter/proxy range. Private, loopback, empty and unparsable input is never hosting (KTD8).
func Hosting(ip string) bool {
	a, ok := Routable(ip)
	if !ok {
		return false
	}
	tab, n := hosting6, 16
	if a.Is4() {
		tab, n = hosting4, 4
	}
	key := a.AsSlice()
	rec := 2 * n
	i := sort.Search(len(tab)/rec, func(i int) bool { // first interval whose hi >= ip
		return bytes.Compare(tab[i*rec+n:(i+1)*rec], key) >= 0
	})
	return i < len(tab)/rec && bytes.Compare(tab[i*rec:i*rec+n], key) <= 0
}
