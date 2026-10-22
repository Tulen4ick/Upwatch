package monitor

import (
	"errors"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	MinIntervalSeconds    = 30
	MaxIntervalSeconds    = 86400
	DefaultTimeoutSeconds = 10
	MaxTimeoutSeconds     = 60
	MaxNameLength         = 100
)

var ErrNotFound = errors.New("monitor not found")

type Monitor struct {
	ID              int64     `json:"id"`
	Name            string    `json:"name"`
	URL             string    `json:"url"`
	IntervalSeconds int       `json:"interval_seconds"`
	TimeoutSeconds  int       `json:"timeout_seconds"`
	Enabled         bool      `json:"enabled"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type Input struct {
	Name            string `json:"name"`
	URL             string `json:"url"`
	IntervalSeconds int    `json:"interval_seconds"`
	TimeoutSeconds  int    `json:"timeout_seconds"`
	Enabled         *bool  `json:"enabled"`
}

type ValidationError struct {
	Fields map[string]string
}

func (e *ValidationError) Error() string {
	if e == nil || len(e.Fields) == 0 {
		return "validation failed"
	}

	keys := slices.Sorted(maps.Keys(e.Fields))

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+": "+e.Fields[k])
	}

	return "validation failed " + strings.Join(parts, "; ")
}

func (in *Input) Normalize() {
	in.Name = strings.TrimSpace(in.Name)
	in.URL = strings.TrimSpace(in.URL)

	if in.TimeoutSeconds == 0 {
		in.TimeoutSeconds = DefaultTimeoutSeconds
	}

	if in.Enabled == nil {
		enabled := true
		in.Enabled = &enabled
	}
}

func (in Input) Validate() error {
	ve := &ValidationError{Fields: make(map[string]string)}

	switch n := utf8.RuneCountInString(in.Name); {
	case n == 0:
		ve.Fields["name"] = "must not be empty"
	case n > 100:
		ve.Fields["name"] = "must be at most 100 characters"
	}

	if in.URL == "" {
		ve.Fields["url"] = "must not be empty"
	} else if u, err := url.Parse(in.URL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		ve.Fields["url"] = "must be an absolute http(s) URL"
	}

	if in.IntervalSeconds < MinIntervalSeconds || in.IntervalSeconds > MaxIntervalSeconds {
		ve.Fields["interval_seconds"] = fmt.Sprintf("must be between %d and %d", MinIntervalSeconds, MaxIntervalSeconds)
	}

	switch {
	case in.TimeoutSeconds < 1 || in.TimeoutSeconds > MaxTimeoutSeconds:
		ve.Fields["timeout_seconds"] = fmt.Sprintf("must be between 1 and %d", MaxTimeoutSeconds)
	case in.TimeoutSeconds >= in.IntervalSeconds:
		ve.Fields["timeout_seconds"] = "must be less than interval_seconds"
	}

	if len(ve.Fields) > 0 {
		return ve
	}
	return nil
}
