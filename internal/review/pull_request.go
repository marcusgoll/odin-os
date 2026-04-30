package review

import (
	"context"
	"errors"
	"strings"
)

var ErrMissingPullRequestEvidence = errors.New("pull request handoff evidence is incomplete")

// PullRequestManager is the canonical PR handoff seam. It deliberately has no
// merge, approval, or deployment method; those remain human-controlled.
type PullRequestManager interface {
	Upsert(ctx context.Context, request PullRequestRequest) (PullRequest, error)
	AddComment(ctx context.Context, request PullRequestComment) error
}

type PullRequestRequest struct {
	IssueURL string
	Title    string
	Branch   string
	Body     string
	Labels   []string
}

type PullRequest struct {
	Provider string
	Repo     string
	Number   int
	URL      string
	State    string
}

type PullRequestComment struct {
	PullRequest PullRequest
	Body        string
}

type PullRequestBodyInput struct {
	IssueURL               string
	Summary                string
	Tests                  []string
	Risks                  []string
	Blockers               []string
	CommandsRun            []string
	RuntimeBehaviorChanged bool
	RealOdinProofIncluded  bool
}

func BuildPullRequestBody(input PullRequestBodyInput) (string, error) {
	if strings.TrimSpace(input.Summary) == "" || len(nonEmpty(input.Tests)) == 0 || len(nonEmpty(input.Risks)) == 0 || len(nonEmpty(input.CommandsRun)) == 0 {
		return "", ErrMissingPullRequestEvidence
	}
	if input.RuntimeBehaviorChanged && (!input.RealOdinProofIncluded || !containsOdinCommand(input.CommandsRun)) {
		return "", ErrMissingPullRequestEvidence
	}

	var body strings.Builder
	writeSection(&body, "Summary", bulletLines([]string{input.Summary}))
	writeSection(&body, "Issue", bulletLines(defaulted(input.IssueURL, "No linked issue provided.")))
	writeSection(&body, "Tests", bulletLines(input.Tests))
	writeSection(&body, "Risks", bulletLines(input.Risks))
	writeSection(&body, "Blockers", bulletLines(defaultList(input.Blockers, "No blockers reported.")))

	body.WriteString("## Human Checklist\n\n")
	body.WriteString("- [ ] Human reviewed the diff.\n")
	body.WriteString("- [ ] Human approved merge.\n")
	body.WriteString("- [ ] Human approved production deployment if deployment is in scope.\n\n")

	body.WriteString("## Verification Contract\n\n")
	body.WriteString("- [ ] unit coverage added or updated where applicable\n")
	body.WriteString("- [ ] contract coverage added or updated where applicable\n")
	body.WriteString("- [ ] integration coverage added or updated where applicable\n")
	writeCheckbox(&body, input.RuntimeBehaviorChanged, "this PR changes user-visible or orchestration-facing behavior")
	writeCheckbox(&body, input.RealOdinProofIncluded, "if the box above is checked, real `odin` command proof is included below")
	body.WriteString("\n")

	writeSection(&body, "Proven", bulletLines(input.Tests))
	writeSection(&body, "Unproven", bulletLines(append(defaultList(input.Blockers, "No blockers reported."), input.Risks...)))

	body.WriteString("## Commands Run\n\n```bash\n")
	for _, command := range nonEmpty(input.CommandsRun) {
		body.WriteString(command)
		body.WriteString("\n")
	}
	body.WriteString("```\n")

	return body.String(), nil
}

func BuildReviewComment(input PullRequestBodyInput) (string, error) {
	if strings.TrimSpace(input.Summary) == "" || len(nonEmpty(input.Tests)) == 0 || len(nonEmpty(input.Risks)) == 0 {
		return "", ErrMissingPullRequestEvidence
	}

	var body strings.Builder
	writeSection(&body, "Summary", bulletLines([]string{input.Summary}))
	writeSection(&body, "Tests", bulletLines(input.Tests))
	writeSection(&body, "Risks", bulletLines(input.Risks))
	writeSection(&body, "Blockers", bulletLines(defaultList(input.Blockers, "No blockers reported.")))
	return body.String(), nil
}

func writeSection(body *strings.Builder, heading string, content string) {
	body.WriteString("## ")
	body.WriteString(heading)
	body.WriteString("\n\n")
	body.WriteString(content)
	body.WriteString("\n")
}

func writeCheckbox(body *strings.Builder, checked bool, label string) {
	if checked {
		body.WriteString("- [x] ")
	} else {
		body.WriteString("- [ ] ")
	}
	body.WriteString(label)
	body.WriteString("\n")
}

func bulletLines(values []string) string {
	var body strings.Builder
	for _, value := range nonEmpty(values) {
		body.WriteString("- ")
		body.WriteString(value)
		body.WriteString("\n")
	}
	return body.String()
}

func defaulted(value string, fallback string) []string {
	if strings.TrimSpace(value) == "" {
		return []string{fallback}
	}
	return []string{value}
}

func defaultList(values []string, fallback string) []string {
	clean := nonEmpty(values)
	if len(clean) == 0 {
		return []string{fallback}
	}
	return clean
}

func nonEmpty(values []string) []string {
	clean := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			clean = append(clean, trimmed)
		}
	}
	return clean
}

func containsOdinCommand(commands []string) bool {
	for _, command := range nonEmpty(commands) {
		fields := strings.Fields(command)
		for _, field := range fields {
			if strings.Trim(field, `"'`) == "odin" || strings.HasSuffix(strings.Trim(field, `"'`), "/odin") {
				return true
			}
		}
	}
	return false
}
