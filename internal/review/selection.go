package review

import (
	"sort"
	"strings"
)

const (
	RoleReviewer = "reviewer"
	RoleQA       = "qa"
	RoleSecurity = "security"
)

type ReviewSelectionInput struct {
	ChangedFiles []string
}

type ReviewSelection struct {
	Roles            []string
	ReadOnly         bool
	SecurityRequired bool
	SecurityReasons  []string
}

func SelectReviewAgents(input ReviewSelectionInput) ReviewSelection {
	roles := []string{RoleReviewer, RoleQA}
	reasonSet := map[string]struct{}{}
	for _, path := range input.ChangedFiles {
		for _, trigger := range securityTriggers {
			if trigger.matches(path) {
				reasonSet[trigger.reason] = struct{}{}
			}
		}
	}

	reasons := make([]string, 0, len(reasonSet))
	for reason := range reasonSet {
		reasons = append(reasons, reason)
	}
	sort.Strings(reasons)
	if len(reasons) > 0 {
		roles = append(roles, RoleSecurity)
	}

	return ReviewSelection{
		Roles:            roles,
		ReadOnly:         true,
		SecurityRequired: len(reasons) > 0,
		SecurityReasons:  reasons,
	}
}

type securityTrigger struct {
	prefix string
	reason string
}

func (trigger securityTrigger) matches(path string) bool {
	normalized := strings.TrimPrefix(strings.TrimSpace(path), "./")
	return normalized == trigger.prefix || strings.HasPrefix(normalized, trigger.prefix+"/")
}

var securityTriggers = []securityTrigger{
	{prefix: "internal/executors", reason: "runners and process execution require security review"},
	{prefix: "internal/runner", reason: "runners and process execution require security review"},
	{prefix: "shims", reason: "shims require security review"},
	{prefix: "scripts/dev/install-systemd-service.sh", reason: "deployment requires security review"},
	{prefix: "scripts", reason: "process execution and shell commands require security review"},
	{prefix: "internal/tracker/github", reason: "GitHub tokens and API writes require security review"},
	{prefix: "internal/adapters/github", reason: "GitHub tokens and API writes require security review"},
	{prefix: "internal/vcs", reason: "filesystem operations and worktree management require security review"},
	{prefix: "internal/workspace", reason: "filesystem operations and worktree management require security review"},
	{prefix: "internal/api/http", reason: "dashboard controls require security review"},
	{prefix: "internal/dashboard", reason: "dashboard controls require security review"},
	{prefix: "config", reason: "secrets and runtime config require security review"},
	{prefix: "configs", reason: "secrets and runtime config require security review"},
	{prefix: "docs/SECURITY.md", reason: "secrets and security policy require security review"},
	{prefix: "deploy", reason: "deployment requires security review"},
	{prefix: ".github/workflows", reason: "deployment and GitHub automation require security review"},
}
