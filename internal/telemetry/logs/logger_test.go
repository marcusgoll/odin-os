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

func TestLoggerRedactsSensitiveStructuredFields(t *testing.T) {
	t.Parallel()

	const (
		githubToken  = "ghp_1234567890abcdefghijklmnopqrstuvwx"
		openAIToken  = "sk-proj-1234567890abcdefghijklmnopqrstuvwxyzABCDE"
		adminToken   = "local-admin-token-value"
		genericToken = "generic-token-1234567890abcdef"
	)

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
		Message:   "request failed with " + openAIToken,
		Fields: map[string]any{
			"github_token":          githubToken,
			"openai_error":          "request failed with " + openAIToken,
			"admin-token":           adminToken,
			"callback_url":          "https://example.test/callback?token=" + genericToken + "&status=failed",
			"header_" + githubToken: "present",
			"nested": map[string]any{
				"authorization": "Bearer " + genericToken,
				"status":        "queued",
			},
			"argv": []any{
				"--admin-token=" + adminToken,
				"--mode=read-only",
			},
			"request_id":  "req-123",
			"status":      "retryable",
			"token_count": 2048,
		},
	}); err != nil {
		t.Fatalf("Log() error = %v", err)
	}

	for _, leaked := range []string{githubToken, openAIToken, adminToken, genericToken} {
		if bytes.Contains(output.Bytes(), []byte(leaked)) {
			t.Fatalf("Log() output leaked %q: %s", leaked, output.String())
		}
	}
	if !bytes.Contains(output.Bytes(), []byte("[REDACTED]")) {
		t.Fatalf("Log() output = %q, want redaction marker", output.String())
	}

	var decoded map[string]any
	if err := json.Unmarshal(output.Bytes(), &decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	fields, ok := decoded["fields"].(map[string]any)
	if !ok {
		t.Fatalf("fields = %#v, want object", decoded["fields"])
	}
	for key, want := range map[string]any{
		"request_id":  "req-123",
		"status":      "retryable",
		"token_count": float64(2048),
	} {
		if fields[key] != want {
			t.Fatalf("fields[%q] = %v, want %v", key, fields[key], want)
		}
	}
	if fields["github_token"] != "[REDACTED]" || fields["admin-token"] != "[REDACTED]" {
		t.Fatalf("fields = %#v, want sensitive key values redacted", fields)
	}
	if _, ok := fields["header_[REDACTED]"]; !ok {
		t.Fatalf("fields = %#v, want token-like key redacted", fields)
	}
	nested, ok := fields["nested"].(map[string]any)
	if !ok {
		t.Fatalf("fields[nested] = %#v, want object", fields["nested"])
	}
	if nested["authorization"] != "[REDACTED]" || nested["status"] != "queued" {
		t.Fatalf("nested fields = %#v, want secret redacted and status preserved", nested)
	}
}

func int64Ptr(value int64) *int64 {
	return &value
}
