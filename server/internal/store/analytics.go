package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"detur.dev/server/internal/fraud"
)

type DayStat struct {
	Day        string `json:"day"`
	Clicks     int64  `json:"clicks"`
	Web        int64  `json:"webFallbacks"`
	Opens      int64  `json:"opens"`
	Organic    int64  `json:"organic"`
	NonOrganic int64  `json:"nonOrganic"`
}

type LinkStat struct {
	LinkID  string `json:"linkId"`
	Key     string `json:"key"`
	Title   string `json:"title"`
	Clicks  int64  `json:"clicks"`
	Matches int64  `json:"matches"`
}

type EventStat struct {
	Event string `json:"event"`
	Count int64  `json:"count"`
}

type SourceStat struct {
	Source string `json:"source"`
	Count  int64  `json:"count"`
}

type LinkConversion struct {
	LinkID string `json:"linkId"`
	Key    string `json:"key"`
	Event  string `json:"event"`
	Count  int64  `json:"count"`
}

type LinkRetention struct {
	LinkID  string `json:"linkId"`
	Key     string `json:"key"`
	Devices int64  `json:"devices"`
	D1      Mark   `json:"d1"`
	D7      Mark   `json:"d7"`
	D30     Mark   `json:"d30"`
}

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

func newAnalytics(n int) Analytics {
	return Analytics{Days: make([]DayStat, n), Links: []LinkStat{}, Events: []EventStat{}, Sources: []SourceStat{},
		Conversions: []LinkConversion{}, Retention: []LinkRetention{}}
}

func (s *Store) AnalyticsRange(appID string, from, to time.Time, platform, targetLink string) (Analytics, error) {
	from = from.UTC()
	to = to.UTC()
	fromDay := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, time.UTC)
	toDay := time.Date(to.Year(), to.Month(), to.Day(), 0, 0, 0, 0, time.UTC)
	days := int(toDay.Sub(fromDay).Hours()/24) + 1
	if days < 1 {
		days = 1
	}
	a := newAnalytics(days)
	idx := map[string]int{}
	for i := range a.Days {
		d := day(fromDay.AddDate(0, 0, i))
		a.Days[i].Day = d
		idx[d] = i
	}
	lo, hi := day(fromDay), day(toDay)

	cw, ca := optEq([]any{appID, lo, hi}, "link_id", targetLink, "platform", platform)
	rows, err := s.db.Query(
		`SELECT day, kind, SUM(n) FROM click_days
		 WHERE app_id = ? AND day BETWEEN ? AND ?`+cw+` GROUP BY day, kind`, ca...)
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
		addKind(&a.Days[idx[d]], kind, n)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return a, err
	}
	rows.Close()

	iw, ia := optEq([]any{appID, lo, hi}, "link_id", targetLink, "platform", platform)
	rows, err = s.db.Query(
		`SELECT substr(created_at, 1, 10) d, attribution, COUNT(*) FROM installs
		 WHERE app_id = ? AND created_at BETWEEN ? AND ? || '~'`+iw+` AND attribution IN (?, ?)
		 GROUP BY d, attribution`, append(ia, AttributionOrganic, AttributionNonOrganic)...)
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
			if targetLink == "" {
				a.Days[idx[d]].Organic = n
			}
		} else {
			a.Days[idx[d]].NonOrganic = n
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return a, err
	}
	rows.Close()

	rows, err = s.linkStats(appID, true, lo, hi, platform, targetLink)
	if err != nil {
		return a, fmt.Errorf("analytics links: %w", err)
	}
	for rows.Next() {
		var l LinkStat
		if err := rows.Scan(&l.LinkID, &l.Key, &l.Title, &l.Clicks, &l.Matches); err != nil {
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

	if a.Events, err = s.analyticsEvents(appID, lo, hi, targetLink); err != nil {
		return a, err
	}
	if a.Sources, err = s.analyticsSources(appID, lo, hi, targetLink); err != nil {
		return a, err
	}
	if a.Conversions, err = s.analyticsConversions(appID, lo, hi, targetLink); err != nil {
		return a, err
	}
	a.Retention, err = s.analyticsRetention(appID, lo, hi, targetLink)
	return a, err
}

func (s *Store) AnalyticsSubDay(appID, window, platform, linkID string, now time.Time) (Analytics, error) {
	now = now.UTC()
	var dur, bucketDur time.Duration
	var count int
	switch window {
	case "15m":
		dur = 15 * time.Minute
		bucketDur = 1 * time.Minute
		count = 15
	case "1h":
		dur = 1 * time.Hour
		bucketDur = 5 * time.Minute
		count = 12
	case "3h":
		dur = 3 * time.Hour
		bucketDur = 10 * time.Minute
		count = 18
	case "12h":
		dur = 12 * time.Hour
		bucketDur = 30 * time.Minute
		count = 24
	default:
		return Analytics{}, fmt.Errorf("invalid sub-day window: %q", window)
	}

	end := now.Truncate(bucketDur).Add(bucketDur)
	from := end.Add(-dur)

	a := newAnalytics(count)
	for i := range a.Days {
		bucketStart := from.Add(time.Duration(i) * bucketDur)
		a.Days[i].Day = rfc3339(bucketStart)
	}

	fromStr := rfc3339(from)
	toStr := rfc3339(now)
	fromUnix := from.Unix()
	bucketSec := int64(bucketDur.Seconds())

	cw, ca := optEq([]any{fromUnix, bucketSec, appID, fromStr, toStr}, "link_id", linkID, "platform", platform)
	cRows, err := s.db.Query(
		`SELECT (CAST(strftime('%s', created_at) AS INTEGER) - ?) / ? AS b_idx, COALESCE(kind, ''), COUNT(*)
		 FROM clicks
		 WHERE app_id = ? AND created_at >= ? AND created_at <= ? AND is_bot = 0 AND ua_suspect = 0`+cw+`
		 GROUP BY b_idx, kind`, ca...)
	if err != nil {
		return a, fmt.Errorf("sub-day clicks: %w", err)
	}
	for cRows.Next() {
		var bIdx int
		var kind string
		var n int64
		if err := cRows.Scan(&bIdx, &kind, &n); err != nil {
			cRows.Close()
			return a, err
		}
		if bIdx >= 0 && bIdx < count {
			addKind(&a.Days[bIdx], kind, n)
		}
	}
	cRows.Close()
	if err := cRows.Err(); err != nil {
		return a, err
	}

	iw, ia := optEq([]any{fromUnix, bucketSec, appID, fromStr, toStr}, "link_id", linkID, "platform", platform)
	iRows, err := s.db.Query(
		`SELECT (CAST(strftime('%s', created_at) AS INTEGER) - ?) / ? AS b_idx, attribution, COUNT(*)
		 FROM installs
		 WHERE app_id = ? AND created_at >= ? AND created_at <= ?`+iw+` AND attribution IN (?, ?)
		 GROUP BY b_idx, attribution`, append(ia, AttributionOrganic, AttributionNonOrganic)...)
	if err != nil {
		return a, fmt.Errorf("sub-day installs: %w", err)
	}
	for iRows.Next() {
		var bIdx int
		var attr string
		var n int64
		if err := iRows.Scan(&bIdx, &attr, &n); err != nil {
			iRows.Close()
			return a, err
		}
		if bIdx >= 0 && bIdx < count {
			if attr == AttributionOrganic {
				if linkID == "" {
					a.Days[bIdx].Organic += n
				}
			} else {
				a.Days[bIdx].NonOrganic += n
			}
		}
	}
	iRows.Close()
	if err := iRows.Err(); err != nil {
		return a, err
	}

	lRows, err := s.linkStats(appID, false, fromStr, toStr, platform, linkID)
	if err != nil {
		return a, fmt.Errorf("sub-day links: %w", err)
	}
	for lRows.Next() {
		var l LinkStat
		if err := lRows.Scan(&l.LinkID, &l.Key, &l.Title, &l.Clicks, &l.Matches); err != nil {
			lRows.Close()
			return a, err
		}
		a.Links = append(a.Links, l)
	}
	lRows.Close()
	if err := lRows.Err(); err != nil {
		return a, err
	}

	sw, sa := optEq([]any{appID, fromStr, toStr}, "link_id", linkID, "platform", platform)
	sRows, err := s.db.Query(
		`SELECT source, COUNT(*) FROM clicks
		 WHERE app_id = ? AND created_at >= ? AND created_at <= ? AND is_bot = 0 AND ua_suspect = 0
		   AND source IS NOT NULL AND source != ''`+sw+`
		 GROUP BY source
		 ORDER BY 2 DESC, source`, sa...)
	if err != nil {
		return a, fmt.Errorf("sub-day sources: %w", err)
	}
	for sRows.Next() {
		var src string
		var cnt int64
		if err := sRows.Scan(&src, &cnt); err != nil {
			sRows.Close()
			return a, err
		}
		a.Sources = append(a.Sources, SourceStat{Source: src, Count: cnt})
	}
	sRows.Close()
	if err := sRows.Err(); err != nil {
		return a, err
	}

	if linkID != "" {
		return a, nil
	}
	eRows, err := s.db.Query(
		`SELECT event, COUNT(*) FROM events
		 WHERE app_id = ? AND created_at >= ? AND created_at <= ?
		 GROUP BY event ORDER BY 2 DESC, event LIMIT 10`,
		appID, fromStr, toStr)
	if err != nil {
		return a, fmt.Errorf("sub-day events: %w", err)
	}
	for eRows.Next() {
		var ev string
		var cnt int64
		if err := eRows.Scan(&ev, &cnt); err != nil {
			eRows.Close()
			return a, err
		}
		a.Events = append(a.Events, EventStat{Event: ev, Count: cnt})
	}
	eRows.Close()
	return a, eRows.Err()
}

// optEq appends " AND col = ?" + value for each non-empty (col, val) pair, so sqlite can seek indexes (no "? = ” OR" guards).
func optEq(args []any, pairs ...string) (string, []any) {
	w := ""
	for i := 0; i+1 < len(pairs); i += 2 {
		if pairs[i+1] != "" {
			w += " AND " + pairs[i] + " = ?"
			args = append(args, pairs[i+1])
		}
	}
	return w, args
}

func addKind(ds *DayStat, kind string, n int64) {
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

func (s *Store) linkStats(appID string, daily bool, lo, hi, platform, linkID string) (*sql.Rows, error) {
	cq := `SELECT link_id, COUNT(*) n FROM clicks WHERE app_id = ? AND created_at >= ? AND created_at <= ? AND is_bot = 0 AND ua_suspect = 0 AND COALESCE(kind, '') != ?`
	iq := `SELECT link_id, COUNT(*) n FROM installs WHERE app_id = ? AND created_at >= ? AND created_at <= ? AND attribution = ? AND link_id IS NOT NULL AND link_id != ''`
	if daily {
		cq = `SELECT link_id, SUM(n) n FROM click_days WHERE app_id = ? AND day BETWEEN ? AND ? AND kind != ?`
		iq = `SELECT link_id, COUNT(*) n FROM installs WHERE app_id = ? AND created_at BETWEEN ? AND ? || '~' AND attribution = ?`
	}
	cw, ca := optEq([]any{appID, lo, hi, KindOpen}, "link_id", linkID, "platform", platform)
	iw, ia := optEq([]any{appID, lo, hi, AttributionNonOrganic}, "link_id", linkID, "platform", platform)
	ow, oa := optEq([]any{appID}, "l.id", linkID)
	args := append(append(ca, ia...), oa...)
	return s.db.Query(`SELECT l.id, l.key, COALESCE(l.title, ''), COALESCE(c.n, 0), COALESCE(i.n, 0) FROM links l
		 LEFT JOIN (`+cq+cw+` GROUP BY link_id) c ON c.link_id = l.id
		 LEFT JOIN (`+iq+iw+` GROUP BY link_id) i ON i.link_id = l.id
		 WHERE l.app_id = ?`+ow+` ORDER BY 4 DESC, l.key`, args...)
}

func (s *Store) analyticsConversions(appID, lo, hi, targetLink string) ([]LinkConversion, error) {
	rows, err := s.db.Query(
		`SELECT e.link_id, l.key, e.event, SUM(e.n) FROM link_event_days e JOIN links l ON l.id = e.link_id
		 WHERE e.app_id = ? AND (? = '' OR e.link_id = ?) AND e.day BETWEEN ? AND ?
		 GROUP BY e.link_id, e.event ORDER BY 4 DESC, l.key, e.event LIMIT 20`, appID, targetLink, targetLink, lo, hi)
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

func (s *Store) analyticsRetention(appID, lo, hi, targetLink string) ([]LinkRetention, error) {
	in := func(n int) string {
		return fmt.Sprintf(`date(c.day, '+%d day') BETWEEN ?1 AND ?2`, n)
	}
	// cohort_devices, not devices: in HAVING a bare name hits the table column before the alias
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
	 WHERE c.app_id = ?3 AND (?4 = '' OR c.link_id = ?4) AND c.day BETWEEN date(?1, '-30 day') AND ?2
	 GROUP BY c.link_id
	 HAVING cohort_devices + SUM(CASE WHEN ` + in(1) + ` OR ` + in(7) + ` OR ` + in(30) + ` THEN c.devices ELSE 0 END) > 0
	 ORDER BY cohort_devices DESC, l.key`
	rows, err := s.db.Query(q, lo, hi, appID, targetLink)
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

func (s *Store) analyticsEvents(appID, lo, hi, targetLink string) ([]EventStat, error) {
	var rows *sql.Rows
	var err error
	if targetLink != "" {
		rows, err = s.db.Query(
			`SELECT event, SUM(n) FROM link_event_days WHERE app_id = ? AND link_id = ? AND day BETWEEN ? AND ?
			 GROUP BY event ORDER BY 2 DESC, event LIMIT 10`, appID, targetLink, lo, hi)
	} else {
		rows, err = s.db.Query(
			`SELECT event, SUM(n) FROM event_days WHERE app_id = ? AND day BETWEEN ? AND ?
			 GROUP BY event ORDER BY 2 DESC, event LIMIT 10`, appID, lo, hi)
	}
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

func (s *Store) analyticsSources(appID, lo, hi, targetLink string) ([]SourceStat, error) {
	rows, err := s.db.Query(
		`SELECT source, SUM(n) FROM click_sources WHERE app_id = ? AND (? = '' OR link_id = ?) AND day BETWEEN ? AND ?
		 GROUP BY source ORDER BY 2 DESC, source`, appID, targetLink, targetLink, lo, hi)
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

const LinkTag = "detur_link"

const deviceLinkDays = 90

type LinkEvent struct {
	AppID     string
	Device    string
	LinkKey   string
	Event     string
	Retention bool
}

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

type MatchQuality struct {
	Methods map[string]int64 `json:"methods"`
	Buckets []ScoreBucket    `json:"buckets"`
}

type ScoreBucket struct {
	From    int   `json:"from"`
	Matched int64 `json:"matched"`
	Organic int64 `json:"organic"`
}

const ScoreBucketSize = 50

func (s *Store) MatchQuality(appID string, days int, now time.Time) (MatchQuality, error) {
	q := MatchQuality{Methods: map[string]int64{}, Buckets: []ScoreBucket{}}
	lo, hi := day(now.UTC().AddDate(0, 0, -(days-1))), day(now)
	rows, err := s.db.Query(
		`SELECT COALESCE(method, ''), COUNT(*) FROM installs
		 WHERE app_id = ? AND created_at BETWEEN ? AND ? || '~' AND attribution IN (?, ?)
		 GROUP BY 1`, appID, lo, hi, AttributionOrganic, AttributionNonOrganic)
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
		 WHERE app_id = ? AND created_at BETWEEN ? AND ? || '~' AND score IS NOT NULL AND method IN (?, ?)
		 GROUP BY 1 ORDER BY 1`,
		ScoreBucketSize, ScoreBucketSize, MethodProbabilistic, MethodOrganic,
		appID, lo, hi, MethodProbabilistic, MethodOrganic)
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

func (s *Store) FraudRange(appID string, from, to time.Time) (FraudStats, error) {
	f := FraudStats{Installs: []FlaggedInstall{}}
	var err error
	if f.Signals, err = s.FraudSignalsRange(appID, from, to); err != nil {
		return f, err
	}
	lo, hi := day(from.UTC()), day(to.UTC())
	rows, err := s.db.Query(
		`SELECT id, device_hash, created_at, attribution, COALESCE(method, ''), COALESCE(link_id, ''), COALESCE(platform, ''),
		   fraud, COALESCE(fraud_action, ''), COALESCE(fraud_link_id, '')
		 FROM installs WHERE app_id = ? AND created_at BETWEEN ? AND ? || '~' AND fraud IS NOT NULL
		 ORDER BY created_at DESC LIMIT ?`, appID, lo, hi, flaggedLimit)
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

func (s *Store) FraudSignalsRange(appID string, from, to time.Time) (map[string]int64, error) {
	lo, hi := day(from.UTC()), day(to.UTC())
	sums := make([]string, len(fraud.Signals))
	args := make([]any, 0, len(fraud.Signals)+3)
	dest := make([]any, len(fraud.Signals))
	counts := make([]int64, len(fraud.Signals))
	for i, sig := range fraud.Signals {
		sums[i] = `COALESCE(SUM(instr(',' || fraud || ',', ?) > 0), 0)`
		args = append(args, ","+sig+",")
		dest[i] = &counts[i]
	}
	args = append(args, appID, lo, hi)
	if err := s.db.QueryRow(
		`SELECT `+strings.Join(sums, ", ")+` FROM installs
		 WHERE app_id = ? AND created_at BETWEEN ? AND ? || '~'`, args...).Scan(dest...); err != nil {
		return nil, fmt.Errorf("fraud signals: %w", err)
	}
	out := make(map[string]int64, len(fraud.Signals))
	for i, sig := range fraud.Signals {
		out[sig] = counts[i]
	}
	return out, nil
}
