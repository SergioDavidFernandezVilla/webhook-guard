//go:build integration && naive

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

func TestNaive_DuplicateEventFails(t *testing.T) {
	s := testStore(t)
	n := New(s)
	ev := newEvent("evt_naive_dup")
	ctx := context.Background()
	errRollback := errors.New("rollback after assertions")

	var duplicateErr error
	err := s.RunInTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if _, insertErr := n.InsertEvent(ctx, tx, ev); insertErr != nil {
			return insertErr
		}
		_, duplicateErr = n.InsertEvent(ctx, tx, ev)
		return errRollback
	})

	if !errors.Is(err, errRollback) {
		t.Fatalf("RunInTx() error = %v, want rollback sentinel", err)
	}
	if !errors.Is(duplicateErr, store.ErrDuplicate) {
		t.Fatalf("second insert error = %v, want store.ErrDuplicate", duplicateErr)
	}
}
