package config

import (
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

const testDatabaseURL = "postgres://upwatch:upwatch@localhost:5432/upwatch?sslmode=disable"

// clearEnv обнуляет все переменные конфига, чтобы тест не зависел от окружения (например, от .env через make).
func clearEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{envHTTPAddr, envDatabaseURL, envLogLevel, envShutdownTimeout} {
		t.Setenv(key, "")
	}
}

func TestLoad(t *testing.T) {
	tests := []struct {
		name        string
		env         map[string]string
		want        Config
		wantErrVar  string // пусто — ошибки быть не должно
		wantWrapped bool   // ошибка должна оборачивать исходную (%w)
	}{
		{
			name: "defaults",
			env:  map[string]string{envDatabaseURL: testDatabaseURL},
			want: Config{
				HTTPAddr:        ":8080",
				DatabaseURL:     testDatabaseURL,
				LogLevel:        slog.LevelInfo,
				ShutdownTimeout: 10 * time.Second,
			},
		},
		{
			name: "overrides",
			env: map[string]string{
				envHTTPAddr:        ":9090",
				envDatabaseURL:     testDatabaseURL,
				envLogLevel:        "warn",
				envShutdownTimeout: "30s",
			},
			want: Config{
				HTTPAddr:        ":9090",
				DatabaseURL:     testDatabaseURL,
				LogLevel:        slog.LevelWarn,
				ShutdownTimeout: 30 * time.Second,
			},
		},
		{
			name:       "missing database url",
			wantErrVar: envDatabaseURL,
		},
		{
			name: "bad log level",
			env: map[string]string{
				envDatabaseURL: testDatabaseURL,
				envLogLevel:    "loud",
			},
			wantErrVar:  envLogLevel,
			wantWrapped: true,
		},
		{
			name: "bad duration",
			env: map[string]string{
				envDatabaseURL:     testDatabaseURL,
				envShutdownTimeout: "ten",
			},
			wantErrVar:  envShutdownTimeout,
			wantWrapped: true,
		},
		{
			name: "negative duration",
			env: map[string]string{
				envDatabaseURL:     testDatabaseURL,
				envShutdownTimeout: "-1s",
			},
			wantErrVar: envShutdownTimeout,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearEnv(t)
			for k, v := range tt.env {
				t.Setenv(k, v)
			}

			got, err := Load()

			if tt.wantErrVar != "" {
				if err == nil {
					t.Fatalf("expected error, got nil (cfg: %+v)", got)
				}
				if !strings.Contains(err.Error(), tt.wantErrVar) {
					t.Errorf("error %q does not mention %s", err, tt.wantErrVar)
				}
				if tt.wantWrapped && errors.Unwrap(err) == nil {
					t.Errorf("error %q does not wrap the underlying error", err)
				}
				if got != (Config{}) {
					t.Errorf("expected zero Config on error, got %+v", got)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got  %+v\nwant %+v", got, tt.want)
			}
		})
	}
}
