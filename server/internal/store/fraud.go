package store

import (
	"database/sql"
	"errors"
	"fmt"
	"net/netip"
	"time"

	"detur.dev/server/internal/fraud"
)

// Fraud actions (installs.fraud_action); "" = attribution untouched (KTD7).
const (
	FraudActionReattributed = "reattributed" // best click excluded, next valid candidate credited
	FraudActionExcluded     = "excluded"     // best click excluded, install went organic
)

// FraudSettings: app's fraud config; no row = fraud.Defaults.
func (s *Store) FraudSettings(appID string) (fraud.Settings, error) {
	return fraudSettings(s.db, appID)
}

func fraudSettings(q interface {
	QueryRow(string, ...any) *sql.Row
}, appID string) (fraud.Settings, error) {
	f := fraud.Defaults
	err := q.QueryRow(
		`SELECT velocity_mode, timing_mode, user_agent_mode, ip_mode, velocity_ip_max, velocity_link_max, velocity_window_minutes,
		   timing_short_seconds, timing_long_hours, fingerprint_max, fingerprint_window_days
		 FROM fraud_settings WHERE app_id = ?`, appID,
	).Scan(&f.VelocityMode, &f.TimingMode, &f.UserAgentMode, &f.IPMode, &f.VelocityIPMax, &f.VelocityLinkMax, &f.VelocityWindowMinutes,
		&f.TimingShortSeconds, &f.TimingLongHours, &f.FingerprintMax, &f.FingerprintWindowDays)
	if errors.Is(err, sql.ErrNoRows) {
		return fraud.Defaults, nil
	}
	if err != nil {
		return fraud.Defaults, fmt.Errorf("fraud settings: %w", err)
	}
	return f, nil
}

// UpdateFraudSettings: upsert app's fraud config; ErrNotFound if app unknown. Range checks live in fraud.Settings.Validate.
func (s *Store) UpdateFraudSettings(appID string, f fraud.Settings) error {
	res, err := s.db.Exec(
		`INSERT INTO fraud_settings (app_id, velocity_mode, timing_mode, user_agent_mode, ip_mode, velocity_ip_max, velocity_link_max,
		   velocity_window_minutes, timing_short_seconds, timing_long_hours, fingerprint_max, fingerprint_window_days)
		 SELECT id, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ? FROM apps WHERE id = ?
		 ON CONFLICT(app_id) DO UPDATE SET velocity_mode = excluded.velocity_mode, timing_mode = excluded.timing_mode,
		   user_agent_mode = excluded.user_agent_mode, ip_mode = excluded.ip_mode, velocity_ip_max = excluded.velocity_ip_max,
		   velocity_link_max = excluded.velocity_link_max, velocity_window_minutes = excluded.velocity_window_minutes,
		   timing_short_seconds = excluded.timing_short_seconds, timing_long_hours = excluded.timing_long_hours,
		   fingerprint_max = excluded.fingerprint_max, fingerprint_window_days = excluded.fingerprint_window_days`,
		f.VelocityMode, f.TimingMode, f.UserAgentMode, f.IPMode, f.VelocityIPMax, f.VelocityLinkMax, f.VelocityWindowMinutes,
		f.TimingShortSeconds, f.TimingLongHours, f.FingerprintMax, f.FingerprintWindowDays, appID)
	if err != nil {
		return fmt.Errorf("update fraud settings: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// bucketLayout: click_hits minute bucket, fixed width so text compares in order.
const bucketLayout = "2006-01-02T15:04"

// bumpHits: +1 to the click's IP and link minute buckets, return each key's sum over the app's velocity window. Settings read in the tx so callers stay unchanged. IP skipped (0) when empty/private/loopback (KTD8).
func bumpHits(tx *sql.Tx, c Click, now time.Time) (hitsIP, hitsLink int, err error) {
	set, err := fraudSettings(tx, c.AppID)
	if err != nil {
		set = fraud.Defaults // settings trouble never drops the click (fail-open, same as match)
	}
	since := now.Add(-time.Duration(set.VelocityWindowMinutes) * time.Minute).Format(bucketLayout)
	bump := func(keyType, key string) (int, error) {
		if _, err := tx.Exec(
			`INSERT INTO click_hits (app_id, key_type, key, bucket, n) VALUES (?, ?, ?, ?, 1)
			 ON CONFLICT DO UPDATE SET n = n + 1`, c.AppID, keyType, key, now.Format(bucketLayout)); err != nil {
			return 0, err
		}
		var n int
		err := tx.QueryRow(`SELECT COALESCE(SUM(n), 0) FROM click_hits WHERE app_id = ? AND key_type = ? AND key = ? AND bucket > ?`,
			c.AppID, keyType, key, since).Scan(&n)
		return n, err
	}
	if k := ipKey(c.Fingerprint.IP); k != "" {
		if hitsIP, err = bump("ip", k); err != nil {
			return 0, 0, err
		}
	}
	hitsLink, err = bump("link", c.LinkID)
	return hitsIP, hitsLink, err
}

// ipKey: hashed velocity key, IPv6 collapsed to its /64; "" = don't count (not fraud.Routable, KTD8).
func ipKey(ip string) string {
	a, ok := fraud.Routable(ip)
	if !ok {
		return ""
	}
	if a.Is6() {
		return HashKey(netip.PrefixFrom(a, 64).Masked().String())
	}
	return HashKey(a.String())
}

// CountFingerprintInstalls: probabilistic installs sharing deviceHash on linkID created >= since (R5; device configurations, not devices).
func (s *Store) CountFingerprintInstalls(appID, deviceHash, linkID string, since time.Time) (int, error) {
	var n int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM installs WHERE app_id = ? AND device_hash = ? AND link_id = ? AND method = ? AND created_at >= ?`,
		appID, deviceHash, linkID, MethodProbabilistic, rfc3339(since)).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count fingerprint installs: %w", err)
	}
	return n, nil
}
