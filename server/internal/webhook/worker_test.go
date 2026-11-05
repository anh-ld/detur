package webhook_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
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

	wh, err := st.CreateWebhook(app.ID, srv.URL, []string{"installs"})
	if err != nil {
		t.Fatalf("CreateWebhook: %v", err)
	}

	_, err = st.RecordInstall(store.Install{
		AppID:       app.ID,
		DeviceHash:  "hash123",
		Attribution: store.AttributionOrganic,
	})
	if err != nil {
		t.Fatalf("RecordInstall: %v", err)
	}

	w := webhook.NewWorker(st, 15*time.Second)
	w.ProcessOnce(context.Background())

	mu.Lock()
	defer mu.Unlock()

	if len(receivedBody) == 0 {
		t.Fatal("expected webhook receiver to receive a delivery")
	}

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
	// PII: deviceHash must NEVER be sent
	if _, exists := items[0]["deviceHash"]; exists {
		t.Error("deviceHash MUST NOT be present in webhook payload")
	}

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

	for i := 1; i <= 150; i++ {
		_ = st.RecordEvent(app.ID, "custom_event", "")
	}

	w := webhook.NewWorker(st, 15*time.Second)
	w.ProcessOnce(context.Background())

	mu.Lock()
	defer mu.Unlock()

	// 2 batches in one cycle: 100 + 50
	if len(batchSizes) != 2 {
		t.Fatalf("expected 2 drained batches, got %d (%v)", len(batchSizes), batchSizes)
	}
	if batchSizes[0] != 100 || batchSizes[1] != 50 {
		t.Errorf("expected batches [100, 50], got %v", batchSizes)
	}

	whUpdated, _ := st.GetWebhook(wh.ID)
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
			// click stream: 500
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		// install stream: 200
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	wh, _ := st.CreateWebhook(app.ID, srv.URL, []string{"installs", "clicks"})

	link, _ := st.CreateLink(store.Link{AppID: app.ID, Key: "k", URL: "https://example.com"})
	_, _ = st.RecordClick(store.Click{AppID: app.ID, LinkID: link.ID, Destination: "https://example.com"}, 24)
	_, _ = st.RecordInstall(store.Install{AppID: app.ID, DeviceHash: "d1", Attribution: store.AttributionOrganic})

	w := webhook.NewWorker(st, 15*time.Second)
	w.ProcessOnce(context.Background())

	whUpdated, _ := st.GetWebhook(wh.ID)

	// installs: advance
	if whUpdated.CursorInstalls <= wh.CursorInstalls {
		t.Errorf("installs cursor should have advanced, got %d", whUpdated.CursorInstalls)
	}
	if whUpdated.FailsInstalls != 0 || whUpdated.BackoffInstalls != nil {
		t.Errorf("installs stream should have 0 fails, got %d", whUpdated.FailsInstalls)
	}

	// clicks: failed -> backoff
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

// Live rows after replay start must not ride in a replay batch (would lose/steal Detur-Replay).
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

// Purge empties table -> new rows reuse rowids <= cursor; must still be delivered.
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

// Tail deleted + refilled before next tick: no rowid reuse -> each new row delivered once.
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

// Cursor > rowid high-water mark (restored older backup): rewind, new rows still delivered.
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

// Stop mid-delivery cancels the POST: returns promptly, no failure counted, no backoff, cursor unmoved (redelivered next start).
func TestWorkerStopCancelsInFlightWithoutBackoff(t *testing.T) {
	st := newTestStore(t)
	app, _ := st.CreateApp("Stop App", "dk_stop")
	entered := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body) // server notices a client hang-up only once the body is read
		entered <- struct{}{}
		<-r.Context().Done() // slow endpoint: holds until the client gives up
	}))
	defer srv.Close()
	wh, _ := st.CreateWebhook(app.ID, srv.URL, []string{"installs"})
	_, _ = st.RecordInstall(store.Install{AppID: app.ID, DeviceHash: "d1", Attribution: store.AttributionOrganic})

	w := webhook.NewWorker(st, time.Hour)
	w.Start()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("delivery never reached the endpoint")
	}
	done := make(chan struct{})
	go func() { w.Stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Stop waited out the in-flight POST")
	}
	got, _ := st.GetWebhook(wh.ID)
	if got.FailsInstalls != 0 || got.BackoffInstalls != nil || got.CursorInstalls != wh.CursorInstalls {
		t.Errorf("after stop: fails %d backoff %v cursor %d; want 0, nil, %d", got.FailsInstalls, got.BackoffInstalls, got.CursorInstalls, wh.CursorInstalls)
	}
}

// Delivery error logs never carry the endpoint URL (Slack-style tokens live in the path).
func TestWorkerFailureLogOmitsURL(t *testing.T) {
	st := newTestStore(t)
	app, _ := st.CreateApp("Log App", "dk_log")
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL + "/services/SECRET123"
	srv.Close() // connection refused
	_, _ = st.CreateWebhook(app.ID, url, []string{"installs"})
	_, _ = st.RecordInstall(store.Install{AppID: app.ID, DeviceHash: "d1", Attribution: store.AttributionOrganic})

	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	webhook.NewWorker(st, time.Hour).ProcessOnce(context.Background())
	out := buf.String()
	if !strings.Contains(out, "POST failed") || strings.Contains(out, "SECRET123") {
		t.Errorf("log = %q; want a POST failure without the URL", out)
	}
}

// Admin disables a webhook mid-cycle: the batch in flight finishes, no further batch goes to the old URL.
func TestWorkerStopsWhenDisabledMidCycle(t *testing.T) {
	st := newTestStore(t)
	app, _ := st.CreateApp("Revoke App", "dk_revoke")
	var mu sync.Mutex
	posts := 0
	var whID string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		posts++
		mu.Unlock()
		off := false
		_, _ = st.UpdateWebhook(whID, nil, nil, &off)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	wh, _ := st.CreateWebhook(app.ID, srv.URL, []string{"events"})
	whID = wh.ID
	for range 250 {
		_ = st.RecordEvent(app.ID, "custom_event", "")
	}
	webhook.NewWorker(st, 15*time.Second).ProcessOnce(context.Background())
	mu.Lock()
	defer mu.Unlock()
	if posts != 1 {
		t.Fatalf("POSTs after disable = %d, want 1", posts)
	}
}
