package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// EventTypeCreated is the only event whose effect has no ledger entry. The
// ordering watermark still counts it as an applied effect.
const EventTypeCreated = "payment.created"

// Event is a row of webhook_events.
type Event struct {
	ID                 int64
	EventID            string
	Type               string
	ChargeID           string
	Payload            json.RawMessage
	SignatureTimestamp int64
	CorrelationID      string
	ReceivedAt         time.Time
	AppliedAt          *time.Time
}

// InsertEventTx inserts an event once. A repeated event_id is not an error:
// inserted is false and id is zero.
func (s *Store) InsertEventTx(ctx context.Context, tx pgx.Tx, ev Event) (id int64, inserted bool, err error) {
	var chargeID any
	if ev.ChargeID != "" {
		chargeID = ev.ChargeID
	}
	var correlationID any
	if ev.CorrelationID != "" {
		correlationID = ev.CorrelationID
	}

	err = tx.QueryRow(ctx, `
		INSERT INTO webhook_events (event_id, type, charge_id, payload, signature_timestamp, correlation_id)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (event_id) DO NOTHING
		RETURNING id`,
		ev.EventID, ev.Type, chargeID, ev.Payload, ev.SignatureTimestamp, correlationID,
	).Scan(&id)

	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, mapError("insert webhook event", err)
	}
	return id, true, nil
}

// MarkAppliedTx stamps applied_at once. Re-marking an already applied event is
// a no-op so a retried job stays idempotent.
func (s *Store) MarkAppliedTx(ctx context.Context, tx pgx.Tx, eventID string) error {
	_, err := tx.Exec(ctx, `
		UPDATE webhook_events
		SET applied_at = now()
		WHERE event_id = $1 AND applied_at IS NULL`,
		eventID,
	)
	if err != nil {
		return mapError("mark webhook event applied", err)
	}
	return nil
}

// InsertEventPlainTx performs a plain INSERT without conflict handling. It is
// used only by the naive comparison path; the guarded path uses InsertEventTx.
func (s *Store) InsertEventPlainTx(ctx context.Context, tx pgx.Tx, ev Event) (int64, error) {
	var chargeID any
	if ev.ChargeID != "" {
		chargeID = ev.ChargeID
	}
	var correlationID any
	if ev.CorrelationID != "" {
		correlationID = ev.CorrelationID
	}

	var id int64
	err := tx.QueryRow(ctx, `
		INSERT INTO webhook_events (event_id, type, charge_id, payload, signature_timestamp, correlation_id)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id`,
		ev.EventID, ev.Type, chargeID, ev.Payload, ev.SignatureTimestamp, correlationID,
	).Scan(&id)
	if err != nil {
		return 0, mapError("insert webhook event", err)
	}
	return id, nil
}

// rowQuerier is satisfied by both pgxpool.Pool and pgx.Tx, so a read can run
// inside or outside a transaction without duplicating the query.
type rowQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func getEvent(ctx context.Context, q rowQuerier, eventID string) (Event, error) {
	var ev Event
	err := q.QueryRow(ctx, `
		SELECT id, event_id, type, COALESCE(charge_id, ''), payload, signature_timestamp,
		       COALESCE(correlation_id, ''), received_at, applied_at
		FROM webhook_events
		WHERE event_id = $1`,
		eventID,
	).Scan(
		&ev.ID, &ev.EventID, &ev.Type, &ev.ChargeID, &ev.Payload,
		&ev.SignatureTimestamp, &ev.CorrelationID, &ev.ReceivedAt, &ev.AppliedAt,
	)
	if err != nil {
		return Event{}, mapError("get webhook event", err)
	}
	return ev, nil
}

// GetEvent loads one event by event_id or ErrNotFound.
func (s *Store) GetEvent(ctx context.Context, eventID string) (Event, error) {
	return getEvent(ctx, s.pool, eventID)
}

// GetEventTx loads one event by event_id or ErrNotFound.
func (s *Store) GetEventTx(ctx context.Context, tx pgx.Tx, eventID string) (Event, error) {
	return getEvent(ctx, tx, eventID)
}

// EventCounts summarizes the webhook inbox for the dashboard.
type EventCounts struct {
	Total   int64
	Applied int64
	Pending int64
}

// CountEvents returns the inbox totals in one pass.
func (s *Store) CountEvents(ctx context.Context) (EventCounts, error) {
	var counts EventCounts
	err := s.pool.QueryRow(ctx, `
		SELECT count(*),
		       count(*) FILTER (WHERE applied_at IS NOT NULL),
		       count(*) FILTER (WHERE applied_at IS NULL)
		FROM webhook_events`,
	).Scan(&counts.Total, &counts.Applied, &counts.Pending)
	if err != nil {
		return EventCounts{}, mapError("count webhook events", err)
	}
	return counts, nil
}

// EventFilter selects a page of webhook events for the dashboard.
type EventFilter struct {
	Limit    int
	BeforeID int64
	Status   string
	Type     string
	ChargeID string
}

// ListEvents returns a page ordered newest first. The cursor is the id of the
// last item of the previous page (keyset pagination).
func (s *Store) ListEvents(ctx context.Context, f EventFilter) ([]Event, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}

	where := "WHERE 1=1"
	args := make([]any, 0, 5)
	if f.BeforeID > 0 {
		args = append(args, f.BeforeID)
		where += fmt.Sprintf(" AND id < $%d", len(args))
	}
	switch f.Status {
	case "applied":
		where += " AND applied_at IS NOT NULL"
	case "pending":
		where += " AND applied_at IS NULL"
	}
	if f.Type != "" {
		args = append(args, f.Type)
		where += fmt.Sprintf(" AND type = $%d", len(args))
	}
	if f.ChargeID != "" {
		args = append(args, f.ChargeID)
		where += fmt.Sprintf(" AND charge_id = $%d", len(args))
	}
	args = append(args, limit)

	rows, err := s.pool.Query(ctx, `
		SELECT id, event_id, type, COALESCE(charge_id, ''), payload, signature_timestamp,
		       COALESCE(correlation_id, ''), received_at, applied_at
		FROM webhook_events `+where+fmt.Sprintf(" ORDER BY id DESC LIMIT $%d", len(args)),
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("list webhook events: %w", err)
	}
	defer rows.Close()

	events := make([]Event, 0, limit)
	for rows.Next() {
		var ev Event
		if err := rows.Scan(
			&ev.ID, &ev.EventID, &ev.Type, &ev.ChargeID, &ev.Payload,
			&ev.SignatureTimestamp, &ev.CorrelationID, &ev.ReceivedAt, &ev.AppliedAt,
		); err != nil {
			return nil, fmt.Errorf("scan webhook event: %w", err)
		}
		events = append(events, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate webhook events: %w", err)
	}
	return events, nil
}

// LastAppliedTimestamp returns the newest timestamp of an event that actually
// changed the charge, or 0 when no event has been applied yet. Rejected events,
// no-ops and late records keep applied_at but have no effect, so they must not
// raise the ordering watermark. A created event is the only effective event
// without a ledger entry; a repeated created on an existing charge is a no-op
// and must not count either.
func (s *Store) LastAppliedTimestamp(ctx context.Context, tx pgx.Tx, chargeID string) (int64, error) {
	var timestamp int64
	err := tx.QueryRow(ctx, `
		SELECT COALESCE(MAX(we.signature_timestamp), 0)
		FROM webhook_events we
		WHERE we.charge_id = $1
		  AND we.applied_at IS NOT NULL
		  AND (
		      EXISTS (
		          SELECT 1 FROM ledger_entries le
		          WHERE le.source_event_id = we.event_id
		      )
		      OR (
		          we.type = $2
		          AND EXISTS (
		              SELECT 1 FROM charges c
		              WHERE c.charge_id = we.charge_id
		                AND c.last_event_id = we.event_id
		          )
		      )
		  )`,
		chargeID, EventTypeCreated,
	).Scan(&timestamp)
	if err != nil {
		return 0, mapError("get last applied timestamp", err)
	}
	return timestamp, nil
}

// PendingBatch returns unapplied events received in [from, to) after a given id,
// ordered by id, for replay pagination.
func (s *Store) PendingBatch(ctx context.Context, from, to time.Time, afterID int64, limit int) ([]Event, error) {
	if limit <= 0 {
		limit = 100
	}

	rows, err := s.pool.Query(ctx, `
		SELECT id, event_id, type, COALESCE(charge_id, ''), payload, signature_timestamp,
		       COALESCE(correlation_id, ''), received_at, applied_at
		FROM webhook_events
		WHERE applied_at IS NULL AND received_at >= $1 AND received_at < $2 AND id > $3
		ORDER BY id
		LIMIT $4`,
		from, to, afterID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query pending events: %w", err)
	}
	defer rows.Close()

	events := make([]Event, 0, limit)
	for rows.Next() {
		var ev Event
		if err := rows.Scan(
			&ev.ID, &ev.EventID, &ev.Type, &ev.ChargeID, &ev.Payload,
			&ev.SignatureTimestamp, &ev.CorrelationID, &ev.ReceivedAt, &ev.AppliedAt,
		); err != nil {
			return nil, fmt.Errorf("scan pending event: %w", err)
		}
		events = append(events, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pending events: %w", err)
	}
	return events, nil
}
