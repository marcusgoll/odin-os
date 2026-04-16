package policy

import (
	"testing"

	"odin-os/internal/core/policies"
	"odin-os/internal/executors/contract"
)

func TestWorkspacePolicyResolveDefault(t *testing.T) {
	t.Parallel()

	engine := Engine{}
	resolved := engine.Resolve(policies.WorkspacePolicy{}, nil, nil)

	if resolved.RequireApprovalForExternalEffects != true {
		t.Fatalf("RequireApprovalForExternalEffects = %v, want true", resolved.RequireApprovalForExternalEffects)
	}
	if resolved.ToolPolicy.Mode != policies.ToolPolicyModeDeny {
		t.Fatalf("ToolPolicy.Mode = %q, want %q", resolved.ToolPolicy.Mode, policies.ToolPolicyModeDeny)
	}
}

func TestWorkspacePolicyOverlayKeepsApprovalGates(t *testing.T) {
	t.Parallel()

	engine := Engine{}
	workspace := policies.WorkspacePolicy{
		ToolPolicy: contract.ToolPolicy{
			Mode:    policies.ToolPolicyModeAllow,
			Allowed: []string{"calendar.read", "calendar.write"},
		},
		ExternalSideEffects:              []string{"calendar.write"},
		RequireApprovalForExternalEffects: true,
	}
	companion := &policies.PolicyOverlay{
		ToolPolicy: &contract.ToolPolicy{
			Mode:    policies.ToolPolicyModeAllow,
			Allowed: []string{"calendar.write"},
		},
		ExternalSideEffects:              []string{"calendar.write"},
		RequireApprovalForExternalEffects: boolPtr(false),
	}

	resolved := engine.Resolve(workspace, nil, companion)
	decision := engine.DecideExternalEffect(resolved, "calendar.write")

	if !decision.Allowed {
		t.Fatalf("Allowed = false, want true")
	}
	if !decision.RequiresApproval {
		t.Fatalf("RequiresApproval = false, want true")
	}
}

func TestWorkspacePolicyRejectsUnknownExternalSideEffects(t *testing.T) {
	t.Parallel()

	engine := Engine{}
	workspace := policies.WorkspacePolicy{
		ToolPolicy: contract.ToolPolicy{
			Mode:    policies.ToolPolicyModeAllow,
			Allowed: []string{"calendar.read"},
		},
		ExternalSideEffects: []string{"calendar.read"},
	}

	resolved := engine.Resolve(workspace, &policies.PolicyOverlay{
		ExternalSideEffects: []string{"calendar.read"},
	}, nil)
	decision := engine.DecideExternalEffect(resolved, "email.send")

	if decision.Allowed {
		t.Fatalf("Allowed = true, want false")
	}
	if decision.Reason == "" {
		t.Fatalf("Reason = empty, want explanation")
	}
}

func boolPtr(value bool) *bool {
	return &value
}
