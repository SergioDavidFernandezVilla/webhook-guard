//go:build !naive

package guard

import (
	"context"
	"errors"
	"testing"

	"github.com/sergiodavidfv/webhook-guard/internal/store"
)

var _ Guard = (*Guarded)(nil)

func TestGuarded_ApplyTransitionRejectsInvalid(t *testing.T) {
	g := New(nil)
	ctx := context.Background()

	if err := g.ApplyTransition(ctx, nil, store.StateAuthorized, store.StateCaptured); err != nil {
		t.Fatalf("valid transition error = %v, want nil", err)
	}

	err := g.ApplyTransition(ctx, nil, store.StateRefunded, store.StateCreated)
	if !errors.Is(err, store.ErrInvalidTransition) {
		t.Fatalf("backward transition error = %v, want ErrInvalidTransition", err)
	}
}
