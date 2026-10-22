package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"
)

const (
	envHTTPAddr        = "UPWATCH_HTTP_ADDR"
	envDatabaseURL     = "UPWATCH_DATABASE_URL"
	envLogLevel        = "UPWATCH_LOG_LEVEL"
	envShutdownTimeout = "UPWATCH_SHUTDOWN_TIMEOUT"
)

type Config struct {
	HTTPAddr        string
	DatabaseURL     string
	LogLevel        slog.Level
	ShutdownTimeout time.Duration
}

func Load() (Config, error) {
	var cfg Config
	cfg.HTTPAddr = getEnv(envHTTPAddr, ":8080")

	cfg.DatabaseURL = os.Getenv(envDatabaseURL)
	if cfg.DatabaseURL == "" {
		return Config{}, errors.New(envDatabaseURL + ": not set")
	}

	if err := cfg.LogLevel.UnmarshalText([]byte(getEnv(envLogLevel, "info"))); err != nil {
		return Config{}, fmt.Errorf("%s: %w", envLogLevel, err)
	}

	timeout, err := time.ParseDuration(getEnv(envShutdownTimeout, "10s"))
	if err != nil {
		return Config{}, fmt.Errorf("%s: %w", envShutdownTimeout, err)
	}
	if timeout <= 0 {
		return Config{}, fmt.Errorf("%s: must be positive, got %s", envShutdownTimeout, timeout)
	}
	cfg.ShutdownTimeout = timeout

	return cfg, nil
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
