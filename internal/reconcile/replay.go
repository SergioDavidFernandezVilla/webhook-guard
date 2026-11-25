package reconcile

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/sergiodavidfv/webhook-guard/internal/store"
)

// EnqueueFunc enqueues one pending event inside the caller's transaction.
type EnqueueFunc func(ctx context.Context, tx pgx.Tx, eventID string) error

// ReplayOptions defines the replay window and mode.
type ReplayOptions struct {
	From      time.Time
	To        time.Time
	DryRun    bool
	BatchSize int
}

// Replay re-enqueues unapplied events in [From, To). It never applies effects
// itself and never marks events applied; the worker does that idempotently.
// With DryRun it only counts.
func Replay(ctx context.Context, st *store.Store, enqueue EnqueueFunc, opts ReplayOptions) (int, error) {
	if opts.BatchSize <= 0 {
		opts.BatchSize = 500
	}
	if opts.From.IsZero() {
		opts.From = time.Unix(0, 0).UTC()
	}
	if opts.To.IsZero() {
		opts.To = time.Now().Add(time.Second)
	}

	total := 0
	var afterID int64
	for {
		events, err := st.PendingBatch(ctx, opts.From, opts.To, afterID, opts.BatchSize)
		if err != nil {
			return total, err
		}
		if len(events) == 0 {
			break
		}

		if !opts.DryRun {
			err := st.RunInTx(ctx, func(txCtx context.Context, tx pgx.Tx) error {
				for _, ev := range events {
					if err := enqueue(txCtx, tx, ev.EventID); err != nil {
						return fmt.Errorf("enqueue event %s: %w", ev.EventID, err)
					}
				}
				return nil
			})
			if err != nil {
				return total, err
			}
		}

		total += len(events)
		afterID = events[len(events)-1].ID
		if len(events) < opts.BatchSize {
			break
		}
	}
	return total, nil
}
