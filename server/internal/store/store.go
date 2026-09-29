// Package store persists detur state in SQLite (WAL mode).
//
// Attribution values: organic | non_organic | unknown (backend-error path).
// Clicks carry expires_at = max(configured window, retention floor);
// deterministic clickId lookups skip the window filter (deterministic match
// has no window).
package store

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite" // pure-Go driver, no cgo, static builds
)

//go:embed schema.sql
var schemaSQL string

const (
	// AttributionOrganic marks an install with no matched click.
	AttributionOrganic = "organic"
	// AttributionNonOrganic marks an install matched to a click.
	AttributionNonOrganic = "non_organic"
	// AttributionUnknown marks a backend-error path: logged, not surfaced in readout.
	AttributionUnknown = "unknown"
)

// ErrNotFound is returned when a requested row does not exist.
var ErrNotFound = errors.New("not found")
var ErrKeyConflict = errors.New("short key already exists")
var ErrAmbiguousKey = errors.New("short key is ambiguous")

// Store wraps a SQLite database.
type Store struct {
	db *sql.DB
}

// Open opens (creating if needed) the SQLite database at path.
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
	if err := addMissingColumns(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate columns: %w", err)
	}
	if err := migrateInstallUniqueness(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate installs: %w", err)
	}
	return &Store{db: db}, nil
}

// Close closes the underlying database.
func (s *Store) Close() error { return s.db.Close() }

// Ping verifies the database answers a trivial query (healthcheck probe).
func (s *Store) Ping() error {
	var one int
	return s.db.QueryRow(`SELECT 1`).Scan(&one)
}

// Matching defaults, applied when no settings are stored; the portal edits
// threshold/window through the settings table.
const (
	DefaultThreshold     = 850
	DefaultWindowMinutes = 15
	RetentionFloorHours  = 24
)

// HashKey returns the SHA-256 hex digest of an API key (hash-at-rest).
func HashKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

// App is a registered application holding links and API keys. Trailing
// fields carry the well-known hosting details: iOS appID
// (TEAMID.BUNDLEID), Android package + SHA-256 cert fingerprint.
type App struct {
	ID                     string
	Name                   string
	APIKeyHash             string
	IOSAppID               string
	AndroidPackage         string
	AndroidCertFingerprint string
}

// CreateApp inserts an app and returns it. apiKey returns to the caller
// only once, from the portal; only its hash is stored.
func (s *Store) CreateApp(name, apiKey string) (App, error) {
	id := Nanoid(21)
	hash := HashKey(apiKey)
	if _, err := s.db.Exec(
		`INSERT INTO apps (id, name, api_key_hash) VALUES (?, ?, ?)`, id, name, hash,
	); err != nil {
		return App{}, fmt.Errorf("create app: %w", err)
	}
	return App{ID: id, Name: name, APIKeyHash: hash}, nil
}

// GetApp returns the app by id, including the well-known hosting details.
func (s *Store) GetApp(id string) (App, error) {
	var a App
	err := s.db.QueryRow(
		`SELECT id, name, api_key_hash, COALESCE(ios_app_id, ''), COALESCE(android_package, ''), COALESCE(android_cert_fingerprint, '')
		 FROM apps WHERE id = ?`, id,
	).Scan(&a.ID, &a.Name, &a.APIKeyHash, &a.IOSAppID, &a.AndroidPackage, &a.AndroidCertFingerprint)
	if errors.Is(err, sql.ErrNoRows) {
		return App{}, ErrNotFound
	}
	if err != nil {
		return App{}, err
	}
	return a, nil
}

// UpdateAppDetails sets the app's well-known hosting details. Empty strings
// clear the corresponding field (nullable columns). ErrNotFound when the app
// is unknown.
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

// DeleteApp removes an app; its links cascade (schema ON DELETE CASCADE).
func (s *Store) DeleteApp(id string) error {
	res, err := s.db.Exec(`DELETE FROM apps WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete app: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ListApps returns all apps.
func (s *Store) ListApps() ([]App, error) {
	rows, err := s.db.Query(
		`SELECT id, name, api_key_hash, COALESCE(ios_app_id, ''), COALESCE(android_package, ''), COALESCE(android_cert_fingerprint, '')
		 FROM apps ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("list apps: %w", err)
	}
	defer rows.Close()
	var apps []App
	for rows.Next() {
		var a App
		if err := rows.Scan(&a.ID, &a.Name, &a.APIKeyHash, &a.IOSAppID, &a.AndroidPackage, &a.AndroidCertFingerprint); err != nil {
			return nil, err
		}
		apps = append(apps, a)
	}
	return apps, rows.Err()
}

// UpdateAppKey replaces the app's API key hash (rotation); the previous key
// stops working immediately.
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

// ClearAppKey removes the app's API key (revocation): SDK auth then fails.
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
	Threshold     int        // per-link match threshold
	WindowMinutes int        // per-link match window
	ExpiresAt     *time.Time // nil = never expires
	ExpiredURL    string     // redirect target once expired; empty = 410
}

// Expired: link past its expiry.
func (l Link) Expired(now time.Time) bool {
	return l.ExpiresAt != nil && now.After(*l.ExpiresAt)
}

// CreateLink inserts a link. Threshold/window 0 = "unset": the matching
// engine and click expiry then fall back to the global settings; a link
// overrides only when explicitly set.
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
		`INSERT INTO links (id, app_id, key, url, ios, android, fallback_url, threshold, window_minutes, expires_at, expired_url)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		l.ID, l.AppID, l.Key, l.URL, nullStr(l.IOS), nullStr(l.Android), nullStr(l.FallbackURL),
		l.Threshold, l.WindowMinutes, nullTime(l.ExpiresAt), nullStr(l.ExpiredURL),
	)
	if err != nil {
		// An unknown app_id hits the FK; the portal treats it as 404.
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

// GetLinkByKey returns the link with the given key under the app.
func (s *Store) GetLinkByKey(appID, key string) (Link, error) {
	return scanLink(s.db.QueryRow(
		`SELECT id, app_id, key, url, COALESCE(ios, ''), COALESCE(android, ''), COALESCE(fallback_url, ''), threshold, window_minutes, COALESCE(expires_at, ''), COALESCE(expired_url, '')
		 FROM links WHERE app_id = ? AND key = ? COLLATE NOCASE`, appID, key,
	))
}

// GetLinkByKeyGlobal resolves a short key across all apps. Historic duplicates
// fail closed rather than silently sending traffic to the wrong app.
func (s *Store) GetLinkByKeyGlobal(key string) (Link, error) {
	rows, err := s.db.Query(
		`SELECT id, app_id, key, url, COALESCE(ios, ''), COALESCE(android, ''), COALESCE(fallback_url, ''), threshold, window_minutes, COALESCE(expires_at, ''), COALESCE(expired_url, '')
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

// GetLink returns the link by id.
func (s *Store) GetLink(id string) (Link, error) {
	return scanLink(s.db.QueryRow(
		`SELECT id, app_id, key, url, COALESCE(ios, ''), COALESCE(android, ''), COALESCE(fallback_url, ''), threshold, window_minutes, COALESCE(expires_at, ''), COALESCE(expired_url, '')
		 FROM links WHERE id = ?`, id,
	))
}

// ListLinks returns all links under the app.
func (s *Store) ListLinks(appID string) ([]Link, error) {
	rows, err := s.db.Query(
		`SELECT id, app_id, key, url, COALESCE(ios, ''), COALESCE(android, ''), COALESCE(fallback_url, ''), threshold, window_minutes, COALESCE(expires_at, ''), COALESCE(expired_url, '')
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

// UpdateLink updates mutable link fields.
func (s *Store) UpdateLink(l Link) error {
	res, err := s.db.Exec(
		`UPDATE links SET url = ?, ios = ?, android = ?, fallback_url = ?, threshold = ?, window_minutes = ?, expires_at = ?, expired_url = ?
		 WHERE id = ?`,
		l.URL, nullStr(l.IOS), nullStr(l.Android), nullStr(l.FallbackURL), l.Threshold, l.WindowMinutes,
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
	l, err := scanLinkRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Link{}, ErrNotFound
	}
	return l, err
}

func scanLinkRow(row rowScanner) (Link, error) {
	var l Link
	var expiresAt string
	if err := row.Scan(&l.ID, &l.AppID, &l.Key, &l.URL, &l.IOS, &l.Android, &l.FallbackURL, &l.Threshold, &l.WindowMinutes, &expiresAt, &l.ExpiredURL); err != nil {
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

// Fingerprint is the device signal set captured at click time (IP, device,
// locale, timezone, screen, user-agent, pasted link) and matched against the
// first-launch fingerprint.
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

// clickDedupWindow: mirrors Dub's recordClickCache — one click per link and
// device (IP + user agent) per hour.
const clickDedupWindow = time.Hour

// RecordClick: persist a click, or return the existing click when the same
// device (IP + user agent) clicked the same link within the last hour (Dub
// record-click-cache) — repeat taps keep one clickId. Expiry = max(configured
// window, retention floor), so deterministic lookups survive beyond the
// probabilistic window.
func (s *Store) RecordClick(c Click, windowMinutes, retentionHours int) (Click, error) {
	window := time.Duration(windowMinutes) * time.Minute
	floor := time.Duration(retentionHours) * time.Hour
	if window < floor {
		window = floor
	}
	now := time.Now().UTC()
	tx, err := s.db.Begin()
	if err != nil {
		return Click{}, fmt.Errorf("record click: %w", err)
	}
	defer tx.Rollback()
	existing, err := scanClick(tx.QueryRow(
		`SELECT `+clickCols+` FROM clicks
		 WHERE link_id = ? AND COALESCE(ip, '') = ? AND COALESCE(user_agent, '') = ? AND created_at >= ? AND expires_at >= ?
		 ORDER BY created_at DESC LIMIT 1`,
		c.LinkID, c.Fingerprint.IP, c.Fingerprint.UserAgent, rfc3339(now.Add(-clickDedupWindow)), rfc3339(now),
	))
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return Click{}, fmt.Errorf("record click dedup: %w", err)
	}
	if c.ID == "" {
		c.ID = Nanoid(16)
	}
	// The id handed to clients (rec.ID, Android Play referrer, universal-link
	// clickId) is the deterministic match key: default click_id to it so
	// ClickByClickID can resolve what the SDK sends back.
	if c.ClickID == "" {
		c.ClickID = c.ID
	}
	c.CreatedAt = now
	c.ExpiresAt = now.Add(window)
	_, err = tx.Exec(
		`INSERT INTO clicks (id, app_id, link_id, ip, device, locale, timezone, screen, user_agent, os_version, pasted_link, destination, click_id, is_bot, created_at, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ID, c.AppID, c.LinkID, nullStr(c.Fingerprint.IP), nullStr(c.Fingerprint.Device),
		nullStr(c.Fingerprint.Locale), nullStr(c.Fingerprint.Timezone), nullStr(c.Fingerprint.Screen),
		nullStr(c.Fingerprint.UserAgent), nullStr(c.Fingerprint.OSVersion), nullStr(c.Fingerprint.PastedLink), c.Destination,
		nullStr(c.ClickID), boolInt(c.IsBot), rfc3339(c.CreatedAt), rfc3339(c.ExpiresAt),
	)
	if err != nil {
		return Click{}, fmt.Errorf("record click: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Click{}, fmt.Errorf("record click: %w", err)
	}
	c.CreatedAt = parseTime(rfc3339(c.CreatedAt)) // stored precision
	c.ExpiresAt = parseTime(rfc3339(c.ExpiresAt))
	return c, nil
}

const clickCols = `id, app_id, link_id, COALESCE(ip, ''), COALESCE(device, ''), COALESCE(locale, ''), COALESCE(timezone, ''), COALESCE(screen, ''), COALESCE(user_agent, ''), COALESCE(os_version, ''), COALESCE(pasted_link, ''), destination, COALESCE(click_id, ''), is_bot, created_at, expires_at`

// GetClick returns a click by id.
func (s *Store) GetClick(id string) (Click, error) {
	return scanClick(s.db.QueryRow(
		`SELECT `+clickCols+` FROM clicks WHERE id = ?`, id,
	))
}

// ClickByClickID finds a click by deterministic clickId (no window filter:
// deterministic matching has no window; the retention floor governs expiry,
// so expired-but-unpurged rows no longer resolve).
func (s *Store) ClickByClickID(appID, clickID string) (Click, error) {
	return scanClick(s.db.QueryRow(
		`SELECT `+clickCols+` FROM clicks WHERE app_id = ? AND click_id = ? AND expires_at >= ?`, appID, clickID, rfc3339(time.Now()),
	))
}

// ClicksSince returns non-bot clicks created at or after since (window scan).
func (s *Store) ClicksSince(appID string, since time.Time) ([]Click, error) {
	rows, err := s.db.Query(
		`SELECT `+clickCols+` FROM clicks WHERE app_id = ? AND is_bot = 0 AND created_at >= ? ORDER BY created_at DESC`,
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

// LinkThresholds returns per-link matching thresholds as a map from link id
// to threshold; links without an explicit threshold are absent.
func (s *Store) LinkThresholds(appID string) (map[string]int, error) {
	rows, err := s.db.Query(`SELECT id, threshold FROM links WHERE app_id = ?`, appID)
	if err != nil {
		return nil, fmt.Errorf("link thresholds: %w", err)
	}
	defer rows.Close()
	byLink := map[string]int{}
	for rows.Next() {
		var id string
		var t int
		if err := rows.Scan(&id, &t); err != nil {
			return nil, err
		}
		byLink[id] = t
	}
	return byLink, rows.Err()
}

// LinkWindows returns per-link match windows; zero means use the global value.
func (s *Store) LinkWindows(appID string) (map[string]int, error) {
	rows, err := s.db.Query(`SELECT id, window_minutes FROM links WHERE app_id = ?`, appID)
	if err != nil {
		return nil, fmt.Errorf("link windows: %w", err)
	}
	defer rows.Close()
	byLink := map[string]int{}
	for rows.Next() {
		var id string
		var minutes int
		if err := rows.Scan(&id, &minutes); err != nil {
			return nil, err
		}
		byLink[id] = minutes
	}
	return byLink, rows.Err()
}

// PurgeExpired deletes clicks past their expiry and events older than the
// retention floor, returning the total rows removed. Retention governs both
// tables; events get the same treatment.
func (s *Store) PurgeExpired(now time.Time, retentionHours int) (int64, error) {
	var removed int64
	res, err := s.db.Exec(`DELETE FROM clicks WHERE expires_at < ?`, rfc3339(now))
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
		createdAt, expiresAt                                             string
	)
	err := row.Scan(&c.ID, &c.AppID, &c.LinkID, &ip, &device, &locale, &timezone,
		&screen, &ua, &osVer, &pasted, &c.Destination, &clickID, &c.IsBot, &createdAt, &expiresAt)
	if err != nil {
		return Click{}, err
	}
	c.Fingerprint = Fingerprint{IP: ip, Device: device, Locale: locale, Timezone: timezone, Screen: screen, UserAgent: ua, OSVersion: osVer, PastedLink: pasted}
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

// RecordInstall upserts an install attribution idempotently per app, device,
// and click. Empty click_id marks organic/unknown installs and is deduped per
// app and device too.
func (s *Store) RecordInstall(i Install) (Install, error) {
	if i.ID == "" {
		i.ID = Nanoid(16)
	}
	now := time.Now().UTC()
	i.CreatedAt = now
	_, err := s.db.Exec(
		`INSERT INTO installs (id, app_id, device_hash, click_id, attribution, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)
			 ON CONFLICT(app_id, device_hash, click_id) DO NOTHING`,
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
		 FROM installs WHERE app_id = ? AND device_hash = ? AND click_id = ?`,
		i.AppID, i.DeviceHash, i.ClickID,
	).Scan(&got.ID, &got.AppID, &got.DeviceHash, &got.ClickID, &got.Attribution, &createdAt)
	if err != nil {
		return Install{}, fmt.Errorf("record install readback: %w", err)
	}
	got.CreatedAt = parseTime(createdAt)
	return got, nil
}

// SetSettings persists both matching defaults in one transaction.
func (s *Store) SetSettings(threshold, windowMinutes *string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for key, value := range map[string]*string{"threshold": threshold, "window_minutes": windowMinutes} {
		if value == nil {
			continue
		}
		if _, err := tx.Exec(`INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, *value); err != nil {
			return fmt.Errorf("set settings: %w", err)
		}
	}
	return tx.Commit()
}

// addMissingColumns: columns introduced after v0.1, added to existing
// databases (CREATE TABLE IF NOT EXISTS leaves old tables untouched).
func addMissingColumns(db *sql.DB) error {
	for _, c := range []struct{ table, column, decl string }{
		{"links", "expires_at", "TEXT"},
		{"links", "expired_url", "TEXT"},
		{"clicks", "os_version", "TEXT"},
	} {
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?`, c.table, c.column).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			if _, err := db.Exec(`ALTER TABLE ` + c.table + ` ADD COLUMN ` + c.column + ` ` + c.decl); err != nil {
				return err
			}
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

// CountInstalls returns organic and non-organic install counts for an app
// (readout; unknown excluded).
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

// CountClicks returns the click count for an app (readout).
func (s *Store) CountClicks(appID string) (int64, error) {
	var n int64
	err := s.db.QueryRow(`SELECT COUNT(*) FROM clicks WHERE app_id = ?`, appID).Scan(&n)
	return n, err
}

// RecordEvent persists an analytics event.
func (s *Store) RecordEvent(appID, event, metadata string) error {
	_, err := s.db.Exec(
		`INSERT INTO events (id, app_id, event, metadata) VALUES (?, ?, ?, ?)`,
		Nanoid(21), appID, event, nullStr(metadata),
	)
	if err != nil {
		return fmt.Errorf("record event: %w", err)
	}
	return nil
}

// CountEvents returns the analytics event count for an app.
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

// IntSetting returns a settings value parsed as an int, falling back to def
// when the key is missing or unparsable. Only a store read error is
// returned.
func (s *Store) IntSetting(key string, def int) (int, error) {
	v, ok, err := s.GetSetting(key)
	if err != nil {
		return 0, err
	}
	if !ok {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def, nil
	}
	return n, nil
}

// rawExec executes a statement directly (test-only hook for expiry/purge setup).
func (s *Store) rawExec(q string, args ...any) (sql.Result, error) {
	return s.db.Exec(q, args...)
}

// Nanoid returns a crypto-random base62 string of length n (ids, not secrets).
func Nanoid(n int) string {
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

// timeLayout: matches SQLite strftime('%Y-%m-%dT%H:%M:%fZ') column defaults
// — fixed width, stored timestamps compare correctly as text.
const timeLayout = "2006-01-02T15:04:05.000Z"

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
