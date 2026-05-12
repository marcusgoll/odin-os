package capabilities

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"odin-os/internal/core/policy"
	"odin-os/internal/registry"
	"odin-os/internal/tools/catalog"
)

const BuiltinToolVersion = "1.0.0"

func FromRegistrySnapshot(digest string, snapshot registry.Snapshot) Snapshot {
	if strings.TrimSpace(digest) == "" {
		digest = "registry-snapshot"
	}

	capabilities := make(map[string]Descriptor, len(snapshot.Items))
	for _, item := range snapshot.Items {
		if strings.TrimSpace(item.Key) == "" {
			continue
		}
		capabilities[item.Key] = cloneDescriptor(item)
	}

	return Snapshot{
		Digest:       digest,
		Diagnostics:  append([]registry.Diagnostic(nil), snapshot.Diagnostics...),
		Capabilities: capabilities,
	}
}

func WithBuiltinToolDescriptors(snapshot Snapshot, definitions map[string]catalog.ToolDefinition) Snapshot {
	next := cloneSnapshot(snapshot)
	if next.Digest == "" {
		next.Digest = "capability-snapshot"
	}
	if next.Capabilities == nil {
		next.Capabilities = map[string]Descriptor{}
	}

	descriptors := BuiltinToolDescriptors(definitions)
	for key, descriptor := range descriptors {
		next.Capabilities[key] = descriptor
	}
	return next
}

func BuiltinToolDescriptors(definitions map[string]catalog.ToolDefinition) map[string]Descriptor {
	descriptors := make(map[string]Descriptor)
	keys := make([]string, 0, len(definitions))
	for key := range definitions {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		definition := definitions[key]
		if definition.Hidden {
			continue
		}
		if strings.TrimSpace(definition.Key) == "" {
			continue
		}

		sourceRef := strings.TrimSpace(definition.SourceRef)
		if sourceRef == "" {
			sourceRef = "builtin://" + definition.Key
		}

		descriptors[definition.Key] = Descriptor{
			APIVersion:   registry.NormalizedAPIVersion,
			Kind:         registry.KindTool,
			Key:          definition.Key,
			Name:         definition.Key,
			Version:      BuiltinToolVersion,
			Title:        definition.Title,
			Summary:      definition.Summary,
			Status:       "active",
			Availability: registry.Availability{Scope: primaryScope(definition.Scopes)},
			Scopes:       append([]string(nil), definition.Scopes...),
			Tags:         append([]string(nil), definition.Tags...),
			InputSchema: registry.SchemaRef{
				Ref:  "schema://odin/tools/" + definition.Key + "/input",
				Type: schemaType(definition.Schema),
			},
			OutputSchema: registry.SchemaRef{
				Ref:  "schema://odin/tools/" + definition.Key + "/output",
				Type: "object",
			},
			Execution: registry.ExecutionPolicy{
				Mode: "local",
			},
			Implementation: registry.ImplementationRef{
				Kind: "builtin_tool",
				Ref:  sourceRef,
			},
		}
	}

	return descriptors
}

func InvokeBuiltinToolCapability(ctx context.Context, definitions map[string]catalog.ToolDefinition, request InvokeRequest, descriptor Descriptor) (InvokeResponse, error) {
	_ = ctx
	if descriptor.Kind != registry.KindTool {
		return InvokeResponse{}, fmt.Errorf("capability %q is not a builtin tool", descriptor.Key)
	}

	definition, ok := definitions[request.CapabilityID]
	if !ok {
		return InvokeResponse{}, fmt.Errorf("unknown builtin tool %q", request.CapabilityID)
	}
	if definition.CanonicalKey != "" && definition.CanonicalKey != definition.Key {
		canonical, ok := definitions[definition.CanonicalKey]
		if !ok {
			return InvokeResponse{}, fmt.Errorf("unknown canonical builtin tool %q", definition.CanonicalKey)
		}
		definition = canonical
	}
	if definition.Hidden {
		return InvokeResponse{}, fmt.Errorf("builtin tool %q is hidden", request.CapabilityID)
	}
	if err := policy.NewService(nil).AuthorizeApproval(ctx, policy.ApprovalRequest{
		Subject:  fmt.Sprintf("tool %q", definition.Key),
		Required: definition.RequiresApproval,
		Reason:   definition.ApprovalReason,
	}); err != nil {
		return InvokeResponse{}, err
	}
	if definition.Invoke == nil {
		return InvokeResponse{}, fmt.Errorf("builtin tool %q is not invokable", request.CapabilityID)
	}

	input, err := builtinToolInput(request.Input)
	if err != nil {
		return InvokeResponse{}, err
	}
	result, err := definition.Invoke(input)
	if err != nil {
		return InvokeResponse{}, err
	}

	output, err := json.Marshal(map[string]any{
		"tool_key":          result.CapabilityKey,
		"summary":           result.Summary,
		"artifacts":         append([]string(nil), result.Artifacts...),
		"key_facts":         cloneStringMap(result.KeyFacts),
		"follow_on_options": append([]string(nil), result.FollowOnOptions...),
		"raw_ref":           result.RawRef,
		"raw_output":        result.RawOutput,
	})
	if err != nil {
		return InvokeResponse{}, err
	}

	return InvokeResponse{
		RunID:     result.RawRef,
		Status:    "completed",
		Output:    output,
		Artifacts: toolArtifacts(result.Artifacts),
	}, nil
}

func primaryScope(scopes []string) string {
	for _, scope := range scopes {
		scope = strings.TrimSpace(scope)
		if scope != "" {
			return scope
		}
	}
	return "global"
}

func schemaType(schema map[string]any) string {
	if raw, ok := schema["type"].(string); ok && strings.TrimSpace(raw) != "" {
		return strings.TrimSpace(raw)
	}
	return "object"
}

func builtinToolInput(raw json.RawMessage) (map[string]string, error) {
	if strings.TrimSpace(string(raw)) == "" {
		return map[string]string{}, nil
	}

	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("decode builtin tool input: %w", err)
	}

	input := make(map[string]string, len(payload))
	for key, value := range payload {
		switch typed := value.(type) {
		case string:
			input[key] = typed
		case nil:
			input[key] = ""
		default:
			encoded, err := json.Marshal(typed)
			if err != nil {
				return nil, fmt.Errorf("encode builtin tool input %q: %w", key, err)
			}
			input[key] = string(encoded)
		}
	}
	return input, nil
}

func toolArtifacts(values []string) []Artifact {
	artifacts := make([]Artifact, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			continue
		}
		artifacts = append(artifacts, Artifact{
			Name: value,
			Type: "tool_artifact",
			URI:  value,
		})
	}
	return artifacts
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
