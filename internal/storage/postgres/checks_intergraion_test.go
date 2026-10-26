package postgres

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Tulen4ick/Upwatch/internal/monitor"
)

func createMonitors(t *testing.T, pool *pgxpool.Pool, n int) []monitor.Monitor {
	t.Helper()
	repo := NewMonitorRepo(pool)
	out := make([]monitor.Monitor, 0, n)
	for range n {
		m, err := repo.Create(context.Background(), input("m"))
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		out = append(out, m)
	}
	return out
}

func now() time.Time {
	return time.Now().Truncate(time.Microsecond)
}

func TestCheckRepo_ClaimDue_OnlyEnabledAndDue(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	createMonitors(t, pool, 2)

	off := input("disabled")
	disabled := false
	off.Enabled = &disabled
	if _, err := NewMonitorRepo(pool).Create(ctx, off); err != nil {
		t.Fatalf("Create: %v", err)
	}

	repo := NewCheckRepo(pool)

	got, err := repo.ClaimDue(ctx, 10)
	if err != nil {
		t.Fatalf("ClaimDue: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("first ClaimDue returned %d monitors, want 2 enabled", len(got))
	}
	for _, m := range got {
		if !m.Enabled {
			t.Errorf("ClaimDue returned disabled monitor %d", m.ID)
		}
	}

	got, err = repo.ClaimDue(ctx, 10)
	if err != nil {
		t.Fatalf("second ClaimDue: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("second ClaimDue returned %d monitors, want 0 (next_check_at must move forward)", len(got))
	}
}

func TestCheckRepo_ClaimDue_Limit(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	createMonitors(t, pool, 5)
	repo := NewCheckRepo(pool)

	first, err := repo.ClaimDue(ctx, 2)
	if err != nil {
		t.Fatalf("ClaimDue: %v", err)
	}
	rest, err := repo.ClaimDue(ctx, 10)
	if err != nil {
		t.Fatalf("ClaimDue: %v", err)
	}
	if len(first) != 2 || len(rest) != 3 {
		t.Errorf("got %d then %d monitors, want 2 then 3", len(first), len(rest))
	}
}

func TestCheckRepo_ClaimDue_DueAgain(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	ms := createMonitors(t, pool, 1)
	repo := NewCheckRepo(pool)

	if got, err := repo.ClaimDue(ctx, 10); err != nil || len(got) != 1 {
		t.Fatalf("ClaimDue = %d monitors, %v; want 1", len(got), err)
	}

	if _, err := pool.Exec(ctx, `UPDATE monitors SET next_check_at = now() - interval '1 second' WHERE id = $1`, ms[0].ID); err != nil {
		t.Fatalf("move next_check_at: %v", err)
	}

	got, err := repo.ClaimDue(ctx, 10)
	if err != nil {
		t.Fatalf("ClaimDue: %v", err)
	}
	if len(got) != 1 || got[0].ID != ms[0].ID {
		t.Errorf("got %+v, want monitor %d to be due again", got, ms[0].ID)
	}
}

func TestCheckRepo_ClaimDue_PushesByInterval(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()

	in := input("slow")
	in.IntervalSeconds = 300
	m, err := NewMonitorRepo(pool).Create(ctx, in)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := NewCheckRepo(pool).ClaimDue(ctx, 10); err != nil {
		t.Fatalf("ClaimDue: %v", err)
	}

	var secondsAhead float64
	err = pool.QueryRow(ctx,
		`SELECT EXTRACT(EPOCH FROM next_check_at - now()) FROM monitors WHERE id = $1`, m.ID).Scan(&secondsAhead)
	if err != nil {
		t.Fatalf("read next_check_at: %v", err)
	}
	if secondsAhead < 295 || secondsAhead > 301 {
		t.Errorf("next_check_at is %.1fs ahead, want about 300s", secondsAhead)
	}
}

func TestCheckRepo_ClaimDue_Concurrent(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	createMonitors(t, pool, 20)
	repo := NewCheckRepo(pool)

	var (
		wg      sync.WaitGroup
		results [2][]monitor.Monitor
		errs    [2]error
	)
	for i := range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = repo.ClaimDue(ctx, 20)
		}()
	}
	wg.Wait()

	seen := map[int64]int{}
	for i := range 2 {
		if errs[i] != nil {
			t.Fatalf("ClaimDue #%d: %v", i, errs[i])
		}
		for _, m := range results[i] {
			seen[m.ID]++
		}
	}
	if len(seen) != 20 {
		t.Errorf("claimed %d distinct monitors, want 20", len(seen))
	}
	for id, n := range seen {
		if n != 1 {
			t.Errorf("monitor %d claimed %d times, want exactly once", id, n)
		}
	}
}

func TestCheckRepo_SaveAndList(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	ms := createMonitors(t, pool, 1)
	repo := NewCheckRepo(pool)
	t0 := now()

	saved := []monitor.CheckResult{
		{MonitorID: ms[0].ID, CheckedAt: t0.Add(-2 * time.Minute), Success: true, StatusCode: 200, LatencyMS: 10},
		{MonitorID: ms[0].ID, CheckedAt: t0.Add(-1 * time.Minute), Success: false, LatencyMS: 1000, Error: "timeout after 1s"},
		{MonitorID: ms[0].ID, CheckedAt: t0, Success: false, StatusCode: 503, LatencyMS: 30, Error: "unexpected status 503"},
	}
	for _, r := range saved {
		if err := repo.SaveResult(ctx, r); err != nil {
			t.Fatalf("SaveResult: %v", err)
		}
	}

	got, err := repo.ListChecks(ctx, ms[0].ID, 2)
	if err != nil {
		t.Fatalf("ListChecks: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}

	for i, want := range []monitor.CheckResult{saved[2], saved[1]} { // новые первыми
		g := got[i]
		if g.ID == 0 {
			t.Errorf("[%d] ID was not filled", i)
		}
		if !g.CheckedAt.Equal(want.CheckedAt) || g.Success != want.Success || g.StatusCode != want.StatusCode ||
			g.LatencyMS != want.LatencyMS || g.Error != want.Error || g.MonitorID != want.MonitorID {
			t.Errorf("[%d] got %+v, want %+v", i, g, want)
		}
	}
}

func TestCheckRepo_SaveStoresNull(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	ms := createMonitors(t, pool, 1)
	repo := NewCheckRepo(pool)

	r := monitor.CheckResult{MonitorID: ms[0].ID, CheckedAt: now(), Success: true, StatusCode: 0, LatencyMS: 5, Error: ""}
	if err := repo.SaveResult(ctx, r); err != nil {
		t.Fatalf("SaveResult: %v", err)
	}

	var statusNull, errorNull bool
	err := pool.QueryRow(ctx,
		`SELECT status_code IS NULL, error IS NULL FROM check_results WHERE monitor_id = $1`, ms[0].ID).
		Scan(&statusNull, &errorNull)
	if err != nil {
		t.Fatalf("read row: %v", err)
	}
	if !statusNull || !errorNull {
		t.Errorf("status_code IS NULL = %v, error IS NULL = %v; want both true (use NULLIF)", statusNull, errorNull)
	}

	got, err := repo.ListChecks(ctx, ms[0].ID, 10)
	if err != nil {
		t.Fatalf("ListChecks: %v", err)
	}
	if len(got) != 1 || got[0].StatusCode != 0 || got[0].Error != "" {
		t.Errorf("got %+v, want StatusCode 0 and empty Error (use COALESCE)", got)
	}
}

func TestCheckRepo_SaveForDeletedMonitor(t *testing.T) {
	repo := NewCheckRepo(newTestPool(t))

	err := repo.SaveResult(context.Background(), monitor.CheckResult{MonitorID: 999999, CheckedAt: now(), LatencyMS: 1})
	if !errors.Is(err, monitor.ErrNotFound) {
		t.Fatalf("SaveResult error = %v, want monitor.ErrNotFound", err)
	}
}

func TestCheckRepo_DeleteCascades(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	ms := createMonitors(t, pool, 1)
	repo := NewCheckRepo(pool)

	for range 2 {
		if err := repo.SaveResult(ctx, monitor.CheckResult{MonitorID: ms[0].ID, CheckedAt: now(), Success: true, StatusCode: 200, LatencyMS: 1}); err != nil {
			t.Fatalf("SaveResult: %v", err)
		}
	}
	if err := NewMonitorRepo(pool).Delete(ctx, ms[0].ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	got, err := repo.ListChecks(ctx, ms[0].ID, 10)
	if err != nil {
		t.Fatalf("ListChecks: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("ListChecks after delete returned %d results, want 0 (ON DELETE CASCADE)", len(got))
	}
}

func TestCheckRepo_Uptime(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	ms := createMonitors(t, pool, 2)
	repo := NewCheckRepo(pool)
	t0 := now()

	results := []monitor.CheckResult{
		{CheckedAt: t0.Add(-50 * time.Minute), Success: true, StatusCode: 200},
		{CheckedAt: t0.Add(-40 * time.Minute), Success: true, StatusCode: 200},
		{CheckedAt: t0.Add(-30 * time.Minute), Success: false, Error: "timeout after 10s"},
		{CheckedAt: t0.Add(-10 * time.Minute), Success: true, StatusCode: 200},
		{CheckedAt: t0.Add(-48 * time.Hour), Success: true, StatusCode: 200}, // вне периода
	}
	for _, r := range results {
		r.MonitorID = ms[0].ID
		r.LatencyMS = 10
		if err := repo.SaveResult(ctx, r); err != nil {
			t.Fatalf("SaveResult: %v", err)
		}
	}

	st, err := repo.Uptime(ctx, ms[0].ID, t0.Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("Uptime: %v", err)
	}
	if st != (monitor.UptimeStats{Checks: 4, Successful: 3}) {
		t.Errorf("Uptime = %+v, want {Checks:4 Successful:3}", st)
	}

	st, err = repo.Uptime(ctx, ms[1].ID, t0.Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("Uptime for monitor without checks: %v", err)
	}
	if st != (monitor.UptimeStats{}) {
		t.Errorf("Uptime = %+v, want zeros", st)
	}
}
