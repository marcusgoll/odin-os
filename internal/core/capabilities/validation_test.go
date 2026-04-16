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
