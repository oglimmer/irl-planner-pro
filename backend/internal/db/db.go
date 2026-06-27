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

//go:embed migrations/0008_event_hotel_link.sql
var migration0008 string

//go:embed migrations/0009_event_image.sql
var migration0009 string

//go:embed migrations/0010_event_attendees.sql
var migration0010 string

//go:embed migrations/0011_activity_category.sql
var migration0011 string

//go:embed migrations/0012_messaging.sql
var migration0012 string

//go:embed migrations/0013_message_send_log.sql
var migration0013 string

// migrations lists all embedded migrations in apply order.
// Each entry must have a corresponding //go:embed var above.
// Adding a new migration requires (1) the file, (2) the embed var, (3) an
// entry here. The loop below makes (3) impossible to forget relative to (2).
var migrations = []struct {
	name string
	sql  string
}{
	{"0001_init", migration0001},
	{"0002_profile_names", migration0002},
	{"0003_profile_allergies", migration0003},
	{"0004_oauth", migration0004},
	{"0005_travel_independent", migration0005},
	{"0006_profile_confirmed", migration0006},
	{"0007_travel_independent_per_leg", migration0007},
	{"0008_event_hotel_link", migration0008},
	{"0009_event_image", migration0009},
	{"0010_event_attendees", migration0010},
	{"0011_activity_category", migration0011},
	{"0012_messaging", migration0012},
	{"0013_message_send_log", migration0013},
}

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
	for _, m := range migrations {
		if _, err := db.Exec(m.sql); err != nil {
			return fmt.Errorf("%s: %w", m.name, err)
		}
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
