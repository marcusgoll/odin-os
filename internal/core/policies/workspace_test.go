package policies

import "testing"

func TestWorkspacePolicyDefaultIsFailClosed(t *testing.T) {
	t.Parallel()

	policy := DefaultWorkspacePolicy()

	if policy.RequireApprovalForExternalEffects != true {
		t.Fatalf("RequireApprovalForExternalEffects = %v, want true", policy.RequireApprovalForExternalEffects)
	}
	if len(policy.ExternalSideEffects) != 0 {
		t.Fatalf("ExternalSideEffects len = %d, want 0", len(policy.ExternalSideEffects))
	}
	if policy.ToolPolicy.Mode != ToolPolicyModeDeny {
		t.Fatalf("ToolPolicy.Mode = %q, want %q", policy.ToolPolicy.Mode, ToolPolicyModeDeny)
	}
}
