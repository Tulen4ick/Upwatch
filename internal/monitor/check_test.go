package monitor

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestCheckResultJSON_Success(t *testing.T) {
	r := CheckResult{
		ID:         1,
		MonitorID:  7,
		CheckedAt:  time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC),
		Success:    true,
		StatusCode: 200,
		LatencyMS:  42,
	}

	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}

	want := `{"id":1,"monitor_id":7,"checked_at":"2026-10-26T12:00:00Z","success":true,"status_code":200,"latency_ms":42}`
	if string(b) != want {
		t.Errorf("got  %s\nwant %s", b, want)
	}
}

func TestCheckResultJSON_FailureWithoutResponse(t *testing.T) {
	r := CheckResult{MonitorID: 7, Success: false, LatencyMS: 1000, Error: "timeout after 1s"}

	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)

	if strings.Contains(s, "status_code") {
		t.Errorf("status_code must be omitted when there was no response: %s", s)
	}
	if !strings.Contains(s, `"error":"timeout after 1s"`) {
		t.Errorf("error is missing: %s", s)
	}
	if !strings.Contains(s, `"success":false`) {
		t.Errorf(`"success":false must be present even though it is a zero value: %s`, s)
	}
}

func TestUptimeStatsJSON(t *testing.T) {
	b, err := json.Marshal(UptimeStats{})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"checks":0,"successful":0}`; string(b) != want {
		t.Errorf("got %s, want %s", b, want)
	}
}
