package scheduler

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/Tulen4ick/Upwatch/internal/monitor"
)

type Checker interface {
	Check(ctx context.Context, m monitor.Monitor) monitor.CheckResult
}

type Store interface {
	ClaimDue(ctx context.Context, limit int) ([]monitor.Monitor, error)
	SaveResult(ctx context.Context, r monitor.CheckResult) error
}

type Config struct {
	Workers      int
	TickInterval time.Duration
	BatchSize    int
}

type Scheduler struct {
	cfg     Config
	checker Checker
	store   Store
	log     *slog.Logger
}

func New(cfg Config, checker Checker, store Store, log *slog.Logger) *Scheduler {
	return &Scheduler{cfg: cfg, checker: checker, store: store, log: log}
}

func (s *Scheduler) Run(ctx context.Context) {
	jobs := make(chan monitor.Monitor, s.cfg.Workers)

	var wg sync.WaitGroup
	for range s.cfg.Workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.worker(ctx, jobs)
		}()
	}

	s.log.Info("scheduler started", "workers", s.cfg.Workers, "tick", s.cfg.TickInterval)

	s.dispatch(ctx, jobs)

	close(jobs)
	wg.Wait()

	s.log.Info("scheduler stopped")
}

func (s *Scheduler) dispatch(ctx context.Context, jobs chan<- monitor.Monitor) {
	ticker := time.NewTicker(s.cfg.TickInterval)
	defer ticker.Stop()

	for {
		s.enqueueDue(ctx, jobs)

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Scheduler) enqueueDue(ctx context.Context, jobs chan<- monitor.Monitor) {
	due, err := s.store.ClaimDue(ctx, s.cfg.BatchSize)
	if err != nil {
		if ctx.Err() == nil {
			s.log.Error("claim due monitors", "err", err)
		}
		return
	}

	for _, m := range due {
		select {
		case jobs <- m:
		case <-ctx.Done():
			return
		}
	}
}

func (s *Scheduler) worker(ctx context.Context, jobs <-chan monitor.Monitor) {
	for m := range jobs {
		res := s.checker.Check(ctx, m)

		if ctx.Err() != nil {
			continue
		}

		if err := s.store.SaveResult(ctx, res); err != nil {
			if errors.Is(err, monitor.ErrNotFound) {
				s.log.Debug("monitor deleted during check", "monitor_id", m.ID)
				continue
			}
			s.log.Error("save check result", "monitor_id", m.ID, "err", err)
			continue
		}

		s.log.Debug("check done",
			"monitor_id", m.ID,
			"success", res.Success,
			"status", res.StatusCode,
			"latency_ms", res.LatencyMS,
		)
	}
}
