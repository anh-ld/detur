package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"slices"
	"sync"
	"time"

	"detur.dev/server/internal/store"
)

type Worker struct {
	st       *store.Store
	client   *http.Client
	interval time.Duration
	logger   *log.Logger

	mu     sync.Mutex
	stopCh chan struct{}
	doneCh chan struct{}
}

func NewWorker(st *store.Store, interval time.Duration) *Worker {
	if interval <= 0 {
		interval = 15 * time.Second
	}
	return &Worker{
		st:       st,
		client:   NewSafeHTTPClient(),
		interval: interval,
		logger:   log.Default(),
	}
}

func (w *Worker) SetClient(c *http.Client) {
	w.client = c
}

func (w *Worker) Start() {
	w.mu.Lock()
	if w.stopCh != nil {
		w.mu.Unlock()
		return
	}
	w.stopCh = make(chan struct{})
	w.doneCh = make(chan struct{})
	w.mu.Unlock()

	stopCh := w.stopCh
	go func() {
		defer close(w.doneCh)
		ticker := time.NewTicker(w.interval)
		defer ticker.Stop()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() {
			<-stopCh
			cancel()
		}()
		once := func() {
			defer func() {
				if r := recover(); r != nil {
					w.logger.Printf("webhook worker panic: %v", r)
				}
			}()
			w.ProcessOnce(ctx)
		}

		once()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				once()
			}
		}
	}()
}

func (w *Worker) Stop() {
	w.mu.Lock()
	if w.stopCh == nil {
		w.mu.Unlock()
		return
	}
	close(w.stopCh)
	w.mu.Unlock()
	<-w.doneCh
}

func (w *Worker) ProcessOnce(ctx context.Context) {
	webhooks, err := w.st.ListActiveWebhooks()
	if err != nil {
		w.logger.Printf("webhook worker list active failed: %v", err)
		return
	}
	if len(webhooks) == 0 {
		return
	}

	now := time.Now().UTC()
	for _, wh := range webhooks {
		for _, stream := range wh.Types {
			if ctx.Err() != nil {
				return
			}
			w.processStream(ctx, wh, stream, now)
		}
	}
}

const (
	batchSize       = 100
	batchesPerCycle = 10
)

func (w *Worker) processStream(ctx context.Context, wh store.Webhook, stream string, now time.Time) {
	st, ok := wh.Stream(stream)
	if !ok || (st.Backoff != nil && now.Before(*st.Backoff)) {
		return
	}

	tableMax, err := w.st.WebhookStreamMaxRowID(stream)
	if err != nil {
		w.logger.Printf("webhook %s %s max rowid failed: %v", wh.ID, stream, err)
		return
	}
	if st.Cursor > tableMax {
		w.logger.Printf("webhook %s %s: cursor %d above rowid high-water mark %d, rewinding to 0", wh.ID, stream, st.Cursor, tableMax)
		if !w.advance(wh.ID, stream, &st, 0, true) {
			return
		}
	}

	for range batchesPerCycle {
		cur, err := w.st.GetWebhook(wh.ID)
		if err != nil || !cur.Enabled || !slices.Contains(cur.Types, stream) {
			return
		}
		wh.URL, wh.Secret = cur.URL, cur.Secret
		records, err := w.st.FetchPendingWebhookBatch(wh.AppID, stream, st.Cursor, st.ReplayUntil, batchSize)
		if err != nil {
			w.logger.Printf("webhook %s fetch %s failed: %v", wh.ID, stream, err)
			return
		}
		isReplay := st.ReplayUntil > 0
		if len(records) == 0 {
			if isReplay && w.advance(wh.ID, stream, &st, st.ReplayUntil, true) {
				continue
			}
			return
		}

		maxRowID := records[len(records)-1].RowID
		isReplayComplete := isReplay && (maxRowID >= st.ReplayUntil || len(records) < batchSize)

		payloads := make([]any, len(records))
		for i, r := range records {
			payloads[i] = r.Payload
		}
		bodyBytes, err := json.Marshal(payloads)
		if err != nil {
			w.logger.Printf("webhook %s marshal failed: %v", wh.ID, err)
			return
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, wh.URL, bytes.NewReader(bodyBytes))
		if err != nil {
			w.logger.Printf("webhook %s invalid request URL", wh.ID) // url.Parse errors quote the URL (tokens)
			w.handleFailure(wh.ID, stream, st.Fails)
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Detur-Signature", Sign(bodyBytes, wh.Secret, time.Now().Unix()))
		if isReplay {
			req.Header.Set("Detur-Replay", "true")
		}

		resp, err := w.client.Do(req)
		if err != nil && ctx.Err() != nil {
			return
		}
		if err != nil {
			var ue *url.Error
			if errors.As(err, &ue) {
				err = ue.Err // *url.Error prints the URL: Slack-style endpoints carry their token in the path
			}
			w.logger.Printf("webhook %s %s POST failed: %v", wh.ID, stream, err)
			w.handleFailure(wh.ID, stream, st.Fails)
			return
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
		resp.Body.Close()

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			w.logger.Printf("webhook %s %s POST returned HTTP %d", wh.ID, stream, resp.StatusCode)
			w.handleFailure(wh.ID, stream, st.Fails)
			return
		}

		if !w.advance(wh.ID, stream, &st, maxRowID, isReplayComplete) {
			return
		}
		if len(records) < batchSize && !isReplayComplete {
			return
		}
	}
}

func (w *Worker) advance(webhookID, stream string, st *store.WebhookStream, cursor int64, replayDone bool) bool {
	if err := w.st.UpdateWebhookCursor(webhookID, stream, *st, cursor, replayDone); err != nil {
		if !errors.Is(err, store.ErrWebhookCursorMoved) {
			w.logger.Printf("webhook %s update cursor failed: %v", webhookID, err)
		}
		return false
	}
	st.Cursor, st.Fails = cursor, 0
	if replayDone {
		st.ReplayUntil = 0
	}
	return true
}

func ComputeBackoff(fails int) time.Duration {
	shift := min(max(fails, 1), 10)
	return min(30*time.Second*time.Duration(1<<shift), time.Hour)
}

func (w *Worker) handleFailure(webhookID, stream string, currentFails int) {
	newFails := currentFails + 1
	backoffDur := ComputeBackoff(newFails)
	backoffUntil := time.Now().UTC().Add(backoffDur)
	if err := w.st.RecordWebhookFailure(webhookID, stream, newFails, backoffUntil); err != nil {
		w.logger.Printf("webhook %s record failure failed: %v", webhookID, err)
	}
}
