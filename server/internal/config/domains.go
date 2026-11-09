package config

func DomainSet(cfg *Config) []string {
	if cfg.Domain == "" || cfg.Domain == "localhost" {
		return []string{"localhost"}
	}
	return []string{cfg.Domain, "localhost"}
}
