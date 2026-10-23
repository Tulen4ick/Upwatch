package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Tulen4ick/Upwatch/internal/config"
	"github.com/Tulen4ick/Upwatch/internal/httpapi"
	"github.com/Tulen4ick/Upwatch/internal/storage/postgres"
)

var _ httpapi.MonitorStore = (*postgres.MonitorRepo)(nil)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "upwatch:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := postgres.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer pool.Close()
	logger.Info("connected to database")

	repo := postgres.NewMonitorRepo(pool)
	handler := httpapi.NewServer(repo, pool, logger)

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("starting", "addr", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case err := <-errCh:
		return fmt.Errorf("http server: %w", err)
	case <-ctx.Done():
		logger.Info("shutting down", "timeout", cfg.ShutdownTimeout)
	}

	stop()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	if err := <-errCh; err != nil {
		return fmt.Errorf("http server: %w", err)
	}

	logger.Info("stopped")
	return nil
}
