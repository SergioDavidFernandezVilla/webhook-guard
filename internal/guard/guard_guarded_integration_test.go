//go:build integration && !naive

package guard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
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

func newEvent(prefix string) store.Event {
	stamp := time.Now().UnixNano()
	return store.Event{
		EventID:            fmt.Sprintf("%s_%d", prefix, stamp),
		Type:               "payment.captured",
		ChargeID:           fmt.Sprintf("ch_%s_%d", prefix, stamp),
		Payload:            json.RawMessage(`{"amount":1000,"currency":"USD"}`),
		SignatureTimestamp: time.Now().Unix(),
		CorrelationID:      "guard-test",
	}
}

func TestGuarded_DuplicateEventIsIdempotent(t *testing.T) {
	s := testStore(t)
	g := New(s)
	ev := newEvent("evt_guard_dup")
	ctx := context.Background()
	errRollback := errors.New("rollback after assertions")

	var first, second bool
	err := s.RunInTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var insertErr error
		first, insertErr = g.InsertEvent(ctx, tx, ev)
		if insertErr != nil {
			return insertErr
		}
		second, insertErr = g.InsertEvent(ctx, tx, ev)
		if insertErr != nil {
			return insertErr
		}
		return errRollback
	})

	if !errors.Is(err, errRollback) {
		t.Fatalf("RunInTx() error = %v, want rollback sentinel", err)
	}
	if !first {
		t.Error("first insert inserted = false, want true")
	}
	if second {
		t.Error("second insert inserted = true, want false")
	}
}
