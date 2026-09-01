package storage

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/InfraVex/api-monitor/internal/target"
)

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

func TestMemoryTargetRepository_CreateAndGet(t *testing.T) {
	repo := NewMemoryTargetRepository()
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

func TestMemoryTargetRepository_CreateDuplicate(t *testing.T) {
	repo := NewMemoryTargetRepository()
	ctx := context.Background()
	tg := newTestTarget(t, "a")

	if err := repo.Create(ctx, tg); err != nil {
		t.Fatalf("first Create() unexpected error: %v", err)
	}
	if err := repo.Create(ctx, tg); !errors.Is(err, target.ErrAlreadyExists) {
		t.Fatalf("second Create() error = %v, want %v", err, target.ErrAlreadyExists)
	}
}

func TestMemoryTargetRepository_GetMissing(t *testing.T) {
	repo := NewMemoryTargetRepository()
	if _, err := repo.Get(context.Background(), "missing"); !errors.Is(err, target.ErrNotFound) {
		t.Fatalf("Get() error = %v, want %v", err, target.ErrNotFound)
	}
}

func TestMemoryTargetRepository_List(t *testing.T) {
	repo := NewMemoryTargetRepository()
	ctx := context.Background()

	b := newTestTarget(t, "b")
	a := newTestTarget(t, "a")
	if err := repo.Create(ctx, b); err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(ctx, a); err != nil {
		t.Fatal(err)
	}

	list, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("List() unexpected error: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("List() returned %d targets, want 2", len(list))
	}
	if list[0].Name != "a" || list[1].Name != "b" {
		t.Errorf("List() not sorted by name: got [%s, %s]", list[0].Name, list[1].Name)
	}
}

func TestMemoryTargetRepository_UpdateAndDelete(t *testing.T) {
	repo := NewMemoryTargetRepository()
	ctx := context.Background()
	tg := newTestTarget(t, "a")

	if err := repo.Create(ctx, tg); err != nil {
		t.Fatal(err)
	}

	tg.Enabled = false
	if err := repo.Update(ctx, tg); err != nil {
		t.Fatalf("Update() unexpected error: %v", err)
	}
	got, err := repo.Get(ctx, tg.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Enabled {
		t.Error("Update() did not persist the change")
	}

	if err := repo.Delete(ctx, tg.ID); err != nil {
		t.Fatalf("Delete() unexpected error: %v", err)
	}
	if _, err := repo.Get(ctx, tg.ID); !errors.Is(err, target.ErrNotFound) {
		t.Fatalf("Get() after Delete() error = %v, want %v", err, target.ErrNotFound)
	}
}

func TestMemoryTargetRepository_UpdateMissing(t *testing.T) {
	repo := NewMemoryTargetRepository()
	tg := newTestTarget(t, "a")

	if err := repo.Update(context.Background(), tg); !errors.Is(err, target.ErrNotFound) {
		t.Fatalf("Update() error = %v, want %v", err, target.ErrNotFound)
	}
}

func TestMemoryTargetRepository_DeleteMissing(t *testing.T) {
	repo := NewMemoryTargetRepository()

	if err := repo.Delete(context.Background(), "missing"); !errors.Is(err, target.ErrNotFound) {
		t.Fatalf("Delete() error = %v, want %v", err, target.ErrNotFound)
	}
}

// TestMemoryTargetRepository_ConcurrentAccess exercises Create, Get, List,
// Update, and Delete from many goroutines at once. It is meant to be run
// with -race; it does not assert much beyond "nothing panics/races",
// which is exactly the property this repository needs to guarantee once
// the HTTP server is handling concurrent requests.
func TestMemoryTargetRepository_ConcurrentAccess(t *testing.T) {
	repo := NewMemoryTargetRepository()
	ctx := context.Background()

	const n = 50
	targets := make([]target.Target, n)
	for i := range targets {
		targets[i] = newTestTarget(t, "concurrent")
	}

	var wg sync.WaitGroup
	for _, tg := range targets {
		wg.Add(1)
		go func(tg target.Target) {
			defer wg.Done()
			if err := repo.Create(ctx, tg); err != nil {
				t.Errorf("Create() unexpected error: %v", err)
			}
		}(tg)
	}
	wg.Wait()

	for _, tg := range targets {
		wg.Add(3)
		go func(tg target.Target) {
			defer wg.Done()
			if _, err := repo.Get(ctx, tg.ID); err != nil {
				t.Errorf("Get() unexpected error: %v", err)
			}
		}(tg)
		go func() {
			defer wg.Done()
			if _, err := repo.List(ctx); err != nil {
				t.Errorf("List() unexpected error: %v", err)
			}
		}()
		go func(tg target.Target) {
			defer wg.Done()
			tg.Enabled = false
			if err := repo.Update(ctx, tg); err != nil {
				t.Errorf("Update() unexpected error: %v", err)
			}
		}(tg)
	}
	wg.Wait()

	for _, tg := range targets {
		wg.Add(1)
		go func(tg target.Target) {
			defer wg.Done()
			if err := repo.Delete(ctx, tg.ID); err != nil {
				t.Errorf("Delete() unexpected error: %v", err)
			}
		}(tg)
	}
	wg.Wait()

	list, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("List() unexpected error: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("List() after deleting everything returned %d targets, want 0", len(list))
	}
}
