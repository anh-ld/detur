package fraud

import (
	"bytes"
	_ "embed"
	"net/netip"
	"sort"
)

var (
	//go:embed data/hosting4.bin
	hosting4 []byte
	//go:embed data/hosting6.bin
	hosting6 []byte
)

func Routable(ip string) (netip.Addr, bool) {
	a, err := netip.ParseAddr(ip)
	if err != nil {
		return netip.Addr{}, false
	}
	a = a.Unmap()
	return a, !(a.IsPrivate() || a.IsLoopback() || a.IsLinkLocalUnicast() || a.IsUnspecified())
}

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
	i := sort.Search(len(tab)/rec, func(i int) bool {
		return bytes.Compare(tab[i*rec+n:(i+1)*rec], key) >= 0
	})
	return i < len(tab)/rec && bytes.Compare(tab[i*rec:i*rec+n], key) <= 0
}
