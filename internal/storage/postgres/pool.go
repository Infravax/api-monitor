package postgres

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"time"

	"github.com/golang-migrate/migrate/v4"
	pgxmigrate "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5/pgxpool"

	// Registers the "pgx" driver with database/sql. This is used only by
	// Migrate below — the application's own runtime queries go through
	// the native *pgxpool.Pool from NewPool, not database/sql. Migrations
	// use database/sql because that is the interface golang-migrate's
	// database driver is built on; using it here avoids pulling in a
	// second, unrelated PostgreSQL driver (e.g. lib/pq) just for this one
	// step.
	_ "github.com/jackc/pgx/v5/stdlib"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// maxConns is a conservative, explicit default for this application's
// current scale: M6's actual database load is REST API target CRUD plus
// the Scheduler's periodic List() calls — comparatively low-volume
// traffic, nothing like a per-worker-connection need (the M5 worker pool
// and checker do not touch the database at all yet). This is not exposed
// as an environment variable in M6: there is no measured need to tune it
// yet, and adding a knob nobody has a reason to turn would be exactly the
// kind of speculative configuration this project avoids elsewhere. See
// docs/architecture.md for the full reasoning.
const maxConns = 10

// pingTimeout bounds NewPool's connectivity check, so a completely
// unreachable host fails within a bounded time at startup rather than
// hanging on whatever default OS-level TCP timeout applies (or waiting
// forever if the caller passed a context with no deadline of its own).
const pingTimeout = 5 * time.Second

// NewPool creates a connection pool for databaseURL and verifies
// connectivity with a Ping before returning, so a misconfigured or
// unreachable database fails loudly here — at startup — rather than
// letting the application start and pretend persistence works until the
// first request fails.
func NewPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("postgres: invalid database url: %w", err)
	}
	cfg.MaxConns = maxConns

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("postgres: failed to create connection pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres: failed to connect: %w", err)
	}

	return pool, nil
}

// Migrate applies any pending migrations embedded in migrations/*.sql,
// bringing the schema up to date. It is safe to call every time the
// application starts: with nothing pending it returns nil (migrate's
// ErrNoChange is not treated as an error).
//
// Migrations run through their own short-lived database/sql connection,
// separate from the application's long-lived *pgxpool.Pool returned by
// NewPool — golang-migrate's database driver is built on database/sql,
// and there is no reason to share a connection/pool between "run DDL
// once at startup" and "serve queries for the life of the process".
func Migrate(databaseURL string) error {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return fmt.Errorf("postgres: invalid database url: %w", err)
	}

	driver, err := pgxmigrate.WithInstance(db, &pgxmigrate.Config{})
	if err != nil {
		db.Close()
		return fmt.Errorf("postgres: failed to init migration driver: %w", err)
	}

	src, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		db.Close()
		return fmt.Errorf("postgres: failed to load embedded migrations: %w", err)
	}

	m, err := migrate.NewWithInstance("iofs", src, "pgx", driver)
	if err != nil {
		db.Close()
		return fmt.Errorf("postgres: failed to init migrator: %w", err)
	}
	// m.Close() closes both src and the database driver — which in turn
	// closes db, since it was handed to WithInstance directly. That
	// makes this the one place responsible for closing db; the error
	// paths above (before m exists) close it themselves.
	defer m.Close()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("postgres: migration failed: %w", err)
	}

	return nil
}
