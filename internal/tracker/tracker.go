package tracker

import "context"

const (
	LabelReady       = "odin:ready"
	LabelRunning     = "odin:running"
	LabelBlocked     = "odin:blocked"
	LabelHumanReview = "odin:human-review"
	LabelFailed      = "odin:failed"
	LabelDone        = "odin:done"
	LabelPaused      = "odin:paused"
)

const (
	AgentLabelArchitect      = "agent:architect"
	AgentLabelGoOrchestrator = "agent:go-orchestrator"
	AgentLabelBackend        = "agent:backend"
	AgentLabelFrontend       = "agent:frontend"
	AgentLabelIOS            = "agent:ios"
	AgentLabelQA             = "agent:qa"
	AgentLabelSecurity       = "agent:security"
	AgentLabelReviewer       = "agent:reviewer"
	AgentLabelDevOps         = "agent:devops"
	AgentLabelDocs           = "agent:docs"
)

// Issue is the normalized tracker intake shape used before persistence.
type Issue struct {
	Provider    string
	Repo        string
	Number      int
	Title       string
	Body        string
	URL         string
	State       string
	Labels      []string
	PullRequest bool
}

// IssueComment is the normalized comment shape used for idempotency checks and
// live mutation reports.
type IssueComment struct {
	Body string
	URL  string
}

// IssueID identifies one issue in an external tracker.
type IssueID struct {
	Provider string
	Repo     string
	Number   int
}

// FollowUpIssue is the minimal shape for a human-review follow-up issue.
type FollowUpIssue struct {
	Repo   string
	Title  string
	Body   string
	Labels []string
}

// RequestAudit records tracker HTTP method usage for operator proof.
type RequestAudit struct {
	Reads     int
	Writes    int
	Forbidden []ForbiddenRequest
}

// ForbiddenRequest records a forbidden external mutation attempt without secrets.
type ForbiddenRequest struct {
	Method string `json:"method"`
	Path   string `json:"path"`
}

// RequestAuditor is implemented by trackers that can prove read/write request counts.
type RequestAuditor interface {
	RequestAudit() RequestAudit
}

// Tracker wraps issue-tracker behavior without making the tracker Odin runtime authority.
type Tracker interface {
	FetchEligibleIssues(ctx context.Context) ([]Issue, error)
	FetchIssueByID(ctx context.Context, id IssueID) (Issue, error)
	MarkInProgress(ctx context.Context, id IssueID) error
	MarkBlocked(ctx context.Context, id IssueID, reason string) error
	MarkFailed(ctx context.Context, id IssueID, reason string) error
	MarkReadyForReview(ctx context.Context, id IssueID) error
	MarkDone(ctx context.Context, id IssueID) error
	AddComment(ctx context.Context, id IssueID, body string) error
	CreateFollowUpIssue(ctx context.Context, issue FollowUpIssue) (Issue, error)
}
