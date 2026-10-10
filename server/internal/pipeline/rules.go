package pipeline

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"detur.dev/server/internal/httpx"
	"detur.dev/server/internal/store"
)

// ResolvedRoute contains the evaluated targets and variant for a short link click.
type ResolvedRoute struct {
	Variant     string
	Destination string
	IOS         string
	Android     string
	FallbackURL string
}

// EvaluateRules evaluates ordered link rules against the incoming request and returns the resolved route.
func EvaluateRules(rules []store.LinkRule, baseLink store.Link, req *http.Request, now time.Time) ResolvedRoute {
	resolved := ResolvedRoute{
		Variant:     "",
		Destination: baseLink.URL,
		IOS:         baseLink.IOS,
		Android:     baseLink.Android,
		FallbackURL: baseLink.FallbackURL,
	}

	var q url.Values
	if req != nil && req.URL != nil {
		q = req.URL.Query()
	}

	for _, rule := range rules {
		if matchRuleCond(rule.Cond, req, q, now) {
			applyRuleAction(&resolved, rule, baseLink, req)
			return resolved
		}
	}

	return resolved
}

// matchRuleCond checks all criteria in RuleCond using AND logic. Empty criteria are ignored.
func matchRuleCond(cond store.RuleCond, req *http.Request, q url.Values, now time.Time) bool {
	agent := ""
	if req != nil {
		agent = req.UserAgent()
	}

	// 1. Platform condition: "ios", "android", "desktop"
	if cond.Platform != "" {
		if clickPlatform(agent) != strings.ToLower(strings.TrimSpace(cond.Platform)) {
			return false
		}
	}

	// 2. Language condition: prefix of Accept-Language primary tag (e.g. "de")
	if cond.Lang != "" {
		acceptLang := ""
		if req != nil {
			acceptLang = req.Header.Get("Accept-Language")
		}
		if !matchLang(acceptLang, cond.Lang) {
			return false
		}
	}

	// 3. Query parameter condition: ParamKey presence and optional ParamVal match
	if cond.ParamKey != "" {
		if q == nil || !q.Has(cond.ParamKey) {
			return false
		}
		if cond.ParamVal != "" && q.Get(cond.ParamKey) != cond.ParamVal {
			return false
		}
	}

	// 4. Date window condition: UTC From / Until
	if cond.From != nil && now.Before(*cond.From) {
		return false
	}
	if cond.Until != nil && now.After(*cond.Until) {
		return false
	}

	return true
}

// primaryLang extracts the first language tag from an Accept-Language header.
func primaryLang(al string) string {
	primary, _, _ := strings.Cut(al, ",")
	primary, _, _ = strings.Cut(primary, ";")
	return strings.TrimSpace(primary)
}

// matchLang checks if the user's primary preferred language tag starts with ruleLang (case-insensitive).
func matchLang(acceptLang, ruleLang string) bool {
	if ruleLang == "" {
		return true
	}
	ruleLang = strings.ToLower(strings.TrimSpace(ruleLang))
	primary := strings.ToLower(primaryLang(acceptLang))
	if primary == "" {
		return false
	}
	return primary == ruleLang || strings.HasPrefix(primary, ruleLang+"-") || strings.HasPrefix(primary, ruleLang+"_")
}

// applyTargets copies non-empty target overrides to the resolved route.
func applyTargets(resolved *ResolvedRoute, targets store.RuleTargets) {
	if targets.Destination != "" {
		resolved.Destination = targets.Destination
	}
	if targets.IOS != "" {
		resolved.IOS = targets.IOS
	}
	if targets.Android != "" {
		resolved.Android = targets.Android
	}
	if targets.FallbackURL != "" {
		resolved.FallbackURL = targets.FallbackURL
	}
}

// applyRuleAction updates the resolved route based on direct targets or sticky A/B split. Variant = split variant name, or the rule name for a direct match (R8).
func applyRuleAction(resolved *ResolvedRoute, rule store.LinkRule, baseLink store.Link, req *http.Request) {
	action := rule.Action
	if len(action.Split) > 0 {
		ip := ""
		agent := ""
		if req != nil {
			ip = httpx.RemoteIP(req)
			agent = req.UserAgent()
		}
		bucket := HashBucket(baseLink.ID, ip, agent)

		var selected store.SplitVariant
		acc := 0
		for _, v := range action.Split {
			acc += v.Weight
			if bucket < acc {
				selected = v
				break
			}
		}
		if selected.Name == "" {
			selected = action.Split[len(action.Split)-1]
		}

		resolved.Variant = selected.Name
		applyTargets(resolved, selected.Targets)
		return
	}

	if action.Targets != nil {
		resolved.Variant = rule.Name
		applyTargets(resolved, *action.Targets)
	}
}

const (
	fnvOffset64 = 14695981039346656037
	fnvPrime64  = 1099511628211
)

// HashBucket computes a deterministic integer in [0, 99] using 64-bit FNV-1a without heap allocations.
func HashBucket(linkID, ip, userAgent string) int {
	var h uint64 = fnvOffset64
	for _, s := range []string{linkID, "|", ip, "|", userAgent} {
		for i := 0; i < len(s); i++ {
			h ^= uint64(s[i])
			h *= fnvPrime64
		}
	}
	return int(h % 100)
}
