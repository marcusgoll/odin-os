package sqlite

import "time"

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

type CreateProjectParams struct {
	Key           string
	Name          string
	Scope         string
	GitRoot       string
	DefaultBranch string
	GitHubRepo    string
	ManifestPath  string
}

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

type Task struct {
	ID           int64
	ProjectID    int64
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
	ProjectID   int64
	Key         string
	Title       string
	Status      string
	Scope       string
	RequestedBy string
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

type Approval struct {
	ID          int64
	TaskID      int64
	RunID       *int64
	ActionID    *int64
	PayloadHash string
	Status      string
	RequestedAt time.Time
	ResolvedAt  *time.Time
	DecisionBy  string
	Reason      string
}

type RequestApprovalParams struct {
	TaskID      int64
	RunID       *int64
	ActionID    *int64
	PayloadHash string
	Status      string
	RequestedBy string
}

type ResolveApprovalParams struct {
	ApprovalID int64
	Status     string
	DecisionBy string
	Reason     string
}

type Action struct {
	ID                 int64
	WorkflowKey        string
	WorkflowRunID      int64
	ActionType         string
	LifecycleState     string
	CurrentPayloadHash string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type ActionPayload struct {
	ID                   int64
	ActionID             int64
	PayloadSchema        string
	PayloadSchemaVersion int
	PayloadHash          string
	PayloadJSON          string
	SubmitPath           string
	ReadbackPath         string
	ProofRequirement     string
	CreatedAt            time.Time
}

type ActionEvidenceEvent struct {
	ID           int64
	ActionID     int64
	EventType    string
	EventVersion int
	PayloadHash  *string
	ApprovalID   *int64
	RunID        *int64
	Source       string
	EvidenceJSON string
	OccurredAt   time.Time
}

type CreateActionWithPayloadParams struct {
	WorkflowKey          string
	WorkflowRunID        int64
	ActionType           string
	PayloadSchema        string
	PayloadSchemaVersion int
	PayloadHash          string
	PayloadJSON          string
	SubmitPath           string
	ReadbackPath         string
	ProofRequirement     string
}

type AppendActionEvidenceParams struct {
	ActionID     int64
	EventType    string
	EventVersion int
	PayloadHash  string
	ApprovalID   *int64
	RunID        *int64
	Source       string
	EvidenceJSON string
}

type ListActionsParams struct {
	WorkflowKey   string
	WorkflowRunID *int64
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
	WorkspaceID         string
	PreferencesJSON     string
	BoundariesJSON      string
	CadenceDefaultsJSON string
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type UpsertWorkspaceProfileParams struct {
	WorkspaceID         string
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

type ListMemorySummariesParams struct {
	ProjectID          *int64
	SourceTranscriptID *int64
	TaskID             *int64
	RunID              *int64
	Scope              string
	ScopeKey           string
	MemoryType         string
}

type KnowledgeArtifact struct {
	ID           int64
	SHA256       string
	SizeBytes    int64
	SourceType   string
	MimeType     string
	ArtifactPath string
	OriginalPath string
	OCRRequired  bool
	RecordedAt   time.Time
}

type RecordKnowledgeArtifactParams struct {
	SHA256       string
	SizeBytes    int64
	SourceType   string
	MimeType     string
	ArtifactPath string
	OriginalPath string
	OCRRequired  bool
}

type MarkKnowledgeArtifactOCRRequiredParams struct {
	ArtifactID int64
}

type KnowledgeSource struct {
	ID                  int64
	Key                 string
	Title               string
	Scope               string
	ScopeKey            string
	Restricted          bool
	SourceKind          string
	SourceClass         string
	Lifecycle           string
	ManifestPath        string
	CurrentArtifactID   *int64
	CurrentExtractionID *int64
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type UpsertKnowledgeSourceParams struct {
	Key                 string
	Title               string
	Scope               string
	ScopeKey            string
	Restricted          bool
	SourceKind          string
	SourceClass         string
	Lifecycle           string
	ManifestPath        string
	CurrentArtifactID   *int64
	CurrentExtractionID *int64
}

type ListKnowledgeSourcesParams struct {
	Scope      string
	ScopeKey   string
	Lifecycle  string
	Restricted *bool
}

type KnowledgeExtraction struct {
	ID                     int64
	SourceID               int64
	ArtifactID             int64
	ExtractorName          string
	ExtractorVersion       string
	Status                 string
	FailureCode            string
	FailureSummary         string
	ExtractedTextHash      string
	NormalizedMarkdownPath string
	StartedAt              time.Time
	FinishedAt             *time.Time
}

type RecordKnowledgeExtractionParams struct {
	SourceID               int64
	ArtifactID             int64
	ExtractorName          string
	ExtractorVersion       string
	Status                 string
	Lifecycle              string
	FailureCode            string
	FailureSummary         string
	ExtractedTextHash      string
	NormalizedMarkdownPath string
	StartedAt              *time.Time
	FinishedAt             *time.Time
}

type KnowledgeChunk struct {
	ID           int64
	SourceID     int64
	ExtractionID int64
	Ordinal      int
	Text         string
	Anchor       string
	PageNumber   *int64
	Restricted   bool
	CreatedAt    time.Time
}

type RecordKnowledgeChunkParams struct {
	SourceID     int64
	ExtractionID int64
	Ordinal      int
	Text         string
	Anchor       string
	PageNumber   *int64
	Restricted   bool
}

type RecordReadyKnowledgeExtractionParams struct {
	SourceID               int64
	ArtifactID             int64
	Key                    string
	Title                  string
	Scope                  string
	ScopeKey               string
	Restricted             bool
	SourceKind             string
	SourceClass            string
	ManifestPath           string
	ExtractorName          string
	ExtractorVersion       string
	ExtractedTextHash      string
	NormalizedMarkdownPath string
	StartedAt              *time.Time
	FinishedAt             *time.Time
	Chunks                 []RecordKnowledgeChunkParams
	Topics                 []string
	Entities               []string
}

type ReadyKnowledgeExtraction struct {
	Source     KnowledgeSource
	Extraction KnowledgeExtraction
	Chunks     []KnowledgeChunk
}

type IndexKnowledgeChunkParams struct {
	ChunkID  int64
	Topics   []string
	Entities []string
}

type SearchKnowledgeChunksParams struct {
	Query    string
	Scope    string
	ScopeKey string
	Limit    int
}

type KnowledgeSearchResult struct {
	SourceID               int64
	SourceKey              string
	Title                  string
	ManifestPath           string
	ChunkID                int64
	ExtractionID           int64
	ArtifactID             int64
	ArtifactSHA256         string
	ExtractorName          string
	ExtractorVersion       string
	ExtractedTextHash      string
	NormalizedMarkdownPath string
	ExtractionFinishedAt   *time.Time
	Text                   string
	Anchor                 string
	PageNumber             *int64
	Restricted             bool
	Rank                   float64
}

type RestrictedKnowledgeUseApproval struct {
	ID           int64
	SourceID     int64
	UseType      string
	Reason       string
	Decision     string
	EvidenceJSON string
	DecidedBy    string
	DecidedAt    time.Time
}

type RecordRestrictedKnowledgeUseApprovalParams struct {
	SourceID     int64
	UseType      string
	Reason       string
	Decision     string
	EvidenceJSON string
	DecidedBy    string
	DecidedAt    *time.Time
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

type SupervisionControl struct {
	ID                   int64
	ModeKey              string
	Status               string
	KillSwitchActive     bool
	ConfigHash           string
	MaxConcurrentTasks   int
	DryRun               bool
	RequireHumanApproval bool
	UpdatedBy            string
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

type UpsertSupervisionControlParams struct {
	ModeKey              string
	Status               string
	KillSwitchActive     bool
	ConfigHash           string
	MaxConcurrentTasks   int
	DryRun               bool
	RequireHumanApproval bool
	UpdatedBy            string
}

type SupervisionQueueDecision struct {
	ID           int64
	ProjectID    int64
	Repo         string
	IssueNumber  int
	Decision     string
	Reason       string
	ConfigHash   string
	DecisionJSON string
	DecidedAt    time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type UpsertSupervisionQueueDecisionParams struct {
	ProjectID    int64
	Repo         string
	IssueNumber  int
	Decision     string
	Reason       string
	ConfigHash   string
	DecisionJSON string
}

type ListSupervisionQueueDecisionsParams struct {
	ProjectID *int64
	Repo      string
	Decision  string
}

type SupervisionDispatchClaim struct {
	ID          int64
	ProjectID   int64
	Repo        string
	IssueNumber int
	ClaimKey    string
	Status      string
	ConfigHash  string
	ClaimedBy   string
	ClaimedAt   time.Time
	ReleasedAt  *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
	Created     bool
}

type UpsertSupervisionDispatchClaimParams struct {
	ProjectID   int64
	Repo        string
	IssueNumber int
	ClaimKey    string
	Status      string
	ConfigHash  string
	ClaimedBy   string
}

type ReleaseSupervisionDispatchClaimParams struct {
	ClaimKey string
	Status   string
}

type ListSupervisionDispatchClaimsParams struct {
	ProjectID *int64
	Repo      string
	Status    string
}

type SupervisionRecoveryObservation struct {
	ID              int64
	ProjectID       *int64
	ModeKey         string
	ObservationType string
	Status          string
	Reason          string
	ConfigHash      string
	DetailsJSON     string
	ObservedAt      time.Time
	CreatedAt       time.Time
}

type CreateSupervisionRecoveryObservationParams struct {
	ProjectID       *int64
	ModeKey         string
	ObservationType string
	Status          string
	Reason          string
	ConfigHash      string
	DetailsJSON     string
}

type ListSupervisionRecoveryObservationsParams struct {
	ProjectID *int64
	ModeKey   string
	Status    string
	Limit     int
}
