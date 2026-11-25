package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// LedgerEntry is an append-only row of ledger_entries. Corrections are new
// entries; this package exposes no update or delete.
type LedgerEntry struct {
	ID            int64
	ChargeID      string
	EntryType     ChargeState
	AmountMinor   int64
	Currency      string
	SourceEventID string
	CreatedAt     time.Time
}

// AppendTx appends one ledger entry. Not used by the service path; it is
// operational API for operators and tests. A repeated source_event_id returns
// ErrDuplicate. Note that a unique violation aborts the surrounding PostgreSQL
// transaction; callers that must keep going (the worker) use AppendIfAbsentTx.
func (s *Store) AppendTx(ctx context.Context, tx pgx.Tx, e LedgerEntry) (int64, error) {
	var id int64
	err := tx.QueryRow(ctx, `
		INSERT INTO ledger_entries (charge_id, entry_type, amount_minor, currency, source_event_id)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id`,
		e.ChargeID, string(e.EntryType), e.AmountMinor, e.Currency, e.SourceEventID,
	).Scan(&id)
	if err != nil {
		return 0, mapError("append ledger entry", err)
	}
	return id, nil
}

// AppendIfAbsentTx appends unless source_event_id already exists. A duplicate
// returns inserted=false without error, so a retried job keeps its transaction
// usable.
func (s *Store) AppendIfAbsentTx(ctx context.Context, tx pgx.Tx, e LedgerEntry) (inserted bool, err error) {
	tag, err := tx.Exec(ctx, `
		INSERT INTO ledger_entries (charge_id, entry_type, amount_minor, currency, source_event_id)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (source_event_id) DO NOTHING`,
		e.ChargeID, string(e.EntryType), e.AmountMinor, e.Currency, e.SourceEventID,
	)
	if err != nil {
		return false, mapError("append ledger entry", err)
	}
	return tag.RowsAffected() > 0, nil
}

// Count returns how many ledger entries exist, for the dashboard.
func (s *Store) Count(ctx context.Context) (int64, error) {
	var total int64
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM ledger_entries`).Scan(&total); err != nil {
		return 0, mapError("count ledger entries", err)
	}
	return total, nil
}

// GetBySourceEvent returns the ledger entry produced by an event, or
// ErrNotFound when the event had no ledger effect (for example created events).
func (s *Store) GetBySourceEvent(ctx context.Context, eventID string) (LedgerEntry, error) {
	var e LedgerEntry
	err := s.pool.QueryRow(ctx, `
		SELECT id, charge_id, entry_type, amount_minor, currency, source_event_id, created_at
		FROM ledger_entries
		WHERE source_event_id = $1`,
		eventID,
	).Scan(
		&e.ID, &e.ChargeID, &e.EntryType, &e.AmountMinor,
		&e.Currency, &e.SourceEventID, &e.CreatedAt,
	)
	if err != nil {
		return LedgerEntry{}, mapError("get ledger entry by event", err)
	}
	return e, nil
}

// SumByCharge returns the signed total of a charge's ledger entries. Not used
// by the service path; it is operational API for operators and tests.
func (s *Store) SumByCharge(ctx context.Context, chargeID string) (int64, error) {
	var total int64
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(amount_minor), 0)::bigint
		FROM ledger_entries
		WHERE charge_id = $1`,
		chargeID,
	).Scan(&total)
	if err != nil {
		return 0, mapError("sum ledger by charge", err)
	}
	return total, nil
}

// LedgerTotals returns the signed sum per charge in a single query.
func (s *Store) LedgerTotals(ctx context.Context) (map[string]int64, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT charge_id, COALESCE(SUM(amount_minor), 0)::bigint
		FROM ledger_entries
		GROUP BY charge_id`,
	)
	if err != nil {
		return nil, fmt.Errorf("sum ledger totals: %w", err)
	}
	defer rows.Close()

	totals := make(map[string]int64)
	for rows.Next() {
		var chargeID string
		var total int64
		if err := rows.Scan(&chargeID, &total); err != nil {
			return nil, fmt.Errorf("scan ledger total: %w", err)
		}
		totals[chargeID] = total
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate ledger totals: %w", err)
	}
	return totals, nil
}

// ListByCharge returns a charge's ledger entries in insertion order.
func (s *Store) ListByCharge(ctx context.Context, chargeID string) ([]LedgerEntry, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, charge_id, entry_type, amount_minor, currency, source_event_id, created_at
		FROM ledger_entries
		WHERE charge_id = $1
		ORDER BY created_at, id`,
		chargeID,
	)
	if err != nil {
		return nil, fmt.Errorf("list ledger by charge: %w", err)
	}
	defer rows.Close()

	var entries []LedgerEntry
	for rows.Next() {
		var e LedgerEntry
		if err := rows.Scan(
			&e.ID, &e.ChargeID, &e.EntryType, &e.AmountMinor,
			&e.Currency, &e.SourceEventID, &e.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan ledger entry: %w", err)
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate ledger entries: %w", err)
	}
	return entries, nil
}
