package catalog

import (
	"context"
	"fmt"
	"strings"

	"odin-os/internal/adapters/browserhuman"
	"odin-os/internal/tools/invocation"
)

func BuiltinDefinitions() map[string]ToolDefinition {
	definitions := []ToolDefinition{
		{
			Key:        "huginn_browser_session",
			Title:      "Huginn Browser Session",
			Summary:    "Runs the bounded generic browser session workflow.",
			Scopes:     []string{"global", "project", "odin-core", "new-project"},
			Tags:       []string{"browser", "human", "session"},
			CostHint:   CostHintLow,
			BudgetCost: 1,
			SourceRef:  "builtin://huginn_browser_session",
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
			Scopes:     []string{"global", "project", "odin-core", "new-project"},
			Tags:       []string{"browser", "human", "plaid", "transfer"},
			CostHint:   CostHintMedium,
			BudgetCost: 2,
			SourceRef:  "builtin://plaid_transfer_application",
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
	}

	index := make(map[string]ToolDefinition, len(definitions))
	for _, definition := range definitions {
		index[definition.Key] = definition
	}
	return index
}

func BuiltinDefinitionsWithInvoker(invoker invocation.Invoker) map[string]ToolDefinition {
	definitions := BuiltinDefinitions()
	if invoker == nil {
		return definitions
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

	definitions[definition.Key] = definition
	return definitions
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
