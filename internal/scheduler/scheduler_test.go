package scheduler

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tulen4ick/Upwatch/internal/monitor"
)

// ---------- fakes ----------

type fakeStore struct {
	mu        sync.Mutex
	batches   [][]monitor.Monitor
	errs      []error
	saved     []monitor.CheckResult
	saveErr   error
	claims    int
	lastLimit int
}

func (f *fakeStore) ClaimDue(_ context.Context, limit int) ([]monitor.Monitor, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.claims++
	f.lastLimit = limit
	if len(f.errs) > 0 {
		err := f.errs[0]
		f.errs = f.errs[1:]
		if err != nil {
			return nil, err
		}
	}
	if len(f.batches) == 0 {
		return nil, nil
	}
	b := f.batches[0]
	f.batches = f.batches[1:]
	return b, nil
}

func (f *fakeStore) SaveResult(_ context.Context, r monitor.CheckResult) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.saveErr != nil {
		return f.saveErr
	}
	f.saved = append(f.saved, r)
	return nil
}

func (f *fakeStore) savedCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.saved)
}

type fakeChecker struct {
	delay    time.Duration
	block    bool
	inFlight atomic.Int32
	maxSeen  atomic.Int32
	calls    atomic.Int32
}

func (c *fakeChecker) Check(ctx context.Context, m monitor.Monitor) monitor.CheckResult {
	c.calls.Add(1)
	n := c.inFlight.Add(1)
	defer c.inFlight.Add(-1)

	for {
		old := c.maxSeen.Load()
		if n <= old || c.maxSeen.CompareAndSwap(old, n) {
			break
		}
	}

	if c.block {
		<-ctx.Done()
	} else if c.delay > 0 {
		select {
		case <-time.After(c.delay):
		case <-ctx.Done():
		}
	}

	return monitor.CheckResult{MonitorID: m.ID, CheckedAt: time.Now(), Success: true, StatusCode: 200}
}

// ---------- helpers ----------

func monitors(n int) []monitor.Monitor {
	out := make([]monitor.Monitor, n)
	for i := range out {
		out[i] = monitor.Monitor{ID: int64(i + 1), TimeoutSeconds: 1}
	}
	return out
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met within 2s")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func start(t *testing.T, cfg Config, c Checker, s Store) (cancel func(), done <-chan struct{}) {
	t.Helper()
	ctx, cancelFn := context.WithCancel(context.Background())
	ch := make(chan struct{})
	sched := New(cfg, c, s, slog.New(slog.NewTextHandler(io.Discard, nil)))
	go func() {
		sched.Run(ctx)
		close(ch)
	}()
	t.Cleanup(func() {
		cancelFn()
		<-ch
	})
	return cancelFn, ch
}

func stopped(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not return within 1s after cancel")
	}
}

var testCfg = Config{Workers: 3, TickInterval: 10 * time.Millisecond, BatchSize: 50}

// ---------- tests ----------

func TestScheduler_ChecksAndSavesAllDue(t *testing.T) {
	store := &fakeStore{batches: [][]monitor.Monitor{monitors(10)}}
	checker := &fakeChecker{}

	cancel, done := start(t, testCfg, checker, store)
	waitFor(t, func() bool { return store.savedCount() == 10 })
	cancel()
	stopped(t, done)

	if store.lastLimit != testCfg.BatchSize {
		t.Errorf("ClaimDue limit = %d, want %d", store.lastLimit, testCfg.BatchSize)
	}
	seen := map[int64]bool{}
	for _, r := range store.saved {
		seen[r.MonitorID] = true
	}
	if len(seen) != 10 {
		t.Errorf("saved results for %d distinct monitors, want 10", len(seen))
	}
}

func TestScheduler_PollsOnEveryTick(t *testing.T) {
	store := &fakeStore{batches: [][]monitor.Monitor{monitors(1), nil, monitors(2)}}
	_, _ = start(t, testCfg, &fakeChecker{}, store)
	waitFor(t, func() bool { return store.savedCount() == 3 })
}

func TestScheduler_RespectsWorkerLimit(t *testing.T) {
	store := &fakeStore{batches: [][]monitor.Monitor{monitors(12)}}
	checker := &fakeChecker{delay: 30 * time.Millisecond}

	_, _ = start(t, testCfg, checker, store)
	waitFor(t, func() bool { return store.savedCount() == 12 })

	if got := checker.maxSeen.Load(); got > int32(testCfg.Workers) {
		t.Errorf("max concurrent checks = %d, want <= %d", got, testCfg.Workers)
	}
	if got := checker.maxSeen.Load(); got < 2 {
		t.Errorf("max concurrent checks = %d, checks do not run in parallel", got)
	}
}

func TestScheduler_StopsPromptlyAndSkipsCanceledResults(t *testing.T) {
	store := &fakeStore{batches: [][]monitor.Monitor{monitors(3)}}
	checker := &fakeChecker{block: true}

	cancel, done := start(t, testCfg, checker, store)
	waitFor(t, func() bool { return checker.inFlight.Load() == 3 })
	cancel()
	stopped(t, done)

	if n := store.savedCount(); n != 0 {
		t.Errorf("saved %d results of canceled checks, want 0", n)
	}
}

func TestScheduler_SurvivesClaimError(t *testing.T) {
	store := &fakeStore{
		errs:    []error{errors.New("db is down")},
		batches: [][]monitor.Monitor{monitors(2)},
	}
	_, _ = start(t, testCfg, &fakeChecker{}, store)
	waitFor(t, func() bool { return store.savedCount() == 2 })
}

func TestScheduler_SurvivesSaveError(t *testing.T) {
	store := &fakeStore{
		batches: [][]monitor.Monitor{monitors(2), monitors(2)},
		saveErr: monitor.ErrNotFound,
	}
	checker := &fakeChecker{}
	_, _ = start(t, testCfg, checker, store)
	waitFor(t, func() bool { return checker.calls.Load() == 4 })
}
