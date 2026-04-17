package claude_code

import (
	"encoding/json"
	"testing"

	"odin-os/internal/core/capabilities"
)

func TestClaudeBridgeBuildsCanonicalInvokeRequest(t *testing.T) {
	t.Parallel()

	bridge := NewBridge()
	got, err := bridge.ToInvokeRequest(ProviderCall{
		RequestID:         "req-2",
		CapabilityID:      "project.status",
		CapabilityVersion: "1.2.3",
		Scope:             capabilities.ScopeRef{Kind: "project", ProjectKey: "odin-core"},
		Caller:            capabilities.CallerRef{Kind: "user", ID: "operator"},
		Input:             json.RawMessage(`{"mode":"safe"}`),
		Execution:         capabilities.ExecutionRequest{Mode: "stream", Timeout: "30s", RetryLimit: 2},
		ProviderPrompt:    "claude-specific prompt shaping",
	})
	if err != nil {
		t.Fatalf("ToInvokeRequest() error = %v", err)
	}

	if got.RequestID != "req-2" {
		t.Fatalf("ToInvokeRequest().RequestID = %q, want %q", got.RequestID, "req-2")
	}
	if got.CapabilityID != "project.status" {
		t.Fatalf("ToInvokeRequest().CapabilityID = %q, want %q", got.CapabilityID, "project.status")
	}
	if got.CapabilityVersion != "1.2.3" {
		t.Fatalf("ToInvokeRequest().CapabilityVersion = %q, want %q", got.CapabilityVersion, "1.2.3")
	}
	if got.Scope.Kind != "project" || got.Scope.ProjectKey != "odin-core" {
		t.Fatalf("ToInvokeRequest().Scope = %+v, want project/odin-core", got.Scope)
	}
	if got.Caller.Kind != "user" || got.Caller.ID != "operator" {
		t.Fatalf("ToInvokeRequest().Caller = %+v, want user/operator", got.Caller)
	}
	if string(got.Input) != `{"mode":"safe"}` {
		t.Fatalf("ToInvokeRequest().Input = %s, want %s", got.Input, `{"mode":"safe"}`)
	}
	if got.Execution.Mode != "stream" || got.Execution.Timeout != "30s" || got.Execution.RetryLimit != 2 {
		t.Fatalf("ToInvokeRequest().Execution = %+v, want stream/30s/2", got.Execution)
	}
}
