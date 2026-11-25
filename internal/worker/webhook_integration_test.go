//go:build integration && !naive

package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/sergiodavidfv/webhook-guard/internal/guard"
	"github.com/sergiodavidfv/webhook-guard/internal/store"
)

const testBaseTimestamp int64 = 1_700_000_000

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

func beginTx(t *testing.T, s *store.Store) (context.Context, pgx.Tx) {
	t.Helper()

	ctx := context.Background()
	tx, err := s.Pool().Begin(ctx)
	if err != nil {
		t.Fatalf("begin transaction: %v", err)
	}
	t.Cleanup(func() {
		if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			t.Errorf("rollback transaction: %v", err)
		}
	})
	return ctx, tx
}

func unique(prefix string) string {
	return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
}

func sampleEvent(eventID, chargeID, eventType string, timestamp int64) store.Event {
	return store.Event{
		EventID:            eventID,
		Type:               eventType,
		ChargeID:           chargeID,
		Payload:            json.RawMessage(`{"data":{"amount_minor":1000,"currency":"USD"}}`),
		SignatureTimestamp: timestamp,
		CorrelationID:      "worker-test",
	}
}

func insertEvent(t *testing.T, s *store.Store, ctx context.Context, tx pgx.Tx, ev store.Event) {
	t.Helper()
	_, inserted, err := s.InsertEventTx(ctx, tx, ev)
	if err != nil {
		t.Fatalf("insert event %s: %v", ev.EventID, err)
	}
	if !inserted {
		t.Fatalf("event %s already existed", ev.EventID)
	}
}

func newWorker(t *testing.T, s *store.Store) *WebhookWorker {
	t.Helper()
	return NewWebhookWorker(s, guard.New(s), slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func chargeState(t *testing.T, ctx context.Context, tx pgx.Tx, chargeID string) store.ChargeState {
	t.Helper()
	var state store.ChargeState
	if err := tx.QueryRow(ctx, `SELECT state FROM charges WHERE charge_id = $1`, chargeID).Scan(&state); err != nil {
		t.Fatalf("read charge state: %v", err)
	}
	return state
}

func count(t *testing.T, ctx context.Context, tx pgx.Tx, query string, args ...any) int {
	t.Helper()
	var n int
	if err := tx.QueryRow(ctx, query, args...).Scan(&n); err != nil {
		t.Fatalf("count query: %v", err)
	}
	return n
}

func TestWorker_AppliesInOrder(t *testing.T) {
	s := testStore(t)
	ctx, tx := beginTx(t, s)
	w := newWorker(t, s)

	chargeID := unique("ch_order")
	authorized := sampleEvent(unique("evt_auth"), chargeID, "payment.authorized", testBaseTimestamp)
	captured := sampleEvent(unique("evt_cap"), chargeID, "payment.captured", testBaseTimestamp+10)
	insertEvent(t, s, ctx, tx, authorized)
	insertEvent(t, s, ctx, tx, captured)

	if _, err := w.applyEventTx(ctx, tx, authorized.EventID); err != nil {
		t.Fatalf("apply authorized: %v", err)
	}
	if state := chargeState(t, ctx, tx, chargeID); state != store.StateAuthorized {
		t.Fatalf("state after authorized = %q, want %q", state, store.StateAuthorized)
	}

	if _, err := w.applyEventTx(ctx, tx, captured.EventID); err != nil {
		t.Fatalf("apply captured: %v", err)
	}
	if state := chargeState(t, ctx, tx, chargeID); state != store.StateCaptured {
		t.Fatalf("state after captured = %q, want %q", state, store.StateCaptured)
	}
	if got := count(t, ctx, tx, `SELECT count(*) FROM ledger_entries WHERE charge_id = $1`, chargeID); got != 2 {
		t.Errorf("ledger entries = %d, want 2", got)
	}
	if got := count(t, ctx, tx, `SELECT count(*) FROM webhook_events WHERE charge_id = $1 AND applied_at IS NOT NULL`, chargeID); got != 2 {
		t.Errorf("applied events = %d, want 2", got)
	}
}

func TestWorker_OutOfOrderEventIsRejected(t *testing.T) {
	s := testStore(t)
	ctx, tx := beginTx(t, s)
	w := newWorker(t, s)

	chargeID := unique("ch_ooo")
	authorized := sampleEvent(unique("evt_auth"), chargeID, "payment.authorized", testBaseTimestamp)
	captured := sampleEvent(unique("evt_cap"), chargeID, "payment.captured", testBaseTimestamp+20)
	staleAuthorized := sampleEvent(unique("evt_stale"), chargeID, "payment.authorized", testBaseTimestamp+10)
	insertEvent(t, s, ctx, tx, authorized)
	insertEvent(t, s, ctx, tx, captured)
	insertEvent(t, s, ctx, tx, staleAuthorized)

	if _, err := w.applyEventTx(ctx, tx, authorized.EventID); err != nil {
		t.Fatalf("apply authorized: %v", err)
	}
	if _, err := w.applyEventTx(ctx, tx, captured.EventID); err != nil {
		t.Fatalf("apply captured: %v", err)
	}
	if _, err := w.applyEventTx(ctx, tx, staleAuthorized.EventID); err != nil {
		t.Fatalf("apply stale authorized: %v", err)
	}

	if state := chargeState(t, ctx, tx, chargeID); state != store.StateCaptured {
		t.Errorf("state = %q, want %q (must not move backward)", state, store.StateCaptured)
	}
	if got := count(t, ctx, tx, `SELECT count(*) FROM ledger_entries WHERE charge_id = $1`, chargeID); got != 2 {
		t.Errorf("ledger entries = %d, want 2", got)
	}
	if got := count(t, ctx, tx, `SELECT count(*) FROM late_events WHERE charge_id = $1 AND reason = 'out_of_order'`, chargeID); got != 1 {
		t.Errorf("late events = %d, want 1", got)
	}
	if got := count(t, ctx, tx, `SELECT count(*) FROM webhook_events WHERE event_id = $1 AND applied_at IS NOT NULL`, staleAuthorized.EventID); got != 1 {
		t.Errorf("stale event marked applied = %d, want 1", got)
	}
}

func TestWorker_LateEventGoesToLateEvents(t *testing.T) {
	s := testStore(t)
	ctx, tx := beginTx(t, s)
	w := newWorker(t, s)

	chargeID := unique("ch_late")
	authorized := sampleEvent(unique("evt_auth"), chargeID, "payment.authorized", testBaseTimestamp)
	captured := sampleEvent(unique("evt_cap"), chargeID, "payment.captured", testBaseTimestamp+10)
	refunded := sampleEvent(unique("evt_refund"), chargeID, "payment.refunded", testBaseTimestamp+20)
	afterClose := sampleEvent(unique("evt_after"), chargeID, "payment.captured", testBaseTimestamp+30)
	for _, ev := range []store.Event{authorized, captured, refunded, afterClose} {
		insertEvent(t, s, ctx, tx, ev)
	}

	for _, ev := range []store.Event{authorized, captured, refunded} {
		if _, err := w.applyEventTx(ctx, tx, ev.EventID); err != nil {
			t.Fatalf("apply %s: %v", ev.Type, err)
		}
	}
	if _, err := w.applyEventTx(ctx, tx, afterClose.EventID); err != nil {
		t.Fatalf("apply after close: %v", err)
	}

	if state := chargeState(t, ctx, tx, chargeID); state != store.StateRefunded {
		t.Errorf("state = %q, want %q", state, store.StateRefunded)
	}
	if got := count(t, ctx, tx, `SELECT count(*) FROM ledger_entries WHERE charge_id = $1`, chargeID); got != 3 {
		t.Errorf("ledger entries = %d, want 3", got)
	}
	if got := count(t, ctx, tx, `SELECT count(*) FROM late_events WHERE charge_id = $1 AND reason = 'charge_closed'`, chargeID); got != 1 {
		t.Errorf("late closed events = %d, want 1", got)
	}
}

func TestWorker_CreatedEventCreatesCharge(t *testing.T) {
	s := testStore(t)
	ctx, tx := beginTx(t, s)
	w := newWorker(t, s)

	chargeID := unique("ch_created")
	created := sampleEvent(unique("evt_created"), chargeID, "payment.created", testBaseTimestamp)
	createdAgain := sampleEvent(unique("evt_created_again"), chargeID, "payment.created", testBaseTimestamp+50)
	authorized := sampleEvent(unique("evt_created_auth"), chargeID, "payment.authorized", testBaseTimestamp+20)
	insertEvent(t, s, ctx, tx, created)
	insertEvent(t, s, ctx, tx, createdAgain)
	insertEvent(t, s, ctx, tx, authorized)

	if _, err := w.applyEventTx(ctx, tx, created.EventID); err != nil {
		t.Fatalf("apply created: %v", err)
	}

	if state := chargeState(t, ctx, tx, chargeID); state != store.StateCreated {
		t.Fatalf("state after created = %q, want %q", state, store.StateCreated)
	}

	var amount int64
	var currency, lastEventID string
	if err := tx.QueryRow(ctx,
		`SELECT amount_minor, currency, last_event_id FROM charges WHERE charge_id = $1`, chargeID,
	).Scan(&amount, &currency, &lastEventID); err != nil {
		t.Fatalf("read charge: %v", err)
	}
	if amount != 1000 || currency != "USD" || lastEventID != created.EventID {
		t.Errorf("charge = amount:%d currency:%q last_event:%q, want 1000/USD/%s",
			amount, currency, lastEventID, created.EventID)
	}
	if got := count(t, ctx, tx, `SELECT count(*) FROM ledger_entries WHERE charge_id = $1`, chargeID); got != 0 {
		t.Errorf("ledger entries after created = %d, want 0", got)
	}
	if got := count(t, ctx, tx,
		`SELECT count(*) FROM webhook_events WHERE event_id = $1 AND applied_at IS NOT NULL`, created.EventID); got != 1 {
		t.Errorf("created event applied = %d, want 1", got)
	}

	// A repeated created on an existing charge is a no-op: it must not raise the
	// ordering watermark and hide the later authorized event.
	if _, err := w.applyEventTx(ctx, tx, createdAgain.EventID); err != nil {
		t.Fatalf("apply repeated created: %v", err)
	}
	if state := chargeState(t, ctx, tx, chargeID); state != store.StateCreated {
		t.Errorf("state after repeated created = %q, want %q", state, store.StateCreated)
	}
	if got := count(t, ctx, tx, `SELECT count(*) FROM ledger_entries WHERE charge_id = $1`, chargeID); got != 0 {
		t.Errorf("ledger entries after repeated created = %d, want 0", got)
	}

	if _, err := w.applyEventTx(ctx, tx, authorized.EventID); err != nil {
		t.Fatalf("apply authorized: %v", err)
	}
	if state := chargeState(t, ctx, tx, chargeID); state != store.StateAuthorized {
		t.Errorf("state after authorized = %q, want %q", state, store.StateAuthorized)
	}
	if got := count(t, ctx, tx, `SELECT count(*) FROM ledger_entries WHERE charge_id = $1`, chargeID); got != 1 {
		t.Errorf("ledger entries after authorized = %d, want 1", got)
	}
}

func TestWorker_RejectedEventDoesNotBlockLaterValidEvent(t *testing.T) {
	s := testStore(t)
	ctx, tx := beginTx(t, s)
	w := newWorker(t, s)

	chargeID := unique("ch_ordering")
	authorized := sampleEvent(unique("evt_order_auth"), chargeID, "payment.authorized", testBaseTimestamp)
	refunded := sampleEvent(unique("evt_order_refund"), chargeID, "payment.refunded", testBaseTimestamp+20)
	captured := sampleEvent(unique("evt_order_cap"), chargeID, "payment.captured", testBaseTimestamp+10)
	insertEvent(t, s, ctx, tx, authorized)
	insertEvent(t, s, ctx, tx, refunded)
	insertEvent(t, s, ctx, tx, captured)

	if _, err := w.applyEventTx(ctx, tx, authorized.EventID); err != nil {
		t.Fatalf("apply authorized: %v", err)
	}
	if _, err := w.applyEventTx(ctx, tx, refunded.EventID); err != nil {
		t.Fatalf("apply rejected refund: %v", err)
	}
	if _, err := w.applyEventTx(ctx, tx, captured.EventID); err != nil {
		t.Fatalf("apply captured: %v", err)
	}

	if state := chargeState(t, ctx, tx, chargeID); state != store.StateCaptured {
		t.Errorf("state = %q, want %q (valid event must not be diverted by a rejection)", state, store.StateCaptured)
	}
	if got := count(t, ctx, tx, `SELECT count(*) FROM ledger_entries WHERE charge_id = $1`, chargeID); got != 2 {
		t.Errorf("ledger entries = %d, want 2", got)
	}
	if got := count(t, ctx, tx, `SELECT count(*) FROM late_events WHERE charge_id = $1`, chargeID); got != 0 {
		t.Errorf("late events = %d, want 0", got)
	}
}

func TestWorker_RetryIsIdempotent(t *testing.T) {
	s := testStore(t)
	ctx, tx := beginTx(t, s)
	w := newWorker(t, s)

	chargeID := unique("ch_retry")
	authorized := sampleEvent(unique("evt_auth"), chargeID, "payment.authorized", testBaseTimestamp)
	captured := sampleEvent(unique("evt_cap"), chargeID, "payment.captured", testBaseTimestamp+10)
	insertEvent(t, s, ctx, tx, authorized)
	insertEvent(t, s, ctx, tx, captured)

	if _, err := w.applyEventTx(ctx, tx, authorized.EventID); err != nil {
		t.Fatalf("apply authorized: %v", err)
	}
	if _, err := w.applyEventTx(ctx, tx, captured.EventID); err != nil {
		t.Fatalf("apply captured: %v", err)
	}

	if _, err := w.applyEventTx(ctx, tx, captured.EventID); err != nil {
		t.Fatalf("retry apply captured: %v", err)
	}
	if got := count(t, ctx, tx, `SELECT count(*) FROM ledger_entries WHERE charge_id = $1`, chargeID); got != 2 {
		t.Errorf("ledger entries after retry = %d, want 2", got)
	}

	// Simulate a crash between the ledger append and the applied flag: roll the
	// charge back one state and re-run the job. The duplicate ledger entry must
	// be treated as already applied.
	if _, err := tx.Exec(ctx, `UPDATE charges SET state = 'authorized' WHERE charge_id = $1`, chargeID); err != nil {
		t.Fatalf("rewind charge: %v", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE webhook_events SET applied_at = NULL WHERE event_id = $1`, captured.EventID); err != nil {
		t.Fatalf("clear applied flag: %v", err)
	}
	if _, err := w.applyEventTx(ctx, tx, captured.EventID); err != nil {
		t.Fatalf("apply with existing ledger entry: %v", err)
	}

	if state := chargeState(t, ctx, tx, chargeID); state != store.StateCaptured {
		t.Errorf("state = %q, want %q", state, store.StateCaptured)
	}
	if got := count(t, ctx, tx, `SELECT count(*) FROM ledger_entries WHERE charge_id = $1`, chargeID); got != 2 {
		t.Errorf("ledger entries after duplicate handling = %d, want 2", got)
	}
}

func TestWorker_RefundAmountDoesNotOverwriteChargeAmount(t *testing.T) {
	s := testStore(t)
	ctx, tx := beginTx(t, s)
	w := newWorker(t, s)

	chargeID := unique("ch_amount")
	created := sampleEvent(unique("evt_created"), chargeID, "payment.created", testBaseTimestamp)
	authorized := sampleEvent(unique("evt_auth"), chargeID, "payment.authorized", testBaseTimestamp+10)
	captured := sampleEvent(unique("evt_cap"), chargeID, "payment.captured", testBaseTimestamp+20)
	for _, ev := range []store.Event{created, authorized, captured} {
		insertEvent(t, s, ctx, tx, ev)
	}
	for _, ev := range []store.Event{created, authorized, captured} {
		if _, err := w.applyEventTx(ctx, tx, ev.EventID); err != nil {
			t.Fatalf("apply %s: %v", ev.Type, err)
		}
	}

	// A refund whose data.amount_minor is the movement amount (a partial
	// refund), not the charge amount.
	refunded := sampleEvent(unique("evt_refund"), chargeID, "payment.refunded", testBaseTimestamp+30)
	var payload struct {
		Data struct {
			AmountMinor int64  `json:"amount_minor"`
			Currency    string `json:"currency"`
		} `json:"data"`
	}
	payload.Data.AmountMinor = 300
	payload.Data.Currency = "USD"
	raw, err := json.Marshal(struct {
		Data any `json:"data"`
	}{Data: payload.Data})
	if err != nil {
		t.Fatalf("marshal refund payload: %v", err)
	}
	refunded.Payload = raw
	insertEvent(t, s, ctx, tx, refunded)

	if _, err := w.applyEventTx(ctx, tx, refunded.EventID); err != nil {
		t.Fatalf("apply refund: %v", err)
	}

	var amount int64
	var currency string
	if err := tx.QueryRow(ctx,
		`SELECT amount_minor, currency FROM charges WHERE charge_id = $1`, chargeID,
	).Scan(&amount, &currency); err != nil {
		t.Fatalf("read charge: %v", err)
	}
	if amount != 1000 || currency != "USD" {
		t.Errorf("charge after refund = %d %s, want 1000 USD (original amount kept)", amount, currency)
	}
	var ledgerAmount int64
	if err := tx.QueryRow(ctx,
		`SELECT amount_minor FROM ledger_entries WHERE source_event_id = $1`, refunded.EventID,
	).Scan(&ledgerAmount); err != nil {
		t.Fatalf("read ledger entry: %v", err)
	}
	if ledgerAmount != 300 {
		t.Errorf("ledger entry = %d, want 300 (movement amount)", ledgerAmount)
	}
}

func TestWorker_SameSecondEventsApplyInArrivalOrder(t *testing.T) {
	s := testStore(t)
	ctx, tx := beginTx(t, s)
	w := newWorker(t, s)

	chargeID := unique("ch_same_second")
	authorized := sampleEvent(unique("evt_auth"), chargeID, "payment.authorized", testBaseTimestamp)
	captured := sampleEvent(unique("evt_cap"), chargeID, "payment.captured", testBaseTimestamp)
	insertEvent(t, s, ctx, tx, authorized)
	insertEvent(t, s, ctx, tx, captured)

	// Two events signed in the same second apply in arrival order; neither is
	// classified as late. This documents the watermark's second resolution.
	if _, err := w.applyEventTx(ctx, tx, authorized.EventID); err != nil {
		t.Fatalf("apply authorized: %v", err)
	}
	if _, err := w.applyEventTx(ctx, tx, captured.EventID); err != nil {
		t.Fatalf("apply captured: %v", err)
	}

	if state := chargeState(t, ctx, tx, chargeID); state != store.StateCaptured {
		t.Errorf("state = %q, want %q", state, store.StateCaptured)
	}
	if got := count(t, ctx, tx, `SELECT count(*) FROM late_events WHERE charge_id = $1`, chargeID); got != 0 {
		t.Errorf("late events = %d, want 0", got)
	}
}

func TestWorker_EmptyChargeIDIsRejectedNotRetried(t *testing.T) {
	s := testStore(t)
	ctx, tx := beginTx(t, s)
	w := newWorker(t, s)

	eventID := unique("evt_nocharge")
	ev := sampleEvent(eventID, "", "payment.authorized", testBaseTimestamp)
	insertEvent(t, s, ctx, tx, ev)

	poison, err := w.applyEventTx(ctx, tx, eventID)
	if err != nil {
		t.Fatalf("applyEventTx() error = %v", err)
	}
	if !poison {
		t.Fatal("poison = false, want true for missing charge_id")
	}

	// The rejection commits: the event is marked applied and counted, so
	// replay never picks it up again.
	if got := count(t, ctx, tx, `SELECT count(*) FROM webhook_events WHERE event_id = $1 AND applied_at IS NOT NULL`, eventID); got != 1 {
		t.Errorf("applied = %d, want 1", got)
	}
}
