package recovery

import (
	"encoding/json"
	"fmt"
	"strings"
)

type FailureCategory string

const (
	FailureUnclearTicket                FailureCategory = "unclear_ticket"
	FailureMissingAcceptanceCriteria    FailureCategory = "missing_acceptance_criteria"
	FailureExistingCodeBehaviorUnknown  FailureCategory = "existing_code_behavior_unknown"
	FailureCharacterizationTestMissing  FailureCategory = "characterization_test_missing"
	FailureTestFailure                  FailureCategory = "test_failure"
	FailureMigrationConflict            FailureCategory = "migration_conflict"
	FailureDependencyIssue              FailureCategory = "dependency_issue"
	FailurePermissionIssue              FailureCategory = "permission_issue"
	FailureCodexTimeout                 FailureCategory = "codex_timeout"
	FailureBadPrompt                    FailureCategory = "bad_prompt"
	FailureBadSkillSelection            FailureCategory = "bad_skill_selection"
	FailureConflictingAgentInstructions FailureCategory = "conflicting_agent_instructions"
	FailureUnsafeShimBehavior           FailureCategory = "unsafe_shim_behavior"
	FailureSecurityBlocker              FailureCategory = "security_blocker"
	FailureMergeConflict                FailureCategory = "merge_conflict"
	FailureWorkspaceFailure             FailureCategory = "workspace_failure"
	FailureGitHubAPIFailure             FailureCategory = "github_api_failure"
	FailureDashboardAdminFailure        FailureCategory = "dashboard_admin_failure"
	FailureDeploymentFailure            FailureCategory = "deployment_failure"
	FailureUnknown                      FailureCategory = "unknown"
)

type NextStepTarget string

const (
	NextStepPrompt         NextStepTarget = "prompt"
	NextStepSkill          NextStepTarget = "skill"
	NextStepTest           NextStepTarget = "test"
	NextStepShim           NextStepTarget = "shim"
	NextStepWorkflow       NextStepTarget = "workflow"
	NextStepArchitecture   NextStepTarget = "architecture"
	NextStepImplementation NextStepTarget = "implementation"
	NextStepOperator       NextStepTarget = "operator"
)

type FailureInput struct {
	Step                        string
	TicketTitle                 string
	AcceptanceCriteria          []string
	AcceptanceCriteriaRequired  bool
	ExistingBehaviorKnown       bool
	CharacterizationTestPresent bool
	ErrorText                   string
	Summary                     string
	RetryCount                  int
	MaxAttempts                 int
}

type FailureAnalysis struct {
	Category                FailureCategory        `json:"category"`
	Step                    string                 `json:"step,omitempty"`
	Summary                 string                 `json:"summary"`
	SuggestedFix            string                 `json:"suggested_fix"`
	NextStepTarget          NextStepTarget         `json:"next_step_target"`
	FollowUp                FollowUpRecommendation `json:"follow_up"`
	RetryRecommended        bool                   `json:"retry_recommended"`
	MaxAttemptsReached      bool                   `json:"max_attempts_reached"`
	AutoApplyWorkflowChange bool                   `json:"auto_apply_workflow_change"`
}

type FollowUpRecommendation struct {
	Recommended bool     `json:"recommended"`
	Title       string   `json:"title,omitempty"`
	Labels      []string `json:"labels,omitempty"`
	Reason      string   `json:"reason,omitempty"`
}

func AnalyzeFailure(input FailureInput) FailureAnalysis {
	normalized := normalizeFailureInput(input)
	category := classifyFailure(normalized)
	target := nextStepTargetFor(category)
	summary := failureSummary(category, normalized)
	maxAttemptsReached := retryBudgetExhausted(normalized.RetryCount, normalized.MaxAttempts)

	return FailureAnalysis{
		Category:                category,
		Step:                    normalized.Step,
		Summary:                 summary,
		SuggestedFix:            suggestedFixFor(category),
		NextStepTarget:          target,
		FollowUp:                followUpFor(category, normalized),
		RetryRecommended:        retryRecommendedFor(category) && !maxAttemptsReached,
		MaxAttemptsReached:      maxAttemptsReached,
		AutoApplyWorkflowChange: false,
	}
}

func MarshalFailureAnalysisArtifact(analysis FailureAnalysis) (string, error) {
	payload := struct {
		FailureAnalysis FailureAnalysis `json:"failure_analysis"`
	}{
		FailureAnalysis: analysis,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func normalizeFailureInput(input FailureInput) FailureInput {
	input.Step = strings.TrimSpace(input.Step)
	input.TicketTitle = strings.TrimSpace(input.TicketTitle)
	input.ErrorText = strings.TrimSpace(input.ErrorText)
	input.Summary = strings.TrimSpace(input.Summary)
	criteria := input.AcceptanceCriteria[:0]
	for _, criterion := range input.AcceptanceCriteria {
		if trimmed := strings.TrimSpace(criterion); trimmed != "" {
			criteria = append(criteria, trimmed)
		}
	}
	input.AcceptanceCriteria = criteria
	return input
}

func classifyFailure(input FailureInput) FailureCategory {
	text := strings.ToLower(strings.Join([]string{input.Step, input.TicketTitle, input.Summary, input.ErrorText}, "\n"))

	if input.TicketTitle == "" || containsAny(text, "unclear ticket", "ambiguous ticket", "needs clarification", "unclear task") {
		return FailureUnclearTicket
	}
	if input.AcceptanceCriteriaRequired && len(input.AcceptanceCriteria) == 0 {
		return FailureMissingAcceptanceCriteria
	}
	if containsAny(text, "missing acceptance criteria", "acceptance criteria required") {
		return FailureMissingAcceptanceCriteria
	}
	if !input.ExistingBehaviorKnown || containsAny(text, "existing code behavior unknown", "behavior unknown", "explore existing implementation first") {
		return FailureExistingCodeBehaviorUnknown
	}
	if containsAny(text, "characterization test missing", "add characterization test", "missing characterization") {
		return FailureCharacterizationTestMissing
	}
	if containsAny(text, "security blocker", "danger-full-access", "secret exposed", "token leaked") {
		return FailureSecurityBlocker
	}
	if containsAny(text, "unsafe shim", "bash -c", "shell injection") {
		return FailureUnsafeShimBehavior
	}
	if containsAny(text, "conflicting agent instructions", "instruction conflict", "conflicting instructions") {
		return FailureConflictingAgentInstructions
	}
	if containsAny(text, "bad skill selection", "wrong skill", "skill mismatch") {
		return FailureBadSkillSelection
	}
	if containsAny(text, "bad prompt", "prompt render", "prompt rendering", "prompt too large") {
		return FailureBadPrompt
	}
	if containsAny(text, "merge conflict", "conflict (content)", "automatic merge failed") {
		return FailureMergeConflict
	}
	if containsAny(text, "github api", "gh api", "rate limit", "403 forbidden", "404 not found") {
		return FailureGitHubAPIFailure
	}
	if containsAny(text, "dashboard", "admin token", "admin endpoint", "health endpoint") {
		return FailureDashboardAdminFailure
	}
	if containsAny(text, "deployment", "deploy", "systemd", "docker compose", "docker", "service failed") {
		return FailureDeploymentFailure
	}
	if containsAny(text, "worktree", "workspace", "path traversal", "dirty worktree") {
		return FailureWorkspaceFailure
	}
	if containsAny(text, "migration conflict", "duplicate column", "schema migration", "migration failed") {
		return FailureMigrationConflict
	}
	if containsAny(text, "permission denied", "unauthorized", "forbidden", "eacces") {
		return FailurePermissionIssue
	}
	if containsAny(text, "context deadline exceeded", "codex timeout", "timeout", "timed out") {
		return FailureCodexTimeout
	}
	if containsAny(text, "dependency", "module not found", "no required module", "missing module", "package not found") {
		return FailureDependencyIssue
	}
	if containsAny(text, "go test", "test failed", "failing test", " fail\t", "\nfail", "assertion failed") {
		return FailureTestFailure
	}
	return FailureUnknown
}

func nextStepTargetFor(category FailureCategory) NextStepTarget {
	switch category {
	case FailureUnclearTicket, FailureMissingAcceptanceCriteria, FailureBadPrompt:
		return NextStepPrompt
	case FailureExistingCodeBehaviorUnknown, FailureCharacterizationTestMissing:
		return NextStepTest
	case FailureTestFailure:
		return NextStepImplementation
	case FailureBadSkillSelection, FailureConflictingAgentInstructions:
		return NextStepSkill
	case FailureUnsafeShimBehavior:
		return NextStepShim
	case FailureMigrationConflict, FailureMergeConflict:
		return NextStepArchitecture
	case FailureDependencyIssue, FailurePermissionIssue, FailureCodexTimeout, FailureWorkspaceFailure, FailureGitHubAPIFailure, FailureDashboardAdminFailure, FailureDeploymentFailure:
		return NextStepWorkflow
	case FailureSecurityBlocker:
		return NextStepArchitecture
	default:
		return NextStepImplementation
	}
}

func suggestedFixFor(category FailureCategory) string {
	switch category {
	case FailureUnclearTicket:
		return "Clarify the ticket scope, expected behavior, and stop conditions before dispatching another worker."
	case FailureMissingAcceptanceCriteria:
		return "Add concrete acceptance criteria before dispatch; do not treat the failure as an implementation bug yet."
	case FailureExistingCodeBehaviorUnknown:
		return "Audit existing implementation behavior and document current, partial, and missing behavior before editing."
	case FailureCharacterizationTestMissing:
		return "Add characterization coverage around the risky current behavior before refactoring."
	case FailureTestFailure:
		return "Inspect the failing test output, reproduce locally, then fix the implementation or update the test only with a documented behavior change."
	case FailureMigrationConflict:
		return "Stop migration work, inspect the conflicting schema or asset state, and write a narrow migration repair plan."
	case FailureDependencyIssue:
		return "Verify dependency availability, versions, and module configuration before retrying."
	case FailurePermissionIssue:
		return "Check token scope, filesystem permissions, and operator authorization before retrying."
	case FailureCodexTimeout:
		return "Reduce prompt scope, preserve logs, and retry only within the configured attempt budget."
	case FailureBadPrompt:
		return "Update the prompt template or rendered prompt inputs; do not patch workflow behavior automatically."
	case FailureBadSkillSelection:
		return "Select a more specific skill or update skill routing guidance after review."
	case FailureConflictingAgentInstructions:
		return "Resolve instruction precedence in repo guidance before dispatching another worker."
	case FailureUnsafeShimBehavior:
		return "Stop using the shim for untrusted input and convert it to explicit typed arguments or a reviewed adapter."
	case FailureSecurityBlocker:
		return "Create a security follow-up and require human review before continuing the blocked path."
	case FailureMergeConflict:
		return "Rebase or merge current main in an isolated worktree and resolve the conflict explicitly."
	case FailureWorkspaceFailure:
		return "Inspect workspace root, lease state, branch naming, and dirty worktree status before retrying."
	case FailureGitHubAPIFailure:
		return "Check GitHub API response, token scope, rate limits, and dry-run settings before retrying."
	case FailureDashboardAdminFailure:
		return "Check admin authentication, endpoint exposure, and dashboard runtime state before retrying."
	case FailureDeploymentFailure:
		return "Stop deployment, preserve logs, verify rollback, and create an operator-reviewed deployment fix."
	default:
		return "Preserve logs and classify manually before retrying."
	}
}

func failureSummary(category FailureCategory, input FailureInput) string {
	if input.Summary != "" {
		return input.Summary
	}
	if input.ErrorText != "" {
		return trimForSummary(input.ErrorText)
	}
	return fmt.Sprintf("failure classified as %s", category)
}

func followUpFor(category FailureCategory, input FailureInput) FollowUpRecommendation {
	if category == FailureUnknown {
		return FollowUpRecommendation{
			Recommended: true,
			Title:       followUpTitle("Classify unknown Odin failure", input),
			Labels:      []string{"odin:needs-plan", "agent:qa", "type:safety"},
			Reason:      "unknown failures need explicit triage before retry",
		}
	}
	if category == FailureCodexTimeout && !retryBudgetExhausted(input.RetryCount, input.MaxAttempts) {
		return FollowUpRecommendation{}
	}
	labels := []string{"odin:ready", agentLabelFor(category), typeLabelFor(category)}
	return FollowUpRecommendation{
		Recommended: true,
		Title:       followUpTitle(titlePrefixFor(category), input),
		Labels:      labels,
		Reason:      fmt.Sprintf("%s requires an explicit follow-up or operator decision", category),
	}
}

func followUpTitle(prefix string, input FailureInput) string {
	subject := input.TicketTitle
	if subject == "" {
		subject = input.Step
	}
	if subject == "" {
		return prefix
	}
	return fmt.Sprintf("%s: %s", prefix, subject)
}

func titlePrefixFor(category FailureCategory) string {
	switch category {
	case FailureMissingAcceptanceCriteria, FailureUnclearTicket:
		return "Clarify failed Odin ticket"
	case FailureExistingCodeBehaviorUnknown, FailureCharacterizationTestMissing:
		return "Add brownfield characterization"
	case FailureSecurityBlocker, FailureUnsafeShimBehavior:
		return "Resolve Odin security blocker"
	case FailureDeploymentFailure:
		return "Repair Odin deployment failure"
	case FailureMergeConflict:
		return "Resolve Odin merge conflict"
	default:
		return "Investigate Odin failure"
	}
}

func agentLabelFor(category FailureCategory) string {
	switch category {
	case FailureSecurityBlocker, FailureUnsafeShimBehavior, FailurePermissionIssue:
		return "agent:security"
	case FailureDeploymentFailure, FailureDependencyIssue:
		return "agent:devops"
	case FailureUnclearTicket, FailureMissingAcceptanceCriteria, FailureMigrationConflict, FailureMergeConflict, FailureConflictingAgentInstructions:
		return "agent:architect"
	case FailureTestFailure, FailureCharacterizationTestMissing:
		return "agent:qa"
	default:
		return "agent:go-orchestrator"
	}
}

func typeLabelFor(category FailureCategory) string {
	switch category {
	case FailureSecurityBlocker, FailureUnsafeShimBehavior, FailurePermissionIssue, FailureCodexTimeout:
		return "type:safety"
	case FailureUnclearTicket, FailureMissingAcceptanceCriteria, FailureMigrationConflict, FailureMergeConflict, FailureConflictingAgentInstructions:
		return "type:refactor"
	case FailureDashboardAdminFailure:
		return "type:operator-surface"
	default:
		return "type:cleanup"
	}
}

func retryRecommendedFor(category FailureCategory) bool {
	switch category {
	case FailureCodexTimeout, FailureDependencyIssue, FailureGitHubAPIFailure:
		return true
	default:
		return false
	}
}

func retryBudgetExhausted(retryCount int, maxAttempts int) bool {
	return maxAttempts > 0 && retryCount+1 >= maxAttempts
}

func containsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}

func trimForSummary(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	const max = 180
	if len(value) <= max {
		return value
	}
	return value[:max-3] + "..."
}
