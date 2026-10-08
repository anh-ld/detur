package fraud

import (
	_ "embed"
	"strings"
	"sync"
)

//go:embed data/bots.txt
var botsTxt string

// bots: lowercased PostHog bot substrings, parsed once on first use.
var bots = sync.OnceValue(func() []string {
	return strings.Split(strings.TrimRight(strings.ToLower(botsTxt), "\n"), "\n") // patterns may hold spaces
})

// SuspectUA: empty/whitespace UA, or case-insensitive substring hit from the bundled bot list. Separate from ua.IsBot (redirect-time filter, unchanged).
func SuspectUA(ua string) bool {
	l := strings.ToLower(strings.TrimSpace(ua))
	if l == "" {
		return true
	}
	for _, b := range bots() {
		if strings.Contains(l, b) {
			return true
		}
	}
	return false
}
