package target

import (
	"context"
	"errors"
	"testing"
	"time"
)

// fakeRepository is a minimal, non-concurrent Repository implementation
// used only by these tests. The real, concurrency-safe implementation
// lives in internal/storage; it can't be imported here without creating an
// import cycle (storage already imports target).
type fakeRepository struct {
	targets map[string]Target
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{targets: make(map[string]Target)}
}

func (f *fakeRepository) Create(_ context.Context, t Target) error {
	if _, ok := f.targets[t.ID]; ok {
		return ErrAlreadyExists
	}
	f.targets[t.ID] = t
	return nil
}

func (f *fakeRepository) Get(_ context.Context, id string) (Target, error) {
	t, ok := f.targets[id]
	if !ok {
		return Target{}, ErrNotFound
	}
	return t, nil
}

func (f *fakeRepository) List(_ context.Context) ([]Target, error) {
	list := make([]Target, 0, len(f.targets))
	for _, t := range f.targets {
		list = append(list, t)
	}
	return list, nil
}

func (f *fakeRepository) Update(_ context.Context, t Target) error {
	if _, ok := f.targets[t.ID]; !ok {
		return ErrNotFound
	}
	f.targets[t.ID] = t
	return nil
}

func (f *fakeRepository) Delete(_ context.Context, id string) error {
	if _, ok := f.targets[id]; !ok {
		return ErrNotFound
	}
	delete(f.targets, id)
	return nil
}

func validNewParams() NewParams {
	return NewParams{
		Name:               "Example API",
		URL:                "https://api.example.com/health",
		Method:             "GET",
		Interval:           30 * time.Second,
		Timeout:            5 * time.Second,
		ExpectedStatusCode: 200,
	}
}

func TestService_Create(t *testing.T) {
	svc := NewService(newFakeRepository())

	tg, err := svc.Create(context.Background(), validNewParams())
	if err != nil {
		t.Fatalf("Create() unexpected error: %v", err)
	}
	if !tg.Enabled {
		t.Error("Create() should produce an enabled target")
	}

	got, err := svc.Get(context.Background(), tg.ID)
	if err != nil {
		t.Fatalf("Get() unexpected error: %v", err)
	}
	if got != tg {
		t.Errorf("Get() = %+v, want %+v", got, tg)
	}
}

func TestService_Create_ValidationError(t *testing.T) {
	svc := NewService(newFakeRepository())

	p := validNewParams()
	p.URL = ""

	if _, err := svc.Create(context.Background(), p); !errors.Is(err, ErrEmptyURL) {
		t.Fatalf("Create() error = %v, want %v", err, ErrEmptyURL)
	}
}

func TestService_Get_NotFound(t *testing.T) {
	svc := NewService(newFakeRepository())

	if _, err := svc.Get(context.Background(), "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get() error = %v, want %v", err, ErrNotFound)
	}
}

func TestService_List(t *testing.T) {
	svc := NewService(newFakeRepository())
	ctx := context.Background()

	if _, err := svc.Create(ctx, validNewParams()); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(ctx, validNewParams()); err != nil {
		t.Fatal(err)
	}

	list, err := svc.List(ctx)
	if err != nil {
		t.Fatalf("List() unexpected error: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("List() returned %d targets, want 2", len(list))
	}
}

func TestService_Update(t *testing.T) {
	svc := NewService(newFakeRepository())
	ctx := context.Background()

	tg, err := svc.Create(ctx, validNewParams())
	if err != nil {
		t.Fatal(err)
	}

	updated, err := svc.Update(ctx, tg.ID, UpdateParams{
		Name:               "Renamed API",
		URL:                "https://api.example.com/v2/health",
		Method:             "POST",
		Interval:           time.Minute,
		Timeout:            10 * time.Second,
		ExpectedStatusCode: 204,
		Enabled:            false,
	})
	if err != nil {
		t.Fatalf("Update() unexpected error: %v", err)
	}

	if updated.ID != tg.ID {
		t.Errorf("Update() changed the ID: got %q, want %q", updated.ID, tg.ID)
	}
	if updated.Name != "Renamed API" || updated.Enabled {
		t.Errorf("Update() did not apply new fields: %+v", updated)
	}
}

func TestService_Update_NotFound(t *testing.T) {
	svc := NewService(newFakeRepository())

	p := UpdateParams{
		Name: "x", URL: "https://a.com", Method: "GET",
		Interval: time.Second, Timeout: time.Second, ExpectedStatusCode: 200,
	}
	if _, err := svc.Update(context.Background(), "missing", p); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Update() error = %v, want %v", err, ErrNotFound)
	}
}

func TestService_Update_ValidationError(t *testing.T) {
	svc := NewService(newFakeRepository())
	ctx := context.Background()

	tg, err := svc.Create(ctx, validNewParams())
	if err != nil {
		t.Fatal(err)
	}

	p := UpdateParams{
		Name: "x", URL: "not-a-url", Method: "GET",
		Interval: time.Second, Timeout: time.Second, ExpectedStatusCode: 200,
	}
	if _, err := svc.Update(ctx, tg.ID, p); !errors.Is(err, ErrInvalidURL) {
		t.Fatalf("Update() error = %v, want %v", err, ErrInvalidURL)
	}
}

func TestService_Delete(t *testing.T) {
	svc := NewService(newFakeRepository())
	ctx := context.Background()

	tg, err := svc.Create(ctx, validNewParams())
	if err != nil {
		t.Fatal(err)
	}

	if err := svc.Delete(ctx, tg.ID); err != nil {
		t.Fatalf("Delete() unexpected error: %v", err)
	}
	if _, err := svc.Get(ctx, tg.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get() after Delete() error = %v, want %v", err, ErrNotFound)
	}
}

func TestService_Delete_NotFound(t *testing.T) {
	svc := NewService(newFakeRepository())

	if err := svc.Delete(context.Background(), "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Delete() error = %v, want %v", err, ErrNotFound)
	}
}
