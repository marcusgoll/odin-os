package sqlite

import (
	"time"

	runtimeevents "odin-os/internal/runtime/events"
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

type ExternalIssue struct {
	ID           int64
	ProjectID    int64
	Provider     string
	Repo         string
	Number       int
	Title        string
	BodyHash     string
	URL          string
	State        string
	LabelsJSON   string
	SyncStatus   string
	LastSyncedAt time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type Initiative struct {
	ID               int64
	WorkspaceID      int64
	Key              string
	Title            string
	Kind             string
	Status           string
	Summary          string
	OwnerCompanionID *int64
	LinkedProjectID  *int64
	CreatedAt        time.Time
	UpdatedAt        time.Time
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

type CreateProjectParams struct {
	Key           string
	Name          string
	Scope         string
	GitRoot       string
	DefaultBranch string
	GitHubRepo    string
	ManifestPath  string
}

type UpsertProjectParams = CreateProjectParams

type UpsertExternalIssueParams struct {
	ProjectID  int64
	Provider   string
	Repo       string
	Number     int
	Title      string
	BodyHash   string
	URL        string
	State      string
	LabelsJSON string
	SyncStatus string
}

type ListExternalIssuesParams struct {
	Repo       string
	SyncStatus string
	ProjectID  *int64
}

type UpsertInitiativeParams struct {
	WorkspaceID      int64
	Key              string
	Title            string
	Kind             string
	Status           string
	Summary          string
	OwnerCompanionID *int64
	LinkedProjectID  *int64
}

type UpdateInitiativeStatusParams struct {
	InitiativeID int64
	Status       string
}

type UpsertCompanionParams struct {
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
	WorkspaceID int64
}

type ManagedProjectRegistrationParams struct {
	Workspace CreateWorkspaceParams
	Project   UpsertProjectParams
}

type Workspace struct {
	ID                  int64
	Key                 string
	Name                string
	OwnerRef            string
	DefaultCompanionKey string
	Status              string
	PolicyJSON          string
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type FollowUpObligation struct {
	ID                 int64
	WorkspaceID        int64
	InitiativeID       *int64
	CompanionID        *int64
	TargetProjectID    int64
	Title              string
	Status             string
	CadenceJSON        string
	NextDueAt          time.Time
	LastMaterializedAt *time.Time
	LastCompletedAt    *time.Time
	PolicyJSON         string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type CreateWorkspaceParams struct {
	Key                 string
	Name                string
	OwnerRef            string
	DefaultCompanionKey string
	Status              string
	PolicyJSON          string
}

type UpdateWorkspacePolicyParams struct {
	WorkspaceID int64
	PolicyJSON  string
}

type CreateFollowUpObligationParams struct {
	WorkspaceID     int64
	InitiativeID    *int64
	CompanionID     *int64
	TargetProjectID int64
	Title           string
	Status          string
	CadenceJSON     string
	NextDueAt       time.Time
	PolicyJSON      string
}

type ListFollowUpObligationsParams struct {
	WorkspaceID  int64
	InitiativeID *int64
	Status       string
}

type RecordFollowUpMaterializationParams struct {
	ObligationID       int64
	LastMaterializedAt time.Time
	NextDueAt          *time.Time
}

type UpdateFollowUpObligationParams struct {
	ObligationID       int64
	Status             string
	NextDueAt          *time.Time
	LastMaterializedAt *time.Time
	LastCompletedAt    *time.Time
}

type Task struct {
	ID                    int64
	ProjectID             int64
	Key                   string
	Title                 string
	ActionKey             string
	Status                string
	Scope                 string
	RequestedBy           string
	WorkspaceID           *int64
	InitiativeID          *int64
	CompanionID           *int64
	FollowUpObligationID  *int64
	FollowUpOccurrenceKey string
	WorkKind              string
	Summary               string
	TerminalReason        string
	ArtifactsJSON         string
	CurrentRunID          *int64
	NextEligibleAt        time.Time
	Priority              int
	LastError             string
	RetryCount            int
	MaxAttempts           int
	BlockedReason         string
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

type TaskIntake struct {
	ID          int64
	TaskID      int64
	Source      string
	IntakeType  string
	DedupKey    string
	RequestedBy string
	PayloadJSON string
	CreatedAt   time.Time
}

type IntakeItem struct {
	ID                       int64
	WorkspaceID              string
	SourceFamily             string
	ExternalObjectID         string
	EventKind                string
	Subject                  string
	DedupeKey                string
	DedupeRecipeVersion      string
	SourceFactsJSON          string
	Status                   string
	Scope                    string
	ScopeKey                 string
	Summary                  string
	ConversationTranscriptID *int64
	CanonicalIntakeItemID    *int64
	SuppressionReason        string
	RoutingNotes             string
	ReceivedAt               time.Time
	CreatedAt                time.Time
	UpdatedAt                time.Time
}

type AutomationTrigger struct {
	ID                     int64
	WorkspaceID            string
	Key                    string
	ProjectID              int64
	InitiativeKey          string
	Kind                   string
	Status                 string
	RuleJSON               string
	RuleSummary            string
	WorkItemTitle          string
	NextEligibleAt         *time.Time
	LastEvaluatedAt        *time.Time
	LastMaterializedAt     *time.Time
	LastMaterializationKey string
	LastWorkItemID         *int64
	LastWorkItemKey        string
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

type AutomationTriggerMaterialization struct {
	ID                 int64
	TriggerID          int64
	MaterializationKey string
	TaskID             int64
	Reason             string
	RequestedBy        string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type CreateTaskParams struct {
	ProjectID             int64
	Key                   string
	Title                 string
	ActionKey             string
	Status                string
	Scope                 string
	RequestedBy           string
	WorkspaceID           *int64
	InitiativeID          *int64
	CompanionID           *int64
	FollowUpObligationID  *int64
	FollowUpOccurrenceKey string
	WorkKind              string
}

type CreateTaskIntakeParams struct {
	TaskID      int64
	Source      string
	IntakeType  string
	DedupKey    string
	RequestedBy string
	PayloadJSON string
}

type CreateIntakeItemParams struct {
	WorkspaceID              string
	SourceFamily             string
	ExternalObjectID         string
	EventKind                string
	Subject                  string
	DedupeKey                string
	DedupeRecipeVersion      string
	SourceFactsJSON          string
	Status                   string
	Scope                    string
	ScopeKey                 string
	Summary                  string
	ConversationTranscriptID *int64
	CanonicalIntakeItemID    *int64
	SuppressionReason        string
	RoutingNotes             string
	ReceivedAt               time.Time
}

type ListIntakeItemsParams struct {
	WorkspaceID string
	Status      string
	Scope       string
	ScopeKey    string
}

type ProcessIntakeItemParams struct {
	ID                    int64
	Status                string
	Summary               string
	CanonicalIntakeItemID *int64
	SuppressionReason     string
	RoutingNotes          string
	Events                []IntakeItemProcessingEvent
}

type ReviewIntakeItemParams struct {
	ID               int64
	Status           string
	Summary          string
	RoutingNotes     string
	EventType        runtimeevents.Type
	Decision         string
	WorkCreated      bool
	ApprovalRequired bool
	PolicyDecision   string
	PolicyReason     string
	WorkItemID       *int64
	WorkItemKey      string
}

type IntakeItemProcessingEvent struct {
	Type    runtimeevents.Type
	Stage   string
	Result  string
	Payload any
}

type UpsertAutomationTriggerParams struct {
	WorkspaceID    string
	Key            string
	ProjectID      int64
	InitiativeKey  string
	Kind           string
	Status         string
	RuleJSON       string
	RuleSummary    string
	WorkItemTitle  string
	NextEligibleAt *time.Time
}

type ListAutomationTriggersParams struct {
	WorkspaceID string
	Status      string
}

type FireAutomationTriggerParams struct {
	WorkspaceID       string
	Key               string
	Source            string
	Reason            string
	RequestedBy       string
	SetNextEligibleAt bool
	NextEligibleAt    *time.Time
}

type FireAutomationTriggerResult struct {
	Trigger         AutomationTrigger
	Materialization AutomationTriggerMaterialization
	WorkItem        Task
	CreatedWorkItem bool
}

type MarkAutomationTriggerErroredParams struct {
	WorkspaceID string
	Key         string
	Reason      string
	Error       string
}

type UpdateTaskStatusParams struct {
	TaskID                 int64
	Status                 string
	Summary                string
	TerminalReason         string
	ArtifactsJSON          string
	AllowedCurrentStatuses []string
}

type UpdateTaskQueueStateParams struct {
	TaskID         int64
	Status         string
	NextEligibleAt time.Time
	Priority       int
	LastError      string
	RetryCount     int
	MaxAttempts    int
	BlockedReason  string
}

type BlockTaskParams struct {
	TaskID int64
	Reason string
}

type RequeueTaskAtParams struct {
	TaskID         int64
	NextEligibleAt time.Time
}

type IncrementTaskRetryParams struct {
	TaskID         int64
	LastError      string
	NextEligibleAt time.Time
}

type Run struct {
	ID             int64
	TaskID         int64
	Executor       string
	Status         string
	Attempt        int
	StartedAt      time.Time
	FinishedAt     *time.Time
	Summary        string
	TerminalReason string
	ArtifactsJSON  string
}

type RunArtifact struct {
	ID           int64
	RunID        int64
	ArtifactType string
	Summary      string
	DetailsJSON  string
	CreatedAt    time.Time
}

type RecordRunArtifactParams struct {
	RunID        int64
	ArtifactType string
	Summary      string
	DetailsJSON  string
}

type ListRunArtifactsParams struct {
	RunID        int64
	ArtifactType string
}

type MemoryEntry struct {
	ID              int64
	WorkspaceID     int64
	InitiativeID    *int64
	CompanionID     *int64
	TaskID          *int64
	RunID           *int64
	EntryType       string
	VisibilityScope string
	RetentionClass  string
	Summary         string
	Content         string
	MetadataJSON    string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type CreateMemoryEntryParams struct {
	WorkspaceID     int64
	InitiativeID    *int64
	CompanionID     *int64
	TaskID          *int64
	RunID           *int64
	EntryType       string
	VisibilityScope string
	RetentionClass  string
	Summary         string
	Content         string
	MetadataJSON    string
}

type ListMemoryEntriesParams struct {
	WorkspaceID     int64
	InitiativeID    *int64
	CompanionID     *int64
	TaskID          *int64
	RunID           *int64
	EntryType       string
	VisibilityScope string
	RetentionClass  string
	Limit           int
}

type StartRunParams struct {
	TaskID     int64
	Executor   string
	Attempt    int
	Status     string
	TaskStatus string
}

type StartRunAndUpdateTaskStatusParams struct {
	TaskID     int64
	Executor   string
	Attempt    int
	RunStatus  string
	TaskStatus string
}

type UpdateRunAndTaskStatusParams struct {
	RunID      int64
	RunStatus  string
	TaskStatus string
}

type ClaimRunExecutionParams struct {
	TaskID int64
	RunID  int64
	Actor  string
}

type FinishRunParams struct {
	RunID          int64
	Status         string
	Summary        string
	TerminalReason string
	ArtifactsJSON  string
}

type ResolveStalledRunParams struct {
	RunID          int64
	TaskID         int64
	TaskStatus     string
	Summary        string
	TerminalReason string
	ArtifactsJSON  string
}

type AwaitApprovalParams struct {
	TaskID         int64
	RunID          int64
	RequestedBy    string
	Summary        string
	TerminalReason string
	ArtifactsJSON  string
}

type FinishRunAndUpdateTaskStatusParams struct {
	RunID          int64
	RunStatus      string
	Summary        string
	TerminalReason string
	ArtifactsJSON  string
	TaskID         int64
	TaskStatus     string
}

type FinishRunAndSetTaskStatusParams struct {
	RunID          int64
	RunStatus      string
	Summary        string
	TerminalReason string
	ArtifactsJSON  string
	TaskStatus     string
}

type FailRunAndRetryTaskParams struct {
	RunID          int64
	Summary        string
	TerminalReason string
	ArtifactsJSON  string
	LastError      string
	NextEligibleAt time.Time
}

type InterruptRunAndRequeueTaskParams struct {
	RunID   int64
	Summary string
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

type BlockTaskAndRequestApprovalParams struct {
	TaskID      int64
	RunID       *int64
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

type RecordSkillLifecycleEventParams struct {
	SkillKey         string
	Scope            string
	ProjectID        *int64
	Operation        string
	Outcome          string
	ExecutionProfile string
	RuntimeEffect    string
	Version          string
	HandlerType      string
	HandlerRef       string
	Permissions      []string
	DurationMS       int64
	ErrorCode        string
	ErrorText        string
}

type SkillArtifact struct {
	ID               int64
	SkillKey         string
	Scope            string
	ProjectID        *int64
	Status           string
	ArtifactType     string
	Summary          string
	OutputJSON       string
	RawOutput        string
	HandlerRef       string
	ExecutionProfile string
	PermissionsJSON  string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type CreateSkillArtifactParams struct {
	SkillKey         string
	Scope            string
	ProjectID        *int64
	Status           string
	ArtifactType     string
	Summary          string
	OutputJSON       string
	RawOutput        string
	HandlerRef       string
	ExecutionProfile string
	PermissionsJSON  string
}

type ListSkillArtifactsParams struct {
	SkillKey string
	Status   string
	Limit    int
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

type UpdateContextPacketStatusParams struct {
	PacketID    int64
	Status      string
	Summary     string
	PayloadJSON string
}

type ConversationTranscript struct {
	ID          int64
	ProjectID   *int64
	TaskID      *int64
	RunID       *int64
	Scope       string
	ScopeKey    string
	Mode        string
	Prompt      string
	Response    string
	ToolSummary string
	Executor    string
	CreatedAt   time.Time
}

type RecordConversationTranscriptParams struct {
	ProjectID   *int64
	TaskID      *int64
	RunID       *int64
	Scope       string
	ScopeKey    string
	Mode        string
	Prompt      string
	Response    string
	ToolSummary string
	Executor    string
}

type ListConversationTranscriptsParams struct {
	ProjectID *int64
	TaskID    *int64
	RunID     *int64
	Scope     string
	ScopeKey  string
	Mode      string
}

type MemorySummary struct {
	ID                 int64
	ProjectID          *int64
	SourceTranscriptID *int64
	TaskID             *int64
	RunID              *int64
	Scope              string
	ScopeKey           string
	MemoryType         string
	Summary            string
	DetailsJSON        string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type WorkspaceProfile struct {
	ID                  int64
	WorkspaceID         int64
	PreferencesJSON     string
	BoundariesJSON      string
	CadenceDefaultsJSON string
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type UpsertWorkspaceProfileParams struct {
	WorkspaceID         int64
	PreferencesJSON     string
	BoundariesJSON      string
	CadenceDefaultsJSON string
}
type RecordMemorySummaryParams struct {
	ProjectID          *int64
	SourceTranscriptID *int64
	TaskID             *int64
	RunID              *int64
	Scope              string
	ScopeKey           string
	MemoryType         string
	Summary            string
	DetailsJSON        string
}

type UpdateMemorySummaryDetailsParams struct {
	MemoryID    int64
	DetailsJSON string
}

type ListMemorySummariesParams struct {
	ProjectID          *int64
	SourceTranscriptID *int64
	TaskID             *int64
	RunID              *int64
	Scope              string
	ScopeKey           string
	MemoryType         string
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

type RuntimeState struct {
	SingletonKey       string
	BootID             string
	Status             string
	PID                int
	StartedAt          time.Time
	ReadyAt            *time.Time
	LastHeartbeatAt    time.Time
	LastShutdownReason string
	LastError          string
	UpdatedAt          time.Time
}

type UpsertRuntimeStateParams struct {
	BootID             string
	Status             string
	PID                int
	StartedAt          time.Time
	ReadyAt            *time.Time
	LastHeartbeatAt    time.Time
	LastShutdownReason string
	LastError          string
	UpdatedAt          time.Time
}

type RuntimeStateWriteOptions struct {
	ExpectedBootID    string
	ExpectedUpdatedAt time.Time
	EventReason       string
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

type Delegation struct {
	ID              int64
	ParentTaskID    int64
	ParentRunID     *int64
	ProjectID       int64
	Scope           string
	DelegationKey   string
	Role            string
	ActionClass     string
	ActionKey       string
	MutationMode    string
	Status          string
	ConvergenceMode string
	ArtifactTarget  string
	Executor        string
	ChildTaskID     *int64
	ChildRunID      *int64
	WorktreeLeaseID *int64
	BranchName      string
	DetailsJSON     string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type CreateDelegationParams struct {
	ParentTaskID    int64
	ParentRunID     *int64
	ProjectID       int64
	Scope           string
	DelegationKey   string
	Role            string
	ActionClass     string
	ActionKey       string
	MutationMode    string
	Status          string
	ConvergenceMode string
	ArtifactTarget  string
	Executor        string
	WorktreeLeaseID *int64
	BranchName      string
	DetailsJSON     string
}

type UpdateDelegationStatusParams struct {
	DelegationID int64
	Status       string
}

type AttachDelegationChildTaskParams struct {
	DelegationID int64
	ChildTaskID  int64
	ChildRunID   *int64
}

type AttachDelegationWorktreeParams struct {
	DelegationID    int64
	WorktreeLeaseID *int64
	BranchName      string
}

type ListDelegationsParams struct {
	ProjectID       *int64
	ParentTaskID    *int64
	ChildTaskID     *int64
	WorktreeLeaseID *int64
	Status          string
	DelegationKey   string
}

type DelegationArtifact struct {
	ID           int64
	DelegationID int64
	ArtifactType string
	Summary      string
	DetailsJSON  string
	CreatedAt    time.Time
}

type CreateDelegationArtifactParams struct {
	DelegationID int64
	ArtifactType string
	Summary      string
	DetailsJSON  string
}

type ListDelegationArtifactsParams struct {
	DelegationID int64
	ArtifactType string
}

type RecordDelegationRetryEventParams struct {
	DelegationID int64
	EventType    runtimeevents.Type
	Reason       string
}

type RecordDelegationReuseEventParams struct {
	DelegationID int64
	Reason       string
}
