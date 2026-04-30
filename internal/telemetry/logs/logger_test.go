package logs

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"
)

func TestLoggerWritesStructuredJSONWithCorrelationIdentifiers(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	logger := Logger{
		Writer: &output,
		Now: func() time.Time {
			return time.Date(2026, 4, 9, 18, 30, 0, 0, time.UTC)
		},
	}

	if err := logger.Log(Record{
		Level:         LevelInfo,
		Component:     "doctor",
		Message:       "health check completed",
		CorrelationID: "corr-123",
		Scope:         "project",
		ProjectID:     int64Ptr(7),
		TaskID:        int64Ptr(42),
		RunID:         int64Ptr(9),
		Fields: map[string]any{
			"check":  "database",
			"status": "healthy",
		},
	}); err != nil {
		t.Fatalf("Log() error = %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(output.Bytes(), &decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if !bytes.HasSuffix(output.Bytes(), []byte("\n")) {
		t.Fatalf("Log() output = %q, want newline-delimited record", output.String())
	}

	for key, want := range map[string]any{
		"level":          "info",
		"component":      "doctor",
		"message":        "health check completed",
		"correlation_id": "corr-123",
		"scope":          "project",
	} {
		if decoded[key] != want {
			t.Fatalf("%s = %v, want %v", key, decoded[key], want)
		}
	}

	fields, ok := decoded["fields"].(map[string]any)
	if !ok {
		t.Fatalf("fields = %#v, want object", decoded["fields"])
	}
	if fields["check"] != "database" || fields["status"] != "healthy" {
		t.Fatalf("fields = %#v, want database/healthy", fields)
	}
}

func TestLoggerCurrentlyWritesSensitiveFieldValuesVerbatim(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	logger := Logger{
		Writer: &output,
		Now: func() time.Time {
			return time.Date(2026, 4, 9, 18, 30, 0, 0, time.UTC)
		},
	}

	if err := logger.Log(Record{
		Level:     LevelWarn,
		Component: "github",
		Message:   "request failed",
		Fields: map[string]any{
			"github_token": "ghp_fake_characterization_token",
		},
	}); err != nil {
		t.Fatalf("Log() error = %v", err)
	}

	if !bytes.Contains(output.Bytes(), []byte("ghp_fake_characterization_token")) {
		t.Fatalf("Log() output = %q, want current verbatim field behavior", output.String())
	}
}

func int64Ptr(value int64) *int64 {
	return &value
}
