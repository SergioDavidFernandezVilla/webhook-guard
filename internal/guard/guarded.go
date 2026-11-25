//go:build !naive

package guard

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/sergiodavidfv/webhook-guard/internal/store"
)

// Guarded is the default path: idempotent insert, per-charge advisory lock and
// validated state transitions.
type Guarded struct {
	store *store.Store
}

// New returns the guarded implementation.
func New(s *store.Store) Guard {
	return &Guarded{store: s}
}

// InsertEvent inserts once; a repeated event_id is reported as inserted=false.
func (g *Guarded) InsertEvent(ctx context.Context, tx pgx.Tx, ev store.Event) (bool, error) {
	_, inserted, err := g.store.InsertEventTx(ctx, tx, ev)
	return inserted, err
}

// LockCharge serializes work on a charge with a transaction-scoped advisory lock.
func (g *Guarded) LockCharge(ctx context.Context, tx pgx.Tx, chargeID string) error {
	return g.store.LockChargeTx(ctx, tx, chargeID)
}

// ReadCharge loads the charge with SELECT ... FOR UPDATE so the effect applies
// against a stable row.
func (g *Guarded) ReadCharge(ctx context.Context, tx pgx.Tx, chargeID string) (store.Charge, error) {
	return g.store.GetForUpdateTx(ctx, tx, chargeID)
}

// ApplyTransition rejects any transition not allowed by the domain table.
func (*Guarded) ApplyTransition(_ context.Context, _ pgx.Tx, from, to store.ChargeState) error {
	if !store.CanTransition(from, to) {
		return fmt.Errorf("guard: %s -> %s: %w", from, to, store.ErrInvalidTransition)
	}
	return nil
}
