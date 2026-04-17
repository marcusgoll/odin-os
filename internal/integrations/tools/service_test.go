package tools

import (
	"testing"

	"odin-os/internal/tools/catalog"
)

func TestToolServiceAuthorizesAndInvokesDefinitions(t *testing.T) {
	t.Parallel()

	service := Service{
		Definitions: map[string]catalog.ToolDefinition{
			"project_status": {
				Key:        "project_status",
				Title:      "Project Status",
				Summary:    "Summarizes project status.",
				SourceRef:  "builtin://project_status",
				BudgetCost: 1,
				Invoke: func(input map[string]string) (catalog.StructuredResult, error) {
					return catalog.StructuredResult{
						CapabilityKey: "project_status",
						Summary:       "Project status prepared.",
						RawRef:        "builtin://project_status/result",
						RawOutput:     "project=alpha status=ready",
					}, nil
				},
			},
		},
		AuthorizeFunc: func(request ToolRequest) AuthorizationResult {
			if request.Scope != "project" {
				return AuthorizationResult{Allowed: false, Reason: "project scope required"}
			}
			return AuthorizationResult{Allowed: true}
		},
	}

	authorization := service.Authorize(ToolRequest{
		ToolKey: "project_status",
		Scope:   "project",
	})
	if !authorization.Allowed {
		t.Fatalf("Authorize() = %+v, want allowed", authorization)
	}

	result, err := service.Invoke(ToolRequest{
		ToolKey: "project_status",
		Scope:   "project",
		Parameters: map[string]string{
			"project_key": "alpha",
		},
	})
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if result.ToolKey != "project_status" {
		t.Fatalf("ToolKey = %q, want project_status", result.ToolKey)
	}
	if result.Summary == "" {
		t.Fatalf("Summary = empty, want value")
	}
}

func TestToolServiceRejectsUnauthorizedInvocation(t *testing.T) {
	t.Parallel()

	service := Service{
		Definitions: map[string]catalog.ToolDefinition{
			"project_status": {
				Key: "project_status",
			},
		},
		AuthorizeFunc: func(ToolRequest) AuthorizationResult {
			return AuthorizationResult{Allowed: false, Reason: "denied"}
		},
	}

	_, err := service.Invoke(ToolRequest{
		ToolKey: "project_status",
		Scope:   "global",
	})
	if err == nil {
		t.Fatal("Invoke() error = nil, want authorization denial")
	}
}
