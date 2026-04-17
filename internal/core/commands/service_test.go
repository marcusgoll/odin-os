package commands

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"odin-os/internal/core/capabilities"
	"odin-os/internal/registry"
)

type recordingCapabilityGateway struct {
	descriptor       capabilities.Descriptor
	getCalls         []getCapabilityCall
	invokeCalls      []capabilities.InvokeRequest
	invokeResponse   capabilities.InvokeResponse
	getCapabilityErr error
	invokeErr        error
}

type getCapabilityCall struct {
	id      string
	version string
}

func (gateway *recordingCapabilityGateway) GetCapability(id, version string) (capabilities.Descriptor, error) {
	gateway.getCalls = append(gateway.getCalls, getCapabilityCall{id: id, version: version})
	if gateway.getCapabilityErr != nil {
		return capabilities.Descriptor{}, gateway.getCapabilityErr
	}
	return gateway.descriptor, nil
}

func (gateway *recordingCapabilityGateway) InvokeCapability(_ context.Context, request capabilities.InvokeRequest) (capabilities.InvokeResponse, error) {
	gateway.invokeCalls = append(gateway.invokeCalls, request)
	if gateway.invokeErr != nil {
		return capabilities.InvokeResponse{}, gateway.invokeErr
	}
	return gateway.invokeResponse, nil
}

func TestCommandServiceResolvesRegistryCommand(t *testing.T) {
	t.Parallel()

	gateway := &recordingCapabilityGateway{
		descriptor: capabilities.Descriptor{
			Kind:         registry.KindCommand,
			Key:          "project.status",
			Version:      "1.0.0",
			InputSchema:  registry.SchemaRef{Type: "object"},
			OutputSchema: registry.SchemaRef{Type: "object"},
		},
		invokeResponse: capabilities.InvokeResponse{
			Status: "ok",
			Output: json.RawMessage(`{"status":"ready"}`),
		},
	}

	service := Service{caps: gateway}
	response, err := service.Execute(context.Background(), capabilities.InvokeRequest{
		RequestID:         "req-1",
		CapabilityID:      "project.status",
		CapabilityVersion: "1.0.0",
		Input:             json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if len(gateway.getCalls) != 1 {
		t.Fatalf("GetCapability() calls = %d, want 1", len(gateway.getCalls))
	}
	if got := gateway.getCalls[0]; got.id != "project.status" || got.version != "1.0.0" {
		t.Fatalf("GetCapability() call = %+v, want project.status 1.0.0", got)
	}
	if len(gateway.invokeCalls) != 1 {
		t.Fatalf("InvokeCapability() calls = %d, want 1", len(gateway.invokeCalls))
	}
	if got := response.Output; string(got) != `{"status":"ready"}` {
		t.Fatalf("Execute() response.Output = %s, want status payload", got)
	}
}

func TestCommandServiceRejectsNonCommandDescriptor(t *testing.T) {
	t.Parallel()

	gateway := &recordingCapabilityGateway{
		descriptor: capabilities.Descriptor{
			Kind:         registry.KindSkill,
			Key:          "skill.triage",
			Version:      "1.0.0",
			InputSchema:  registry.SchemaRef{Type: "object"},
			OutputSchema: registry.SchemaRef{Type: "object"},
		},
	}

	service := Service{caps: gateway}
	_, err := service.Execute(context.Background(), capabilities.InvokeRequest{
		RequestID:         "req-1",
		CapabilityID:      "skill.triage",
		CapabilityVersion: "1.0.0",
		Input:             json.RawMessage(`{}`),
	})
	if err == nil {
		t.Fatal("Execute() error = nil, want error")
	}
	if !errors.Is(err, errUnsupportedCommandCapabilityKind) {
		t.Fatalf("Execute() error = %v, want errUnsupportedCommandCapabilityKind", err)
	}
	if len(gateway.getCalls) != 1 {
		t.Fatalf("GetCapability() calls = %d, want 1", len(gateway.getCalls))
	}
	if len(gateway.invokeCalls) != 0 {
		t.Fatalf("InvokeCapability() calls = %d, want 0", len(gateway.invokeCalls))
	}
}

func TestCommandServiceRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	gateway := &recordingCapabilityGateway{
		descriptor: capabilities.Descriptor{
			Kind:        registry.KindCommand,
			Key:         "project.status",
			Version:     "1.0.0",
			InputSchema: registry.SchemaRef{Type: "object"},
		},
	}

	service := Service{caps: gateway}
	_, err := service.Execute(context.Background(), capabilities.InvokeRequest{
		RequestID:         "req-2",
		CapabilityID:      "project.status",
		CapabilityVersion: "1.0.0",
		Input:             json.RawMessage(`"not-an-object"`),
	})
	if err == nil {
		t.Fatal("Execute() error = nil, want error")
	}
	if !errors.Is(err, errInvalidCommandInput) {
		t.Fatalf("Execute() error = %v, want errInvalidCommandInput", err)
	}
	if len(gateway.invokeCalls) != 0 {
		t.Fatalf("InvokeCapability() calls = %d, want 0", len(gateway.invokeCalls))
	}
}
