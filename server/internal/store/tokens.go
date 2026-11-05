package store

import (
	"database/sql"
	"fmt"
	"slices"
	"strings"
	"time"
)

// Token: data API / MCP bearer token. Plaintext only returned by CreateToken; only SHA-256 stored.
type Token struct {
	ID         string
	Name       string
	Prefix     string // dtr_ + first 4 chars, for display
	AppIDs     []string
	CreatedAt  time.Time
	LastUsedAt *time.Time
}

// TouchEvery: min gap between last_used_at writes per token (one SQLite conn).
const TouchEvery = time.Minute

// CreateToken: new token for appIDs (deduped); unknown app -> ErrNotFound, nothing stored. Plaintext returned once.
func (s *Store) CreateToken(name string, appIDs []string) (Token, string, error) {
	plain := "dtr_" + Nanoid(32)
	t := Token{ID: Nanoid(21), Name: name, Prefix: plain[:8], AppIDs: dedup(appIDs), CreatedAt: time.Now().UTC()}
	tx, err := s.db.Begin()
	if err != nil {
		return Token{}, "", fmt.Errorf("create token: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO api_tokens (id, name, token_hash, prefix, created_at) VALUES (?, ?, ?, ?, ?)`,
		t.ID, name, HashKey(plain), t.Prefix, rfc3339(t.CreatedAt)); err != nil {
		return Token{}, "", fmt.Errorf("create token: %w", err)
	}
	if err := setTokenApps(tx, t.ID, t.AppIDs); err != nil {
		return Token{}, "", err
	}
	if err := tx.Commit(); err != nil {
		return Token{}, "", fmt.Errorf("create token: %w", err)
	}
	return t, plain, nil
}

// setTokenApps: replace token's app set; unknown app -> ErrNotFound.
func setTokenApps(tx *sql.Tx, id string, appIDs []string) error {
	if _, err := tx.Exec(`DELETE FROM api_token_apps WHERE token_id = ?`, id); err != nil {
		return fmt.Errorf("token apps: %w", err)
	}
	for _, a := range appIDs {
		var one int
		if err := tx.QueryRow(`SELECT 1 FROM apps WHERE id = ?`, a).Scan(&one); err != nil {
			if err == sql.ErrNoRows {
				return ErrNotFound
			}
			return fmt.Errorf("token apps: %w", err)
		}
		if _, err := tx.Exec(`INSERT INTO api_token_apps (token_id, app_id) VALUES (?, ?)`, id, a); err != nil {
			return fmt.Errorf("token apps: %w", err)
		}
	}
	return nil
}

func dedup(ids []string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out
}

// ListTokens: all tokens, oldest first, with allowed app ids.
func (s *Store) ListTokens() ([]Token, error) {
	return s.tokens(`ORDER BY created_at, id`)
}

// TokenByPlain: token for a presented secret (hash lookup); ErrNotFound on miss.
func (s *Store) TokenByPlain(plain string) (Token, error) {
	ts, err := s.tokens(`WHERE token_hash = ?`, HashKey(plain))
	if err != nil {
		return Token{}, err
	}
	if len(ts) == 0 {
		return Token{}, ErrNotFound
	}
	return ts[0], nil
}

// TokenByID: token by id; ErrNotFound on miss.
func (s *Store) TokenByID(id string) (Token, error) {
	ts, err := s.tokens(`WHERE id = ?`, id)
	if err != nil {
		return Token{}, err
	}
	if len(ts) == 0 {
		return Token{}, ErrNotFound
	}
	return ts[0], nil
}

func (s *Store) tokens(tail string, args ...any) ([]Token, error) {
	rows, err := s.db.Query(`SELECT id, name, prefix, created_at, last_used_at FROM api_tokens `+tail, args...)
	if err != nil {
		return nil, fmt.Errorf("list tokens: %w", err)
	}
	defer rows.Close()
	out := []Token{}
	for rows.Next() {
		var t Token
		var created string
		var used sql.NullString
		if err := rows.Scan(&t.ID, &t.Name, &t.Prefix, &created, &used); err != nil {
			return nil, fmt.Errorf("list tokens: %w", err)
		}
		t.CreatedAt = parseTime(created)
		if used.Valid {
			u := parseTime(used.String)
			t.LastUsedAt = &u
		}
		t.AppIDs = []string{}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list tokens: %w", err)
	}
	rows.Close() // one connection: free it before the app query
	if len(out) == 0 {
		return out, nil
	}
	idx := make(map[string]int, len(out))
	args = make([]any, len(out))
	for i, t := range out {
		idx[t.ID] = i
		args[i] = t.ID
	}
	ar, err := s.db.Query(`SELECT token_id, app_id FROM api_token_apps WHERE token_id IN (?`+strings.Repeat(",?", len(out)-1)+`) ORDER BY app_id`, args...)
	if err != nil {
		return nil, fmt.Errorf("list tokens: %w", err)
	}
	defer ar.Close()
	for ar.Next() {
		var id, a string
		if err := ar.Scan(&id, &a); err != nil {
			return nil, fmt.Errorf("list tokens: %w", err)
		}
		out[idx[id]].AppIDs = append(out[idx[id]].AppIDs, a)
	}
	if err := ar.Err(); err != nil {
		return nil, fmt.Errorf("list tokens: %w", err)
	}
	return out, nil
}

// UpdateToken: rename ("" keeps) and/or replace app set (nil keeps) in one tx. ErrNotFound if token or any app unknown.
func (s *Store) UpdateToken(id, name string, appIDs []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("update token: %w", err)
	}
	defer tx.Rollback()
	res, err := tx.Exec(`UPDATE api_tokens SET name = COALESCE(NULLIF(?, ''), name) WHERE id = ?`, name, id)
	if err != nil {
		return fmt.Errorf("update token: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if appIDs != nil {
		if err := setTokenApps(tx, id, dedup(appIDs)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// DeleteToken: revoke; ErrNotFound if unknown. Takes effect on the next request.
func (s *Store) DeleteToken(id string) error {
	res, err := s.db.Exec(`DELETE FROM api_tokens WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete token: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// TouchToken: set last_used_at = now unless set within TouchEvery.
func (s *Store) TouchToken(id string, now time.Time) error {
	_, err := s.db.Exec(`UPDATE api_tokens SET last_used_at = ? WHERE id = ? AND (last_used_at IS NULL OR last_used_at < ?)`,
		rfc3339(now), id, rfc3339(now.Add(-TouchEvery)))
	if err != nil {
		return fmt.Errorf("touch token: %w", err)
	}
	return nil
}
