package review

import (
	"strings"
	"testing"
)

func TestBuildPullRequestBodyIncludesTemplateEvidenceAndHumanChecklist(t *testing.T) {
	t.Parallel()

	body, err := BuildPullRequestBody(PullRequestBodyInput{
		IssueURL:               "https://github.com/marcusgoll/odin-os/issues/72",
		Summary:                "Contain worker panic failures.",
		Tests:                  []string{"go test ./internal/runtime/jobs -count=1", "/tmp/odin doctor --json"},
		Risks:                  []string{"Direct task panic path still needs characterization."},
		Blockers:               []string{"Live GitHub PR mutation is not wired."},
		CommandsRun:            []string{"go test ./internal/runtime/jobs -count=1", "/tmp/odin doctor --json"},
		RuntimeBehaviorChanged: true,
		RealOdinProofIncluded:  true,
	})
	if err != nil {
		t.Fatalf("BuildPullRequestBody() error = %v", err)
	}

	for _, want := range []string{
		"## Summary",
		"## Verification Contract",
		"## Proven",
		"## Unproven",
		"## Commands Run",
		"## Issue",
		"## Tests",
		"## Risks",
		"## Blockers",
		"## Human Checklist",
		"- Contain worker panic failures.",
		"- https://github.com/marcusgoll/odin-os/issues/72",
		"- go test ./internal/runtime/jobs -count=1",
		"- Direct task panic path still needs characterization.",
		"- Live GitHub PR mutation is not wired.",
		"- [ ] Human reviewed the diff.",
		"- [ ] Human approved merge.",
		"- [ ] Human approved production deployment if deployment is in scope.",
		"- [x] this PR changes user-visible or orchestration-facing behavior",
		"- [x] if the box above is checked, real `odin` command proof is included below",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("PR body missing %q:\n%s", want, body)
		}
	}
}

func TestBuildPullRequestBodyRejectsMissingAcceptanceEvidence(t *testing.T) {
	t.Parallel()

	_, err := BuildPullRequestBody(PullRequestBodyInput{
		IssueURL:    "https://github.com/marcusgoll/odin-os/issues/72",
		Summary:     "Contain worker panic failures.",
		Tests:       []string{"go test ./internal/runtime/jobs -count=1"},
		Risks:       []string{"No live proof."},
		CommandsRun: []string{},
	})
	if err == nil {
		t.Fatal("BuildPullRequestBody() error = nil, want missing commands error")
	}
}

func TestBuildPullRequestBodyRejectsRuntimeChangeWithoutOdinProof(t *testing.T) {
	t.Parallel()

	_, err := BuildPullRequestBody(PullRequestBodyInput{
		IssueURL:               "https://github.com/marcusgoll/odin-os/issues/72",
		Summary:                "Contain worker panic failures.",
		Tests:                  []string{"go test ./internal/runtime/jobs -count=1"},
		Risks:                  []string{"No real odin proof."},
		CommandsRun:            []string{"go test ./internal/runtime/jobs -count=1"},
		RuntimeBehaviorChanged: true,
		RealOdinProofIncluded:  false,
	})
	if err == nil {
		t.Fatal("BuildPullRequestBody() error = nil, want missing odin proof error")
	}
}

func TestBuildReviewCommentIncludesSummaryTestsRisksAndBlockers(t *testing.T) {
	t.Parallel()

	comment, err := BuildReviewComment(PullRequestBodyInput{
		Summary:  "PR handoff is ready for human review.",
		Tests:    []string{"go test ./internal/review -count=1"},
		Risks:    []string{"Live GitHub mutation is deferred."},
		Blockers: []string{"No live PR adapter yet."},
	})
	if err != nil {
		t.Fatalf("BuildReviewComment() error = %v", err)
	}

	for _, want := range []string{
		"## Summary",
		"## Tests",
		"## Risks",
		"## Blockers",
		"- PR handoff is ready for human review.",
		"- go test ./internal/review -count=1",
		"- Live GitHub mutation is deferred.",
		"- No live PR adapter yet.",
	} {
		if !strings.Contains(comment, want) {
			t.Fatalf("review comment missing %q:\n%s", want, comment)
		}
	}
}

func TestSelectReviewAgentsRequiresSecurityForSensitivePaths(t *testing.T) {
	t.Parallel()

	selection := SelectReviewAgents(ReviewSelectionInput{ChangedFiles: []string{
		"internal/executors/codex/adapter.go",
		"docs/SECURITY.md",
		"scripts/dev/install-systemd-service.sh",
	}})

	if !selection.ReadOnly {
		t.Fatal("SelectReviewAgents().ReadOnly = false, want true")
	}
	if !selection.SecurityRequired {
		t.Fatalf("SelectReviewAgents().SecurityRequired = false, want true: %+v", selection)
	}
	for _, want := range []string{RoleReviewer, RoleQA, RoleSecurity} {
		if !hasRole(selection.Roles, want) {
			t.Fatalf("roles = %#v, want %s", selection.Roles, want)
		}
	}
	for _, wantReason := range []string{"process execution", "secrets", "deployment"} {
		if !containsSubstring(selection.SecurityReasons, wantReason) {
			t.Fatalf("security reasons = %#v, want substring %q", selection.SecurityReasons, wantReason)
		}
	}
}

func TestSelectReviewAgentsDoesNotRequireSecurityForDocsOnlyChange(t *testing.T) {
	t.Parallel()

	selection := SelectReviewAgents(ReviewSelectionInput{ChangedFiles: []string{
		"docs/brownfield/PR_REVIEW_CONSOLIDATION.md",
	}})

	if selection.SecurityRequired {
		t.Fatalf("SelectReviewAgents().SecurityRequired = true, want false: %+v", selection)
	}
	for _, want := range []string{RoleReviewer, RoleQA} {
		if !hasRole(selection.Roles, want) {
			t.Fatalf("roles = %#v, want %s", selection.Roles, want)
		}
	}
	if hasRole(selection.Roles, RoleSecurity) {
		t.Fatalf("roles = %#v, did not expect %s", selection.Roles, RoleSecurity)
	}
}

func hasRole(roles []string, want string) bool {
	for _, role := range roles {
		if role == want {
			return true
		}
	}
	return false
}

func containsSubstring(values []string, want string) bool {
	for _, value := range values {
		if strings.Contains(value, want) {
			return true
		}
	}
	return false
}
