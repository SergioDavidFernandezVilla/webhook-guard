package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Metric is one counter bucket. Count is the delta to add.
type Metric struct {
	Name          string
	WindowStart   time.Time
	WindowSeconds int
	Count         int64
}

// MetricPoint is one persisted counter bucket as read back by the dashboard.
type MetricPoint struct {
	Name          string
	WindowStart   time.Time
	WindowSeconds int
	Count         int64
}

// ListMetrics returns every bucket whose window starts at or after since,
// ordered oldest first. Not used by the service path; it is the read side of
// the dashboard API.
func (s *Store) ListMetrics(ctx context.Context, since time.Time) ([]MetricPoint, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT name, window_start, window_seconds, count
		FROM metrics
		WHERE window_start >= $1
		ORDER BY window_start, name`,
		since,
	)
	if err != nil {
		return nil, fmt.Errorf("list metrics: %w", err)
	}
	defer rows.Close()

	points := make([]MetricPoint, 0)
	for rows.Next() {
		var p MetricPoint
		if err := rows.Scan(&p.Name, &p.WindowStart, &p.WindowSeconds, &p.Count); err != nil {
			return nil, fmt.Errorf("scan metric point: %w", err)
		}
		points = append(points, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate metrics: %w", err)
	}
	return points, nil
}

// AddTx increments a metric bucket, creating it on first use.
func (s *Store) AddTx(ctx context.Context, tx pgx.Tx, m Metric) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO metrics (name, window_start, window_seconds, count)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (name, window_start, window_seconds) DO UPDATE SET
			count = metrics.count + EXCLUDED.count`,
		m.Name, m.WindowStart, m.WindowSeconds, m.Count,
	)
	if err != nil {
		return mapError("add metric", err)
	}
	return nil
}
