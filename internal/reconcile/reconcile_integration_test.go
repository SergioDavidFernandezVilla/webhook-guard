//go:build integration

package reconcile

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/sergiodavidfv/webhook-guard/internal/store"
)

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

var eventSeq atomic.Int64

func pendingEvent(prefix string) store.Event {
	suffix := fmt.Sprintf("%d_%d", time.Now().UnixNano(), eventSeq.Add(1))
	return store.Event{
		EventID:            fmt.Sprintf("%s_%s", prefix, suffix),
		Type:               "payment.authorized",
		ChargeID:           fmt.Sprintf("ch_%s_%s", prefix, suffix),
		Payload:            json.RawMessage(`{"data":{"amount_minor":1000,"currency":"USD"}}`),
		SignatureTimestamp: time.Now().Unix(),
		CorrelationID:      "reconcile-test",
	}
}

func insertPending(t *testing.T, s *store.Store, ctx context.Context, ev store.Event) {
	t.Helper()
	err := s.RunInTx(ctx, func(txCtx context.Context, tx pgx.Tx) error {
		_, inserted, insertErr := s.InsertEventTx(txCtx, tx, ev)
		if insertErr != nil {
			return insertErr
		}
		if !inserted {
			return fmt.Errorf("event %s already existed", ev.EventID)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("insert pending event %s: %v", ev.EventID, err)
	}
}

func cleanupEvents(t *testing.T, s *store.Store, ids ...string) {
	t.Cleanup(func() {
		for _, id := range ids {
			if _, err := s.Pool().Exec(context.Background(),
				`DELETE FROM webhook_events WHERE event_id = $1`, id); err != nil {
				t.Errorf("cleanup event %s: %v", id, err)
			}
		}
	})
}

func TestImportRejectsBadChecksum(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	export := Export{
		BatchID:  unique("batch_bad"),
		Checksum: "deadbeef",
		Charges:  []ExportCharge{exportCharge(unique("ch_bad"), "captured", 1000)},
	}
	raw, err := json.Marshal(export)
	if err != nil {
		t.Fatalf("marshal export: %v", err)
	}
	if _, err := Import(ctx, s, bytes.NewReader(raw)); err == nil {
		t.Fatal("Import() error = nil, want checksum error")
	}
}

func TestImportAndDiff_WritesReportAndKeepsLedger(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	chargeID := unique("ch_diff")
	eventID := unique("evt_diff")
	authorizedEventID := unique("evt_diff_auth")
	err := s.RunInTx(ctx, func(txCtx context.Context, tx pgx.Tx) error {
		if err := s.UpsertTx(txCtx, tx, store.Charge{
			ChargeID:    chargeID,
			State:       store.StateCaptured,
			AmountMinor: 1000,
			Currency:    "USD",
			LastEventID: eventID,
		}); err != nil {
			return err
		}
		// Two transitions leave a ledger sum of 2000 for a 1000 charge: Diff must
		// compare the charge amount, not the append-only audit sum.
		for _, entry := range []store.LedgerEntry{
			{ChargeID: chargeID, EntryType: store.StateAuthorized, AmountMinor: 1000, Currency: "USD", SourceEventID: authorizedEventID},
			{ChargeID: chargeID, EntryType: store.StateCaptured, AmountMinor: 1000, Currency: "USD", SourceEventID: eventID},
		} {
			if _, err := s.AppendIfAbsentTx(txCtx, tx, entry); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("seed charge and ledger: %v", err)
	}

	beforeTotals, err := s.LedgerTotals(ctx)
	if err != nil {
		t.Fatalf("ledger totals before: %v", err)
	}

	batchID := unique("batch_diff")
	charges := []ExportCharge{
		exportCharge(chargeID, "captured", 1000),
		exportCharge(unique("ch_only_remote"), "authorized", 500),
	}
	chargesRaw, err := json.Marshal(charges)
	if err != nil {
		t.Fatalf("marshal charges: %v", err)
	}
	exportRaw, err := json.Marshal(struct {
		BatchID  string          `json:"batch_id"`
		Checksum string          `json:"checksum"`
		Charges  json.RawMessage `json:"charges"`
	}{BatchID: batchID, Checksum: checksumHex(chargesRaw), Charges: chargesRaw})
	if err != nil {
		t.Fatalf("marshal export: %v", err)
	}
	exportID, err := Import(ctx, s, bytes.NewReader(exportRaw))
	if err != nil {
		t.Fatalf("Import() error = %v", err)
	}
	t.Cleanup(func() {
		if _, err := s.Pool().Exec(context.Background(), `DELETE FROM reconciliation_exports WHERE id = $1`, exportID); err != nil {
			t.Errorf("cleanup export: %v", err)
		}
		if _, err := s.Pool().Exec(context.Background(), `DELETE FROM reconciliation_reports WHERE batch_id = $1`, batchID); err != nil {
			t.Errorf("cleanup report: %v", err)
		}
	})

	report, err := Diff(ctx, s, exportID)
	if err != nil {
		t.Fatalf("Diff() error = %v", err)
	}
	if report.Counts[CategoryOK] != 1 {
		t.Errorf("ok = %d, want 1", report.Counts[CategoryOK])
	}
	if report.Counts[CategoryMissingLocal] != 1 {
		t.Errorf("missing_local = %d, want 1", report.Counts[CategoryMissingLocal])
	}
	if report.Counts[CategoryAmountMismatch] != 0 || report.Counts[CategoryStateMismatch] != 0 {
		t.Errorf("unexpected mismatches: %#v", report.Counts)
	}

	reportRaw, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	if _, err := s.InsertReport(ctx, report.BatchID, reportRaw); err != nil {
		t.Fatalf("InsertReport() error = %v", err)
	}

	afterTotals, err := s.LedgerTotals(ctx)
	if err != nil {
		t.Fatalf("ledger totals after: %v", err)
	}
	if beforeTotals[chargeID] != afterTotals[chargeID] {
		t.Errorf("ledger total changed: before=%d after=%d", beforeTotals[chargeID], afterTotals[chargeID])
	}
}

func TestReplay_DryRunWritesNothing(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	events := []store.Event{pendingEvent("evt_dry"), pendingEvent("evt_dry")}
	for _, ev := range events {
		insertPending(t, s, ctx, ev)
	}
	cleanupEvents(t, s, events[0].EventID, events[1].EventID)

	calls := 0
	enqueue := func(context.Context, pgx.Tx, string) error {
		calls++
		return nil
	}
	now := time.Now()
	count, err := Replay(ctx, s, enqueue, ReplayOptions{
		From:   now.Add(-30 * time.Second),
		To:     now.Add(30 * time.Second),
		DryRun: true,
	})
	if err != nil {
		t.Fatalf("Replay() error = %v", err)
	}
	if count != len(events) {
		t.Errorf("replayed = %d, want %d", count, len(events))
	}
	if calls != 0 {
		t.Errorf("enqueue calls = %d, want 0 in dry-run", calls)
	}
}

func TestReplay_IsIdempotent(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	events := []store.Event{pendingEvent("evt_replay"), pendingEvent("evt_replay")}
	for _, ev := range events {
		insertPending(t, s, ctx, ev)
	}
	cleanupEvents(t, s, events[0].EventID, events[1].EventID)

	var enqueued []string
	enqueue := func(_ context.Context, _ pgx.Tx, eventID string) error {
		enqueued = append(enqueued, eventID)
		return nil
	}
	opts := ReplayOptions{
		From:   time.Now().Add(-30 * time.Second),
		To:     time.Now().Add(30 * time.Second),
		DryRun: false,
	}

	if _, err := Replay(ctx, s, enqueue, opts); err != nil {
		t.Fatalf("first Replay() error = %v", err)
	}
	if len(enqueued) != len(events) {
		t.Fatalf("first enqueues = %d, want %d", len(enqueued), len(events))
	}

	if _, err := Replay(ctx, s, enqueue, opts); err != nil {
		t.Fatalf("second Replay() error = %v", err)
	}
	if len(enqueued) != 2*len(events) {
		t.Errorf("second enqueues = %d, want %d", len(enqueued), 2*len(events))
	}

	for _, ev := range events {
		var applied bool
		if err := s.Pool().QueryRow(ctx,
			`SELECT applied_at IS NOT NULL FROM webhook_events WHERE event_id = $1`, ev.EventID,
		).Scan(&applied); err != nil {
			t.Fatalf("read applied flag: %v", err)
		}
		if applied {
			t.Errorf("event %s was applied by replay, want untouched", ev.EventID)
		}
	}
}

func TestResolveLateEvents_ResolvesAllUnresolved(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	chargeID := unique("ch_late")
	eventID := unique("evt_late")
	ev := pendingEvent("late")
	insertPending(t, s, ctx, ev)
	cleanupEvents(t, s, ev.EventID)
	err := s.RunInTx(ctx, func(txCtx context.Context, tx pgx.Tx) error {
		inserted, err := s.InsertTx(txCtx, tx, store.LateEvent{
			EventID:  eventID,
			ChargeID: chargeID,
			Reason:   "out_of_order",
			Payload:  json.RawMessage(`{}`),
		})
		if err != nil {
			return err
		}
		if !inserted {
			return fmt.Errorf("late event %s already existed", eventID)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("insert late event: %v", err)
	}
	t.Cleanup(func() {
		if _, err := s.Pool().Exec(context.Background(), `DELETE FROM late_events WHERE event_id = $1`, eventID); err != nil {
			t.Errorf("cleanup late event: %v", err)
		}
	})

	resolved, err := ResolveLateEvents(ctx, s)
	if err != nil {
		t.Fatalf("ResolveLateEvents() error = %v", err)
	}
	if resolved < 1 {
		t.Fatalf("ResolveLateEvents() = %d, want >= 1", resolved)
	}

	var resolvedAt *time.Time
	if err := s.Pool().QueryRow(ctx, `SELECT resolved_at FROM late_events WHERE event_id = $1`, eventID).Scan(&resolvedAt); err != nil {
		t.Fatalf("read late event: %v", err)
	}
	if resolvedAt == nil {
		t.Error("resolved_at is nil, want a timestamp")
	}

	// A second run must be a no-op for this event.
	_, err = ResolveLateEvents(ctx, s)
	if err != nil {
		t.Fatalf("second ResolveLateEvents() error = %v", err)
	}
}
