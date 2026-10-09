package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"detur.dev/server/internal/fraud"
)

// DayStat: one UTC day of analytics. Clicks = browser clicks (app + web kinds); Web = web fallbacks (subset of Clicks); Opens = SDK opens of an installed app.
type DayStat struct {
	Day        string `json:"day"`
	Clicks     int64  `json:"clicks"`
	Web        int64  `json:"webFallbacks"`
	Opens      int64  `json:"opens"`
	Organic    int64  `json:"organic"`
	NonOrganic int64  `json:"nonOrganic"`
}

// LinkStat: per-link browser clicks and matched installs.
type LinkStat struct {
	LinkID  string `json:"linkId"`
	Key     string `json:"key"`
	Clicks  int64  `json:"clicks"`
	Matches int64  `json:"matches"`
}

// EventStat: SDK event count by name.
type EventStat struct {
	Event string `json:"event"`
	Count int64  `json:"count"`
}

// SourceStat: browser clicks from one in-app source.
type SourceStat struct {
	Source string `json:"source"`
	Count  int64  `json:"count"`
}

// LinkConversion: SDK events from devices tagged with a link.
type LinkConversion struct {
	LinkID string `json:"linkId"`
	Key    string `json:"key"`
	Event  string `json:"event"`
	Count  int64  `json:"count"`
}

// LinkRetention: Devices = devices first tagged in range. Each mark covers the cohorts whose mark day falls in range.
type LinkRetention struct {
	LinkID  string `json:"linkId"`
	Key     string `json:"key"`
	Devices int64  `json:"devices"`
	D1      Mark   `json:"d1"`
	D7      Mark   `json:"d7"`
	D30     Mark   `json:"d30"`
}

// Mark: Returned of Devices came back on exactly that day.
type Mark struct {
	Returned int64 `json:"returned"`
	Devices  int64 `json:"devices"`
}

type Analytics struct {
	Days        []DayStat        `json:"days"`
	Links       []LinkStat       `json:"links"`
	Events      []EventStat      `json:"events"`
	Sources     []SourceStat     `json:"sources"`
	Conversions []LinkConversion `json:"conversions"`
	Retention   []LinkRetention  `json:"retention"`
}

// Analytics: app stats for the last `days` UTC days ending today, every day present (zeros filled). platform "" = all; events ignore platform (SDK events carry none).
func (s *Store) Analytics(appID string, days int, platform string, now time.Time) (Analytics, error) {
	from := now.UTC().AddDate(0, 0, -(days - 1))
	a := Analytics{Days: make([]DayStat, days), Links: []LinkStat{}, Events: []EventStat{}, Sources: []SourceStat{},
		Conversions: []LinkConversion{}, Retention: []LinkRetention{}}
	idx := map[string]int{}
	for i := range a.Days {
		d := day(from.AddDate(0, 0, i))
		a.Days[i].Day = d
		idx[d] = i
	}
	lo, hi := day(from), day(now)

	rows, err := s.db.Query(
		`SELECT day, kind, SUM(n) FROM click_days
		 WHERE app_id = ? AND day BETWEEN ? AND ? AND (? = '' OR platform = ?)
		 GROUP BY day, kind`, appID, lo, hi, platform, platform)
	if err != nil {
		return a, fmt.Errorf("analytics clicks: %w", err)
	}
	for rows.Next() {
		var d, kind string
		var n int64
		if err := rows.Scan(&d, &kind, &n); err != nil {
			rows.Close()
			return a, err
		}
		ds := &a.Days[idx[d]]
		switch kind {
		case KindOpen:
			ds.Opens += n
		case KindWeb:
			ds.Web += n
			ds.Clicks += n
		default:
			ds.Clicks += n
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return a, err
	}
	rows.Close()

	rows, err = s.db.Query(
		`SELECT substr(created_at, 1, 10) d, attribution, COUNT(*) FROM installs
		 WHERE app_id = ? AND d BETWEEN ? AND ? AND (? = '' OR platform = ?) AND attribution IN (?, ?)
		 GROUP BY d, attribution`, appID, lo, hi, platform, platform, AttributionOrganic, AttributionNonOrganic)
	if err != nil {
		return a, fmt.Errorf("analytics installs: %w", err)
	}
	for rows.Next() {
		var d, attr string
		var n int64
		if err := rows.Scan(&d, &attr, &n); err != nil {
			rows.Close()
			return a, err
		}
		if attr == AttributionOrganic {
			a.Days[idx[d]].Organic = n
		} else {
			a.Days[idx[d]].NonOrganic = n
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return a, err
	}
	rows.Close()

	rows, err = s.db.Query(
		`SELECT l.id, l.key, COALESCE(c.n, 0), COALESCE(i.n, 0) FROM links l
		 LEFT JOIN (SELECT link_id, SUM(n) n FROM click_days
		   WHERE app_id = ? AND day BETWEEN ? AND ? AND (? = '' OR platform = ?) AND kind != ?
		   GROUP BY link_id) c ON c.link_id = l.id
		 LEFT JOIN (SELECT link_id, COUNT(*) n FROM installs
		   WHERE app_id = ? AND substr(created_at, 1, 10) BETWEEN ? AND ? AND (? = '' OR platform = ?) AND attribution = ?
		   GROUP BY link_id) i ON i.link_id = l.id
		 WHERE l.app_id = ?
		 ORDER BY 3 DESC, l.key`,
		appID, lo, hi, platform, platform, KindOpen,
		appID, lo, hi, platform, platform, AttributionNonOrganic, appID)
	if err != nil {
		return a, fmt.Errorf("analytics links: %w", err)
	}
	for rows.Next() {
		var l LinkStat
		if err := rows.Scan(&l.LinkID, &l.Key, &l.Clicks, &l.Matches); err != nil {
			rows.Close()
			return a, err
		}
		a.Links = append(a.Links, l)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return a, err
	}
	rows.Close()

	if a.Events, err = s.analyticsEvents(appID, lo, hi); err != nil {
		return a, err
	}
	if a.Sources, err = s.analyticsSources(appID, lo, hi); err != nil {
		return a, err
	}
	if a.Conversions, err = s.analyticsConversions(appID, lo, hi); err != nil {
		return a, err
	}
	a.Retention, err = s.analyticsRetention(appID, lo, hi)
	return a, err
}

// analyticsConversions: top 20 (link, event) pairs in the day range; app-wide (no platform).
func (s *Store) analyticsConversions(appID, lo, hi string) ([]LinkConversion, error) {
	rows, err := s.db.Query(
		`SELECT e.link_id, l.key, e.event, SUM(e.n) FROM link_event_days e JOIN links l ON l.id = e.link_id
		 WHERE e.app_id = ? AND e.day BETWEEN ? AND ?
		 GROUP BY e.link_id, e.event ORDER BY 4 DESC, l.key, e.event LIMIT 20`, appID, lo, hi)
	if err != nil {
		return nil, fmt.Errorf("analytics conversions: %w", err)
	}
	defer rows.Close()
	out := []LinkConversion{}
	for rows.Next() {
		var c LinkConversion
		if err := rows.Scan(&c.LinkID, &c.Key, &c.Event, &c.Count); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// analyticsRetention: per link, cohorts started in range plus, per mark, cohorts whose cohort day + N is in range; app-wide (no platform). Busiest links first.
func (s *Store) analyticsRetention(appID, lo, hi string) ([]LinkRetention, error) {
	in := func(n int) string { // cohort day + n falls in [lo, hi]
		return fmt.Sprintf(`date(c.day, '+%d day') BETWEEN ?1 AND ?2`, n)
	}
	// cohort_devices, not devices: in HAVING a bare name resolves to the table column before the alias
	q := `SELECT c.link_id, l.key,
	  COALESCE(SUM(CASE WHEN c.day BETWEEN ?1 AND ?2 THEN c.devices END), 0) cohort_devices`
	for _, m := range []struct {
		col string
		n   int
	}{{"d1", 1}, {"d7", 7}, {"d30", 30}} {
		q += fmt.Sprintf(`,
	  COALESCE(SUM(CASE WHEN %[1]s THEN c.%[2]s END), 0), COALESCE(SUM(CASE WHEN %[1]s THEN c.devices END), 0)`, in(m.n), m.col)
	}
	q += `
	 FROM link_cohorts c JOIN links l ON l.id = c.link_id
	 WHERE c.app_id = ?3 AND c.day BETWEEN date(?1, '-30 day') AND ?2
	 GROUP BY c.link_id
	 HAVING cohort_devices + SUM(CASE WHEN ` + in(1) + ` OR ` + in(7) + ` OR ` + in(30) + ` THEN c.devices ELSE 0 END) > 0
	 ORDER BY cohort_devices DESC, l.key`
	rows, err := s.db.Query(q, lo, hi, appID)
	if err != nil {
		return nil, fmt.Errorf("analytics retention: %w", err)
	}
	defer rows.Close()
	out := []LinkRetention{}
	for rows.Next() {
		var r LinkRetention
		if err := rows.Scan(&r.LinkID, &r.Key, &r.Devices,
			&r.D1.Returned, &r.D1.Devices, &r.D7.Returned, &r.D7.Devices, &r.D30.Returned, &r.D30.Devices); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// analyticsEvents: top 10 SDK events in the day range.
func (s *Store) analyticsEvents(appID, lo, hi string) ([]EventStat, error) {
	rows, err := s.db.Query(
		`SELECT event, SUM(n) FROM event_days WHERE app_id = ? AND day BETWEEN ? AND ?
		 GROUP BY event ORDER BY 2 DESC, event LIMIT 10`, appID, lo, hi)
	if err != nil {
		return nil, fmt.Errorf("analytics events: %w", err)
	}
	defer rows.Close()
	events := []EventStat{}
	for rows.Next() {
		var e EventStat
		if err := rows.Scan(&e.Event, &e.Count); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

// analyticsSources: in-app source counts in the day range; app-wide (the rollup carries no platform).
func (s *Store) analyticsSources(appID, lo, hi string) ([]SourceStat, error) {
	rows, err := s.db.Query(
		`SELECT source, SUM(n) FROM click_sources WHERE app_id = ? AND day BETWEEN ? AND ?
		 GROUP BY source ORDER BY 2 DESC, source`, appID, lo, hi)
	if err != nil {
		return nil, fmt.Errorf("analytics sources: %w", err)
	}
	defer rows.Close()
	sources := []SourceStat{}
	for rows.Next() {
		var st SourceStat
		if err := rows.Scan(&st.Source, &st.Count); err != nil {
			return nil, err
		}
		sources = append(sources, st)
	}
	return sources, rows.Err()
}

// LinkTag: tag marker event name (not a conversion) and the destination param that hands the app its link key.
const LinkTag = "detur_link"

// deviceLinkDays: device_links lifetime; D30 is long done by then.
const deviceLinkDays = 90

// LinkEvent: one SDK analytics call as the per-link rollups see it. Device = raw SDK device_id (hashed before storage); LinkKey = data.link key, "" when absent; Retention = /api/analytics/retention call.
type LinkEvent struct {
	AppID     string
	Device    string
	LinkKey   string
	Event     string
	Retention bool
}

// RecordLinkEvent: tag the device with LinkKey's link (first tag wins), then count the call for the device's link: an event as a conversion, a retention call toward its cohort's D1/D7/D30 (once per device per mark). No device, unknown key or untagged device: nothing.
func (s *Store) RecordLinkEvent(e LinkEvent, now time.Time) error {
	if e.Device == "" {
		return nil
	}
	device, today := HashKey(e.Device), day(now)
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("record link event: %w", err)
	}
	defer tx.Rollback()
	if e.LinkKey != "" {
		var linkID string
		err := tx.QueryRow(`SELECT id FROM links WHERE app_id = ? AND key = ? COLLATE NOCASE`, e.AppID, e.LinkKey).Scan(&linkID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("record link event: %w", err)
		}
		if linkID != "" {
			res, err := tx.Exec(`INSERT INTO device_links (app_id, device, link_id, first_seen) VALUES (?, ?, ?, ?) ON CONFLICT DO NOTHING`,
				e.AppID, device, linkID, rfc3339(now))
			if err != nil {
				return fmt.Errorf("record link tag: %w", err)
			}
			if n, _ := res.RowsAffected(); n == 1 {
				if _, err := tx.Exec(`INSERT INTO link_cohorts (app_id, link_id, day, devices) VALUES (?, ?, ?, 1)
				 ON CONFLICT DO UPDATE SET devices = devices + 1`, e.AppID, linkID, today); err != nil {
					return fmt.Errorf("record link cohort: %w", err)
				}
			}
		}
	}
	var linkID, firstSeen string
	err = tx.QueryRow(`SELECT link_id, first_seen FROM device_links WHERE app_id = ? AND device = ?`, e.AppID, device).Scan(&linkID, &firstSeen)
	if errors.Is(err, sql.ErrNoRows) {
		return tx.Commit()
	}
	if err != nil {
		return fmt.Errorf("record link event: %w", err)
	}
	cohortDay := firstSeen[:10]
	switch {
	case !e.Retention && e.Event != LinkTag:
		if _, err := tx.Exec(`INSERT INTO link_event_days (app_id, link_id, event, day, n) VALUES (?, ?, ?, ?, 1)
		 ON CONFLICT DO UPDATE SET n = n + 1`, e.AppID, linkID, rollupEventName(e.Event), today); err != nil {
			return fmt.Errorf("record link conversion: %w", err)
		}
	case e.Retention:
		mark := retentionMark(cohortDay, today)
		if mark == "" {
			break
		}
		// mark is one of d1/d7/d30 (retentionMark), never input: safe to splice
		res, err := tx.Exec(`UPDATE device_links SET `+mark+` = 1 WHERE app_id = ? AND device = ? AND `+mark+` = 0`, e.AppID, device)
		if err != nil {
			return fmt.Errorf("record link retention: %w", err)
		}
		if n, _ := res.RowsAffected(); n == 1 {
			if _, err := tx.Exec(`UPDATE link_cohorts SET `+mark+` = `+mark+` + 1 WHERE app_id = ? AND link_id = ? AND day = ?`,
				e.AppID, linkID, cohortDay); err != nil {
				return fmt.Errorf("record link retention: %w", err)
			}
		}
	}
	return tx.Commit()
}

// retentionMark: column for a retention call `today` from a cohort started `cohortDay` (both YYYY-MM-DD), "" unless exactly 1, 7 or 30 UTC days later.
func retentionMark(cohortDay, today string) string {
	from, err1 := time.Parse(time.DateOnly, cohortDay)
	to, err2 := time.Parse(time.DateOnly, today)
	if err1 != nil || err2 != nil {
		return ""
	}
	switch int(to.Sub(from).Hours() / 24) {
	case 1:
		return "d1"
	case 7:
		return "d7"
	case 30:
		return "d30"
	}
	return ""
}

// MatchQuality: last `days` UTC days. Methods: installs per method ("" = pre-receipt).
// Buckets: probabilistic + scored organic per 50-pt score.
type MatchQuality struct {
	Methods map[string]int64 `json:"methods"`
	Buckets []ScoreBucket    `json:"buckets"`
}

type ScoreBucket struct {
	From    int   `json:"from"` // score in [From, From+ScoreBucketSize)
	Matched int64 `json:"matched"`
	Organic int64 `json:"organic"`
}

const ScoreBucketSize = 50

func (s *Store) MatchQuality(appID string, days int, now time.Time) (MatchQuality, error) {
	q := MatchQuality{Methods: map[string]int64{}, Buckets: []ScoreBucket{}}
	lo := day(now.UTC().AddDate(0, 0, -(days - 1)))
	rows, err := s.db.Query(
		`SELECT COALESCE(method, ''), COUNT(*) FROM installs
		 WHERE app_id = ? AND substr(created_at, 1, 10) >= ? AND attribution IN (?, ?)
		 GROUP BY 1`, appID, lo, AttributionOrganic, AttributionNonOrganic)
	if err != nil {
		return q, fmt.Errorf("match quality methods: %w", err)
	}
	for rows.Next() {
		var m string
		var n int64
		if err := rows.Scan(&m, &n); err != nil {
			rows.Close()
			return q, err
		}
		q.Methods[m] = n
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return q, err
	}
	rows.Close()

	rows, err = s.db.Query(
		`SELECT score / ? * ?, COALESCE(SUM(method = ?), 0), COALESCE(SUM(method = ?), 0) FROM installs
		 WHERE app_id = ? AND substr(created_at, 1, 10) >= ? AND score IS NOT NULL AND method IN (?, ?)
		 GROUP BY 1 ORDER BY 1`,
		ScoreBucketSize, ScoreBucketSize, MethodProbabilistic, MethodOrganic,
		appID, lo, MethodProbabilistic, MethodOrganic)
	if err != nil {
		return q, fmt.Errorf("match quality buckets: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var b ScoreBucket
		if err := rows.Scan(&b.From, &b.Matched, &b.Organic); err != nil {
			return q, err
		}
		q.Buckets = append(q.Buckets, b)
	}
	return q, rows.Err()
}

// FraudStats: last `days` UTC days. Signals: flagged installs per signal (every fraud.Signal* key present). Installs: latest flaggedLimit flagged installs, newest first. Settings' 7-day counts (R10) use FraudSignals(appID, 7, now).
type FraudStats struct {
	Signals  map[string]int64 `json:"signals"`
	Installs []FlaggedInstall `json:"installs"`
}

type FlaggedInstall struct {
	ID          string    `json:"id"`
	DeviceHash  string    `json:"deviceHash"`
	CreatedAt   time.Time `json:"createdAt"`
	Attribution string    `json:"attribution"`
	Method      string    `json:"method"`
	LinkID      string    `json:"linkId"`
	Platform    string    `json:"platform"`
	Fraud       []string  `json:"fraud"`
	FraudAction string    `json:"fraudAction"`
	FraudLinkID string    `json:"fraudLinkId"`
}

const flaggedLimit = 100

func (s *Store) Fraud(appID string, days int, now time.Time) (FraudStats, error) {
	f := FraudStats{Installs: []FlaggedInstall{}}
	var err error
	if f.Signals, err = s.FraudSignals(appID, days, now); err != nil {
		return f, err
	}
	lo := day(now.UTC().AddDate(0, 0, -(days - 1)))
	rows, err := s.db.Query(
		`SELECT id, device_hash, created_at, attribution, COALESCE(method, ''), COALESCE(link_id, ''), COALESCE(platform, ''),
		   fraud, COALESCE(fraud_action, ''), COALESCE(fraud_link_id, '')
		 FROM installs WHERE app_id = ? AND substr(created_at, 1, 10) >= ? AND fraud IS NOT NULL
		 ORDER BY created_at DESC LIMIT ?`, appID, lo, flaggedLimit)
	if err != nil {
		return f, fmt.Errorf("fraud installs: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var in FlaggedInstall
		var created, labels string
		if err := rows.Scan(&in.ID, &in.DeviceHash, &created, &in.Attribution, &in.Method, &in.LinkID, &in.Platform,
			&labels, &in.FraudAction, &in.FraudLinkID); err != nil {
			return f, err
		}
		in.CreatedAt = parseTime(created)
		in.Fraud = strings.Split(labels, ",")
		f.Installs = append(f.Installs, in)
	}
	return f, rows.Err()
}

// FraudSignals: per-signal flagged-install counts over the last `days` UTC days; every signal key present.
func (s *Store) FraudSignals(appID string, days int, now time.Time) (map[string]int64, error) {
	lo := day(now.UTC().AddDate(0, 0, -(days - 1)))
	sums := make([]string, len(fraud.Signals))
	args := make([]any, 0, len(fraud.Signals)+2)
	dest := make([]any, len(fraud.Signals))
	counts := make([]int64, len(fraud.Signals))
	for i, sig := range fraud.Signals {
		sums[i] = `COALESCE(SUM(instr(',' || fraud || ',', ?) > 0), 0)`
		args = append(args, ","+sig+",")
		dest[i] = &counts[i]
	}
	args = append(args, appID, lo)
	if err := s.db.QueryRow(
		`SELECT `+strings.Join(sums, ", ")+` FROM installs
		 WHERE app_id = ? AND substr(created_at, 1, 10) >= ?`, args...).Scan(dest...); err != nil {
		return nil, fmt.Errorf("fraud signals: %w", err)
	}
	out := make(map[string]int64, len(fraud.Signals))
	for i, sig := range fraud.Signals {
		out[sig] = counts[i]
	}
	return out, nil
}
