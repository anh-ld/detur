package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"detur.dev/server/internal/ua"
)

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

type WebhookStream struct {
	Cursor      int64
	Fails       int
	Backoff     *time.Time
	ReplayUntil int64
}

const (
	StreamInstalls = "installs"
	StreamClicks   = "clicks"
	StreamEvents   = "events"
)

func (w Webhook) Stream(t string) (ws WebhookStream, ok bool) {
	switch t {
	case StreamInstalls:
		return WebhookStream{w.CursorInstalls, w.FailsInstalls, w.BackoffInstalls, w.ReplayUntilInstalls}, true
	case StreamClicks:
		return WebhookStream{w.CursorClicks, w.FailsClicks, w.BackoffClicks, w.ReplayUntilClicks}, true
	case StreamEvents:
		return WebhookStream{w.CursorEvents, w.FailsEvents, w.BackoffEvents, w.ReplayUntilEvents}, true
	}
	return WebhookStream{}, false
}

// webhookStreamCol: only these literals reach SQL.
func webhookStreamCol(t string) (string, error) {
	switch t {
	case StreamInstalls, StreamClicks, StreamEvents:
		return t, nil
	}
	return "", fmt.Errorf("unknown event type %q", t)
}

func ValidStream(t string) bool {
	_, err := webhookStreamCol(t)
	return err == nil
}

type WebhookRecord struct {
	RowID   int64
	Payload any
}

// Webhook payload types are PII-safe: no device_hash, ip, user_agent, pasted_link.
type WebhookInstallPayload struct {
	Type        string  `json:"type"`
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

type WebhookClickPayload struct {
	Type        string  `json:"type"`
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
	Source      *string `json:"source,omitempty"`
	Variant     *string `json:"variant,omitempty"`
	IsBot       bool    `json:"isBot"`
	CreatedAt   string  `json:"createdAt"`
}

type WebhookEventPayload struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	AppID     string `json:"appId"`
	Event     string `json:"event"`
	Metadata  any    `json:"metadata,omitempty"`
	CreatedAt string `json:"createdAt"`
}

func GenerateWebhookSecret() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand unavailable: " + err.Error())
	}
	return "whsec_" + hex.EncodeToString(b)
}

func (s *Store) CreateWebhook(appID, webhookURL string, eventTypes []string) (Webhook, error) {
	if _, err := s.GetApp(appID); err != nil {
		return Webhook{}, err
	}

	id := "wh_" + Nanoid(20)
	secret := GenerateWebhookSecret()
	typesStr := strings.Join(eventTypes, ",")

	var cursorInstalls, cursorClicks, cursorEvents int64
	for table, dst := range map[string]*int64{StreamInstalls: &cursorInstalls, StreamClicks: &cursorClicks, StreamEvents: &cursorEvents} {
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

func (s *Store) GetWebhook(id string) (Webhook, error) {
	row := s.db.QueryRow(`SELECT `+webhookSelectCols+` FROM webhooks WHERE id = ?`, id)
	return s.scanWebhook(row)
}

func (s *Store) ListWebhooks(appID string) ([]Webhook, error) {
	list, err := s.queryWebhooks(`WHERE app_id = ?`, appID)
	if err != nil {
		return nil, fmt.Errorf("list webhooks: %w", err)
	}
	return list, nil
}

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

	tx, err := s.db.Begin()
	if err != nil {
		return Webhook{}, fmt.Errorf("update webhook: %w", err)
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`UPDATE webhooks SET url = ?, types = ?, enabled = ? WHERE id = ?`,
		newURL, newTypesStr, boolInt(newEnabled), id); err != nil {
		return Webhook{}, fmt.Errorf("update webhook: %w", err)
	}
	for _, t := range types {
		col, err := webhookStreamCol(t)
		if err != nil || slices.Contains(current.Types, t) {
			continue
		}
		if _, err := tx.Exec(`UPDATE webhooks SET cursor_`+col+` = (SELECT COALESCE(MAX(rowid), 0) FROM `+col+` WHERE app_id = ?),
			replay_until_`+col+` = 0, fails_`+col+` = 0, backoff_`+col+` = NULL WHERE id = ?`, current.AppID, id); err != nil {
			return Webhook{}, fmt.Errorf("update webhook: %s cursor: %w", t, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return Webhook{}, fmt.Errorf("update webhook: %w", err)
	}

	return s.GetWebhook(id)
}

func (s *Store) DeleteWebhook(id string) error {
	return execOne(s.db, "delete webhook", `DELETE FROM webhooks WHERE id = ?`, id)
}

func (s *Store) RotateWebhookSecret(id string) (string, error) {
	newSecret := GenerateWebhookSecret()
	if err := execOne(s.db, "rotate webhook secret", `UPDATE webhooks SET secret = ? WHERE id = ?`, newSecret, id); err != nil {
		return "", err
	}
	return newSecret, nil
}

var ErrWebhookCursorMoved = errors.New("webhook cursor moved")

// UpdateWebhookCursor: CAS vs prev, so a concurrent replay is never overwritten.
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

func (s *Store) WebhookStreamMaxRowID(eventType string) (int64, error) {
	col, err := webhookStreamCol(eventType)
	if err != nil {
		return 0, err
	}
	var n int64
	err = s.db.QueryRow(`SELECT COALESCE(MAX(n), 0) FROM row_seq WHERE name = ?`, col).Scan(&n)
	return n, err
}

func (s *Store) RecordWebhookFailure(id, eventType string, fails int, backoffUntil time.Time) error {
	col, err := webhookStreamCol(eventType)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`UPDATE webhooks SET fails_`+col+` = ?, backoff_`+col+` = ? WHERE id = ?`,
		fails, backoffUntil.UTC().Format(time.RFC3339), id)
	return err
}

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

	// order must match the UPDATE columns below
	streams := [3]string{StreamInstalls, StreamClicks, StreamEvents}
	var tail, target [3]int64
	for i, table := range streams {
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

func replayCursor(tx *sql.Tx, stream, appID, from string) (tail, target int64, err error) {
	table, err := webhookStreamCol(stream)
	if err != nil {
		return 0, 0, err
	}
	if err = tx.QueryRow(`SELECT COALESCE(MAX(rowid), 0) FROM `+table+` WHERE app_id = ?`, appID).Scan(&tail); err != nil {
		return 0, 0, err
	}
	err = tx.QueryRow(`SELECT COALESCE(MIN(rowid) - 1, ?) FROM `+table+` WHERE app_id = ? AND created_at >= ?`,
		tail, appID, from).Scan(&target)
	return tail, max(target, 0), err
}

func (s *Store) FetchPendingWebhookBatch(appID, eventType string, cursor, until int64, limit int) ([]WebhookRecord, error) {
	if limit <= 0 {
		limit = 100
	}
	if until <= 0 {
		until = math.MaxInt64
	}

	switch eventType {
	case StreamInstalls:
		return s.fetchInstallsBatch(appID, cursor, until, limit)
	case StreamClicks:
		return s.fetchClicksBatch(appID, cursor, until, limit)
	case StreamEvents:
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
		WHERE i.app_id = ? AND i.rowid > ? AND i.rowid <= ? AND i.attribution != 'unknown'
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

		payload := WebhookClickPayload{
			Type:        "click",
			ID:          id,
			AppID:       aID,
			LinkID:      linkID,
			Destination: destination,
			Kind:        kind.String,
			IsBot:       isBot == 1,
			CreatedAt:   createdAt,
		}
		payload.LinkKey = nonEmpty(linkKey)
		payload.ClickID = nonEmpty(clickID)
		payload.Device = nonEmpty(device)
		// UA is PII, never serialized
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

func nonEmpty(ns sql.NullString) *string {
	if !ns.Valid || ns.String == "" {
		return nil
	}
	return &ns.String
}

func timePtr(ns sql.NullString) *time.Time {
	if t := parseTime(ns.String); !t.IsZero() {
		return &t
	}
	return nil
}
