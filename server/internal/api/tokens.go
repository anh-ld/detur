package api

// Portal admin routes for data API / MCP tokens. Tokens are credentials to data: fail closed without ADMIN_PASSWORD.

import (
	"net/http"
	"time"
	"unicode/utf8"

	"detur.dev/server/internal/httpx"
	"detur.dev/server/internal/store"
)

// requireTokenAdmin: 403 if ADMIN_PASSWORD unset (like requireWebhookAdmin), else needs an admin session.
func (p *portalServer) requireTokenAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if p.adminPassword == "" {
			http.Error(w, "API tokens require ADMIN_PASSWORD to be configured", http.StatusForbidden)
			return
		}
		p.requireAdmin(next)(w, r)
	}
}

// tokenJSON: token wire shape. Never the hash; plaintext only in the create response.
type tokenJSON struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Prefix     string   `json:"prefix"`
	AppIDs     []string `json:"appIds"`
	CreatedAt  string   `json:"createdAt"`
	LastUsedAt *string  `json:"lastUsedAt"`
}

func toTokenJSON(t store.Token) tokenJSON {
	return tokenJSON{ID: t.ID, Name: t.Name, Prefix: t.Prefix, AppIDs: t.AppIDs,
		CreatedAt: t.CreatedAt.UTC().Format(time.RFC3339), LastUsedAt: fmtTimePtr(t.LastUsedAt)}
}

func validTokenName(n string) bool {
	c := utf8.RuneCountInString(n)
	return c >= 1 && c <= 100
}

func (p *portalServer) listTokens(w http.ResponseWriter, r *http.Request) {
	ts, err := p.st.ListTokens()
	if err != nil {
		p.internal(w, err)
		return
	}
	out := make([]tokenJSON, 0, len(ts))
	for _, t := range ts {
		out = append(out, toTokenJSON(t))
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (p *portalServer) createToken(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name   string   `json:"name"`
		AppIDs []string `json:"appIds"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if !validTokenName(body.Name) {
		http.Error(w, "name must be 1-100 characters", http.StatusBadRequest)
		return
	}
	if len(body.AppIDs) == 0 {
		http.Error(w, "appIds must list at least one app", http.StatusBadRequest)
		return
	}
	t, plain, err := p.st.CreateToken(body.Name, body.AppIDs)
	if err != nil {
		p.storeErr(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, struct {
		tokenJSON
		Token string `json:"token"` // show-once
	}{toTokenJSON(t), plain})
}

func (p *portalServer) updateToken(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name   *string  `json:"name"`
		AppIDs []string `json:"appIds"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if body.Name != nil && !validTokenName(*body.Name) {
		http.Error(w, "name must be 1-100 characters", http.StatusBadRequest)
		return
	}
	if body.AppIDs != nil && len(body.AppIDs) == 0 {
		http.Error(w, "appIds must list at least one app", http.StatusBadRequest)
		return
	}
	id := r.PathValue("id")
	if err := p.st.UpdateToken(id, stringValue(body.Name), body.AppIDs); err != nil {
		p.storeErr(w, err)
		return
	}
	ts, err := p.st.ListTokens()
	if err != nil {
		p.internal(w, err)
		return
	}
	for _, t := range ts {
		if t.ID == id {
			httpx.WriteJSON(w, http.StatusOK, toTokenJSON(t))
			return
		}
	}
	http.Error(w, "not found", http.StatusNotFound)
}

func (p *portalServer) deleteToken(w http.ResponseWriter, r *http.Request) {
	if err := p.st.DeleteToken(r.PathValue("id")); err != nil {
		p.storeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
