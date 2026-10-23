package httpapi

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/Tulen4ick/Upwatch/internal/monitor"
)

type MonitorStore interface {
	Create(ctx context.Context, in monitor.Input) (monitor.Monitor, error)
	Get(ctx context.Context, id int64) (monitor.Monitor, error)
	List(ctx context.Context) ([]monitor.Monitor, error)
	Update(ctx context.Context, id int64, in monitor.Input) (monitor.Monitor, error)
	Delete(ctx context.Context, id int64) error
}

type Pinger interface {
	Ping(ctx context.Context) error
}

type Server struct {
	store MonitorStore
	db    Pinger
	log   *slog.Logger
}

func NewServer(store MonitorStore, db Pinger, log *slog.Logger) http.Handler {
	s := &Server{store: store, db: db, log: log}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("POST /api/v1/monitors", s.handleCreateMonitor)
	mux.HandleFunc("GET /api/v1/monitors", s.handleListMonitors)
	mux.HandleFunc("GET /api/v1/monitors/{id}", s.handleGetMonitor)
	mux.HandleFunc("PUT /api/v1/monitors/{id}", s.handleUpdateMonitor)
	mux.HandleFunc("DELETE /api/v1/monitors/{id}", s.handleDeleteMonitor)

	return s.logRequests(s.recoverPanic(mux))
}
