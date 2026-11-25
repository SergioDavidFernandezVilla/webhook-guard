package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/sergiodavidfv/webhook-guard/internal/guard"
	"github.com/sergiodavidfv/webhook-guard/internal/store"
)

const (
	metricJobsApplied    = "jobs_applied"
	metricJobsRejected   = "jobs_rejected_transition"
	metricLateEvents     = "late_events_total"
	metricWindowSeconds  = 60
	webhookJobKind       = "webhook_event"
	lateReasonOutOfOrder = "out_of_order"
	lateReasonClosed     = "charge_closed"
)

// WebhookArgs identifies the event a queued job must apply.
type WebhookArgs struct {
	EventID string `json:"event_id"`
}

// Kind implements river.JobArgs.
func (WebhookArgs) Kind() string {
	return webhookJobKind
}

// WebhookWorker applies ingested events in a second transaction (TX2).
type WebhookWorker struct {
	river.WorkerDefaults[WebhookArgs]

	store  *store.Store
	guard  guard.Guard
	logger *slog.Logger
}

// NewWebhookWorker builds the River worker for webhook events.
func NewWebhookWorker(st *store.Store, g guard.Guard, logger *slog.Logger) *WebhookWorker {
	if logger == nil {
		logger = slog.Default()
	}
	return &WebhookWorker{store: st, guard: g, logger: logger}
}

// Work implements river.Worker and runs the whole effect in TX2.
func (w *WebhookWorker) Work(ctx context.Context, job *river.Job[WebhookArgs]) error {
	return w.applyEvent(ctx, job.Args.EventID)
}

func (w *WebhookWorker) applyEvent(ctx context.Context, eventID string) error {
	poison := false
	err := w.store.RunInTx(ctx, func(txCtx context.Context, tx pgx.Tx) error {
		var applyErr error
		poison, applyErr = w.applyEventTx(txCtx, tx, eventID)
		return applyErr
	})
	if err != nil {
		return err
	}
	if poison {
		// A rejected poison pill must commit its rejection and stop retrying;
		// River's JobCancel ends the job without further attempts.
		return river.JobCancel(fmt.Errorf("event %s rejected as unprocessable", eventID))
	}
	return nil
}

func (w *WebhookWorker) applyEventTx(ctx context.Context, tx pgx.Tx, eventID string) (poison bool, err error) {
	ev, err := w.store.GetEventTx(ctx, tx, eventID)
	if err != nil {
		return false, fmt.Errorf("load event %s: %w", eventID, err)
	}
	if ev.AppliedAt != nil {
		return false, nil
	}

	logger := w.logger.With(
		slog.String("event_id", ev.EventID),
		slog.String("charge_id", ev.ChargeID),
		slog.String("correlation_id", ev.CorrelationID),
	)

	if ev.ChargeID == "" {
		// A payload without charge_id can never be applied; retrying it forever
		// would be a poison pill. Reject, commit and stop.
		if err := w.reject(ctx, tx, ev.EventID, "missing_charge_id"); err != nil {
			return false, err
		}
		return true, nil
	}

	target, ok := stateForEventType(ev.Type)
	if !ok {
		logger.WarnContext(ctx, "unknown event type; not applied", slog.String("type", ev.Type))
		return false, w.reject(ctx, tx, ev.EventID, "unknown_event_type")
	}

	if err := w.guard.LockCharge(ctx, tx, ev.ChargeID); err != nil {
		return false, fmt.Errorf("lock charge %s: %w", ev.ChargeID, err)
	}

	// Re-read the event after acquiring the per-charge lock: a concurrent job
	// may have applied this event between the first read and the lock.
	ev, err = w.store.GetEventTx(ctx, tx, eventID)
	if err != nil {
		return false, fmt.Errorf("reload event %s: %w", eventID, err)
	}
	if ev.AppliedAt != nil {
		return false, nil
	}

	lastApplied, err := w.store.LastAppliedTimestamp(ctx, tx, ev.ChargeID)
	if err != nil {
		return false, fmt.Errorf("load last applied timestamp for %s: %w", ev.ChargeID, err)
	}
	if lastApplied > 0 && ev.SignatureTimestamp < lastApplied {
		return false, w.recordLate(ctx, tx, ev, lateReasonOutOfOrder)
	}

	charge, err := w.guard.ReadCharge(ctx, tx, ev.ChargeID)
	chargeExists := err == nil
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return false, fmt.Errorf("load charge %s: %w", ev.ChargeID, err)
	}

	from := store.StateCreated
	if chargeExists {
		from = charge.State
	}

	if chargeExists && (from == store.StateFailed || from == store.StateRefunded) {
		return false, w.recordLate(ctx, tx, ev, lateReasonClosed)
	}

	data, err := parseEventData(ev.Payload)
	if err != nil {
		logger.WarnContext(ctx, "invalid event payload; not applied", slog.Any("error", err))
		return false, w.reject(ctx, tx, ev.EventID, "invalid_payload")
	}

	if !chargeExists && target == store.StateCreated {
		return false, w.applyEffect(ctx, tx, ev, data, store.StateCreated, nil)
	}

	if target == from {
		return false, w.markApplied(ctx, tx, ev.EventID, "already_in_state")
	}

	if err := w.guard.ApplyTransition(ctx, tx, from, target); err != nil {
		if errors.Is(err, store.ErrInvalidTransition) {
			logger.WarnContext(ctx, "invalid transition; not applied",
				slog.String("from", string(from)),
				slog.String("to", string(target)),
			)
			return false, w.reject(ctx, tx, ev.EventID, "invalid_transition")
		}
		return false, fmt.Errorf("validate transition %s -> %s: %w", from, target, err)
	}

	var prevCharge *store.Charge
	if chargeExists {
		prevCharge = &charge
	}
	return false, w.applyEffect(ctx, tx, ev, data, target, prevCharge)
}

// applyEffect writes the charge row and the ledger entry. prev is the charge
// row read (and, on the guarded path, locked) before applying the effect.
func (w *WebhookWorker) applyEffect(ctx context.Context, tx pgx.Tx, ev store.Event, data eventData, target store.ChargeState, prev *store.Charge) error {
	charge := store.Charge{
		ChargeID:    ev.ChargeID,
		State:       target,
		AmountMinor: data.AmountMinor,
		Currency:    data.Currency,
		LastEventID: ev.EventID,
	}
	// charges keeps the charge's original amount and currency: only
	// payment.created sets them. The event's amount_minor is the movement
	// amount and belongs to the ledger entry, never over the charge's own.
	if prev != nil && target != store.StateCreated {
		charge.AmountMinor = prev.AmountMinor
		charge.Currency = prev.Currency
	}

	if err := w.store.UpsertTx(ctx, tx, charge); err != nil {
		return fmt.Errorf("upsert charge %s: %w", ev.ChargeID, err)
	}

	if target != store.StateCreated {
		entry := ledgerEntryFor(ev, data, target)
		inserted, err := w.store.AppendIfAbsentTx(ctx, tx, entry)
		if err != nil {
			return fmt.Errorf("append ledger for event %s: %w", ev.EventID, err)
		}
		if !inserted {
			w.logger.WarnContext(ctx, "ledger entry already present; treating as applied",
				slog.String("event_id", ev.EventID),
			)
		}
	}

	if err := w.store.MarkAppliedTx(ctx, tx, ev.EventID); err != nil {
		return fmt.Errorf("mark event %s applied: %w", ev.EventID, err)
	}
	if err := w.addMetric(ctx, tx, metricJobsApplied, 1); err != nil {
		return err
	}

	w.logger.InfoContext(ctx, "event applied",
		slog.String("event_id", ev.EventID),
		slog.String("charge_id", ev.ChargeID),
		slog.String("state", string(target)),
	)
	return nil
}

func (w *WebhookWorker) recordLate(ctx context.Context, tx pgx.Tx, ev store.Event, reason string) error {
	inserted, err := w.store.InsertTx(ctx, tx, store.LateEvent{
		EventID:  ev.EventID,
		ChargeID: ev.ChargeID,
		Reason:   reason,
		Payload:  ev.Payload,
	})
	if err != nil {
		return fmt.Errorf("insert late event %s: %w", ev.EventID, err)
	}
	if err := w.store.MarkAppliedTx(ctx, tx, ev.EventID); err != nil {
		return fmt.Errorf("mark late event %s applied: %w", ev.EventID, err)
	}
	if inserted {
		if err := w.addMetric(ctx, tx, metricLateEvents, 1); err != nil {
			return err
		}
	}

	w.logger.WarnContext(ctx, "late event recorded",
		slog.String("event_id", ev.EventID),
		slog.String("charge_id", ev.ChargeID),
		slog.String("reason", reason),
	)
	return nil
}

func (w *WebhookWorker) reject(ctx context.Context, tx pgx.Tx, eventID, reason string) error {
	if err := w.addMetric(ctx, tx, metricJobsRejected, 1); err != nil {
		return err
	}
	if err := w.store.MarkAppliedTx(ctx, tx, eventID); err != nil {
		return fmt.Errorf("mark rejected event %s applied: %w", eventID, err)
	}
	w.logger.WarnContext(ctx, "event rejected", slog.String("event_id", eventID), slog.String("reason", reason))
	return nil
}

func (w *WebhookWorker) markApplied(ctx context.Context, tx pgx.Tx, eventID, reason string) error {
	if err := w.store.MarkAppliedTx(ctx, tx, eventID); err != nil {
		return fmt.Errorf("mark event %s applied: %w", eventID, err)
	}
	w.logger.InfoContext(ctx, "event has no effect", slog.String("event_id", eventID), slog.String("reason", reason))
	return nil
}

func (w *WebhookWorker) addMetric(ctx context.Context, tx pgx.Tx, name string, delta int64) error {
	err := w.store.AddTx(ctx, tx, store.Metric{
		Name:          name,
		WindowStart:   time.Now().UTC().Truncate(time.Minute),
		WindowSeconds: metricWindowSeconds,
		Count:         delta,
	})
	if err != nil {
		return fmt.Errorf("record metric %s: %w", name, err)
	}
	return nil
}
