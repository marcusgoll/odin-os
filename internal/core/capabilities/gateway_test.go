package capabilities

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"odin-os/internal/registry"
)

type testRunLookup struct {
	run RunEnvelope
	err error
}

func (lookup testRunLookup) GetRunEnvelope(context.Context, int64) (RunEnvelope, error) {
	if lookup.err != nil {
		return RunEnvelope{}, lookup.err
	}
	return lookup.run, nil
}

func TestGatewayListsCapabilities(t *testing.T) {
	t.Parallel()

	gateway := &Gateway{
		snapshot: func() Snapshot {
			return Snapshot{
				Digest: "digest-123",
				Capabilities: map[string]Descriptor{
					"skill.alpha": {
						Kind:         registry.KindSkill,
						Key:          "skill.alpha",
						Name:         "Alpha Skill",
						Version:      "1.0.0",
						Availability: registry.Availability{Scope: "project"},
					},
					"command.beta": {
						Kind:         registry.KindCommand,
						Key:          "command.beta",
						Name:         "Beta Command",
						Version:      "1.0.0",
						Availability: registry.Availability{Scope: "project"},
					},
					"skill.global": {
						Kind:         registry.KindSkill,
						Key:          "skill.global",
						Name:         "Global Skill",
						Version:      "1.0.0",
						Availability: registry.Availability{Scope: "global"},
					},
				},
			}
		},
	}

	cards := gateway.ListCapabilities(registry.KindSkill, "project")
	if len(cards) != 1 {
		t.Fatalf("ListCapabilities() len = %d, want 1", len(cards))
	}
	if cards[0].ID != "skill.alpha" {
		t.Fatalf("ListCapabilities()[0].ID = %q, want %q", cards[0].ID, "skill.alpha")
	}
	if cards[0].Kind != registry.KindSkill {
		t.Fatalf("ListCapabilities()[0].Kind = %q, want %q", cards[0].Kind, registry.KindSkill)
	}
	if cards[0].Scope != "project" {
		t.Fatalf("ListCapabilities()[0].Scope = %q, want %q", cards[0].Scope, "project")
	}
}

func TestGatewayReturnsExpandedDescriptor(t *testing.T) {
	t.Parallel()

	gateway := &Gateway{
		snapshot: func() Snapshot {
			return Snapshot{
				Digest: "digest-123",
				Capabilities: map[string]Descriptor{
					"skill.alpha": {
						Kind:         registry.KindSkill,
						Key:          "skill.alpha",
						Name:         "Alpha Skill",
						Version:      "2.1.0",
						Summary:      "Expanded descriptor",
						Availability: registry.Availability{Scope: "project"},
						Permissions:  []string{"filesystem"},
					},
				},
			}
		},
	}

	descriptor, err := gateway.GetCapability("skill.alpha", "2.1.0")
	if err != nil {
		t.Fatalf("GetCapability() error = %v", err)
	}
	if descriptor.Key != "skill.alpha" {
		t.Fatalf("GetCapability().Key = %q, want %q", descriptor.Key, "skill.alpha")
	}
	if descriptor.Version != "2.1.0" {
		t.Fatalf("GetCapability().Version = %q, want %q", descriptor.Version, "2.1.0")
	}
	if len(descriptor.Permissions) != 1 || descriptor.Permissions[0] != "filesystem" {
		t.Fatalf("GetCapability().Permissions = %+v, want filesystem", descriptor.Permissions)
	}
}

func TestGatewayRejectsMissingVersionLookup(t *testing.T) {
	t.Parallel()

	gateway := &Gateway{
		snapshot: func() Snapshot {
			return Snapshot{
				Digest: "digest-123",
				Capabilities: map[string]Descriptor{
					"skill.alpha": {
						Kind:         registry.KindSkill,
						Key:          "skill.alpha",
						Version:      "2.1.0",
						InputSchema:  registry.SchemaRef{Type: "object"},
						Availability: registry.Availability{Scope: "project"},
					},
				},
			}
		},
		invoke: func(context.Context, InvokeRequest, Descriptor) (InvokeResponse, error) {
			t.Fatal("invoke callback should not be called for missing version")
			return InvokeResponse{}, nil
		},
	}

	if _, err := gateway.GetCapability("skill.alpha", ""); err == nil {
		t.Fatal("GetCapability() error = nil, want error")
	}

	if _, err := gateway.InvokeCapability(context.Background(), InvokeRequest{
		RequestID:    "req-1",
		CapabilityID: "skill.alpha",
		Input:        json.RawMessage(`{"input":"ok"}`),
	}); err == nil {
		t.Fatal("InvokeCapability() error = nil, want error")
	}
}

func TestGatewayRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	gateway := &Gateway{
		snapshot: func() Snapshot {
			return Snapshot{
				Digest: "digest-123",
				Capabilities: map[string]Descriptor{
					"skill.alpha": {
						Kind:        registry.KindSkill,
						Key:         "skill.alpha",
						Version:     "1.0.0",
						InputSchema: registry.SchemaRef{Type: "object"},
					},
				},
			}
		},
		invoke: func(context.Context, InvokeRequest, Descriptor) (InvokeResponse, error) {
			t.Fatal("invoke callback should not be called for invalid input")
			return InvokeResponse{}, nil
		},
	}

	_, err := gateway.InvokeCapability(context.Background(), InvokeRequest{
		RequestID:         "req-1",
		CapabilityID:      "skill.alpha",
		CapabilityVersion: "1.0.0",
		Input:             json.RawMessage(`"not-an-object"`),
	})
	if err == nil {
		t.Fatal("InvokeCapability() error = nil, want error")
	}
}

func TestGatewayRejectsBlankInput(t *testing.T) {
	t.Parallel()

	gateway := &Gateway{
		snapshot: func() Snapshot {
			return Snapshot{
				Digest: "digest-123",
				Capabilities: map[string]Descriptor{
					"skill.alpha": {
						Kind:        registry.KindSkill,
						Key:         "skill.alpha",
						Version:     "1.0.0",
						InputSchema: registry.SchemaRef{Type: "object"},
					},
				},
			}
		},
		invoke: func(context.Context, InvokeRequest, Descriptor) (InvokeResponse, error) {
			t.Fatal("invoke callback should not be called for blank input")
			return InvokeResponse{}, nil
		},
	}

	_, err := gateway.InvokeCapability(context.Background(), InvokeRequest{
		RequestID:         "req-2",
		CapabilityID:      "skill.alpha",
		CapabilityVersion: "1.0.0",
		Input:             json.RawMessage(``),
	})
	if err == nil {
		t.Fatal("InvokeCapability() error = nil, want error")
	}
}

func TestGatewayReturnsRunEnvelope(t *testing.T) {
	t.Parallel()

	gateway := &Gateway{
		runs: testRunLookup{
			run: RunEnvelope{
				RunID:  "42",
				Status: "completed",
				Artifacts: []Artifact{
					{Name: "log", Type: "text/plain", URI: "file:///tmp/run.log"},
				},
			},
		},
	}

	envelope, err := gateway.GetRun(context.Background(), 42)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if envelope.RunID != "42" {
		t.Fatalf("GetRun().RunID = %q, want %q", envelope.RunID, "42")
	}
	if envelope.Status != "completed" {
		t.Fatalf("GetRun().Status = %q, want %q", envelope.Status, "completed")
	}
	if len(envelope.Artifacts) != 1 || envelope.Artifacts[0].Name != "log" {
		t.Fatalf("GetRun().Artifacts = %+v, want one log artifact", envelope.Artifacts)
	}
}

func TestGatewayReturnsRunLookupError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("lookup failed")
	gateway := &Gateway{
		runs: testRunLookup{err: wantErr},
	}

	_, err := gateway.GetRun(context.Background(), 42)
	if !errors.Is(err, wantErr) {
		t.Fatalf("GetRun() error = %v, want %v", err, wantErr)
	}
}

func TestGatewayReturnsPolicyErrorForDeniedPermission(t *testing.T) {
	gateway := newGatewayWithDescriptor(Descriptor{
		Kind:         registry.KindCommand,
		Key:          "project.status",
		Version:      "1.0.0",
		Availability: registry.Availability{Scope: "project"},
		Permissions:  []string{"filesystem"},
		InputSchema:  registry.SchemaRef{Type: "object"},
		OutputSchema: registry.SchemaRef{Type: "object"},
	}, func(context.Context, InvokeRequest, Descriptor) (InvokeResponse, error) {
		t.Fatal("invoke callback should not be called when permissions are denied")
		return InvokeResponse{}, nil
	})

	_, err := gateway.InvokeCapability(context.Background(), InvokeRequest{
		RequestID:         "req-1",
		CapabilityID:      "project.status",
		CapabilityVersion: "1.0.0",
		Scope: ScopeRef{
			Kind: "project",
		},
		Caller: CallerRef{
			Kind: "guest",
		},
		Input: json.RawMessage(`{}`),
	})
	if err == nil {
		t.Fatal("InvokeCapability() error = nil, want policy failure")
	}

	code, ok := errorCode(err)
	if !ok {
		t.Fatalf("InvokeCapability() error = %v, want coded policy error", err)
	}
	if code != "permission_denied" {
		t.Fatalf("InvokeCapability() code = %q, want %q", code, "permission_denied")
	}
}

func TestGatewayRejectsInvalidScopeForCapability(t *testing.T) {
	gateway := newGatewayWithDescriptor(Descriptor{
		Kind:         registry.KindCommand,
		Key:          "project.status",
		Version:      "1.0.0",
		Availability: registry.Availability{Scope: "project"},
		InputSchema:  registry.SchemaRef{Type: "object"},
		OutputSchema: registry.SchemaRef{Type: "object"},
	}, func(context.Context, InvokeRequest, Descriptor) (InvokeResponse, error) {
		t.Fatal("invoke callback should not be called when scope is invalid")
		return InvokeResponse{}, nil
	})

	_, err := gateway.InvokeCapability(context.Background(), InvokeRequest{
		RequestID:         "req-2",
		CapabilityID:      "project.status",
		CapabilityVersion: "1.0.0",
		Scope: ScopeRef{
			Kind: "global",
		},
		Caller: CallerRef{
			Kind: "cli",
		},
		Input: json.RawMessage(`{}`),
	})
	if err == nil {
		t.Fatal("InvokeCapability() error = nil, want invalid scope failure")
	}

	code, ok := errorCode(err)
	if !ok {
		t.Fatalf("InvokeCapability() error = %v, want coded policy error", err)
	}
	if code != "invalid_scope" {
		t.Fatalf("InvokeCapability() code = %q, want %q", code, "invalid_scope")
	}
}
