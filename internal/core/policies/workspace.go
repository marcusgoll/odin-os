package policies

import "odin-os/internal/executors/contract"

const (
	ToolPolicyModeAllow = "allow"
	ToolPolicyModeDeny  = "deny"
)

type WorkspacePolicy struct {
	ToolPolicy                        contract.ToolPolicy
	ExternalSideEffects               []string
	RequireApprovalForExternalEffects bool
}

type PolicyOverlay struct {
	ToolPolicy                        *contract.ToolPolicy
	ExternalSideEffects               []string
	RequireApprovalForExternalEffects bool
}

func DefaultWorkspacePolicy() WorkspacePolicy {
	return WorkspacePolicy{
		ToolPolicy: contract.ToolPolicy{
			Mode:    ToolPolicyModeDeny,
			Allowed: []string{},
		},
		ExternalSideEffects:               []string{},
		RequireApprovalForExternalEffects: true,
	}
}
