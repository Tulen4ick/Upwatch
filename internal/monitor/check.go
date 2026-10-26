package monitor

import "time"

type CheckResult struct {
	ID         int64     `json:"id"`
	MonitorID  int64     `json:"monitor_id"`
	CheckedAt  time.Time `json:"checked_at"`
	Success    bool      `json:"success"`
	StatusCode int       `json:"status_code,omitempty"`
	LatencyMS  int64     `json:"latency_ms"`
	Error      string    `json:"error,omitempty"`
}

type UptimeStats struct {
	Checks     int64 `json:"checks"`
	Successful int64 `json:"successful"`
}
