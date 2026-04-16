package policy

import (
	"fmt"

	"odin-os/internal/core/policies"
	"odin-os/internal/executors/contract"
)

type Engine struct{}

type ExternalEffectDecision struct {
	Allowed          bool
	RequiresApproval bool
	Reason           string
}

func (Engine) Resolve(workspace policies.WorkspacePolicy, initiative *policies.PolicyOverlay, companion *policies.PolicyOverlay) policies.WorkspacePolicy {
	resolved := normalizeWorkspacePolicy(workspace)
	resolved = applyOverlay(resolved, initiative)
	resolved = applyOverlay(resolved, companion)
	return resolved
}

func (Engine) DecideExternalEffect(policy policies.WorkspacePolicy, effect string) ExternalEffectDecision {
	if !toolAllowed(policy.ToolPolicy, effect) {
		return ExternalEffectDecision{
			Allowed: false,
			Reason:  fmt.Sprintf("tool policy denies external effect %q", effect),
		}
	}
	if !contains(policy.ExternalSideEffects, effect) {
		return ExternalEffectDecision{
			Allowed: false,
			Reason:  fmt.Sprintf("external effect %q is not allowed", effect),
		}
	}

	return ExternalEffectDecision{
		Allowed:          true,
		RequiresApproval: policy.RequireApprovalForExternalEffects,
		Reason:           "external effect allowed by resolved policy",
	}
}

func normalizeWorkspacePolicy(policy policies.WorkspacePolicy) policies.WorkspacePolicy {
	defaults := policies.DefaultWorkspacePolicy()

	if policy.ToolPolicy.Mode == "" {
		policy.ToolPolicy = defaults.ToolPolicy
	}
	if policy.ExternalSideEffects == nil {
		policy.ExternalSideEffects = defaults.ExternalSideEffects
	}
	policy.RequireApprovalForExternalEffects = policy.RequireApprovalForExternalEffects || defaults.RequireApprovalForExternalEffects

	return policy
}

func applyOverlay(base policies.WorkspacePolicy, overlay *policies.PolicyOverlay) policies.WorkspacePolicy {
	if overlay == nil {
		return base
	}

	if overlay.ToolPolicy != nil {
		base.ToolPolicy = mergeToolPolicies(base.ToolPolicy, *overlay.ToolPolicy)
	}
	if overlay.ExternalSideEffects != nil {
		base.ExternalSideEffects = intersectStrings(base.ExternalSideEffects, overlay.ExternalSideEffects)
	}
	if overlay.RequireApprovalForExternalEffects {
		base.RequireApprovalForExternalEffects = true
	}

	return base
}

func mergeToolPolicies(base contract.ToolPolicy, overlay contract.ToolPolicy) contract.ToolPolicy {
	if base.Mode != policies.ToolPolicyModeAllow || overlay.Mode != policies.ToolPolicyModeAllow {
		return contract.ToolPolicy{Mode: policies.ToolPolicyModeDeny, Allowed: []string{}}
	}

	return contract.ToolPolicy{
		Mode:    policies.ToolPolicyModeAllow,
		Allowed: intersectStrings(base.Allowed, overlay.Allowed),
	}
}

func toolAllowed(policy contract.ToolPolicy, tool string) bool {
	if policy.Mode != policies.ToolPolicyModeAllow {
		return false
	}
	return contains(policy.Allowed, tool)
}

func intersectStrings(left []string, right []string) []string {
	if len(left) == 0 || len(right) == 0 {
		return []string{}
	}

	items := make([]string, 0, len(left))
	for _, candidate := range left {
		if contains(right, candidate) {
			items = append(items, candidate)
		}
	}
	return items
}

func contains(items []string, value string) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}
