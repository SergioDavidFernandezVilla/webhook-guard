package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// InsertReport persists a reconciliation report artifact. It never touches
// charges or the ledger.
func (s *Store) InsertReport(ctx context.Context, batchID string, report json.RawMessage) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO reconciliation_reports (batch_id, report)
		VALUES ($1, $2)
		RETURNING id`,
		batchID, report,
	).Scan(&id)
	if err != nil {
		return 0, mapError("insert reconciliation report", err)
	}
	return id, nil
}

// StoredReport is one persisted reconciliation report.
type StoredReport struct {
	ID        int64
	BatchID   string
	Report    json.RawMessage
	CreatedAt time.Time
}

// ListReports returns the most recent reports first.
func (s *Store) ListReports(ctx context.Context, limit int) ([]StoredReport, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	rows, err := s.pool.Query(ctx, `
		SELECT id, batch_id, report, created_at
		FROM reconciliation_reports
		ORDER BY created_at DESC, id DESC
		LIMIT $1`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list reconciliation reports: %w", err)
	}
	defer rows.Close()

	reports := make([]StoredReport, 0, limit)
	for rows.Next() {
		var r StoredReport
		if err := rows.Scan(&r.ID, &r.BatchID, &r.Report, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan reconciliation report: %w", err)
		}
		reports = append(reports, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate reconciliation reports: %w", err)
	}
	return reports, nil
}

// LatestReport returns the most recently persisted report or ErrNotFound.
func (s *Store) LatestReport(ctx context.Context) (batchID string, report json.RawMessage, createdAt time.Time, err error) {
	err = s.pool.QueryRow(ctx, `
		SELECT batch_id, report, created_at
		FROM reconciliation_reports
		ORDER BY created_at DESC, id DESC
		LIMIT 1`,
	).Scan(&batchID, &report, &createdAt)
	if err != nil {
		return "", nil, time.Time{}, mapError("get latest reconciliation report", err)
	}
	return batchID, report, createdAt, nil
}
