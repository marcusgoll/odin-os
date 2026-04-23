package workflows

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"odin-os/internal/core/capabilities"
	"odin-os/internal/registry"
	"odin-os/internal/tools/catalog"
)

var errWorkflowGatewayMissing = errors.New("workflow gateway is required")
var errWorkflowStepCapabilityRequired = errors.New("workflow step capability is required")
var errWorkflowStepVersionRequired = errors.New("workflow step version is required")
var errWorkflowStepKindInvalid = errors.New("workflow step kind is invalid")

type CapabilityGateway interface {
	GetCapability(id, version string) (capabilities.Descriptor, error)
	InvokeCapability(ctx context.Context, request capabilities.InvokeRequest) (capabilities.InvokeResponse, error)
}

type Step struct {
	Capability   string
	VersionRange string
	With         map[string]any
}

type DependencyError struct {
	Workflow     string
	StepIndex    int
	Kind         string
	CapabilityID string
	Capability   string
	VersionRange string
	Stage        string
	Err          error
}

func (err *DependencyError) Error() string {
	if err == nil {
		return "<nil>"
	}

	parts := make([]string, 0, 4)
	if err.Workflow != "" {
		parts = append(parts, fmt.Sprintf("workflow %q", err.Workflow))
	}
	if err.Stage != "" {
		parts = append(parts, err.Stage)
	}
	if err.Capability != "" {
		parts = append(parts, err.Capability)
	} else if err.Kind != "" && err.CapabilityID != "" {
		parts = append(parts, fmt.Sprintf("%s:%s", err.Kind, err.CapabilityID))
	}
	if err.VersionRange != "" {
		parts = append(parts, "@"+err.VersionRange)
	}
	if err.Err != nil {
		parts = append(parts, err.Err.Error())
	}
	return strings.Join(parts, ": ")
}

func (err *DependencyError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.Err
}

type ResolvedStep struct {
	Step         Step
	Capability   string
	Kind         string
	CapabilityID string
	Version      string
	Descriptor   *capabilities.Descriptor
	Tool         *catalog.ToolDefinition
	Response     *capabilities.InvokeResponse
}

type Result struct {
	Steps []ResolvedStep
}

type Service struct {
	caps  CapabilityGateway
	tools map[string]catalog.ToolDefinition
}

func NewService(caps CapabilityGateway, tools map[string]catalog.ToolDefinition) *Service {
	clonedTools := make(map[string]catalog.ToolDefinition, len(tools))
	for key, definition := range tools {
		clonedTools[key] = cloneToolDefinition(definition)
	}

	return &Service{
		caps:  caps,
		tools: clonedTools,
	}
}

func (service *Service) Compile(workflow registry.Item) ([]Step, error) {
	if service == nil || service.caps == nil {
		return nil, errWorkflowGatewayMissing
	}

	steps := make([]Step, 0, len(workflow.Dependencies))
	for index, dependency := range workflow.Dependencies {
		step := Step{
			Capability:   fmt.Sprintf("%s:%s", dependency.Kind, dependency.Name),
			VersionRange: dependency.Version,
		}
		if _, err := service.resolveDependency(context.Background(), workflow.Key, index, step, false); err != nil {
			return nil, err
		}
		steps = append(steps, step)
	}
	return steps, nil
}

func (service *Service) Execute(ctx context.Context, steps []Step) (Result, error) {
	if service == nil || service.caps == nil {
		return Result{}, errWorkflowGatewayMissing
	}

	resolved := make([]ResolvedStep, 0, len(steps))
	for index, step := range steps {
		stepResult, err := service.resolveDependency(ctx, "", index, step, true)
		if err != nil {
			return Result{}, err
		}
		resolved = append(resolved, stepResult)
	}

	return Result{Steps: resolved}, nil
}

func (service *Service) resolveDependency(ctx context.Context, workflow string, index int, step Step, invoke bool) (ResolvedStep, error) {
	kind, capabilityID, err := parseStepCapability(step.Capability)
	if err != nil {
		return ResolvedStep{}, &DependencyError{
			Workflow:     workflow,
			StepIndex:    index,
			Capability:   step.Capability,
			VersionRange: step.VersionRange,
			Stage:        stageFor(invoke),
			Err:          err,
		}
	}
	if strings.TrimSpace(step.VersionRange) == "" {
		return ResolvedStep{}, &DependencyError{
			Workflow:     workflow,
			StepIndex:    index,
			Kind:         kind,
			CapabilityID: capabilityID,
			Capability:   step.Capability,
			VersionRange: step.VersionRange,
			Stage:        stageFor(invoke),
			Err:          errWorkflowStepVersionRequired,
		}
	}

	if kind == "tool" {
		definition, err := service.lookupTool(capabilityID, step.VersionRange)
		if err != nil {
			return ResolvedStep{}, &DependencyError{
				Workflow:     workflow,
				StepIndex:    index,
				Kind:         kind,
				Capability:   capabilityID,
				VersionRange: step.VersionRange,
				Stage:        stageFor(invoke),
				Err:          err,
			}
		}

		return ResolvedStep{
			Step:         step,
			Capability:   step.Capability,
			Kind:         kind,
			CapabilityID: capabilityID,
			Version:      step.VersionRange,
			Tool:         &definition,
		}, nil
	}

	descriptor, err := service.caps.GetCapability(capabilityID, step.VersionRange)
	if err != nil {
		return ResolvedStep{}, &DependencyError{
			Workflow:     workflow,
			StepIndex:    index,
			Kind:         kind,
			CapabilityID: capabilityID,
			Capability:   step.Capability,
			VersionRange: step.VersionRange,
			Stage:        stageFor(invoke),
			Err:          err,
		}
	}
	if string(descriptor.Kind) != kind {
		return ResolvedStep{}, &DependencyError{
			Workflow:     workflow,
			StepIndex:    index,
			Kind:         kind,
			CapabilityID: capabilityID,
			Capability:   step.Capability,
			VersionRange: step.VersionRange,
			Stage:        stageFor(invoke),
			Err:          fmt.Errorf("resolved capability kind %q does not match requested kind %q", descriptor.Kind, kind),
		}
	}

	resolved := ResolvedStep{
		Step:         step,
		Capability:   step.Capability,
		Kind:         kind,
		CapabilityID: capabilityID,
		Version:      descriptor.Version,
		Descriptor:   cloneDescriptor(&descriptor),
	}

	if invoke && kind == string(registry.KindCommand) {
		input, err := marshalStepInput(step.With)
		if err != nil {
			return ResolvedStep{}, &DependencyError{
				Workflow:     workflow,
				StepIndex:    index,
				Kind:         kind,
				CapabilityID: capabilityID,
				Capability:   step.Capability,
				VersionRange: step.VersionRange,
				Stage:        stageFor(invoke),
				Err:          err,
			}
		}
		response, err := service.caps.InvokeCapability(ctx, capabilities.InvokeRequest{
			CapabilityID:      capabilityID,
			CapabilityVersion: step.VersionRange,
			Input:             input,
		})
		if err != nil {
			return ResolvedStep{}, &DependencyError{
				Workflow:     workflow,
				StepIndex:    index,
				Kind:         kind,
				CapabilityID: capabilityID,
				Capability:   step.Capability,
				VersionRange: step.VersionRange,
				Stage:        stageFor(invoke),
				Err:          err,
			}
		}
		resolved.Response = &response
	}

	return resolved, nil
}

func (service *Service) lookupTool(key string, _ string) (catalog.ToolDefinition, error) {
	definition, ok := service.tools[key]
	if !ok {
		return catalog.ToolDefinition{}, fmt.Errorf("unknown tool %q", key)
	}
	return cloneToolDefinition(definition), nil
}

func parseStepCapability(value string) (string, string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", "", errWorkflowStepCapabilityRequired
	}

	kind, capability, ok := strings.Cut(trimmed, ":")
	if !ok || strings.TrimSpace(kind) == "" || strings.TrimSpace(capability) == "" {
		return "", "", errWorkflowStepKindInvalid
	}
	return strings.TrimSpace(kind), strings.TrimSpace(capability), nil
}

func stageFor(invoke bool) string {
	if invoke {
		return "execute"
	}
	return "compile"
}

func cloneDescriptor(descriptor *capabilities.Descriptor) *capabilities.Descriptor {
	if descriptor == nil {
		return nil
	}

	cloned := *descriptor
	cloned.Permissions = append([]string(nil), descriptor.Permissions...)
	cloned.Tags = append([]string(nil), descriptor.Tags...)
	cloned.Owners = append([]string(nil), descriptor.Owners...)
	cloned.Scopes = append([]string(nil), descriptor.Scopes...)
	cloned.Tools = append([]string(nil), descriptor.Tools...)
	cloned.AppliesTo = append([]string(nil), descriptor.AppliesTo...)
	cloned.Composes = append([]string(nil), descriptor.Composes...)
	cloned.Aliases = append([]string(nil), descriptor.Aliases...)
	cloned.Sections = cloneStringMap(descriptor.Sections)
	return &cloned
}

func cloneToolDefinition(definition catalog.ToolDefinition) catalog.ToolDefinition {
	cloned := definition
	cloned.Scopes = append([]string(nil), definition.Scopes...)
	cloned.Tags = append([]string(nil), definition.Tags...)
	if definition.Schema != nil {
		cloned.Schema = cloneAnyMap(definition.Schema)
	}
	return cloned
}

func cloneStringMap(values map[string]string) map[string]string {
	if values == nil {
		return nil
	}
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func cloneAnyMap(values map[string]any) map[string]any {
	if values == nil {
		return nil
	}
	cloned := make(map[string]any, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func marshalStepInput(values map[string]any) (json.RawMessage, error) {
	if len(values) == 0 {
		return json.RawMessage(`{}`), nil
	}
	payload, err := json.Marshal(values)
	if err != nil {
		return nil, err
	}
	return payload, nil
}
