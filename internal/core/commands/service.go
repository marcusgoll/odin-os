package commands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"odin-os/internal/core/capabilities"
	"odin-os/internal/registry"
)

var errCommandGatewayMissing = errors.New("command gateway is required")
var errCommandCapabilityIDRequired = errors.New("command capability id is required")
var errCommandCapabilityVersionRequired = errors.New("command capability version is required")
var errInvalidCommandInput = errors.New("invalid command input")

type CapabilityGateway interface {
	GetCapability(id, version string) (capabilities.Descriptor, error)
	InvokeCapability(ctx context.Context, request capabilities.InvokeRequest) (capabilities.InvokeResponse, error)
}

type WorkflowRunner interface {
	Run(context.Context, capabilities.InvokeRequest) (capabilities.InvokeResponse, error)
}

type Service struct {
	caps      CapabilityGateway
	workflows WorkflowRunner
}

func NewService(caps CapabilityGateway, workflows WorkflowRunner) *Service {
	return &Service{caps: caps, workflows: workflows}
}

func (s *Service) Execute(ctx context.Context, req capabilities.InvokeRequest) (capabilities.InvokeResponse, error) {
	if s == nil || s.caps == nil {
		return capabilities.InvokeResponse{}, errCommandGatewayMissing
	}
	if strings.TrimSpace(req.CapabilityID) == "" {
		return capabilities.InvokeResponse{}, errCommandCapabilityIDRequired
	}
	if strings.TrimSpace(req.CapabilityVersion) == "" {
		return capabilities.InvokeResponse{}, errCommandCapabilityVersionRequired
	}

	descriptor, err := s.caps.GetCapability(req.CapabilityID, req.CapabilityVersion)
	if err != nil {
		return capabilities.InvokeResponse{}, err
	}

	if err := validateCommandInput(descriptor, req.Input); err != nil {
		return capabilities.InvokeResponse{}, err
	}

	if descriptor.Kind == registry.KindWorkflow && s.workflows != nil {
		return s.workflows.Run(ctx, req)
	}

	return s.caps.InvokeCapability(ctx, req)
}

func validateCommandInput(descriptor capabilities.Descriptor, input json.RawMessage) error {
	if descriptor.InputSchema.Type != "object" {
		return nil
	}
	if strings.TrimSpace(string(input)) == "" {
		return fmt.Errorf("%w: input is required", errInvalidCommandInput)
	}

	var payload any
	if err := json.Unmarshal(input, &payload); err != nil {
		return fmt.Errorf("%w: %v", errInvalidCommandInput, err)
	}
	if _, ok := payload.(map[string]any); !ok {
		return fmt.Errorf("%w: expected JSON object input", errInvalidCommandInput)
	}
	return nil
}
