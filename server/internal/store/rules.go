package store

import (
	"encoding/json"
	"fmt"
	"time"
)

// LinkRule: ordered routing rule for a short link.
type LinkRule struct {
	ID        string     `json:"id"`
	LinkID    string     `json:"link_id"`
	Position  int        `json:"position"`
	Name      string     `json:"name"`
	Cond      RuleCond   `json:"cond"`
	Action    RuleAction `json:"action"`
	CreatedAt time.Time  `json:"created_at"`
}

// RuleCond: criteria required for this rule to match (AND logic).
type RuleCond struct {
	Platform string     `json:"platform,omitempty"` // "ios", "android", "desktop"
	Lang     string     `json:"lang,omitempty"`     // 2-letter ISO prefix (e.g. "de")
	ParamKey string     `json:"param_key,omitempty"` // query parameter key
	ParamVal string     `json:"param_val,omitempty"` // optional query parameter exact value
	From     *time.Time `json:"from,omitempty"`      // active window start UTC
	Until    *time.Time `json:"until,omitempty"`     // active window end UTC
}

// RuleAction: direct targets or weighted A/B split.
type RuleAction struct {
	Targets *RuleTargets   `json:"targets,omitempty"`
	Split   []SplitVariant `json:"split,omitempty"`
}

// SplitVariant: one weighted variant in an A/B split.
type SplitVariant struct {
	Name    string      `json:"name"`
	Weight  int         `json:"weight"` // 1-100; sum must equal 100
	Targets RuleTargets `json:"targets"`
}

// RuleTargets: destination overrides; empty strings inherit base link values.
type RuleTargets struct {
	Destination string `json:"destination,omitempty"`
	IOS         string `json:"ios,omitempty"`
	Android     string `json:"android,omitempty"`
	FallbackURL string `json:"fallback_url,omitempty"`
}

// VariantStat: aggregated clicks, matched installs, and conversion rate per variant.
type VariantStat struct {
	Variant        string  `json:"variant"`
	Clicks         int64   `json:"clicks"`
	Installs       int64   `json:"installs"`
	ConversionRate float64 `json:"conversion_rate"`
}

// GetLinkRules: ordered rules for a link (lowest position first).
func (s *Store) GetLinkRules(linkID string) ([]LinkRule, error) {
	rows, err := s.db.Query(
		`SELECT id, link_id, position, name, cond, action, created_at
		 FROM link_rules
		 WHERE link_id = ?
		 ORDER BY position ASC, created_at ASC`, linkID,
	)
	if err != nil {
		return nil, fmt.Errorf("get link rules: %w", err)
	}
	defer rows.Close()

	var rules []LinkRule
	for rows.Next() {
		var (
			r         LinkRule
			condStr   string
			actionStr string
			createdAt string
		)
		if err := rows.Scan(&r.ID, &r.LinkID, &r.Position, &r.Name, &condStr, &actionStr, &createdAt); err != nil {
			return nil, fmt.Errorf("scan link rule: %w", err)
		}
		if err := json.Unmarshal([]byte(condStr), &r.Cond); err != nil {
			return nil, fmt.Errorf("unmarshal rule cond: %w", err)
		}
		if err := json.Unmarshal([]byte(actionStr), &r.Action); err != nil {
			return nil, fmt.Errorf("unmarshal rule action: %w", err)
		}
		r.CreatedAt = parseTime(createdAt)
		rules = append(rules, r)
	}
	if rules == nil {
		rules = []LinkRule{}
	}
	return rules, rows.Err()
}

// SetLinkRules: atomic replacement of all rules for a link.
func (s *Store) SetLinkRules(linkID string, rules []LinkRule) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin set link rules: %w", err)
	}
	defer tx.Rollback()

	// ids and created_at survive only for rules this link already owns; position is array order (one source of truth)
	created := map[string]string{}
	rows, err := tx.Query(`SELECT id, created_at FROM link_rules WHERE link_id = ?`, linkID)
	if err != nil {
		return fmt.Errorf("read old link rules: %w", err)
	}
	for rows.Next() {
		var id, at string
		if err := rows.Scan(&id, &at); err != nil {
			rows.Close()
			return fmt.Errorf("scan old link rule: %w", err)
		}
		created[id] = at
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read old link rules: %w", err)
	}

	if _, err := tx.Exec(`DELETE FROM link_rules WHERE link_id = ?`, linkID); err != nil {
		return fmt.Errorf("delete old link rules: %w", err)
	}

	now := rfc3339(time.Now().UTC())
	for i, r := range rules {
		id, createdAt := r.ID, now
		if at, ok := created[id]; ok {
			createdAt = at
			delete(created, id) // a repeated id gets a fresh one
		} else {
			id = Nanoid(16)
		}
		condBytes, err := json.Marshal(r.Cond)
		if err != nil {
			return fmt.Errorf("marshal rule cond: %w", err)
		}
		actionBytes, err := json.Marshal(r.Action)
		if err != nil {
			return fmt.Errorf("marshal rule action: %w", err)
		}
		if _, err := tx.Exec(
			`INSERT INTO link_rules (id, link_id, position, name, cond, action, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			id, linkID, i+1, r.Name, string(condBytes), string(actionBytes), createdAt,
		); err != nil {
			return fmt.Errorf("insert link rule: %w", err)
		}
	}
	return tx.Commit()
}

// GetLinkVariantStats: clicks and installs aggregated per variant for [from, to] UTC range.
func (s *Store) GetLinkVariantStats(appID, linkID string, from, to time.Time) ([]VariantStat, error) {
	from = from.UTC()
	to = to.UTC()
	lo, hi := day(from), day(to)
	rows, err := s.db.Query(
		`SELECT variant, SUM(clicks), SUM(installs)
		 FROM variant_days
		 WHERE app_id = ? AND link_id = ? AND day BETWEEN ? AND ?
		 GROUP BY variant
		 ORDER BY SUM(clicks) DESC`,
		appID, linkID, lo, hi,
	)
	if err != nil {
		return nil, fmt.Errorf("query variant stats: %w", err)
	}
	defer rows.Close()

	var stats []VariantStat
	for rows.Next() {
		var v VariantStat
		if err := rows.Scan(&v.Variant, &v.Clicks, &v.Installs); err != nil {
			return nil, fmt.Errorf("scan variant stat: %w", err)
		}
		if v.Clicks > 0 {
			v.ConversionRate = float64(v.Installs) / float64(v.Clicks)
		}
		stats = append(stats, v)
	}
	if stats == nil {
		stats = []VariantStat{}
	}
	return stats, rows.Err()
}
