package config

import (
	"os"
	"strings"
)

// DomainSet returns the operator's configured domains (R12, R17): the
// primary DETUR_DOMAIN, any DETUR_EXTRA_DOMAINS (comma-separated), plus
// "localhost" for local development. Plain slice, order preserved, no
// duplicates.
func DomainSet(cfg *Config) []string {
	var domains []string
	seen := make(map[string]bool)
	add := func(d string) {
		d = strings.TrimSpace(d)
		if d == "" || seen[d] {
			return
		}
		seen[d] = true
		domains = append(domains, d)
	}
	add(cfg.Domain)
	for _, d := range strings.Split(os.Getenv("DETUR_EXTRA_DOMAINS"), ",") {
		add(d)
	}
	add("localhost")
	return domains
}
