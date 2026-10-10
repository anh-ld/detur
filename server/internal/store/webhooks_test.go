package store

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestWebhooksCRUD(t *testing.T) {
	s := newTestStore(t)
	app, err := s.CreateApp("Hook App", "dk_hook")
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}

	// 1. Create Webhook
	wh, err := s.CreateWebhook(app.ID, "https://example.com/webhook", []string{"installs", "events"})
	if err != nil {
		t.Fatalf("CreateWebhook: %v", err)
	}
	if wh.ID == "" {
		t.Error("expected non-empty webhook ID")
	}
	if wh.AppID != app.ID {
		t.Errorf("AppID = %q, want %q", wh.AppID, app.ID)
	}
	if wh.URL != "https://example.com/webhook" {
		t.Errorf("URL = %q, want %q", wh.URL, "https://example.com/webhook")
	}
	if !strings.HasPrefix(wh.Secret, "whsec_") || len(wh.Secret) < 70 {
		t.Errorf("Secret = %q, expected prefix whsec_ with at least 32 bytes hex", wh.Secret)
	}
	if len(wh.Types) != 2 || wh.Types[0] != "installs" || wh.Types[1] != "events" {
		t.Errorf("Types = %v, want [installs, events]", wh.Types)
	}
	if !wh.Enabled {
		t.Error("expected webhook to be enabled by default")
	}
	if wh.CursorInstalls != 0 || wh.CursorClicks != 0 || wh.CursorEvents != 0 {
		t.Errorf("expected 0 initial cursors on empty DB, got (%d, %d, %d)", wh.CursorInstalls, wh.CursorClicks, wh.CursorEvents)
	}

	// 2. Get Webhook
	got, err := s.GetWebhook(wh.ID)
	if err != nil {
		t.Fatalf("GetWebhook: %v", err)
	}
	if got.ID != wh.ID || got.Secret != wh.Secret || got.URL != wh.URL {
		t.Errorf("GetWebhook mismatch: %+v vs %+v", got, wh)
	}

	// 3. List Webhooks
	list, err := s.ListWebhooks(app.ID)
	if err != nil {
		t.Fatalf("ListWebhooks: %v", err)
	}
	if len(list) != 1 || list[0].ID != wh.ID {
		t.Errorf("ListWebhooks = %+v, want 1 webhook with ID %q", list, wh.ID)
	}

	// 4. Update Webhook
	newURL := "https://example.com/webhook-updated"
	newTypes := []string{"installs", "events", "clicks"}
	disabled := false
	updated, err := s.UpdateWebhook(wh.ID, &newURL, newTypes, &disabled)
	if err != nil {
		t.Fatalf("UpdateWebhook: %v", err)
	}
	if updated.URL != newURL || len(updated.Types) != 3 || updated.Enabled != false {
		t.Errorf("UpdateWebhook result unexpected: %+v", updated)
	}

	// 5. Rotate Secret
	oldSecret := wh.Secret
	newSecret, err := s.RotateWebhookSecret(wh.ID)
	if err != nil {
		t.Fatalf("RotateWebhookSecret: %v", err)
	}
	if newSecret == oldSecret || !strings.HasPrefix(newSecret, "whsec_") {
		t.Errorf("RotateWebhookSecret: old %q, new %q", oldSecret, newSecret)
	}
	gotAfterRotate, _ := s.GetWebhook(wh.ID)
	if gotAfterRotate.Secret != newSecret {
		t.Errorf("stored secret = %q, want %q", gotAfterRotate.Secret, newSecret)
	}

	// 6. Delete Webhook
	if err := s.DeleteWebhook(wh.ID); err != nil {
		t.Fatalf("DeleteWebhook: %v", err)
	}
	if _, err := s.GetWebhook(wh.ID); err != ErrNotFound {
		t.Errorf("expected ErrNotFound after DeleteWebhook, got %v", err)
	}
}

func TestWebhookInitialCursors(t *testing.T) {
	s := newTestStore(t)
	app, err := s.CreateApp("Cursor App", "dk_cursor")
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}

	// Record an install, click, event before creating webhook
	link, err := s.CreateLink(Link{
		AppID: app.ID,
		Key:   "k1",
		URL:   "https://example.com",
	})
	if err != nil {
		t.Fatalf("CreateLink: %v", err)
	}
	_, err = s.RecordClick(Click{
		AppID:       app.ID,
		LinkID:      link.ID,
		Destination: "https://example.com",
	}, 24)
	if err != nil {
		t.Fatalf("RecordClick: %v", err)
	}
	_, err = s.RecordInstall(Install{
		AppID:       app.ID,
		DeviceHash:  "dev1",
		Attribution: AttributionOrganic,
	})
	if err != nil {
		t.Fatalf("RecordInstall: %v", err)
	}
	if err := s.RecordEvent(app.ID, "signup", `{"foo":"bar"}`); err != nil {
		t.Fatalf("RecordEvent: %v", err)
	}

	// Create webhook: cursors should be set to existing MAX(rowid) > 0
	wh, err := s.CreateWebhook(app.ID, "https://example.com/hook", []string{"installs", "clicks", "events"})
	if err != nil {
		t.Fatalf("CreateWebhook: %v", err)
	}
	if wh.CursorInstalls <= 0 {
		t.Errorf("CursorInstalls = %d, want > 0", wh.CursorInstalls)
	}
	if wh.CursorClicks <= 0 {
		t.Errorf("CursorClicks = %d, want > 0", wh.CursorClicks)
	}
	if wh.CursorEvents <= 0 {
		t.Errorf("CursorEvents = %d, want > 0", wh.CursorEvents)
	}
}

func TestWebhooksCascadeDelete(t *testing.T) {
	s := newTestStore(t)
	app, _ := s.CreateApp("Cascade App", "dk_cascade")
	wh, err := s.CreateWebhook(app.ID, "https://example.com/hook", []string{"installs"})
	if err != nil {
		t.Fatalf("CreateWebhook: %v", err)
	}

	if err := s.DeleteApp(app.ID); err != nil {
		t.Fatalf("DeleteApp: %v", err)
	}

	if _, err := s.GetWebhook(wh.ID); err != ErrNotFound {
		t.Errorf("expected ErrNotFound for webhook after app deleted, got %v", err)
	}
}

func TestFetchPendingWebhookBatchPIIExclusion(t *testing.T) {
	s := newTestStore(t)
	app, _ := s.CreateApp("PII App", "dk_pii")
	link, _ := s.CreateLink(Link{AppID: app.ID, Key: "secret-key", URL: "https://example.com"})

	// Record click with sensitive PII
	recordedClick, err := s.RecordClick(Click{
		AppID:  app.ID,
		LinkID: link.ID,
		Fingerprint: Fingerprint{
			IP:         "198.51.100.42",
			UserAgent:  "Mozilla/5.0 (Linux; Android 14; Pixel 8) SecretAgent/1.0",
			PastedLink: "https://secret.pasted.com",
		},
		Destination: "https://example.com",
		Source:      "messenger",
	}, 24)
	if err != nil {
		t.Fatalf("RecordClick: %v", err)
	}

	// Record install with sensitive device hash
	_, err = s.RecordInstall(Install{
		AppID:       app.ID,
		DeviceHash:  "sensitive_device_hash_123",
		ClickID:     recordedClick.ID,
		Attribution: AttributionNonOrganic,
		LinkID:      link.ID,
		Platform:    "ios",
		Method:      MethodClickID,
	})
	if err != nil {
		t.Fatalf("RecordInstall: %v", err)
	}

	// Record event
	err = s.RecordEvent(app.ID, "purchase", `{"amount":99.99,"currency":"USD"}`)
	if err != nil {
		t.Fatalf("RecordEvent: %v", err)
	}

	// 1. Fetch installs batch
	installs, err := s.FetchPendingWebhookBatch(app.ID, "installs", 0, 0, 100)
	if err != nil {
		t.Fatalf("FetchPendingWebhookBatch(installs): %v", err)
	}
	if len(installs) != 1 {
		t.Fatalf("got %d installs, want 1", len(installs))
	}
	instPayload, ok := installs[0].Payload.(WebhookInstallPayload)
	if !ok {
		t.Fatalf("expected WebhookInstallPayload, got %T", installs[0].Payload)
	}
	if instPayload.Type != "install" || instPayload.AppID != app.ID || instPayload.Attribution != AttributionNonOrganic {
		t.Errorf("unexpected install payload: %+v", instPayload)
	}

	// 2. Fetch clicks batch
	clicks, err := s.FetchPendingWebhookBatch(app.ID, "clicks", 0, 0, 100)
	if err != nil {
		t.Fatalf("FetchPendingWebhookBatch(clicks): %v", err)
	}
	if len(clicks) != 1 {
		t.Fatalf("got %d clicks, want 1", len(clicks))
	}
	clickPayload, ok := clicks[0].Payload.(WebhookClickPayload)
	if !ok {
		t.Fatalf("expected WebhookClickPayload, got %T", clicks[0].Payload)
	}
	if clickPayload.Type != "click" || clickPayload.LinkID != link.ID {
		t.Errorf("unexpected click payload: %+v", clickPayload)
	}
	// Serialized payloads must never carry the excluded fields
	for _, rec := range append(installs, clicks...) {
		b, _ := json.Marshal(rec.Payload)
		for _, secret := range []string{"198.51.100.42", "SecretAgent", "secret.pasted.com", "sensitive_device_hash_123"} {
			if strings.Contains(string(b), secret) {
				t.Errorf("payload leaks %q: %s", secret, b)
			}
		}
	}
	// Platform comes from the UA (Android device labels carry only the model name); the UA itself stays out
	if clickPayload.Platform == nil || *clickPayload.Platform != "android" {
		t.Errorf("click Platform = %v, want android", clickPayload.Platform)
	}
	if clickPayload.Source == nil || *clickPayload.Source != "messenger" {
		t.Errorf("click Source = %v, want messenger", clickPayload.Source)
	}
	if clickPayload.LinkKey == nil || *clickPayload.LinkKey != "secret-key" {
		t.Errorf("expected LinkKey = secret-key, got %v", clickPayload.LinkKey)
	}

	// 3. Fetch events batch
	events, err := s.FetchPendingWebhookBatch(app.ID, "events", 0, 0, 100)
	if err != nil {
		t.Fatalf("FetchPendingWebhookBatch(events): %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	eventPayload, ok := events[0].Payload.(WebhookEventPayload)
	if !ok {
		t.Fatalf("expected WebhookEventPayload, got %T", events[0].Payload)
	}
	if eventPayload.Type != "event" || eventPayload.Event != "purchase" {
		t.Errorf("unexpected event payload: %+v", eventPayload)
	}
}

func TestWebhookStreamIsolationAndBackoff(t *testing.T) {
	s := newTestStore(t)
	app, _ := s.CreateApp("Iso App", "dk_iso")
	wh, err := s.CreateWebhook(app.ID, "https://example.com/hook", []string{"installs", "clicks"})
	if err != nil {
		t.Fatalf("CreateWebhook: %v", err)
	}

	// Record failure on clicks only
	backoffUntil := time.Now().Add(60 * time.Second)
	err = s.RecordWebhookFailure(wh.ID, "clicks", 1, backoffUntil)
	if err != nil {
		t.Fatalf("RecordWebhookFailure: %v", err)
	}

	whUpdated, err := s.GetWebhook(wh.ID)
	if err != nil {
		t.Fatalf("GetWebhook: %v", err)
	}
	if whUpdated.FailsClicks != 1 {
		t.Errorf("FailsClicks = %d, want 1", whUpdated.FailsClicks)
	}
	if whUpdated.BackoffClicks == nil {
		t.Error("expected BackoffClicks to be set")
	}
	// Installs stream must remain completely unaffected!
	if whUpdated.FailsInstalls != 0 || whUpdated.BackoffInstalls != nil {
		t.Errorf("Installs stream should not be affected: fails=%d, backoff=%v",
			whUpdated.FailsInstalls, whUpdated.BackoffInstalls)
	}

	// Advance cursor on installs
	prev, _ := whUpdated.Stream("installs")
	err = s.UpdateWebhookCursor(wh.ID, "installs", prev, 42, false)
	if err != nil {
		t.Fatalf("UpdateWebhookCursor: %v", err)
	}
	// A write based on stale state (e.g. a replay moved the cursor meanwhile) is refused, not applied
	if err := s.UpdateWebhookCursor(wh.ID, "installs", prev, 99, false); !errors.Is(err, ErrWebhookCursorMoved) {
		t.Fatalf("stale UpdateWebhookCursor err = %v, want ErrWebhookCursorMoved", err)
	}
	whUpdated, _ = s.GetWebhook(wh.ID)
	if whUpdated.CursorInstalls != 42 {
		t.Errorf("CursorInstalls = %d, want 42", whUpdated.CursorInstalls)
	}
	// Success clears failure and backoff on that stream
	if whUpdated.FailsInstalls != 0 || whUpdated.BackoffInstalls != nil {
		t.Errorf("Installs stream fails should be 0, got %d", whUpdated.FailsInstalls)
	}
}

func TestWebhookReplay(t *testing.T) {
	s := newTestStore(t)
	app, _ := s.CreateApp("Replay App", "dk_replay")

	// Insert 3 installs with timestamps 1 hour apart
	t0 := time.Now().Add(-3 * time.Hour)
	for i := 1; i <= 3; i++ {
		_, err := s.RecordInstall(Install{
			AppID:       app.ID,
			DeviceHash:  Nanoid(16),
			Attribution: AttributionOrganic,
		})
		if err != nil {
			t.Fatalf("RecordInstall %d: %v", i, err)
		}
	}

	// Create webhook initially at max cursor
	wh, err := s.CreateWebhook(app.ID, "https://example.com/hook", []string{"installs"})
	if err != nil {
		t.Fatalf("CreateWebhook: %v", err)
	}
	initialCursor := wh.CursorInstalls
	if initialCursor == 0 {
		t.Fatalf("expected initialCursor > 0")
	}

	// Replay from t0 (should rewind cursor to 0 so all 3 installs can replay)
	err = s.ReplayWebhook(wh.ID, t0)
	if err != nil {
		t.Fatalf("ReplayWebhook: %v", err)
	}

	whReplayed, err := s.GetWebhook(wh.ID)
	if err != nil {
		t.Fatalf("GetWebhook: %v", err)
	}
	if whReplayed.CursorInstalls >= initialCursor {
		t.Errorf("CursorInstalls after replay = %d, want < %d", whReplayed.CursorInstalls, initialCursor)
	}
	if whReplayed.ReplayUntilInstalls != initialCursor {
		t.Errorf("ReplayUntilInstalls = %d, want %d", whReplayed.ReplayUntilInstalls, initialCursor)
	}
}

// Webhook delivery worker / fetch batches serialize click and install payloads containing "variant".
func TestWebhookVariantPayload(t *testing.T) {
	s := newTestStore(t)
	app, _ := s.CreateApp("Variant Hook App", "dk_var_hook")
	link, _ := s.CreateLink(Link{AppID: app.ID, Key: "vlink", URL: "https://example.com"})

	// 1. Record click with variant
	c, err := s.RecordClick(Click{
		AppID:       app.ID,
		LinkID:      link.ID,
		Variant:     "Variant Alpha",
		Destination: "https://example.com/alpha",
	}, 24)
	if err != nil {
		t.Fatalf("RecordClick: %v", err)
	}

	// 2. Record install with variant
	_, err = s.RecordInstall(Install{
		AppID:       app.ID,
		DeviceHash:  "dev-var-hook",
		ClickID:     c.ID,
		Attribution: AttributionNonOrganic,
		LinkID:      link.ID,
		Variant:     "Variant Alpha",
	})
	if err != nil {
		t.Fatalf("RecordInstall: %v", err)
	}

	// 3. Fetch clicks batch
	clicks, err := s.FetchPendingWebhookBatch(app.ID, "clicks", 0, 0, 100)
	if err != nil {
		t.Fatalf("FetchPendingWebhookBatch(clicks): %v", err)
	}
	if len(clicks) != 1 {
		t.Fatalf("expected 1 click, got %d", len(clicks))
	}
	cp := clicks[0].Payload.(WebhookClickPayload)
	if cp.Variant == nil || *cp.Variant != "Variant Alpha" {
		t.Errorf("expected click payload variant 'Variant Alpha', got %v", cp.Variant)
	}
	clickJSON, _ := json.Marshal(cp)
	if !strings.Contains(string(clickJSON), `"variant":"Variant Alpha"`) {
		t.Errorf("serialized click JSON missing variant: %s", string(clickJSON))
	}

	// 4. Fetch installs batch
	installs, err := s.FetchPendingWebhookBatch(app.ID, "installs", 0, 0, 100)
	if err != nil {
		t.Fatalf("FetchPendingWebhookBatch(installs): %v", err)
	}
	if len(installs) != 1 {
		t.Fatalf("expected 1 install, got %d", len(installs))
	}
	ip := installs[0].Payload.(WebhookInstallPayload)
	if ip.Variant == nil || *ip.Variant != "Variant Alpha" {
		t.Errorf("expected install payload variant 'Variant Alpha', got %v", ip.Variant)
	}
	instJSON, _ := json.Marshal(ip)
	if !strings.Contains(string(instJSON), `"variant":"Variant Alpha"`) {
		t.Errorf("serialized install JSON missing variant: %s", string(instJSON))
	}
}
