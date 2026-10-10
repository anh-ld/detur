package pipeline

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"detur.dev/server/internal/store"
)

// Rule matching with platform and language (ios + de) routes to German iOS store, whereas Android falls through.
func TestRulesPlatformAndLangMatch(t *testing.T) {
	baseLink := store.Link{
		ID:          "link-1",
		Key:         "promo",
		URL:         "https://example.com/default",
		IOS:         "https://apps.apple.com/us/app/id111",
		Android:     "https://play.google.com/store/apps/details?id=com.default",
		FallbackURL: "https://example.com/fallback",
	}

	rules := []store.LinkRule{
		{
			Position: 1,
			Name:     "German iOS",
			Cond: store.RuleCond{
				Platform: "ios",
				Lang:     "de",
			},
			Action: store.RuleAction{
				Targets: &store.RuleTargets{
					IOS:         "https://apps.apple.com/de/app/id222",
					Destination: "myapp://promo/de",
				},
			},
		},
	}

	now := time.Now().UTC()

	// 1. iOS visitor with Accept-Language "de-DE,de;q=0.9" -> matches
	reqIOSDe, _ := http.NewRequest("GET", "https://detur.dev/promo", nil)
	reqIOSDe.Header.Set("User-Agent", "Mozilla/5.0 (iPhone; CPU iPhone OS 16_5 like Mac OS X)")
	reqIOSDe.Header.Set("Accept-Language", "de-DE,de;q=0.9,en;q=0.8")

	route := EvaluateRules(rules, baseLink, reqIOSDe, now)
	if route.IOS != "https://apps.apple.com/de/app/id222" {
		t.Errorf("expected German iOS store, got %q", route.IOS)
	}
	if route.Destination != "myapp://promo/de" {
		t.Errorf("expected German destination, got %q", route.Destination)
	}
	// Inherited Android target remains default
	if route.Android != baseLink.Android {
		t.Errorf("expected Android target %q, got %q", baseLink.Android, route.Android)
	}

	// 2. Android visitor with Accept-Language "de" -> does NOT match platform, falls through
	reqAndroidDe, _ := http.NewRequest("GET", "https://detur.dev/promo", nil)
	reqAndroidDe.Header.Set("User-Agent", "Mozilla/5.0 (Linux; Android 13; Pixel 7)")
	reqAndroidDe.Header.Set("Accept-Language", "de-DE,de;q=0.9")

	route = EvaluateRules(rules, baseLink, reqAndroidDe, now)
	if route.IOS != baseLink.IOS {
		t.Errorf("expected default iOS, got %q", route.IOS)
	}
	if route.Destination != baseLink.URL {
		t.Errorf("expected default destination, got %q", route.Destination)
	}

	// 3. iOS visitor with Accept-Language "en-US" -> does NOT match lang, falls through
	reqIOSEn, _ := http.NewRequest("GET", "https://detur.dev/promo", nil)
	reqIOSEn.Header.Set("User-Agent", "Mozilla/5.0 (iPhone; CPU iPhone OS 16_5 like Mac OS X)")
	reqIOSEn.Header.Set("Accept-Language", "en-US,en;q=0.9")

	route = EvaluateRules(rules, baseLink, reqIOSEn, now)
	if route.IOS != baseLink.IOS {
		t.Errorf("expected default iOS, got %q", route.IOS)
	}
}

// 50/50 split across 10,000 synthetic visitor IPs/UAs produces a split between 45% and 55% with 100% stickiness on identical IP+UA.
func TestRulesABSplitDistributionAndStickiness(t *testing.T) {
	baseLink := store.Link{
		ID:  "ab-link",
		URL: "https://example.com",
	}

	rules := []store.LinkRule{
		{
			Position: 1,
			Name:     "50/50 Split",
			Cond:     store.RuleCond{}, // Matches everything
			Action: store.RuleAction{
				Split: []store.SplitVariant{
					{
						Name:   "variant-a",
						Weight: 50,
						Targets: store.RuleTargets{
							Destination: "myapp://promo/a",
						},
					},
					{
						Name:   "variant-b",
						Weight: 50,
						Targets: store.RuleTargets{
							Destination: "myapp://promo/b",
						},
					},
				},
			},
		},
	}

	now := time.Now().UTC()
	counts := map[string]int{"variant-a": 0, "variant-b": 0}
	total := 10000

	// Test 10,000 synthetic visitors
	for i := 0; i < total; i++ {
		ip := fmt.Sprintf("198.51.100.%d", i%250)
		ua := fmt.Sprintf("Mozilla/5.0 (VisitorDevice %d)", i)

		req, _ := http.NewRequest("GET", "https://detur.dev/promo", nil)
		req.RemoteAddr = ip + ":1234"
		req.Header.Set("User-Agent", ua)

		route1 := EvaluateRules(rules, baseLink, req, now)
		counts[route1.Variant]++

		// Verify 100% stickiness: same IP + UA yields identical variant every time
		route2 := EvaluateRules(rules, baseLink, req, now)
		if route1.Variant != route2.Variant {
			t.Fatalf("stickiness failure for visitor %d: got %s then %s", i, route1.Variant, route2.Variant)
		}
	}

	pctA := float64(counts["variant-a"]) / float64(total) * 100.0
	pctB := float64(counts["variant-b"]) / float64(total) * 100.0

	t.Logf("Split result: variant-a=%.2f%%, variant-b=%.2f%%", pctA, pctB)

	// Acceptable range for 50/50 split across 10k random items is well within [45%, 55%]
	if pctA < 45.0 || pctA > 55.0 {
		t.Errorf("variant-a percentage %.2f%% outside expected [45%%, 55%%]", pctA)
	}
	if pctB < 45.0 || pctB > 55.0 {
		t.Errorf("variant-b percentage %.2f%% outside expected [45%%, 55%%]", pctB)
	}
}

// In-app deep link destination override + Partial rule target inherits base link.
func TestRulesTargetInheritanceAndDestinationOverride(t *testing.T) {
	baseLink := store.Link{
		ID:          "link-inherit",
		URL:         "https://example.com/default-dest",
		IOS:         "https://apps.apple.com/app/base-ios",
		Android:     "https://play.google.com/store/apps/details?id=base.android",
		FallbackURL: "https://example.com/base-fallback",
	}

	rules := []store.LinkRule{
		{
			Position: 1,
			Name:     "Desktop Fallback Only",
			Cond: store.RuleCond{
				Platform: "desktop",
			},
			Action: store.RuleAction{
				Targets: &store.RuleTargets{
					FallbackURL: "https://example.com/new-desktop-landing",
					Destination: "myapp://overridden-destination",
				},
			},
		},
	}

	reqDesktop, _ := http.NewRequest("GET", "https://detur.dev/desktop", nil)
	reqDesktop.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)")

	route := EvaluateRules(rules, baseLink, reqDesktop, time.Now().UTC())

	// FallbackURL and Destination were overridden
	if route.FallbackURL != "https://example.com/new-desktop-landing" {
		t.Errorf("expected overridden fallback, got %q", route.FallbackURL)
	}
	if route.Destination != "myapp://overridden-destination" {
		t.Errorf("expected overridden destination, got %q", route.Destination)
	}

	// IOS and Android were not in the rule, so they inherit from baseLink
	if route.IOS != baseLink.IOS {
		t.Errorf("expected inherited IOS %q, got %q", baseLink.IOS, route.IOS)
	}
	if route.Android != baseLink.Android {
		t.Errorf("expected inherited Android %q, got %q", baseLink.Android, route.Android)
	}
}

func TestRulesQueryParamAndDateWindow(t *testing.T) {
	baseLink := store.Link{
		ID:  "date-param-link",
		URL: "https://example.com/base",
	}

	t0 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	from := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	until := time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC)

	rules := []store.LinkRule{
		{
			Position: 1,
			Name:     "Promo Campaign Window",
			Cond: store.RuleCond{
				ParamKey: "campaign",
				ParamVal: "summer",
				From:     &from,
				Until:    &until,
			},
			Action: store.RuleAction{
				Targets: &store.RuleTargets{
					Destination: "myapp://summer-sale",
				},
			},
		},
		{
			Position: 2,
			Name:     "Any Ref Key Present",
			Cond: store.RuleCond{
				ParamKey: "ref",
			},
			Action: store.RuleAction{
				Targets: &store.RuleTargets{
					Destination: "myapp://ref-partner",
				},
			},
		},
	}

	// 1. Before 'from' date -> does not match rule 1
	req1, _ := http.NewRequest("GET", "https://detur.dev/p?campaign=summer", nil)
	r1 := EvaluateRules(rules, baseLink, req1, t0)
	if r1.Destination != baseLink.URL {
		t.Errorf("expected base destination before start date, got %q", r1.Destination)
	}

	// 2. During active window -> matches rule 1
	midWindow := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	r2 := EvaluateRules(rules, baseLink, req1, midWindow)
	if r2.Destination != "myapp://summer-sale" {
		t.Errorf("expected summer sale destination, got %q", r2.Destination)
	}

	// 3. Campaign param is different value -> does not match rule 1
	reqDiffVal, _ := http.NewRequest("GET", "https://detur.dev/p?campaign=winter", nil)
	r3 := EvaluateRules(rules, baseLink, reqDiffVal, midWindow)
	if r3.Destination != baseLink.URL {
		t.Errorf("expected base destination for different campaign param, got %q", r3.Destination)
	}

	// 4. Matches rule 2 via presence of ?ref (any value)
	reqRef, _ := http.NewRequest("GET", "https://detur.dev/p?ref=influencer123", nil)
	r4 := EvaluateRules(rules, baseLink, reqRef, t0)
	if r4.Destination != "myapp://ref-partner" {
		t.Errorf("expected ref partner destination, got %q", r4.Destination)
	}
}

// End-to-end integration test via Register HTTP handler
func TestPipelineShortLinkWithRulesE2E(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "pipeline_rules_test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer st.Close()

	app, err := st.CreateApp("E2E App", "app-secret")
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}

	link, err := st.CreateLink(store.Link{
		AppID:       app.ID,
		Key:         "e2e-promo",
		URL:         "https://example.com/base",
		IOS:         "https://apps.apple.com/us/app/id111",
		Android:     "https://play.google.com/store/apps/details?id=com.base",
		FallbackURL: "https://example.com/base-fallback",
	})
	if err != nil {
		t.Fatalf("CreateLink: %v", err)
	}

	// Configure split test rule on the link
	err = st.SetLinkRules(link.ID, []store.LinkRule{
		{
			Position: 1,
			Name:     "Landing Variant Split",
			Cond:     store.RuleCond{Platform: "desktop"},
			Action: store.RuleAction{
				Split: []store.SplitVariant{
					{
						Name:   "v-landing-a",
						Weight: 100, // 100% to variant a for deterministic assertion
						Targets: store.RuleTargets{
							FallbackURL: "https://example.com/landing-a",
							Destination: "myapp://variant-a",
						},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("SetLinkRules: %v", err)
	}

	mux := http.NewServeMux()
	Register(mux, st, 24)

	req, _ := http.NewRequest("GET", "/e2e-promo", nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)")
	req.RemoteAddr = "203.0.113.50:54321"

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	// Desktop redirect is 302 directly to FallbackURL
	if w.Code != http.StatusFound {
		t.Fatalf("expected status 302, got %d", w.Code)
	}
	loc := w.Header().Get("Location")
	if loc != "https://example.com/landing-a" {
		t.Fatalf("expected redirect to https://example.com/landing-a, got %q", loc)
	}

	// Verify click recorded with Variant = "v-landing-a" and Destination = "myapp://variant-a"
	clicks, err := st.ClicksSince(app.ID, time.Now().Add(-1*time.Hour))
	if err != nil {
		t.Fatalf("ClicksSince: %v", err)
	}
	if len(clicks) != 1 {
		t.Fatalf("expected 1 recorded click, got %d", len(clicks))
	}
	if clicks[0].Variant != "v-landing-a" {
		t.Errorf("expected click variant 'v-landing-a', got %q", clicks[0].Variant)
	}
	if clicks[0].Destination != "myapp://variant-a" {
		t.Errorf("expected click destination 'myapp://variant-a', got %q", clicks[0].Destination)
	}

	// Verify variant_days rollup
	stats, err := st.GetLinkVariantStats(app.ID, link.ID, time.Now().AddDate(0, 0, -1), time.Now().AddDate(0, 0, 1))
	if err != nil {
		t.Fatalf("GetLinkVariantStats: %v", err)
	}
	if len(stats) != 1 || stats[0].Variant != "v-landing-a" || stats[0].Clicks != 1 {
		t.Fatalf("unexpected variant stats: %+v", stats)
	}
}

// HashBucket is pinned: changing it re-buckets every live visitor mid-experiment.
func TestHashBucketPinned(t *testing.T) {
	for _, c := range []struct {
		ip   string
		want int
	}{{"1.2.3.4", 20}, {"10.0.0.1", 16}, {"192.168.1.9", 79}, {"8.8.8.8", 32}} {
		if got := HashBucket("link-1", c.ip, "ua-x"); got != c.want {
			t.Errorf("HashBucket(link-1, %s, ua-x) = %d, want %d", c.ip, got, c.want)
		}
	}
}

// First matching rule wins; a 70/30 split puts bucket 69 in A and bucket 70 in B; direct matches record the rule name.
func TestRulesFirstMatchAndSplitBoundary(t *testing.T) {
	base := store.Link{ID: "link-1", URL: "https://example.com/base"}
	direct := store.LinkRule{Name: "campaign", Cond: store.RuleCond{ParamKey: "campaign"},
		Action: store.RuleAction{Targets: &store.RuleTargets{Destination: "myapp://campaign"}}}
	split := store.LinkRule{Name: "ref split", Cond: store.RuleCond{ParamKey: "ref"},
		Action: store.RuleAction{Split: []store.SplitVariant{
			{Name: "A", Weight: 70, Targets: store.RuleTargets{Destination: "myapp://a"}},
			{Name: "B", Weight: 30, Targets: store.RuleTargets{Destination: "myapp://b"}},
		}}}
	req := func(q, ip string) *http.Request {
		r := httptest.NewRequest("GET", "https://detur.dev/promo?"+q, nil)
		r.Header.Set("User-Agent", "ua-x")
		r.RemoteAddr = ip + ":1234"
		return r
	}
	now := time.Now()

	// both rules match: order decides
	if got := EvaluateRules([]store.LinkRule{direct, split}, base, req("campaign=1&ref=1", "10.1.0.175"), now); got.Variant != "campaign" || got.Destination != "myapp://campaign" {
		t.Errorf("direct first: got %+v", got)
	}
	if got := EvaluateRules([]store.LinkRule{split, direct}, base, req("campaign=1&ref=1", "10.1.0.175"), now); got.Variant != "A" {
		t.Errorf("split first: got %+v", got)
	}
	// boundary: bucket 69 -> A, bucket 70 -> B
	if got := EvaluateRules([]store.LinkRule{split}, base, req("ref=1", "10.1.0.175"), now); got.Variant != "A" || got.Destination != "myapp://a" {
		t.Errorf("bucket 69: got %+v", got)
	}
	if got := EvaluateRules([]store.LinkRule{split}, base, req("ref=1", "10.1.0.80"), now); got.Variant != "B" || got.Destination != "myapp://b" {
		t.Errorf("bucket 70: got %+v", got)
	}
	// no match: base targets, no variant
	if got := EvaluateRules([]store.LinkRule{direct, split}, base, req("", "10.1.0.80"), now); got.Variant != "" || got.Destination != base.URL {
		t.Errorf("no match: got %+v", got)
	}
}
