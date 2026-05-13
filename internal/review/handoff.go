package review

import (
	"context"
	"fmt"
	"strings"

	"odin-os/internal/store/sqlite"
)

const (
	ReviewStateSelected          = "review_selected"
	ReviewRunStateSelected       = "selected"
	ReviewOutcomeReadOnlyPending = "read_only_pending"
)

type HandoffStore interface {
	UpsertPullRequestHandoff(context.Context, sqlite.UpsertPullRequestHandoffParams) (sqlite.PullRequestHandoff, error)
	UpsertPullRequestReviewResult(context.Context, sqlite.UpsertPullRequestReviewResultParams) (sqlite.PullRequestReviewResult, error)
}

type HandoffOrchestrator struct {
	Store        HandoffStore
	PullRequests PullRequestManager
}

type PullRequestHandoffRequest struct {
	ProjectID              int64
	IssueURL               string
	Title                  string
	Branch                 string
	Summary                string
	Tests                  []string
	Risks                  []string
	Blockers               []string
	CommandsRun            []string
	ChangedFiles           []string
	RuntimeBehaviorChanged bool
	RealOdinProofIncluded  bool
	PostComment            bool
}

type PullRequestHandoffResult struct {
	PullRequest   PullRequest
	Handoff       sqlite.PullRequestHandoff
	Selection     ReviewSelection
	ReviewResults []sqlite.PullRequestReviewResult
}

func (orchestrator HandoffOrchestrator) Upsert(ctx context.Context, request PullRequestHandoffRequest) (PullRequestHandoffResult, error) {
	if orchestrator.Store == nil {
		return PullRequestHandoffResult{}, fmt.Errorf("review handoff store is required")
	}
	if orchestrator.PullRequests == nil {
		return PullRequestHandoffResult{}, fmt.Errorf("pull request manager is required")
	}

	body, err := BuildPullRequestBody(PullRequestBodyInput{
		IssueURL:               request.IssueURL,
		Summary:                request.Summary,
		Tests:                  request.Tests,
		Risks:                  request.Risks,
		Blockers:               request.Blockers,
		CommandsRun:            request.CommandsRun,
		RuntimeBehaviorChanged: request.RuntimeBehaviorChanged,
		RealOdinProofIncluded:  request.RealOdinProofIncluded,
	})
	if err != nil {
		return PullRequestHandoffResult{}, err
	}

	pullRequest, err := orchestrator.PullRequests.Upsert(ctx, PullRequestRequest{
		IssueURL: request.IssueURL,
		Title:    request.Title,
		Branch:   request.Branch,
		Body:     body,
	})
	if err != nil {
		return PullRequestHandoffResult{}, err
	}

	selection := SelectReviewAgents(ReviewSelectionInput{ChangedFiles: request.ChangedFiles})
	handoff, err := orchestrator.Store.UpsertPullRequestHandoff(ctx, sqlite.UpsertPullRequestHandoffParams{
		ProjectID:     request.ProjectID,
		Provider:      pullRequest.Provider,
		Repo:          pullRequest.Repo,
		Number:        pullRequest.Number,
		URL:           pullRequest.URL,
		State:         pullRequest.State,
		IssueURL:      request.IssueURL,
		Branch:        request.Branch,
		Title:         request.Title,
		Summary:       request.Summary,
		Tests:         request.Tests,
		Risks:         request.Risks,
		Blockers:      request.Blockers,
		SelectedRoles: selection.Roles,
		ReviewState:   ReviewStateSelected,
	})
	if err != nil {
		return PullRequestHandoffResult{}, err
	}

	results := make([]sqlite.PullRequestReviewResult, 0, len(selection.Roles))
	for _, role := range selection.Roles {
		result, err := orchestrator.Store.UpsertPullRequestReviewResult(ctx, sqlite.UpsertPullRequestReviewResultParams{
			HandoffID: handoff.ID,
			Role:      role,
			State:     ReviewRunStateSelected,
			Summary:   reviewSelectionSummary(role, selection),
			Comments:  reviewSelectionComments(role, selection),
			Blockers:  []string{},
			Outcome:   ReviewOutcomeReadOnlyPending,
		})
		if err != nil {
			return PullRequestHandoffResult{}, err
		}
		results = append(results, result)
	}

	if request.PostComment {
		comment, err := BuildReviewComment(PullRequestBodyInput{
			Summary:  request.Summary,
			Tests:    request.Tests,
			Risks:    request.Risks,
			Blockers: request.Blockers,
		})
		if err != nil {
			return PullRequestHandoffResult{}, err
		}
		if err := orchestrator.PullRequests.AddComment(ctx, PullRequestComment{PullRequest: pullRequest, Body: comment}); err != nil {
			return PullRequestHandoffResult{}, err
		}
	}

	return PullRequestHandoffResult{
		PullRequest:   pullRequest,
		Handoff:       handoff,
		Selection:     selection,
		ReviewResults: results,
	}, nil
}

func reviewSelectionSummary(role string, selection ReviewSelection) string {
	switch role {
	case RoleSecurity:
		return "Read-only security review selected."
	case RoleQA:
		return "Read-only QA review selected."
	default:
		return "Read-only reviewer review selected."
	}
}

func reviewSelectionComments(role string, selection ReviewSelection) []string {
	if role != RoleSecurity || len(selection.SecurityReasons) == 0 {
		return []string{"read-only review selected; no merge, approval, or deployment authority granted"}
	}
	comments := make([]string, 0, len(selection.SecurityReasons)+1)
	comments = append(comments, "read-only security review selected; no merge, approval, or deployment authority granted")
	for _, reason := range selection.SecurityReasons {
		reason = strings.TrimSpace(reason)
		if reason != "" {
			comments = append(comments, reason)
		}
	}
	return comments
}
