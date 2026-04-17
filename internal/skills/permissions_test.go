package skills

import "testing"

func TestParsePermissionAcceptsEnforcedVocabulary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want Permission
	}{
		{
			name: "repo read",
			raw:  "repo.read",
			want: Permission{
				Raw:  "repo.read",
				Kind: PermissionKindRepoRead,
			},
		},
		{
			name: "runtime read",
			raw:  "runtime.read",
			want: Permission{
				Raw:  "runtime.read",
				Kind: PermissionKindRuntimeRead,
			},
		},
		{
			name: "isolated mutation",
			raw:  "repo.mutate.isolated:docs_audit_note",
			want: Permission{
				Raw:       "repo.mutate.isolated:docs_audit_note",
				Kind:      PermissionKindRepoMutateIsolated,
				ActionKey: "docs_audit_note",
			},
		},
		{
			name: "full mutation",
			raw:  "repo.mutate.full",
			want: Permission{
				Raw:  "repo.mutate.full",
				Kind: PermissionKindRepoMutateFull,
			},
		},
		{
			name: "governance mutation",
			raw:  "repo.mutate.governance",
			want: Permission{
				Raw:  "repo.mutate.governance",
				Kind: PermissionKindRepoMutateGovernance,
			},
		},
		{
			name: "destructive mutation",
			raw:  "repo.mutate.destructive",
			want: Permission{
				Raw:  "repo.mutate.destructive",
				Kind: PermissionKindRepoMutateDestructive,
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := ParsePermission(tt.raw)
			if err != nil {
				t.Fatalf("ParsePermission(%q) error = %v", tt.raw, err)
			}
			if got != tt.want {
				t.Fatalf("ParsePermission(%q) = %#v, want %#v", tt.raw, got, tt.want)
			}
		})
	}
}

func TestParsePermissionRejectsUnknownValue(t *testing.T) {
	t.Parallel()

	tests := []string{
		"repo.write",
		"runtime.write",
		"repo.mutate",
		"repo.mutate.partial",
		"repo.mutate.isolated",
		"repo.mutate.isolated:",
		"repo.mutate.isolated::docs_audit_note",
		"repo.mutate.isolated:docs-audit-note",
	}

	for _, raw := range tests {
		raw := raw
		t.Run(raw, func(t *testing.T) {
			t.Parallel()

			if _, err := ParsePermission(raw); err == nil {
				t.Fatalf("ParsePermission(%q) error = nil, want rejection", raw)
			}
		})
	}
}
