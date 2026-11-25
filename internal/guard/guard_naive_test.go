//go:build naive

package guard

import (
	"context"
	"testing"

	"github.com/sergiodavidfv/webhook-guard/internal/store"
)

var _ Guard = (*Naive)(nil)

func TestNaive_ApplyTransitionAppliesAnything(t *testing.T) {
	n := New(nil)
	ctx := context.Background()

	if err := n.ApplyTransition(ctx, nil, store.StateRefunded, store.StateCreated); err != nil {
		t.Fatalf("naive backward transition error = %v, want nil", err)
	}
	if err := n.ApplyTransition(ctx, nil, store.StateCreated, store.StateCaptured); err != nil {
		t.Fatalf("naive skipped transition error = %v, want nil", err)
	}
}

func TestNaive_LockChargeIsNoop(t *testing.T) {
	n := New(nil)
	if err := n.LockCharge(context.Background(), nil, "ch_test"); err != nil {
		t.Fatalf("naive LockCharge() error = %v, want nil", err)
	}
}
