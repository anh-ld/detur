package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"detur.dev/server/internal/ua"
)

// Webhook: external destination streaming app activity via signed HTTP POST requests.
type Webhook struct {
	ID                  string
	AppID               string
	URL                 string
	Secret              string
	Types               []string
	CursorInstalls      int64
	CursorClicks        int64
	CursorEvents        int64
	FailsInstalls       int
	FailsClicks         int
	FailsEvents         int
	BackoffInstalls     *time.Time
	BackoffClicks       *time.Time
	BackoffEvents       *time.Time
	ReplayUntilInstalls int64
	ReplayUntilClicks   int64
	ReplayUntilEvents   int64
	Enabled             bool
	CreatedAt           time.Time
}

// WebhookStream: delivery state of one stream (installs, clicks or events) of a webhook.
type WebhookStream struct {
	Cursor      int64
	Fails       int
	Backoff     *time.Time
	ReplayUntil int64
}

// Stream returns the delivery state for stream t; ok is false for an unknown stream.
func (w Webhook) Stream(t string) (ws WebhookStream, ok bool) {
	switch t {
	case "installs":
		return WebhookStream{w.CursorInstalls, w.FailsInstalls, w.BackoffInstalls, w.ReplayUntilInstalls}, true
	case "clicks":
		return WebhookStream{w.CursorClicks, w.FailsClicks, w.BackoffClicks, w.ReplayUntilClicks}, true
	case "events":
		return WebhookStream{w.CursorEvents, w.FailsEvents, w.BackoffEvents, w.ReplayUntilEvents}, true
	}
	return WebhookStream{}, false
}

// webhookStreamCol: column suffix for stream t. Only these literals ever reach SQL.
func webhookStreamCol(t string) (string, error) {
	switch t {
	case "installs", "clicks", "events":
		return t, nil
	}
	return "", fmt.Errorf("unknown event type %q", t)
}

// WebhookRecord: single record fetched for delivery with internal rowid.
type WebhookRecord struct {
	RowID   int64
	Payload any
}

// WebhookInstallPayload: PII-safe install event. device_hash is strictly excluded.
type WebhookInstallPayload struct {
	Type        string  `json:"type"` // "install"
	ID          string  `json:"id"`
	AppID       string  `json:"appId"`
	ClickID     *string `json:"clickId,omitempty"`
	Attribution string  `json:"attribution"`
	LinkID      *string `json:"linkId,omitempty"`
	LinkKey     *string `json:"linkKey,omitempty"`
	Platform    *string `json:"platform,omitempty"`
	Method      *string `json:"method,omitempty"`
	Score       *int    `json:"score,omitempty"`
	RunnerUp    *int    `json:"runnerUp,omitempty"`
	Fraud       *string `json:"fraud,omitempty"`
	FraudAction *string `json:"fraudAction,omitempty"`
	Variant     *string `json:"variant,omitempty"`
	CreatedAt   string  `json:"createdAt"`
}

// WebhookClickPayload: PII-safe click event. ip, user_agent, pasted_link are strictly excluded.
type WebhookClickPayload struct {
	Type        string  `json:"type"` // "click"
	ID          string  `json:"id"`
	AppID       string  `json:"appId"`
	LinkID      string  `json:"linkId"`
	LinkKey     *string `json:"linkKey,omitempty"`
	Destination string  `json:"destination"`
	ClickID     *string `json:"clickId,omitempty"`
	Kind        string  `json:"kind"`
	Platform    *string `json:"platform,omitempty"`
	Device      *string `json:"device,omitempty"`
	OSVersion   *string `json:"osVersion,omitempty"`
	Locale      *string `json:"locale,omitempty"`
	Timezone    *string `json:"timezone,omitempty"`
	Screen      *string `json:"screen,omitempty"`
	Source      *string `json:"source,omitempty"` // in-app browser (messenger, zalo, ..., unknown-inapp)
	Variant     *string `json:"variant,omitempty"`
	IsBot       bool    `json:"isBot"`
	CreatedAt   string  `json:"createdAt"`
}

// WebhookEventPayload: custom analytics event.
type WebhookEventPayload struct {
	Type      string `json:"type"` // "event"
	ID        string `json:"id"`
	AppID     string `json:"appId"`
	Event     string `json:"event"`
	Metadata  any    `json:"metadata,omitempty"`
	CreatedAt string `json:"createdAt"`
}

// GenerateWebhookSecret: generates a 32-byte cryptographically secure random secret.
func GenerateWebhookSecret() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand unavailable: " + err.Error())
	}
	return "whsec_" + hex.EncodeToString(b)
}

// CreateWebhook: inserts new webhook endpoint for app. Initial cursors are set to current max rowids.
func (s *Store) CreateWebhook(appID, webhookURL string, eventTypes []string) (Webhook, error) {
	if _, err := s.GetApp(appID); err != nil {
		return Webhook{}, err
	}

	id := "wh_" + Nanoid(20)
	secret := GenerateWebhookSecret()
	typesStr := strings.Join(eventTypes, ",")

	// Start at the current tail: a failed read must not leave a cursor at 0 (would replay all history).
	var cursorInstalls, cursorClicks, cursorEvents int64
	for table, dst := range map[string]*int64{"installs": &cursorInstalls, "clicks": &cursorClicks, "events": &cursorEvents} {
		if err := s.db.QueryRow(`SELECT COALESCE(MAX(rowid), 0) FROM `+table+` WHERE app_id = ?`, appID).Scan(dst); err != nil {
			return Webhook{}, fmt.Errorf("create webhook: %s cursor: %w", table, err)
		}
	}

	now := time.Now().UTC()
	createdAtStr := rfc3339(now)

	_, err := s.db.Exec(`
		INSERT INTO webhooks (
			id, app_id, url, secret, types,
			cursor_installs, cursor_clicks, cursor_events,
			fails_installs, fails_clicks, fails_events,
			enabled, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0, 0, 0, 1, ?)`,
		id, appID, webhookURL, secret, typesStr,
		cursorInstalls, cursorClicks, cursorEvents,
		createdAtStr,
	)
	if err != nil {
		return Webhook{}, fmt.Errorf("create webhook: %w", err)
	}

	return s.GetWebhook(id)
}

func (s *Store) scanWebhook(row rowScanner) (Webhook, error) {
	var (
		w                                    Webhook
		typesStr, createdAtStr               string
		backoffInst, backoffClick, backoffEv sql.NullString
		enabledInt                           int
	)
	err := row.Scan(
		&w.ID, &w.AppID, &w.URL, &w.Secret, &typesStr,
		&w.CursorInstalls, &w.CursorClicks, &w.CursorEvents,
		&w.FailsInstalls, &w.FailsClicks, &w.FailsEvents,
		&backoffInst, &backoffClick, &backoffEv,
		&w.ReplayUntilInstalls, &w.ReplayUntilClicks, &w.ReplayUntilEvents,
		&enabledInt, &createdAtStr,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Webhook{}, ErrNotFound
	}
	if err != nil {
		return Webhook{}, err
	}

	w.Enabled = enabledInt == 1
	if typesStr != "" {
		w.Types = strings.Split(typesStr, ",")
	} else {
		w.Types = []string{}
	}

	w.CreatedAt = parseTime(createdAtStr)
	w.BackoffInstalls = timePtr(backoffInst)
	w.BackoffClicks = timePtr(backoffClick)
	w.BackoffEvents = timePtr(backoffEv)

	return w, nil
}

const webhookSelectCols = `id, app_id, url, secret, types,
	cursor_installs, cursor_clicks, cursor_events,
	fails_installs, fails_clicks, fails_events,
	backoff_installs, backoff_clicks, backoff_events,
	replay_until_installs, replay_until_clicks, replay_until_events,
	enabled, created_at`

// GetWebhook returns a webhook by id.
func (s *Store) GetWebhook(id string) (Webhook, error) {
	row := s.db.QueryRow(`SELECT `+webhookSelectCols+` FROM webhooks WHERE id = ?`, id)
	return s.scanWebhook(row)
}

// ListWebhooks returns all webhooks configured for an app.
func (s *Store) ListWebhooks(appID string) ([]Webhook, error) {
	list, err := s.queryWebhooks(`WHERE app_id = ?`, appID)
	if err != nil {
		return nil, fmt.Errorf("list webhooks: %w", err)
	}
	return list, nil
}

// ListActiveWebhooks returns all enabled webhooks across all apps.
func (s *Store) ListActiveWebhooks() ([]Webhook, error) {
	list, err := s.queryWebhooks(`WHERE enabled = 1`)
	if err != nil {
		return nil, fmt.Errorf("list active webhooks: %w", err)
	}
	return list, nil
}

func (s *Store) queryWebhooks(where string, args ...any) ([]Webhook, error) {
	rows, err := s.db.Query(`SELECT `+webhookSelectCols+` FROM webhooks `+where+` ORDER BY created_at ASC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []Webhook
	for rows.Next() {
		w, err := s.scanWebhook(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, w)
	}
	return list, rows.Err()
}

// UpdateWebhook updates url, types, or enabled status.
func (s *Store) UpdateWebhook(id string, url *string, types []string, enabled *bool) (Webhook, error) {
	current, err := s.GetWebhook(id)
	if err != nil {
		return Webhook{}, err
	}

	newURL := current.URL
	if url != nil {
		newURL = *url
	}
	newTypesStr := strings.Join(current.Types, ",")
	if types != nil {
		newTypesStr = strings.Join(types, ",")
	}
	newEnabled := current.Enabled
	if enabled != nil {
		newEnabled = *enabled
	}

	_, err = s.db.Exec(`UPDATE webhooks SET url = ?, types = ?, enabled = ? WHERE id = ?`,
		newURL, newTypesStr, boolInt(newEnabled), id)
	if err != nil {
		return Webhook{}, fmt.Errorf("update webhook: %w", err)
	}

	return s.GetWebhook(id)
}

// DeleteWebhook deletes a webhook by id.
func (s *Store) DeleteWebhook(id string) error {
	res, err := s.db.Exec(`DELETE FROM webhooks WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete webhook: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// RotateWebhookSecret generates and stores a new secret for a webhook.
func (s *Store) RotateWebhookSecret(id string) (string, error) {
	newSecret := GenerateWebhookSecret()
	res, err := s.db.Exec(`UPDATE webhooks SET secret = ? WHERE id = ?`, newSecret, id)
	if err != nil {
		return "", fmt.Errorf("rotate webhook secret: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return "", ErrNotFound
	}
	return newSecret, nil
}

// ErrWebhookCursorMoved: the stream's cursor or replay window changed since the worker read it (e.g. a replay); the write was skipped.
var ErrWebhookCursorMoved = errors.New("webhook cursor moved")

// UpdateWebhookCursor advances cursor and clears backoff/failure state for a successful batch.
// Compare-and-set against prev (the state the batch was read with), so a concurrent replay is never overwritten.
func (s *Store) UpdateWebhookCursor(id, eventType string, prev WebhookStream, newCursor int64, isReplayComplete bool) error {
	col, err := webhookStreamCol(eventType)
	if err != nil {
		return err
	}
	set := `cursor_` + col + ` = ?, fails_` + col + ` = 0, backoff_` + col + ` = NULL`
	if isReplayComplete {
		set += `, replay_until_` + col + ` = 0`
	}
	res, err := s.db.Exec(`UPDATE webhooks SET `+set+` WHERE id = ? AND cursor_`+col+` = ? AND replay_until_`+col+` = ?`,
		newCursor, id, prev.Cursor, prev.ReplayUntil)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrWebhookCursorMoved
	}
	return nil
}

// WebhookStreamMaxRowID: highest rowid in the stream's table across all apps (0 when empty).
// A cursor above it means a purge lowered the table's rowid ceiling, so new rows may reuse rowids at or below the cursor.
func (s *Store) WebhookStreamMaxRowID(eventType string) (int64, error) {
	col, err := webhookStreamCol(eventType)
	if err != nil {
		return 0, err
	}
	var n int64
	err = s.db.QueryRow(`SELECT COALESCE(MAX(rowid), 0) FROM ` + col).Scan(&n)
	return n, err
}

// RecordWebhookFailure increments failure count and sets backoff delay for the given stream.
func (s *Store) RecordWebhookFailure(id, eventType string, fails int, backoffUntil time.Time) error {
	col, err := webhookStreamCol(eventType)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`UPDATE webhooks SET fails_`+col+` = ?, backoff_`+col+` = ? WHERE id = ?`,
		fails, backoffUntil.UTC().Format(time.RFC3339), id)
	return err
}

// ReplayWebhook resets cursors to replay records created at or after from.
// Purged clicks/events are simply gone, so retention needs no separate clamp.
func (s *Store) ReplayWebhook(id string, from time.Time) error {
	wh, err := s.GetWebhook(id)
	if err != nil {
		return err
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var tail, target [3]int64
	for i, table := range []string{"installs", "clicks", "events"} {
		if tail[i], target[i], err = replayCursor(tx, table, wh.AppID, rfc3339(from)); err != nil {
			return fmt.Errorf("replay %s: %w", table, err)
		}
	}

	_, err = tx.Exec(`
		UPDATE webhooks SET
			cursor_installs = ?, fails_installs = 0, backoff_installs = NULL, replay_until_installs = ?,
			cursor_clicks = ?, fails_clicks = 0, backoff_clicks = NULL, replay_until_clicks = ?,
			cursor_events = ?, fails_events = 0, backoff_events = NULL, replay_until_events = ?
		WHERE id = ?`,
		target[0], tail[0],
		target[1], tail[1],
		target[2], tail[2],
		id,
	)
	if err != nil {
		return fmt.Errorf("reset replay cursors: %w", err)
	}

	return tx.Commit()
}

// replayCursor returns table's current max rowid for the app and the cursor that re-delivers rows created at or after from.
// table is one of the fixed stream table names, never user input.
func replayCursor(tx *sql.Tx, table, appID, from string) (tail, target int64, err error) {
	if err = tx.QueryRow(`SELECT COALESCE(MAX(rowid), 0) FROM `+table+` WHERE app_id = ?`, appID).Scan(&tail); err != nil {
		return 0, 0, err
	}
	err = tx.QueryRow(`SELECT COALESCE(MIN(rowid) - 1, ?) FROM `+table+` WHERE app_id = ? AND created_at >= ?`,
		tail, appID, from).Scan(&target)
	return tail, max(target, 0), err
}

// FetchPendingWebhookBatch returns up to limit records for an app and stream where cursor < rowid <= until.
// until 0 means no upper bound; a replay passes its replay_until so replayed and live rows never share a batch.
func (s *Store) FetchPendingWebhookBatch(appID, eventType string, cursor, until int64, limit int) ([]WebhookRecord, error) {
	if limit <= 0 {
		limit = 100
	}
	if until <= 0 {
		until = math.MaxInt64
	}

	switch eventType {
	case "installs":
		return s.fetchInstallsBatch(appID, cursor, until, limit)
	case "clicks":
		return s.fetchClicksBatch(appID, cursor, until, limit)
	case "events":
		return s.fetchEventsBatch(appID, cursor, until, limit)
	default:
		return nil, fmt.Errorf("unknown event type %q", eventType)
	}
}

func (s *Store) fetchInstallsBatch(appID string, cursor, until int64, limit int) ([]WebhookRecord, error) {
	rows, err := s.db.Query(`
		SELECT i.rowid, i.id, i.app_id, i.click_id, i.attribution, i.created_at,
		       i.link_id, l.key, i.platform, i.method, i.score, i.runner_up,
		       i.fraud, i.fraud_action, i.variant
		FROM installs i
		LEFT JOIN links l ON l.id = i.link_id
		WHERE i.app_id = ? AND i.rowid > ? AND i.rowid <= ?
		ORDER BY i.rowid ASC
		LIMIT ?`, appID, cursor, until, limit)
	if err != nil {
		return nil, fmt.Errorf("fetch installs batch: %w", err)
	}
	defer rows.Close()

	var records []WebhookRecord
	for rows.Next() {
		var (
			rowID                                                                   int64
			id, aID, attr, createdAt                                                string
			clickID, linkID, linkKey, platform, method, fraud, fraudAction, variant sql.NullString
			score, runnerUp                                                         sql.NullInt64
		)
		if err := rows.Scan(
			&rowID, &id, &aID, &clickID, &attr, &createdAt,
			&linkID, &linkKey, &platform, &method, &score, &runnerUp,
			&fraud, &fraudAction, &variant,
		); err != nil {
			return nil, err
		}

		payload := WebhookInstallPayload{
			Type:        "install",
			ID:          id,
			AppID:       aID,
			Attribution: attr,
			CreatedAt:   createdAt,
		}
		payload.ClickID = nonEmpty(clickID)
		payload.LinkID = nonEmpty(linkID)
		payload.LinkKey = nonEmpty(linkKey)
		payload.Platform = nonEmpty(platform)
		payload.Method = nonEmpty(method)
		if score.Valid {
			n := int(score.Int64)
			payload.Score = &n
		}
		if runnerUp.Valid {
			n := int(runnerUp.Int64)
			payload.RunnerUp = &n
		}
		payload.Fraud = nonEmpty(fraud)
		payload.FraudAction = nonEmpty(fraudAction)
		payload.Variant = nonEmpty(variant)

		records = append(records, WebhookRecord{RowID: rowID, Payload: payload})
	}
	return records, rows.Err()
}

func (s *Store) fetchClicksBatch(appID string, cursor, until int64, limit int) ([]WebhookRecord, error) {
	rows, err := s.db.Query(`
		SELECT c.rowid, c.id, c.app_id, c.link_id, l.key, c.device, c.locale,
		       c.timezone, c.screen, c.os_version, c.destination, c.click_id,
		       c.is_bot, c.created_at, c.kind, c.user_agent, c.source, c.variant
		FROM clicks c
		LEFT JOIN links l ON l.id = c.link_id
		WHERE c.app_id = ? AND c.rowid > ? AND c.rowid <= ?
		ORDER BY c.rowid ASC
		LIMIT ?`, appID, cursor, until, limit)
	if err != nil {
		return nil, fmt.Errorf("fetch clicks batch: %w", err)
	}
	defer rows.Close()

	var records []WebhookRecord
	for rows.Next() {
		var (
			rowID                                                                                           int64
			id, aID, linkID, destination, createdAt                                                         string
			linkKey, device, locale, timezone, screen, osVersion, clickID, kind, userAgent, source, variant sql.NullString
			isBot                                                                                           int
		)
		if err := rows.Scan(
			&rowID, &id, &aID, &linkID, &linkKey, &device, &locale,
			&timezone, &screen, &osVersion, &destination, &clickID,
			&isBot, &createdAt, &kind, &userAgent, &source, &variant,
		); err != nil {
			return nil, err
		}

		kindStr := ""
		if kind.Valid {
			kindStr = kind.String
		}

		payload := WebhookClickPayload{
			Type:        "click",
			ID:          id,
			AppID:       aID,
			LinkID:      linkID,
			Destination: destination,
			Kind:        kindStr,
			IsBot:       isBot == 1,
			CreatedAt:   createdAt,
		}
		payload.LinkKey = nonEmpty(linkKey)
		payload.ClickID = nonEmpty(clickID)
		payload.Device = nonEmpty(device)
		// Platform derives from the UA; the UA itself is PII-excluded and never serialized.
		if p := ua.Platform(userAgent.String); p != "" {
			payload.Platform = &p
		}
		payload.OSVersion = nonEmpty(osVersion)
		payload.Locale = nonEmpty(locale)
		payload.Timezone = nonEmpty(timezone)
		payload.Screen = nonEmpty(screen)
		payload.Source = nonEmpty(source)
		payload.Variant = nonEmpty(variant)

		records = append(records, WebhookRecord{RowID: rowID, Payload: payload})
	}
	return records, rows.Err()
}

func (s *Store) fetchEventsBatch(appID string, cursor, until int64, limit int) ([]WebhookRecord, error) {
	rows, err := s.db.Query(`
		SELECT e.rowid, e.id, e.app_id, e.event, e.metadata, e.created_at
		FROM events e
		WHERE e.app_id = ? AND e.rowid > ? AND e.rowid <= ?
		ORDER BY e.rowid ASC
		LIMIT ?`, appID, cursor, until, limit)
	if err != nil {
		return nil, fmt.Errorf("fetch events batch: %w", err)
	}
	defer rows.Close()

	var records []WebhookRecord
	for rows.Next() {
		var (
			rowID                     int64
			id, aID, event, createdAt string
			metadata                  sql.NullString
		)
		if err := rows.Scan(&rowID, &id, &aID, &event, &metadata, &createdAt); err != nil {
			return nil, err
		}

		payload := WebhookEventPayload{
			Type:      "event",
			ID:        id,
			AppID:     aID,
			Event:     event,
			CreatedAt: createdAt,
		}
		if metadata.Valid && strings.TrimSpace(metadata.String) != "" {
			var parsed any
			if err := json.Unmarshal([]byte(metadata.String), &parsed); err == nil {
				payload.Metadata = parsed
			} else {
				payload.Metadata = metadata.String
			}
		}

		records = append(records, WebhookRecord{RowID: rowID, Payload: payload})
	}
	return records, rows.Err()
}

// nonEmpty: nil for NULL or "", else a pointer to the string.
func nonEmpty(ns sql.NullString) *string {
	if !ns.Valid || ns.String == "" {
		return nil
	}
	return &ns.String
}

// timePtr: nil for NULL, "" or unparseable text, else the parsed time.
func timePtr(ns sql.NullString) *time.Time {
	if t := parseTime(ns.String); !t.IsZero() {
		return &t
	}
	return nil
}
