package store

import (
	"path/filepath"
	"testing"
	"time"
)

func TestLinkRulesCRUD(t *testing.T) {
	s := newTestStore(t)
	app, err := s.CreateApp("Test App", "secret-key")
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	link, err := s.CreateLink(Link{
		AppID: app.ID,
		Key:   "promo",
		URL:   "https://example.com/promo",
	})
	if err != nil {
		t.Fatalf("CreateLink: %v", err)
	}

	// Initial empty rules
	rules, err := s.GetLinkRules(link.ID)
	if err != nil {
		t.Fatalf("GetLinkRules: %v", err)
	}
	if len(rules) != 0 {
		t.Fatalf("expected 0 rules, got %d", len(rules))
	}

	now := time.Now().UTC().Truncate(time.Second)
	from := now.Add(-1 * time.Hour)
	until := now.Add(24 * time.Hour)

	// Set initial rules
	rulesToSet := []LinkRule{
		{
			Position: 1,
			Name:     "iOS German Campaign",
			Cond: RuleCond{
				Platform: "ios",
				Lang:     "de",
				ParamKey: "utm_source",
				ParamVal: "newsletter",
				From:     &from,
				Until:    &until,
			},
			Action: RuleAction{
				Targets: &RuleTargets{
					Destination: "myapp://promo/de",
					IOS:         "https://apps.apple.com/de/app/id123",
				},
			},
		},
		{
			Position: 2,
			Name:     "Landing Page Split Test",
			Cond: RuleCond{
				Platform: "desktop",
			},
			Action: RuleAction{
				Split: []SplitVariant{
					{
						Name:   "variant-a",
						Weight: 50,
						Targets: RuleTargets{
							FallbackURL: "https://example.com/landing-a",
						},
					},
					{
						Name:   "variant-b",
						Weight: 50,
						Targets: RuleTargets{
							FallbackURL: "https://example.com/landing-b",
						},
					},
				},
			},
		},
	}

	if err := s.SetLinkRules(link.ID, rulesToSet); err != nil {
		t.Fatalf("SetLinkRules: %v", err)
	}

	storedRules, err := s.GetLinkRules(link.ID)
	if err != nil {
		t.Fatalf("GetLinkRules: %v", err)
	}
	if len(storedRules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(storedRules))
	}
	if storedRules[0].Name != "iOS German Campaign" || storedRules[0].Position != 1 {
		t.Errorf("unexpected rule 0: %+v", storedRules[0])
	}
	if storedRules[0].ID == "" {
		t.Error("expected auto-generated ID on rule 0")
	}
	if storedRules[0].Cond.Platform != "ios" || storedRules[0].Cond.Lang != "de" {
		t.Errorf("unexpected rule 0 cond: %+v", storedRules[0].Cond)
	}
	if storedRules[1].Name != "Landing Page Split Test" || storedRules[1].Position != 2 {
		t.Errorf("unexpected rule 1: %+v", storedRules[1])
	}
	if len(storedRules[1].Action.Split) != 2 {
		t.Fatalf("expected 2 split variants, got %d", len(storedRules[1].Action.Split))
	}

	// Atomic update: replace with 1 rule
	newRules := []LinkRule{
		{
			Position: 1,
			Name:     "Android Only",
			Cond: RuleCond{
				Platform: "android",
			},
			Action: RuleAction{
				Targets: &RuleTargets{
					Android: "https://play.google.com/store/apps/details?id=com.example",
				},
			},
		},
	}
	if err := s.SetLinkRules(link.ID, newRules); err != nil {
		t.Fatalf("SetLinkRules update: %v", err)
	}
	storedRules, err = s.GetLinkRules(link.ID)
	if err != nil {
		t.Fatalf("GetLinkRules: %v", err)
	}
	if len(storedRules) != 1 || storedRules[0].Name != "Android Only" {
		t.Fatalf("expected 1 rule 'Android Only', got %+v", storedRules)
	}

	// DeleteLink cascades to link_rules
	if err := s.DeleteLink(link.ID); err != nil {
		t.Fatalf("DeleteLink: %v", err)
	}
	storedRules, err = s.GetLinkRules(link.ID)
	if err != nil {
		t.Fatalf("GetLinkRules after delete: %v", err)
	}
	if len(storedRules) != 0 {
		t.Fatalf("expected 0 rules after link deletion, got %d", len(storedRules))
	}
}

func TestVariantDaysRollups(t *testing.T) {
	s := newTestStore(t)
	app, err := s.CreateApp("Variant App", "key")
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	link, err := s.CreateLink(Link{
		AppID: app.ID,
		Key:   "vsplit",
		URL:   "https://example.com",
	})
	if err != nil {
		t.Fatalf("CreateLink: %v", err)
	}

	now := time.Now().UTC()

	// 1. Record clicks with variants
	c1, err := s.RecordClick(Click{
		AppID:   app.ID,
		LinkID:  link.ID,
		Variant: "variant-a",
		Fingerprint: Fingerprint{
			IP:        "192.0.2.1",
			UserAgent: "Mozilla/5.0 (iPhone; CPU iPhone OS 16_0 like Mac OS X)",
		},
		Destination: "myapp://promo-a",
	}, 24)
	if err != nil {
		t.Fatalf("RecordClick c1: %v", err)
	}
	if c1.Variant != "variant-a" {
		t.Errorf("expected variant-a, got %q", c1.Variant)
	}

	// Record second click for variant-a
	_, err = s.RecordClick(Click{
		AppID:   app.ID,
		LinkID:  link.ID,
		Variant: "variant-a",
		Fingerprint: Fingerprint{
			IP:        "192.0.2.2",
			UserAgent: "Mozilla/5.0 (iPhone; CPU iPhone OS 16_0 like Mac OS X)",
		},
		Destination: "myapp://promo-a",
	}, 24)
	if err != nil {
		t.Fatalf("RecordClick c2: %v", err)
	}

	// Record click for variant-b
	c3, err := s.RecordClick(Click{
		AppID:   app.ID,
		LinkID:  link.ID,
		Variant: "variant-b",
		Fingerprint: Fingerprint{
			IP:        "192.0.2.3",
			UserAgent: "Mozilla/5.0 (iPhone; CPU iPhone OS 16_0 like Mac OS X)",
		},
		Destination: "myapp://promo-b",
	}, 24)
	if err != nil {
		t.Fatalf("RecordClick c3: %v", err)
	}

	// Record click with UASuspect should NOT increment variant_days
	_, err = s.RecordClick(Click{
		AppID:     app.ID,
		LinkID:    link.ID,
		Variant:   "variant-b",
		UASuspect: true,
		Fingerprint: Fingerprint{
			IP:        "192.0.2.4",
			UserAgent: "BadBot/1.0",
		},
		Destination: "myapp://promo-b",
	}, 24)
	if err != nil {
		t.Fatalf("RecordClick suspect: %v", err)
	}

	// Check variant stats: variant-a has 2 clicks, variant-b has 1 click
	stats, err := s.GetLinkVariantStats(app.ID, link.ID, now.AddDate(0, 0, -1), now.AddDate(0, 0, 1))
	if err != nil {
		t.Fatalf("GetLinkVariantStats: %v", err)
	}
	if len(stats) != 2 {
		t.Fatalf("expected 2 variant stats, got %d", len(stats))
	}
	if stats[0].Variant != "variant-a" || stats[0].Clicks != 2 || stats[0].Installs != 0 {
		t.Errorf("unexpected stats[0]: %+v", stats[0])
	}
	if stats[1].Variant != "variant-b" || stats[1].Clicks != 1 || stats[1].Installs != 0 {
		t.Errorf("unexpected stats[1]: %+v", stats[1])
	}

	// 2. Record install for variant-b
	inst1, err := s.RecordInstall(Install{
		AppID:       app.ID,
		DeviceHash:  "dev-b1",
		ClickID:     c3.ID,
		Attribution: AttributionNonOrganic,
		LinkID:      link.ID,
		Variant:     "variant-b",
	})
	if err != nil {
		t.Fatalf("RecordInstall: %v", err)
	}
	if inst1.Variant != "variant-b" {
		t.Errorf("expected variant-b on install, got %q", inst1.Variant)
	}

	// Organic install with variant should NOT increment variant_days
	_, err = s.RecordInstall(Install{
		AppID:       app.ID,
		DeviceHash:  "dev-org",
		Attribution: AttributionOrganic,
		Variant:     "variant-b",
	})
	if err != nil {
		t.Fatalf("RecordInstall organic: %v", err)
	}

	// Idempotent duplicate install should NOT double increment
	_, err = s.RecordInstall(Install{
		AppID:       app.ID,
		DeviceHash:  "dev-b1",
		ClickID:     c3.ID,
		Attribution: AttributionNonOrganic,
		LinkID:      link.ID,
		Variant:     "variant-b",
	})
	if err != nil {
		t.Fatalf("RecordInstall duplicate: %v", err)
	}

	// Re-check variant stats
	stats, err = s.GetLinkVariantStats(app.ID, link.ID, now.AddDate(0, 0, -1), now.AddDate(0, 0, 1))
	if err != nil {
		t.Fatalf("GetLinkVariantStats: %v", err)
	}
	// variant-a: 2 clicks, 0 installs (0% conv)
	// variant-b: 1 click, 1 install (100% conv)
	var statA, statB *VariantStat
	for i := range stats {
		if stats[i].Variant == "variant-a" {
			statA = &stats[i]
		} else if stats[i].Variant == "variant-b" {
			statB = &stats[i]
		}
	}
	if statA == nil || statA.Clicks != 2 || statA.Installs != 0 || statA.ConversionRate != 0.0 {
		t.Errorf("unexpected statA: %+v", statA)
	}
	if statB == nil || statB.Clicks != 1 || statB.Installs != 1 || statB.ConversionRate != 1.0 {
		t.Errorf("unexpected statB: %+v", statB)
	}

	// Check DeleteApp removes variant_days
	if err := s.DeleteApp(app.ID); err != nil {
		t.Fatalf("DeleteApp: %v", err)
	}
	stats, err = s.GetLinkVariantStats(app.ID, link.ID, now.AddDate(0, 0, -1), now.AddDate(0, 0, 1))
	if err != nil {
		t.Fatalf("GetLinkVariantStats after DeleteApp: %v", err)
	}
	if len(stats) != 0 {
		t.Errorf("expected 0 stats after DeleteApp, got %d", len(stats))
	}
}

func TestAddMissingColumnsVariant(t *testing.T) {
	// Create database without variant columns initially to test migration
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "legacy.db")

	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	if !columnExists(s.db, "clicks", "variant") {
		t.Error("expected variant column in clicks table")
	}
	if !columnExists(s.db, "installs", "variant") {
		t.Error("expected variant column in installs table")
	}
}

// Position is array order; ids survive only for rules the link already owns, so foreign or repeated ids never collide.
func TestSetLinkRulesIDsAndPositions(t *testing.T) {
	s := newTestStore(t)
	app, err := s.CreateApp("App", "key")
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	l1, err := s.CreateLink(Link{AppID: app.ID, Key: "l1", URL: "https://example.com/1"})
	if err != nil {
		t.Fatalf("CreateLink l1: %v", err)
	}
	l2, err := s.CreateLink(Link{AppID: app.ID, Key: "l2", URL: "https://example.com/2"})
	if err != nil {
		t.Fatalf("CreateLink l2: %v", err)
	}
	act := RuleAction{Targets: &RuleTargets{Destination: "myapp://x"}}
	get := func(linkID string) []LinkRule {
		t.Helper()
		rules, err := s.GetLinkRules(linkID)
		if err != nil {
			t.Fatalf("GetLinkRules: %v", err)
		}
		return rules
	}

	if err := s.SetLinkRules(l1.ID, []LinkRule{{Name: "a", Action: act}, {Name: "b", Action: act}}); err != nil {
		t.Fatalf("SetLinkRules l1: %v", err)
	}
	orig := get(l1.ID)
	if len(orig) != 2 || orig[0].Name != "a" || orig[1].Name != "b" {
		t.Fatalf("initial rules: %+v", orig)
	}

	// 1. link 2 sends link 1's rule id: saved under a fresh id, link 1 untouched
	if err := s.SetLinkRules(l2.ID, []LinkRule{{ID: orig[0].ID, Name: "c", Action: act}}); err != nil {
		t.Fatalf("SetLinkRules with foreign id: %v", err)
	}
	if got := get(l2.ID); len(got) != 1 || got[0].ID == orig[0].ID {
		t.Fatalf("link 2: want 1 rule with a fresh id, got %+v", got)
	}
	if got := get(l1.ID); len(got) != 2 || got[0].ID != orig[0].ID || got[1].ID != orig[1].ID {
		t.Fatalf("link 1 changed: %+v", got)
	}

	// 2. reorder with bogus client positions and a repeated id
	if err := s.SetLinkRules(l1.ID, []LinkRule{
		{ID: orig[1].ID, Position: 9, Name: "b", Action: act},
		{ID: orig[1].ID, Position: 1, Name: "b2", Action: act},
	}); err != nil {
		t.Fatalf("SetLinkRules reorder: %v", err)
	}
	got := get(l1.ID)
	if len(got) != 2 {
		t.Fatalf("want 2 rules, got %+v", got)
	}
	// array order wins over client position
	if got[0].Name != "b" || got[0].Position != 1 || got[1].Name != "b2" || got[1].Position != 2 {
		t.Errorf("order: got %q@%d, %q@%d", got[0].Name, got[0].Position, got[1].Name, got[1].Position)
	}
	// first use of an owned id keeps id + created_at
	if got[0].ID != orig[1].ID || !got[0].CreatedAt.Equal(orig[1].CreatedAt) {
		t.Errorf("kept rule: id %s created %v, want %s %v", got[0].ID, got[0].CreatedAt, orig[1].ID, orig[1].CreatedAt)
	}
	// repeat of the same id gets a fresh one
	if got[1].ID == orig[1].ID {
		t.Errorf("repeated id reused: %s", got[1].ID)
	}
}

// Dedup refresh: the variant changes only together with the destination; an in-app reopen keeps both.
func TestRefreshClickVariantFollowsDestination(t *testing.T) {
	s := newTestStore(t)
	app, err := s.CreateApp("App", "key")
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	link, err := s.CreateLink(Link{AppID: app.ID, Key: "v", URL: "https://example.com"})
	if err != nil {
		t.Fatalf("CreateLink: %v", err)
	}
	const safari = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 Version/17.0 Mobile/15E148 Safari/604.1"
	click := func(ip, uagent, source, variant, dest string) Click {
		t.Helper()
		c, err := s.RecordClick(Click{AppID: app.ID, LinkID: link.ID, Variant: variant, Destination: dest, Source: source,
			Fingerprint: Fingerprint{IP: ip, UserAgent: uagent}}, 24)
		if err != nil {
			t.Fatalf("RecordClick: %v", err)
		}
		return c
	}
	clicksByVariant := func() map[string]int64 {
		t.Helper()
		stats, err := s.GetLinkVariantStats(app.ID, link.ID, time.Now().AddDate(0, 0, -1), time.Now().AddDate(0, 0, 1))
		if err != nil {
			t.Fatalf("GetLinkVariantStats: %v", err)
		}
		m := map[string]int64{}
		for _, v := range stats {
			m[v.Variant] = v.Clicks
		}
		return m
	}
	stored := func(id string) Click {
		t.Helper()
		c, err := s.GetClick(id)
		if err != nil {
			t.Fatalf("GetClick: %v", err)
		}
		return c
	}

	// 1. same visitor, rule edited mid-window: label follows the new destination, B counts the new exposure
	a := click("192.0.2.10", safari, "", "A", "myapp://a")
	b := click("192.0.2.10", safari, "", "B", "myapp://b")
	if b.ID != a.ID {
		t.Fatalf("want dedup onto %s, got new click %s", a.ID, b.ID)
	}
	if c := stored(a.ID); c.Variant != "B" || c.Destination != "myapp://b" {
		t.Errorf("after refresh: variant %q destination %q, want B myapp://b", c.Variant, c.Destination)
	}
	if m := clicksByVariant(); m["A"] != 1 || m["B"] != 1 {
		t.Errorf("after refresh: clicks %v, want A=1 B=1", m)
	}

	// 2. in-app click, then "Open in browser" (no source, other UA, same IP + platform): original label and destination kept
	inApp := click("192.0.2.20", safari+" Instagram 300.0", "instagram", "A", "myapp://a")
	reopen := click("192.0.2.20", safari, "", "B", "myapp://b")
	if reopen.ID != inApp.ID {
		t.Fatalf("want reopen onto %s, got new click %s", inApp.ID, reopen.ID)
	}
	if c := stored(inApp.ID); c.Variant != "A" || c.Destination != "myapp://a" {
		t.Errorf("after reopen: variant %q destination %q, want A myapp://a", c.Variant, c.Destination)
	}
	if m := clicksByVariant(); m["A"] != 2 || m["B"] != 1 {
		t.Errorf("after reopen: clicks %v, want A=2 B=1", m)
	}
}
