package catalog

import (
	"context"
	"fmt"
	"strings"

	"odin-os/internal/adapters/browserhuman"
	"odin-os/internal/tools/invocation"
)

// BuiltinDefinitions contains bootstrap-only tool inventory until each tool has
// a manifest-backed or gateway-backed replacement.
func BuiltinDefinitions() map[string]ToolDefinition {
	definitions := []ToolDefinition{
		{
			Key:        "project_status",
			Title:      "Project Status",
			Summary:    "Summarizes managed project status for planning.",
			Version:    "1.0.0",
			Scopes:     []string{"global", "project", "odin-core"},
			Tags:       []string{"project", "status", "bootstrap-only"},
			CostHint:   CostHintLow,
			BudgetCost: 1,
			SourceRef:  "bootstrap://legacy/project_status",
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
				return StructuredResult{
					CapabilityKey:   "project_status",
					Summary:         fmt.Sprintf("Project status prepared for %s.", projectKey),
					KeyFacts:        map[string]string{"project_key": projectKey},
					FollowOnOptions: []string{"expand skill", "inspect tasks"},
					RawRef:          "builtin://project_status/result",
					RawOutput:       fmt.Sprintf("project=%s status=ready", projectKey),
				}, nil
			},
		},
		{
			Key:        "huginn_browser_session",
			Title:      "Huginn Browser Session",
			Summary:    "Runs the bounded generic browser session workflow.",
			Version:    "1.0.0",
			Scopes:     []string{"global", "project", "odin-core", "new-project"},
			Tags:       []string{"browser", "human", "session", "bootstrap-only"},
			CostHint:   CostHintLow,
			BudgetCost: 1,
			SourceRef:  "bootstrap://driver/huginn_browser_session",
			Schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"action": map[string]any{
						"type":        "string",
						"enum":        []string{"health", "launch", "snapshot", "screenshot", "stop"},
						"description": "Bounded browser session action.",
					},
					"url": map[string]any{
						"type":        "string",
						"description": "Optional URL to inspect in the browser session.",
					},
					"path": map[string]any{
						"type":        "string",
						"description": "Optional filesystem path for session artifacts.",
					},
				},
				"required":             []string{"action"},
				"additionalProperties": false,
			},
			Invoke: func(input map[string]string) (StructuredResult, error) {
				return invokeBrowserHuman("huginn_browser_session", input, []string{
					"inspect browser artifacts",
					"run plaid_transfer_application",
				})
			},
		},
		{
			Key:        "plaid_transfer_application",
			Title:      "Plaid Transfer Application",
			Summary:    "Runs the bounded Plaid Transfer application workflow.",
			Version:    "1.0.0",
			Scopes:     []string{"global", "project", "odin-core", "new-project"},
			Tags:       []string{"browser", "human", "plaid", "transfer", "bootstrap-only"},
			CostHint:   CostHintMedium,
			BudgetCost: 2,
			SourceRef:  "bootstrap://driver/plaid_transfer_application",
			Schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"application_url": map[string]any{
						"type":        "string",
						"description": "Optional Plaid application URL to open.",
					},
					"path": map[string]any{
						"type":        "string",
						"description": "Optional filesystem path for workflow artifacts.",
					},
				},
				"additionalProperties": false,
			},
			Invoke: func(input map[string]string) (StructuredResult, error) {
				return invokeBrowserHuman("plaid_transfer_application", input, []string{
					"inspect browser artifacts",
					"stop browser session",
				})
			},
		},
		{
			Key:        "task_list",
			Title:      "Task List",
			Summary:    "Lists task projections for the requested scope.",
			Version:    "1.0.0",
			Scopes:     []string{"global", "project", "odin-core", "new-project"},
			Tags:       []string{"runtime", "tasks", "bootstrap-only"},
			CostHint:   CostHintLow,
			BudgetCost: 1,
			SourceRef:  "bootstrap://legacy/task_list",
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
			Version:    "1.0.0",
			Scopes:     []string{"global", "project", "odin-core", "new-project"},
			Tags:       []string{"runtime", "events", "bootstrap-only"},
			CostHint:   CostHintMedium,
			BudgetCost: 2,
			SourceRef:  "bootstrap://legacy/event_log",
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

func invokeBrowserHuman(toolKey string, input map[string]string, followOnOptions []string) (StructuredResult, error) {
	var requestInput any
	if len(input) > 0 {
		requestInput = input
	}

	response, err := invocation.Service{}.BrowserHuman(context.Background(), browserhuman.Request{
		ToolKey: toolKey,
		Input:   requestInput,
	})
	if err != nil {
		return StructuredResult{}, err
	}

	result := StructuredResult{
		CapabilityKey:   response.ToolKey,
		Summary:         response.Summary,
		KeyFacts:        browserHumanKeyFacts(response.Artifacts),
		FollowOnOptions: append([]string(nil), followOnOptions...),
		RawRef:          fmt.Sprintf("browserhuman://%s/result", toolKey),
		RawOutput:       response.RawOutput,
	}
	if strings.TrimSpace(result.CapabilityKey) == "" {
		result.CapabilityKey = toolKey
	}
	return result, nil
}

func browserHumanKeyFacts(artifacts map[string]any) map[string]string {
	facts := make(map[string]string)
	for _, key := range []string{"session_state", "current_url", "screenshot_path", "next_action"} {
		if value, ok := artifacts[key]; ok {
			if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
				facts[key] = text
			}
		}
	}
	if len(facts) == 0 {
		return nil
	}
	return facts
}
