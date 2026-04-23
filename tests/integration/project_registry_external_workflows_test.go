package integration_test

import (
	"context"
	"path/filepath"
	"slices"
	"testing"

	"odin-os/internal/app/bootstrap"
	"odin-os/internal/core/projects"
)

func TestRegistryIncludesN8NTargetProjects(t *testing.T) {
	ctx := context.Background()
	repoRoot := projectRoot(t)
	runtimeRoot := t.TempDir()

	app, err := bootstrap.Load(ctx, repoRoot, runtimeRoot)
	if err != nil {
		t.Fatalf("bootstrap.Load() error = %v", err)
	}
	defer app.Store.Close()

	pbsPilot, ok := app.Registry.CutoverPilotProject("pbs")
	if !ok {
		t.Fatal("expected pbs cutover pilot metadata")
	}
	if pbsPilot.RuntimeOwner != "odin_os" {
		t.Fatalf("pbs runtime owner = %q, want odin_os", pbsPilot.RuntimeOwner)
	}
	if pbsPilot.PrimaryController != "odin_os" {
		t.Fatalf("pbs primary controller = %q, want odin_os", pbsPilot.PrimaryController)
	}
	if pbsPilot.Stage != projects.TransitionStateCutover {
		t.Fatalf("pbs stage = %q, want cutover", pbsPilot.Stage)
	}

	for _, tc := range []struct {
		key               string
		gitRoot           string
		defaultBranch     string
		githubRepo        string
		runtimeOwner      string
		primaryController string
		transitionState   projects.TransitionState
	}{
		{
			key:               "cfipros",
			gitRoot:           "/home/orchestrator/cfipros",
			defaultBranch:     "main",
			githubRepo:        "marcusgoll/cfipros",
			runtimeOwner:      "legacy_odin",
			primaryController: "legacy_odin",
			transitionState:   projects.TransitionStateShadow,
		},
		{
			key:               "marcusgoll",
			gitRoot:           "/home/orchestrator/marcusgoll",
			defaultBranch:     "main",
			githubRepo:        "marcusgoll/marcusgoll",
			runtimeOwner:      "legacy_odin",
			primaryController: "legacy_odin",
			transitionState:   projects.TransitionStateShadow,
		},
	} {
		manifest, ok := app.Registry.Lookup(tc.key)
		if !ok {
			t.Fatalf("expected %s project in registry", tc.key)
		}
		if manifest.GitRoot != tc.gitRoot {
			t.Fatalf("%s git root = %q, want %q", tc.key, manifest.GitRoot, tc.gitRoot)
		}
		if manifest.DefaultBranch != tc.defaultBranch {
			t.Fatalf("%s default branch = %q, want %q", tc.key, manifest.DefaultBranch, tc.defaultBranch)
		}
		if manifest.GitHub.Repo != tc.githubRepo {
			t.Fatalf("%s github repo = %q, want %q", tc.key, manifest.GitHub.Repo, tc.githubRepo)
		}
		if !slices.Equal(manifest.Policy.AllowedCommands, []string{"status", "test", "build"}) {
			t.Fatalf("%s allowed commands = %v, want pbs baseline", tc.key, manifest.Policy.AllowedCommands)
		}
		if manifest.Policy.BranchRules.RequireWorktree == nil || !*manifest.Policy.BranchRules.RequireWorktree {
			t.Fatalf("%s require_worktree = %v, want true", tc.key, manifest.Policy.BranchRules.RequireWorktree)
		}
		if manifest.Policy.BranchRules.RequireTaskBranch == nil || !*manifest.Policy.BranchRules.RequireTaskBranch {
			t.Fatalf("%s require_task_branch = %v, want true", tc.key, manifest.Policy.BranchRules.RequireTaskBranch)
		}
		if manifest.Policy.BranchRules.AllowDefaultBranchMutation == nil || *manifest.Policy.BranchRules.AllowDefaultBranchMutation {
			t.Fatalf("%s allow_default_branch_mutation = %v, want false", tc.key, manifest.Policy.BranchRules.AllowDefaultBranchMutation)
		}
		if manifest.Policy.MergePolicy.AllowDirectToDefaultBranch == nil || *manifest.Policy.MergePolicy.AllowDirectToDefaultBranch {
			t.Fatalf("%s allow_direct_to_default_branch = %v, want false", tc.key, manifest.Policy.MergePolicy.AllowDirectToDefaultBranch)
		}
		if manifest.Policy.ApprovalGates.RequireForGovernanceChanges == nil || !*manifest.Policy.ApprovalGates.RequireForGovernanceChanges {
			t.Fatalf("%s require_for_governance_changes = %v, want true", tc.key, manifest.Policy.ApprovalGates.RequireForGovernanceChanges)
		}
		if manifest.Policy.ApprovalGates.RequireForDestructiveOperations == nil || !*manifest.Policy.ApprovalGates.RequireForDestructiveOperations {
			t.Fatalf("%s require_for_destructive_operations = %v, want true", tc.key, manifest.Policy.ApprovalGates.RequireForDestructiveOperations)
		}
		if manifest.Policy.ApprovalGates.RequireForSystemProjectChanges == nil || *manifest.Policy.ApprovalGates.RequireForSystemProjectChanges {
			t.Fatalf("%s require_for_system_project_changes = %v, want false", tc.key, manifest.Policy.ApprovalGates.RequireForSystemProjectChanges)
		}
		if manifest.Policy.DestructiveOperations.AllowReset == nil || *manifest.Policy.DestructiveOperations.AllowReset {
			t.Fatalf("%s allow_reset = %v, want false", tc.key, manifest.Policy.DestructiveOperations.AllowReset)
		}
		if manifest.Policy.DestructiveOperations.AllowClean == nil || *manifest.Policy.DestructiveOperations.AllowClean {
			t.Fatalf("%s allow_clean = %v, want false", tc.key, manifest.Policy.DestructiveOperations.AllowClean)
		}
		if manifest.Policy.DestructiveOperations.AllowForcePush == nil || *manifest.Policy.DestructiveOperations.AllowForcePush {
			t.Fatalf("%s allow_force_push = %v, want false", tc.key, manifest.Policy.DestructiveOperations.AllowForcePush)
		}
		if len(manifest.Policy.LimitedActions) != 0 {
			t.Fatalf("%s limited actions = %d, want none for shadow onboarding", tc.key, len(manifest.Policy.LimitedActions))
		}

		pilot, ok := app.Registry.CutoverPilotProject(tc.key)
		if !ok {
			t.Fatalf("expected %s cutover pilot metadata", tc.key)
		}
		if pilot.RuntimeOwner != tc.runtimeOwner {
			t.Fatalf("%s runtime owner = %q, want %q", tc.key, pilot.RuntimeOwner, tc.runtimeOwner)
		}
		if pilot.PrimaryController != tc.primaryController {
			t.Fatalf("%s primary controller = %q, want %q", tc.key, pilot.PrimaryController, tc.primaryController)
		}
		if pilot.Stage != tc.transitionState {
			t.Fatalf("%s stage = %q, want %q", tc.key, pilot.Stage, tc.transitionState)
		}
		if !pilot.LegacyPrimaryRequired {
			t.Fatalf("%s legacy_primary_required = false, want true for shadow onboarding", tc.key)
		}
	}

	assertFileContains(t, filepath.Join(repoRoot, "docs/operations/project-overlays-cfipros-marcusgoll.md"), []string{
		"cfipros",
		"marcusgoll",
		"shadow",
		"limited_action",
		"cutover",
		"legacy_odin",
		"odin_os",
	})
}
