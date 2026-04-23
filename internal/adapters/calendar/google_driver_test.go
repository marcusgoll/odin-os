package calendar

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDriverInvokesConfiguredCommandAndDecodesStructuredJSON(t *testing.T) {
	requestPath := filepath.Join(t.TempDir(), "request.json")
	script := writeFixtureDriver(t, `#!/usr/bin/env bash
cat >"$ODIN_DRIVER_REQUEST_PATH"
printf '{"status":"completed","tool_key":"google_calendar_off_dates","summary":"Found 2 off-dates for 2026-05.","artifacts":{"bid_period":"2026-05","calendar_id":"primary","timezone":"America/Chicago","off_dates":["2026-05-03","2026-05-04"]}}'
`)
	t.Setenv("ODIN_GOOGLE_CALENDAR_DRIVER", script)
	t.Setenv("ODIN_DRIVER_REQUEST_PATH", requestPath)

	driver := NewDriver()
	response, err := driver.Invoke(context.Background(), Request{
		ToolKey: "google_calendar_off_dates",
		Input: Input{
			BidPeriod:  "2026-05",
			CalendarID: "primary",
			Timezone:   "America/Chicago",
		},
	})
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if response.ToolKey != "google_calendar_off_dates" {
		t.Fatalf("ToolKey = %q, want google_calendar_off_dates", response.ToolKey)
	}
	if response.Summary != "Found 2 off-dates for 2026-05." {
		t.Fatalf("Summary = %q, want fixture summary", response.Summary)
	}

	offDates, ok := response.Artifacts["off_dates"].([]any)
	if !ok || len(offDates) != 2 {
		t.Fatalf("Artifacts.off_dates = %#v, want 2 dates", response.Artifacts["off_dates"])
	}

	requestBytes, err := os.ReadFile(requestPath)
	if err != nil {
		t.Fatalf("ReadFile(request) error = %v", err)
	}
	var request Request
	if err := json.Unmarshal(requestBytes, &request); err != nil {
		t.Fatalf("request json = %v", err)
	}
	if request.ToolKey != "google_calendar_off_dates" {
		t.Fatalf("Request.ToolKey = %q, want google_calendar_off_dates", request.ToolKey)
	}
	if request.Input.BidPeriod != "2026-05" {
		t.Fatalf("Request.Input.BidPeriod = %q, want 2026-05", request.Input.BidPeriod)
	}
}

func TestDriverFailsClosedWithoutCommand(t *testing.T) {
	t.Setenv("ODIN_GOOGLE_CALENDAR_DRIVER", "")

	driver := NewDriver()
	if _, err := driver.Invoke(context.Background(), Request{ToolKey: "google_calendar_off_dates"}); err == nil {
		t.Fatal("Invoke() error = nil, want missing driver config failure")
	}
}

func TestDriverFailsClosedOnNonCompletedStatus(t *testing.T) {
	script := writeFixtureDriver(t, `#!/usr/bin/env bash
printf '{"status":"failed","tool_key":"google_calendar_off_dates","summary":"driver failed","artifacts":{"reason":"calendar unavailable"}}'
`)
	t.Setenv("ODIN_GOOGLE_CALENDAR_DRIVER", script)

	driver := NewDriver()
	if _, err := driver.Invoke(context.Background(), Request{
		ToolKey: "google_calendar_off_dates",
		Input: Input{
			BidPeriod:  "2026-05",
			CalendarID: "primary",
			Timezone:   "America/Chicago",
		},
	}); err == nil {
		t.Fatal("Invoke() error = nil, want non-completed status failure")
	}
}

func writeFixtureDriver(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "driver.sh")
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("WriteFile(driver) error = %v", err)
	}
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatalf("Chmod(driver) error = %v", err)
	}
	return path
}
