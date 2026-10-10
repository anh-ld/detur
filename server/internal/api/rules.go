package api

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"detur.dev/server/internal/httpx"
	"detur.dev/server/internal/store"
)

var langRegex = regexp.MustCompile(`^[A-Za-z]{2,3}([-_][A-Za-z0-9]{2,4})?$`)

func (p *portalServer) getLinkForApp(w http.ResponseWriter, appID, linkID string) (store.Link, bool) {
	link, err := p.st.GetLink(linkID)
	if err != nil {
		p.storeErr(w, err)
		return store.Link{}, false
	}
	if link.AppID != appID {
		http.Error(w, "not found", http.StatusNotFound)
		return store.Link{}, false
	}
	return link, true
}

func (p *portalServer) getLinkRules(w http.ResponseWriter, r *http.Request) {
	appID := r.PathValue("app_id")
	linkID := r.PathValue("link_id")
	if _, ok := p.getLinkForApp(w, appID, linkID); !ok {
		return
	}
	rules, err := p.st.GetLinkRules(linkID)
	if err != nil {
		p.internal(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, rules)
}

func (p *portalServer) setLinkRules(w http.ResponseWriter, r *http.Request) {
	appID := r.PathValue("app_id")
	linkID := r.PathValue("link_id")
	if _, ok := p.getLinkForApp(w, appID, linkID); !ok {
		return
	}
	var rules []store.LinkRule
	if err := decodeJSON(w, r, &rules); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if rules == nil {
		rules = []store.LinkRule{}
	}
	if err := validateLinkRules(rules); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := p.st.SetLinkRules(linkID, rules); err != nil {
		p.internal(w, err)
		return
	}
	updated, err := p.st.GetLinkRules(linkID)
	if err != nil {
		p.internal(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, updated)
}

func (p *portalServer) getLinkVariants(w http.ResponseWriter, r *http.Request) {
	appID := r.PathValue("app_id")
	linkID := r.PathValue("link_id")
	if _, ok := p.getLinkForApp(w, appID, linkID); !ok {
		return
	}
	from, to, ok := parseDateRange(w, r, 7)
	if !ok {
		return
	}
	stats, err := p.st.GetLinkVariantStats(appID, linkID, from, to)
	if err != nil {
		p.internal(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, stats)
}

func validRuleTargets(t store.RuleTargets) error {
	for _, f := range []struct{ name, v string }{
		{"destination", t.Destination},
		{"ios", t.IOS},
		{"android", t.Android},
		{"fallback_url", t.FallbackURL},
	} {
		if err := validTarget(f.name, f.v); err != nil {
			return err
		}
	}
	return nil
}

// validateLinkRules: variant labels (direct rule names + split variant names) are unique per link, since variant_days keys on link + label.
func validateLinkRules(rules []store.LinkRule) error {
	labels := map[string]bool{}
	for i, r := range rules {
		name := strings.TrimSpace(r.Name)
		if name == "" {
			return fmt.Errorf("rule #%d: name is required", i+1)
		}

		// Validate conditions
		if r.Cond.Platform != "" {
			plat := strings.ToLower(strings.TrimSpace(r.Cond.Platform))
			if plat != "ios" && plat != "android" && plat != "desktop" {
				return fmt.Errorf("rule %q: platform must be 'ios', 'android', or 'desktop'", name)
			}
		}
		if r.Cond.Lang != "" {
			if !langRegex.MatchString(strings.TrimSpace(r.Cond.Lang)) {
				return fmt.Errorf("rule %q: invalid language code %q", name, r.Cond.Lang)
			}
		}
		if r.Cond.From != nil && r.Cond.Until != nil {
			if r.Cond.Until.Before(*r.Cond.From) {
				return fmt.Errorf("rule %q: active window until must be after from", name)
			}
		}

		// Validate action
		hasTargets := r.Action.Targets != nil
		hasSplit := len(r.Action.Split) > 0
		if !hasTargets && !hasSplit {
			return fmt.Errorf("rule %q: must specify direct targets or split variants", name)
		}

		if hasTargets && !hasSplit {
			if labels[name] {
				return fmt.Errorf("rule %q: name already used by another rule or variant on this link", name)
			}
			labels[name] = true
		}
		if hasTargets {
			if err := validRuleTargets(*r.Action.Targets); err != nil {
				return fmt.Errorf("rule %q: %w", name, err)
			}
		}

		if hasSplit {
			if len(r.Action.Split) < 2 {
				return fmt.Errorf("rule %q: A/B split requires at least 2 variants", name)
			}
			weightSum := 0
			for j, v := range r.Action.Split {
				vName := strings.TrimSpace(v.Name)
				if vName == "" {
					return fmt.Errorf("rule %q variant #%d: name is required", name, j+1)
				}
				if labels[vName] {
					return fmt.Errorf("rule %q: variant name %q already used by another rule or variant on this link", name, vName)
				}
				labels[vName] = true

				if v.Weight < 1 || v.Weight > 100 {
					return fmt.Errorf("rule %q variant %q: weight must be between 1 and 100", name, vName)
				}
				weightSum += v.Weight

				if err := validRuleTargets(v.Targets); err != nil {
					return fmt.Errorf("rule %q variant %q: %w", name, vName, err)
				}
			}
			if weightSum != 100 {
				return fmt.Errorf("rule %q: split variant weights must sum to 100 (got %d)", name, weightSum)
			}
		}
	}
	return nil
}
