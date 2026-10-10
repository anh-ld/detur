package webhook_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"detur.dev/server/internal/store"
	"detur.dev/server/internal/webhook"
)

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(t.TempDir() + "/detur-worker-test.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestWorkerDeliverySuccess(t *testing.T) {
	st := newTestStore(t)
	app, err := st.CreateApp("Worker App", "dk_worker")
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}

	var mu sync.Mutex
	var receivedBody []byte
	var receivedSig, receivedReplay string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		receivedSig = r.Header.Get("Detur-Signature")
		receivedReplay = r.Header.Get("Detur-Replay")
		b, _ := io.ReadAll(r.Body)
		receivedBody = b
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// Webhook pointing to srv.URL
	wh, err := st.CreateWebhook(app.ID, srv.URL, []string{"installs"})
	if err != nil {
		t.Fatalf("CreateWebhook: %v", err)
	}

	// Insert an install
	_, err = st.RecordInstall(store.Install{
		AppID:       app.ID,
		DeviceHash:  "hash123",
		Attribution: store.AttributionOrganic,
	})
	if err != nil {
		t.Fatalf("RecordInstall: %v", err)
	}

	w := webhook.NewWorker(st, 15*time.Second)
	// Process one tick
	w.ProcessOnce(context.Background())

	mu.Lock()
	defer mu.Unlock()

	if len(receivedBody) == 0 {
		t.Fatal("expected webhook receiver to receive a delivery")
	}

	// Verify HMAC signature
	if !webhook.VerifySignature(receivedBody, wh.Secret, receivedSig, 1*time.Minute) {
		t.Errorf("HMAC signature verification failed on received body: %s", string(receivedBody))
	}

	if receivedReplay != "" {
		t.Errorf("Detur-Replay header should be empty, got %q", receivedReplay)
	}

	var items []map[string]any
	if err := json.Unmarshal(receivedBody, &items); err != nil {
		t.Fatalf("unmarshal received body: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item in batch, got %d", len(items))
	}
	if items[0]["type"] != "install" {
		t.Errorf("expected type 'install', got %v", items[0]["type"])
	}
	// Assert PII exclusion: deviceHash must NEVER be present!
	if _, exists := items[0]["deviceHash"]; exists {
		t.Error("deviceHash MUST NOT be present in webhook payload")
	}

	// Verify cursor advanced in store
	whUpdated, _ := st.GetWebhook(wh.ID)
	if whUpdated.CursorInstalls <= wh.CursorInstalls {
		t.Errorf("CursorInstalls was %d, now %d; expected it to advance", wh.CursorInstalls, whUpdated.CursorInstalls)
	}
}

func TestWorkerBatchDraining(t *testing.T) {
	st := newTestStore(t)
	app, _ := st.CreateApp("Drain App", "dk_drain")

	var mu sync.Mutex
	batchSizes := []int{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var items []any
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &items)
		mu.Lock()
		batchSizes = append(batchSizes, len(items))
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	wh, _ := st.CreateWebhook(app.ID, srv.URL, []string{"events"})

	// Insert 150 events
	for i := 1; i <= 150; i++ {
		_ = st.RecordEvent(app.ID, "custom_event", "")
	}

	w := webhook.NewWorker(st, 15*time.Second)
	w.ProcessOnce(context.Background())

	mu.Lock()
	defer mu.Unlock()

	// Should have drained 2 batches in one cycle: first of 100, second of 50!
	if len(batchSizes) != 2 {
		t.Fatalf("expected 2 drained batches, got %d (%v)", len(batchSizes), batchSizes)
	}
	if batchSizes[0] != 100 || batchSizes[1] != 50 {
		t.Errorf("expected batches [100, 50], got %v", batchSizes)
	}

	whUpdated, _ := st.GetWebhook(wh.ID)
	// Cursor should have advanced past all 150 records
	if whUpdated.CursorEvents < 150 {
		t.Errorf("CursorEvents = %d, expected >= 150", whUpdated.CursorEvents)
	}
}

func TestWorkerStreamIsolationAndBackoff(t *testing.T) {
	st := newTestStore(t)
	app, _ := st.CreateApp("Iso Worker App", "dk_isow")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var items []map[string]any
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &items)
		if len(items) > 0 && items[0]["type"] == "click" {
			// Fail click stream with 500
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		// Succeed install stream with 200
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	wh, _ := st.CreateWebhook(app.ID, srv.URL, []string{"installs", "clicks"})

	// Insert an install and a click
	link, _ := st.CreateLink(store.Link{AppID: app.ID, Key: "k", URL: "https://example.com"})
	_, _ = st.RecordClick(store.Click{AppID: app.ID, LinkID: link.ID, Destination: "https://example.com"}, 24)
	_, _ = st.RecordInstall(store.Install{AppID: app.ID, DeviceHash: "d1", Attribution: store.AttributionOrganic})

	w := webhook.NewWorker(st, 15*time.Second)
	w.ProcessOnce(context.Background())

	whUpdated, _ := st.GetWebhook(wh.ID)

	// Installs stream should succeed and advance
	if whUpdated.CursorInstalls <= wh.CursorInstalls {
		t.Errorf("installs cursor should have advanced, got %d", whUpdated.CursorInstalls)
	}
	if whUpdated.FailsInstalls != 0 || whUpdated.BackoffInstalls != nil {
		t.Errorf("installs stream should have 0 fails, got %d", whUpdated.FailsInstalls)
	}

	// Clicks stream should have failed and entered backoff
	if whUpdated.CursorClicks != wh.CursorClicks {
		t.Errorf("clicks cursor should NOT have advanced, was %d now %d", wh.CursorClicks, whUpdated.CursorClicks)
	}
	if whUpdated.FailsClicks != 1 {
		t.Errorf("clicks fails should be 1, got %d", whUpdated.FailsClicks)
	}
	if whUpdated.BackoffClicks == nil {
		t.Error("clicks backoff should be set")
	}
}

func TestWorkerReplayHeader(t *testing.T) {
	st := newTestStore(t)
	app, _ := st.CreateApp("Replay Worker App", "dk_replayw")

	// Insert 2 installs
	t0 := time.Now().Add(-2 * time.Hour)
	_, _ = st.RecordInstall(store.Install{AppID: app.ID, DeviceHash: "d1", Attribution: store.AttributionOrganic})
	_, _ = st.RecordInstall(store.Install{AppID: app.ID, DeviceHash: "d2", Attribution: store.AttributionOrganic})

	var replayHeaders []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		replayHeaders = append(replayHeaders, r.Header.Get("Detur-Replay"))
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	wh, _ := st.CreateWebhook(app.ID, srv.URL, []string{"installs"})

	// Trigger replay
	if err := st.ReplayWebhook(wh.ID, t0); err != nil {
		t.Fatalf("ReplayWebhook: %v", err)
	}

	w := webhook.NewWorker(st, 15*time.Second)
	w.ProcessOnce(context.Background())

	if len(replayHeaders) == 0 {
		t.Fatal("expected receiver to receive replayed batch")
	}
	if replayHeaders[0] != "true" {
		t.Errorf("expected Detur-Replay header 'true', got %q", replayHeaders[0])
	}
}

// Live rows arriving after a replay starts must not ride in a replay batch (and lose or steal Detur-Replay).
func TestWorkerReplayBatchStopsAtReplayUntil(t *testing.T) {
	st := newTestStore(t)
	app, _ := st.CreateApp("Replay Bound App", "dk_replaybound")

	var got []struct {
		replay string
		n      int
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body []any
		_ = json.NewDecoder(r.Body).Decode(&body)
		got = append(got, struct {
			replay string
			n      int
		}{r.Header.Get("Detur-Replay"), len(body)})
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	t0 := time.Now().Add(-time.Hour)
	_, _ = st.RecordInstall(store.Install{AppID: app.ID, DeviceHash: "d1", Attribution: store.AttributionOrganic})
	_, _ = st.RecordInstall(store.Install{AppID: app.ID, DeviceHash: "d2", Attribution: store.AttributionOrganic})
	wh, _ := st.CreateWebhook(app.ID, srv.URL, []string{"installs"})
	if err := st.ReplayWebhook(wh.ID, t0); err != nil {
		t.Fatalf("ReplayWebhook: %v", err)
	}
	_, _ = st.RecordInstall(store.Install{AppID: app.ID, DeviceHash: "d3", Attribution: store.AttributionOrganic})

	webhook.NewWorker(st, 15*time.Second).ProcessOnce(context.Background())

	if len(got) != 2 || got[0].replay != "true" || got[0].n != 2 || got[1].replay != "" || got[1].n != 1 {
		t.Fatalf("deliveries = %+v, want replay batch of 2 then live batch of 1", got)
	}
	after, _ := st.GetWebhook(wh.ID)
	if after.ReplayUntilInstalls != 0 {
		t.Errorf("ReplayUntilInstalls = %d, want 0 after replay completes", after.ReplayUntilInstalls)
	}
}

// A purge that empties the table lets new rows reuse rowids at or below the cursor; they must still be delivered.
func TestWorkerDeliversAfterPurgeEmptiesTable(t *testing.T) {
	st := newTestStore(t)
	app, _ := st.CreateApp("Purge App", "dk_purge")

	var delivered int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body []any
		_ = json.NewDecoder(r.Body).Decode(&body)
		delivered += len(body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	wh, _ := st.CreateWebhook(app.ID, srv.URL, []string{"events"})
	w := webhook.NewWorker(st, 15*time.Second)
	for range 3 {
		_ = st.RecordEvent(app.ID, "old", "")
	}
	w.ProcessOnce(context.Background())
	if delivered != 3 {
		t.Fatalf("delivered = %d before purge, want 3", delivered)
	}

	if _, err := st.PurgeExpired(time.Now().Add(48*time.Hour), 24); err != nil {
		t.Fatalf("PurgeExpired: %v", err)
	}
	_ = st.RecordEvent(app.ID, "new", "")
	w.ProcessOnce(context.Background())

	if delivered != 4 {
		t.Fatalf("delivered = %d after purge + new event, want 4 (new row reused a rowid below the cursor)", delivered)
	}
	_ = wh
}

// Tail deleted + refilled before the next tick: no rowid reuse -> each new row delivered once.
func TestWorkerDeliversRefilledTailOnce(t *testing.T) {
	st := newTestStore(t)
	app, _ := st.CreateApp("Refill App", "dk_refill")
	var delivered int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body []any
		_ = json.NewDecoder(r.Body).Decode(&body)
		delivered += len(body)
	}))
	defer srv.Close()
	if _, err := st.CreateWebhook(app.ID, srv.URL, []string{"events"}); err != nil {
		t.Fatal(err)
	}
	w := webhook.NewWorker(st, 15*time.Second)
	for range 3 {
		_ = st.RecordEvent(app.ID, "old", "")
	}
	w.ProcessOnce(context.Background())
	if _, err := st.PurgeExpired(time.Now().Add(48*time.Hour), 24); err != nil {
		t.Fatal(err)
	}
	for range 4 {
		_ = st.RecordEvent(app.ID, "new", "")
	}
	w.ProcessOnce(context.Background())
	if delivered != 7 {
		t.Fatalf("delivered = %d, want 7 (3 old + 4 refilled, no skips, no duplicates)", delivered)
	}
}

// Unknown install never sent; its replacement is.
func TestWorkerSkipsUnknownSendsReplacement(t *testing.T) {
	st := newTestStore(t)
	app, _ := st.CreateApp("Unknown App", "dk_unknown")
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body []map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		for _, rec := range body {
			got = append(got, fmt.Sprint(rec["attribution"]))
		}
	}))
	defer srv.Close()
	if _, err := st.CreateWebhook(app.ID, srv.URL, []string{"installs"}); err != nil {
		t.Fatal(err)
	}
	w := webhook.NewWorker(st, 15*time.Second)
	if _, err := st.RecordInstall(store.Install{AppID: app.ID, DeviceHash: "d1", Attribution: store.AttributionUnknown}); err != nil {
		t.Fatal(err)
	}
	w.ProcessOnce(context.Background())
	if _, err := st.RecordInstall(store.Install{AppID: app.ID, DeviceHash: "d1", Attribution: store.AttributionOrganic}); err != nil {
		t.Fatal(err)
	}
	w.ProcessOnce(context.Background())
	if len(got) != 1 || got[0] != store.AttributionOrganic {
		t.Fatalf("delivered attributions = %v, want [organic]", got)
	}
}

// Cursor above the rowid high-water mark (restored older backup): rewind, new rows still delivered.
func TestWorkerRewindsCursorAboveHighWater(t *testing.T) {
	st := newTestStore(t)
	app, _ := st.CreateApp("Restore App", "dk_restore")
	var delivered int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body []any
		_ = json.NewDecoder(r.Body).Decode(&body)
		delivered += len(body)
	}))
	defer srv.Close()
	wh, err := st.CreateWebhook(app.ID, srv.URL, []string{"events"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.UpdateWebhookCursor(wh.ID, "events", store.WebhookStream{Cursor: wh.CursorEvents}, 1000, false); err != nil {
		t.Fatal(err)
	}
	_ = st.RecordEvent(app.ID, "after-restore", "")
	webhook.NewWorker(st, 15*time.Second).ProcessOnce(context.Background())
	if delivered != 1 {
		t.Fatalf("delivered = %d, want 1 (cursor rewound below the new row)", delivered)
	}
}
