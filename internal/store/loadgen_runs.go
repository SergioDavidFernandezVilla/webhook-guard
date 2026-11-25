package store

import (
	"context"
	"fmt"
	"time"
)

// LoadgenRun is one persisted loadgen result. It is the evidence behind the
// dashboard's live guarded-vs-naive comparison: the numbers come from an actual
// run, not from a hand-written table.
type LoadgenRun struct {
	ID              int64
	Route           string
	Mode            string
	Seed            int64
	Target          string
	Events          int
	Concurrency     int
	Sent            int64
	Accepted        int64
	Duplicates      int64
	Rejected        int64
	ServerErrors    int64
	Other           int64
	TransportErrors int64
	P50MS           float64
	P95MS           float64
	P99MS           float64
	AvgMS           float64
	DurationMS      float64
	CreatedAt       time.Time
}

const loadgenRunColumns = `id, route, mode, seed, target, events, concurrency,
	sent, accepted, duplicates, rejected, server_errors, other, transport_errors,
	p50_ms, p95_ms, p99_ms, avg_ms, duration_ms, created_at`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanLoadgenRun(row rowScanner) (LoadgenRun, error) {
	var run LoadgenRun
	err := row.Scan(
		&run.ID, &run.Route, &run.Mode, &run.Seed, &run.Target,
		&run.Events, &run.Concurrency,
		&run.Sent, &run.Accepted, &run.Duplicates, &run.Rejected,
		&run.ServerErrors, &run.Other, &run.TransportErrors,
		&run.P50MS, &run.P95MS, &run.P99MS, &run.AvgMS, &run.DurationMS,
		&run.CreatedAt,
	)
	if err != nil {
		return LoadgenRun{}, err
	}
	return run, nil
}

// InsertLoadgenRun persists one run and returns the stored row (id and
// created_at included). It is the only write behind the comparison page and is
// reachable only through the gated ingest endpoint.
func (s *Store) InsertLoadgenRun(ctx context.Context, run LoadgenRun) (LoadgenRun, error) {
	row := s.pool.QueryRow(ctx, `
		INSERT INTO loadgen_runs (
			route, mode, seed, target, events, concurrency,
			sent, accepted, duplicates, rejected, server_errors, other,
			transport_errors, p50_ms, p95_ms, p99_ms, avg_ms, duration_ms
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
		RETURNING `+loadgenRunColumns,
		run.Route, run.Mode, run.Seed, run.Target, run.Events, run.Concurrency,
		run.Sent, run.Accepted, run.Duplicates, run.Rejected, run.ServerErrors, run.Other,
		run.TransportErrors, run.P50MS, run.P95MS, run.P99MS, run.AvgMS, run.DurationMS,
	)
	stored, err := scanLoadgenRun(row)
	if err != nil {
		return LoadgenRun{}, mapError("insert loadgen run", err)
	}
	return stored, nil
}

// ListLoadgenRuns returns the most recent runs, newest first.
func (s *Store) ListLoadgenRuns(ctx context.Context, limit int) ([]LoadgenRun, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+loadgenRunColumns+`
		FROM loadgen_runs
		ORDER BY created_at DESC, id DESC
		LIMIT $1`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list loadgen runs: %w", err)
	}
	defer rows.Close()

	runs := make([]LoadgenRun, 0)
	for rows.Next() {
		run, err := scanLoadgenRun(rows)
		if err != nil {
			return nil, fmt.Errorf("scan loadgen run: %w", err)
		}
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate loadgen runs: %w", err)
	}
	return runs, nil
}

// LatestLoadgenRuns returns the newest run for each route (one guarded, one
// naive when both exist). This is what the comparison page reads.
func (s *Store) LatestLoadgenRuns(ctx context.Context) ([]LoadgenRun, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT ON (route) `+loadgenRunColumns+`
		FROM loadgen_runs
		ORDER BY route, created_at DESC, id DESC`)
	if err != nil {
		return nil, fmt.Errorf("list latest loadgen runs: %w", err)
	}
	defer rows.Close()

	runs := make([]LoadgenRun, 0, 2)
	for rows.Next() {
		run, err := scanLoadgenRun(rows)
		if err != nil {
			return nil, fmt.Errorf("scan latest loadgen run: %w", err)
		}
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate latest loadgen runs: %w", err)
	}
	return runs, nil
}
