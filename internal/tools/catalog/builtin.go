package catalog

import (
	"context"
	"fmt"

	"odin-os/internal/tools/invocation"
)

func BuiltinDefinitions() map[string]ToolDefinition {
	return BuiltinDefinitionsWithInvoker(nil)
}

func BuiltinDefinitionsWithInvoker(invoker invocation.Invoker) map[string]ToolDefinition {
	definitions := []ToolDefinition{
		projectStatusDefinition(invoker),
		{
			Key:        "task_list",
			Title:      "Task List",
			Summary:    "Lists task projections for the requested scope.",
			Scopes:     []string{"global", "project", "odin-core", "new-project"},
			Tags:       []string{"runtime", "tasks"},
			CostHint:   CostHintLow,
			BudgetCost: 1,
			SourceRef:  "builtin://task_list",
			Schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"scope": map[string]any{"type": "string"},
				},
			},
			Invoke: func(input map[string]string) (StructuredResult, error) {
				scope := input["scope"]
				if scope == "" {
					scope = "global"
				}
				return StructuredResult{
					CapabilityKey:   "task_list",
					Source:          "builtin",
					Summary:         fmt.Sprintf("Task list prepared for %s scope.", scope),
					KeyFacts:        map[string]string{"scope": scope},
					FollowOnOptions: []string{"expand sub-agent", "invoke event_log"},
					RawRef:          "builtin://task_list/result",
					RawOutput:       fmt.Sprintf("scope=%s tasks=0", scope),
				}, nil
			},
		},
		{
			Key:        "event_log",
			Title:      "Event Log",
			Summary:    "Retrieves recent audit event summaries.",
			Scopes:     []string{"global", "project", "odin-core", "new-project"},
			Tags:       []string{"runtime", "events"},
			CostHint:   CostHintMedium,
			BudgetCost: 2,
			SourceRef:  "builtin://event_log",
			Schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"limit": map[string]any{"type": "integer"},
				},
			},
			Invoke: func(input map[string]string) (StructuredResult, error) {
				limit := input["limit"]
				if limit == "" {
					limit = "10"
				}
				return StructuredResult{
					CapabilityKey:   "event_log",
					Source:          "builtin",
					Summary:         fmt.Sprintf("Event log prepared with limit %s.", limit),
					KeyFacts:        map[string]string{"limit": limit},
					FollowOnOptions: []string{"invoke task_list"},
					RawRef:          "builtin://event_log/result",
					RawOutput:       fmt.Sprintf("limit=%s events=0", limit),
				}, nil
			},
		},
	}

	index := make(map[string]ToolDefinition, len(definitions))
	for _, definition := range definitions {
		index[definition.Key] = definition
	}
	return index
}

func projectStatusDefinition(invoker invocation.Invoker) ToolDefinition {
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
	}

	if invoker != nil {
		definition.Invoke = func(input map[string]string) (StructuredResult, error) {
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
		}
		return definition
	}

	definition.Invoke = func(input map[string]string) (StructuredResult, error) {
		projectKey := input["project_key"]
		if projectKey == "" {
			projectKey = "current"
		}
		return StructuredResult{
			CapabilityKey:   "project_status",
			Source:          "builtin",
			Summary:         fmt.Sprintf("Project status prepared for %s.", projectKey),
			KeyFacts:        map[string]string{"project_key": projectKey},
			FollowOnOptions: []string{"expand skill", "inspect tasks"},
			RawRef:          "builtin://project_status/result",
			RawOutput:       fmt.Sprintf("project=%s status=ready", projectKey),
		}, nil
	}
	return definition
}

func structuredResultFromInvocation(result invocation.Result, projectKey string) StructuredResult {
	keyFacts := cloneStringMap(result.KeyFacts)
	if keyFacts == nil {
		keyFacts = make(map[string]string)
	}
	if projectKey != "" && keyFacts["project_key"] == "" {
		keyFacts["project_key"] = projectKey
	}

	source := result.Source
	if source == "" {
		source = "driver"
	}

	return StructuredResult{
		CapabilityKey:   "project_status",
		Source:          source,
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
