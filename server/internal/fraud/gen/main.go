// Command gen: dev-only refresh of the fraud package's embedded datasets; run by hand from server/internal/fraud with `go run ./gen`, never at build or runtime.
//
// Writes data/hosting4.bin and data/hosting6.bin (sorted, merged hosting intervals as big-endian lo,hi address pairs: 8 and 32 bytes per record; Apple Private Relay egress subtracted) and data/bots.txt (bot UA substrings, one per line).
package main

import (
	"archive/tar"
	"compress/gzip"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

const (
	ipverseURL = "https://github.com/ipverse/as-ip-blocks/releases/latest/download/as-ip-blocks.tar.gz"
	appleURL   = "https://mask-api.icloud.com/egress-ip-ranges.csv"
	posthogURL = "https://raw.githubusercontent.com/PostHog/posthog/master/livestream/bot/definitions.json"
)

// cdnOrg: Akamai, Cloudflare and Fastly carry real-user egress (Private Relay, WARP), so their ASNs are dropped; Linode is Akamai-owned VPS hosting and stays.
var cdnOrg = regexp.MustCompile(`(?i)akamai|cloudflare|fastly`)

// span: inclusive address interval.
type span struct{ lo, hi netip.Addr }

func main() {
	out := flag.String("out", "data", "output directory")
	flag.Parse()

	hosting, asns, err := hostingSpans()
	if err != nil {
		log.Fatal(err)
	}
	egress, err := appleSpans()
	if err != nil {
		log.Fatal(err)
	}
	merged := merge(hosting)
	final := subtract(merged, merge(egress))
	log.Printf("intervals before Private Relay subtraction %d, after %d", len(merged), len(final))

	var v4, v6 []byte
	for _, s := range final {
		if s.lo.Is4() {
			v4 = append(append(v4, s.lo.AsSlice()...), s.hi.AsSlice()...)
		} else {
			v6 = append(append(v6, s.lo.AsSlice()...), s.hi.AsSlice()...)
		}
	}
	write(filepath.Join(*out, "hosting4.bin"), v4)
	write(filepath.Join(*out, "hosting6.bin"), v6)

	bots, err := botSubstrings()
	if err != nil {
		log.Fatal(err)
	}
	write(filepath.Join(*out, "bots.txt"), []byte(strings.Join(bots, "\n")+"\n"))
	log.Printf("hosting ASNs %d, intervals %d, bot substrings %d", asns, len(final), len(bots))
}

func write(path string, b []byte) {
	if err := os.WriteFile(path, b, 0o644); err != nil {
		log.Fatal(err)
	}
}

func get(url string) (io.ReadCloser, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	return resp.Body, nil
}

// hostingSpans: prefixes of ipverse ASNs with category "hosting", minus CDN orgs.
func hostingSpans() ([]span, int, error) {
	body, err := get(ipverseURL)
	if err != nil {
		return nil, 0, err
	}
	defer body.Close()
	gz, err := gzip.NewReader(body)
	if err != nil {
		return nil, 0, err
	}
	tr := tar.NewReader(gz)
	var spans []span
	asns := 0
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, 0, err
		}
		if filepath.Base(h.Name) != "aggregated.json" {
			continue
		}
		var as struct {
			Metadata struct{ Handle, Description, Category string }
			Prefixes struct{ IPv4, IPv6 []string }
		}
		if err := json.NewDecoder(tr).Decode(&as); err != nil {
			return nil, 0, fmt.Errorf("%s: %w", h.Name, err)
		}
		m := as.Metadata
		if m.Category != "hosting" || (cdnOrg.MatchString(m.Description+" "+m.Handle) && !strings.Contains(strings.ToUpper(m.Handle), "LINODE")) {
			continue
		}
		asns++
		for _, p := range append(as.Prefixes.IPv4, as.Prefixes.IPv6...) {
			pp, err := netip.ParsePrefix(p)
			if err != nil {
				return nil, 0, fmt.Errorf("%s: %w", h.Name, err)
			}
			spans = append(spans, prefixSpan(pp))
		}
	}
	if asns == 0 {
		return nil, 0, fmt.Errorf("no hosting ASNs found: ipverse category field changed?")
	}
	return spans, asns, nil
}

// appleSpans: Private Relay egress ranges, used only to subtract; never written out.
func appleSpans() ([]span, error) {
	body, err := get(appleURL)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	r := csv.NewReader(body)
	r.FieldsPerRecord = -1
	var spans []span
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		p, err := netip.ParsePrefix(rec[0])
		if err != nil {
			return nil, err
		}
		spans = append(spans, prefixSpan(p))
	}
	if len(spans) == 0 {
		return nil, fmt.Errorf("apple egress list empty")
	}
	return spans, nil
}

// botSubstrings: PostHog bot patterns (already de-regexed to literal substrings), deduped case-insensitively.
func botSubstrings() ([]string, error) {
	body, err := get(posthogURL)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	var defs []struct{ Pattern string }
	if err := json.NewDecoder(body).Decode(&defs); err != nil {
		return nil, err
	}
	// in-app browser tokens: real users tapping links inside these apps; their crawlers carry "bot" and C3 drops them already
	seen := map[string]bool{"pinterest": true, "whatsapp": true, "viber": true}
	var out []string
	for _, d := range defs {
		k := strings.ToLower(strings.TrimSpace(d.Pattern))
		if k == "" || strings.Contains(k, "\n") || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, d.Pattern)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no bot patterns")
	}
	return out, nil
}

// prefixSpan: first and last address of a prefix.
func prefixSpan(p netip.Prefix) span {
	p = p.Masked()
	lo := p.Addr()
	b := lo.As16()
	off := 0
	if lo.Is4() {
		off = 96
	}
	for i := off + p.Bits(); i < 128; i++ {
		b[i/8] |= 1 << (7 - i%8)
	}
	hi := netip.AddrFrom16(b)
	if lo.Is4() {
		hi = hi.Unmap()
	}
	return span{lo, hi}
}

// merge: sort and coalesce overlapping or adjacent spans.
func merge(s []span) []span {
	slices.SortFunc(s, func(a, b span) int { return a.lo.Compare(b.lo) })
	var out []span
	for _, x := range s {
		if n := len(out); n > 0 && out[n-1].lo.BitLen() == x.lo.BitLen() {
			last := &out[n-1]
			if next := last.hi.Next(); !next.IsValid() || !next.Less(x.lo) {
				if last.hi.Less(x.hi) {
					last.hi = x.hi
				}
				continue
			}
		}
		out = append(out, x)
	}
	return out
}

// subtract: h minus e; both sorted and merged.
func subtract(h, e []span) []span {
	var out []span
	j := 0
	for _, x := range h {
		for j < len(e) && e[j].hi.Less(x.lo) {
			j++
		}
		lo := x.lo
		for k := j; k < len(e) && !x.hi.Less(e[k].lo); k++ {
			if lo.Less(e[k].lo) {
				out = append(out, span{lo, e[k].lo.Prev()})
			}
			if !e[k].hi.Less(x.hi) {
				lo = netip.Addr{}
				break
			}
			lo = e[k].hi.Next()
		}
		if lo.IsValid() {
			out = append(out, span{lo, x.hi})
		}
	}
	return out
}
