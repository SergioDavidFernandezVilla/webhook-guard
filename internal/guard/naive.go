//go:build naive

package guard

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/sergiodavidfv/webhook-guard/internal/store"
)

// Naive is the deliberately unguarded comparison path: plain insert, no lock
// and no transition validation. It exists to measure the cost of the guards.
type Naive struct {
	store *store.Store
}

// New returns the naive implementation.
func New(s *store.Store) Guard {
	return &Naive{store: s}
}

// InsertEvent performs a plain INSERT, so a duplicate delivery surfaces as
// store.ErrDuplicate instead of being idempotent.
func (n *Naive) InsertEvent(ctx context.Context, tx pgx.Tx, ev store.Event) (bool, error) {
	if _, err := n.store.InsertEventPlainTx(ctx, tx, ev); err != nil {
		return false, err
	}
	return true, nil
}

// LockCharge does nothing: the naive path does not serialize per charge.
func (*Naive) LockCharge(context.Context, pgx.Tx, string) error {
	return nil
}

// ReadCharge loads the charge without locking; the naive path must not
// serialize per charge, so no FOR UPDATE is issued.
func (n *Naive) ReadCharge(ctx context.Context, tx pgx.Tx, chargeID string) (store.Charge, error) {
	return n.store.GetTx(ctx, tx, chargeID)
}

// ApplyTransition applies anything, including backward transitions.
func (*Naive) ApplyTransition(context.Context, pgx.Tx, store.ChargeState, store.ChargeState) error {
	return nil
}
