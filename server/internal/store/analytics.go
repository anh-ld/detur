package store

import (
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

type Analytics struct {
	Days    []DayStat    `json:"days"`
	Links   []LinkStat   `json:"links"`
	Events  []EventStat  `json:"events"`
	Sources []SourceStat `json:"sources"`
}

// Analytics: app stats for the last `days` UTC days ending today, every day present (zeros filled). platform "" = all; events ignore platform (SDK events carry none).
func (s *Store) Analytics(appID string, days int, platform string, now time.Time) (Analytics, error) {
	from := now.UTC().AddDate(0, 0, -(days - 1))
	a := Analytics{Days: make([]DayStat, days), Links: []LinkStat{}, Events: []EventStat{}, Sources: []SourceStat{}}
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
	a.Sources, err = s.analyticsSources(appID, lo, hi)
	return a, err
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
