package sqlite

import (
	"encoding/json"
	"strings"
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
	ID                 int64
	ProjectID          int64
	Provider           string
	Repo               string
	Number             int
	Title              string
	BodyHash           string
	URL                string
	State              string
	LabelsJSON         string
	SyncStatus         string
	SyncCursor         string
	AcceptanceCriteria []string
	LastSyncedAt       time.Time
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type PullRequestHandoff struct {
	ID            int64
	ProjectID     int64
	Provider      string
	Repo          string
	Number        int
	URL           string
	State         string
	IssueURL      string
	Branch        string
	Title         string
	Summary       string
	Tests         []string
	Risks         []string
	Blockers      []string
	SelectedRoles []string
	ReviewState   string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type PullRequestReviewResult struct {
	ID        int64
	HandoffID int64
	Role      string
	State     string
	Summary   string
	Comments  []string
	Blockers  []string
	Outcome   string
	CreatedAt time.Time
	UpdatedAt time.Time
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
	ProjectID          int64
	Provider           string
	Repo               string
	Number             int
	Title              string
	BodyHash           string
	URL                string
	State              string
	LabelsJSON         string
	SyncStatus         string
	SyncCursor         string
	AcceptanceCriteria []string
}

type UpsertPullRequestHandoffParams struct {
	ProjectID     int64
	Provider      string
	Repo          string
	Number        int
	URL           string
	State         string
	IssueURL      string
	Branch        string
	Title         string
	Summary       string
	Tests         []string
	Risks         []string
	Blockers      []string
	SelectedRoles []string
	ReviewState   string
}

type ListPullRequestHandoffsParams struct {
	Repo        string
	ReviewState string
	ProjectID   *int64
}

type UpsertPullRequestReviewResultParams struct {
	HandoffID int64
	Role      string
	State     string
	Summary   string
	Comments  []string
	Blockers  []string
	Outcome   string
}

type RecordExternalGitHubIssueEventParams struct {
	ProjectID        int64
	ProjectKey       string
	ExternalIssueID  int64
	Provider         string
	Repo             string
	Number           int
	Action           string
	Title            string
	BodyHash         string
	URL              string
	LabelsJSON       string
	ExternalEventKey string
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
	AcceptanceCriteria    []string
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
	ExecutionIntent       string
	ExecutionIntentSource string
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
	GoalID                   *int64
	SuppressionReason        string
	RoutingNotes             string
	ReceivedAt               time.Time
	CreatedAt                time.Time
	UpdatedAt                time.Time
}

type IntakeAttachment struct {
	ID           int64
	IntakeItemID int64
	Kind         string
	Filename     string
	ContentType  string
	SizeBytes    int64
	SHA256       string
	Status       string
	Bytes        []byte
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type MobileDeviceStatus string

const (
	MobileDeviceStatusActive  MobileDeviceStatus = "active"
	MobileDeviceStatusRevoked MobileDeviceStatus = "revoked"
)

type MobileSessionStatus string

const (
	MobileSessionStatusActive  MobileSessionStatus = "active"
	MobileSessionStatusRevoked MobileSessionStatus = "revoked"
	MobileSessionStatusExpired MobileSessionStatus = "expired"
)

type MobilePushSubscriptionStatus string

const (
	MobilePushSubscriptionStatusActive  MobilePushSubscriptionStatus = "active"
	MobilePushSubscriptionStatusRevoked MobilePushSubscriptionStatus = "revoked"
)

type MobileDevice struct {
	ID           int64
	DeviceID     string
	DeviceName   string
	Status       MobileDeviceStatus
	RegisteredAt time.Time
	LastSeenAt   *time.Time
	RevokedAt    *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type MobileSession struct {
	ID          int64
	DeviceRowID int64
	TokenSHA256 string
	CSRFSHA256  string
	Status      MobileSessionStatus
	ExpiresAt   time.Time
	RevokedAt   *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type MobileAuthenticatedSession struct {
	Device  MobileDevice
	Session MobileSession
}

type MobilePushSubscription struct {
	ID             int64
	DeviceRowID    int64
	EndpointSHA256 string
	EndpointHost   string
	UserAgent      string
	Platform       string
	Status         MobilePushSubscriptionStatus
	RevokedAt      *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
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
	AcceptanceCriteria    []string
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
	ExecutionIntent       string
	ExecutionIntentSource string
	ArtifactsJSON         string
}

func EncodeAcceptanceCriteriaJSON(criteria []string) string {
	normalized := NormalizeAcceptanceCriteria(criteria)
	if len(normalized) == 0 {
		return "[]"
	}
	encoded, err := json.Marshal(normalized)
	if err != nil {
		return "[]"
	}
	return string(encoded)
}

func DecodeAcceptanceCriteriaJSON(raw string) []string {
	var criteria []string
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &criteria); err != nil {
		return nil
	}
	return NormalizeAcceptanceCriteria(criteria)
}

func EncodeStringListJSON(values []string) string {
	normalized := NormalizeStringList(values)
	if len(normalized) == 0 {
		return "[]"
	}
	encoded, err := json.Marshal(normalized)
	if err != nil {
		return "[]"
	}
	return string(encoded)
}

func DecodeStringListJSON(raw string) []string {
	var values []string
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &values); err != nil {
		return nil
	}
	return NormalizeStringList(values)
}

func NormalizeStringList(values []string) []string {
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			normalized = append(normalized, trimmed)
		}
	}
	return normalized
}

func NormalizeAcceptanceCriteria(criteria []string) []string {
	normalized := make([]string, 0, len(criteria))
	seen := make(map[string]struct{}, len(criteria))
	for _, criterion := range criteria {
		criterion = strings.TrimSpace(criterion)
		if criterion == "" {
			continue
		}
		if _, ok := seen[criterion]; ok {
			continue
		}
		seen[criterion] = struct{}{}
		normalized = append(normalized, criterion)
	}
	return normalized
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

type CreateIntakeAttachmentParams struct {
	IntakeItemID int64
	Kind         string
	Filename     string
	ContentType  string
	SizeBytes    int64
	SHA256       string
	Status       string
	Bytes        []byte
}

type ListIntakeAttachmentsParams struct {
	IntakeItemID int64
}

type ListIntakeItemsParams struct {
	WorkspaceID string
	Status      string
	Scope       string
	ScopeKey    string
}

type CreateMobileDeviceSessionParams struct {
	DeviceID    string
	DeviceName  string
	TokenSHA256 string
	CSRFSHA256  string
	ExpiresAt   time.Time
	Actor       string
}

type GetMobileSessionByTokenHashParams struct {
	TokenSHA256 string
}

type RevokeMobileDeviceParams struct {
	DeviceID string
	Actor    string
	Reason   string
}

type RecordMobileIntakeEventParams struct {
	DeviceID     string
	SessionID    int64
	IntakeItemID int64
	IntakeType   string
}

type RecordMobileApprovalEventParams struct {
	DeviceID   string
	SessionID  int64
	ApprovalID int64
	Action     string
}

type CreateMobilePushSubscriptionParams struct {
	DeviceID       string
	EndpointSHA256 string
	EndpointHost   string
	UserAgent      string
	Platform       string
}

type RevokeMobilePushSubscriptionParams struct {
	DeviceID       string
	SubscriptionID int64
	Actor          string
	Reason         string
}

type ProcessIntakeItemParams struct {
	ID                    int64
	Status                string
	Summary               string
	CanonicalIntakeItemID *int64
	GoalID                *int64
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
	GoalID           *int64
	GoalStatus       string
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
	DueAt             *time.Time
	SourceOccurredAt  *time.Time
	SourceEventID     *int64
	SourceEventType   string
	ReuseTaskID       *int64
	WorkKind          string
	ArtifactsJSON     string
}

type FireAutomationTriggerResult struct {
	Trigger         AutomationTrigger
	Materialization AutomationTriggerMaterialization
	WorkItem        Task
	CreatedWorkItem bool
}

type RecordAutomationTriggerTestParams struct {
	WorkspaceID        string
	Key                string
	Decision           string
	Reason             string
	Source             string
	MaterializationKey string
	DueAt              *time.Time
	SourceOccurredAt   *time.Time
	NextRun            *time.Time
	QuietHourEffect    string
	BatchKey           string
	BatchWindow        string
	ApprovalRequired   bool
	RecoveryState      string
	Mutates            bool
}

type RecordSchedulerTickParams struct {
	Now              time.Time
	Scope            string
	ProjectID        *int64
	DryRun           bool
	Mutates          bool
	Evaluated        int
	Materialized     int
	Deferred         int
	Errored          int
	WouldRun         int
	WouldDefer       int
	WouldBatch       int
	ApprovalRequired int
	RecoveryRan      bool
}

type DeferAutomationTriggerParams struct {
	WorkspaceID   string
	Key           string
	Reason        string
	DueAt         time.Time
	DeferredUntil time.Time
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

type UpdateTaskExecutionIntentParams struct {
	TaskID                int64
	ExecutionIntent       string
	ExecutionIntentSource string
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
	TaskID                 int64
	LastError              string
	NextEligibleAt         time.Time
	RecordDecision         bool
	Decision               string
	RetryEligible          bool
	RecoveryRecommendation string
	RetrySource            string
	ReviewQueueID          string
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
	ID                  int64
	TaskID              int64
	RunID               *int64
	Status              string
	RequestedAt         time.Time
	ResolvedAt          *time.Time
	DecisionBy          string
	Reason              string
	PolicySnapshotHash  string
	RuntimeSnapshotHash string
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

type BrowserMutationRequest struct {
	ID               int64
	ApprovalID       int64
	TaskID           int64
	ActionKind       string
	AllowedDomains   string
	StartURL         string
	BrowserSessionID *int64
	PayloadJSON      string
	PayloadHash      string
	CreatedAt        time.Time
}

type BlockTaskAndRequestBrowserMutationApprovalParams struct {
	TaskID             int64
	RunID              *int64
	RequestedBy        string
	ActionKind         string
	AllowedDomainsJSON string
	StartURL           string
	BrowserSessionID   *int64
	PayloadJSON        string
	PayloadHash        string
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
	RecoveryID        int64
	Playbook          string
	FaultKey          string
	ActionName        string
	Attempt           int
	Result            string
	Description       string
	ContractViolation *runtimeevents.RecoveryActionContractViolation
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
	ReviewDecision   string
	ReviewedAt       *time.Time
	ReviewedBy       string
	ReviewReason     string
	FollowOnTaskID   *int64
	FollowOnTaskKey  string
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

type ReviewSkillArtifactParams struct {
	ArtifactID        int64
	Decision          string
	Status            string
	ReviewedBy        string
	Reason            string
	Repeated          bool
	WorkCreated       bool
	FollowOnTaskID    *int64
	FollowOnTaskKey   string
	FollowOnTaskState string
}

type RecordDesignRequestCreatedEventParams struct {
	RequestArtifactID int64
	SkillKey          string
	Scope             string
	ProjectID         *int64
	Status            string
	ArtifactType      string
	Summary           string
	ExecutionProfile  string
}

type RecordDesignExecutionStartedEventParams struct {
	RequestArtifactID int64
	SkillKey          string
	Scope             string
	ProjectID         *int64
	ToolKey           string
	Summary           string
	ExecutionProfile  string
}

type RecordDesignArtifactCreatedEventParams struct {
	RequestArtifactID int64
	OutputArtifactID  int64
	SkillKey          string
	ProjectID         *int64
	Scope             string
	ArtifactType      string
	Status            string
	Summary           string
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

type ReviewContextPacketParams struct {
	PacketID    int64
	Status      string
	Decision    string
	ReviewedBy  string
	Reason      string
	Summary     string
	PayloadJSON string
}

type ReviewContextPacketResult struct {
	Packet   ContextPacket
	Repeated bool
}

type ReviewContextPacketAndRecordMemorySummaryParams struct {
	Review                ReviewContextPacketParams
	Memory                RecordMemorySummaryParams
	SourceContextPacketID int64
}

type ReviewContextPacketAndRecordMemorySummaryResult struct {
	Review ReviewContextPacketResult
	Memory *MemorySummary
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

type NotificationDevice struct {
	ID           int64      `json:"id"`
	WorkspaceID  int64      `json:"workspace_id"`
	DeviceKey    string     `json:"device_key"`
	Label        string     `json:"label"`
	EndpointHash string     `json:"endpoint_hash"`
	UserAgent    string     `json:"user_agent,omitempty"`
	Status       string     `json:"status"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	LastSeenAt   time.Time  `json:"last_seen_at"`
	RevokedAt    *time.Time `json:"revoked_at,omitempty"`
	RevokeReason string     `json:"revoke_reason,omitempty"`
}

type UpsertNotificationDeviceParams struct {
	WorkspaceID int64
	DeviceKey   string
	Label       string
	Endpoint    string
	P256DH      string
	Auth        string
	UserAgent   string
}

type ListNotificationDevicesParams struct {
	WorkspaceID    int64
	IncludeRevoked bool
}

type RevokeNotificationDeviceParams struct {
	WorkspaceID int64
	DeviceID    int64
	Reason      string
}

type Notification struct {
	ID                int64      `json:"id"`
	WorkspaceID       int64      `json:"workspace_id"`
	SourceEventID     *int64     `json:"source_event_id,omitempty"`
	NotificationType  string     `json:"notification_type"`
	Priority          string     `json:"priority"`
	Title             string     `json:"title"`
	Body              string     `json:"body"`
	Route             string     `json:"route"`
	Status            string     `json:"status"`
	PushPayloadJSON   string     `json:"push_payload_json,omitempty"`
	SuppressionReason string     `json:"suppression_reason,omitempty"`
	ReadAt            *time.Time `json:"read_at,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

type CreateNotificationParams struct {
	WorkspaceID       int64
	SourceEventID     *int64
	NotificationType  string
	Priority          string
	Title             string
	Body              string
	Route             string
	Status            string
	PushPayloadJSON   string
	SuppressionReason string
}

type ListNotificationsParams struct {
	WorkspaceID int64
	Limit       int
	UnreadOnly  bool
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
