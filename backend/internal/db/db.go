// Package db owns the connection pool and the embedded migration scripts.
package db

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

//go:embed migrations/0001_init.sql
var migration0001 string

//go:embed migrations/0002_profile_names.sql
var migration0002 string

//go:embed migrations/0003_profile_allergies.sql
var migration0003 string

//go:embed migrations/0004_oauth.sql
var migration0004 string

//go:embed migrations/0005_travel_independent.sql
var migration0005 string

//go:embed migrations/0006_profile_confirmed.sql
var migration0006 string

//go:embed migrations/0007_travel_independent_per_leg.sql
var migration0007 string

// Open opens the application's *sql.DB through pgx's database/sql adapter and
// configures it for use behind a transaction-pool PgBouncer (the common HA
// layout in front of managed Postgres).
//
// QueryExecModeExec pipelines Parse+Bind+Describe+Execute+Sync into a single
// message group so the whole exchange completes inside one PgBouncer-owned
// transaction; there is no window where the server can be swapped between a
// Parse and its Bind. Statement/description caches are disabled for the same
// reason. Harmless on a direct PG connection.
func Open(url string) (*sql.DB, error) {
	cfg, err := pgx.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse db url: %w", err)
	}
	cfg.DefaultQueryExecMode = pgx.QueryExecModeExec
	cfg.StatementCacheCapacity = 0
	cfg.DescriptionCacheCapacity = 0

	db := stdlib.OpenDB(*cfg)
	// Bound the pool so a single backend can't exhaust the bouncer's client
	// slots, and cap idle lifetime so proxies don't hand back a connection
	// whose server-side peer was already reaped.
	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(5)
	db.SetConnMaxIdleTime(30 * time.Second)
	db.SetConnMaxLifetime(5 * time.Minute)
	for i := 0; i < 30; i++ {
		if err = db.Ping(); err == nil {
			return db, nil
		}
		time.Sleep(time.Second)
	}
	return nil, fmt.Errorf("ping db: %w", err)
}

// Migrate applies the embedded migrations in order. Each script is idempotent
// (IF NOT EXISTS), so re-running is safe.
func Migrate(db *sql.DB) error {
	if _, err := db.Exec(migration0001); err != nil {
		return fmt.Errorf("0001_init: %w", err)
	}
	if _, err := db.Exec(migration0002); err != nil {
		return fmt.Errorf("0002_profile_names: %w", err)
	}
	if _, err := db.Exec(migration0003); err != nil {
		return fmt.Errorf("0003_profile_allergies: %w", err)
	}
	if _, err := db.Exec(migration0004); err != nil {
		return fmt.Errorf("0004_oauth: %w", err)
	}
	if _, err := db.Exec(migration0005); err != nil {
		return fmt.Errorf("0005_travel_independent: %w", err)
	}
	if _, err := db.Exec(migration0006); err != nil {
		return fmt.Errorf("0006_profile_confirmed: %w", err)
	}
	if _, err := db.Exec(migration0007); err != nil {
		return fmt.Errorf("0007_travel_independent_per_leg: %w", err)
	}
	return nil
}

// Exec is the subset of *sql.DB / *sql.Tx that the application reaches for.
// Anything that takes Exec can run inside or outside a transaction.
type Exec interface {
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
}
