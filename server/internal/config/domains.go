package config

// DomainSet returns the operator's configured domains (R12, R17): the
// primary DOMAIN, any EXTRA_DOMAINS, plus "localhost" for local
// development. Plain slice, order preserved, no duplicates.
func DomainSet(cfg *Config) []string {
	var domains []string
	seen := make(map[string]bool)
	add := func(d string) {
		if d == "" || seen[d] {
			return
		}
		seen[d] = true
		domains = append(domains, d)
	}
	add(cfg.Domain)
	for _, d := range cfg.ExtraDomains {
		add(d)
	}
	add("localhost")
	return domains
}
