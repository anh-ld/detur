package fraud

import (
	_ "embed"
	"strings"
	"sync"
)

//go:embed data/bots.txt
var botsTxt string

var bots = sync.OnceValue(func() []string {
	return strings.Split(strings.TrimRight(strings.ToLower(botsTxt), "\n"), "\n")
})

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
