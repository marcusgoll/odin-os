package capabilities

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"odin-os/internal/registry"
)

type codedError interface {
	Code() string
}

func errorCode(err error) (string, bool) {
	var coded codedError
	if errors.As(err, &coded) {
		return coded.Code(), true
	}
	return "", false
}

func newGatewayWithDescriptor(descriptor Descriptor, invoke InvokerFunc) *Gateway {
	return &Gateway{
		snapshot: func() Snapshot {
			return Snapshot{
				Digest: "digest-123",
				Capabilities: map[string]Descriptor{
					descriptor.Key: descriptor,
				},
			}
		},
		invoke: invoke,
	}
}

func TestValidateInvocationInputAgainstSchema(t *testing.T) {
	tests := []struct {
		name       string
		descriptor Descriptor
	}{
		{
			name: "missing input schema",
			descriptor: Descriptor{
				Kind:         registry.KindCommand,
				Key:          "project.status",
				Version:      "1.0.0",
				Availability: registry.Availability{Scope: "project"},
				OutputSchema: registry.SchemaRef{Type: "object"},
			},
		},
		{
			name: "missing output schema",
			descriptor: Descriptor{
				Kind:         registry.KindCommand,
				Key:          "project.status",
				Version:      "1.0.0",
				Availability: registry.Availability{Scope: "project"},
				InputSchema:  registry.SchemaRef{Type: "object"},
			},
		},
	}

	for _, testCase := range tests {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			gateway := newGatewayWithDescriptor(testCase.descriptor, func(context.Context, InvokeRequest, Descriptor) (InvokeResponse, error) {
				t.Fatal("invoke callback should not be called when validation fails")
				return InvokeResponse{}, nil
			})

			_, err := gateway.InvokeCapability(context.Background(), InvokeRequest{
				RequestID:         "req-1",
				CapabilityID:      testCase.descriptor.Key,
				CapabilityVersion: testCase.descriptor.Version,
				Input:             json.RawMessage(`{}`),
			})
			if err == nil {
				t.Fatal("InvokeCapability() error = nil, want validation failure")
			}

			code, ok := errorCode(err)
			if !ok {
				t.Fatalf("InvokeCapability() error = %v, want coded validation error", err)
			}
			if code != "validation_failed" {
				t.Fatalf("InvokeCapability() code = %q, want %q", code, "validation_failed")
			}
		})
	}
}

func TestValidateInvocationRejectsMismatchedDeclaredInputTypes(t *testing.T) {
	tests := []struct {
		name      string
		inputType string
	}{
		{name: "array", inputType: "array"},
		{name: "string", inputType: "string"},
		{name: "number", inputType: "number"},
		{name: "integer", inputType: "integer"},
		{name: "boolean", inputType: "boolean"},
		{name: "null", inputType: "null"},
	}

	for _, testCase := range tests {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			descriptor := Descriptor{
				Kind:         registry.KindCommand,
				Key:          "project.status",
				Version:      "1.0.0",
				Availability: registry.Availability{Scope: "project"},
				InputSchema:  registry.SchemaRef{Type: testCase.inputType},
				OutputSchema: registry.SchemaRef{Type: "object"},
			}
			gateway := newGatewayWithDescriptor(descriptor, func(context.Context, InvokeRequest, Descriptor) (InvokeResponse, error) {
				t.Fatal("invoke callback should not be called when input schema validation fails")
				return InvokeResponse{}, nil
			})

			_, err := gateway.InvokeCapability(context.Background(), InvokeRequest{
				RequestID:         "req-1",
				CapabilityID:      descriptor.Key,
				CapabilityVersion: descriptor.Version,
				Input:             json.RawMessage(`{}`),
			})
			if err == nil {
				t.Fatal("InvokeCapability() error = nil, want validation failure")
			}

			code, ok := errorCode(err)
			if !ok {
				t.Fatalf("InvokeCapability() error = %v, want coded validation error", err)
			}
			if code != "validation_failed" {
				t.Fatalf("InvokeCapability() code = %q, want %q", code, "validation_failed")
			}
		})
	}
}

func TestValidateInvocationAcceptsDeclaredInputTypes(t *testing.T) {
	tests := []struct {
		name      string
		inputType string
		input     json.RawMessage
	}{
		{name: "object", inputType: "object", input: json.RawMessage(`{"key":"value"}`)},
		{name: "array", inputType: "array", input: json.RawMessage(`[1,2,3]`)},
		{name: "string", inputType: "string", input: json.RawMessage(`"value"`)},
		{name: "number", inputType: "number", input: json.RawMessage(`1.25`)},
		{name: "integer", inputType: "integer", input: json.RawMessage(`42`)},
		{name: "boolean", inputType: "boolean", input: json.RawMessage(`true`)},
		{name: "null", inputType: "null", input: json.RawMessage(`null`)},
	}

	for _, testCase := range tests {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			descriptor := Descriptor{
				Kind:         registry.KindCommand,
				Key:          "project.status",
				Version:      "1.0.0",
				Availability: registry.Availability{Scope: "project"},
				InputSchema:  registry.SchemaRef{Type: testCase.inputType},
				OutputSchema: registry.SchemaRef{Type: "object"},
			}
			gateway := newGatewayWithDescriptor(descriptor, func(context.Context, InvokeRequest, Descriptor) (InvokeResponse, error) {
				return InvokeResponse{RunID: "run-1"}, nil
			})

			response, err := gateway.InvokeCapability(context.Background(), InvokeRequest{
				RequestID:         "req-1",
				CapabilityID:      descriptor.Key,
				CapabilityVersion: descriptor.Version,
				Caller:            CallerRef{Kind: "api", ID: "validation-test"},
				Input:             testCase.input,
			})
			if err != nil {
				t.Fatalf("InvokeCapability() error = %v, want success", err)
			}
			if response.RunID != "run-1" {
				t.Fatalf("InvokeCapability().RunID = %q, want %q", response.RunID, "run-1")
			}
		})
	}
}

func TestValidateInvocationRejectsUnsupportedDeclaredInputType(t *testing.T) {
	descriptor := Descriptor{
		Kind:         registry.KindCommand,
		Key:          "project.status",
		Version:      "1.0.0",
		Availability: registry.Availability{Scope: "project"},
		InputSchema:  registry.SchemaRef{Ref: "#/components/schemas/Input", Type: "uuid"},
		OutputSchema: registry.SchemaRef{Type: "object"},
	}
	gateway := newGatewayWithDescriptor(descriptor, func(context.Context, InvokeRequest, Descriptor) (InvokeResponse, error) {
		t.Fatal("invoke callback should not be called when input schema validation fails")
		return InvokeResponse{}, nil
	})

	_, err := gateway.InvokeCapability(context.Background(), InvokeRequest{
		RequestID:         "req-1",
		CapabilityID:      descriptor.Key,
		CapabilityVersion: descriptor.Version,
		Input:             json.RawMessage(`{"key":"value"}`),
	})
	if err == nil {
		t.Fatal("InvokeCapability() error = nil, want validation failure")
	}

	code, ok := errorCode(err)
	if !ok {
		t.Fatalf("InvokeCapability() error = %v, want coded validation error", err)
	}
	if code != "validation_failed" {
		t.Fatalf("InvokeCapability() code = %q, want %q", code, "validation_failed")
	}
}

func TestValidateInvocationAcceptsInputSchemaRefWithoutType(t *testing.T) {
	descriptor := Descriptor{
		Kind:         registry.KindCommand,
		Key:          "project.status",
		Version:      "1.0.0",
		Availability: registry.Availability{Scope: "project"},
		InputSchema:  registry.SchemaRef{Ref: "#/components/schemas/Input"},
		OutputSchema: registry.SchemaRef{Type: "object"},
	}
	gateway := newGatewayWithDescriptor(descriptor, func(context.Context, InvokeRequest, Descriptor) (InvokeResponse, error) {
		return InvokeResponse{RunID: "run-1"}, nil
	})

	response, err := gateway.InvokeCapability(context.Background(), InvokeRequest{
		RequestID:         "req-1",
		CapabilityID:      descriptor.Key,
		CapabilityVersion: descriptor.Version,
		Caller:            CallerRef{Kind: "api", ID: "validation-test"},
		Input:             json.RawMessage(`"string"`),
	})
	if err != nil {
		t.Fatalf("InvokeCapability() error = %v, want success", err)
	}
	if response.RunID != "run-1" {
		t.Fatalf("InvokeCapability().RunID = %q, want %q", response.RunID, "run-1")
	}
}
