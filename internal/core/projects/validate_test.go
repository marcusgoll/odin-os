package projects

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateRejectsUnsafeProjectDefinitions(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	localGitRoot := filepath.Join(root, "local")
	if err := os.MkdirAll(filepath.Join(localGitRoot, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir git root: %v", err)
	}

	trueValue := true
	falseValue := false

	validPolicy := Policy{
		AllowedCommands: []string{"status"},
		BranchRules: BranchRules{
			ProtectedBranches:          []string{"main"},
			RequireWorktree:            &trueValue,
			RequireTaskBranch:          &trueValue,
			AllowDefaultBranchMutation: &falseValue,
		},
		ApprovalGates: ApprovalGates{
			RequireForGovernanceChanges:     &trueValue,
			RequireForDestructiveOperations: &trueValue,
			RequireForSystemProjectChanges:  &falseValue,
		},
		MergePolicy: MergePolicy{
			Mode:                       "squash",
			AllowDirectToDefaultBranch: &falseValue,
		},
		DestructiveOperations: DestructiveOperations{
			AllowReset:              &falseValue,
			AllowClean:              &falseValue,
			AllowForcePush:          &falseValue,
			RequireExplicitApproval: &trueValue,
		},
	}

	cfg := Config{
		Version: 1,
		Projects: []Manifest{
			{
				Key:           "local",
				Name:          "Local",
				ProjectClass:  ProjectClassLocalGit,
				GitRoot:       localGitRoot,
				DefaultBranch: "main",
				Policy:        validPolicy,
			},
			{
				Key:           "local",
				Name:          "Duplicate",
				ProjectClass:  ProjectClassLocalGit,
				GitRoot:       localGitRoot,
				DefaultBranch: "main",
				Policy:        validPolicy,
			},
			{
				Key:           "gh-missing",
				Name:          "GitHub Missing",
				ProjectClass:  ProjectClassGitHubBacked,
				GitRoot:       localGitRoot,
				DefaultBranch: "main",
				Policy:        validPolicy,
			},
			{
				Key:           "wrong-system",
				Name:          "Wrong System",
				ProjectClass:  ProjectClassSystem,
				SystemProject: true,
				GitRoot:       localGitRoot,
				DefaultBranch: "main",
				Policy:        validPolicy,
			},
			{
				Key:           "unsafe-core",
				Name:          "Unsafe Core",
				ProjectClass:  ProjectClassSystem,
				SystemProject: true,
				GitRoot:       localGitRoot,
				DefaultBranch: "main",
				Policy: Policy{
					AllowedCommands: []string{"status"},
					BranchRules: BranchRules{
						ProtectedBranches:          []string{"main"},
						RequireWorktree:            &trueValue,
						RequireTaskBranch:          &trueValue,
						AllowDefaultBranchMutation: &trueValue,
					},
					ApprovalGates: ApprovalGates{
						RequireForGovernanceChanges:     &trueValue,
						RequireForDestructiveOperations: &trueValue,
						RequireForSystemProjectChanges:  &trueValue,
					},
					MergePolicy: MergePolicy{
						Mode:                       "squash",
						AllowDirectToDefaultBranch: &falseValue,
					},
					DestructiveOperations: DestructiveOperations{
						AllowReset:              &falseValue,
						AllowClean:              &falseValue,
						AllowForcePush:          &falseValue,
						RequireExplicitApproval: &trueValue,
					},
				},
			},
			{
				Key:           "incomplete",
				Name:          "Incomplete Policy",
				ProjectClass:  ProjectClassLocalGit,
				GitRoot:       filepath.Join(root, "not-git"),
				DefaultBranch: "main",
				Policy: Policy{
					AllowedCommands: []string{"status"},
					BranchRules: BranchRules{
						ProtectedBranches: []string{"main"},
					},
					ApprovalGates: ApprovalGates{},
					MergePolicy: MergePolicy{
						Mode: "squash",
					},
					DestructiveOperations: DestructiveOperations{},
				},
			},
		},
	}

	diagnostics := Validate(cfg)
	if len(diagnostics) == 0 {
		t.Fatalf("expected diagnostics")
	}

	codes := make(map[string]bool, len(diagnostics))
	for _, diagnostic := range diagnostics {
		codes[diagnostic.Code] = true
	}

	for _, code := range []string{
		"duplicate_key",
		"missing_github_repo",
		"invalid_system_project_key",
		"unsafe_system_project_policy",
		"git_repository_required",
		"missing_policy_field",
	} {
		if !codes[code] {
			t.Fatalf("expected diagnostic code %q, got %#v", code, codes)
		}
	}
}

func TestValidateRejectsUnsafeLimitedActionPaths(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	localGitRoot := filepath.Join(root, "local")
	if err := os.MkdirAll(filepath.Join(localGitRoot, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir git root: %v", err)
	}

	trueValue := true
	falseValue := false

	basePolicy := Policy{
		AllowedCommands: []string{"status"},
		BranchRules: BranchRules{
			ProtectedBranches:          []string{"main"},
			RequireWorktree:            &trueValue,
			RequireTaskBranch:          &trueValue,
			AllowDefaultBranchMutation: &falseValue,
		},
		ApprovalGates: ApprovalGates{
			RequireForGovernanceChanges:     &trueValue,
			RequireForDestructiveOperations: &trueValue,
			RequireForSystemProjectChanges:  &falseValue,
		},
		MergePolicy: MergePolicy{
			Mode:                       "squash",
			AllowDirectToDefaultBranch: &falseValue,
		},
		DestructiveOperations: DestructiveOperations{
			AllowReset:              &falseValue,
			AllowClean:              &falseValue,
			AllowForcePush:          &falseValue,
			RequireExplicitApproval: &trueValue,
		},
	}

	tests := []struct {
		name    string
		key     string
		rule    LimitedActionRule
		wantSub string
	}{
		{
			name: "empty prefix",
			key:  "docs_audit_note",
			rule: LimitedActionRule{
				Description:  "unsafe",
				PathPrefixes: []string{""},
				ContentMode:  string(LimitedActionContentModeCreateMarkdownNote),
			},
			wantSub: "path_prefixes[0]",
		},
		{
			name: "absolute prefix",
			key:  "docs_audit_note",
			rule: LimitedActionRule{
				Description:  "unsafe",
				PathPrefixes: []string{"/docs/audits/"},
				ContentMode:  string(LimitedActionContentModeCreateMarkdownNote),
			},
			wantSub: "path_prefixes[0]",
		},
		{
			name: "traversing prefix",
			key:  "docs_audit_note",
			rule: LimitedActionRule{
				Description:  "unsafe",
				PathPrefixes: []string{"docs/../"},
				ContentMode:  string(LimitedActionContentModeCreateMarkdownNote),
			},
			wantSub: "path_prefixes[0]",
		},
		{
			name: "traversing target path",
			key:  "docs_update",
			rule: LimitedActionRule{
				Description:  "unsafe",
				PathPrefixes: []string{"docs/"},
				TargetPath:   "docs/../cmd/pwn.go",
				ContentMode:  string(LimitedActionContentModeAppendMarkdownNote),
			},
			wantSub: "target_path",
		},
		{
			name: "segment boundary mismatch",
			key:  "docs_update",
			rule: LimitedActionRule{
				Description:  "unsafe",
				PathPrefixes: []string{"docs"},
				TargetPath:   "docs-archive/note.md",
				ContentMode:  string(LimitedActionContentModeAppendMarkdownNote),
			},
			wantSub: "must be covered by path_prefixes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{
				Version: 1,
				Projects: []Manifest{
					{
						Key:           "alpha",
						Name:          "Alpha",
						ProjectClass:  ProjectClassLocalGit,
						GitRoot:       localGitRoot,
						DefaultBranch: "main",
						Policy: Policy{
							AllowedCommands:       basePolicy.AllowedCommands,
							LimitedActions:        map[string]LimitedActionRule{tt.key: tt.rule},
							BranchRules:           basePolicy.BranchRules,
							ApprovalGates:         basePolicy.ApprovalGates,
							MergePolicy:           basePolicy.MergePolicy,
							DestructiveOperations: basePolicy.DestructiveOperations,
						},
					},
				},
			}

			diagnostics := Validate(cfg)
			if len(diagnostics) == 0 {
				t.Fatalf("expected diagnostics")
			}

			found := false
			for _, diagnostic := range diagnostics {
				if diagnostic.Code == "invalid_limited_action" && strings.Contains(diagnostic.Message, tt.wantSub) {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("expected invalid_limited_action containing %q, got %#v", tt.wantSub, diagnostics)
			}
		})
	}
}

func TestValidateRejectsInvalidCutoverPilotProjects(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	for _, dir := range []string{
		filepath.Join(root, "pbs"),
		filepath.Join(root, "odin-core"),
	} {
		if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
			t.Fatalf("mkdir git root: %v", err)
		}
	}

	trueValue := true
	falseValue := false

	validPolicy := Policy{
		AllowedCommands: []string{"status"},
		BranchRules: BranchRules{
			ProtectedBranches:          []string{"main"},
			RequireWorktree:            &trueValue,
			RequireTaskBranch:          &trueValue,
			AllowDefaultBranchMutation: &falseValue,
		},
		ApprovalGates: ApprovalGates{
			RequireForGovernanceChanges:     &trueValue,
			RequireForDestructiveOperations: &trueValue,
			RequireForSystemProjectChanges:  &falseValue,
		},
		MergePolicy: MergePolicy{
			Mode:                       "squash",
			AllowDirectToDefaultBranch: &falseValue,
		},
		DestructiveOperations: DestructiveOperations{
			AllowReset:              &falseValue,
			AllowClean:              &falseValue,
			AllowForcePush:          &falseValue,
			RequireExplicitApproval: &trueValue,
		},
	}

	systemPolicy := validPolicy
	systemPolicy.ApprovalGates.RequireForSystemProjectChanges = &trueValue

	cfg := Config{
		Version: 1,
		Projects: []Manifest{
			{
				Key:           "pbs",
				Name:          "PBS",
				ProjectClass:  ProjectClassLocalGit,
				GitRoot:       filepath.Join(root, "pbs"),
				DefaultBranch: "main",
				Policy:        validPolicy,
			},
			{
				Key:           "odin-core",
				Name:          "Odin Core",
				ProjectClass:  ProjectClassSystem,
				SystemProject: true,
				GitRoot:       filepath.Join(root, "odin-core"),
				DefaultBranch: "main",
				Policy:        systemPolicy,
			},
		},
		Cutover: CutoverConfig{
			PilotProjects: []CutoverPilotProject{
				{Key: "", RuntimeOwner: "odin_os"},
				{Key: "pbs", RuntimeOwner: "odin_os"},
				{Key: "pbs", RuntimeOwner: "odin_os"},
				{Key: "ghost", RuntimeOwner: "odin_os"},
			},
		},
	}

	diagnostics := Validate(cfg)
	if len(diagnostics) == 0 {
		t.Fatalf("expected diagnostics")
	}

	codes := make(map[string]bool, len(diagnostics))
	for _, diagnostic := range diagnostics {
		codes[diagnostic.Code] = true
	}

	for _, code := range []string{
		"missing_field",
		"duplicate_cutover_pilot_key",
		"unknown_cutover_pilot_project",
	} {
		if !codes[code] {
			t.Fatalf("expected diagnostic code %q, got %#v", code, codes)
		}
	}
}

func TestValidateRejectsInvalidCutoverPilotStage(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	projectRoot := filepath.Join(root, "cfipros")
	if err := os.MkdirAll(filepath.Join(projectRoot, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir git root: %v", err)
	}

	trueValue := true
	falseValue := false

	cfg := Config{
		Version: 1,
		Projects: []Manifest{
			{
				Key:           "cfipros",
				Name:          "CFIPros",
				ProjectClass:  ProjectClassGitHubBacked,
				GitRoot:       projectRoot,
				DefaultBranch: "main",
				GitHub: GitHub{
					Repo: "marcusgoll/cfipros",
				},
				Policy: Policy{
					AllowedCommands: []string{"status", "test", "build"},
					BranchRules: BranchRules{
						ProtectedBranches:          []string{"main"},
						RequireWorktree:            &trueValue,
						RequireTaskBranch:          &trueValue,
						AllowDefaultBranchMutation: &falseValue,
					},
					ApprovalGates: ApprovalGates{
						RequireForGovernanceChanges:     &trueValue,
						RequireForDestructiveOperations: &trueValue,
						RequireForSystemProjectChanges:  &falseValue,
					},
					MergePolicy: MergePolicy{
						Mode:                       "squash",
						AllowDirectToDefaultBranch: &falseValue,
					},
					DestructiveOperations: DestructiveOperations{
						AllowReset:              &falseValue,
						AllowClean:              &falseValue,
						AllowForcePush:          &falseValue,
						RequireExplicitApproval: &trueValue,
					},
				},
			},
		},
		Cutover: CutoverConfig{
			PilotProjects: []CutoverPilotProject{
				{
					Key:          "cfipros",
					RuntimeOwner: "legacy_odin",
					Stage:        "shdaow",
				},
			},
		},
	}

	diagnostics := Validate(cfg)
	if len(diagnostics) == 0 {
		t.Fatal("expected diagnostics")
	}

	found := false
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == "invalid_cutover_pilot_stage" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected invalid_cutover_pilot_stage diagnostic, got %#v", diagnostics)
	}
}
