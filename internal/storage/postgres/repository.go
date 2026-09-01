package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/InfraVex/api-monitor/internal/target"
)

// TargetRepository is a PostgreSQL-backed implementation of
// target.Repository. It is the second implementation of that interface —
// storage.MemoryTargetRepository (M2) is the first — and satisfies it
// without any change to target.Repository itself or to target.Service,
// the same "swap the implementation behind an existing interface" pattern
// that let M5's worker pool slot in without touching internal/scheduler.
type TargetRepository struct {
	pool *pgxpool.Pool
}

// NewTargetRepository creates a TargetRepository backed by pool. pool is
// accepted as a dependency (built via NewPool) rather than constructed
// internally, so main.go controls the pool's lifetime — including
// closing it during shutdown — independently of the repository itself.
func NewTargetRepository(pool *pgxpool.Pool) *TargetRepository {
	return &TargetRepository{pool: pool}
}

const selectColumns = `id, name, url, method, interval_ns, timeout_ns, expected_status_code, enabled`

// Create inserts a new target row. A primary key collision (practically
// unreachable, since IDs are server-generated UUIDs — see internal/id)
// maps to target.ErrAlreadyExists rather than a raw PostgreSQL error, so
// callers never need to know this repository is backed by PostgreSQL at
// all.
func (r *TargetRepository) Create(ctx context.Context, t target.Target) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO targets (`+selectColumns+`) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		t.ID, t.Name, t.URL, t.Method, t.Interval.Nanoseconds(), t.Timeout.Nanoseconds(), t.ExpectedStatusCode, t.Enabled,
	)
	if err != nil {
		return mapError(err)
	}
	return nil
}

// Get retrieves the target with the given ID, or target.ErrNotFound if
// none exists.
func (r *TargetRepository) Get(ctx context.Context, id string) (target.Target, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+selectColumns+` FROM targets WHERE id = $1`, id)
	t, err := scanTarget(row)
	if err != nil {
		return target.Target{}, mapError(err)
	}
	return t, nil
}

// List returns all stored targets, ordered by name then id — the same
// ordering storage.MemoryTargetRepository uses (see its own doc comment
// for why: the domain model has no creation timestamp to order by), kept
// identical here via an explicit ORDER BY rather than relying on
// PostgreSQL's incidental row order, which is not guaranteed stable.
func (r *TargetRepository) List(ctx context.Context) ([]target.Target, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+selectColumns+` FROM targets ORDER BY name, id`)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()

	list := make([]target.Target, 0)
	for rows.Next() {
		t, err := scanTarget(rows)
		if err != nil {
			return nil, mapError(err)
		}
		list = append(list, t)
	}
	if err := rows.Err(); err != nil {
		return nil, mapError(err)
	}
	return list, nil
}

// Update replaces a stored target's row. It returns target.ErrNotFound if
// no row with the given ID exists — the same behavior
// storage.MemoryTargetRepository provides, kept consistent here by
// checking rows affected rather than assuming the row exists (in
// practice, target.Service.Update already fetches the target with Get
// before calling Update, but this repository still guards its own
// contract independently, e.g. against a target deleted concurrently
// between those two calls).
func (r *TargetRepository) Update(ctx context.Context, t target.Target) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE targets SET name = $2, url = $3, method = $4, interval_ns = $5, timeout_ns = $6, expected_status_code = $7, enabled = $8 WHERE id = $1`,
		t.ID, t.Name, t.URL, t.Method, t.Interval.Nanoseconds(), t.Timeout.Nanoseconds(), t.ExpectedStatusCode, t.Enabled,
	)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() == 0 {
		return target.ErrNotFound
	}
	return nil
}

// Delete removes a stored target's row. It returns target.ErrNotFound if
// no row with the given ID exists.
func (r *TargetRepository) Delete(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM targets WHERE id = $1`, id)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() == 0 {
		return target.ErrNotFound
	}
	return nil
}

// rowScanner is satisfied by both pgx.Row (QueryRow) and pgx.Rows (a
// single row of Query's result set), letting Get and List share one scan
// implementation.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanTarget(row rowScanner) (target.Target, error) {
	var (
		t                     target.Target
		intervalNs, timeoutNs int64
	)
	err := row.Scan(&t.ID, &t.Name, &t.URL, &t.Method, &intervalNs, &timeoutNs, &t.ExpectedStatusCode, &t.Enabled)
	if err != nil {
		return target.Target{}, err
	}
	t.Interval = time.Duration(intervalNs)
	t.Timeout = time.Duration(timeoutNs)
	return t, nil
}

// mapError translates PostgreSQL/pgx-specific errors into the sentinel
// errors target.Repository's contract promises, so target.Service (and
// everything above it) never needs to know this implementation is
// PostgreSQL-backed. Anything not specifically recognized is returned
// as-is, wrapped with context.
func mapError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return target.ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
		return target.ErrAlreadyExists
	}
	return err
}
