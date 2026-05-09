package skills

import (
	"testing"

	"odin-os/internal/core/projects"
)

func TestResolveInvocationPolicyAllowsReadOnlyPermissionsInGlobalScope(t *testing.T) {
	t.Parallel()

	policy, err := ResolveInvocationPolicy(InvocationPolicyInput{
		ResolvedScopeKind: "global",
		Permissions:       []string{"repo.read"},
	})
	if err != nil {
		t.Fatalf("ResolveInvocationPolicy() error = %v", err)
	}
	if !policy.Allowed {
		t.Fatal("policy.Allowed = false, want true")
	}
	if !policy.ReadOnly {
		t.Fatal("policy.ReadOnly = false, want true")
	}
	if policy.Mutating {
		t.Fatal("policy.Mutating = true, want false")
	}
	if policy.ActionClass != projects.ActionClassReadOnly {
		t.Fatalf("policy.ActionClass = %q, want %q", policy.ActionClass, projects.ActionClassReadOnly)
	}
	if policy.ApprovalNeeded {
		t.Fatal("policy.ApprovalNeeded = true, want false")
	}
}

func TestResolveInvocationPolicyDeniesMutatingPermissionsInGlobalScope(t *testing.T) {
	t.Parallel()

	policy, err := ResolveInvocationPolicy(InvocationPolicyInput{
		ResolvedScopeKind: "global",
		Permissions:       []string{"repo.mutate.full"},
	})
	if err == nil {
		t.Fatal("ResolveInvocationPolicy() error = nil, want denial")
	}
	if policy.Allowed {
		t.Fatal("policy.Allowed = true, want false")
	}
}

func TestResolveInvocationPolicyExtractsLimitedActionKey(t *testing.T) {
	t.Parallel()

	policy, err := ResolveInvocationPolicy(InvocationPolicyInput{
		ResolvedScopeKind: "project",
		Project: &InvocationProject{
			Key: "alpha",
		},
		Permissions: []string{"repo.mutate.isolated:docs_audit_note"},
	})
	if err != nil {
		t.Fatalf("ResolveInvocationPolicy() error = %v", err)
	}
	if !policy.Allowed {
		t.Fatal("policy.Allowed = false, want true")
	}
	if policy.ReadOnly {
		t.Fatal("policy.ReadOnly = true, want false")
	}
	if policy.ActionClass != projects.ActionClassIsolatedMutation {
		t.Fatalf("policy.ActionClass = %q, want %q", policy.ActionClass, projects.ActionClassIsolatedMutation)
	}
	if policy.LimitedActionKey != "docs_audit_note" {
		t.Fatalf("policy.LimitedActionKey = %q, want %q", policy.LimitedActionKey, "docs_audit_note")
	}
	if policy.ApprovalNeeded {
		t.Fatal("policy.ApprovalNeeded = true, want false")
	}
}

func TestResolveInvocationPolicyCollapsesToMostRestrictiveEffectivePolicy(t *testing.T) {
	t.Parallel()

	policy, err := ResolveInvocationPolicy(InvocationPolicyInput{
		ResolvedScopeKind: "project",
		Project: &InvocationProject{
			Key: "alpha",
		},
		Permissions: []string{"repo.mutate.full", "repo.read"},
	})
	if err != nil {
		t.Fatalf("ResolveInvocationPolicy() error = %v", err)
	}
	if !policy.Mutating {
		t.Fatal("policy.Mutating = false, want true")
	}
	if policy.ReadOnly {
		t.Fatal("policy.ReadOnly = true, want false")
	}
	if policy.ActionClass != projects.ActionClassFullMutation {
		t.Fatalf("policy.ActionClass = %q, want %q", policy.ActionClass, projects.ActionClassFullMutation)
	}
	if policy.ApprovalNeeded {
		t.Fatal("policy.ApprovalNeeded = true, want false")
	}
}

func TestResolveInvocationPolicyMarksGovernanceMutationAsApprovalNeeded(t *testing.T) {
	t.Parallel()

	policy, err := ResolveInvocationPolicy(InvocationPolicyInput{
		ResolvedScopeKind: "project",
		Project: &InvocationProject{
			Key: "alpha",
		},
		Permissions: []string{"repo.mutate.governance"},
	})
	if err != nil {
		t.Fatalf("ResolveInvocationPolicy() error = %v", err)
	}
	if !policy.Mutating {
		t.Fatal("policy.Mutating = false, want true")
	}
	if !policy.ApprovalNeeded {
		t.Fatal("policy.ApprovalNeeded = false, want true")
	}
	if policy.ActionClass != projects.ActionClassGovernanceMutation {
		t.Fatalf("policy.ActionClass = %q, want %q", policy.ActionClass, projects.ActionClassGovernanceMutation)
	}
}

func TestResolveInvocationPolicyMarksDestructiveMutationAsApprovalNeeded(t *testing.T) {
	t.Parallel()

	policy, err := ResolveInvocationPolicy(InvocationPolicyInput{
		ResolvedScopeKind: "project",
		Project: &InvocationProject{
			Key: "alpha",
		},
		Permissions: []string{"repo.mutate.destructive"},
	})
	if err != nil {
		t.Fatalf("ResolveInvocationPolicy() error = %v", err)
	}
	if !policy.Mutating {
		t.Fatal("policy.Mutating = false, want true")
	}
	if !policy.ApprovalNeeded {
		t.Fatal("policy.ApprovalNeeded = false, want true")
	}
	if policy.ActionClass != projects.ActionClassDestructiveMutation {
		t.Fatalf("policy.ActionClass = %q, want %q", policy.ActionClass, projects.ActionClassDestructiveMutation)
	}
}

func TestResolveInvocationPolicyMarksSystemProjectMutationAsApprovalNeeded(t *testing.T) {
	t.Parallel()

	policy, err := ResolveInvocationPolicy(InvocationPolicyInput{
		ResolvedScopeKind: "odin-core",
		Project: &InvocationProject{
			Key:           "odin-core",
			SystemProject: true,
		},
		Permissions: []string{"repo.mutate.full"},
	})
	if err != nil {
		t.Fatalf("ResolveInvocationPolicy() error = %v", err)
	}
	if !policy.Mutating {
		t.Fatal("policy.Mutating = false, want true")
	}
	if !policy.ApprovalNeeded {
		t.Fatal("policy.ApprovalNeeded = false, want true")
	}
}

func TestResolveInvocationPolicyDeniesMutationInNewProjectScope(t *testing.T) {
	t.Parallel()

	policy, err := ResolveInvocationPolicy(InvocationPolicyInput{
		ResolvedScopeKind: "new-project",
		Permissions:       []string{"repo.mutate.full"},
	})
	if err == nil {
		t.Fatal("ResolveInvocationPolicy() error = nil, want denial")
	}
	if policy.Allowed {
		t.Fatal("policy.Allowed = true, want false")
	}
}

func TestResolveInvocationPolicyDeniesProjectMutationWithoutProjectMetadata(t *testing.T) {
	t.Parallel()

	policy, err := ResolveInvocationPolicy(InvocationPolicyInput{
		ResolvedScopeKind: "project",
		Permissions:       []string{"repo.mutate.full"},
	})
	if err == nil {
		t.Fatal("ResolveInvocationPolicy() error = nil, want denial")
	}
	if policy.Allowed {
		t.Fatal("policy.Allowed = true, want false")
	}
}

func TestResolveInvocationPolicyRejectsConflictingIsolatedPermissions(t *testing.T) {
	t.Parallel()

	policy, err := ResolveInvocationPolicy(InvocationPolicyInput{
		ResolvedScopeKind: "project",
		Project: &InvocationProject{
			Key: "alpha",
		},
		Permissions: []string{
			"repo.mutate.isolated:docs_audit_note",
			"repo.mutate.isolated:repo_hygiene_note",
		},
	})
	if err == nil {
		t.Fatal("ResolveInvocationPolicy() error = nil, want conflicting isolated permission rejection")
	}
	if policy.Allowed {
		t.Fatal("policy.Allowed = true, want false")
	}
}

func TestResolveInvocationPolicyDeniesUnknownOrEmptyPermissionsAtInvocationTime(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   InvocationPolicyInput
	}{
		{
			name: "empty",
			in:   InvocationPolicyInput{ResolvedScopeKind: "project"},
		},
		{
			name: "unknown",
			in: InvocationPolicyInput{
				ResolvedScopeKind: "project",
				Permissions:       []string{"repo.write"},
			},
		},
		{
			name: "blank",
			in: InvocationPolicyInput{
				ResolvedScopeKind: "project",
				Permissions:       []string{""},
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			policy, err := ResolveInvocationPolicy(tt.in)
			if err == nil {
				t.Fatal("ResolveInvocationPolicy() error = nil, want denial")
			}
			if policy.Allowed {
				t.Fatal("policy.Allowed = true, want false")
			}
		})
	}
}
