// Package guard defines the contract that the handler and worker use to enforce
// idempotency, per-charge ordering and valid state transitions.
package guard

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/sergiodavidfv/webhook-guard/internal/store"
)

// Guard is implemented by both the guarded (default) and naive (comparison)
// paths. The two implementations must expose the same method set.
type Guard interface {
	InsertEvent(ctx context.Context, tx pgx.Tx, ev store.Event) (inserted bool, err error)
	LockCharge(ctx context.Context, tx pgx.Tx, chargeID string) error
	// ReadCharge loads the charge the effect applies to. The guarded path
	// locks the row (SELECT ... FOR UPDATE); the naive path reads without
	// locking so the comparison is honest.
	ReadCharge(ctx context.Context, tx pgx.Tx, chargeID string) (store.Charge, error)
	ApplyTransition(ctx context.Context, tx pgx.Tx, from, to store.ChargeState) error
}
