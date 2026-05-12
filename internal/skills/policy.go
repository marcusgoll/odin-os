package skills

import (
	"fmt"
	"strings"

	corepolicy "odin-os/internal/core/policy"
	"odin-os/internal/core/projects"
)

type InvocationPolicyInput struct {
	ResolvedScopeKind string             `json:"resolved_scope_kind,omitempty"`
	Project           *InvocationProject `json:"project,omitempty"`
	Permissions       []string           `json:"permissions,omitempty"`
}

type InvocationPolicy struct {
	Allowed          bool                 `json:"allowed"`
	ReadOnly         bool                 `json:"read_only"`
	Mutating         bool                 `json:"mutating"`
	ActionClass      projects.ActionClass `json:"action_class"`
	LimitedActionKey string               `json:"limited_action_key,omitempty"`
	ApprovalNeeded   bool                 `json:"approval_needed"`
}

type resolvedPermission struct {
	rank             int
	actionClass      projects.ActionClass
	readOnly         bool
	mutating         bool
	limitedActionKey string
}

func ResolveInvocationPolicy(input InvocationPolicyInput) (InvocationPolicy, error) {
	scopeKind := normalizeInvocationScopeKind(input.ResolvedScopeKind)
	permissions, err := parseInvocationPermissions(input.Permissions)
	if err != nil {
		return InvocationPolicy{}, err
	}
	if len(permissions) == 0 {
		return InvocationPolicy{}, fmt.Errorf("permission set is required")
	}

	policy, err := resolveInvocationPolicyFromPermissions(permissions)
	if err != nil {
		return InvocationPolicy{}, err
	}
	systemProject := input.Project != nil && input.Project.SystemProject && policy.Mutating
	policy.ApprovalNeeded = corepolicy.RequiresApprovalForActionClass(string(policy.ActionClass), systemProject)
	policy.Allowed = true

	if policy.Mutating {
		switch scopeKind {
		case "global":
			policy.Allowed = false
			return policy, fmt.Errorf("mutating permissions are not allowed in global scope")
		case "new-project":
			policy.Allowed = false
			return policy, fmt.Errorf("mutating permissions are not allowed in new-project scope")
		case "project", "odin-core":
			if input.Project == nil || (strings.TrimSpace(input.Project.Key) == "" && !input.Project.SystemProject) {
				policy.Allowed = false
				return policy, fmt.Errorf("mutating permissions require project metadata in %s scope", scopeKind)
			}
		default:
			policy.Allowed = false
			return policy, fmt.Errorf("mutating permissions are not allowed in unknown scope %q", scopeKind)
		}
	}

	return policy, nil
}

func normalizeInvocationScopeKind(scopeKind string) string {
	scopeKind = strings.TrimSpace(scopeKind)
	if scopeKind == "" {
		return "global"
	}
	return scopeKind
}

func parseInvocationPermissions(rawPermissions []string) ([]Permission, error) {
	permissions := make([]Permission, 0, len(rawPermissions))
	for _, raw := range rawPermissions {
		permission, err := ParsePermission(raw)
		if err != nil {
			return nil, err
		}
		permissions = append(permissions, permission)
	}
	return permissions, nil
}

func resolveInvocationPolicyFromPermissions(permissions []Permission) (InvocationPolicy, error) {
	best := resolvedPermission{
		rank: -1,
	}

	for _, permission := range permissions {
		current, err := resolvedPermissionFromPermission(permission)
		if err != nil {
			return InvocationPolicy{}, err
		}
		if current.rank > best.rank {
			best = current
			continue
		}
		if current.rank == best.rank {
			if best.limitedActionKey != "" && current.limitedActionKey != "" && best.limitedActionKey != current.limitedActionKey {
				return InvocationPolicy{}, fmt.Errorf("conflicting isolated permissions %q and %q", best.limitedActionKey, current.limitedActionKey)
			}
			if best.limitedActionKey == "" {
				best.limitedActionKey = current.limitedActionKey
			}
		}
	}

	if best.rank == -1 {
		return InvocationPolicy{}, fmt.Errorf("permission set is required")
	}

	return InvocationPolicy{
		ReadOnly:         best.readOnly,
		Mutating:         best.mutating,
		ActionClass:      best.actionClass,
		LimitedActionKey: best.limitedActionKey,
	}, nil
}

func resolvedPermissionFromPermission(permission Permission) (resolvedPermission, error) {
	switch permission.Kind {
	case PermissionKindRepoRead, PermissionKindRuntimeRead:
		return resolvedPermission{
			rank:        0,
			actionClass: projects.ActionClassReadOnly,
			readOnly:    true,
		}, nil
	case PermissionKindRepoMutateIsolated:
		return resolvedPermission{
			rank:             1,
			actionClass:      projects.ActionClassIsolatedMutation,
			mutating:         true,
			limitedActionKey: permission.ActionKey,
		}, nil
	case PermissionKindRepoMutateFull:
		return resolvedPermission{
			rank:        2,
			actionClass: projects.ActionClassFullMutation,
			mutating:    true,
		}, nil
	case PermissionKindRepoMutateGovernance:
		return resolvedPermission{
			rank:        3,
			actionClass: projects.ActionClassGovernanceMutation,
			mutating:    true,
		}, nil
	case PermissionKindRepoMutateDestructive:
		return resolvedPermission{
			rank:        4,
			actionClass: projects.ActionClassDestructiveMutation,
			mutating:    true,
		}, nil
	default:
		return resolvedPermission{}, fmt.Errorf("unsupported permission kind %q", permission.Kind)
	}
}
