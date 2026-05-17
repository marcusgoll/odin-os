package goals

import (
	"strings"

	"odin-os/internal/store/sqlite"
)

type AutoPolicyDecision struct {
	AutoStart       bool
	Reason          string
	ProjectKey      string
	ExecutionIntent string
}

const (
	AutoPolicyReasonReadOnly              = "read_only_goal"
	AutoPolicyReasonNeedsReview           = "goal_requires_review"
	AutoPolicyReasonExternalAccountReview = "external_account_goal_requires_review"
	AutoPolicyReasonMutationReview        = "mutation_goal_requires_review"
	AutoPolicyExecutionIntentReadOnly     = "read_only"
	AutoPolicyDefaultProjectKey           = "odin-core"
)

func ClassifyAutoPolicy(goal sqlite.Goal) AutoPolicyDecision {
	text := strings.ToLower(strings.Join([]string{
		goal.Title,
		goal.Description,
		goal.Source,
	}, " "))

	projectKey := goalPolicyProjectKey(text)
	if containsAnyToken(text, []string{
		"login",
		"mfa",
		"captcha",
		"x ",
		"twitter",
		"publish",
		"social",
		"browser",
	}) {
		return AutoPolicyDecision{Reason: AutoPolicyReasonExternalAccountReview, ProjectKey: projectKey}
	}
	if containsAnyToken(text, []string{
		"autonomous",
		"worker",
		"executor",
		"subagent",
		"shim",
		"tmux",
		"e2e",
		"merge",
		"deploy",
		"release",
		"implement",
		"build",
		"fix ",
		"change",
		"edit",
		"write",
		"policy",
		"governance",
		"destructive",
		"delete",
		"transfer",
		"finance",
		"robinhood",
	}) {
		return AutoPolicyDecision{Reason: AutoPolicyReasonMutationReview, ProjectKey: projectKey}
	}
	if startsWithAny(text, []string{
		"review ",
		"inspect ",
		"summarize ",
		"summarise ",
		"audit ",
		"research ",
		"report ",
		"analyze ",
		"analyse ",
	}) {
		return AutoPolicyDecision{
			AutoStart:       true,
			Reason:          AutoPolicyReasonReadOnly,
			ProjectKey:      projectKey,
			ExecutionIntent: AutoPolicyExecutionIntentReadOnly,
		}
	}
	return AutoPolicyDecision{Reason: AutoPolicyReasonNeedsReview, ProjectKey: projectKey}
}

func goalPolicyProjectKey(text string) string {
	switch {
	case strings.Contains(text, "pbs"):
		return "pbs"
	case strings.Contains(text, "cfipros"):
		return "cfipros"
	case strings.Contains(text, "marcusgoll"):
		return "marcusgoll"
	default:
		return AutoPolicyDefaultProjectKey
	}
}

func containsAnyToken(text string, tokens []string) bool {
	for _, token := range tokens {
		if strings.Contains(text, token) {
			return true
		}
	}
	return false
}

func startsWithAny(text string, prefixes []string) bool {
	text = strings.TrimSpace(text)
	for _, prefix := range prefixes {
		if strings.HasPrefix(text, prefix) {
			return true
		}
	}
	return false
}
