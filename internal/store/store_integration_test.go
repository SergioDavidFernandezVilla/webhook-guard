//go:build integration

package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func testStore(t *testing.T) *Store {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://webhook:webhook@localhost:5432/webhook_guard?sslmode=disable"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	s, err := Open(ctx, dsn, OpenOptions{})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(s.Close)
	return s
}

// beginTx opens a transaction that is always rolled back, so integration tests
// leave no residue (the ledger is append-only and cannot be cleaned up later).
func beginTx(t *testing.T, s *Store) (context.Context, pgx.Tx) {
	t.Helper()

	ctx := context.Background()
	tx, err := s.pool.Begin(ctx)
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

func sampleEvent(eventID, chargeID string) Event {
	return Event{
		EventID:            eventID,
		Type:               "payment.captured",
		ChargeID:           chargeID,
		Payload:            json.RawMessage(`{"amount":1000,"currency":"USD"}`),
		SignatureTimestamp: time.Now().Unix(),
		CorrelationID:      "test",
	}
}

func TestInsertEventTx_DuplicateIsIdempotent(t *testing.T) {
	s := testStore(t)
	ctx, tx := beginTx(t, s)
	ev := sampleEvent(unique("evt_dup"), unique("ch"))

	id, inserted, err := s.InsertEventTx(ctx, tx, ev)
	if err != nil {
		t.Fatalf("InsertEventTx() error = %v", err)
	}
	if !inserted || id == 0 {
		t.Fatalf("first insert: inserted = %v, id = %d; want true and non-zero", inserted, id)
	}

	id, inserted, err = s.InsertEventTx(ctx, tx, ev)
	if err != nil {
		t.Fatalf("second InsertEventTx() error = %v", err)
	}
	if inserted || id != 0 {
		t.Errorf("second insert: inserted = %v, id = %d; want false and 0", inserted, id)
	}

	var count int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM webhook_events WHERE event_id = $1`, ev.EventID).Scan(&count); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if count != 1 {
		t.Errorf("rows for event_id = %d, want 1", count)
	}
}

func TestAppendTx_DuplicateReturnsErrDuplicate(t *testing.T) {
	s := testStore(t)
	ctx, tx := beginTx(t, s)

	chargeID := unique("ch")
	eventID := unique("evt")
	if err := s.UpsertTx(ctx, tx, Charge{
		ChargeID:    chargeID,
		State:       StateCaptured,
		AmountMinor: 1000,
		Currency:    "USD",
		LastEventID: eventID,
	}); err != nil {
		t.Fatalf("UpsertTx() error = %v", err)
	}

	entry := LedgerEntry{
		ChargeID:      chargeID,
		EntryType:     StateCaptured,
		AmountMinor:   1000,
		Currency:      "USD",
		SourceEventID: eventID,
	}
	if _, err := s.AppendTx(ctx, tx, entry); err != nil {
		t.Fatalf("first AppendTx() error = %v", err)
	}
	if _, err := s.AppendTx(ctx, tx, entry); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("second AppendTx() error = %v, want ErrDuplicate", err)
	}
}

func TestRunInTx_RollsBackOnError(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	ev := sampleEvent(unique("evt_rollback"), unique("ch"))
	sentinel := errors.New("boom")

	err := s.RunInTx(ctx, func(txCtx context.Context, tx pgx.Tx) error {
		if _, _, err := s.InsertEventTx(txCtx, tx, ev); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("RunInTx() error = %v, want sentinel", err)
	}

	var count int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM webhook_events WHERE event_id = $1`, ev.EventID).Scan(&count); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if count != 0 {
		t.Errorf("rows for event_id = %d, want 0 after rollback", count)
	}
}

func TestLedgerEntries_RejectUpdate(t *testing.T) {
	s := testStore(t)
	ctx, tx := beginTx(t, s)

	chargeID := unique("ch")
	eventID := unique("evt")
	if err := s.UpsertTx(ctx, tx, Charge{
		ChargeID:    chargeID,
		State:       StateCaptured,
		AmountMinor: 1000,
		Currency:    "USD",
		LastEventID: eventID,
	}); err != nil {
		t.Fatalf("UpsertTx() error = %v", err)
	}
	if _, err := s.AppendTx(ctx, tx, LedgerEntry{
		ChargeID:      chargeID,
		EntryType:     StateCaptured,
		AmountMinor:   1000,
		Currency:      "USD",
		SourceEventID: eventID,
	}); err != nil {
		t.Fatalf("AppendTx() error = %v", err)
	}

	_, err := tx.Exec(ctx, `UPDATE ledger_entries SET amount_minor = amount_minor + 1 WHERE source_event_id = $1`, eventID)
	if err == nil {
		t.Fatal("UPDATE ledger_entries succeeded, want append-only trigger error")
	}
	if !strings.Contains(err.Error(), "append-only") {
		t.Errorf("UPDATE error = %v, want it to mention append-only", err)
	}
}

func TestGetForUpdateTx_NotFound(t *testing.T) {
	s := testStore(t)
	ctx, tx := beginTx(t, s)

	if _, err := s.GetForUpdateTx(ctx, tx, unique("missing")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetForUpdateTx() error = %v, want ErrNotFound", err)
	}
}

func TestCharge_LockAndReadRoundTrip(t *testing.T) {
	s := testStore(t)
	ctx, tx := beginTx(t, s)

	chargeID := unique("ch")
	if err := s.LockChargeTx(ctx, tx, chargeID); err != nil {
		t.Fatalf("LockChargeTx() error = %v", err)
	}
	if err := s.UpsertTx(ctx, tx, Charge{
		ChargeID:    chargeID,
		State:       StateAuthorized,
		AmountMinor: 2500,
		Currency:    "EUR",
		LastEventID: unique("evt"),
	}); err != nil {
		t.Fatalf("UpsertTx() error = %v", err)
	}

	got, err := s.GetForUpdateTx(ctx, tx, chargeID)
	if err != nil {
		t.Fatalf("GetForUpdateTx() error = %v", err)
	}
	if got.State != StateAuthorized {
		t.Errorf("state = %q, want %q", got.State, StateAuthorized)
	}
	if got.AmountMinor != 2500 || got.Currency != "EUR" {
		t.Errorf("amount/currency = %d/%q, want 2500/EUR", got.AmountMinor, got.Currency)
	}
}

func TestLateEvent_InsertIsIdempotent(t *testing.T) {
	s := testStore(t)
	ctx, tx := beginTx(t, s)

	ev := LateEvent{
		EventID:  unique("late"),
		ChargeID: unique("ch"),
		Reason:   "charge closed",
		Payload:  json.RawMessage(`{"late":true}`),
	}
	inserted, err := s.InsertTx(ctx, tx, ev)
	if err != nil {
		t.Fatalf("first InsertTx() error = %v", err)
	}
	if !inserted {
		t.Fatal("first InsertTx() inserted = false, want true")
	}

	inserted, err = s.InsertTx(ctx, tx, ev)
	if err != nil {
		t.Fatalf("second InsertTx() error = %v", err)
	}
	if inserted {
		t.Error("second InsertTx() inserted = true, want false")
	}
}

func TestMetric_AddAccumulates(t *testing.T) {
	s := testStore(t)
	ctx, tx := beginTx(t, s)

	m := Metric{
		Name:          unique("metric"),
		WindowStart:   time.Now().UTC().Truncate(time.Minute),
		WindowSeconds: 60,
		Count:         2,
	}
	if err := s.AddTx(ctx, tx, m); err != nil {
		t.Fatalf("first AddTx() error = %v", err)
	}
	m.Count = 3
	if err := s.AddTx(ctx, tx, m); err != nil {
		t.Fatalf("second AddTx() error = %v", err)
	}

	var got int64
	if err := tx.QueryRow(ctx, `
		SELECT count FROM metrics WHERE name = $1 AND window_start = $2 AND window_seconds = $3`,
		m.Name, m.WindowStart, m.WindowSeconds,
	).Scan(&got); err != nil {
		t.Fatalf("read metric: %v", err)
	}
	if got != 5 {
		t.Errorf("metric count = %d, want 5", got)
	}
}

func TestRunInTx_RollbackSurvivesCancelledContext(t *testing.T) {
	s := testStore(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := s.RunInTx(ctx, func(context.Context, pgx.Tx) error {
		// Simulate a client hang-up: the context is cancelled while the
		// transaction is still open.
		cancel()
		return errors.New("boom")
	})
	if err == nil {
		t.Fatal("RunInTx() error = nil, want the fn error")
	}
	if strings.Contains(err.Error(), "rollback transaction") {
		t.Errorf("rollback failed on a cancelled context: %v", err)
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("RunInTx() error = %v, want it to keep the fn error", err)
	}
}

func TestLoadgenRuns_InsertListAndLatest(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	guarded := LoadgenRun{
		Route:       "guarded",
		Mode:        "duplicate",
		Seed:        1,
		Target:      "http://app:8080",
		Events:      10,
		Concurrency: 2,
		Sent:        20,
		Accepted:    10,
		Duplicates:  10,
		P50MS:       1.5,
	}
	first, err := s.InsertLoadgenRun(ctx, guarded)
	if err != nil {
		t.Fatalf("InsertLoadgenRun(guarded) error = %v", err)
	}
	if first.ID == 0 || first.CreatedAt.IsZero() {
		t.Fatalf("stored run = %+v, want id and created_at", first)
	}

	naive := LoadgenRun{Route: "naive", Mode: "duplicate", Seed: 2, Events: 10, Concurrency: 2, Sent: 20, ServerErrors: 10}
	second, err := s.InsertLoadgenRun(ctx, naive)
	if err != nil {
		t.Fatalf("InsertLoadgenRun(naive) error = %v", err)
	}

	newestGuarded := LoadgenRun{Route: "guarded", Mode: "normal", Seed: 3, Events: 5, Concurrency: 1, Sent: 5, Accepted: 5}
	third, err := s.InsertLoadgenRun(ctx, newestGuarded)
	if err != nil {
		t.Fatalf("InsertLoadgenRun(newest guarded) error = %v", err)
	}

	t.Cleanup(func() {
		if _, err := s.pool.Exec(context.Background(),
			`DELETE FROM loadgen_runs WHERE id = ANY($1)`,
			[]int64{first.ID, second.ID, third.ID},
		); err != nil {
			t.Errorf("cleanup loadgen runs: %v", err)
		}
	})

	runs, err := s.ListLoadgenRuns(ctx, 100)
	if err != nil {
		t.Fatalf("ListLoadgenRuns() error = %v", err)
	}
	if len(runs) < 3 {
		t.Fatalf("ListLoadgenRuns() returned %d runs, want at least 3", len(runs))
	}

	latest, err := s.LatestLoadgenRuns(ctx)
	if err != nil {
		t.Fatalf("LatestLoadgenRuns() error = %v", err)
	}
	if len(latest) != 2 {
		t.Fatalf("LatestLoadgenRuns() returned %d runs, want 2 (one per route)", len(latest))
	}
	byRoute := make(map[string]LoadgenRun, len(latest))
	for _, run := range latest {
		byRoute[run.Route] = run
	}
	if byRoute["guarded"].ID != third.ID {
		t.Errorf("latest guarded id = %d, want %d", byRoute["guarded"].ID, third.ID)
	}
	if byRoute["naive"].ID != second.ID {
		t.Errorf("latest naive id = %d, want %d", byRoute["naive"].ID, second.ID)
	}
}
