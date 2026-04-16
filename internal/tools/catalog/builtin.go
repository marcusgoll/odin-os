package catalog

import (
	"context"

	"odin-os/internal/tools/invocation"
)

func BuiltinDefinitions() map[string]ToolDefinition {
	return map[string]ToolDefinition{}
}

func BuiltinDefinitionsWithInvoker(invoker invocation.Invoker) map[string]ToolDefinition {
	if invoker == nil {
		return BuiltinDefinitions()
	}

	definition := ToolDefinition{
		Key:        "project_status",
		Title:      "Project Status",
		Summary:    "Summarizes managed project status for planning.",
		Scopes:     []string{"global", "project", "odin-core"},
		Tags:       []string{"project", "status"},
		CostHint:   CostHintLow,
		BudgetCost: 1,
		SourceRef:  "builtin://project_status",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"project_key": map[string]any{"type": "string"},
			},
		},
		Invoke: func(input map[string]string) (StructuredResult, error) {
			projectKey := input["project_key"]
			if projectKey == "" {
				projectKey = "current"
			}

			result, err := invoker.Invoke(context.Background(), "project_status", invocation.Request{
				Args: cloneStringMap(input),
			})
			if err != nil {
				return StructuredResult{}, err
			}
			return structuredResultFromInvocation(result, projectKey), nil
		},
	}

	return map[string]ToolDefinition{
		definition.Key: definition,
	}
}

func structuredResultFromInvocation(result invocation.Result, projectKey string) StructuredResult {
	keyFacts := cloneStringMap(result.KeyFacts)
	if keyFacts == nil {
		keyFacts = make(map[string]string)
	}
	if projectKey != "" && keyFacts["project_key"] == "" {
		keyFacts["project_key"] = projectKey
	}

	return StructuredResult{
		CapabilityKey:   "project_status",
		Source:          "driver",
		Summary:         result.Summary,
		KeyFacts:        keyFacts,
		FollowOnOptions: append([]string(nil), result.FollowOnOptions...),
		RawRef:          result.RawRef,
		RawOutput:       result.RawOutput,
	}
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}

	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}
