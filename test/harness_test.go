//go:build integration

package integration

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/sergiodavidfv/webhook-guard/internal/guard"
	"github.com/sergiodavidfv/webhook-guard/internal/handler"
	"github.com/sergiodavidfv/webhook-guard/internal/store"
	"github.com/sergiodavidfv/webhook-guard/internal/worker"
)

var (
	testSecret = []byte("whsec_integration")
	sequence   atomic.Int64
)

func unique(prefix string) string {
	return fmt.Sprintf("%s_%d_%d", prefix, time.Now().UnixNano(), sequence.Add(1))
}

func testStore(t *testing.T) *store.Store {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://webhook:webhook@localhost:5432/webhook_guard?sslmode=disable"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	s, err := store.Open(ctx, dsn, store.OpenOptions{})
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(s.Close)
	return s
}

// recorder is a JobInserter that remembers enqueued event ids instead of using
// River, so scenarios can process them synchronously.
type recorder struct {
	mu  sync.Mutex
	ids []string
}

func (r *recorder) InsertTx(_ context.Context, _ pgx.Tx, args river.JobArgs, _ *river.InsertOpts) (*rivertype.JobInsertResult, error) {
	webhookArgs, ok := args.(worker.WebhookArgs)
	if !ok {
		return nil, fmt.Errorf("unexpected job args %T", args)
	}
	r.mu.Lock()
	r.ids = append(r.ids, webhookArgs.EventID)
	r.mu.Unlock()
	return nil, nil
}

func (r *recorder) eventIDs() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.ids...)
}

func newHarness(t *testing.T) (*handler.Handler, *store.Store, *recorder) {
	t.Helper()
	s := testStore(t)
	rec := &recorder{}
	h := handler.New(s, guard.New(s), rec, testSecret, 5*time.Minute, "default")
	return h, s, rec
}

func webhookBody(eventID, eventType, chargeID string) []byte {
	return []byte(fmt.Sprintf(
		`{"event_id":%q,"type":%q,"charge_id":%q,"data":{"amount_minor":1000,"currency":"USD"}}`,
		eventID, eventType, chargeID,
	))
}

func signedRequest(secret []byte, timestamp int64, body []byte) *http.Request {
	mac := hmac.New(sha256.New, secret)
	_, _ = fmt.Fprintf(mac, "%d.", timestamp)
	mac.Write(body)

	req := httptest.NewRequest(http.MethodPost, "/webhooks/payments", bytes.NewReader(body))
	req.Header.Set("X-Signature", fmt.Sprintf("t=%d,v1=%x", timestamp, mac.Sum(nil)))
	return req
}

func send(t *testing.T, h *handler.Handler, secret []byte, timestamp int64, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, signedRequest(secret, timestamp, body))
	return rec
}

func responseStatus(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var payload struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		return ""
	}
	return payload.Status
}

func processEvents(t *testing.T, s *store.Store, ids []string) {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	w := worker.NewWebhookWorker(s, guard.New(s), logger)
	for _, id := range ids {
		job := &river.Job[worker.WebhookArgs]{Args: worker.WebhookArgs{EventID: id}}
		if err := w.Work(context.Background(), job); err != nil {
			t.Fatalf("process event %s: %v", id, err)
		}
	}
}

func cleanup(t *testing.T, s *store.Store, eventIDs, chargeIDs []string) {
	t.Cleanup(func() {
		ctx := context.Background()
		for _, id := range eventIDs {
			if _, err := s.Pool().Exec(ctx, `DELETE FROM webhook_events WHERE event_id = $1`, id); err != nil {
				t.Errorf("cleanup event %s: %v", id, err)
			}
		}
		for _, id := range chargeIDs {
			if _, err := s.Pool().Exec(ctx, `DELETE FROM late_events WHERE charge_id = $1`, id); err != nil {
				t.Errorf("cleanup late events %s: %v", id, err)
			}
		}
	})
}

func countRows(t *testing.T, s *store.Store, query string, args ...any) int {
	t.Helper()
	var count int
	if err := s.Pool().QueryRow(context.Background(), query, args...).Scan(&count); err != nil {
		t.Fatalf("count query: %v", err)
	}
	return count
}

//nolint:unused // used by the guarded integration tests, not the naive build.
func eventCount(t *testing.T, s *store.Store, eventID string) int {
	t.Helper()
	return countRows(t, s, `SELECT count(*) FROM webhook_events WHERE event_id = $1`, eventID)
}

func ledgerCount(t *testing.T, s *store.Store, chargeID string) int {
	t.Helper()
	return countRows(t, s, `SELECT count(*) FROM ledger_entries WHERE charge_id = $1`, chargeID)
}

func chargeState(t *testing.T, s *store.Store, chargeID string) store.ChargeState {
	t.Helper()
	var state store.ChargeState
	if err := s.Pool().QueryRow(context.Background(),
		`SELECT state FROM charges WHERE charge_id = $1`, chargeID).Scan(&state); err != nil {
		t.Fatalf("read charge state: %v", err)
	}
	return state
}

func metricTotal(t *testing.T, s *store.Store, name string) int64 {
	t.Helper()
	var total int64
	if err := s.Pool().QueryRow(context.Background(),
		`SELECT COALESCE(SUM(count), 0)::bigint FROM metrics WHERE name = $1`, name).Scan(&total); err != nil {
		t.Fatalf("read metric %s: %v", name, err)
	}
	return total
}

//nolint:unused // used by the guarded integration tests, not the naive build.
func lateEvents(t *testing.T, s *store.Store, chargeID string) (count int, reason string, resolved bool) {
	t.Helper()
	err := s.Pool().QueryRow(context.Background(),
		`SELECT count(*), COALESCE(MAX(reason), ''), COALESCE(bool_or(resolved_at IS NOT NULL), false)
		 FROM late_events WHERE charge_id = $1`, chargeID).Scan(&count, &reason, &resolved)
	if err != nil {
		t.Fatalf("read late events: %v", err)
	}
	return count, reason, resolved
}
