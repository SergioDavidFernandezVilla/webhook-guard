// Package store is the only place that talks to PostgreSQL. It exposes a
// connection pool, transaction helpers and one file per table.
package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	// ErrNotFound reports that a requested row does not exist.
	ErrNotFound = errors.New("store: not found")
	// ErrDuplicate reports a unique-constraint violation.
	ErrDuplicate = errors.New("store: duplicate")
)

// OpenOptions tunes the connection pool. Zero values keep the pgx defaults.
type OpenOptions struct {
	MaxConns       int32
	MinConns       int32
	ConnectTimeout time.Duration
}

// Store owns the PostgreSQL pool.
type Store struct {
	pool *pgxpool.Pool
}

// Open parses the DSN, builds the pool and pings the database before returning.
func Open(ctx context.Context, dsn string, opts OpenOptions) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	if opts.MaxConns > 0 {
		cfg.MaxConns = opts.MaxConns
	}
	if opts.MinConns > 0 {
		cfg.MinConns = opts.MinConns
	}
	if opts.ConnectTimeout > 0 {
		cfg.ConnConfig.ConnectTimeout = opts.ConnectTimeout
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return &Store{pool: pool}, nil
}

// Close releases every pooled connection.
func (s *Store) Close() {
	s.pool.Close()
}

// Ping reports whether the database is reachable.
func (s *Store) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

// Pool exposes the underlying pool to integrations that must share the same
// connections (River) and to integration tests.
func (s *Store) Pool() *pgxpool.Pool {
	return s.pool
}

// RunInTx runs fn inside a transaction and commits only when fn returns nil.
// Any error (or panic) triggers a rollback; a rollback failure is joined to the
// original error instead of being swallowed.
func (s *Store) RunInTx(ctx context.Context, fn func(context.Context, pgx.Tx) error) (err error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	committed := false
	defer func() {
		if committed {
			return
		}
		// The rollback must survive a cancelled request context (for example a
		// client that hung up), so it runs on a context that cannot be
		// cancelled; otherwise the rollback itself would fail.
		rollbackErr := tx.Rollback(context.WithoutCancel(ctx))
		if rollbackErr == nil || errors.Is(rollbackErr, pgx.ErrTxClosed) {
			return
		}
		wrapped := fmt.Errorf("rollback transaction: %w", rollbackErr)
		if err == nil {
			err = wrapped
		} else {
			err = errors.Join(err, wrapped)
		}
	}()

	if fnErr := fn(ctx, tx); fnErr != nil {
		return fnErr
	}

	if commitErr := tx.Commit(ctx); commitErr != nil {
		return fmt.Errorf("commit transaction: %w", commitErr)
	}

	committed = true
	return nil
}

// mapError translates driver errors into the package's domain errors.
func mapError(action string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%s: %w", action, ErrNotFound)
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return fmt.Errorf("%s: %w", action, ErrDuplicate)
	}
	return fmt.Errorf("%s: %w", action, err)
}
