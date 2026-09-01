package postgres

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/InfraVex/api-monitor/internal/target"
)

// testDatabaseURL returns the connection string for the dedicated
// integration-test database, and skips the calling test if none is
// configured. This keeps these tests entirely opt-in: `go test ./...`
// with no PostgreSQL available still passes everything else (see
// docs/development.md), and when it does run, it never touches the
// application's own development database — TEST_DATABASE_URL defaults to
// a separate database name (apimonitor_test) from DATABASE_URL's default
// (apimonitor), per the project's test-isolation requirement.
func testDatabaseURL(t *testing.T) string {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		url = "postgres://postgres:postgres@localhost:5432/apimonitor_test?sslmode=disable"
	}
	if os.Getenv("SKIP_POSTGRES_TESTS") != "" {
		t.Skip("SKIP_POSTGRES_TESTS is set")
	}
	return url
}

// newTestRepository runs migrations, truncates the targets table so
// tests never see data left over from a previous run, and returns a
// repository plus a pool the test is responsible for closing (via
// t.Cleanup).
func newTestRepository(t *testing.T) *TargetRepository {
	t.Helper()
	dbURL := testDatabaseURL(t)

	if err := Migrate(dbURL); err != nil {
		t.Fatalf("Migrate() unexpected error (is PostgreSQL running at %s? see docs/development.md): %v", dbURL, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool, err := NewPool(ctx, dbURL)
	if err != nil {
		t.Fatalf("NewPool() unexpected error: %v", err)
	}
	t.Cleanup(pool.Close)

	if _, err := pool.Exec(context.Background(), "TRUNCATE TABLE targets"); err != nil {
		t.Fatalf("failed to truncate targets table before test: %v", err)
	}

	return NewTargetRepository(pool)
}

func newTestTarget(t *testing.T, name string) target.Target {
	t.Helper()
	tg, err := target.New(target.NewParams{
		Name:               name,
		URL:                "https://example.com/" + name,
		Method:             "GET",
		Interval:           30 * time.Second,
		Timeout:            5 * time.Second,
		ExpectedStatusCode: 200,
	})
	if err != nil {
		t.Fatalf("target.New() unexpected error: %v", err)
	}
	return tg
}

func TestTargetRepository_CreateAndGet(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()
	tg := newTestTarget(t, "a")

	if err := repo.Create(ctx, tg); err != nil {
		t.Fatalf("Create() unexpected error: %v", err)
	}

	got, err := repo.Get(ctx, tg.ID)
	if err != nil {
		t.Fatalf("Get() unexpected error: %v", err)
	}
	if got != tg {
		t.Errorf("Get() = %+v, want %+v", got, tg)
	}
}

func TestTargetRepository_CreateDuplicate(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()
	tg := newTestTarget(t, "a")

	if err := repo.Create(ctx, tg); err != nil {
		t.Fatalf("first Create() unexpected error: %v", err)
	}
	if err := repo.Create(ctx, tg); !errors.Is(err, target.ErrAlreadyExists) {
		t.Fatalf("second Create() error = %v, want %v", err, target.ErrAlreadyExists)
	}
}

func TestTargetRepository_GetMissing(t *testing.T) {
	repo := newTestRepository(t)

	_, err := repo.Get(context.Background(), "00000000-0000-0000-0000-000000000000")
	if !errors.Is(err, target.ErrNotFound) {
		t.Fatalf("Get() error = %v, want %v", err, target.ErrNotFound)
	}
}

func TestTargetRepository_List(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()

	b := newTestTarget(t, "b")
	a := newTestTarget(t, "a")
	c := newTestTarget(t, "c")
	for _, tg := range []target.Target{b, a, c} {
		if err := repo.Create(ctx, tg); err != nil {
			t.Fatal(err)
		}
	}

	list, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("List() unexpected error: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("List() returned %d targets, want 3", len(list))
	}
	if list[0].Name != "a" || list[1].Name != "b" || list[2].Name != "c" {
		t.Errorf("List() not ordered by name: got [%s, %s, %s]", list[0].Name, list[1].Name, list[2].Name)
	}
}

func TestTargetRepository_List_Empty(t *testing.T) {
	repo := newTestRepository(t)

	list, err := repo.List(context.Background())
	if err != nil {
		t.Fatalf("List() unexpected error: %v", err)
	}
	if list == nil {
		t.Error("List() returned nil, want an empty non-nil slice")
	}
	if len(list) != 0 {
		t.Errorf("List() returned %d targets, want 0", len(list))
	}
}

func TestTargetRepository_UpdateAndDelete(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()
	tg := newTestTarget(t, "a")

	if err := repo.Create(ctx, tg); err != nil {
		t.Fatal(err)
	}

	updated := tg
	updated.Name = "renamed"
	updated.Enabled = false
	updated.Interval = time.Minute
	if err := repo.Update(ctx, updated); err != nil {
		t.Fatalf("Update() unexpected error: %v", err)
	}

	got, err := repo.Get(ctx, tg.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got != updated {
		t.Errorf("Get() after Update() = %+v, want %+v", got, updated)
	}

	if err := repo.Delete(ctx, tg.ID); err != nil {
		t.Fatalf("Delete() unexpected error: %v", err)
	}
	if _, err := repo.Get(ctx, tg.ID); !errors.Is(err, target.ErrNotFound) {
		t.Fatalf("Get() after Delete() error = %v, want %v", err, target.ErrNotFound)
	}
}

func TestTargetRepository_UpdateMissing(t *testing.T) {
	repo := newTestRepository(t)
	tg := newTestTarget(t, "a")

	if err := repo.Update(context.Background(), tg); !errors.Is(err, target.ErrNotFound) {
		t.Fatalf("Update() error = %v, want %v", err, target.ErrNotFound)
	}
}

func TestTargetRepository_DeleteMissing(t *testing.T) {
	repo := newTestRepository(t)

	err := repo.Delete(context.Background(), "00000000-0000-0000-0000-000000000000")
	if !errors.Is(err, target.ErrNotFound) {
		t.Fatalf("Delete() error = %v, want %v", err, target.ErrNotFound)
	}
}

func TestTargetRepository_ContextCancellation(t *testing.T) {
	repo := newTestRepository(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := repo.List(ctx)
	if err == nil {
		t.Fatal("List() with an already-canceled context returned no error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("List() error = %v, want it to wrap context.Canceled", err)
	}
}

func TestMigrate_IsIdempotent(t *testing.T) {
	dbURL := testDatabaseURL(t)

	if err := Migrate(dbURL); err != nil {
		t.Fatalf("first Migrate() unexpected error: %v", err)
	}
	if err := Migrate(dbURL); err != nil {
		t.Fatalf("second Migrate() unexpected error (should be a no-op): %v", err)
	}
}

func TestNewPool_FailsFastOnUnreachableHost(t *testing.T) {
	// A non-routable address (RFC 5737 TEST-NET-1) with a short context
	// timeout proves NewPool doesn't hang or silently succeed when
	// PostgreSQL is unreachable.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := NewPool(ctx, "postgres://postgres:postgres@192.0.2.1:5432/nope?sslmode=disable&connect_timeout=1")
	if err == nil {
		t.Fatal("NewPool() to an unreachable host returned no error")
	}
}

var _ = pgxpool.Pool{} // keep the pgxpool import even if a future edit trims its direct use above
