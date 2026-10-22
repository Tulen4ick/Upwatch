package postgres

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/Tulen4ick/Upwatch/internal/monitor"
)

func newTestRepo(t *testing.T) *MonitorRepo {
	t.Helper()
	dsn := os.Getenv("UPWATCH_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("UPWATCH_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()

	pool, err := NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	t.Cleanup(pool.Close)

	if _, err := pool.Exec(ctx, `TRUNCATE monitors RESTART IDENTITY`); err != nil {
		t.Fatalf("truncate: %v (миграции применены к тестовой БД?)", err)
	}
	return NewMonitorRepo(pool)
}

func input(name string) monitor.Input {
	in := monitor.Input{Name: name, URL: "https://example.com", IntervalSeconds: 60}
	in.Normalize()
	return in
}

func TestMonitorRepo_CreateAndGet(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	created, err := repo.Create(ctx, input("Example"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.ID == 0 {
		t.Error("ID was not filled from the database")
	}
	if created.CreatedAt.IsZero() || created.UpdatedAt.IsZero() {
		t.Error("CreatedAt/UpdatedAt were not filled from the database")
	}
	if !created.Enabled || created.TimeoutSeconds != monitor.DefaultTimeoutSeconds {
		t.Errorf("unexpected monitor: %+v", created)
	}

	got, err := repo.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != created.ID || got.Name != "Example" || got.URL != created.URL {
		t.Errorf("Get() = %+v, want %+v", got, created)
	}
}

func TestMonitorRepo_GetNotFound(t *testing.T) {
	repo := newTestRepo(t)

	_, err := repo.Get(context.Background(), 12345)
	if !errors.Is(err, monitor.ErrNotFound) {
		t.Fatalf("Get() error = %v, want monitor.ErrNotFound", err)
	}
}

func TestMonitorRepo_List(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	list, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("List on empty table: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("len = %d, want 0", len(list))
	}

	for _, name := range []string{"first", "second", "third"} {
		if _, err := repo.Create(ctx, input(name)); err != nil {
			t.Fatalf("Create(%s): %v", name, err)
		}
	}

	list, err = repo.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 3 || list[0].Name != "first" || list[2].Name != "third" {
		t.Errorf("List() = %+v, want 3 monitors ordered by id", list)
	}
}

func TestMonitorRepo_Update(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	created, err := repo.Create(ctx, input("before"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	in := input("after")
	in.URL = "http://localhost:3000/health"
	in.IntervalSeconds = 300
	in.TimeoutSeconds = 30
	disabled := false
	in.Enabled = &disabled

	updated, err := repo.Update(ctx, created.ID, in)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Name != "after" || updated.URL != in.URL || updated.IntervalSeconds != 300 ||
		updated.TimeoutSeconds != 30 || updated.Enabled {
		t.Errorf("unexpected monitor after update: %+v", updated)
	}
	if !updated.CreatedAt.Equal(created.CreatedAt) {
		t.Error("CreatedAt must not change on update")
	}
	if !updated.UpdatedAt.After(created.UpdatedAt) {
		t.Error("UpdatedAt must move forward on update")
	}

	if _, err := repo.Update(ctx, 12345, in); !errors.Is(err, monitor.ErrNotFound) {
		t.Errorf("Update(missing) error = %v, want monitor.ErrNotFound", err)
	}
}

func TestMonitorRepo_Delete(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	created, err := repo.Create(ctx, input("doomed"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := repo.Delete(ctx, created.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := repo.Get(ctx, created.ID); !errors.Is(err, monitor.ErrNotFound) {
		t.Errorf("Get after Delete error = %v, want monitor.ErrNotFound", err)
	}
	if err := repo.Delete(ctx, created.ID); !errors.Is(err, monitor.ErrNotFound) {
		t.Errorf("second Delete error = %v, want monitor.ErrNotFound", err)
	}
}

func TestMonitorRepo_DatabaseConstraints(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	bad := map[string]func(in *monitor.Input){
		"interval too small":  func(in *monitor.Input) { in.IntervalSeconds = 5; in.TimeoutSeconds = 1 },
		"timeout too big":     func(in *monitor.Input) { in.IntervalSeconds = 600; in.TimeoutSeconds = 61 },
		"timeout >= interval": func(in *monitor.Input) { in.IntervalSeconds = 30; in.TimeoutSeconds = 30 },
		"empty name":          func(in *monitor.Input) { in.Name = "" },
	}
	for name, modify := range bad {
		t.Run(name, func(t *testing.T) {
			in := input("x")
			modify(&in)
			if _, err := repo.Create(ctx, in); err == nil {
				t.Error("Create() error = nil, want CHECK constraint violation")
			}
		})
	}
}
