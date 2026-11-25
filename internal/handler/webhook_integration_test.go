//go:build integration && !naive

package handler

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/sergiodavidfv/webhook-guard/internal/guard"
	"github.com/sergiodavidfv/webhook-guard/internal/store"
	"github.com/sergiodavidfv/webhook-guard/internal/worker"
)

var testSecret = []byte("whsec_test")

type fakeInserter struct {
	calls    int
	err      error
	lastArgs river.JobArgs
}

func (f *fakeInserter) InsertTx(_ context.Context, _ pgx.Tx, args river.JobArgs, _ *river.InsertOpts) (*rivertype.JobInsertResult, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.calls++
	f.lastArgs = args
	return nil, nil
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

func unique(prefix string) string {
	return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
}

func eventBody(eventID, eventType, chargeID string) []byte {
	return []byte(fmt.Sprintf(
		`{"event_id":%q,"type":%q,"charge_id":%q,"data":{"amount_minor":1000,"currency":"USD"}}`,
		eventID, eventType, chargeID,
	))
}

func signedRequest(secret []byte, timestamp int64, body []byte) *http.Request {
	mac := hmac.New(sha256.New, secret)
	_, _ = fmt.Fprintf(mac, "%d.%s", timestamp, body)

	req := httptest.NewRequest(http.MethodPost, "/webhooks/payments", bytes.NewReader(body))
	req.Header.Set("X-Signature", fmt.Sprintf("t=%d,v1=%x", timestamp, mac.Sum(nil)))
	return req
}

func newHandler(t *testing.T, inserter JobInserter) (*Handler, *store.Store) {
	t.Helper()
	s := testStore(t)
	h := New(s, guard.New(s), inserter, testSecret, 5*time.Minute, river.QueueDefault)
	return h, s
}

func eventCount(t *testing.T, s *store.Store, eventID string) int {
	t.Helper()
	var count int
	err := s.Pool().QueryRow(context.Background(),
		`SELECT count(*) FROM webhook_events WHERE event_id = $1`, eventID).Scan(&count)
	if err != nil {
		t.Fatalf("count events: %v", err)
	}
	return count
}

func cleanupEvent(t *testing.T, s *store.Store, eventID string) {
	t.Cleanup(func() {
		if _, err := s.Pool().Exec(context.Background(),
			`DELETE FROM webhook_events WHERE event_id = $1`, eventID); err != nil {
			t.Errorf("cleanup webhook_events: %v", err)
		}
	})
}

func TestWebhook_ValidEventPersistsAndEnqueues(t *testing.T) {
	inserter := &fakeInserter{}
	h, s := newHandler(t, inserter)
	eventID := unique("evt_valid")
	cleanupEvent(t, s, eventID)

	body := eventBody(eventID, "payment.authorized", unique("ch_valid"))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, signedRequest(testSecret, time.Now().Unix(), body))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"status":"accepted"`) {
		t.Errorf("body = %s, want accepted", rec.Body.String())
	}
	if inserter.calls != 1 {
		t.Errorf("enqueue calls = %d, want 1", inserter.calls)
	}
	args, ok := inserter.lastArgs.(worker.WebhookArgs)
	if !ok || args.EventID != eventID {
		t.Errorf("enqueued args = %#v, want WebhookArgs{EventID: %q}", inserter.lastArgs, eventID)
	}
	if got := eventCount(t, s, eventID); got != 1 {
		t.Errorf("persisted events = %d, want 1", got)
	}
}

func TestWebhook_DuplicateDeliveryIsIdempotent(t *testing.T) {
	inserter := &fakeInserter{}
	h, s := newHandler(t, inserter)
	eventID := unique("evt_dup")
	cleanupEvent(t, s, eventID)

	body := eventBody(eventID, "payment.authorized", unique("ch_dup"))

	first := httptest.NewRecorder()
	h.ServeHTTP(first, signedRequest(testSecret, time.Now().Unix(), body))
	if first.Code != http.StatusOK {
		t.Fatalf("first status = %d, want 200; body = %s", first.Code, first.Body.String())
	}

	second := httptest.NewRecorder()
	h.ServeHTTP(second, signedRequest(testSecret, time.Now().Unix(), body))
	if second.Code != http.StatusOK {
		t.Fatalf("second status = %d, want 200; body = %s", second.Code, second.Body.String())
	}
	if !strings.Contains(second.Body.String(), `"status":"duplicate"`) {
		t.Errorf("second body = %s, want duplicate", second.Body.String())
	}
	if inserter.calls != 1 {
		t.Errorf("enqueue calls = %d, want 1 (duplicate must not enqueue)", inserter.calls)
	}
	if got := eventCount(t, s, eventID); got != 1 {
		t.Errorf("persisted events = %d, want 1", got)
	}
}

func TestWebhook_InvalidSignatureRejectedWithoutPersistence(t *testing.T) {
	inserter := &fakeInserter{}
	h, s := newHandler(t, inserter)
	eventID := unique("evt_badsig")
	cleanupEvent(t, s, eventID)

	body := eventBody(eventID, "payment.authorized", unique("ch_badsig"))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, signedRequest([]byte("whsec_other"), time.Now().Unix(), body))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if got := eventCount(t, s, eventID); got != 0 {
		t.Errorf("persisted events = %d, want 0", got)
	}
	if inserter.calls != 0 {
		t.Errorf("enqueue calls = %d, want 0", inserter.calls)
	}
}

func TestWebhook_ExpiredTimestampRejected(t *testing.T) {
	inserter := &fakeInserter{}
	h, s := newHandler(t, inserter)
	eventID := unique("evt_expired")
	cleanupEvent(t, s, eventID)

	body := eventBody(eventID, "payment.authorized", unique("ch_expired"))
	timestamp := time.Now().Add(-10 * time.Minute).Unix()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, signedRequest(testSecret, timestamp, body))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if got := eventCount(t, s, eventID); got != 0 {
		t.Errorf("persisted events = %d, want 0", got)
	}
}

func TestWebhook_BodyTooLarge(t *testing.T) {
	inserter := &fakeInserter{}
	h, s := newHandler(t, inserter)
	eventID := unique("evt_big")
	cleanupEvent(t, s, eventID)

	body := bytes.Repeat([]byte("a"), int(DefaultMaxBodyBytes)+1)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, signedRequest(testSecret, time.Now().Unix(), body))

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rec.Code)
	}
	if got := eventCount(t, s, eventID); got != 0 {
		t.Errorf("persisted events = %d, want 0", got)
	}
}

func TestWebhook_InvalidPayloadRejectedWithoutPersistence(t *testing.T) {
	inserter := &fakeInserter{}
	h, s := newHandler(t, inserter)
	eventID := unique("evt_nopayload")
	cleanupEvent(t, s, eventID)

	body := []byte(fmt.Sprintf(`{"event_id":%q,"type":"payment.authorized"}`, eventID))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, signedRequest(testSecret, time.Now().Unix(), body))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if got := eventCount(t, s, eventID); got != 0 {
		t.Errorf("persisted events = %d, want 0", got)
	}
}

func TestWebhook_TXFailureReturns500AndNoRow(t *testing.T) {
	inserter := &fakeInserter{err: errors.New("queue unavailable")}
	h, s := newHandler(t, inserter)
	eventID := unique("evt_txfail")
	cleanupEvent(t, s, eventID)

	body := eventBody(eventID, "payment.authorized", unique("ch_txfail"))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, signedRequest(testSecret, time.Now().Unix(), body))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body = %s", rec.Code, rec.Body.String())
	}
	if got := eventCount(t, s, eventID); got != 0 {
		t.Errorf("persisted events = %d, want 0 after rollback", got)
	}
}

func metricTotal(t *testing.T, s *store.Store, name string) int64 {
	t.Helper()
	var total int64
	err := s.Pool().QueryRow(context.Background(),
		`SELECT COALESCE(SUM(count), 0)::bigint FROM metrics WHERE name = $1`, name).Scan(&total)
	if err != nil {
		t.Fatalf("read metric %s: %v", name, err)
	}
	return total
}

func TestWebhook_MetricsRecorded(t *testing.T) {
	inserter := &fakeInserter{}
	h, s := newHandler(t, inserter)
	eventID := unique("evt_metrics")
	cleanupEvent(t, s, eventID)

	beforeReceived := metricTotal(t, s, "webhooks_received")
	beforeDuplicate := metricTotal(t, s, "webhooks_duplicate")
	beforeRejected := metricTotal(t, s, "webhooks_rejected_signature")

	body := eventBody(eventID, "payment.authorized", unique("ch_metrics"))
	accepted := httptest.NewRecorder()
	h.ServeHTTP(accepted, signedRequest(testSecret, time.Now().Unix(), body))
	duplicate := httptest.NewRecorder()
	h.ServeHTTP(duplicate, signedRequest(testSecret, time.Now().Unix(), body))
	rejected := httptest.NewRecorder()
	h.ServeHTTP(rejected, signedRequest([]byte("whsec_other"), time.Now().Unix(), body))

	if got := metricTotal(t, s, "webhooks_received") - beforeReceived; got != 2 {
		t.Errorf("webhooks_received delta = %d, want 2", got)
	}
	if got := metricTotal(t, s, "webhooks_duplicate") - beforeDuplicate; got != 1 {
		t.Errorf("webhooks_duplicate delta = %d, want 1", got)
	}
	if got := metricTotal(t, s, "webhooks_rejected_signature") - beforeRejected; got != 1 {
		t.Errorf("webhooks_rejected_signature delta = %d, want 1", got)
	}
}
