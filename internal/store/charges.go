package store

import (
	"context"
	"fmt"
	"hash/fnv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// ChargeState is the persisted lifecycle of a charge. It must match the CHECK
// constraint in migration 002 exactly.
type ChargeState string

const (
	StateCreated    ChargeState = "created"
	StateAuthorized ChargeState = "authorized"
	StateCaptured   ChargeState = "captured"
	StateFailed     ChargeState = "failed"
	StateRefunded   ChargeState = "refunded"
)

// Charge is a row of charges.
type Charge struct {
	ChargeID    string
	State       ChargeState
	AmountMinor int64
	Currency    string
	UpdatedAt   time.Time
	LastEventID string
}

const chargeColumns = `charge_id, state, amount_minor, currency, updated_at, last_event_id`

func scanCharge(row pgx.Row) (Charge, error) {
	var c Charge
	err := row.Scan(&c.ChargeID, &c.State, &c.AmountMinor, &c.Currency, &c.UpdatedAt, &c.LastEventID)
	return c, err
}

// GetForUpdateTx locks and returns a charge, or ErrNotFound.
func (s *Store) GetForUpdateTx(ctx context.Context, tx pgx.Tx, chargeID string) (Charge, error) {
	c, err := scanCharge(tx.QueryRow(ctx, `
		SELECT `+chargeColumns+`
		FROM charges
		WHERE charge_id = $1
		FOR UPDATE`,
		chargeID,
	))
	if err != nil {
		return Charge{}, mapError("get charge for update", err)
	}
	return c, nil
}

// GetTx returns a charge without locking it. It backs the naive path, which
// must not serialize per charge.
func (s *Store) GetTx(ctx context.Context, tx pgx.Tx, chargeID string) (Charge, error) {
	c, err := scanCharge(tx.QueryRow(ctx, `
		SELECT `+chargeColumns+`
		FROM charges
		WHERE charge_id = $1`,
		chargeID,
	))
	if err != nil {
		return Charge{}, mapError("get charge", err)
	}
	return c, nil
}

// GetCharge loads a charge outside any transaction, for the dashboard.
func (s *Store) GetCharge(ctx context.Context, chargeID string) (Charge, error) {
	c, err := scanCharge(s.pool.QueryRow(ctx, `
		SELECT `+chargeColumns+`
		FROM charges
		WHERE charge_id = $1`,
		chargeID,
	))
	if err != nil {
		return Charge{}, mapError("get charge", err)
	}
	return c, nil
}

// ChargeFilter selects a page of charges for the dashboard.
type ChargeFilter struct {
	Limit  int
	Offset int
	State  ChargeState
	Query  string
}

func chargeWhere(f ChargeFilter) (string, []any) {
	where := "WHERE 1=1"
	args := make([]any, 0, 2)
	if f.State != "" {
		args = append(args, string(f.State))
		where += fmt.Sprintf(" AND state = $%d", len(args))
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		args = append(args, "%"+q+"%")
		where += fmt.Sprintf(" AND charge_id ILIKE $%d", len(args))
	}
	return where, args
}

// ListCharges returns a page ordered by most recently updated.
func (s *Store) ListCharges(ctx context.Context, f ChargeFilter) ([]Charge, error) {
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

	where, args := chargeWhere(f)
	args = append(args, limit, offset)

	rows, err := s.pool.Query(ctx, `
		SELECT `+chargeColumns+`
		FROM charges `+where+fmt.Sprintf(` ORDER BY updated_at DESC, charge_id LIMIT $%d OFFSET $%d`, len(args)-1, len(args)),
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("list charges: %w", err)
	}
	defer rows.Close()

	charges := make([]Charge, 0, limit)
	for rows.Next() {
		c, err := scanCharge(rows)
		if err != nil {
			return nil, fmt.Errorf("scan charge: %w", err)
		}
		charges = append(charges, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate charges: %w", err)
	}
	return charges, nil
}

// CountCharges returns how many charges match a filter, for pagination.
func (s *Store) CountCharges(ctx context.Context, f ChargeFilter) (int, error) {
	where, args := chargeWhere(f)
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM charges `+where, args...).Scan(&total); err != nil {
		return 0, mapError("count charges", err)
	}
	return total, nil
}

// CountChargesByState returns the number of charges per state.
func (s *Store) CountChargesByState(ctx context.Context) (map[ChargeState]int64, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT state, count(*)
		FROM charges
		GROUP BY state`,
	)
	if err != nil {
		return nil, fmt.Errorf("count charges by state: %w", err)
	}
	defer rows.Close()

	counts := make(map[ChargeState]int64)
	for rows.Next() {
		var state ChargeState
		var count int64
		if err := rows.Scan(&state, &count); err != nil {
			return nil, fmt.Errorf("scan charge state count: %w", err)
		}
		counts[state] = count
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate charge state counts: %w", err)
	}
	return counts, nil
}

// UpsertTx inserts or replaces a charge row.
func (s *Store) UpsertTx(ctx context.Context, tx pgx.Tx, c Charge) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO charges (charge_id, state, amount_minor, currency, last_event_id)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (charge_id) DO UPDATE SET
			state = EXCLUDED.state,
			amount_minor = EXCLUDED.amount_minor,
			currency = EXCLUDED.currency,
			updated_at = now(),
			last_event_id = EXCLUDED.last_event_id`,
		c.ChargeID, string(c.State), c.AmountMinor, c.Currency, c.LastEventID,
	)
	if err != nil {
		return mapError("upsert charge", err)
	}
	return nil
}

// LockChargeTx serializes work on one charge with a transaction-scoped advisory
// lock keyed by a stable hash of charge_id.
func (s *Store) LockChargeTx(ctx context.Context, tx pgx.Tx, chargeID string) error {
	key, err := advisoryLockKey(chargeID)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, key); err != nil {
		return fmt.Errorf("acquire charge advisory lock: %w", err)
	}
	return nil
}

// ListByState returns every charge in the given state ordered by charge_id.
// Not used by the service path; it is operational API for operators and tests.
func (s *Store) ListByState(ctx context.Context, state ChargeState) ([]Charge, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+chargeColumns+`
		FROM charges
		WHERE state = $1
		ORDER BY charge_id`,
		string(state),
	)
	if err != nil {
		return nil, fmt.Errorf("list charges by state: %w", err)
	}
	defer rows.Close()

	var charges []Charge
	for rows.Next() {
		c, err := scanCharge(rows)
		if err != nil {
			return nil, fmt.Errorf("scan charge: %w", err)
		}
		charges = append(charges, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate charges: %w", err)
	}
	return charges, nil
}

func advisoryLockKey(chargeID string) (int64, error) {
	h := fnv.New64a()
	if _, err := h.Write([]byte(chargeID)); err != nil {
		return 0, fmt.Errorf("hash charge id: %w", err)
	}
	return int64(h.Sum64()), nil
}
