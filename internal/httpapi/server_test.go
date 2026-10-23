package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Tulen4ick/Upwatch/internal/monitor"
)

// ---------- fakes ----------

type fakeStore struct {
	monitors    map[int64]monitor.Monitor
	nextID      int64
	err         error
	panicOnList bool
	createCalls int
}

func newFakeStore() *fakeStore {
	return &fakeStore{monitors: map[int64]monitor.Monitor{}, nextID: 1}
}

func (f *fakeStore) fromInput(id int64, in monitor.Input) monitor.Monitor {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	return monitor.Monitor{
		ID:              id,
		Name:            in.Name,
		URL:             in.URL,
		IntervalSeconds: in.IntervalSeconds,
		TimeoutSeconds:  in.TimeoutSeconds,
		Enabled:         in.Enabled != nil && *in.Enabled,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
}

func (f *fakeStore) Create(_ context.Context, in monitor.Input) (monitor.Monitor, error) {
	f.createCalls++
	if f.err != nil {
		return monitor.Monitor{}, f.err
	}
	m := f.fromInput(f.nextID, in)
	f.monitors[m.ID] = m
	f.nextID++
	return m, nil
}

func (f *fakeStore) Get(_ context.Context, id int64) (monitor.Monitor, error) {
	if f.err != nil {
		return monitor.Monitor{}, f.err
	}
	m, ok := f.monitors[id]
	if !ok {
		return monitor.Monitor{}, monitor.ErrNotFound
	}
	return m, nil
}

func (f *fakeStore) List(_ context.Context) ([]monitor.Monitor, error) {
	if f.panicOnList {
		panic("boom")
	}
	if f.err != nil {
		return nil, f.err
	}
	var out []monitor.Monitor // специально nil, если мониторов нет
	for _, m := range f.monitors {
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (f *fakeStore) Update(_ context.Context, id int64, in monitor.Input) (monitor.Monitor, error) {
	if f.err != nil {
		return monitor.Monitor{}, f.err
	}
	if _, ok := f.monitors[id]; !ok {
		return monitor.Monitor{}, monitor.ErrNotFound
	}
	m := f.fromInput(id, in)
	f.monitors[id] = m
	return m, nil
}

func (f *fakeStore) Delete(_ context.Context, id int64) error {
	if f.err != nil {
		return f.err
	}
	if _, ok := f.monitors[id]; !ok {
		return monitor.ErrNotFound
	}
	delete(f.monitors, id)
	return nil
}

type fakePinger struct{ err error }

func (p fakePinger) Ping(context.Context) error { return p.err }

// ---------- helpers ----------

func newTestServer(t *testing.T) (*fakeStore, http.Handler) {
	t.Helper()
	store := newFakeStore()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return store, NewServer(store, fakePinger{}, log)
}

func do(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, r)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func assertStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, want, rec.Body.String())
	}
}

func assertJSON(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("response is not valid JSON for %T: %v; body: %s", v, err, rec.Body.String())
	}
	return v
}

func assertErrorBody(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	assertJSON(t, rec)
	body := decode[map[string]any](t, rec)
	if msg, _ := body["error"].(string); msg == "" {
		t.Errorf(`body has no non-empty "error" field: %s`, rec.Body.String())
	}
}

const validBody = `{"name":"Example","url":"https://example.com","interval_seconds":60}`

// ---------- health ----------

func TestHealth(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Run("ok", func(t *testing.T) {
		h := NewServer(newFakeStore(), fakePinger{}, log)
		rec := do(t, h, http.MethodGet, "/healthz", "")
		assertStatus(t, rec, http.StatusOK)
		assertJSON(t, rec)
		if got := decode[map[string]string](t, rec)["status"]; got != "ok" {
			t.Errorf(`status field = %q, want "ok"`, got)
		}
	})

	t.Run("db is down", func(t *testing.T) {
		h := NewServer(newFakeStore(), fakePinger{err: errors.New("conn refused")}, log)
		rec := do(t, h, http.MethodGet, "/healthz", "")
		assertStatus(t, rec, http.StatusServiceUnavailable)
		if got := decode[map[string]string](t, rec)["status"]; got != "unavailable" {
			t.Errorf(`status field = %q, want "unavailable"`, got)
		}
	})
}

// ---------- create ----------

func TestCreateMonitor(t *testing.T) {
	store, h := newTestServer(t)

	rec := do(t, h, http.MethodPost, "/api/v1/monitors", validBody)

	assertStatus(t, rec, http.StatusCreated)
	assertJSON(t, rec)
	m := decode[monitor.Monitor](t, rec)
	if m.ID != 1 || m.Name != "Example" || m.URL != "https://example.com" || m.IntervalSeconds != 60 {
		t.Errorf("unexpected monitor: %+v", m)
	}
	if m.TimeoutSeconds != monitor.DefaultTimeoutSeconds {
		t.Errorf("TimeoutSeconds = %d, want default %d", m.TimeoutSeconds, monitor.DefaultTimeoutSeconds)
	}
	if !m.Enabled {
		t.Error("Enabled = false, want true by default")
	}
	if len(store.monitors) != 1 {
		t.Errorf("store has %d monitors, want 1", len(store.monitors))
	}
}

func TestCreateMonitor_BadRequest(t *testing.T) {
	tests := map[string]string{
		"broken json":        `{"name":`,
		"empty body":         ``,
		"unknown field":      `{"name":"x","url":"https://example.com","interval_seconds":60,"color":"red"}`,
		"wrong type":         `{"name":"x","url":"https://example.com","interval_seconds":"60"}`,
		"two json documents": validBody + validBody,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			store, h := newTestServer(t)
			rec := do(t, h, http.MethodPost, "/api/v1/monitors", body)
			assertStatus(t, rec, http.StatusBadRequest)
			assertErrorBody(t, rec)
			if store.createCalls != 0 {
				t.Error("store.Create must not be called for a bad request")
			}
		})
	}
}

func TestCreateMonitor_Validation(t *testing.T) {
	store, h := newTestServer(t)

	rec := do(t, h, http.MethodPost, "/api/v1/monitors", `{"name":"  ","url":"example.com","interval_seconds":60}`)

	assertStatus(t, rec, http.StatusUnprocessableEntity)
	assertErrorBody(t, rec)
	body := decode[struct {
		Fields map[string]string `json:"fields"`
	}](t, rec)
	if body.Fields["name"] == "" || body.Fields["url"] == "" {
		t.Errorf("fields = %v, want errors for name and url", body.Fields)
	}
	if store.createCalls != 0 {
		t.Error("store.Create must not be called when validation fails")
	}
}

// ---------- list ----------

func TestListMonitors_EmptyIsArray(t *testing.T) {
	_, h := newTestServer(t)

	rec := do(t, h, http.MethodGet, "/api/v1/monitors", "")

	assertStatus(t, rec, http.StatusOK)
	assertJSON(t, rec)
	if got := strings.TrimSpace(rec.Body.String()); got != "[]" {
		t.Errorf("body = %s, want [] (not null)", got)
	}
}

func TestListMonitors(t *testing.T) {
	_, h := newTestServer(t)
	do(t, h, http.MethodPost, "/api/v1/monitors", validBody)
	do(t, h, http.MethodPost, "/api/v1/monitors", `{"name":"Second","url":"http://localhost:3000","interval_seconds":30,"timeout_seconds":5}`)

	rec := do(t, h, http.MethodGet, "/api/v1/monitors", "")

	assertStatus(t, rec, http.StatusOK)
	list := decode[[]monitor.Monitor](t, rec)
	if len(list) != 2 || list[0].Name != "Example" || list[1].Name != "Second" {
		t.Errorf("unexpected list: %+v", list)
	}
}

// ---------- get ----------

func TestGetMonitor(t *testing.T) {
	_, h := newTestServer(t)
	do(t, h, http.MethodPost, "/api/v1/monitors", validBody)

	t.Run("found", func(t *testing.T) {
		rec := do(t, h, http.MethodGet, "/api/v1/monitors/1", "")
		assertStatus(t, rec, http.StatusOK)
		if m := decode[monitor.Monitor](t, rec); m.ID != 1 {
			t.Errorf("ID = %d, want 1", m.ID)
		}
	})

	t.Run("not found", func(t *testing.T) {
		rec := do(t, h, http.MethodGet, "/api/v1/monitors/42", "")
		assertStatus(t, rec, http.StatusNotFound)
		assertErrorBody(t, rec)
	})

	for _, id := range []string{"abc", "0", "-5", "1.5"} {
		t.Run("bad id "+id, func(t *testing.T) {
			rec := do(t, h, http.MethodGet, "/api/v1/monitors/"+id, "")
			assertStatus(t, rec, http.StatusBadRequest)
			assertErrorBody(t, rec)
		})
	}
}

// ---------- update ----------

func TestUpdateMonitor(t *testing.T) {
	_, h := newTestServer(t)
	do(t, h, http.MethodPost, "/api/v1/monitors", validBody)

	t.Run("ok", func(t *testing.T) {
		rec := do(t, h, http.MethodPut, "/api/v1/monitors/1",
			`{"name":"Renamed","url":"https://example.org","interval_seconds":120,"timeout_seconds":20,"enabled":false}`)
		assertStatus(t, rec, http.StatusOK)
		m := decode[monitor.Monitor](t, rec)
		if m.Name != "Renamed" || m.IntervalSeconds != 120 || m.TimeoutSeconds != 20 || m.Enabled {
			t.Errorf("unexpected monitor after update: %+v", m)
		}
	})

	t.Run("not found", func(t *testing.T) {
		rec := do(t, h, http.MethodPut, "/api/v1/monitors/42", validBody)
		assertStatus(t, rec, http.StatusNotFound)
	})

	t.Run("validation", func(t *testing.T) {
		rec := do(t, h, http.MethodPut, "/api/v1/monitors/1", `{"name":"x","url":"https://example.com","interval_seconds":5}`)
		assertStatus(t, rec, http.StatusUnprocessableEntity)
	})

	t.Run("bad id", func(t *testing.T) {
		rec := do(t, h, http.MethodPut, "/api/v1/monitors/abc", validBody)
		assertStatus(t, rec, http.StatusBadRequest)
	})
}

// ---------- delete ----------

func TestDeleteMonitor(t *testing.T) {
	_, h := newTestServer(t)
	do(t, h, http.MethodPost, "/api/v1/monitors", validBody)

	rec := do(t, h, http.MethodDelete, "/api/v1/monitors/1", "")
	assertStatus(t, rec, http.StatusNoContent)
	if rec.Body.Len() != 0 {
		t.Errorf("204 response must have empty body, got %q", rec.Body.String())
	}

	rec = do(t, h, http.MethodDelete, "/api/v1/monitors/1", "")
	assertStatus(t, rec, http.StatusNotFound)
}

// ---------- errors & middleware ----------

func TestStoreError_IsHidden(t *testing.T) {
	store, h := newTestServer(t)
	store.err = errors.New("pq: password authentication failed for user secret_user")

	rec := do(t, h, http.MethodGet, "/api/v1/monitors", "")

	assertStatus(t, rec, http.StatusInternalServerError)
	assertErrorBody(t, rec)
	if strings.Contains(rec.Body.String(), "secret_user") {
		t.Error("internal error details must not leak to the client")
	}
}

func TestMethodNotAllowed(t *testing.T) {
	_, h := newTestServer(t)
	rec := do(t, h, http.MethodPatch, "/api/v1/monitors/1", validBody)
	assertStatus(t, rec, http.StatusMethodNotAllowed)
}

func TestRecoverPanic(t *testing.T) {
	store, h := newTestServer(t)
	store.panicOnList = true

	var rec *httptest.ResponseRecorder
	func() {
		defer func() {
			if v := recover(); v != nil {
				t.Fatalf("panic escaped the handler: %v (нужен middleware recoverPanic)", v)
			}
		}()
		rec = do(t, h, http.MethodGet, "/api/v1/monitors", "")
	}()

	assertStatus(t, rec, http.StatusInternalServerError)
	assertErrorBody(t, rec)
}
