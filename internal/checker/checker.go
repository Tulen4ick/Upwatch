package checker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/Tulen4ick/Upwatch/internal/monitor"
)

const (
	maxBodyDrain   = 64 << 10
	maxErrorLength = 500
	userAgent      = "upwatch/0.2 (+https://github.com/Tulen4ick/Upwatch)"
)

type Checker struct {
	client *http.Client
}

func New(client *http.Client) *Checker {
	if client == nil {
		client = http.DefaultClient
	}
	return &Checker{client: client}
}

func (c *Checker) Check(ctx context.Context, m monitor.Monitor) monitor.CheckResult {
	res := monitor.CheckResult{MonitorID: m.ID, CheckedAt: time.Now()}

	timeout := time.Duration(m.TimeoutSeconds) * time.Second
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.URL, nil)
	if err != nil {
		res.Error = truncate(fmt.Sprintf("build request: %v", err))
	}
	req.Header.Set("User-Agent", userAgent)

	start := time.Now()
	resp, err := c.client.Do(req)
	if err != nil {
		res.LatencyMS = time.Since(start).Microseconds()
		if errors.Is(err, context.DeadlineExceeded) {
			res.Error = fmt.Sprintf("timeout after %s", timeout)
		} else {
			res.Error = truncate(err.Error())
		}
		return res
	}

	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxBodyDrain))
	resp.Body.Close()
	res.LatencyMS = time.Since(start).Microseconds()

	res.StatusCode = resp.StatusCode
	res.Success = resp.StatusCode >= 200 && resp.StatusCode < 400
	if !res.Success {
		res.Error = fmt.Sprintf("unexpected status %d", resp.StatusCode)
	}
	return res
}

func truncate(s string) string {
	if len(s) <= maxErrorLength {
		return s
	}
	return s[:maxErrorLength]
}
