package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// ReconciliationExport is a row of reconciliation_exports: the gateway's
// snapshot of its own charges.
type ReconciliationExport struct {
	ID         int64
	BatchID    string
	Checksum   string
	Payload    json.RawMessage
	ImportedAt time.Time
}

// InsertExportTx upserts an export by batch_id and returns its id.
func (s *Store) InsertExportTx(ctx context.Context, tx pgx.Tx, exp ReconciliationExport) (int64, error) {
	var id int64
	err := tx.QueryRow(ctx, `
		INSERT INTO reconciliation_exports (batch_id, checksum, payload)
		VALUES ($1, $2, $3)
		ON CONFLICT (batch_id) DO UPDATE SET
			checksum = EXCLUDED.checksum,
			payload = EXCLUDED.payload,
			imported_at = now()
		RETURNING id`,
		exp.BatchID, exp.Checksum, exp.Payload,
	).Scan(&id)
	if err != nil {
		return 0, mapError("insert reconciliation export", err)
	}
	return id, nil
}

// ExportByID returns one export by id or ErrNotFound.
func (s *Store) ExportByID(ctx context.Context, id int64) (ReconciliationExport, error) {
	var exp ReconciliationExport
	err := s.pool.QueryRow(ctx, `
		SELECT id, batch_id, checksum, payload, imported_at
		FROM reconciliation_exports
		WHERE id = $1`,
		id,
	).Scan(&exp.ID, &exp.BatchID, &exp.Checksum, &exp.Payload, &exp.ImportedAt)
	if err != nil {
		return ReconciliationExport{}, mapError("get reconciliation export", err)
	}
	return exp, nil
}

// LatestExport returns the most recently imported export or ErrNotFound. Not
// used by the service path; it is operational API for operators and tests.
func (s *Store) LatestExport(ctx context.Context) (ReconciliationExport, error) {
	var exp ReconciliationExport
	err := s.pool.QueryRow(ctx, `
		SELECT id, batch_id, checksum, payload, imported_at
		FROM reconciliation_exports
		ORDER BY imported_at DESC, id DESC
		LIMIT 1`,
	).Scan(&exp.ID, &exp.BatchID, &exp.Checksum, &exp.Payload, &exp.ImportedAt)
	if err != nil {
		return ReconciliationExport{}, mapError("get latest reconciliation export", err)
	}
	return exp, nil
}

// ListChargesSnapshot returns every charge ordered by charge_id.
func (s *Store) ListChargesSnapshot(ctx context.Context) ([]Charge, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+chargeColumns+`
		FROM charges
		ORDER BY charge_id`,
	)
	if err != nil {
		return nil, fmt.Errorf("list charges snapshot: %w", err)
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
