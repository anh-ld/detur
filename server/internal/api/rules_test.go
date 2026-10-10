package api

import (
	"strings"
	"testing"
	"time"

	"detur.dev/server/internal/store"
)

// One case per reject branch of validateLinkRules, plus valid controls. Each case states its full input.
func TestValidateLinkRules(t *testing.T) {
	direct := func(name string) store.LinkRule {
		return store.LinkRule{Name: name, Action: store.RuleAction{Targets: &store.RuleTargets{Destination: "myapp://x"}}}
	}
	split := func(name string, variants ...store.SplitVariant) store.LinkRule {
		return store.LinkRule{Name: name, Action: store.RuleAction{Split: variants}}
	}
	v := func(name string, weight int) store.SplitVariant { return store.SplitVariant{Name: name, Weight: weight} }
	from := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	until := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

	for _, c := range []struct {
		name  string
		rules []store.LinkRule
		want  string // "" = valid, else exact substring of the error
	}{
		{"empty list", nil, ""},
		{"direct + split", []store.LinkRule{direct("promo"), split("ab", v("A", 50), v("B", 50))}, ""},
		{"three-way split", []store.LinkRule{split("abc", v("A", 34), v("B", 33), v("C", 33))}, ""},

		{"blank rule name", []store.LinkRule{direct("  ")}, `rule #1: name is required`},
		{"bad platform", []store.LinkRule{{Name: "r", Cond: store.RuleCond{Platform: "tv"}, Action: direct("").Action}}, `platform must be 'ios', 'android', or 'desktop'`},
		{"bad lang", []store.LinkRule{{Name: "r", Cond: store.RuleCond{Lang: "german!"}, Action: direct("").Action}}, `invalid language code "german!"`},
		{"window reversed", []store.LinkRule{{Name: "r", Cond: store.RuleCond{From: &from, Until: &until}, Action: direct("").Action}}, `until must be after from`},
		{"no action", []store.LinkRule{{Name: "r"}}, `must specify direct targets or split variants`},
		{"unsafe direct target", []store.LinkRule{{Name: "r", Action: store.RuleAction{Targets: &store.RuleTargets{FallbackURL: "javascript:alert(1)"}}}}, `fallback_url scheme "javascript" not allowed`},
		{"one variant", []store.LinkRule{split("r", v("A", 100))}, `requires at least 2 variants`},
		{"blank variant name", []store.LinkRule{split("r", v("A", 50), v(" ", 50))}, `variant #2: name is required`},
		{"weight zero", []store.LinkRule{split("r", v("A", 100), v("B", 0))}, `weight must be between 1 and 100`},
		{"weights not 100", []store.LinkRule{split("r", v("A", 50), v("B", 40))}, `must sum to 100 (got 90)`},
		{"unsafe variant target", []store.LinkRule{split("r", v("A", 50), store.SplitVariant{Name: "B", Weight: 50, Targets: store.RuleTargets{IOS: "/relative"}})}, `ios must be an absolute URL`},

		// labels are the variant_days key: unique across the whole link
		{"dup variant in one rule", []store.LinkRule{split("r", v("A", 50), v("A", 50))}, `variant name "A" already used`},
		{"dup variant across rules", []store.LinkRule{split("r1", v("A", 50), v("B", 50)), split("r2", v("A", 50), v("C", 50))}, `variant name "A" already used`},
		{"direct name = variant name", []store.LinkRule{split("r1", v("A", 50), v("B", 50)), direct("B")}, `rule "B": name already used`},
		{"dup direct names", []store.LinkRule{direct("promo"), direct("promo")}, `rule "promo": name already used`},
	} {
		t.Run(c.name, func(t *testing.T) {
			err := validateLinkRules(c.rules)
			if c.want == "" {
				if err != nil {
					t.Fatalf("want valid, got %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("got %v, want error containing %q", err, c.want)
			}
		})
	}
}
