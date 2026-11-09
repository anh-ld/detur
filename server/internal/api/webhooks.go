package api

import (
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"detur.dev/server/internal/httpx"
	"detur.dev/server/internal/store"
	"detur.dev/server/internal/webhook"
)

type webhookJSON struct {
	ID                  string   `json:"id"`
	AppID               string   `json:"appId"`
	URL                 string   `json:"url"`
	Secret              string   `json:"secret"`
	Types               []string `json:"types"`
	CursorInstalls      int64    `json:"cursorInstalls"`
	CursorClicks        int64    `json:"cursorClicks"`
	CursorEvents        int64    `json:"cursorEvents"`
	FailsInstalls       int      `json:"failsInstalls"`
	FailsClicks         int      `json:"failsClicks"`
	FailsEvents         int      `json:"failsEvents"`
	BackoffInstalls     *string  `json:"backoffInstalls,omitempty"`
	BackoffClicks       *string  `json:"backoffClicks,omitempty"`
	BackoffEvents       *string  `json:"backoffEvents,omitempty"`
	ReplayUntilInstalls int64    `json:"replayUntilInstalls"`
	ReplayUntilClicks   int64    `json:"replayUntilClicks"`
	ReplayUntilEvents   int64    `json:"replayUntilEvents"`
	Enabled             bool     `json:"enabled"`
	CreatedAt           string   `json:"createdAt"`
}

func toWebhookJSON(wh store.Webhook) webhookJSON {
	j := webhookJSON{
		ID:                  wh.ID,
		AppID:               wh.AppID,
		URL:                 wh.URL,
		Secret:              wh.Secret,
		Types:               wh.Types,
		CursorInstalls:      wh.CursorInstalls,
		CursorClicks:        wh.CursorClicks,
		CursorEvents:        wh.CursorEvents,
		FailsInstalls:       wh.FailsInstalls,
		FailsClicks:         wh.FailsClicks,
		FailsEvents:         wh.FailsEvents,
		ReplayUntilInstalls: wh.ReplayUntilInstalls,
		ReplayUntilClicks:   wh.ReplayUntilClicks,
		ReplayUntilEvents:   wh.ReplayUntilEvents,
		Enabled:             wh.Enabled,
		CreatedAt:           fmtTime(wh.CreatedAt),
	}
	j.BackoffInstalls = fmtTimePtr(wh.BackoffInstalls)
	j.BackoffClicks = fmtTimePtr(wh.BackoffClicks)
	j.BackoffEvents = fmtTimePtr(wh.BackoffEvents)
	return j
}

func fmtTimePtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := fmtTime(*t)
	return &s
}

func sanitizeEventTypes(types []string) ([]string, error) {
	if len(types) == 0 {
		return nil, errors.New("at least one event type is required")
	}
	var out []string
	for _, t := range types {
		t = strings.TrimSpace(strings.ToLower(t))
		if !store.ValidStream(t) {
			return nil, errors.New("invalid event type: must be installs, events, or clicks")
		}
		if !slices.Contains(out, t) {
			out = append(out, t)
		}
	}
	return out, nil
}

func (p *portalServer) listWebhooks(w http.ResponseWriter, r *http.Request) {
	appID := r.PathValue("id")
	if !p.requireApp(w, appID) {
		return
	}

	webhooks, err := p.st.ListWebhooks(appID)
	if err != nil {
		p.internal(w, err)
		return
	}

	out := make([]webhookJSON, 0, len(webhooks))
	for _, wh := range webhooks {
		out = append(out, toWebhookJSON(wh))
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (p *portalServer) createWebhook(w http.ResponseWriter, r *http.Request) {
	appID := r.PathValue("id")
	if !p.requireApp(w, appID) {
		return
	}

	var body struct {
		URL   string   `json:"url"`
		Types []string `json:"types"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	body.URL = strings.TrimSpace(body.URL)
	if err := webhook.ValidateWebhookURL(body.URL); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	sanitizedTypes, err := sanitizeEventTypes(body.Types)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	wh, err := p.st.CreateWebhook(appID, body.URL, sanitizedTypes)
	if err != nil {
		p.internal(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusCreated, toWebhookJSON(wh))
}

func (p *portalServer) updateWebhook(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var body struct {
		URL     *string  `json:"url"`
		Types   []string `json:"types"`
		Enabled *bool    `json:"enabled"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if body.URL != nil {
		trimmed := strings.TrimSpace(*body.URL)
		if err := webhook.ValidateWebhookURL(trimmed); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		body.URL = &trimmed
	}

	var sanitizedTypes []string
	if body.Types != nil {
		var err error
		sanitizedTypes, err = sanitizeEventTypes(body.Types)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}

	wh, err := p.st.UpdateWebhook(id, body.URL, sanitizedTypes, body.Enabled)
	if err != nil {
		p.storeErr(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, toWebhookJSON(wh))
}

func (p *portalServer) deleteWebhook(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := p.st.DeleteWebhook(id); err != nil {
		p.storeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (p *portalServer) rotateWebhookSecret(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	newSecret, err := p.st.RotateWebhookSecret(id)
	if err != nil {
		p.storeErr(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"secret": newSecret})
}

func (p *portalServer) replayWebhook(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var body struct {
		From string `json:"from"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	body.From = strings.TrimSpace(body.From)
	fromTime, err := time.Parse(time.RFC3339, body.From)
	if err != nil {
		http.Error(w, "from must be valid RFC3339 timestamp", http.StatusBadRequest)
		return
	}

	if err := p.st.ReplayWebhook(id, fromTime); err != nil {
		p.storeErr(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
