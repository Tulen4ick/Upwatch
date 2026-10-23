package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/Tulen4ick/Upwatch/internal/monitor"
)

// GET /healthz
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	if err := s.db.Ping(ctx); err != nil {
		s.log.Warn("health check failed", "err", err)
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// POST /api/v1/monitors
func (s *Server) handleCreateMonitor(w http.ResponseWriter, r *http.Request) {
	in, ok := s.readInput(w, r)
	if !ok {
		return
	}
	m, err := s.store.Create(r.Context(), in)
	if err != nil {
		s.storeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, m)
}

// GET /api/v1/monitors
func (s *Server) handleListMonitors(w http.ResponseWriter, r *http.Request) {
	ms, err := s.store.List(r.Context())
	if err != nil {
		s.storeError(w, err)
		return
	}
	if ms == nil {
		ms = []monitor.Monitor{}
	}
	writeJSON(w, http.StatusOK, ms)
}

// GET /api/v1/monitors/{id}
func (s *Server) handleGetMonitor(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	m, err := s.store.Get(r.Context(), id)
	if err != nil {
		s.storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, m)
}

// PUT /api/v1/monitors/{id}
func (s *Server) handleUpdateMonitor(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	in, ok := s.readInput(w, r)
	if !ok {
		return
	}
	m, err := s.store.Update(r.Context(), id, in)
	if err != nil {
		s.storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, m)
}

// DELETE /api/v1/monitors/{id}
func (s *Server) handleDeleteMonitor(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	if err := s.store.Delete(r.Context(), id); err != nil {
		s.storeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) readInput(w http.ResponseWriter, r *http.Request) (monitor.Input, bool) {
	var in monitor.Input
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return in, false
	}

	in.Normalize()

	if err := in.Validate(); err != nil {
		var ve *monitor.ValidationError
		if errors.As(err, &ve) {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
				"error":  "validation failed",
				"fields": ve.Fields,
			})
			return in, false
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return in, false
	}
	return in, true
}

func parseID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "id must be a positive integer")
		return 0, false
	}
	return id, true
}

func (s *Server) storeError(w http.ResponseWriter, err error) {
	if errors.Is(err, monitor.ErrNotFound) {
		writeError(w, http.StatusNotFound, "monitor not found")
		return
	}
	s.log.Error("store error", "err", err)
	writeError(w, http.StatusInternalServerError, "internal error")
}
