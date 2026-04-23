package integration_test

import (
	"encoding/json"
	"testing"

	"odin-os/internal/core/capabilities"
	"odin-os/internal/executors/claude_code"
	"odin-os/internal/executors/codex"
)

func TestProviderBridgesPreserveCanonicalResultEnvelope(t *testing.T) {
	t.Parallel()

	response := capabilities.InvokeResponse{
		RunID:  "run-42",
		Status: "completed",
		Output: json.RawMessage(`{"ok":true}`),
		Artifacts: []capabilities.Artifact{
			{Name: "log", Type: "text/plain", URI: "file:///tmp/run.log"},
		},
		Error: &capabilities.RunError{Code: "provider_error", Message: "upstream failure"},
	}

	codexResult, err := codex.NewBridge().FromInvokeResponse(response)
	if err != nil {
		t.Fatalf("codex FromInvokeResponse() error = %v", err)
	}
	claudeResult, err := claude_code.NewBridge().FromInvokeResponse(response)
	if err != nil {
		t.Fatalf("claude FromInvokeResponse() error = %v", err)
	}

	if codexResult.RunID != "run-42" || codexResult.Status != "completed" {
		t.Fatalf("bridge result envelope = %+v, want run-42/completed", codexResult)
	}
	if string(codexResult.Output) != `{"ok":true}` {
		t.Fatalf("bridge result output = %s, want %s", codexResult.Output, `{"ok":true}`)
	}
	if len(codexResult.Artifacts) != 1 || codexResult.Artifacts[0].Name != "log" {
		t.Fatalf("bridge result artifacts = %+v, want one log artifact", codexResult.Artifacts)
	}
	if codexResult.Error == nil || codexResult.Error.Code != "provider_error" {
		t.Fatalf("bridge result error = %+v, want provider_error", codexResult.Error)
	}
	if claudeResult.RunID != codexResult.RunID || claudeResult.Status != codexResult.Status {
		t.Fatalf("bridge results differ: codex=%+v claude=%+v", codexResult, claudeResult)
	}
	if string(claudeResult.Output) != string(codexResult.Output) {
		t.Fatalf("bridge output differs: codex=%s claude=%s", codexResult.Output, claudeResult.Output)
	}
	if len(claudeResult.Artifacts) != len(codexResult.Artifacts) || claudeResult.Artifacts[0].Name != codexResult.Artifacts[0].Name {
		t.Fatalf("bridge artifacts differ: codex=%+v claude=%+v", codexResult.Artifacts, claudeResult.Artifacts)
	}
	if claudeResult.Error == nil || claudeResult.Error.Code != codexResult.Error.Code || claudeResult.Error.Message != codexResult.Error.Message {
		t.Fatalf("bridge errors differ: codex=%+v claude=%+v", codexResult.Error, claudeResult.Error)
	}
}
