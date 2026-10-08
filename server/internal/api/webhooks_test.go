package api_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"detur.dev/server/internal/api"
	"detur.dev/server/internal/store"
)

func newPortalWithAdmin(t *testing.T, adminPassword string) (string, *store.Store) {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/portal-test.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	portalAddr := "127.0.0.1:0"
	h := api.RegisterPortal(st, filepath.Join(t.TempDir(), "static"), []string{portalAddr}, "", adminPassword, 12)
	srv := httptest.NewServer(h)
	t.Cleanup(func() { srv.Close() })
	return srv.URL, st
}

func elevatePortalSession(t *testing.T, portalURL, password string) *http.Cookie {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"password": password})
	req, _ := http.NewRequest(http.MethodPost, portalURL+"/api/admin/session", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("elevate request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("elevate status: %d", resp.StatusCode)
	}
	for _, c := range resp.Cookies() {
		if c.Name == "detur_admin" {
			return c
		}
	}
	t.Fatal("no detur_admin cookie returned")
	return nil
}

func TestWebhookRoutesFailClosedWithoutAdminPassword(t *testing.T) {
	// ADMIN_PASSWORD is unset ("")
	portalURL, st := newPortalWithAdmin(t, "")
	app, _ := st.CreateApp("App1", "dk_1")

	endpoints := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/apps/" + app.ID + "/webhooks"},
		{http.MethodPost, "/api/apps/" + app.ID + "/webhooks"},
		{http.MethodPatch, "/api/webhooks/wh_123"},
		{http.MethodDelete, "/api/webhooks/wh_123"},
		{http.MethodPost, "/api/webhooks/wh_123/rotate-secret"},
		{http.MethodPost, "/api/webhooks/wh_123/replay"},
	}

	for _, ep := range endpoints {
		req, _ := http.NewRequest(ep.method, portalURL+ep.path, bytes.NewReader([]byte(`{}`)))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", ep.method, ep.path, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s %s without ADMIN_PASSWORD: got %d, want 403 Forbidden (fail closed)",
				ep.method, ep.path, resp.StatusCode)
		}
	}
}

func TestWebhookRoutesRequireElevationWhenAdminSet(t *testing.T) {
	portalURL, st := newPortalWithAdmin(t, "supersecret")
	app, _ := st.CreateApp("App1", "dk_1")

	// Unauthenticated request should be 403 Forbidden
	req, _ := http.NewRequest(http.MethodGet, portalURL+"/api/apps/"+app.ID+"/webhooks", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("unauthenticated GET webhooks: got %d, want 403", resp.StatusCode)
	}
}

func TestWebhookRoutesCRUDWithAdminElevation(t *testing.T) {
	adminPass := "correct-horse"
	portalURL, st := newPortalWithAdmin(t, adminPass)
	cookie := elevatePortalSession(t, portalURL, adminPass)
	app, _ := st.CreateApp("Hook App", "dk_hook")

	// 1. Validation error on SSRF destination (169.254.169.254)
	bodyInvalid, _ := json.Marshal(map[string]any{
		"url":   "http://169.254.169.254/hook",
		"types": []string{"installs"},
	})
	req, _ := http.NewRequest(http.MethodPost, portalURL+"/api/apps/"+app.ID+"/webhooks", bytes.NewReader(bodyInvalid))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST invalid webhook: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("POST prohibited URL: got %d, want 400 Bad Request", resp.StatusCode)
	}

	// 2. Successful Create Webhook
	bodyValid, _ := json.Marshal(map[string]any{
		"url":   "https://example.com/events-receiver",
		"types": []string{"installs", "events"},
	})
	req, _ = http.NewRequest(http.MethodPost, portalURL+"/api/apps/"+app.ID+"/webhooks", bytes.NewReader(bodyValid))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST valid webhook: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST valid webhook: got %d, want 201 Created", resp.StatusCode)
	}
	var created map[string]any
	b, _ := io.ReadAll(resp.Body)
	_ = json.Unmarshal(b, &created)
	whID, _ := created["id"].(string)
	if whID == "" {
		t.Fatal("expected non-empty webhook id")
	}
	secret, _ := created["secret"].(string)
	if secret == "" {
		t.Fatal("expected secret in creation response")
	}

	// 3. List Webhooks
	req, _ = http.NewRequest(http.MethodGet, portalURL+"/api/apps/"+app.ID+"/webhooks", nil)
	req.AddCookie(cookie)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET webhooks: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET webhooks: got %d, want 200 OK", resp.StatusCode)
	}
	var list []map[string]any
	b, _ = io.ReadAll(resp.Body)
	_ = json.Unmarshal(b, &list)
	if len(list) != 1 || list[0]["id"] != whID {
		t.Errorf("List returned unexpected items: %v", list)
	}

	// 4. Update Webhook
	disabled := false
	bodyUpdate, _ := json.Marshal(map[string]any{
		"enabled": disabled,
		"types":   []string{"installs", "clicks", "events"},
	})
	req, _ = http.NewRequest(http.MethodPatch, portalURL+"/api/webhooks/"+whID, bytes.NewReader(bodyUpdate))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PATCH webhook: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PATCH webhook: got %d, want 200 OK", resp.StatusCode)
	}
	var updated map[string]any
	b, _ = io.ReadAll(resp.Body)
	_ = json.Unmarshal(b, &updated)
	if updated["enabled"] != false {
		t.Errorf("expected enabled = false, got %v", updated["enabled"])
	}

	// 5. Rotate Secret
	req, _ = http.NewRequest(http.MethodPost, portalURL+"/api/webhooks/"+whID+"/rotate-secret", nil)
	req.AddCookie(cookie)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST rotate-secret: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST rotate-secret: got %d, want 200 OK", resp.StatusCode)
	}
	var rotRes map[string]string
	b, _ = io.ReadAll(resp.Body)
	_ = json.Unmarshal(b, &rotRes)
	newSecret := rotRes["secret"]
	if newSecret == "" || newSecret == secret {
		t.Errorf("unexpected rotated secret: %q (old %q)", newSecret, secret)
	}

	// 6. Replay Webhook
	bodyReplay, _ := json.Marshal(map[string]string{
		"from": time.Now().Add(-1 * time.Hour).UTC().Format(time.RFC3339),
	})
	req, _ = http.NewRequest(http.MethodPost, portalURL+"/api/webhooks/"+whID+"/replay", bytes.NewReader(bodyReplay))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST replay: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST replay: got %d, want 200 OK", resp.StatusCode)
	}

	// 7. Delete Webhook
	req, _ = http.NewRequest(http.MethodDelete, portalURL+"/api/webhooks/"+whID, nil)
	req.AddCookie(cookie)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE webhook: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE webhook: got %d, want 204 No Content", resp.StatusCode)
	}
}
