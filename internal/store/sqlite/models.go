package sqlite

import (
	"time"

	"odin-os/internal/core/controlscope"
)

type Project struct {
	ID            int64
	Key           string
	Name          string
	Scope         string
	GitRoot       string
	DefaultBranch string
	GitHubRepo    string
	ManifestPath  string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type CreateProjectParams struct {
	Key           string
	Name          string
	Scope         string
	GitRoot       string
	DefaultBranch string
	GitHubRepo    string
	ManifestPath  string
}

type Workspace struct {
	ID                  int64
	Key                 string
	Name                string
	OwnerRef            string
	Status              string
	DefaultCompanionKey string
	PolicyJSON          string
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type CreateWorkspaceParams struct {
	Key                 string
	Name                string
	OwnerRef            string
	Status              string
	DefaultCompanionKey string
	PolicyJSON          string
}

type ListWorkspacesParams struct {
	Status string
}

type Companion struct {
	ID                  int64
	WorkspaceID         int64
	Key                 string
	Title               string
	Kind                string
	Charter             string
	Status              string
	InitiativeScopeJSON string
	ToolPolicyJSON      string
	MemoryPolicyJSON    string
	PlanningPolicyJSON  string
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type CreateCompanionParams struct {
	WorkspaceID         int64
	Key                 string
	Title               string
	Kind                string
	Charter             string
	Status              string
	InitiativeScopeJSON string
	ToolPolicyJSON      string
	MemoryPolicyJSON    string
	PlanningPolicyJSON  string
}

type ListCompanionsParams struct {
	WorkspaceID *int64
	Status      string
}

type Initiative struct {
	ID               int64
	WorkspaceID      int64
	Key              string
	Title            string
	Kind             string
	Status           string
	Summary          string
	LinkedProjectID  *int64
	OwnerCompanionID *int64
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type CreateInitiativeParams struct {
	WorkspaceID      int64
	Key              string
	Title            string
	Kind             string
	Status           string
	Summary          string
	LinkedProjectID  *int64
	OwnerCompanionID *int64
}

type ListInitiativesParams struct {
	WorkspaceID *int64
	Kind        string
	Status      string
}

type ReconcileManagedProjectInitiativeParams struct {
	WorkspaceID int64
	ProjectID   int64
	Key         string
	Title       string
	Status      string
	Summary     string
}

type Task struct {
	ID           int64
	ProjectID    int64
	WorkspaceID  int64
	InitiativeID *int64
	CompanionID  *int64
	Key          string
	Title        string
	Status       string
	Scope        string
	RequestedBy  string
	CurrentRunID *int64
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type CreateTaskParams struct {
	ProjectID    int64
	WorkspaceID  int64
	InitiativeID *int64
	CompanionID  *int64
	Key          string
	Title        string
	Status       string
	Scope        string
	RequestedBy  string
}

type UpdateTaskStatusParams struct {
	TaskID int64
	Status string
}

type Run struct {
	ID         int64
	TaskID     int64
	Executor   string
	Status     string
	Attempt    int
	StartedAt  time.Time
	FinishedAt *time.Time
	Summary    string
}

type StartRunParams struct {
	TaskID   int64
	Executor string
	Attempt  int
	Status   string
}

type FinishRunParams struct {
	RunID   int64
	Status  string
	Summary string
}

type WorkItem struct {
	Task
	Scope         controlscope.ControlScope
	ProjectKey    string
	WorkspaceKey  string
	InitiativeKey string
	CompanionKey  string
}

type Approval struct {
	ID          int64
	TaskID      int64
	RunID       *int64
	Status      string
	RequestedAt time.Time
	ResolvedAt  *time.Time
	DecisionBy  string
	Reason      string
}

type RequestApprovalParams struct {
	TaskID      int64
	RunID       *int64
	Status      string
	RequestedBy string
}

type ResolveApprovalParams struct {
	ApprovalID int64
	Status     string
	DecisionBy string
	Reason     string
}

type Incident struct {
	ID          int64
	RunID       *int64
	Severity    string
	Status      string
	Summary     string
	DetailsJSON string
	OpenedAt    time.Time
	UpdatedAt   time.Time
}

type OpenIncidentParams struct {
	RunID       *int64
	Severity    string
	Status      string
	Summary     string
	DetailsJSON string
}

type UpdateIncidentStatusParams struct {
	IncidentID  int64
	Status      string
	Reason      string
	DetailsJSON string
}

type Recovery struct {
	ID          int64
	IncidentID  *int64
	RunID       *int64
	Status      string
	Strategy    string
	DetailsJSON string
	StartedAt   time.Time
	FinishedAt  *time.Time
	UpdatedAt   time.Time
}

type StartRecoveryParams struct {
	IncidentID  *int64
	RunID       *int64
	Status      string
	Strategy    string
	DetailsJSON string
}

type CompleteRecoveryParams struct {
	RecoveryID  int64
	Status      string
	DetailsJSON string
}

type RecordRecoveryActionParams struct {
	RecoveryID  int64
	Playbook    string
	FaultKey    string
	ActionName  string
	Attempt     int
	Result      string
	Description string
}

type RegistryVersion struct {
	ID          int64
	Source      string
	VersionHash string
	CompiledAt  time.Time
	Notes       string
}

type RecordRegistryVersionParams struct {
	Source      string
	VersionHash string
	Notes       string
}

type ExecutorHealth struct {
	ID          int64
	Executor    string
	Status      string
	CheckedAt   time.Time
	LatencyMS   int64
	DetailsJSON string
}

type RecordExecutorHealthParams struct {
	Executor    string
	Status      string
	LatencyMS   int64
	DetailsJSON string
}

type ContextPacket struct {
	ID                 int64
	TaskID             *int64
	RunID              *int64
	PacketKind         string
	PacketScope        string
	Trigger            string
	CheckpointKey      string
	SupersedesPacketID *int64
	Status             string
	Summary            string
	PayloadJSON        string
	CreatedAt          time.Time
}

type CreateContextPacketParams struct {
	TaskID             *int64
	RunID              *int64
	PacketKind         string
	PacketScope        string
	Trigger            string
	CheckpointKey      string
	SupersedesPacketID *int64
	Status             string
	Summary            string
	PayloadJSON        string
}

type ListContextPacketsParams struct {
	TaskID      *int64
	RunID       *int64
	PacketKind  string
	PacketScope string
	Status      string
}

type WorktreeLease struct {
	ID           int64
	ProjectID    int64
	TaskID       int64
	RunID        int64
	Mode         string
	BranchName   string
	WorktreePath string
	RepoRoot     string
	State        string
	HeartbeatAt  time.Time
	ReleasedAt   *time.Time
	CleanedUpAt  *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type CreateWorktreeLeaseParams struct {
	ProjectID    int64
	TaskID       int64
	RunID        int64
	Mode         string
	BranchName   string
	WorktreePath string
	RepoRoot     string
	State        string
}

type ReleaseWorktreeLeaseParams struct {
	LeaseID int64
	State   string
}

type ProjectTransition struct {
	ID                 int64
	ProjectID          int64
	State              string
	Controller         string
	LimitedActionsJSON string
	Notes              string
	ChangedBy          string
	ChangedAt          time.Time
}

type SetProjectTransitionParams struct {
	ProjectID          int64
	State              string
	Controller         string
	LimitedActionsJSON string
	Notes              string
	ChangedBy          string
}

type ProjectTransitionReport struct {
	ID          int64
	ProjectID   int64
	ReportType  string
	Summary     string
	DetailsJSON string
	RecordedAt  time.Time
}

type RecordProjectTransitionReportParams struct {
	ProjectID   int64
	ReportType  string
	Summary     string
	DetailsJSON string
}

type LearningProposal struct {
	ID                int64
	ProjectID         *int64
	ProposalType      string
	Scope             string
	TargetKey         string
	Summary           string
	Hypothesis        string
	ChangePayloadJSON string
	Status            string
	CreatedBy         string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type CreateLearningProposalParams struct {
	ProjectID         *int64
	ProposalType      string
	Scope             string
	TargetKey         string
	Summary           string
	Hypothesis        string
	ChangePayloadJSON string
	Status            string
	CreatedBy         string
}

type UpdateLearningProposalStatusParams struct {
	ProposalID int64
	Status     string
}

type LearningEvaluation struct {
	ID                   int64
	ProposalID           int64
	FixtureKey           string
	Mode                 string
	Score                float64
	BaselineSummaryJSON  string
	CandidateSummaryJSON string
	ResultSummary        string
	Outcome              string
	RecordedAt           time.Time
}

type RecordLearningEvaluationParams struct {
	ProposalID           int64
	FixtureKey           string
	Mode                 string
	Score                float64
	BaselineSummaryJSON  string
	CandidateSummaryJSON string
	ResultSummary        string
	Outcome              string
}

type LearningPromotion struct {
	ID                    int64
	ProposalID            int64
	ProposalType          string
	Scope                 string
	TargetKey             string
	Status                string
	SupersedesPromotionID *int64
	PromotedBy            string
	PromotedAt            time.Time
	RolledBackBy          string
	RolledBackAt          *time.Time
	RollbackReason        string
}

type PromoteLearningProposalParams struct {
	ProposalID int64
	PromotedBy string
}

type RollbackLearningPromotionParams struct {
	PromotionID    int64
	RolledBackBy   string
	RollbackReason string
}

type ProjectionFreshness struct {
	Surface     string
	Status      string
	RefreshedAt time.Time
	DetailsJSON string
	UpdatedAt   time.Time
}

type RecordProjectionFreshnessParams struct {
	Surface     string
	Status      string
	DetailsJSON string
}

type ListEventsParams struct {
	ProjectID *int64
	TaskID    *int64
	RunID     *int64
}
