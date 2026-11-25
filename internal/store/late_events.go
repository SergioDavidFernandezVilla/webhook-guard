package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// LateEvent is a row of late_events: an event received after the charge closed.
type LateEvent struct {
	ID         int64
	EventID    string
	ChargeID   string
	Reason     string
	Payload    json.RawMessage
	ReceivedAt time.Time
	ResolvedAt *time.Time
}

// InsertTx records a late event once. A repeated event_id is not an error:
// inserted is false.
func (s *Store) InsertTx(ctx context.Context, tx pgx.Tx, ev LateEvent) (inserted bool, err error) {
	tag, err := tx.Exec(ctx, `
		INSERT INTO late_events (event_id, charge_id, reason, payload)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (event_id) DO NOTHING`,
		ev.EventID, ev.ChargeID, ev.Reason, ev.Payload,
	)
	if err != nil {
		return false, mapError("insert late event", err)
	}
	return tag.RowsAffected() > 0, nil
}

// ListUnresolved returns late events still waiting for reconciliation.
func (s *Store) ListUnresolved(ctx context.Context, limit int) ([]LateEvent, error) {
	if limit <= 0 {
		limit = 100
	}

	rows, err := s.pool.Query(ctx, `
		SELECT id, event_id, charge_id, reason, payload, received_at, resolved_at
		FROM late_events
		WHERE resolved_at IS NULL
		ORDER BY received_at, id
		LIMIT $1`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query unresolved late events: %w", err)
	}
	defer rows.Close()

	events := make([]LateEvent, 0, limit)
	for rows.Next() {
		var ev LateEvent
		if err := rows.Scan(
			&ev.ID, &ev.EventID, &ev.ChargeID, &ev.Reason, &ev.Payload,
			&ev.ReceivedAt, &ev.ResolvedAt,
		); err != nil {
			return nil, fmt.Errorf("scan late event: %w", err)
		}
		events = append(events, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate late events: %w", err)
	}
	return events, nil
}

// LateEventFilter selects a page of late events for the dashboard.
type LateEventFilter struct {
	Limit    int
	Offset   int
	Resolved *bool
	ChargeID string
}

func lateWhere(f LateEventFilter) (string, []any) {
	where := "WHERE 1=1"
	args := make([]any, 0, 2)
	if f.Resolved != nil {
		if *f.Resolved {
			where += " AND resolved_at IS NOT NULL"
		} else {
			where += " AND resolved_at IS NULL"
		}
	}
	if f.ChargeID != "" {
		args = append(args, f.ChargeID)
		where += fmt.Sprintf(" AND charge_id = $%d", len(args))
	}
	return where, args
}

// ListLateEvents returns a page ordered newest first.
func (s *Store) ListLateEvents(ctx context.Context, f LateEventFilter) ([]LateEvent, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = 25
	}
	if limit > 100 {
		limit = 100
	}
	offset := f.Offset
	if offset < 0 {
		offset = 0
	}

	where, args := lateWhere(f)
	args = append(args, limit, offset)

	rows, err := s.pool.Query(ctx, `
		SELECT id, event_id, charge_id, reason, payload, received_at, resolved_at
		FROM late_events `+where+fmt.Sprintf(` ORDER BY received_at DESC, id DESC LIMIT $%d OFFSET $%d`, len(args)-1, len(args)),
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("list late events: %w", err)
	}
	defer rows.Close()

	events := make([]LateEvent, 0, limit)
	for rows.Next() {
		var ev LateEvent
		if err := rows.Scan(
			&ev.ID, &ev.EventID, &ev.ChargeID, &ev.Reason, &ev.Payload,
			&ev.ReceivedAt, &ev.ResolvedAt,
		); err != nil {
			return nil, fmt.Errorf("scan late event: %w", err)
		}
		events = append(events, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate late events: %w", err)
	}
	return events, nil
}

// CountLateEvents returns how many late events match a filter.
func (s *Store) CountLateEvents(ctx context.Context, f LateEventFilter) (int, error) {
	where, args := lateWhere(f)
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM late_events `+where, args...).Scan(&total); err != nil {
		return 0, mapError("count late events", err)
	}
	return total, nil
}

// GetByEventID returns the late record for an event, or ErrNotFound.
func (s *Store) GetByEventID(ctx context.Context, eventID string) (LateEvent, error) {
	var ev LateEvent
	err := s.pool.QueryRow(ctx, `
		SELECT id, event_id, charge_id, reason, payload, received_at, resolved_at
		FROM late_events
		WHERE event_id = $1`,
		eventID,
	).Scan(
		&ev.ID, &ev.EventID, &ev.ChargeID, &ev.Reason, &ev.Payload,
		&ev.ReceivedAt, &ev.ResolvedAt,
	)
	if err != nil {
		return LateEvent{}, mapError("get late event", err)
	}
	return ev, nil
}

// CountLateEventsByResolution returns pending and resolved totals.
func (s *Store) CountLateEventsByResolution(ctx context.Context) (pending, resolved int64, err error) {
	err = s.pool.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE resolved_at IS NULL),
		       count(*) FILTER (WHERE resolved_at IS NOT NULL)
		FROM late_events`,
	).Scan(&pending, &resolved)
	if err != nil {
		return 0, 0, mapError("count late events by resolution", err)
	}
	return pending, resolved, nil
}

// ResolveTx marks a late event as resolved.
func (s *Store) ResolveTx(ctx context.Context, tx pgx.Tx, eventID string) error {
	_, err := tx.Exec(ctx, `
		UPDATE late_events
		SET resolved_at = now()
		WHERE event_id = $1 AND resolved_at IS NULL`,
		eventID,
	)
	if err != nil {
		return mapError("resolve late event", err)
	}
	return nil
}
