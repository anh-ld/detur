// Package store persists detur state in SQLite (WAL mode).
//
// Attribution values: organic | non_organic | unknown (R7, R3 backend-error
// path). Clicks carry expires_at = max(configured window, retention floor);
// deterministic clickId lookups are exempt from the window filter (documented
// Detour contract: deterministic match has no window).
package store

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite" // pure-Go driver (KTD1): no cgo, static builds
)

//go:embed schema.sql
var schemaSQL string

const (
	// AttributionOrganic marks an install with no matched click.
	AttributionOrganic = "organic"
	// AttributionNonOrganic marks an install matched to a click.
	AttributionNonOrganic = "non_organic"
	// AttributionUnknown marks a backend-error path (R3): logged, not surfaced in readout.
	AttributionUnknown = "unknown"
)

// ErrNotFound is returned when a requested row does not exist.
var ErrNotFound = errors.New("not found")

// Store wraps a SQLite database.
type Store struct {
	db *sql.DB
}

// Open opens (creating if needed) the SQLite database at path.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	db.SetMaxOpenConns(1) // single writer; WAL readers still share the one conn
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA foreign_keys=ON",
		"PRAGMA busy_timeout=5000",
	} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("%s: %w", pragma, err)
		}
	}
	if _, err := db.Exec(schemaSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	return &Store{db: db}, nil
}

// Close closes the underlying database.
func (s *Store) Close() error { return s.db.Close() }

// HashKey returns the SHA-256 hex digest of an API key (hash-at-rest, R14).
func HashKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

// App is a registered application holding links and API keys.
type App struct {
	ID         string
	Name       string
	APIKeyHash string
}

// CreateApp inserts an app and returns it. apiKey is returned to the caller
// only once by the portal; only its hash is stored.
func (s *Store) CreateApp(name, apiKey string) (App, error) {
	id := nanoid(21)
	hash := HashKey(apiKey)
	if _, err := s.db.Exec(
		`INSERT INTO apps (id, name, api_key_hash) VALUES (?, ?, ?)`, id, name, hash,
	); err != nil {
		return App{}, fmt.Errorf("create app: %w", err)
	}
	return App{ID: id, Name: name, APIKeyHash: hash}, nil
}

// ListApps returns all apps.
func (s *Store) ListApps() ([]App, error) {
	rows, err := s.db.Query(`SELECT id, name, api_key_hash FROM apps ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("list apps: %w", err)
	}
	defer rows.Close()
	var apps []App
	for rows.Next() {
		var a App
		if err := rows.Scan(&a.ID, &a.Name, &a.APIKeyHash); err != nil {
			return nil, err
		}
		apps = append(apps, a)
	}
	return apps, rows.Err()
}

// ValidateAPIKey reports whether the key matches the app's stored hash.
func (s *Store) ValidateAPIKey(appID, apiKey string) (bool, error) {
	var hash string
	err := s.db.QueryRow(`SELECT api_key_hash FROM apps WHERE id = ?`, appID).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("validate api key: %w", err)
	}
	return hash == HashKey(apiKey), nil
}

// Link is a short link under an app.
type Link struct {
	ID            string
	AppID         string
	Key           string
	URL           string
	IOS           string
	Android       string
	FallbackURL   string
	Threshold     int // per-link match threshold (R14 settings)
	WindowMinutes int // per-link match window (R14 settings)
}

const (
	defaultThreshold = 850
	defaultWindowMin = 15
)

// CreateLink inserts a link, applying per-link defaults.
func (s *Store) CreateLink(l Link) (Link, error) {
	if l.ID == "" {
		l.ID = nanoid(16)
	}
	if l.Threshold == 0 {
		l.Threshold = defaultThreshold
	}
	if l.WindowMinutes == 0 {
		l.WindowMinutes = defaultWindowMin
	}
	_, err := s.db.Exec(
		`INSERT INTO links (id, app_id, key, url, ios, android, fallback_url, threshold, window_minutes)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		l.ID, l.AppID, l.Key, l.URL, nullStr(l.IOS), nullStr(l.Android), nullStr(l.FallbackURL),
		l.Threshold, l.WindowMinutes,
	)
	if err != nil {
		return Link{}, fmt.Errorf("create link: %w", err)
	}
	return l, nil
}

// GetLinkByKey returns the link with the given key under the app.
func (s *Store) GetLinkByKey(appID, key string) (Link, error) {
	return scanLink(s.db.QueryRow(
		`SELECT id, app_id, key, url, COALESCE(ios, ''), COALESCE(android, ''), COALESCE(fallback_url, ''), threshold, window_minutes
		 FROM links WHERE app_id = ? AND key = ?`, appID, key,
	))
}

// GetLink returns the link by id.
func (s *Store) GetLink(id string) (Link, error) {
	return scanLink(s.db.QueryRow(
		`SELECT id, app_id, key, url, COALESCE(ios, ''), COALESCE(android, ''), COALESCE(fallback_url, ''), threshold, window_minutes
		 FROM links WHERE id = ?`, id,
	))
}

// ListLinks returns all links under the app.
func (s *Store) ListLinks(appID string) ([]Link, error) {
	rows, err := s.db.Query(
		`SELECT id, app_id, key, url, COALESCE(ios, ''), COALESCE(android, ''), COALESCE(fallback_url, ''), threshold, window_minutes
		 FROM links WHERE app_id = ? ORDER BY created_at`, appID,
	)
	if err != nil {
		return nil, fmt.Errorf("list links: %w", err)
	}
	defer rows.Close()
	var links []Link
	for rows.Next() {
		var l Link
		if err := rows.Scan(&l.ID, &l.AppID, &l.Key, &l.URL, &l.IOS, &l.Android, &l.FallbackURL, &l.Threshold, &l.WindowMinutes); err != nil {
			return nil, err
		}
		links = append(links, l)
	}
	return links, rows.Err()
}

// UpdateLink updates mutable link fields.
func (s *Store) UpdateLink(l Link) error {
	res, err := s.db.Exec(
		`UPDATE links SET url = ?, ios = ?, android = ?, fallback_url = ?, threshold = ?, window_minutes = ?
		 WHERE id = ?`,
		l.URL, nullStr(l.IOS), nullStr(l.Android), nullStr(l.FallbackURL), l.Threshold, l.WindowMinutes, l.ID,
	)
	if err != nil {
		return fmt.Errorf("update link: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteLink removes a link.
func (s *Store) DeleteLink(id string) error {
	res, err := s.db.Exec(`DELETE FROM links WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete link: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func scanLink(row *sql.Row) (Link, error) {
	var l Link
	err := row.Scan(&l.ID, &l.AppID, &l.Key, &l.URL, &l.IOS, &l.Android, &l.FallbackURL, &l.Threshold, &l.WindowMinutes)
	if errors.Is(err, sql.ErrNoRows) {
		return Link{}, ErrNotFound
	}
	if err != nil {
		return Link{}, err
	}
	return l, nil
}

// Fingerprint is the device signal set captured at click time (R8: IP,
// device, locale, timezone, screen, user-agent, pasted link) and matched
// against the first-launch fingerprint.
type Fingerprint struct {
	IP         string
	Device     string
	Locale     string
	Timezone   string
	Screen     string
	UserAgent  string
	PastedLink string
}

// Click is a recorded click on a short link.
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
}

// RecordClick persists a click. Expiry = max(configured window, retention
// floor) so deterministic lookups survive beyond the probabilistic window
// (KTD4, U1).
func (s *Store) RecordClick(c Click, windowMinutes, retentionHours int) (Click, error) {
	window := time.Duration(windowMinutes) * time.Minute
	floor := time.Duration(retentionHours) * time.Hour
	if window < floor {
		window = floor
	}
	now := time.Now().UTC()
	if c.ID == "" {
		c.ID = nanoid(16)
	}
	c.CreatedAt = now
	c.ExpiresAt = now.Add(window)
	_, err := s.db.Exec(
		`INSERT INTO clicks (id, app_id, link_id, ip, device, locale, timezone, screen, user_agent, pasted_link, destination, click_id, is_bot, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ID, c.AppID, c.LinkID, nullStr(c.Fingerprint.IP), nullStr(c.Fingerprint.Device),
		nullStr(c.Fingerprint.Locale), nullStr(c.Fingerprint.Timezone), nullStr(c.Fingerprint.Screen),
		nullStr(c.Fingerprint.UserAgent), nullStr(c.Fingerprint.PastedLink), c.Destination,
		nullStr(c.ClickID), boolInt(c.IsBot), rfc3339(c.ExpiresAt),
	)
	if err != nil {
		return Click{}, fmt.Errorf("record click: %w", err)
	}
	return c, nil
}

// GetClick returns a click by id.
func (s *Store) GetClick(id string) (Click, error) {
	return scanClick(s.db.QueryRow(
		`SELECT id, app_id, link_id, COALESCE(ip, ''), COALESCE(device, ''), COALESCE(locale, ''), COALESCE(timezone, ''), COALESCE(screen, ''), COALESCE(user_agent, ''), COALESCE(pasted_link, ''), destination, COALESCE(click_id, ''), is_bot, created_at, expires_at
		 FROM clicks WHERE id = ?`, id,
	))
}

// ClickByClickID finds a click by deterministic clickId (no window filter —
// deterministic matching has no window; retention floor governs expiry).
func (s *Store) ClickByClickID(appID, clickID string) (Click, error) {
	return scanClick(s.db.QueryRow(
		`SELECT id, app_id, link_id, COALESCE(ip, ''), COALESCE(device, ''), COALESCE(locale, ''), COALESCE(timezone, ''), COALESCE(screen, ''), COALESCE(user_agent, ''), COALESCE(pasted_link, ''), destination, COALESCE(click_id, ''), is_bot, created_at, expires_at
		 FROM clicks WHERE app_id = ? AND click_id = ?`, appID, clickID,
	))
}

// ClicksSince returns non-bot clicks created at or after since (window scan).
func (s *Store) ClicksSince(appID string, since time.Time) ([]Click, error) {
	rows, err := s.db.Query(
		`SELECT id, app_id, link_id, COALESCE(ip, ''), COALESCE(device, ''), COALESCE(locale, ''), COALESCE(timezone, ''), COALESCE(screen, ''), COALESCE(user_agent, ''), COALESCE(pasted_link, ''), destination, COALESCE(click_id, ''), is_bot, created_at, expires_at
		 FROM clicks WHERE app_id = ? AND is_bot = 0 AND created_at >= ? ORDER BY created_at DESC`,
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

// PurgeExpired deletes clicks past their expiry and returns the count removed.
func (s *Store) PurgeExpired(now time.Time) (int64, error) {
	res, err := s.db.Exec(`DELETE FROM clicks WHERE expires_at < ?`, rfc3339(now))
	if err != nil {
		return 0, fmt.Errorf("purge expired: %w", err)
	}
	return res.RowsAffected()
}

func scanClick(row *sql.Row) (Click, error) {
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
		c                                                         Click
		ip, device, locale, timezone, screen, ua, pasted, clickID string
		createdAt, expiresAt                                      string
	)
	err := row.Scan(&c.ID, &c.AppID, &c.LinkID, &ip, &device, &locale, &timezone,
		&screen, &ua, &pasted, &c.Destination, &clickID, &c.IsBot, &createdAt, &expiresAt)
	if err != nil {
		return Click{}, err
	}
	c.Fingerprint = Fingerprint{IP: ip, Device: device, Locale: locale, Timezone: timezone, Screen: screen, UserAgent: ua, PastedLink: pasted}
	c.ClickID = clickID
	c.CreatedAt = parseTime(createdAt)
	c.ExpiresAt = parseTime(expiresAt)
	return c, nil
}

// Install is an install attribution record.
type Install struct {
	ID          string
	AppID       string
	DeviceHash  string
	ClickID     string
	Attribution string
	CreatedAt   time.Time
}

// RecordInstall upserts an install attribution idempotently on
// (device_hash, click_id) — duplicate match-link calls never double-count
// (U3, R7). Empty click_id marks organic/unknown installs; the unique
// constraint then dedupes retried organic installs per device too.
func (s *Store) RecordInstall(i Install) (Install, error) {
	if i.ID == "" {
		i.ID = nanoid(16)
	}
	now := time.Now().UTC()
	i.CreatedAt = now
	_, err := s.db.Exec(
		`INSERT INTO installs (id, app_id, device_hash, click_id, attribution, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(device_hash, click_id) DO NOTHING`,
		i.ID, i.AppID, i.DeviceHash, i.ClickID, i.Attribution, rfc3339(now),
	)
	if err != nil {
		return Install{}, fmt.Errorf("record install: %w", err)
	}
	// Return the stored row (the surviving row on conflict).
	var (
		got       Install
		createdAt string
	)
	err = s.db.QueryRow(
		`SELECT id, app_id, device_hash, click_id, attribution, created_at
		 FROM installs WHERE device_hash = ? AND click_id = ?`,
		i.DeviceHash, i.ClickID,
	).Scan(&got.ID, &got.AppID, &got.DeviceHash, &got.ClickID, &got.Attribution, &createdAt)
	if err != nil {
		return Install{}, fmt.Errorf("record install readback: %w", err)
	}
	got.CreatedAt = parseTime(createdAt)
	return got, nil
}

// CountInstalls returns organic and non-organic install counts for an app
// (R15 readout; unknown excluded).
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

// CountClicks returns the click count for an app (R15 readout).
func (s *Store) CountClicks(appID string) (int64, error) {
	var n int64
	err := s.db.QueryRow(`SELECT COUNT(*) FROM clicks WHERE app_id = ?`, appID).Scan(&n)
	return n, err
}

// RecordEvent persists an analytics event.
func (s *Store) RecordEvent(appID, event, metadata string) error {
	_, err := s.db.Exec(
		`INSERT INTO events (id, app_id, event, metadata) VALUES (?, ?, ?, ?)`,
		nanoid(21), appID, event, nullStr(metadata),
	)
	if err != nil {
		return fmt.Errorf("record event: %w", err)
	}
	return nil
}

// CountEvents returns the analytics event count for an app (U3 persistence check).
func (s *Store) CountEvents(appID string) (int64, error) {
	var n int64
	err := s.db.QueryRow(`SELECT COUNT(*) FROM events WHERE app_id = ?`, appID).Scan(&n)
	return n, err
}

// GetSetting returns a stored setting value.
func (s *Store) GetSetting(key string) (string, bool, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return v, true, nil
}

// SetSetting stores a setting value.
func (s *Store) SetSetting(key, value string) error {
	_, err := s.db.Exec(
		`INSERT INTO settings (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, value,
	)
	if err != nil {
		return fmt.Errorf("set setting: %w", err)
	}
	return nil
}

// rawExec executes a statement directly (test-only hook for expiry/purge setup).
func (s *Store) rawExec(q string, args ...any) (sql.Result, error) {
	return s.db.Exec(q, args...)
}

// nanoid returns a crypto-random base62 string of length n (ids, not secrets).
func nanoid(n int) string {
	const alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand unavailable: " + err.Error())
	}
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return string(b)
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func rfc3339(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

// parseTime parses RFC3339 text stored by rfc3339.
func parseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}
