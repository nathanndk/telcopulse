// Package store persists domain operations atomically in PostgreSQL.
package store

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"telcopulse/services/shared/telemetry"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"telcopulse/services/shared/domain"
)

//go:embed migrations/*.sql
var migrations embed.FS

// ErrNotFound indicates a missing domain entity.
var ErrNotFound = errors.New("resource not found")

// ErrConflict indicates that an idempotency key was reused for different inputs.
var ErrConflict = errors.New("idempotency key already used for a different request")

// Store owns the bounded PostgreSQL pool.
type Store struct{ Pool *pgxpool.Pool }

// Open establishes a database connection and applies the initial idempotent schema.
func Open(ctx context.Context, url string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("database config: %w", err)
	}
	cfg.ConnConfig.Tracer = telemetry.DatabaseTracer{}
	cfg.MaxConns = 10
	cfg.MinConns = 1
	cfg.MaxConnLifetime = time.Hour
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("database pool: %w", err)
	}
	if err = pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("database ping: %w", err)
	}
	if err = migrate(ctx, pool); err != nil {
		pool.Close()
		return nil, err
	}

	return &Store{Pool: pool}, nil
}

// Transaction retrieves persisted evidence by transaction identifier.
func (s *Store) Transaction(ctx context.Context, id string) (domain.Transaction, error) {
	var out domain.Transaction
	var b []byte
	err := s.Pool.QueryRow(ctx, "SELECT result FROM operation_transactions WHERE id=$1", id).Scan(&b)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, ErrNotFound
	}
	if err != nil {
		return out, err
	}
	err = json.Unmarshal(b, &out)
	return out, err
}

// Transactions provides server-side filtering and bounded pagination.
func (s *Store) Transactions(ctx context.Context, env, status, search string, page, size int) (domain.TransactionPage, error) {
	out := domain.TransactionPage{Items: []domain.Transaction{}, Page: page, PageSize: size}

	const where = ` FROM operation_transactions WHERE environment=$1 AND ($2='' OR status=$2) AND ($3='' OR position(lower($3) in lower(id || ' ' || trace_id || ' ' || (result->>'customer_name')))>0)`
	if err := s.Pool.QueryRow(ctx, "SELECT count(*)"+where, env, status, search).Scan(&out.Total); err != nil {
		return out, err
	}
	rows, err := s.Pool.Query(ctx, "SELECT result"+where+" ORDER BY created_at DESC,id DESC LIMIT $4 OFFSET $5", env, status, search, size, (page-1)*size)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var b []byte
		var t domain.Transaction
		if err = rows.Scan(&b); err != nil {
			return out, err
		}
		if err = json.Unmarshal(b, &t); err != nil {
			return out, err
		}
		out.Items = append(out.Items, t)
	}
	return out, rows.Err()
}

// Overview computes the last hour's business metrics and minute buckets.
func (s *Store) Overview(ctx context.Context, env string) (domain.Overview, error) {
	out := domain.Overview{Series: []domain.Point{}}
	err := s.Pool.QueryRow(ctx, `SELECT count(*),count(*) FILTER(WHERE status='SUCCESS'),count(*) FILTER(WHERE status='FAILED'),COALESCE(percentile_cont(0.95) WITHIN GROUP(ORDER BY (result->>'duration_ms')::float),0),count(*) FILTER(WHERE created_at>now()-interval '1 minute') FROM transactions WHERE environment=$1 AND created_at>now()-interval '1 hour'`, env).Scan(&out.Total, &out.Success, &out.Failed, &out.P95MS, &out.PerMinute)
	if err != nil {
		return out, err
	}
	if out.Total > 0 {
		out.SuccessRate = 100 * float64(out.Success) / float64(out.Total)
	}
	rows, err := s.Pool.Query(ctx, `SELECT date_trunc('minute',created_at),count(*),100.0*count(*) FILTER(WHERE status='SUCCESS')/count(*),percentile_cont(0.95) WITHIN GROUP(ORDER BY (result->>'duration_ms')::float) FROM transactions WHERE environment=$1 AND created_at>now()-interval '1 hour' GROUP BY 1 ORDER BY 1`, env)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var p domain.Point
		if err = rows.Scan(&p.Time, &p.Total, &p.SuccessRate, &p.P95MS); err != nil {
			return out, err
		}
		out.Series = append(out.Series, p)
	}
	if err = rows.Err(); err != nil {
		return out, err
	}
	var start, end time.Time
	var success, failed int64
	err = s.Pool.QueryRow(ctx, `SELECT now()-interval '30 days',now(),count(*) FILTER(WHERE status='SUCCESS'),count(*) FILTER(WHERE status='FAILED') FROM transactions WHERE environment=$1 AND created_at>=now()-interval '30 days' AND created_at<now()`, env).Scan(&start, &end, &success, &failed)
	if err != nil {
		return out, err
	}
	out.PurchaseSLO = domain.NewPurchaseSLO(start, end, success, failed)
	return out, nil
}

// migrate applies numbered SQL migrations once under a database-wide transaction lock.
func migrate(ctx context.Context, pool *pgxpool.Pool) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(847321)"); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "CREATE TABLE IF NOT EXISTS schema_migrations (version text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())"); err != nil {
		return err
	}
	names, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		return err
	}
	for _, name := range names {
		var exists bool
		if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)", name).Scan(&exists); err != nil {
			return err
		}
		if exists {
			continue
		}
		body, err := migrations.ReadFile(name)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, string(body)); err != nil {
			return fmt.Errorf("migration %s: %w", name, err)
		}
		if _, err = tx.Exec(ctx, "INSERT INTO schema_migrations(version) VALUES($1)", name); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
