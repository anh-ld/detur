// Package store persists detur state in SQLite (WAL mode).
package store

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"detur.dev/server/internal/ua"
)

//go:embed schema.sql
var schemaSQL string

const (
	AttributionOrganic    = "organic"
	AttributionNonOrganic = "non_organic"
	AttributionUnknown    = "unknown"
)

const (
	MethodPrior         = "prior"
	MethodClickID       = "click_id"
	MethodPasteboard    = "pasteboard"
	MethodProbabilistic = "probabilistic"
	MethodOrganic       = "organic"
	MethodUnknown       = "unknown"
)

var ErrNotFound = errors.New("not found")
var ErrKeyConflict = errors.New("short key already exists")
var ErrAmbiguousKey = errors.New("short key is ambiguous")

type Store struct {
	db           *sql.DB
	ClickIDHours int
}

func Open(path string) (*Store, error) {
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	db, err := sql.Open("sqlite", path+sep+"_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_size_limit(67108864)")
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	db.SetMaxOpenConns(1) // single writer, WAL readers still share the one conn
	fail := func(what string, err error) (*Store, error) {
		db.Close()
		return nil, fmt.Errorf("%s: %w", what, err)
	}
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		return fail("PRAGMA journal_mode=WAL", err)
	}
	if _, err := db.Exec(schemaSQL); err != nil {
		return fail("apply schema", err)
	}
	hadMatch := columnExists(db, "apps", "match_threshold")
	if err := migrateInstallUniqueness(db); err != nil {
		return fail("migrate installs", err)
	}
	if err := addMissingColumns(db); err != nil {
		return fail("migrate columns", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_clicks_platform_null ON clicks (created_at) WHERE platform IS NULL`); err != nil {
		return fail("index click platform", err)
	}
	if err := backfillClickPlatform(db); err != nil {
		return fail("backfill click platform", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_clicks_paste_token ON clicks (app_id, paste_token) WHERE paste_token IS NOT NULL`); err != nil {
		return fail("index paste_token", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_clicks_matched_expires ON clicks (expires_at) WHERE matched_at IS NOT NULL`); err != nil {
		return fail("index matched clicks", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_installs_app_link_created ON installs (app_id, link_id, created_at)`); err != nil {
		return fail("index installs link", err)
	}
	if !hadMatch {
		if err := seedAppMatchFromSettings(db); err != nil {
			return fail("seed app match settings", err)
		}
	}
	if _, err := db.Exec(schemaSQL); err != nil {
		return fail("re-apply schema", err)
	}
	return &Store{db: db}, nil
}

func backfillClickPlatform(db *sql.DB) error {
	rows, err := db.Query(`SELECT id, user_agent, kind FROM clicks WHERE platform IS NULL AND user_agent IS NOT NULL`)
	if err != nil {
		return err
	}
	byID := map[string]string{}
	for rows.Next() {
		var id, agent string
		var kind sql.NullString
		if err := rows.Scan(&id, &agent, &kind); err != nil {
			rows.Close()
			return err
		}
		if kind.String == KindOpen {
			byID[id] = ua.AppPlatform(agent)
		} else {
			byID[id] = ua.ClickPlatform(agent)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil || len(byID) == 0 {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for id, p := range byID {
		if _, err := tx.Exec(`UPDATE clicks SET platform = ? WHERE id = ?`, p, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) Close() error { return s.db.Close() }

// Ping: healthcheck. Reads the schema page: plain SELECT 1 never opens the db file, so a missing/corrupt db would pass.
func (s *Store) Ping() error {
	var n int
	return s.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master`).Scan(&n)
}

func HashKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

var AndroidPackageRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*(\.[A-Za-z][A-Za-z0-9_]*)+$`)

type App struct {
	ID                     string
	Name                   string
	APIKeyHash             string
	IOSAppID               string
	AndroidPackage         string
	AndroidCertFingerprint string
	IOSScheme              string
	MatchThreshold         int
	MatchWindowMinutes     int
	TagLinks               bool
	PageHTML               string
	RedirectProfiles       []RedirectProfile
	SocialPresets          []SocialPreset
}

type SocialPreset struct {
	Name        string `json:"name"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Image       string `json:"image,omitempty"`
}

type RedirectProfile struct {
	Name    string `json:"name"`
	IOS     string `json:"ios,omitempty"`
	Android string `json:"android,omitempty"`
	Web     string `json:"web,omitempty"`
}

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

const appCols = `id, name, api_key_hash, COALESCE(ios_app_id, ''), COALESCE(android_package, ''), COALESCE(android_cert_fingerprint, ''), COALESCE(ios_scheme, ''), match_threshold, match_window_minutes, tag_links, COALESCE(redirect_profiles, ''), COALESCE(social_presets, '')`

func scanApp(row rowScanner, page bool) (App, error) {
	var a App
	var profiles, social string
	dest := []any{&a.ID, &a.Name, &a.APIKeyHash, &a.IOSAppID, &a.AndroidPackage, &a.AndroidCertFingerprint, &a.IOSScheme, &a.MatchThreshold, &a.MatchWindowMinutes, &a.TagLinks, &profiles, &social}
	if page {
		dest = append(dest, &a.PageHTML)
	}
	if err := row.Scan(dest...); err != nil {
		return App{}, err
	}
	a.RedirectProfiles = decodeList[RedirectProfile](profiles)
	a.SocialPresets = decodeList[SocialPreset](social)
	return a, nil
}

func (s *Store) GetApp(id string) (App, error) {
	a, err := scanApp(s.db.QueryRow(`SELECT `+appCols+`, COALESCE(page_html, '') FROM apps WHERE id = ?`, id), true)
	if errors.Is(err, sql.ErrNoRows) {
		return App{}, ErrNotFound
	}
	return a, err
}

func (s *Store) AppTagLinks(id string) (bool, error) {
	var on bool
	err := s.db.QueryRow(`SELECT tag_links FROM apps WHERE id = ?`, id).Scan(&on)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrNotFound
	}
	return on, err
}

func (s *Store) AppName(id string) (string, error) {
	var name string
	err := s.db.QueryRow(`SELECT name FROM apps WHERE id = ?`, id).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return name, err
}

func (s *Store) CountIOSApps() (n int, err error) {
	err = s.db.QueryRow(`SELECT COUNT(*) FROM apps WHERE COALESCE(ios_app_id, '') != ''`).Scan(&n)
	return n, err
}

func (s *Store) CountLinks(appID string) (n int, err error) {
	err = s.db.QueryRow(`SELECT COUNT(*) FROM links WHERE app_id = ?`, appID).Scan(&n)
	return n, err
}

func (s *Store) LinkKeysByID(appID string) (map[string]string, error) {
	rows, err := s.db.Query(`SELECT id, key FROM links WHERE app_id = ?`, appID)
	if err != nil {
		return nil, fmt.Errorf("link keys: %w", err)
	}
	defer rows.Close()
	keys := map[string]string{}
	for rows.Next() {
		var id, key string
		if err := rows.Scan(&id, &key); err != nil {
			return nil, err
		}
		keys[id] = key
	}
	return keys, rows.Err()
}

func decodeList[T any](raw string) []T {
	if raw == "" {
		return nil
	}
	var out []T
	_ = json.Unmarshal([]byte(raw), &out)
	return out
}

func encodeList[T any](v []T) any {
	if len(v) == 0 {
		return nil
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func (s *Store) UpdateAppDetails(id, iosAppID, androidPackage, certFingerprint, iosScheme string) error {
	return execOne(s.db, "update app details", `UPDATE apps SET ios_app_id = ?, android_package = ?, android_cert_fingerprint = ?, ios_scheme = ? WHERE id = ?`,
		nullStr(iosAppID), nullStr(androidPackage), nullStr(certFingerprint), nullStr(iosScheme), id)
}

type AppDetails struct{ IOSAppID, AndroidPackage, AndroidCertFingerprint, IOSScheme string }

func (s *Store) UpdateApp(id string, d AppDetails, profiles []RedirectProfile, presets []SocialPreset) error {
	return execOne(s.db, "update app", `UPDATE apps SET ios_app_id = ?, android_package = ?, android_cert_fingerprint = ?, ios_scheme = ?, redirect_profiles = ?, social_presets = ? WHERE id = ?`,
		nullStr(d.IOSAppID), nullStr(d.AndroidPackage), nullStr(d.AndroidCertFingerprint), nullStr(d.IOSScheme), encodeList(profiles), encodeList(presets), id)
}

func (s *Store) UpdateAppPage(id, pageHTML string) error {
	return execOne(s.db, "update app page", `UPDATE apps SET page_html = ? WHERE id = ?`, nullStr(pageHTML), id)
}

func (s *Store) UpdateAppMatchSettings(id string, threshold, windowMinutes int) error {
	return execOne(s.db, "update app match settings", `UPDATE apps SET match_threshold = ?, match_window_minutes = ? WHERE id = ?`, threshold, windowMinutes, id)
}

func (s *Store) UpdateAppTagLinks(id string, on bool) error {
	return execOne(s.db, "update app tag links", `UPDATE apps SET tag_links = ? WHERE id = ?`, boolInt(on), id)
}

func (s *Store) DeleteApp(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("delete app: %w", err)
	}
	defer tx.Rollback()
	if err := execOne(tx, "delete app", `DELETE FROM apps WHERE id = ?`, id); err != nil {
		return err
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
	rows, err := s.db.Query(`SELECT ` + appCols + ` FROM apps ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("list apps: %w", err)
	}
	defer rows.Close()
	var apps []App
	for rows.Next() {
		a, err := scanApp(rows, false)
		if err != nil {
			return nil, err
		}
		apps = append(apps, a)
	}
	return apps, rows.Err()
}

func (s *Store) UpdateAppKey(id, apiKey string) error {
	return execOne(s.db, "update app key", `UPDATE apps SET api_key_hash = ? WHERE id = ?`, HashKey(apiKey), id)
}

func (s *Store) ClearAppKey(id string) error {
	return execOne(s.db, "clear app key", `UPDATE apps SET api_key_hash = '' WHERE id = ?`, id)
}

const sdkSeenEvery = 5 * time.Minute

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

type Link struct {
	ID                              string
	AppID                           string
	Key                             string
	URL                             string
	Title                           string
	IOS                             string
	Android                         string
	FallbackURL                     string
	ExpiresAt                       *time.Time
	ExpiredURL                      string
	OGTitle, OGDescription, OGImage string
	Tags                            []string
	RuleSummary                     string
	HasSplit                        bool
}

const linkCols = `id, app_id, key, url, COALESCE(title, ''), COALESCE(ios, ''), COALESCE(android, ''), COALESCE(fallback_url, ''), COALESCE(expires_at, ''), COALESCE(expired_url, ''),
  COALESCE(og_title, ''), COALESCE(og_description, ''), COALESCE(og_image, ''), COALESCE(tags, '')`

func (l Link) Expired(now time.Time) bool {
	return l.ExpiresAt != nil && now.After(*l.ExpiresAt)
}

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
		`INSERT INTO links (id, app_id, key, url, title, ios, android, fallback_url, expires_at, expired_url, og_title, og_description, og_image, tags)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		l.ID, l.AppID, l.Key, l.URL, nullStr(l.Title), nullStr(l.IOS), nullStr(l.Android), nullStr(l.FallbackURL),
		nullTime(l.ExpiresAt), nullStr(l.ExpiredURL), nullStr(l.OGTitle), nullStr(l.OGDescription), nullStr(l.OGImage), encodeList(l.Tags),
	)
	if err != nil {
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
		`SELECT `+linkCols+` FROM links WHERE app_id = ? AND key = ? COLLATE NOCASE`, appID, key,
	))
}

func (s *Store) GetLinkByKeyGlobal(key string) (Link, error) {
	rows, err := s.db.Query(
		`SELECT `+linkCols+` FROM links WHERE key = ? COLLATE NOCASE LIMIT 2`, key,
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
		`SELECT `+linkCols+` FROM links WHERE id = ?`, id,
	))
}

// MaxAASAKeys: per-app link-key cap for multi-app AASA enumeration; Apple drops oversized association files whole.
const MaxAASAKeys = 256

func (s *Store) ListLinks(appID string) ([]Link, error) {
	rows, err := s.db.Query(
		`SELECT `+linkCols+` FROM links WHERE app_id = ? ORDER BY created_at`, appID,
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
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	if err := s.attachRuleSummaries(links); err != nil {
		return nil, err
	}
	return links, nil
}

func (s *Store) attachRuleSummaries(links []Link) error {
	if len(links) == 0 {
		return nil
	}
	args := make([]any, len(links))
	marks := make([]string, len(links))
	for i, l := range links {
		args[i] = l.ID
		marks[i] = "?"
	}
	rows, err := s.db.Query(
		`SELECT link_id, name, action FROM link_rules WHERE link_id IN (`+strings.Join(marks, ",")+`) ORDER BY link_id, position`,
		args...,
	)
	if err != nil {
		return fmt.Errorf("list link rules: %w", err)
	}
	defer rows.Close()
	byLink := map[string][]LinkRule{}
	for rows.Next() {
		var linkID, name, action string
		if err := rows.Scan(&linkID, &name, &action); err != nil {
			return fmt.Errorf("scan link rule: %w", err)
		}
		var a RuleAction
		_ = json.Unmarshal([]byte(action), &a)
		byLink[linkID] = append(byLink[linkID], LinkRule{Name: name, Action: a})
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for i := range links {
		links[i].RuleSummary = summarizeRules(byLink[links[i].ID])
		for _, r := range byLink[links[i].ID] {
			if len(r.Action.Split) > 0 {
				links[i].HasSplit = true
			}
		}
	}
	return nil
}

func summarizeRules(rules []LinkRule) string {
	switch {
	case len(rules) == 0:
		return ""
	case len(rules) > 1:
		return fmt.Sprintf("%d rules", len(rules))
	}
	r := rules[0]
	if len(r.Action.Split) == 0 {
		return r.Name
	}
	weights := make([]string, len(r.Action.Split))
	for i, v := range r.Action.Split {
		weights[i] = strconv.Itoa(v.Weight)
	}
	return r.Name + " " + strings.Join(weights, "/")
}

func (s *Store) UpdateLink(l Link) error {
	return execOne(s.db, "update link", `UPDATE links SET url = ?, title = ?, ios = ?, android = ?, fallback_url = ?, expires_at = ?, expired_url = ?,
		   og_title = ?, og_description = ?, og_image = ?, tags = ?
		 WHERE id = ?`,
		l.URL, nullStr(l.Title), nullStr(l.IOS), nullStr(l.Android), nullStr(l.FallbackURL),
		nullTime(l.ExpiresAt), nullStr(l.ExpiredURL), nullStr(l.OGTitle), nullStr(l.OGDescription), nullStr(l.OGImage), encodeList(l.Tags), l.ID)
}

func (s *Store) DeleteLink(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("delete link: %w", err)
	}
	defer tx.Rollback()
	if err := execOne(tx, "delete link", `DELETE FROM links WHERE id = ?`, id); err != nil {
		return err
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
	var expiresAt, tags string
	if err := row.Scan(&l.ID, &l.AppID, &l.Key, &l.URL, &l.Title, &l.IOS, &l.Android, &l.FallbackURL, &expiresAt, &l.ExpiredURL,
		&l.OGTitle, &l.OGDescription, &l.OGImage, &tags); err != nil {
		return Link{}, err
	}
	if expiresAt != "" {
		t := parseTime(expiresAt)
		l.ExpiresAt = &t
	}
	l.Tags = decodeList[string](tags)
	return l, nil
}

func nullTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return rfc3339(*t)
}

type Fingerprint struct {
	IP         string
	Device     string
	Locale     string
	Timezone   string
	Screen     string
	UserAgent  string
	OSVersion  string
	PastedLink string
}

type Click struct {
	ID               string
	AppID            string
	LinkID           string
	Fingerprint      Fingerprint
	Destination      string
	ClickID          string
	IsBot            bool
	CreatedAt        time.Time
	ExpiresAt        time.Time
	Platform         string
	Kind             string
	Source           string
	FirstSeenAt      time.Time
	UASuspect        bool
	IPHosting        bool
	HitsIP, HitsLink int
	Variant          string
	PasteToken       string
}

const (
	KindApp  = "app"
	KindWeb  = "web"
	KindOpen = "open"
)

const clickDedupWindow = time.Hour

func (s *Store) RecordClick(c Click, retentionHours int) (Click, error) {
	return s.recordClickAt(c, retentionHours, time.Now().UTC())
}

func (s *Store) recordClickAt(c Click, retentionHours int, now time.Time) (Click, error) {
	expires := time.Duration(retentionHours) * time.Hour
	tx, err := s.db.Begin()
	if err != nil {
		return Click{}, fmt.Errorf("record click: %w", err)
	}
	defer tx.Rollback()
	c.Fingerprint.UserAgent = clip(c.Fingerprint.UserAgent, 1024)
	c.Fingerprint.Device = clip(c.Fingerprint.Device, 128)
	c.Fingerprint.OSVersion = clip(c.Fingerprint.OSVersion, 64)
	if len(c.Fingerprint.PastedLink) > MaxClickURL {
		c.Fingerprint.PastedLink, c.PasteToken = "", ""
	}
	c.HitsIP, c.HitsLink, err = bumpHits(tx, c, now)
	if err != nil {
		return Click{}, fmt.Errorf("record click hits: %w", err)
	}
	existing, err := scanClick(tx.QueryRow(
		`SELECT `+clickCols+` FROM clicks
		 WHERE link_id = ? AND ip IS ? AND COALESCE(user_agent, '') = ? AND created_at >= ? AND expires_at >= ? AND matched_at IS NULL
		 ORDER BY created_at DESC LIMIT 1`,
		c.LinkID, nullStr(c.Fingerprint.IP), c.Fingerprint.UserAgent, rfc3339(now.Add(-clickDedupWindow)), rfc3339(now),
	))
	if err == nil {
		return s.refreshClick(tx, existing, c, now, now.Add(expires))
	}
	if !errors.Is(err, ErrNotFound) {
		return Click{}, fmt.Errorf("record click dedup: %w", err)
	}
	if c.Kind != KindOpen && c.Source == "" && c.Fingerprint.IP != "" {
		existing, err = reopenCandidate(tx, c, now)
		if err == nil {
			c.Destination, c.Variant = "", ""
			return s.refreshClick(tx, existing, c, now, now.Add(expires))
		}
		if !errors.Is(err, ErrNotFound) {
			return Click{}, fmt.Errorf("record click reopen: %w", err)
		}
	}
	if c.ID == "" {
		c.ID = Nanoid(16)
	}
	if c.ClickID == "" {
		c.ClickID = c.ID
	}
	c.CreatedAt = now
	c.ExpiresAt = now.Add(expires)
	_, err = tx.Exec(
		`INSERT INTO clicks (rowid, id, app_id, link_id, ip, device, locale, timezone, screen, user_agent, os_version, pasted_link, destination, click_id, is_bot, created_at, expires_at,
		   kind, first_seen_at, ua_suspect, ip_hosting, hits_ip, hits_link, source, variant, paste_token, platform)
		 VALUES ((SELECT n + 1 FROM row_seq WHERE name = 'clicks'), ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ID, c.AppID, c.LinkID, nullStr(c.Fingerprint.IP), nullStr(c.Fingerprint.Device),
		nullStr(c.Fingerprint.Locale), nullStr(c.Fingerprint.Timezone), nullStr(c.Fingerprint.Screen),
		nullStr(c.Fingerprint.UserAgent), nullStr(c.Fingerprint.OSVersion), nullStr(c.Fingerprint.PastedLink), c.Destination,
		nullStr(c.ClickID), boolInt(c.IsBot), rfc3339(c.CreatedAt), rfc3339(c.ExpiresAt),
		nullStr(c.Kind), rfc3339(c.CreatedAt), boolInt(c.UASuspect), boolInt(c.IPHosting), c.HitsIP, c.HitsLink, nullStr(c.Source), nullStr(c.Variant),
		nullStr(c.PasteToken), nullStr(c.Platform),
	)
	if err != nil {
		return Click{}, fmt.Errorf("record click: %w", err)
	}
	if !c.UASuspect {
		if err := bumpClickDay(tx, c.AppID, c.LinkID, day(now), c.Platform, c.Kind); err != nil {
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
			if err := bumpVariantClick(tx, c.AppID, c.LinkID, c.Variant, day(now)); err != nil {
				return Click{}, fmt.Errorf("record click variant rollup: %w", err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return Click{}, fmt.Errorf("record click: %w", err)
	}
	c.CreatedAt = parseTime(rfc3339(c.CreatedAt))
	c.ExpiresAt = parseTime(rfc3339(c.ExpiresAt))
	c.FirstSeenAt = c.CreatedAt
	return c, nil
}

func bumpClickDay(tx *sql.Tx, appID, linkID, day, platform, kind string) error {
	_, err := tx.Exec(`INSERT INTO click_days (app_id, link_id, day, platform, kind, n) VALUES (?, ?, ?, ?, ?, 1)
		 ON CONFLICT DO UPDATE SET n = n + 1`, appID, linkID, day, platform, kind)
	return err
}

func bumpVariantClick(tx *sql.Tx, appID, linkID, variant, day string) error {
	_, err := tx.Exec(`INSERT INTO variant_days (app_id, link_id, variant, day, clicks, installs) VALUES (?, ?, ?, ?, 1, 0)
		 ON CONFLICT(app_id, link_id, variant, day) DO UPDATE SET clicks = clicks + 1`, appID, linkID, variant, day)
	return err
}

func bumpVariantInstall(tx *sql.Tx, appID, linkID, variant, day string) error {
	_, err := tx.Exec(`INSERT INTO variant_days (app_id, link_id, variant, day, clicks, installs) VALUES (?, ?, ?, ?, 0, 1)
		 ON CONFLICT(app_id, link_id, variant, day) DO UPDATE SET installs = installs + 1`, appID, linkID, variant, day)
	return err
}

func reopenCandidate(tx *sql.Tx, c Click, now time.Time) (Click, error) {
	rows, err := tx.Query(
		`SELECT `+clickCols+` FROM clicks
		 WHERE link_id = ? AND ip = ? AND COALESCE(source, '') != '' AND COALESCE(user_agent, '') != ? AND COALESCE(kind, '') != ?
		   AND created_at >= ? AND expires_at >= ? AND matched_at IS NULL
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

func (s *Store) refreshClick(tx *sql.Tx, old, c Click, now, expires time.Time) (Click, error) {
	var plat string
	refreshed, err := scanClick(tx.QueryRow(
		`UPDATE clicks SET created_at = ?, expires_at = MAX(expires_at, ?),
		   device = COALESCE(?, device), locale = COALESCE(?, locale), timezone = COALESCE(?, timezone),
		   screen = COALESCE(?, screen), os_version = COALESCE(?, os_version), pasted_link = COALESCE(?, pasted_link),
		   paste_token = COALESCE(?, paste_token),
		   destination = COALESCE(NULLIF(?, ''), destination),
		   variant = CASE WHEN ? = '' THEN variant ELSE ? END,
		   hits_ip = MAX(hits_ip, ?), hits_link = MAX(hits_link, ?)
		 WHERE id = ? RETURNING `+clickCols+`, COALESCE(platform, '')`,
		rfc3339(now), rfc3339(expires),
		nullStr(c.Fingerprint.Device), nullStr(c.Fingerprint.Locale), nullStr(c.Fingerprint.Timezone),
		nullStr(c.Fingerprint.Screen), nullStr(c.Fingerprint.OSVersion), nullStr(c.Fingerprint.PastedLink),
		nullStr(c.PasteToken),
		c.Destination, c.Destination, nullStr(c.Variant), c.HitsIP, c.HitsLink, old.ID,
	), &plat)
	if err != nil {
		return Click{}, fmt.Errorf("record click refresh: %w", err)
	}
	if od, nd := day(old.CreatedAt), day(now); od != nd && !old.UASuspect {
		res, err := tx.Exec(`UPDATE click_days SET n = n - 1 WHERE app_id = ? AND link_id = ? AND day = ? AND platform = ? AND kind = ?`,
			old.AppID, old.LinkID, od, plat, old.Kind)
		if err != nil {
			return Click{}, fmt.Errorf("record click refresh rollup: %w", err)
		}
		if n, _ := res.RowsAffected(); n > 0 {
			if _, err := tx.Exec(`DELETE FROM click_days WHERE n <= 0 AND app_id = ? AND link_id = ? AND day = ?`, old.AppID, old.LinkID, od); err != nil {
				return Click{}, fmt.Errorf("record click refresh rollup: %w", err)
			}
			if err := bumpClickDay(tx, old.AppID, old.LinkID, nd, plat, old.Kind); err != nil {
				return Click{}, fmt.Errorf("record click refresh rollup: %w", err)
			}
		}
	}
	if c.Destination != "" && c.Variant != "" && c.Variant != old.Variant && !c.UASuspect {
		if err := bumpVariantClick(tx, old.AppID, old.LinkID, c.Variant, day(now)); err != nil {
			return Click{}, fmt.Errorf("record click refresh variant rollup: %w", err)
		}
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

func (s *Store) ClickByClickID(appID, clickID, deviceHash string) (Click, error) {
	now := time.Now()
	return scanClick(s.db.QueryRow(
		`SELECT `+clickCols+` FROM clicks WHERE app_id = ? AND click_id = ? AND (expires_at >= ? OR created_at > ?)
		   AND (matched_at IS NULL OR matched_device = ?)`,
		appID, clickID, rfc3339(now), rfc3339(s.clickIDCutoff(now)), nullStr(deviceHash),
	))
}

func (s *Store) ClickByPasteToken(appID, key, token, deviceHash string) (Click, error) {
	now := time.Now()
	return scanClick(s.db.QueryRow(
		`SELECT `+clickColsC+` FROM clicks c JOIN links l ON l.id = c.link_id
		 WHERE c.app_id = ? AND l.key = ? COLLATE NOCASE AND c.paste_token = ? AND (c.expires_at >= ? OR c.created_at > ?)
		   AND (c.matched_at IS NULL OR c.matched_device = ?)
		 ORDER BY c.created_at DESC LIMIT 1`,
		appID, key, token, rfc3339(now), rfc3339(s.clickIDCutoff(now)), nullStr(deviceHash),
	))
}

const ParamPasteToken = "_dc"

const (
	ParamDone     = "_dt"
	ParamScreen   = "screen"
	ParamTimezone = "tz"
	ParamPasted   = "pasted_link"
	ParamNoTrack  = "detur-no-track"
	ParamTap      = "_tap"
	ParamTouch    = "_touch"
)

var InternalParams = map[string]bool{
	ParamDone: true, ParamScreen: true, ParamTimezone: true, ParamPasted: true, ParamNoTrack: true, ParamTap: true, ParamTouch: true, ParamPasteToken: true,
	LinkTag: true,
}

const pasteTokenLen = 16

var (
	rePasteToken      = regexp.MustCompile(fmt.Sprintf(`^[0-9A-Za-z]{%d}$`, pasteTokenLen))
	reTrailPasteToken = regexp.MustCompile(fmt.Sprintf(`[?&]%s=[0-9A-Za-z]{%d}$`, ParamPasteToken, pasteTokenLen))
)

func StripPasteToken(raw string) string {
	return reTrailPasteToken.ReplaceAllString(raw, "")
}

func PasteQuery() string {
	return "?" + ParamPasteToken + "=" + Nanoid(pasteTokenLen)
}

func PasteToken(raw string) (key, token string) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", ""
	}
	t := u.Query().Get(ParamPasteToken)
	if !rePasteToken.MatchString(t) {
		return "", ""
	}
	return strings.Trim(u.Path, "/"), t
}

func (s *Store) clickIDCutoff(now time.Time) time.Time {
	return now.Add(-time.Duration(s.ClickIDHours) * time.Hour)
}

func (s *Store) MarkClickMatched(id, deviceHash string) (bool, error) {
	res, err := s.db.Exec(`UPDATE clicks SET matched_at = ?, matched_device = ? WHERE id = ? AND matched_at IS NULL`,
		rfc3339(time.Now()), nullStr(deviceHash), id)
	if err != nil {
		return false, fmt.Errorf("mark click matched: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 1 {
		return true, nil
	}
	if deviceHash == "" {
		return false, nil
	}
	var one int
	err = s.db.QueryRow(`SELECT 1 FROM clicks WHERE id = ? AND matched_device = ?`, id, deviceHash).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("mark click matched: %w", err)
	}
	return true, nil
}

const RetryWindow = 15 * time.Minute

func (s *Store) PriorMatch(appID, deviceHash string) (Click, error) {
	now := time.Now()
	return scanClick(s.db.QueryRow(
		`SELECT `+clickColsC+` FROM clicks c JOIN installs i ON i.click_id = c.id
		 WHERE i.app_id = ? AND i.device_hash = ? AND i.attribution = ? AND c.expires_at >= ? AND i.created_at >= ?
		 ORDER BY i.created_at DESC LIMIT 1`,
		appID, deviceHash, AttributionNonOrganic, rfc3339(now), rfc3339(now.Add(-RetryWindow))))
}

// ponytail: older clicks past the cap can't match; raise it (or prefilter by IP in SQL) for apps above ~5k clicks per window.
const maxWindowClicks = 5000

func (s *Store) ClicksSince(appID, deviceHash string, since time.Time) ([]Click, error) {
	rows, err := s.db.Query(
		`SELECT `+clickCols+` FROM clicks WHERE app_id = ? AND is_bot = 0 AND (matched_at IS NULL OR (matched_device = ? AND matched_at >= ?)) AND created_at >= ? ORDER BY created_at DESC LIMIT ?`,
		appID, nullStr(deviceHash), rfc3339(time.Now().Add(-RetryWindow)), rfc3339(since), maxWindowClicks,
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

const purgeClickIDSelect = `SELECT rowid FROM clicks INDEXED BY idx_clicks_created WHERE created_at <= ? AND expires_at < ? LIMIT ?`

func (s *Store) PurgeExpired(now time.Time, retentionHours int) (int64, error) {
	var removed int64
	at := rfc3339(now)
	if _, err := s.batched(`UPDATE clicks SET ip = NULL, device = NULL, locale = NULL, timezone = NULL, screen = NULL,
		   user_agent = NULL, os_version = NULL, pasted_link = NULL
		 WHERE rowid IN (SELECT rowid FROM clicks WHERE expires_at < ? AND (ip IS NOT NULL OR user_agent IS NOT NULL) LIMIT ?)`, at); err != nil {
		return 0, fmt.Errorf("scrub clicks: %w", err)
	}
	// two deletes, each on its own index: one OR'd predicate walks every retained unmatched click (up to CLICK_ID_DAYS) per batch.
	// INDEXED BY: the planner otherwise picks idx_clicks_expires and walks every expired, retained click (no stats: no ANALYZE).
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`DELETE FROM clicks WHERE rowid IN (SELECT rowid FROM clicks WHERE matched_at IS NOT NULL AND expires_at < ? LIMIT ?)`, []any{at}},
		{`DELETE FROM clicks WHERE rowid IN (` + purgeClickIDSelect + `)`, []any{rfc3339(s.clickIDCutoff(now)), at}},
		{`DELETE FROM events WHERE rowid IN (SELECT rowid FROM events WHERE created_at < ? LIMIT ?)`, []any{rfc3339(now.Add(-time.Duration(retentionHours) * time.Hour))}},
	} {
		n, err := s.batched(q.sql, q.args...)
		if err != nil {
			return 0, fmt.Errorf("purge: %w", err)
		}
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

const purgeBatch = 2000

func (s *Store) batched(q string, args ...any) (int64, error) {
	var total int64
	for {
		res, err := s.db.Exec(q, append(args, purgeBatch)...)
		if err != nil {
			return total, err
		}
		n, _ := res.RowsAffected()
		total += n
		if n < purgeBatch {
			return total, nil
		}
	}
}

func scanClick(row rowScanner, extra ...any) (Click, error) {
	c, err := scanClickRows(row, extra...)
	if errors.Is(err, sql.ErrNoRows) {
		return Click{}, ErrNotFound
	}
	return c, err
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanClickRows(row rowScanner, extra ...any) (Click, error) {
	var (
		c                                                                Click
		ip, device, locale, timezone, screen, ua, osVer, pasted, clickID string
		createdAt, expiresAt, firstSeen                                  string
	)
	err := row.Scan(append([]any{&c.ID, &c.AppID, &c.LinkID, &ip, &device, &locale, &timezone,
		&screen, &ua, &osVer, &pasted, &c.Destination, &clickID, &c.IsBot, &createdAt, &expiresAt,
		&c.Kind, &firstSeen, &c.UASuspect, &c.IPHosting, &c.HitsIP, &c.HitsLink, &c.Source, &c.Variant}, extra...)...)
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
	LinkID      string
	Platform    string
	Method      string
	Score       int
	RunnerUp    int
	Fraud       string
	FraudAction string
	FraudLinkID string
	Variant     string
}

func (s *Store) RecordInstall(i Install) (Install, error) {
	if i.ID == "" {
		i.ID = Nanoid(16)
	}
	now := time.Now().UTC()
	i.CreatedAt = now
	tx, err := s.db.Begin()
	if err != nil {
		return Install{}, fmt.Errorf("record install: %w", err)
	}
	defer tx.Rollback()
	if i.Attribution != AttributionUnknown {
		if _, err := tx.Exec(`DELETE FROM installs WHERE app_id = ? AND device_hash = ? AND click_id = ? AND attribution = ?`,
			i.AppID, i.DeviceHash, i.ClickID, AttributionUnknown); err != nil {
			return Install{}, fmt.Errorf("record install replace unknown: %w", err)
		}
	}
	var clickID any = i.ClickID
	if i.Attribution == AttributionOrganic && i.ClickID == "" {
		var id string
		err := tx.QueryRow(`SELECT id FROM installs WHERE app_id = ? AND device_hash = ? AND COALESCE(click_id, '') = '' AND attribution = ? AND created_at >= ?
			ORDER BY created_at DESC LIMIT 1`, i.AppID, i.DeviceHash, AttributionOrganic, rfc3339(now.Add(-RetryWindow))).Scan(&id)
		switch {
		case err == nil:
			return s.commitInstall(tx, id)
		case !errors.Is(err, sql.ErrNoRows):
			return Install{}, fmt.Errorf("record install retry check: %w", err)
		}
		clickID = nil
	}
	res, err := tx.Exec(
		`INSERT INTO installs (rowid, id, app_id, device_hash, click_id, attribution, created_at, link_id, platform, method, score, runner_up, fraud, fraud_action, fraud_link_id, variant)
		 VALUES ((SELECT n + 1 FROM row_seq WHERE name = 'installs'), ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			 ON CONFLICT(app_id, device_hash, click_id) DO UPDATE SET attribution = excluded.attribution,
			   link_id = excluded.link_id, platform = excluded.platform,
			   method = excluded.method, score = excluded.score, runner_up = excluded.runner_up,
			   fraud = excluded.fraud, fraud_action = excluded.fraud_action, fraud_link_id = excluded.fraud_link_id,
			   variant = COALESCE(NULLIF(excluded.variant, ''), installs.variant)
			 WHERE installs.attribution = ?`,
		i.ID, i.AppID, i.DeviceHash, clickID, i.Attribution, rfc3339(now), nullStr(i.LinkID), nullStr(i.Platform),
		nullStr(i.Method), i.nullScore(i.Score), i.nullScore(i.RunnerUp), nullStr(i.Fraud), nullStr(i.FraudAction), nullStr(i.FraudLinkID), nullStr(i.Variant), AttributionUnknown,
	)
	if err != nil {
		return Install{}, fmt.Errorf("record install: %w", err)
	}
	n, _ := res.RowsAffected()
	if n > 0 && i.Variant != "" && i.LinkID != "" && i.Attribution != AttributionOrganic && i.Attribution != AttributionUnknown {
		if err := bumpVariantInstall(tx, i.AppID, i.LinkID, i.Variant, day(now)); err != nil {
			return Install{}, fmt.Errorf("record install variant rollup: %w", err)
		}
	}
	id := i.ID
	if clickID != nil {
		if err := tx.QueryRow(`SELECT id FROM installs WHERE app_id = ? AND device_hash = ? AND click_id = ?`,
			i.AppID, i.DeviceHash, i.ClickID).Scan(&id); err != nil {
			return Install{}, fmt.Errorf("record install readback: %w", err)
		}
	}
	return s.commitInstall(tx, id)
}

func (s *Store) commitInstall(tx *sql.Tx, id string) (Install, error) {
	var (
		got       Install
		createdAt string
		variant   string
	)
	err := tx.QueryRow(
		`SELECT id, app_id, device_hash, COALESCE(click_id, ''), attribution, created_at, COALESCE(variant, '')
		 FROM installs WHERE id = ?`, id,
	).Scan(&got.ID, &got.AppID, &got.DeviceHash, &got.ClickID, &got.Attribution, &createdAt, &variant)
	if err != nil {
		return Install{}, fmt.Errorf("record install readback: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Install{}, fmt.Errorf("record install commit: %w", err)
	}
	got.CreatedAt = parseTime(createdAt)
	got.Variant = variant
	return got, nil
}

func addMissingColumns(db *sql.DB) error {
	for _, c := range []struct{ table, column, decl string }{
		{"links", "expires_at", "TEXT"},
		{"links", "expired_url", "TEXT"},
		{"clicks", "os_version", "TEXT"},
		{"clicks", "matched_at", "TEXT"},
		{"clicks", "matched_device", "TEXT"},
		{"apps", "match_threshold", "INTEGER NOT NULL DEFAULT 850"},
		{"apps", "match_window_minutes", "INTEGER NOT NULL DEFAULT 15"},
		{"installs", "link_id", "TEXT"},
		{"installs", "platform", "TEXT"},
		{"installs", "method", "TEXT"},
		{"installs", "score", "INTEGER"},
		{"installs", "runner_up", "INTEGER"},
		{"apps", "redirect_profiles", "TEXT"},
		{"apps", "social_presets", "TEXT"},
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
		{"clicks", "paste_token", "TEXT"},
		{"links", "og_title", "TEXT"},
		{"links", "og_description", "TEXT"},
		{"links", "og_image", "TEXT"},
		{"apps", "ios_scheme", "TEXT"},
		{"apps", "page_html", "TEXT"},
		{"links", "title", "TEXT"},
		{"links", "tags", "TEXT"},
		{"clicks", "platform", "TEXT"},
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

func seedAppMatchFromSettings(db *sql.DB) error {
	if !tableExists(db, "settings") {
		return nil
	}
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
		`INSERT INTO events (rowid, id, app_id, event, metadata) VALUES ((SELECT n + 1 FROM row_seq WHERE name = 'events'), ?, ?, ?, ?)`,
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

type execer interface {
	Exec(string, ...any) (sql.Result, error)
}

func execOne(db execer, what, q string, args ...any) error {
	res, err := db.Exec(q, args...)
	if err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func Nanoid(n int) string {
	const alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	out := make([]byte, 0, n)
	buf := make([]byte, n)
	for len(out) < n {
		if _, err := rand.Read(buf); err != nil {
			panic("crypto/rand unavailable: " + err.Error())
		}
		for _, c := range buf {
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

// timeLayout: = SQLite strftime('%Y-%m-%dT%H:%M:%fZ') column defaults; fixed width -> text compare works.
const timeLayout = "2006-01-02T15:04:05.000Z"

const maxRollupEventName = 64

func rollupEventName(e string) string {
	if r := []rune(e); len(r) > maxRollupEventName {
		return string(r[:maxRollupEventName])
	}
	return e
}

const MaxClickURL = 4096

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.ToValidUTF8(s[:n], "")
}

func day(t time.Time) string {
	return t.UTC().Format("2006-01-02")
}

func rfc3339(t time.Time) string {
	return t.UTC().Format(timeLayout)
}

func parseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}
