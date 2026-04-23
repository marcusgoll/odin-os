package projects

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const odinCoreKey = "odin-core"

type Diagnostic struct {
	Path       string
	ProjectKey string
	Code       string
	Message    string
}

func Validate(cfg Config) []Diagnostic {
	diagnostics := make([]Diagnostic, 0)
	seenKeys := make(map[string]struct{}, len(cfg.Projects))
	registeredKeys := make(map[string]struct{}, len(cfg.Projects))

	if cfg.Version <= 0 {
		diagnostics = append(diagnostics, Diagnostic{
			Code:    "invalid_version",
			Message: "manifest version must be greater than zero",
		})
	}

	for _, project := range cfg.Projects {
		if project.Key != "" {
			registeredKeys[project.Key] = struct{}{}
		}
		diagnostics = append(diagnostics, validateProject(project, seenKeys)...)
	}

	diagnostics = append(diagnostics, validateCutover(cfg, registeredKeys)...)

	return diagnostics
}

func validateCutover(cfg Config, registeredKeys map[string]struct{}) []Diagnostic {
	if len(cfg.Cutover.PilotProjects) == 0 {
		return nil
	}

	diagnostics := make([]Diagnostic, 0)
	seenPilotKeys := make(map[string]struct{}, len(cfg.Cutover.PilotProjects))

	for _, pilot := range cfg.Cutover.PilotProjects {
		if pilot.Key == "" {
			diagnostics = append(diagnostics, Diagnostic{
				Code:    "missing_field",
				Message: "cutover.pilot_projects[].key is required",
			})
			continue
		}
		if _, exists := seenPilotKeys[pilot.Key]; exists {
			diagnostics = append(diagnostics, Diagnostic{
				ProjectKey: pilot.Key,
				Code:       "duplicate_cutover_pilot_key",
				Message:    fmt.Sprintf("cutover pilot key %q is duplicated", pilot.Key),
			})
			continue
		}
		seenPilotKeys[pilot.Key] = struct{}{}

		if _, exists := registeredKeys[pilot.Key]; !exists {
			diagnostics = append(diagnostics, Diagnostic{
				ProjectKey: pilot.Key,
				Code:       "unknown_cutover_pilot_project",
				Message:    fmt.Sprintf("cutover pilot key %q must match a registered project in the same manifest", pilot.Key),
			})
		}
		if pilot.Stage != "" && !isKnownCutoverPilotStage(pilot.Stage) {
			diagnostics = append(diagnostics, Diagnostic{
				ProjectKey: pilot.Key,
				Code:       "invalid_cutover_pilot_stage",
				Message:    fmt.Sprintf("cutover pilot %q has unsupported stage %q", pilot.Key, pilot.Stage),
			})
		}
	}

	return diagnostics
}

func isKnownCutoverPilotStage(stage TransitionState) bool {
	switch stage {
	case TransitionStateInventory,
		TransitionStateShadow,
		TransitionStateCompare,
		TransitionStateLimitedAction,
		TransitionStateCutover,
		TransitionStateDecommissioned:
		return true
	default:
		return false
	}
}

func validateProject(project Manifest, seenKeys map[string]struct{}) []Diagnostic {
	diagnostics := make([]Diagnostic, 0)

	addDiagnostic := func(code string, format string, args ...any) {
		diagnostics = append(diagnostics, Diagnostic{
			Path:       project.SourcePath,
			ProjectKey: project.Key,
			Code:       code,
			Message:    fmt.Sprintf(format, args...),
		})
	}

	if project.Key == "" {
		addDiagnostic("missing_field", "project key is required")
	} else {
		if _, exists := seenKeys[project.Key]; exists {
			addDiagnostic("duplicate_key", "project key %q is duplicated", project.Key)
		}
		seenKeys[project.Key] = struct{}{}
	}

	if project.Name == "" {
		addDiagnostic("missing_field", "project name is required")
	}
	if project.GitRoot == "" || !isGitRepository(project.GitRoot) {
		addDiagnostic("git_repository_required", "project %q must point at a Git repository", project.Key)
	}
	if project.DefaultBranch == "" {
		addDiagnostic("missing_field", "default branch is required for %q", project.Key)
	}

	switch project.ProjectClass {
	case ProjectClassLocalGit, ProjectClassGitHubBacked, ProjectClassSystem:
	default:
		addDiagnostic("invalid_project_class", "project %q has unsupported class %q", project.Key, project.ProjectClass)
	}

	if len(project.Policy.AllowedCommands) == 0 {
		addDiagnostic("missing_field", "policy.allowed_commands is required for %q", project.Key)
	}

	diagnostics = append(diagnostics, validatePolicy(project, addDiagnostic)...)

	if project.ProjectClass == ProjectClassGitHubBacked && project.GitHub.Repo == "" {
		addDiagnostic("missing_github_repo", "github_backed_project %q requires github.repo", project.Key)
	}

	if project.ProjectClass == ProjectClassSystem || project.SystemProject {
		if project.Key != odinCoreKey {
			addDiagnostic("invalid_system_project_key", "system project must use key %q", odinCoreKey)
		}
		if project.ProjectClass != ProjectClassSystem || !project.SystemProject {
			addDiagnostic("invalid_system_project_definition", "system project %q must set project_class=system_project and system_project=true", project.Key)
		}
		if project.Policy.BranchRules.AllowDefaultBranchMutation == nil || *project.Policy.BranchRules.AllowDefaultBranchMutation {
			addDiagnostic("unsafe_system_project_policy", "system project %q cannot allow default branch mutation", project.Key)
		}
		if project.Policy.BranchRules.RequireWorktree == nil || !*project.Policy.BranchRules.RequireWorktree {
			addDiagnostic("unsafe_system_project_policy", "system project %q must require worktrees", project.Key)
		}
		if project.Policy.BranchRules.RequireTaskBranch == nil || !*project.Policy.BranchRules.RequireTaskBranch {
			addDiagnostic("unsafe_system_project_policy", "system project %q must require task-owned branches", project.Key)
		}
		if project.Policy.ApprovalGates.RequireForSystemProjectChanges == nil || !*project.Policy.ApprovalGates.RequireForSystemProjectChanges {
			addDiagnostic("unsafe_system_project_policy", "system project %q must require approval for system changes", project.Key)
		}
		if project.Policy.MergePolicy.AllowDirectToDefaultBranch == nil || *project.Policy.MergePolicy.AllowDirectToDefaultBranch {
			addDiagnostic("unsafe_system_project_policy", "system project %q cannot allow direct merges to the default branch", project.Key)
		}
	}

	return diagnostics
}

func validatePolicy(project Manifest, addDiagnostic func(code string, format string, args ...any)) []Diagnostic {
	knownLimitedActions := KnownLimitedActionKeys()
	for key, rule := range project.Policy.LimitedActions {
		if key == "" {
			addDiagnostic("invalid_limited_action", "policy.limited_actions contains an empty key for %q", project.Key)
			continue
		}
		if _, ok := knownLimitedActions[key]; !ok {
			addDiagnostic("invalid_limited_action", "policy.limited_actions.%s is not a known bounded action for %q", key, project.Key)
		}
		if rule.Description == "" {
			addDiagnostic("invalid_limited_action", "policy.limited_actions.%s.description is required for %q", key, project.Key)
		}
		if len(rule.PathPrefixes) == 0 {
			addDiagnostic("invalid_limited_action", "policy.limited_actions.%s.path_prefixes is required for %q", key, project.Key)
		}
		normalizedPrefixes := make([]string, 0, len(rule.PathPrefixes))
		for index, prefix := range rule.PathPrefixes {
			normalizedPrefix, err := normalizeRepoRelativePath(prefix)
			if err != nil {
				addDiagnostic("invalid_limited_action", "policy.limited_actions.%s.path_prefixes[%d] %q is invalid for %q: %v", key, index, prefix, project.Key, err)
				continue
			}
			normalizedPrefixes = append(normalizedPrefixes, normalizedPrefix)
		}
		if rule.ContentMode == "" {
			addDiagnostic("invalid_limited_action", "policy.limited_actions.%s.content_mode is required for %q", key, project.Key)
		}

		normalizedTarget := ""
		if rule.TargetPath != "" {
			targetPath, err := normalizeRepoRelativePath(rule.TargetPath)
			if err != nil {
				addDiagnostic("invalid_limited_action", "policy.limited_actions.%s.target_path %q is invalid for %q: %v", key, rule.TargetPath, project.Key, err)
			} else {
				normalizedTarget = targetPath
			}
		}

		switch key {
		case string(LimitedActionDocsAuditNote), string(LimitedActionRepoHygieneNote):
			if rule.ContentMode != string(LimitedActionContentModeCreateMarkdownNote) {
				addDiagnostic("invalid_limited_action", "policy.limited_actions.%s.content_mode must be %q for %q", key, LimitedActionContentModeCreateMarkdownNote, project.Key)
			}
		case string(LimitedActionDocsUpdate):
			if rule.ContentMode != string(LimitedActionContentModeAppendMarkdownNote) {
				addDiagnostic("invalid_limited_action", "policy.limited_actions.%s.content_mode must be %q for %q", key, LimitedActionContentModeAppendMarkdownNote, project.Key)
			}
			if rule.TargetPath == "" {
				addDiagnostic("invalid_limited_action", "policy.limited_actions.%s.target_path is required for %q", key, project.Key)
			}
		}

		if normalizedTarget != "" && !pathAllowedByPrefixes(normalizedTarget, normalizedPrefixes) {
			addDiagnostic("invalid_limited_action", "policy.limited_actions.%s.target_path %q must be covered by path_prefixes for %q", key, rule.TargetPath, project.Key)
		}
	}

	if project.Policy.BranchRules.RequireWorktree == nil {
		addDiagnostic("missing_policy_field", "policy.branch_rules.require_worktree is required for %q", project.Key)
	}
	if project.Policy.BranchRules.RequireTaskBranch == nil {
		addDiagnostic("missing_policy_field", "policy.branch_rules.require_task_branch is required for %q", project.Key)
	}
	if project.Policy.BranchRules.AllowDefaultBranchMutation == nil {
		addDiagnostic("missing_policy_field", "policy.branch_rules.allow_default_branch_mutation is required for %q", project.Key)
	}
	if project.Policy.ApprovalGates.RequireForGovernanceChanges == nil {
		addDiagnostic("missing_policy_field", "policy.approval_gates.require_for_governance_changes is required for %q", project.Key)
	}
	if project.Policy.ApprovalGates.RequireForDestructiveOperations == nil {
		addDiagnostic("missing_policy_field", "policy.approval_gates.require_for_destructive_operations is required for %q", project.Key)
	}
	if project.Policy.ApprovalGates.RequireForSystemProjectChanges == nil {
		addDiagnostic("missing_policy_field", "policy.approval_gates.require_for_system_project_changes is required for %q", project.Key)
	}
	if project.Policy.MergePolicy.Mode == "" {
		addDiagnostic("missing_policy_field", "policy.merge_policy.mode is required for %q", project.Key)
	}
	if project.Policy.MergePolicy.AllowDirectToDefaultBranch == nil {
		addDiagnostic("missing_policy_field", "policy.merge_policy.allow_direct_to_default_branch is required for %q", project.Key)
	}
	if project.Policy.DestructiveOperations.AllowReset == nil {
		addDiagnostic("missing_policy_field", "policy.destructive_operations.allow_reset is required for %q", project.Key)
	}
	if project.Policy.DestructiveOperations.AllowClean == nil {
		addDiagnostic("missing_policy_field", "policy.destructive_operations.allow_clean is required for %q", project.Key)
	}
	if project.Policy.DestructiveOperations.AllowForcePush == nil {
		addDiagnostic("missing_policy_field", "policy.destructive_operations.allow_force_push is required for %q", project.Key)
	}
	if project.Policy.DestructiveOperations.RequireExplicitApproval == nil {
		addDiagnostic("missing_policy_field", "policy.destructive_operations.require_explicit_approval is required for %q", project.Key)
	} else if anyDestructiveOperationAllowed(project.Policy.DestructiveOperations) && !*project.Policy.DestructiveOperations.RequireExplicitApproval {
		addDiagnostic("unsafe_destructive_policy", "destructive operations for %q require explicit approval", project.Key)
	}

	return nil
}

func anyDestructiveOperationAllowed(rules DestructiveOperations) bool {
	return boolValue(rules.AllowReset) || boolValue(rules.AllowClean) || boolValue(rules.AllowForcePush)
}

func boolValue(value *bool) bool {
	return value != nil && *value
}

func isGitRepository(root string) bool {
	info, err := os.Stat(filepath.Join(root, ".git"))
	if err != nil {
		return false
	}
	return info.Mode().IsRegular() || info.IsDir()
}

func pathAllowedByPrefixes(target string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if target == prefix || strings.HasPrefix(target, prefix+"/") {
			return true
		}
	}
	return false
}

func normalizeRepoRelativePath(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("must not be empty")
	}
	if strings.Contains(raw, "\\") {
		return "", fmt.Errorf("must use forward slashes")
	}
	if path.IsAbs(raw) {
		return "", fmt.Errorf("must be repo-relative")
	}
	for _, segment := range strings.Split(raw, "/") {
		if segment == ".." {
			return "", fmt.Errorf("must not contain parent-directory traversal")
		}
	}

	cleaned := path.Clean(raw)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("must resolve inside the repository")
	}
	return cleaned, nil
}
