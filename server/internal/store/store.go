// Package store persists detur state in SQLite (WAL mode).
//
// Attribution: organic | non_organic | unknown (backend-error path). Clicks carry
// expires_at = retention; deterministic clickId lookups skip the window filter
// (deterministic match has no window), match applies the app's window at query time.
package store

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite" // pure-Go driver, no cgo, static builds

	"detur.dev/server/internal/ua"
)

//go:embed schema.sql
var schemaSQL string

const (
	// AttributionOrganic: install with no matched click.
	AttributionOrganic = "organic"
	// AttributionNonOrganic: install matched to a click.
	AttributionNonOrganic = "non_organic"
	// AttributionUnknown: backend-error path (logged, not surfaced in analytics).
	AttributionUnknown = "unknown"
)

// Match methods (installs.method).
const (
	MethodPrior         = "prior"         // same-device retry
	MethodClickID       = "click_id"      // deterministic
	MethodProbabilistic = "probabilistic" // score >= threshold
	MethodOrganic       = "organic"       // no match
	MethodUnknown       = "unknown"       // backend error
)

// ErrNotFound: requested row does not exist.
var ErrNotFound = errors.New("not found")
var ErrKeyConflict = errors.New("short key already exists")
var ErrAmbiguousKey = errors.New("short key is ambiguous")

type Store struct {
	db *sql.DB
	// ClickIDHours: unmatched click_id life (CLICK_ID_DAYS). Fingerprint scrubbed at expires_at. 0 = expires_at only.
	ClickIDHours int
}

// Open: open SQLite database at path, creating if needed.
func Open(path string) (*Store, error) {
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	db, err := sql.Open("sqlite", path+sep+"_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	db.SetMaxOpenConns(1) // single writer, WAL readers still share the one conn
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("PRAGMA journal_mode=WAL: %w", err)
	}
	if _, err := db.Exec(schemaSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	hadMatch := columnExists(db, "apps", "match_threshold")
	// legacy installs rebuild drops unknown columns: run before addMissingColumns
	if err := migrateInstallUniqueness(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate installs: %w", err)
	}
	if err := addMissingColumns(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate columns: %w", err)
	}
	if !hadMatch {
		if err := seedAppMatchFromSettings(db); err != nil {
			db.Close()
			return nil, fmt.Errorf("seed app match settings: %w", err)
		}
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// Ping: database answers trivial query (healthcheck probe).
func (s *Store) Ping() error {
	var one int
	return s.db.QueryRow(`SELECT 1`).Scan(&one)
}

// HashKey: SHA-256 hex digest of API key (hash-at-rest).
func HashKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

// App: registered app holding links + API keys; trailing fields carry well-known hosting details: iOS appID (TEAMID.BUNDLEID), Android package + SHA-256 cert fingerprint.
type App struct {
	ID                     string
	Name                   string
	APIKeyHash             string
	IOSAppID               string
	AndroidPackage         string
	AndroidCertFingerprint string
	MatchThreshold         int
	MatchWindowMinutes     int
	TagLinks               bool // match-link + resolve-short destinations carry detur_link=<key>
}

// CreateApp: insert app; apiKey returned once from portal, only hash stored.
func (s *Store) CreateApp(name, apiKey string) (App, error) {
	id := Nanoid(21)
	hash := HashKey(apiKey)
	if _, err := s.db.Exec(
		`INSERT INTO apps (id, name, api_key_hash) VALUES (?, ?, ?)`, id, name, hash,
	); err != nil {
		return App{}, fmt.Errorf("create app: %w", err)
	}
	return App{ID: id, Name: name, APIKeyHash: hash, MatchThreshold: 850, MatchWindowMinutes: 15}, nil
}

// GetApp: app by id incl. well-known hosting details.
func (s *Store) GetApp(id string) (App, error) {
	var a App
	err := s.db.QueryRow(
		`SELECT id, name, api_key_hash, COALESCE(ios_app_id, ''), COALESCE(android_package, ''), COALESCE(android_cert_fingerprint, ''), match_threshold, match_window_minutes, tag_links
		 FROM apps WHERE id = ?`, id,
	).Scan(&a.ID, &a.Name, &a.APIKeyHash, &a.IOSAppID, &a.AndroidPackage, &a.AndroidCertFingerprint, &a.MatchThreshold, &a.MatchWindowMinutes, &a.TagLinks)
	if errors.Is(err, sql.ErrNoRows) {
		return App{}, ErrNotFound
	}
	if err != nil {
		return App{}, err
	}
	return a, nil
}

// UpdateAppDetails: set hosting details; empty string clears field (nullable columns); ErrNotFound if app unknown.
func (s *Store) UpdateAppDetails(id, iosAppID, androidPackage, certFingerprint string) error {
	res, err := s.db.Exec(
		`UPDATE apps SET ios_app_id = ?, android_package = ?, android_cert_fingerprint = ? WHERE id = ?`,
		nullStr(iosAppID), nullStr(androidPackage), nullStr(certFingerprint), id,
	)
	if err != nil {
		return fmt.Errorf("update app details: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateAppMatchSettings: set app's match threshold + window (Detour: app-level); ErrNotFound if app unknown. Range checks live in match.
func (s *Store) UpdateAppMatchSettings(id string, threshold, windowMinutes int) error {
	res, err := s.db.Exec(`UPDATE apps SET match_threshold = ?, match_window_minutes = ? WHERE id = ?`, threshold, windowMinutes, id)
	if err != nil {
		return fmt.Errorf("update app match settings: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateAppTagLinks: turn destination link tagging on or off; ErrNotFound if app unknown.
func (s *Store) UpdateAppTagLinks(id string, on bool) error {
	res, err := s.db.Exec(`UPDATE apps SET tag_links = ? WHERE id = ?`, boolInt(on), id)
	if err != nil {
		return fmt.Errorf("update app tag links: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteApp: remove app; links cascade (schema ON DELETE CASCADE), clicks/installs/events have no FK so deleted explicitly. New app_id tables go here.
func (s *Store) DeleteApp(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("delete app: %w", err)
	}
	defer tx.Rollback()
	res, err := tx.Exec(`DELETE FROM apps WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete app: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	for _, t := range []string{"clicks", "installs", "events", "click_days", "click_sources", "event_days", "fraud_settings", "click_hits",
		"device_links", "link_event_days", "link_cohorts", "variant_days"} {
		if _, err := tx.Exec(`DELETE FROM `+t+` WHERE app_id = ?`, id); err != nil {
			return fmt.Errorf("delete app %s: %w", t, err)
		}
	}
	return tx.Commit()
}

func (s *Store) ListApps() ([]App, error) {
	rows, err := s.db.Query(
		`SELECT id, name, api_key_hash, COALESCE(ios_app_id, ''), COALESCE(android_package, ''), COALESCE(android_cert_fingerprint, ''), match_threshold, match_window_minutes, tag_links
		 FROM apps ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("list apps: %w", err)
	}
	defer rows.Close()
	var apps []App
	for rows.Next() {
		var a App
		if err := rows.Scan(&a.ID, &a.Name, &a.APIKeyHash, &a.IOSAppID, &a.AndroidPackage, &a.AndroidCertFingerprint, &a.MatchThreshold, &a.MatchWindowMinutes, &a.TagLinks); err != nil {
			return nil, err
		}
		apps = append(apps, a)
	}
	return apps, rows.Err()
}

// UpdateAppKey: replace API key hash (rotation); previous key stops working immediately.
func (s *Store) UpdateAppKey(id, apiKey string) error {
	res, err := s.db.Exec(`UPDATE apps SET api_key_hash = ? WHERE id = ?`, HashKey(apiKey), id)
	if err != nil {
		return fmt.Errorf("update app key: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ClearAppKey: remove API key (revocation); SDK auth fails.
func (s *Store) ClearAppKey(id string) error {
	res, err := s.db.Exec(`UPDATE apps SET api_key_hash = '' WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("clear app key: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// sdkSeenEvery: last-seen write throttle per app; version change bypasses.
const sdkSeenEvery = 5 * time.Minute

// NoteSDK: store X-SDK of an authed call.
func (s *Store) NoteSDK(appID, version string, now time.Time) error {
	if r := []rune(version); len(r) > 64 {
		version = string(r[:64])
	}
	_, err := s.db.Exec(
		`UPDATE apps SET sdk_version = ?, sdk_seen_at = ?
		 WHERE id = ? AND (sdk_seen_at IS NULL OR sdk_seen_at < ? OR sdk_version IS NOT ?)`,
		version, rfc3339(now), appID, rfc3339(now.Add(-sdkSeenEvery)), version)
	if err != nil {
		return fmt.Errorf("note sdk: %w", err)
	}
	return nil
}

// SDKSeen: last X-SDK + time; zero = never.
func (s *Store) SDKSeen(appID string) (version string, at time.Time, err error) {
	var seen string
	err = s.db.QueryRow(`SELECT COALESCE(sdk_version, ''), COALESCE(sdk_seen_at, '') FROM apps WHERE id = ?`, appID).Scan(&version, &seen)
	if errors.Is(err, sql.ErrNoRows) {
		return "", time.Time{}, ErrNotFound
	}
	if seen != "" {
		at = parseTime(seen)
	}
	return version, at, err
}

// RecentLinkKeys: link keys created >= since.
func (s *Store) RecentLinkKeys(appID string, since time.Time) ([]string, error) {
	rows, err := s.db.Query(`SELECT key FROM links WHERE app_id = ? AND created_at >= ? ORDER BY created_at`, appID, rfc3339(since))
	if err != nil {
		return nil, fmt.Errorf("recent links: %w", err)
	}
	defer rows.Close()
	var keys []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

// ValidateAPIKey: key matches app's stored hash.
func (s *Store) ValidateAPIKey(appID, apiKey string) (bool, error) {
	var hash string
	err := s.db.QueryRow(`SELECT api_key_hash FROM apps WHERE id = ?`, appID).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("validate api key: %w", err)
	}
	return subtle.ConstantTimeCompare([]byte(hash), []byte(HashKey(apiKey))) == 1, nil
}

// Link: short link under an app.
type Link struct {
	ID          string
	AppID       string
	Key         string
	URL         string
	IOS         string
	Android     string
	FallbackURL string
	ExpiresAt   *time.Time // nil = never expires
	ExpiredURL  string     // redirect target once expired; empty = 410
}

// Expired: link past expiry.
func (l Link) Expired(now time.Time) bool {
	return l.ExpiresAt != nil && now.After(*l.ExpiresAt)
}

// CreateLink: insert link; ErrKeyConflict on duplicate key, ErrNotFound on unknown app.
func (s *Store) CreateLink(l Link) (Link, error) {
	if l.ID == "" {
		l.ID = Nanoid(16)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return Link{}, fmt.Errorf("create link: %w", err)
	}
	defer tx.Rollback()
	var exists int
	if err := tx.QueryRow(`SELECT 1 FROM links WHERE key = ? COLLATE NOCASE LIMIT 1`, l.Key).Scan(&exists); err == nil {
		return Link{}, ErrKeyConflict
	} else if !errors.Is(err, sql.ErrNoRows) {
		return Link{}, fmt.Errorf("create link: %w", err)
	}
	_, err = tx.Exec(
		`INSERT INTO links (id, app_id, key, url, ios, android, fallback_url, expires_at, expired_url)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		l.ID, l.AppID, l.Key, l.URL, nullStr(l.IOS), nullStr(l.Android), nullStr(l.FallbackURL),
		nullTime(l.ExpiresAt), nullStr(l.ExpiredURL),
	)
	if err != nil {
		// Unknown app_id hits FK; portal treats it as 404.
		if strings.Contains(err.Error(), "FOREIGN KEY") {
			return Link{}, ErrNotFound
		}
		return Link{}, fmt.Errorf("create link: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Link{}, fmt.Errorf("create link: %w", err)
	}
	return l, nil
}

func (s *Store) GetLinkByKey(appID, key string) (Link, error) {
	return scanLink(s.db.QueryRow(
		`SELECT id, app_id, key, url, COALESCE(ios, ''), COALESCE(android, ''), COALESCE(fallback_url, ''), COALESCE(expires_at, ''), COALESCE(expired_url, '')
		 FROM links WHERE app_id = ? AND key = ? COLLATE NOCASE`, appID, key,
	))
}

// GetLinkByKeyGlobal: resolve short key across all apps; historic duplicates fail closed, no silent traffic to wrong app.
func (s *Store) GetLinkByKeyGlobal(key string) (Link, error) {
	rows, err := s.db.Query(
		`SELECT id, app_id, key, url, COALESCE(ios, ''), COALESCE(android, ''), COALESCE(fallback_url, ''), COALESCE(expires_at, ''), COALESCE(expired_url, '')
		 FROM links WHERE key = ? COLLATE NOCASE LIMIT 2`, key,
	)
	if err != nil {
		return Link{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return Link{}, err
		}
		return Link{}, ErrNotFound
	}
	l, err := scanLinkRow(rows)
	if err != nil {
		return Link{}, err
	}
	if rows.Next() {
		return Link{}, ErrAmbiguousKey
	}
	return l, rows.Err()
}

func (s *Store) GetLink(id string) (Link, error) {
	return scanLink(s.db.QueryRow(
		`SELECT id, app_id, key, url, COALESCE(ios, ''), COALESCE(android, ''), COALESCE(fallback_url, ''), COALESCE(expires_at, ''), COALESCE(expired_url, '')
		 FROM links WHERE id = ?`, id,
	))
}

func (s *Store) ListLinks(appID string) ([]Link, error) {
	rows, err := s.db.Query(
		`SELECT id, app_id, key, url, COALESCE(ios, ''), COALESCE(android, ''), COALESCE(fallback_url, ''), COALESCE(expires_at, ''), COALESCE(expired_url, '')
		 FROM links WHERE app_id = ? ORDER BY created_at`, appID,
	)
	if err != nil {
		return nil, fmt.Errorf("list links: %w", err)
	}
	defer rows.Close()
	var links []Link
	for rows.Next() {
		l, err := scanLinkRow(rows)
		if err != nil {
			return nil, err
		}
		links = append(links, l)
	}
	return links, rows.Err()
}

// UpdateLink: update mutable link fields.
func (s *Store) UpdateLink(l Link) error {
	res, err := s.db.Exec(
		`UPDATE links SET url = ?, ios = ?, android = ?, fallback_url = ?, expires_at = ?, expired_url = ?
		 WHERE id = ?`,
		l.URL, nullStr(l.IOS), nullStr(l.Android), nullStr(l.FallbackURL),
		nullTime(l.ExpiresAt), nullStr(l.ExpiredURL), l.ID,
	)
	if err != nil {
		return fmt.Errorf("update link: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteLink: link + its clicks (no FK on clicks; orphans would keep matching), rollups, link hit counter and device tags.
func (s *Store) DeleteLink(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("delete link: %w", err)
	}
	defer tx.Rollback()
	res, err := tx.Exec(`DELETE FROM links WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete link: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if _, err := tx.Exec(`DELETE FROM clicks WHERE link_id = ?`, id); err != nil {
		return fmt.Errorf("delete link clicks: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM click_days WHERE link_id = ?`, id); err != nil {
		return fmt.Errorf("delete link rollups: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM click_sources WHERE link_id = ?`, id); err != nil {
		return fmt.Errorf("delete link source rollups: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM click_hits WHERE key_type = 'link' AND key = ?`, id); err != nil {
		return fmt.Errorf("delete link hits: %w", err)
	}
	for _, t := range []string{"device_links", "link_event_days", "link_cohorts", "link_rules", "variant_days"} {
		if _, err := tx.Exec(`DELETE FROM `+t+` WHERE link_id = ?`, id); err != nil {
			return fmt.Errorf("delete link %s: %w", t, err)
		}
	}
	return tx.Commit()
}

func scanLink(row *sql.Row) (Link, error) {
	l, err := scanLinkRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Link{}, ErrNotFound
	}
	return l, err
}

func scanLinkRow(row rowScanner) (Link, error) {
	var l Link
	var expiresAt string
	if err := row.Scan(&l.ID, &l.AppID, &l.Key, &l.URL, &l.IOS, &l.Android, &l.FallbackURL, &expiresAt, &l.ExpiredURL); err != nil {
		return Link{}, err
	}
	if expiresAt != "" {
		t := parseTime(expiresAt)
		l.ExpiresAt = &t
	}
	return l, nil
}

func nullTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return rfc3339(*t)
}

// Fingerprint: device signal set captured at click time (IP, device, locale, timezone, screen, user-agent, pasted link), matched against first-launch fingerprint.
type Fingerprint struct {
	IP         string
	Device     string
	Locale     string
	Timezone   string
	Screen     string
	UserAgent  string
	OSVersion  string // UA Client Hint Sec-CH-UA-Platform-Version; empty = derive from UA
	PastedLink string
}

type Click struct {
	ID          string
	AppID       string
	LinkID      string
	Fingerprint Fingerprint
	Destination string
	ClickID     string // deterministic path (Android Play referrer / explicit)
	IsBot       bool
	CreatedAt   time.Time
	ExpiresAt   time.Time
	// Platform (ios | android | desktop | ""): click_days rollup only, not stored. Kind (KindApp | KindWeb | KindOpen): rollup + stored (open clicks skip UA/short-timing, KTD3).
	Platform string
	Kind     string
	// Source: in-app browser that sent the click (ua.InApp name or "unknown-inapp"), "" = real browser or SDK. Stored + click_sources rollup; not device data, survives the scrub.
	Source string
	// Fraud facts (KTD1), judged at match time. FirstSeenAt: first tap (refresh keeps it). HitsIP/HitsLink: max hits seen over the velocity window, set by RecordClick.
	FirstSeenAt      time.Time
	UASuspect        bool
	IPHosting        bool
	HitsIP, HitsLink int
	// Variant: assigned A/B split variant name ("" = default / no rule). Stored + variant_days rollup.
	Variant string
}

// Click kinds for analytics rollups (click_days.kind).
const (
	KindApp  = "app"  // browser click redirected to a store
	KindWeb  = "web"  // browser click redirected to a web page (web fallback)
	KindOpen = "open" // SDK universal-link open: app already installed
)

// clickDedupWindow: mirrors Dub's recordClickCache — one click per link + device (IP + user agent) per hour.
const clickDedupWindow = time.Hour

// RecordClick: persist click, or refresh same device's (IP + user agent) click on same link within last hour (Dub record-click-cache); one clickId per device. Re-tap re-enters match window (Dub rewrites deepLinkClickCache per tap). Expiry = retention: deterministic lookups live that long; probabilistic window applied by match at query time.
func (s *Store) RecordClick(c Click, retentionHours int) (Click, error) {
	expires := time.Duration(retentionHours) * time.Hour
	now := time.Now().UTC()
	tx, err := s.db.Begin()
	if err != nil {
		return Click{}, fmt.Errorf("record click: %w", err)
	}
	defer tx.Rollback()
	// velocity counter before dedup: merged repeats still count (KTD2)
	c.HitsIP, c.HitsLink, err = bumpHits(tx, c, now)
	if err != nil {
		return Click{}, fmt.Errorf("record click hits: %w", err)
	}
	existing, err := scanClick(tx.QueryRow(
		`SELECT `+clickCols+` FROM clicks
		 WHERE link_id = ? AND ip IS ? AND COALESCE(user_agent, '') = ? AND created_at >= ? AND expires_at >= ?
		 ORDER BY created_at DESC LIMIT 1`,
		c.LinkID, nullStr(c.Fingerprint.IP), c.Fingerprint.UserAgent, rfc3339(now.Add(-clickDedupWindow)), rfc3339(now),
	))
	if err == nil {
		return s.refreshClick(tx, existing, c, now, now.Add(expires))
	}
	if !errors.Is(err, ErrNotFound) {
		return Click{}, fmt.Errorf("record click dedup: %w", err)
	}
	// in-app reopen: "Open in browser" reloads the link in a real browser (no source) on the same network and platform. SDK opens stay their own counter.
	if c.Kind != KindOpen && c.Source == "" && c.Fingerprint.IP != "" {
		existing, err = reopenCandidate(tx, c, now)
		if err == nil {
			// a reopen carries the same URL: keep the in-app click's destination, variant and pasteboard signal
			c.Destination, c.Variant, c.Fingerprint.PastedLink = "", "", ""
			return s.refreshClick(tx, existing, c, now, now.Add(expires))
		}
		if !errors.Is(err, ErrNotFound) {
			return Click{}, fmt.Errorf("record click reopen: %w", err)
		}
	}
	if c.ID == "" {
		c.ID = Nanoid(16)
	}
	// id handed to clients (rec.ID, Android Play referrer, universal-link clickId) is the deterministic match key: default click_id to it so ClickByClickID resolves SDK echo.
	if c.ClickID == "" {
		c.ClickID = c.ID
	}
	c.CreatedAt = now
	c.ExpiresAt = now.Add(expires)
	_, err = tx.Exec(
		`INSERT INTO clicks (id, app_id, link_id, ip, device, locale, timezone, screen, user_agent, os_version, pasted_link, destination, click_id, is_bot, created_at, expires_at,
		   kind, first_seen_at, ua_suspect, ip_hosting, hits_ip, hits_link, source, variant)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ID, c.AppID, c.LinkID, nullStr(c.Fingerprint.IP), nullStr(c.Fingerprint.Device),
		nullStr(c.Fingerprint.Locale), nullStr(c.Fingerprint.Timezone), nullStr(c.Fingerprint.Screen),
		nullStr(c.Fingerprint.UserAgent), nullStr(c.Fingerprint.OSVersion), nullStr(c.Fingerprint.PastedLink), c.Destination,
		nullStr(c.ClickID), boolInt(c.IsBot), rfc3339(c.CreatedAt), rfc3339(c.ExpiresAt),
		nullStr(c.Kind), rfc3339(c.CreatedAt), boolInt(c.UASuspect), boolInt(c.IPHosting), c.HitsIP, c.HitsLink, nullStr(c.Source), nullStr(c.Variant),
	)
	if err != nil {
		return Click{}, fmt.Errorf("record click: %w", err)
	}
	// rollup counts new clicks only; a dedup refresh is the same click. UA-suspect stays out (R3).
	if !c.UASuspect {
		_, err = tx.Exec(
			`INSERT INTO click_days (app_id, link_id, day, platform, kind, n) VALUES (?, ?, ?, ?, ?, 1)
			 ON CONFLICT DO UPDATE SET n = n + 1`,
			c.AppID, c.LinkID, day(now), c.Platform, c.Kind,
		)
		if err != nil {
			return Click{}, fmt.Errorf("record click rollup: %w", err)
		}
		if c.Source != "" {
			if _, err := tx.Exec(
				`INSERT INTO click_sources (app_id, link_id, day, source, n) VALUES (?, ?, ?, ?, 1)
				 ON CONFLICT DO UPDATE SET n = n + 1`,
				c.AppID, c.LinkID, day(now), c.Source,
			); err != nil {
				return Click{}, fmt.Errorf("record click source rollup: %w", err)
			}
		}
		if c.Variant != "" {
			if _, err := tx.Exec(
				`INSERT INTO variant_days (app_id, link_id, variant, day, clicks, installs) VALUES (?, ?, ?, ?, 1, 0)
				 ON CONFLICT(app_id, link_id, variant, day) DO UPDATE SET clicks = clicks + 1`,
				c.AppID, c.LinkID, c.Variant, day(now),
			); err != nil {
				return Click{}, fmt.Errorf("record click variant rollup: %w", err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return Click{}, fmt.Errorf("record click: %w", err)
	}
	c.CreatedAt = parseTime(rfc3339(c.CreatedAt)) // stored precision
	c.ExpiresAt = parseTime(rfc3339(c.ExpiresAt))
	c.FirstSeenAt = c.CreatedAt
	return c, nil
}

// reopenCandidate: newest in-app click on the same link + IP within the dedup window, from a different UA on the visitor's platform. Platform lives only in the UA, so candidates are filtered here, newest first.
func reopenCandidate(tx *sql.Tx, c Click, now time.Time) (Click, error) {
	rows, err := tx.Query(
		`SELECT `+clickCols+` FROM clicks
		 WHERE link_id = ? AND ip = ? AND COALESCE(source, '') != '' AND COALESCE(user_agent, '') != ? AND COALESCE(kind, '') != ?
		   AND created_at >= ? AND expires_at >= ?
		 ORDER BY created_at DESC LIMIT 20`,
		c.LinkID, c.Fingerprint.IP, c.Fingerprint.UserAgent, KindOpen, rfc3339(now.Add(-clickDedupWindow)), rfc3339(now),
	)
	if err != nil {
		return Click{}, err
	}
	defer rows.Close()
	platform := ua.Platform(c.Fingerprint.UserAgent)
	for rows.Next() {
		cand, err := scanClickRows(rows)
		if err != nil {
			return Click{}, err
		}
		if ua.Platform(cand.Fingerprint.UserAgent) == platform {
			return cand, nil
		}
	}
	if err := rows.Err(); err != nil {
		return Click{}, err
	}
	return Click{}, ErrNotFound
}

// refreshClick: dedup hit. Same id, created_at = now, expiry only extends, non-empty new signals overwrite, hit maxima only grow; first_seen_at kept.
func (s *Store) refreshClick(tx *sql.Tx, old, c Click, now, expires time.Time) (Click, error) {
	_, err := tx.Exec(
		`UPDATE clicks SET created_at = ?, expires_at = MAX(expires_at, ?),
		   device = COALESCE(?, device), locale = COALESCE(?, locale), timezone = COALESCE(?, timezone),
		   screen = COALESCE(?, screen), os_version = COALESCE(?, os_version), pasted_link = COALESCE(?, pasted_link),
		   destination = COALESCE(NULLIF(?, ''), destination),
		   variant = CASE WHEN ? = '' THEN variant ELSE ? END,
		   hits_ip = MAX(hits_ip, ?), hits_link = MAX(hits_link, ?)
		 WHERE id = ?`,
		rfc3339(now), rfc3339(expires),
		nullStr(c.Fingerprint.Device), nullStr(c.Fingerprint.Locale), nullStr(c.Fingerprint.Timezone),
		nullStr(c.Fingerprint.Screen), nullStr(c.Fingerprint.OSVersion), nullStr(c.Fingerprint.PastedLink),
		c.Destination, c.Destination, nullStr(c.Variant), c.HitsIP, c.HitsLink, old.ID,
	)
	if err != nil {
		return Click{}, fmt.Errorf("record click refresh: %w", err)
	}
	// variant follows the served destination; a switch (rule edited mid-window) is a new exposure for the new variant
	if c.Destination != "" && c.Variant != "" && c.Variant != old.Variant && !c.UASuspect {
		if _, err := tx.Exec(
			`INSERT INTO variant_days (app_id, link_id, variant, day, clicks, installs) VALUES (?, ?, ?, ?, 1, 0)
			 ON CONFLICT(app_id, link_id, variant, day) DO UPDATE SET clicks = clicks + 1`,
			old.AppID, old.LinkID, c.Variant, day(now),
		); err != nil {
			return Click{}, fmt.Errorf("record click refresh variant rollup: %w", err)
		}
	}
	refreshed, err := scanClick(tx.QueryRow(`SELECT `+clickCols+` FROM clicks WHERE id = ?`, old.ID))
	if err != nil {
		return Click{}, fmt.Errorf("record click refresh: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Click{}, fmt.Errorf("record click refresh: %w", err)
	}
	return refreshed, nil
}

const clickCols = `id, app_id, link_id, COALESCE(ip, ''), COALESCE(device, ''), COALESCE(locale, ''), COALESCE(timezone, ''), COALESCE(screen, ''), COALESCE(user_agent, ''), COALESCE(os_version, ''), COALESCE(pasted_link, ''), destination, COALESCE(click_id, ''), is_bot, created_at, expires_at,
  COALESCE(kind, ''), COALESCE(first_seen_at, created_at), ua_suspect, ip_hosting, hits_ip, hits_link, COALESCE(source, ''), COALESCE(variant, '')`

const clickColsC = `c.id, c.app_id, c.link_id, COALESCE(c.ip, ''), COALESCE(c.device, ''), COALESCE(c.locale, ''), COALESCE(c.timezone, ''), COALESCE(c.screen, ''), COALESCE(c.user_agent, ''), COALESCE(c.os_version, ''), COALESCE(c.pasted_link, ''), c.destination, COALESCE(c.click_id, ''), c.is_bot, c.created_at, c.expires_at,
  COALESCE(c.kind, ''), COALESCE(c.first_seen_at, c.created_at), c.ua_suspect, c.ip_hosting, c.hits_ip, c.hits_link, COALESCE(c.source, ''), COALESCE(c.variant, '')`

func (s *Store) GetClick(id string) (Click, error) {
	return scanClick(s.db.QueryRow(
		`SELECT `+clickCols+` FROM clicks WHERE id = ?`, id,
	))
}

// ClickByClickID: no window. Live until max(expires_at, created_at + ClickIDHours); scrubbed rows keep destination.
func (s *Store) ClickByClickID(appID, clickID string) (Click, error) {
	now := time.Now()
	return scanClick(s.db.QueryRow(
		`SELECT `+clickCols+` FROM clicks WHERE app_id = ? AND click_id = ? AND (expires_at >= ? OR created_at > ?) AND matched_at IS NULL`,
		appID, clickID, rfc3339(now), rfc3339(s.clickIDCutoff(now)),
	))
}

// clickIDCutoff: older clicks lost their click_id.
func (s *Store) clickIDCutoff(now time.Time) time.Time {
	return now.Add(-time.Duration(s.ClickIDHours) * time.Hour)
}

// MarkClickMatched: claim click for one install (Detour: "marks the click as matched"). false = already matched.
func (s *Store) MarkClickMatched(id string) (bool, error) {
	res, err := s.db.Exec(`UPDATE clicks SET matched_at = ? WHERE id = ? AND matched_at IS NULL`, rfc3339(time.Now()), id)
	if err != nil {
		return false, fmt.Errorf("mark click matched: %w", err)
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// PriorMatch: still-retained click this device was already attributed to (idempotent match-link retries).
func (s *Store) PriorMatch(appID, deviceHash string) (Click, error) {
	return scanClick(s.db.QueryRow(
		`SELECT `+clickColsC+` FROM clicks c JOIN installs i ON i.click_id = c.id
		 WHERE i.app_id = ? AND i.device_hash = ? AND i.attribution = ? AND c.expires_at >= ?
		 ORDER BY i.created_at DESC LIMIT 1`,
		appID, deviceHash, AttributionNonOrganic, rfc3339(time.Now())))
}

// ClicksSince: non-bot clicks created at or after since (window scan).
func (s *Store) ClicksSince(appID string, since time.Time) ([]Click, error) {
	rows, err := s.db.Query(
		`SELECT `+clickCols+` FROM clicks WHERE app_id = ? AND is_bot = 0 AND matched_at IS NULL AND created_at >= ? ORDER BY created_at DESC`,
		appID, rfc3339(since),
	)
	if err != nil {
		return nil, fmt.Errorf("clicks since: %w", err)
	}
	defer rows.Close()
	var clicks []Click
	for rows.Next() {
		c, err := scanClickRows(rows)
		if err != nil {
			return nil, err
		}
		clicks = append(clicks, c)
	}
	return clicks, rows.Err()
}

// PurgeExpired: expired clicks scrubbed; matched or past ClickIDHours deleted. Old events deleted. Hit buckets past the 24h max window and device tags past deviceLinkDays deleted (not counted). Returns rows deleted.
func (s *Store) PurgeExpired(now time.Time, retentionHours int) (int64, error) {
	var removed int64
	if _, err := s.db.Exec(
		`UPDATE clicks SET ip = NULL, device = NULL, locale = NULL, timezone = NULL, screen = NULL,
		   user_agent = NULL, os_version = NULL, pasted_link = NULL
		 WHERE expires_at < ? AND (ip IS NOT NULL OR user_agent IS NOT NULL)`, rfc3339(now)); err != nil {
		return 0, fmt.Errorf("scrub clicks: %w", err)
	}
	res, err := s.db.Exec(`DELETE FROM clicks WHERE expires_at < ? AND (matched_at IS NOT NULL OR created_at <= ?)`,
		rfc3339(now), rfc3339(s.clickIDCutoff(now)))
	if err != nil {
		return 0, fmt.Errorf("purge clicks: %w", err)
	}
	if n, _ := res.RowsAffected(); n > 0 {
		removed += n
	}
	cutoff := now.Add(-time.Duration(retentionHours) * time.Hour)
	res, err = s.db.Exec(`DELETE FROM events WHERE created_at < ?`, rfc3339(cutoff))
	if err != nil {
		return 0, fmt.Errorf("purge events: %w", err)
	}
	if n, _ := res.RowsAffected(); n > 0 {
		removed += n
	}
	if _, err := s.db.Exec(`DELETE FROM click_hits WHERE bucket < ?`, now.UTC().Add(-24*time.Hour).Format(bucketLayout)); err != nil {
		return 0, fmt.Errorf("purge click hits: %w", err)
	}
	if _, err := s.db.Exec(`DELETE FROM device_links WHERE first_seen < ?`, rfc3339(now.AddDate(0, 0, -deviceLinkDays))); err != nil {
		return 0, fmt.Errorf("purge device links: %w", err)
	}
	return removed, nil
}

func scanClick(row rowScanner) (Click, error) {
	c, err := scanClickRows(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Click{}, ErrNotFound
	}
	return c, err
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanClickRows(row rowScanner) (Click, error) {
	var (
		c                                                                Click
		ip, device, locale, timezone, screen, ua, osVer, pasted, clickID string
		createdAt, expiresAt, firstSeen                                  string
	)
	err := row.Scan(&c.ID, &c.AppID, &c.LinkID, &ip, &device, &locale, &timezone,
		&screen, &ua, &osVer, &pasted, &c.Destination, &clickID, &c.IsBot, &createdAt, &expiresAt,
		&c.Kind, &firstSeen, &c.UASuspect, &c.IPHosting, &c.HitsIP, &c.HitsLink, &c.Source, &c.Variant)
	if err != nil {
		return Click{}, err
	}
	c.Fingerprint = Fingerprint{IP: ip, Device: device, Locale: locale, Timezone: timezone, Screen: screen, UserAgent: ua, OSVersion: osVer, PastedLink: pasted}
	c.ClickID = clickID
	c.CreatedAt = parseTime(createdAt)
	c.ExpiresAt = parseTime(expiresAt)
	c.FirstSeenAt = parseTime(firstSeen)
	return c, nil
}

type Install struct {
	ID          string
	AppID       string
	DeviceHash  string
	ClickID     string
	Attribution string
	CreatedAt   time.Time
	LinkID      string // matched click's link (non-organic only); analytics
	Platform    string // ios | android | ""; analytics
	Method      string // Method*; "" = NULL
	Score       int    // best score; < 0 = NULL
	RunnerUp    int    // second-best; < 0 = NULL
	Fraud       string // fired signals, comma list (fraud.Signal*); "" = clean (KTD7)
	FraudAction string // FraudAction*; "" = attribution untouched
	FraudLinkID string // excluded best click's link
	Variant     string // matched click's variant; analytics
}

// RecordInstall: upsert install attribution idempotently per app, device, click; empty click_id marks organic/unknown installs, deduped per app + device too. Only an unknown row is upgraded by a later real attribution.
func (s *Store) RecordInstall(i Install) (Install, error) {
	if i.ID == "" {
		i.ID = Nanoid(16)
	}
	now := time.Now().UTC()
	i.CreatedAt = now
	res, err := s.db.Exec(
		`INSERT INTO installs (id, app_id, device_hash, click_id, attribution, created_at, link_id, platform, method, score, runner_up, fraud, fraud_action, fraud_link_id, variant)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			 ON CONFLICT(app_id, device_hash, click_id) DO UPDATE SET attribution = excluded.attribution,
			   link_id = excluded.link_id, platform = excluded.platform,
			   method = excluded.method, score = excluded.score, runner_up = excluded.runner_up,
			   fraud = excluded.fraud, fraud_action = excluded.fraud_action, fraud_link_id = excluded.fraud_link_id,
			   variant = COALESCE(NULLIF(excluded.variant, ''), installs.variant)
			 WHERE installs.attribution = ?`,
		i.ID, i.AppID, i.DeviceHash, i.ClickID, i.Attribution, rfc3339(now), nullStr(i.LinkID), nullStr(i.Platform),
		nullStr(i.Method), i.nullScore(i.Score), i.nullScore(i.RunnerUp), nullStr(i.Fraud), nullStr(i.FraudAction), nullStr(i.FraudLinkID), nullStr(i.Variant), AttributionUnknown,
	)
	if err != nil {
		return Install{}, fmt.Errorf("record install: %w", err)
	}
	n, _ := res.RowsAffected()
	if n > 0 && i.Variant != "" && i.LinkID != "" && i.Attribution != AttributionOrganic && i.Attribution != AttributionUnknown {
		if _, err := s.db.Exec(
			`INSERT INTO variant_days (app_id, link_id, variant, day, clicks, installs) VALUES (?, ?, ?, ?, 0, 1)
			 ON CONFLICT(app_id, link_id, variant, day) DO UPDATE SET installs = installs + 1`,
			i.AppID, i.LinkID, i.Variant, day(now),
		); err != nil {
			return Install{}, fmt.Errorf("record install variant rollup: %w", err)
		}
	}
	// Return stored row (surviving row on conflict).
	var (
		got       Install
		createdAt string
		variant   string
	)
	err = s.db.QueryRow(
		`SELECT id, app_id, device_hash, click_id, attribution, created_at, COALESCE(variant, '')
		 FROM installs WHERE app_id = ? AND device_hash = ? AND click_id = ?`,
		i.AppID, i.DeviceHash, i.ClickID,
	).Scan(&got.ID, &got.AppID, &got.DeviceHash, &got.ClickID, &got.Attribution, &createdAt, &variant)
	if err != nil {
		return Install{}, fmt.Errorf("record install readback: %w", err)
	}
	got.CreatedAt = parseTime(createdAt)
	got.Variant = variant
	return got, nil
}

// addMissingColumns: columns introduced after v0.1, added to existing databases (CREATE TABLE IF NOT EXISTS leaves old tables untouched).
func addMissingColumns(db *sql.DB) error {
	for _, c := range []struct{ table, column, decl string }{
		{"links", "expires_at", "TEXT"},
		{"links", "expired_url", "TEXT"},
		{"clicks", "os_version", "TEXT"},
		{"clicks", "matched_at", "TEXT"},
		{"apps", "match_threshold", "INTEGER NOT NULL DEFAULT 850"},
		{"apps", "match_window_minutes", "INTEGER NOT NULL DEFAULT 15"},
		{"installs", "link_id", "TEXT"},
		{"installs", "platform", "TEXT"},
		{"installs", "method", "TEXT"},
		{"installs", "score", "INTEGER"},
		{"installs", "runner_up", "INTEGER"},
		{"apps", "sdk_version", "TEXT"},
		{"apps", "sdk_seen_at", "TEXT"},
		{"clicks", "kind", "TEXT"},
		{"clicks", "first_seen_at", "TEXT"},
		{"clicks", "ua_suspect", "INTEGER NOT NULL DEFAULT 0"},
		{"clicks", "ip_hosting", "INTEGER NOT NULL DEFAULT 0"},
		{"clicks", "hits_ip", "INTEGER NOT NULL DEFAULT 0"},
		{"clicks", "hits_link", "INTEGER NOT NULL DEFAULT 0"},
		{"clicks", "source", "TEXT"},
		{"installs", "fraud", "TEXT"},
		{"installs", "fraud_action", "TEXT"},
		{"installs", "fraud_link_id", "TEXT"},
		{"apps", "tag_links", "INTEGER NOT NULL DEFAULT 0"},
		{"clicks", "variant", "TEXT"},
		{"installs", "variant", "TEXT"},
	} {
		if !columnExists(db, c.table, c.column) {
			if _, err := db.Exec(`ALTER TABLE ` + c.table + ` ADD COLUMN ` + c.column + ` ` + c.decl); err != nil {
				return err
			}
		}
	}
	return nil
}

func columnExists(db *sql.DB, table, column string) bool {
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?`, table, column).Scan(&n)
	return n > 0
}

func tableExists(db *sql.DB, name string) bool {
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, name).Scan(&n)
	return n > 0
}

// seedAppMatchFromSettings: one-time upgrade, old instance-wide settings become every app's settings. Out-of-range/garbage values ignored (defaults stay).
func seedAppMatchFromSettings(db *sql.DB) error {
	if !tableExists(db, "settings") {
		return nil
	}
	// bounds mirror match.Min*/Max*; store can't import match (cycle)
	for _, k := range []struct {
		key, col string
		lo, hi   int
	}{
		{"threshold", "match_threshold", 700, 1200},
		{"window_minutes", "match_window_minutes", 5, 180},
	} {
		var v string
		err := db.QueryRow(`SELECT value FROM settings WHERE key = ?`, k.key).Scan(&v)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		n, err := strconv.Atoi(v)
		if err != nil || n < k.lo || n > k.hi {
			continue
		}
		if _, err := db.Exec(`UPDATE apps SET `+k.col+` = ?`, n); err != nil {
			return err
		}
	}
	return nil
}

func migrateInstallUniqueness(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA index_list(installs)`)
	if err != nil {
		return err
	}
	legacy := false
	var uniqueIndexes []string
	for rows.Next() {
		var seq, unique, partial int
		var name, origin string
		if err := rows.Scan(&seq, &name, &unique, &origin, &partial); err != nil {
			rows.Close()
			return err
		}
		if unique == 1 {
			uniqueIndexes = append(uniqueIndexes, name)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, name := range uniqueIndexes {
		var cols []string
		info, err := db.Query(`PRAGMA index_info("` + strings.ReplaceAll(name, `"`, `""`) + `")`)
		if err != nil {
			return err
		}
		for info.Next() {
			var seq, cid int
			var col string
			if err := info.Scan(&seq, &cid, &col); err != nil {
				info.Close()
				return err
			}
			cols = append(cols, col)
		}
		if err := info.Err(); err != nil {
			info.Close()
			return err
		}
		info.Close()
		if strings.Join(cols, ",") == "device_hash,click_id" {
			legacy = true
		}
	}
	if legacy {
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		for _, q := range []string{
			`CREATE TABLE installs_new (id TEXT PRIMARY KEY, app_id TEXT NOT NULL, device_hash TEXT NOT NULL, click_id TEXT, attribution TEXT NOT NULL, created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')), UNIQUE (app_id, device_hash, click_id))`,
			`INSERT INTO installs_new SELECT id, app_id, device_hash, click_id, attribution, created_at FROM installs`,
			`DROP TABLE installs`, `ALTER TABLE installs_new RENAME TO installs`,
		} {
			if _, err := tx.Exec(q); err != nil {
				tx.Rollback()
				return err
			}
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_installs_app_attribution ON installs (app_id, attribution)`)
	return err
}

// CountInstalls: organic + non-organic install counts for app (unknown excluded).
func (s *Store) CountInstalls(appID string) (organic, nonOrganic int64, err error) {
	err = s.db.QueryRow(
		`SELECT
			COALESCE(SUM(attribution = ?), 0),
			COALESCE(SUM(attribution = ?), 0)
		 FROM installs WHERE app_id = ?`,
		AttributionOrganic, AttributionNonOrganic, appID,
	).Scan(&organic, &nonOrganic)
	return organic, nonOrganic, err
}

// CountClicks: live clicks; scrubbed rows excluded.
func (s *Store) CountClicks(appID string) (int64, error) {
	var n int64
	err := s.db.QueryRow(`SELECT COUNT(*) FROM clicks WHERE app_id = ? AND expires_at >= ?`, appID, rfc3339(time.Now())).Scan(&n)
	return n, err
}

func (s *Store) RecordEvent(appID, event, metadata string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("record event: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(
		`INSERT INTO events (id, app_id, event, metadata) VALUES (?, ?, ?, ?)`,
		Nanoid(21), appID, event, nullStr(metadata),
	); err != nil {
		return fmt.Errorf("record event: %w", err)
	}
	if _, err := tx.Exec(
		`INSERT INTO event_days (app_id, event, day, n) VALUES (?, ?, ?, 1)
		 ON CONFLICT DO UPDATE SET n = n + 1`,
		appID, rollupEventName(event), day(time.Now()),
	); err != nil {
		return fmt.Errorf("record event rollup: %w", err)
	}
	return tx.Commit()
}

func (s *Store) CountEvents(appID string) (int64, error) {
	var n int64
	err := s.db.QueryRow(`SELECT COUNT(*) FROM events WHERE app_id = ?`, appID).Scan(&n)
	return n, err
}

// rawExec: execute statement directly (test-only hook for expiry/purge setup).
func (s *Store) rawExec(q string, args ...any) (sql.Result, error) {
	return s.db.Exec(q, args...)
}

// Nanoid: crypto-random base62 string of length n (ids, not secrets).
func Nanoid(n int) string {
	const alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	out := make([]byte, 0, n)
	buf := make([]byte, n)
	for len(out) < n {
		if _, err := rand.Read(buf); err != nil {
			panic("crypto/rand unavailable: " + err.Error())
		}
		for _, c := range buf {
			// drop bytes >= 248 (62*4): no modulo bias
			if c < 248 && len(out) < n {
				out = append(out, alphabet[c%62])
			}
		}
	}
	return string(out)
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// nullScore: NULL if < 0 or no Method (zero-value Install).
func (i Install) nullScore(n int) any {
	if n < 0 || i.Method == "" {
		return nil
	}
	return n
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// timeLayout: matches SQLite strftime('%Y-%m-%dT%H:%M:%fZ') column defaults — fixed width, stored timestamps compare correctly as text.
const timeLayout = "2006-01-02T15:04:05.000Z"

// maxRollupEventName: event_days keeps names forever (no purge); long free-form names are cut so a name carrying device data cannot grow unbounded.
const maxRollupEventName = 64

func rollupEventName(e string) string {
	if r := []rune(e); len(r) > maxRollupEventName {
		return string(r[:maxRollupEventName])
	}
	return e
}

// day: UTC calendar day (YYYY-MM-DD), rollup + analytics bucket key.
func day(t time.Time) string {
	return t.UTC().Format("2006-01-02")
}

func rfc3339(t time.Time) string {
	return t.UTC().Format(timeLayout)
}

// parseTime: RFC3339 text stored by rfc3339 or SQLite defaults.
func parseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}
