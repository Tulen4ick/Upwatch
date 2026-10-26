package checker

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Tulen4ick/Upwatch/internal/monitor"
)

func mon(url string, timeout int) monitor.Monitor {
	return monitor.Monitor{ID: 7, URL: url, IntervalSeconds: 60, TimeoutSeconds: timeout, Enabled: true}
}

func TestCheck_Statuses(t *testing.T) {
	tests := []struct {
		name        string
		status      int
		wantSuccess bool
	}{
		{"200", http.StatusOK, true},
		{"204", http.StatusNoContent, true},
		{"404", http.StatusNotFound, false},
		{"500", http.StatusInternalServerError, false},
		{"503", http.StatusServiceUnavailable, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if ua := r.Header.Get("User-Agent"); !strings.HasPrefix(ua, "upwatch/") {
					t.Errorf("User-Agent = %q", ua)
				}
				w.WriteHeader(tt.status)
			}))
			defer srv.Close()

			before := time.Now()
			res := New(srv.Client()).Check(context.Background(), mon(srv.URL, 5))

			if res.Success != tt.wantSuccess || res.StatusCode != tt.status {
				t.Errorf("got success=%v status=%d, want %v %d", res.Success, res.StatusCode, tt.wantSuccess, tt.status)
			}
			if res.MonitorID != 7 {
				t.Errorf("MonitorID = %d", res.MonitorID)
			}
			if res.CheckedAt.Before(before) {
				t.Error("CheckedAt not set")
			}
			if tt.wantSuccess && res.Error != "" || !tt.wantSuccess && res.Error == "" {
				t.Errorf("Error = %q", res.Error)
			}
		})
	}
}

func TestCheck_FollowsRedirect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/old" {
			http.Redirect(w, r, "/new", http.StatusMovedPermanently)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	res := New(srv.Client()).Check(context.Background(), mon(srv.URL+"/old", 5))
	if !res.Success || res.StatusCode != 200 {
		t.Errorf("got %+v, want success with final 200", res)
	}
}

func TestCheck_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	defer srv.Close()

	start := time.Now()
	res := New(srv.Client()).Check(context.Background(), mon(srv.URL, 1))
	elapsed := time.Since(start)

	if res.Success || res.StatusCode != 0 {
		t.Errorf("got %+v, want failure without status", res)
	}
	if !strings.Contains(res.Error, "timeout") {
		t.Errorf("Error = %q, want mention of timeout", res.Error)
	}
	if elapsed > 3*time.Second {
		t.Errorf("check took %v, timeout is 1s", elapsed)
	}
	if res.LatencyMS < 900 {
		t.Errorf("LatencyMS = %d, want about 1000", res.LatencyMS)
	}
}

func TestCheck_ConnectionRefused(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()

	res := New(nil).Check(context.Background(), mon(url, 2))
	if res.Success || res.StatusCode != 0 || res.Error == "" {
		t.Errorf("got %+v, want failure with error", res)
	}
}

func TestCheck_ParentContextCanceled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)

	start := time.Now()
	res := New(srv.Client()).Check(ctx, mon(srv.URL, 10))
	if time.Since(start) > 2*time.Second {
		t.Error("Check ignored parent context cancellation")
	}
	if res.Success {
		t.Error("canceled check must not be successful")
	}
}

func TestCheck_LongErrorTruncated(t *testing.T) {
	res := New(nil).Check(context.Background(), mon("http://"+strings.Repeat("a", 2000)+".invalid", 2))
	if res.Success || len(res.Error) > maxErrorLength {
		t.Errorf("len(Error) = %d, want <= %d", len(res.Error), maxErrorLength)
	}
}
