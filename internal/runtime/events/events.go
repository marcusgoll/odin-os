package events

import (
	"encoding/json"
	"time"
)

type StreamType string

const (
	StreamService            StreamType = "service"
	StreamProject            StreamType = "project"
	StreamTask               StreamType = "task"
	StreamRun                StreamType = "run"
	StreamApproval           StreamType = "approval"
	StreamIncident           StreamType = "incident"
	StreamRecovery           StreamType = "recovery"
	StreamRegistryVersion    StreamType = "registry_version"
	StreamExecutorHealth     StreamType = "executor_health"
	StreamContextPacket      StreamType = "context_packet"
	StreamConversation       StreamType = "conversation_transcript"
	StreamMemorySummary      StreamType = "memory_summary"
	StreamIntakeItem         StreamType = "intake_item"
	StreamExternalEvent      StreamType = "external_event"
	StreamAutomationTrigger  StreamType = "automation_trigger"
	StreamLearningProposal   StreamType = "learning_proposal"
	StreamLearningEvaluation StreamType = "learning_evaluation"
	StreamLearningPromotion  StreamType = "learning_promotion"
	StreamSkill              StreamType = "skill"
	StreamDelegation         StreamType = "delegation"
	StreamCapability         StreamType = "capability"
	StreamFollowUp           StreamType = "follow_up"
	StreamGoal               StreamType = "goal"
	StreamBrowserSession     StreamType = "browser_session"
)

type Type string

const (
	EventServiceLifecycleChanged            Type = "service.lifecycle_changed"
	EventServiceHeartbeatRecorded           Type = "service.heartbeat_recorded"
	EventProjectCreated                     Type = "project.created"
	EventTaskCreated                        Type = "task.created"
	EventTaskDispatchRequested              Type = "task.dispatch_requested"
	EventTaskRetryEvaluated                 Type = "task.retry_evaluated"
	EventTaskRecoveryRecommended            Type = "task.recovery_recommended"
	EventTaskStatusChanged                  Type = "task.status_changed"
	EventTaskQueueStateChanged              Type = "task.queue_state_changed"
	EventRunStarted                         Type = "run.started"
	EventRunStatusChanged                   Type = "run.status_changed"
	EventRunExecutionClaimed                Type = "run.execution_claimed"
	EventRunFinished                        Type = "run.finished"
	EventApprovalRequested                  Type = "approval.requested"
	EventApprovalResolved                   Type = "approval.resolved"
	EventIncidentOpened                     Type = "incident.opened"
	EventIncidentResolved                   Type = "incident.resolved"
	EventIncidentEscalated                  Type = "incident.escalated"
	EventRecoveryStarted                    Type = "recovery.started"
	EventRecoveryActionExecuted             Type = "recovery.action_executed"
	EventRecoveryCompleted                  Type = "recovery.completed"
	EventRegistryVersionRecorded            Type = "registry_version.recorded"
	EventExecutorHealthRecorded             Type = "executor_health.recorded"
	EventContextPacketCreated               Type = "context_packet.created"
	EventContextPacketReviewed              Type = "context_packet.reviewed"
	EventConversationTranscriptRecorded     Type = "conversation.transcript_recorded"
	EventMemorySummaryRecorded              Type = "memory.summary_recorded"
	EventMemorySummaryUpdated               Type = "memory.summary_updated"
	EventReviewApproved                     Type = "review.approved"
	EventReviewRejected                     Type = "review.rejected"
	EventIntakeItemCreated                  Type = "intake.item_created"
	EventIntakeProcessingStarted            Type = "intake.processing_started"
	EventIntakeClassified                   Type = "intake.classified"
	EventIntakeDedupeReviewed               Type = "intake.dedupe_reviewed"
	EventIntakeRouted                       Type = "intake.routed"
	EventIntakeProcessed                    Type = "intake.processed"
	EventIntakeRoutedToGoal                 Type = "intake.routed_to_goal"
	EventIntakeDraftArtifactCreated         Type = "intake.draft_artifact_created"
	EventIntakeClarificationNeeded          Type = "intake.clarification_needed"
	EventIntakeDuplicateLinkedOrSuppressed  Type = "intake.duplicate_linked_or_suppressed"
	EventIntakeReviewAccepted               Type = "intake.review_accepted"
	EventIntakeReviewRejected               Type = "intake.review_rejected"
	EventIntakeReviewClarificationRequested Type = "intake.review_clarification_requested"
	EventIntakeReviewArchived               Type = "intake.review_archived"
	EventIntakeReviewDuplicateAcknowledged  Type = "intake.review_duplicate_acknowledged"
	EventIntakeReviewApprovalRequired       Type = "intake.review_approval_required"
	EventIntakeApprovalApproved             Type = "intake.approval_approved"
	EventIntakeApprovalDenied               Type = "intake.approval_denied"
	EventExternalGitHubIssue                Type = "external.github.issue"
	EventAutomationTriggerCreated           Type = "automation_trigger.created"
	EventAutomationTriggerFireRequested     Type = "automation_trigger.fire_requested"
	EventAutomationTriggerEvaluated         Type = "automation_trigger.evaluated"
	EventAutomationTriggerMaterialized      Type = "automation_trigger.materialized"
	EventAutomationTriggerDeferred          Type = "automation_trigger.deferred"
	EventAutomationTriggerErrored           Type = "automation_trigger.errored"
	EventAutomationTriggerStatusChanged     Type = "automation_trigger.status_changed"
	EventProjectTransitionChanged           Type = "project.transition_changed"
	EventProjectShadowObservationRecorded   Type = "project.shadow_observation_recorded"
	EventProjectCompareReportRecorded       Type = "project.compare_report_recorded"
	EventProjectTransitionDenied            Type = "project.transition_denied"
	EventLearningProposalCreated            Type = "learning.proposal_created"
	EventLearningProposalSubmitted          Type = "learning.proposal_submitted"
	EventLearningProposalPromotionReady     Type = "learning.proposal_promotion_ready"
	EventLearningProposalRejected           Type = "learning.proposal_rejected"
	EventLearningEvaluationRecorded         Type = "learning.evaluation_recorded"
	EventLearningPromotionApplied           Type = "learning.promotion_applied"
	EventLearningPromotionRolledBack        Type = "learning.promotion_rolled_back"
	EventSkillLifecycleRecorded             Type = "skill.lifecycle_recorded"
	EventSkillArtifactRecorded              Type = "skill.artifact_recorded"
	EventSkillArtifactReviewed              Type = "skill.artifact_reviewed"
	EventDelegationCreated                  Type = "delegation.created"
	EventDelegationCreateReused             Type = "delegation.create_reused"
	EventDelegationStatusChanged            Type = "delegation.status_changed"
	EventDelegationChildAttached            Type = "delegation.child_attached"
	EventDelegationArtifactRecorded         Type = "delegation.artifact_recorded"
	EventDelegationRetryRequested           Type = "delegation.retry_requested"
	EventDelegationRetrySkipped             Type = "delegation.retry_skipped"
	EventCapabilitySnapshotPublished        Type = "capability.snapshot_published"
	EventCapabilitySnapshotRejected         Type = "capability.snapshot_rejected"
	EventFollowUpMaterialized               Type = "follow_up.materialized"
	EventFollowUpPaused                     Type = "follow_up.paused"
	EventGoalCreated                        Type = "goal.created"
	EventGoalUpdated                        Type = "goal.updated"
	EventGoalStatusChanged                  Type = "goal.status_changed"
	EventGoalRunnerObserved                 Type = "goal_runner.observed"
	EventGoalRunStarted                     Type = "goal_run.started"
	EventGoalRunStatusChanged               Type = "goal_run.status_changed"
	EventGoalRunFinished                    Type = "goal_run.finished"
	EventGoalBlockerRecorded                Type = "goal.blocker_recorded"
	EventGoalEvidenceRecorded               Type = "goal.evidence_recorded"
	EventBrowserSessionCreated              Type = "browser.session_created"
	EventBrowserSessionStatusChanged        Type = "browser.session_status_changed"
	EventBrowserSessionVerified             Type = "browser.session_verified"
	EventBrowserSessionRevoked              Type = "browser.session_revoked"
	EventBrowserSessionProfilePrepared      Type = "browser.session_profile_prepared"
	EventBrowserSessionLoginRequested       Type = "browser.session_login_requested"
	EventBrowserSessionLoginCompleted       Type = "browser.session_login_completed"
	EventBrowserSessionLoginExpired         Type = "browser.session_login_expired"
	EventBrowserHandoffRunnerRequested      Type = "browser.handoff_runner_requested"
	EventBrowserHandoffRunnerStarted        Type = "browser.handoff_runner_started"
	EventBrowserHandoffRunnerExpired        Type = "browser.handoff_runner_expired"
	EventBrowserHandoffRunnerCancelled      Type = "browser.handoff_runner_cancelled"
	EventBrowserHandoffRunnerCompleted      Type = "browser.handoff_runner_completed"
	EventBrowserHandoffRunnerFailed         Type = "browser.handoff_runner_failed"
	EventBrowserProfileCaptureRequested     Type = "browser.profile_capture_requested"
	EventBrowserProfileEncrypted            Type = "browser.profile_encrypted"
	EventBrowserProfileAttached             Type = "browser.profile_attached"
	EventBrowserProfileRevoked              Type = "browser.profile_revoked"
	EventBrowserProfileExpired              Type = "browser.profile_expired"
	EventBrowserProfileCleaned              Type = "browser.profile_cleaned"
	EventBrowserProfileCleanupFailed        Type = "browser.profile_cleanup_failed"
)

const (
	EventBrowserProfileMaterialized           Type = "browser.profile_materialized"
	EventBrowserProfileMaterializationCleaned Type = "browser.profile_materialization_cleaned"
)

const (
	SkillLifecycleErrorUnknownPermission            = "unknown_permission"
	SkillLifecycleErrorMutationRequiresProjectScope = "mutation_requires_project_scope"
	SkillLifecycleErrorTransitionDenied             = "transition_denied"
	SkillLifecycleErrorApprovalRequired             = "approval_required"
)

type Record struct {
	ID         int64
	StreamType StreamType
	StreamID   int64
	Type       Type
	Version    int
	Scope      string
	ProjectID  *int64
	TaskID     *int64
	RunID      *int64
	Payload    json.RawMessage
	OccurredAt time.Time
}

type ServiceLifecyclePayload struct {
	BootID string `json:"boot_id"`
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
	PID    int    `json:"pid"`
}

type ServiceHeartbeatPayload struct {
	BootID string `json:"boot_id"`
	Status string `json:"status"`
	PID    int    `json:"pid"`
}

type ProjectCreatedPayload struct {
	Key           string `json:"key"`
	Name          string `json:"name"`
	Scope         string `json:"scope"`
	GitRoot       string `json:"git_root"`
	DefaultBranch string `json:"default_branch"`
	GitHubRepo    string `json:"github_repo,omitempty"`
	ManifestPath  string `json:"manifest_path"`
}

type TaskCreatedPayload struct {
	Key                   string `json:"key"`
	Title                 string `json:"title"`
	ActionKey             string `json:"action_key,omitempty"`
	Status                string `json:"status"`
	Scope                 string `json:"scope"`
	RequestedBy           string `json:"requested_by"`
	NextEligibleAt        string `json:"next_eligible_at,omitempty"`
	Priority              int    `json:"priority,omitempty"`
	RetryCount            int    `json:"retry_count,omitempty"`
	MaxAttempts           int    `json:"max_attempts,omitempty"`
	LastError             string `json:"last_error,omitempty"`
	BlockedReason         string `json:"blocked_reason,omitempty"`
	ExecutionIntent       string `json:"execution_intent,omitempty"`
	ExecutionIntentSource string `json:"execution_intent_source,omitempty"`
}

type TaskDispatchRequestedPayload struct {
	TaskID                int64  `json:"task_id"`
	Executor              string `json:"executor"`
	Attempt               int    `json:"attempt"`
	Status                string `json:"status"`
	ExecutionIntent       string `json:"execution_intent,omitempty"`
	ExecutionIntentSource string `json:"execution_intent_source,omitempty"`
}

type TaskRetryEvaluatedPayload struct {
	TaskID                 int64  `json:"task_id"`
	Status                 string `json:"status"`
	Requested              bool   `json:"requested"`
	Source                 string `json:"source,omitempty"`
	QueueID                string `json:"queue_id,omitempty"`
	Decision               string `json:"decision"`
	RetryEligible          bool   `json:"retry_eligible"`
	RetryCount             int    `json:"retry_count"`
	MaxAttempts            int    `json:"max_attempts"`
	NextEligibleAt         string `json:"next_eligible_at,omitempty"`
	LastError              string `json:"last_error,omitempty"`
	RecoveryRecommendation string `json:"recovery_recommendation,omitempty"`
}

type TaskRecoveryRecommendedPayload struct {
	TaskID                 int64  `json:"task_id"`
	Status                 string `json:"status"`
	Source                 string `json:"source,omitempty"`
	Decision               string `json:"decision"`
	RetryEligible          bool   `json:"retry_eligible"`
	RetryCount             int    `json:"retry_count"`
	MaxAttempts            int    `json:"max_attempts"`
	LastError              string `json:"last_error,omitempty"`
	RecoveryRecommendation string `json:"recovery_recommendation,omitempty"`
}

type TaskStatusChangedPayload struct {
	PreviousStatus string `json:"previous_status"`
	Status         string `json:"status"`
	Summary        string `json:"summary,omitempty"`
	TerminalReason string `json:"terminal_reason,omitempty"`
	ArtifactsJSON  string `json:"artifacts_json,omitempty"`
}

type GoalCreatedPayload struct {
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Status      string `json:"status"`
	CreatedBy   string `json:"created_by,omitempty"`
	Source      string `json:"source,omitempty"`
}

type GoalUpdatedPayload struct {
	PreviousTitle       string `json:"previous_title,omitempty"`
	Title               string `json:"title,omitempty"`
	PreviousDescription string `json:"previous_description,omitempty"`
	Description         string `json:"description,omitempty"`
	Status              string `json:"status"`
	Actor               string `json:"actor,omitempty"`
	Reason              string `json:"reason,omitempty"`
}

type GoalStatusChangedPayload struct {
	PreviousStatus string `json:"previous_status"`
	Status         string `json:"status"`
	Actor          string `json:"actor,omitempty"`
	Reason         string `json:"reason,omitempty"`
}

type GoalRunnerObservedPayload struct {
	GoalID int64  `json:"goal_id"`
	Status string `json:"status"`
	Action string `json:"action"`
	Reason string `json:"reason,omitempty"`
	Actor  string `json:"actor,omitempty"`
}

type GoalRunStartedPayload struct {
	GoalRunID int64  `json:"goal_run_id"`
	Status    string `json:"status"`
	Executor  string `json:"executor,omitempty"`
	Attempt   int    `json:"attempt"`
}

type GoalRunStatusChangedPayload struct {
	GoalRunID      int64  `json:"goal_run_id"`
	PreviousStatus string `json:"previous_status"`
	Status         string `json:"status"`
	Attempts       int    `json:"attempts"`
	MaxAttempts    int    `json:"max_attempts"`
	LeaseOwner     string `json:"lease_owner,omitempty"`
	NextWakeAt     string `json:"next_wake_at,omitempty"`
	LastProgressAt string `json:"last_progress_at,omitempty"`
	EndedAt        string `json:"ended_at,omitempty"`
	Summary        string `json:"summary,omitempty"`
}

type GoalRunFinishedPayload struct {
	GoalRunID int64  `json:"goal_run_id"`
	Status    string `json:"status"`
	Summary   string `json:"summary,omitempty"`
}

type GoalBlockerRecordedPayload struct {
	BlockerID   int64  `json:"blocker_id"`
	Status      string `json:"status"`
	BlockerType string `json:"blocker_type,omitempty"`
	Summary     string `json:"summary"`
	CreatedBy   string `json:"created_by,omitempty"`
}

type GoalEvidenceRecordedPayload struct {
	EvidenceID   int64  `json:"evidence_id"`
	GoalRunID    *int64 `json:"goal_run_id,omitempty"`
	EvidenceType string `json:"evidence_type"`
	Summary      string `json:"summary"`
	URI          string `json:"uri,omitempty"`
	CreatedBy    string `json:"created_by,omitempty"`
}

type ReviewApprovedPayload struct {
	ReviewID   string `json:"review_id"`
	SourceType string `json:"source_type"`
	SourceID   int64  `json:"source_id"`
	GoalID     int64  `json:"goal_id,omitempty"`
	Status     string `json:"status"`
	Actor      string `json:"actor,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

type ReviewRejectedPayload struct {
	ReviewID   string `json:"review_id"`
	SourceType string `json:"source_type"`
	SourceID   int64  `json:"source_id"`
	GoalID     int64  `json:"goal_id,omitempty"`
	BlockerID  int64  `json:"blocker_id,omitempty"`
	Status     string `json:"status"`
	Actor      string `json:"actor,omitempty"`
	Reason     string `json:"reason"`
}

type BrowserSessionCreatedPayload struct {
	SessionID            int64  `json:"session_id"`
	Name                 string `json:"name"`
	Domain               string `json:"domain"`
	AccountHint          string `json:"account_hint,omitempty"`
	PermissionTier       string `json:"permission_tier"`
	Status               string `json:"status"`
	ProfileStoragePolicy string `json:"profile_storage_policy"`
	ProfilePath          string `json:"profile_path"`
	ExpiresAt            string `json:"expires_at,omitempty"`
}

type BrowserSessionStatusChangedPayload struct {
	SessionID      int64  `json:"session_id"`
	PreviousStatus string `json:"previous_status"`
	Status         string `json:"status"`
	Actor          string `json:"actor,omitempty"`
	Reason         string `json:"reason,omitempty"`
	LastVerifiedAt string `json:"last_verified_at,omitempty"`
	ExpiresAt      string `json:"expires_at,omitempty"`
}

type BrowserSessionVerifiedPayload struct {
	SessionID      int64  `json:"session_id"`
	PreviousStatus string `json:"previous_status"`
	Status         string `json:"status"`
	Actor          string `json:"actor,omitempty"`
	Reason         string `json:"reason,omitempty"`
	LastVerifiedAt string `json:"last_verified_at"`
	LoginRequestID int64  `json:"login_request_id,omitempty"`
}

type BrowserSessionRevokedPayload struct {
	SessionID      int64  `json:"session_id"`
	PreviousStatus string `json:"previous_status"`
	Status         string `json:"status"`
	Actor          string `json:"actor,omitempty"`
	Reason         string `json:"reason"`
	RevokedAt      string `json:"revoked_at"`
}

type BrowserSessionProfilePreparedPayload struct {
	SessionID            int64  `json:"session_id"`
	Status               string `json:"status"`
	ProfileStoragePolicy string `json:"profile_storage_policy"`
	ProfilePath          string `json:"profile_path"`
	Created              bool   `json:"created"`
	Actor                string `json:"actor,omitempty"`
}

type BrowserSessionLoginRequestedPayload struct {
	SessionID      int64  `json:"session_id"`
	LoginRequestID int64  `json:"login_request_id"`
	Status         string `json:"status"`
	HandoffID      string `json:"handoff_id,omitempty"`
	HandoffURL     string `json:"handoff_url,omitempty"`
	ExpiresAt      string `json:"expires_at"`
}

type BrowserSessionLoginCompletedPayload struct {
	SessionID      int64  `json:"session_id"`
	LoginRequestID int64  `json:"login_request_id"`
	PreviousStatus string `json:"previous_status"`
	Status         string `json:"status"`
	CompletedAt    string `json:"completed_at"`
}

type BrowserSessionLoginExpiredPayload struct {
	SessionID      int64  `json:"session_id"`
	LoginRequestID int64  `json:"login_request_id"`
	PreviousStatus string `json:"previous_status"`
	Status         string `json:"status"`
	ExpiresAt      string `json:"expires_at"`
}

type BrowserHandoffRunnerLifecyclePayload struct {
	ID             int64  `json:"id"`
	SessionID      int64  `json:"session_id"`
	LoginRequestID int64  `json:"login_request_id"`
	HandoffID      string `json:"handoff_id"`
	RunnerID       string `json:"runner_id,omitempty"`
	ProcessID      int64  `json:"process_id,omitempty"`
	PreviousStatus string `json:"previous_status"`
	Status         string `json:"status"`
	ViewerURL      string `json:"viewer_url,omitempty"`
	BindAddr       string `json:"bind_addr,omitempty"`
	PrivateBaseURL string `json:"private_base_url,omitempty"`
	PublicBaseURL  string `json:"public_base_url,omitempty"`
	ExpiresAt      string `json:"expires_at,omitempty"`
	StartedAt      string `json:"started_at,omitempty"`
	ExitedAt       string `json:"exited_at,omitempty"`
	CompletedAt    string `json:"completed_at,omitempty"`
	CancelledAt    string `json:"cancelled_at,omitempty"`
	ErrorCode      string `json:"error_code,omitempty"`
	ErrorMessage   string `json:"error_message,omitempty"`
	Actor          string `json:"actor,omitempty"`
	Reason         string `json:"reason,omitempty"`
}

type TaskQueueStateChangedPayload struct {
	PreviousStatus        string `json:"previous_status"`
	Status                string `json:"status"`
	NextEligibleAt        string `json:"next_eligible_at"`
	Priority              int    `json:"priority"`
	RetryCount            int    `json:"retry_count"`
	MaxAttempts           int    `json:"max_attempts"`
	LastError             string `json:"last_error,omitempty"`
	BlockedReason         string `json:"blocked_reason,omitempty"`
	ExecutionIntent       string `json:"execution_intent,omitempty"`
	ExecutionIntentSource string `json:"execution_intent_source,omitempty"`
}

type FollowUpMaterializedPayload struct {
	ObligationID  int64  `json:"obligation_id"`
	TaskID        int64  `json:"task_id"`
	OccurrenceKey string `json:"occurrence_key"`
	TaskStatus    string `json:"task_status"`
	Reused        bool   `json:"reused"`
}

type FollowUpPausedPayload struct {
	ObligationID     int64  `json:"obligation_id"`
	Status           string `json:"status"`
	InitiativeStatus string `json:"initiative_status,omitempty"`
}

type RunStartedPayload struct {
	TaskID   int64  `json:"task_id"`
	Executor string `json:"executor"`
	Attempt  int    `json:"attempt"`
	Status   string `json:"status"`
}

type RunStatusChangedPayload struct {
	PreviousStatus string `json:"previous_status"`
	Status         string `json:"status"`
}

type RunExecutionClaimedPayload struct {
	TaskID         int64  `json:"task_id"`
	PreviousStatus string `json:"previous_status"`
	Status         string `json:"status"`
	Actor          string `json:"actor"`
}

type RunFinishedPayload struct {
	Status         string `json:"status"`
	Summary        string `json:"summary"`
	TerminalReason string `json:"terminal_reason,omitempty"`
	ArtifactsJSON  string `json:"artifacts_json,omitempty"`
}

type ApprovalRequestedPayload struct {
	TaskID      int64  `json:"task_id"`
	RunID       *int64 `json:"run_id,omitempty"`
	Status      string `json:"status"`
	RequestedBy string `json:"requested_by"`
}

type ApprovalResolvedPayload struct {
	Status     string `json:"status"`
	DecisionBy string `json:"decision_by"`
	Reason     string `json:"reason"`
}

type IncidentOpenedPayload struct {
	Severity string `json:"severity"`
	Status   string `json:"status"`
	Summary  string `json:"summary"`
}

type IncidentResolvedPayload struct {
	PreviousStatus string `json:"previous_status"`
	Status         string `json:"status"`
	Reason         string `json:"reason"`
}

type IncidentEscalatedPayload struct {
	PreviousStatus string `json:"previous_status"`
	Status         string `json:"status"`
	Reason         string `json:"reason"`
}

type RecoveryStartedPayload struct {
	Status   string `json:"status"`
	Strategy string `json:"strategy"`
}

type RecoveryActionExecutedPayload struct {
	Playbook    string `json:"playbook"`
	FaultKey    string `json:"fault_key"`
	ActionName  string `json:"action_name"`
	Attempt     int    `json:"attempt"`
	Result      string `json:"result"`
	Description string `json:"description,omitempty"`
}

type RecoveryCompletedPayload struct {
	Status string `json:"status"`
}

type RegistryVersionRecordedPayload struct {
	Source      string `json:"source"`
	VersionHash string `json:"version_hash"`
	Notes       string `json:"notes,omitempty"`
}

type ExecutorHealthRecordedPayload struct {
	Executor  string `json:"executor"`
	Status    string `json:"status"`
	LatencyMS int64  `json:"latency_ms"`
}

type ContextPacketCreatedPayload struct {
	PacketKind  string `json:"packet_kind"`
	PacketScope string `json:"packet_scope"`
	Trigger     string `json:"trigger"`
	Status      string `json:"status"`
	Summary     string `json:"summary"`
}

type ContextPacketReviewedPayload struct {
	PacketID       int64  `json:"packet_id"`
	PacketKind     string `json:"packet_kind"`
	PacketScope    string `json:"packet_scope"`
	Decision       string `json:"decision"`
	Status         string `json:"status"`
	PreviousStatus string `json:"previous_status"`
	ReviewedBy     string `json:"reviewed_by,omitempty"`
	Reason         string `json:"reason,omitempty"`
	Repeated       bool   `json:"repeated"`
}

type ConversationTranscriptRecordedPayload struct {
	Scope    string `json:"scope"`
	ScopeKey string `json:"scope_key"`
	Mode     string `json:"mode"`
	Executor string `json:"executor,omitempty"`
	TaskID   *int64 `json:"task_id,omitempty"`
	RunID    *int64 `json:"run_id,omitempty"`
}

type MemorySummaryRecordedPayload struct {
	Scope              string `json:"scope"`
	ScopeKey           string `json:"scope_key"`
	MemoryType         string `json:"memory_type"`
	SourceTranscriptID *int64 `json:"source_transcript_id,omitempty"`
	TaskID             *int64 `json:"task_id,omitempty"`
	RunID              *int64 `json:"run_id,omitempty"`
}

type MemorySummaryUpdatedPayload struct {
	Scope              string `json:"scope"`
	ScopeKey           string `json:"scope_key"`
	MemoryType         string `json:"memory_type"`
	SourceTranscriptID *int64 `json:"source_transcript_id,omitempty"`
	TaskID             *int64 `json:"task_id,omitempty"`
	RunID              *int64 `json:"run_id,omitempty"`
}

type IntakeItemCreatedPayload struct {
	WorkspaceID         string `json:"workspace_id"`
	SourceFamily        string `json:"source_family"`
	ExternalObjectID    string `json:"external_object_id,omitempty"`
	EventKind           string `json:"event_kind"`
	Subject             string `json:"subject"`
	DedupeKey           string `json:"dedupe_key"`
	DedupeRecipeVersion string `json:"dedupe_recipe_version"`
	Status              string `json:"status"`
	Scope               string `json:"scope,omitempty"`
	ScopeKey            string `json:"scope_key,omitempty"`
}

type IntakeProcessingPayload struct {
	IntakeItemID          int64  `json:"intake_item_id"`
	Status                string `json:"status,omitempty"`
	Stage                 string `json:"stage"`
	Result                string `json:"result,omitempty"`
	RoutedOutcome         string `json:"routed_outcome,omitempty"`
	ExecutionIntent       string `json:"execution_intent,omitempty"`
	ExecutionIntentSource string `json:"execution_intent_source,omitempty"`
	CanonicalIntakeID     *int64 `json:"canonical_intake_id,omitempty"`
	GoalID                *int64 `json:"goal_id,omitempty"`
	DraftArtifactKind     string `json:"draft_artifact_kind,omitempty"`
	ClarificationState    string `json:"clarification_state,omitempty"`
}

type IntakeReviewDecisionPayload struct {
	IntakeItemID      int64  `json:"intake_item_id"`
	Decision          string `json:"decision"`
	Status            string `json:"status"`
	PreviousStatus    string `json:"previous_status,omitempty"`
	WorkCreated       bool   `json:"work_created"`
	ApprovalRequired  bool   `json:"approval_required,omitempty"`
	PolicyDecision    string `json:"policy_decision,omitempty"`
	PolicyReason      string `json:"policy_reason,omitempty"`
	WorkItemID        *int64 `json:"work_item_id,omitempty"`
	WorkItemKey       string `json:"work_item_key,omitempty"`
	CanonicalIntakeID *int64 `json:"canonical_intake_id,omitempty"`
}

type ExternalGitHubIssuePayload struct {
	Source           string `json:"source"`
	Provider         string `json:"provider"`
	Repo             string `json:"repo"`
	Number           int    `json:"number"`
	Action           string `json:"action"`
	Status           string `json:"status"`
	ProjectKey       string `json:"project_key"`
	ExternalEventKey string `json:"external_event_key"`
	ExternalIssueID  int64  `json:"external_issue_id"`
	Title            string `json:"title"`
	URL              string `json:"url,omitempty"`
	BodyHash         string `json:"body_hash,omitempty"`
	LabelsJSON       string `json:"labels_json,omitempty"`
}

type AutomationTriggerCreatedPayload struct {
	WorkspaceID   string `json:"workspace_id"`
	Key           string `json:"key"`
	InitiativeKey string `json:"initiative_key,omitempty"`
	Kind          string `json:"kind"`
	Status        string `json:"status"`
}

type AutomationTriggerEnvelope struct {
	Source           string                             `json:"source"`
	TriggerType      string                             `json:"trigger_type"`
	DedupeKey        string                             `json:"dedupe_key"`
	OccurredAt       string                             `json:"occurred_at"`
	DueAt            string                             `json:"due_at,omitempty"`
	SourceOccurredAt string                             `json:"source_occurred_at,omitempty"`
	RecoveryState    string                             `json:"recovery_state"`
	Schedule         *AutomationTriggerScheduleEnvelope `json:"schedule,omitempty"`
	Risk             *AutomationTriggerRiskEnvelope     `json:"risk,omitempty"`
}

type AutomationTriggerScheduleEnvelope struct {
	Summary       string `json:"summary,omitempty"`
	Cadence       string `json:"cadence,omitempty"`
	Cron          string `json:"cron,omitempty"`
	QuietHours    string `json:"quiet_hours,omitempty"`
	QuietTimezone string `json:"quiet_timezone,omitempty"`
}

type AutomationTriggerRiskEnvelope struct {
	ExecutionIntent  string `json:"execution_intent,omitempty"`
	ApprovalRequired bool   `json:"approval_required"`
}

type AutomationTriggerFireRequestedPayload struct {
	WorkspaceID        string                     `json:"workspace_id"`
	Key                string                     `json:"key"`
	Source             string                     `json:"source,omitempty"`
	MaterializationKey string                     `json:"materialization_key"`
	Reason             string                     `json:"reason,omitempty"`
	RequestedBy        string                     `json:"requested_by,omitempty"`
	SourceEventID      *int64                     `json:"source_event_id,omitempty"`
	SourceEventType    string                     `json:"source_event_type,omitempty"`
	Envelope           *AutomationTriggerEnvelope `json:"envelope,omitempty"`
}

type AutomationTriggerEvaluatedPayload struct {
	WorkspaceID        string                     `json:"workspace_id"`
	Key                string                     `json:"key"`
	Source             string                     `json:"source,omitempty"`
	MaterializationKey string                     `json:"materialization_key"`
	Status             string                     `json:"status"`
	CreatedWorkItem    bool                       `json:"created_work_item"`
	SourceEventID      *int64                     `json:"source_event_id,omitempty"`
	SourceEventType    string                     `json:"source_event_type,omitempty"`
	Envelope           *AutomationTriggerEnvelope `json:"envelope,omitempty"`
}

type AutomationTriggerMaterializedPayload struct {
	WorkspaceID        string                     `json:"workspace_id"`
	Key                string                     `json:"key"`
	Source             string                     `json:"source,omitempty"`
	MaterializationKey string                     `json:"materialization_key"`
	TaskID             int64                      `json:"task_id"`
	TaskKey            string                     `json:"task_key"`
	RequestedBy        string                     `json:"requested_by,omitempty"`
	SourceEventID      *int64                     `json:"source_event_id,omitempty"`
	SourceEventType    string                     `json:"source_event_type,omitempty"`
	Envelope           *AutomationTriggerEnvelope `json:"envelope,omitempty"`
}

type AutomationTriggerDeferredPayload struct {
	WorkspaceID   string                     `json:"workspace_id"`
	Key           string                     `json:"key"`
	Reason        string                     `json:"reason"`
	DueAt         string                     `json:"due_at"`
	DeferredUntil string                     `json:"deferred_until"`
	Status        string                     `json:"status"`
	Envelope      *AutomationTriggerEnvelope `json:"envelope,omitempty"`
}

type AutomationTriggerErroredPayload struct {
	WorkspaceID string                     `json:"workspace_id"`
	Key         string                     `json:"key"`
	Reason      string                     `json:"reason,omitempty"`
	Error       string                     `json:"error"`
	Envelope    *AutomationTriggerEnvelope `json:"envelope,omitempty"`
}

type AutomationTriggerStatusChangedPayload struct {
	WorkspaceID    string `json:"workspace_id"`
	Key            string `json:"key"`
	PreviousStatus string `json:"previous_status"`
	Status         string `json:"status"`
	Reason         string `json:"reason,omitempty"`
}

type ProjectTransitionChangedPayload struct {
	State          string `json:"state"`
	Controller     string `json:"controller"`
	LimitedActions string `json:"limited_actions,omitempty"`
	Notes          string `json:"notes,omitempty"`
	ChangedBy      string `json:"changed_by"`
}

type ProjectTransitionReportRecordedPayload struct {
	ReportType string `json:"report_type"`
	Summary    string `json:"summary"`
}

type ProjectTransitionDeniedPayload struct {
	ActionClass string `json:"action_class"`
	Reason      string `json:"reason"`
}

type LearningProposalCreatedPayload struct {
	ProposalType string `json:"proposal_type"`
	Scope        string `json:"scope"`
	TargetKey    string `json:"target_key"`
	Status       string `json:"status"`
	Summary      string `json:"summary"`
}

type LearningProposalStatusPayload struct {
	Status string `json:"status"`
}

type LearningEvaluationRecordedPayload struct {
	ProposalID int64   `json:"proposal_id"`
	FixtureKey string  `json:"fixture_key"`
	Mode       string  `json:"mode"`
	Score      float64 `json:"score"`
	Outcome    string  `json:"outcome"`
}

type LearningPromotionAppliedPayload struct {
	ProposalID            int64  `json:"proposal_id"`
	ProposalType          string `json:"proposal_type"`
	Scope                 string `json:"scope"`
	TargetKey             string `json:"target_key"`
	Status                string `json:"status"`
	SupersedesPromotionID *int64 `json:"supersedes_promotion_id,omitempty"`
}

type LearningPromotionRolledBackPayload struct {
	ProposalID          int64  `json:"proposal_id"`
	RolledBackBy        string `json:"rolled_back_by"`
	RollbackReason      string `json:"rollback_reason"`
	RestoredPromotionID *int64 `json:"restored_promotion_id,omitempty"`
}

type SkillLifecycleRecordedPayload struct {
	SkillKey         string   `json:"skill_key"`
	Operation        string   `json:"operation"`
	Outcome          string   `json:"outcome"`
	ExecutionProfile string   `json:"execution_profile,omitempty"`
	RuntimeEffect    string   `json:"runtime_effect,omitempty"`
	Version          string   `json:"version,omitempty"`
	HandlerType      string   `json:"handler_type,omitempty"`
	HandlerRef       string   `json:"handler_ref,omitempty"`
	Permissions      []string `json:"permissions,omitempty"`
	DurationMS       int64    `json:"duration_ms"`
	ErrorCode        string   `json:"error_code,omitempty"`
	ErrorText        string   `json:"error_text,omitempty"`
}

type SkillArtifactRecordedPayload struct {
	ArtifactID       int64    `json:"artifact_id"`
	SkillKey         string   `json:"skill_key"`
	Status           string   `json:"status"`
	ArtifactType     string   `json:"artifact_type"`
	Summary          string   `json:"summary,omitempty"`
	ExecutionProfile string   `json:"execution_profile,omitempty"`
	RuntimeEffect    string   `json:"runtime_effect,omitempty"`
	HandlerRef       string   `json:"handler_ref,omitempty"`
	Permissions      []string `json:"permissions,omitempty"`
}

type SkillArtifactReviewedPayload struct {
	ArtifactID        int64  `json:"artifact_id"`
	SkillKey          string `json:"skill_key"`
	Decision          string `json:"decision"`
	Status            string `json:"status"`
	PreviousStatus    string `json:"previous_status"`
	ReviewedBy        string `json:"reviewed_by,omitempty"`
	Reason            string `json:"reason,omitempty"`
	Repeated          bool   `json:"repeated"`
	WorkCreated       bool   `json:"work_created"`
	FollowOnTaskID    *int64 `json:"follow_on_task_id,omitempty"`
	FollowOnTaskKey   string `json:"follow_on_task_key,omitempty"`
	FollowOnTaskState string `json:"follow_on_task_status,omitempty"`
}

type DelegationCreatedPayload struct {
	DelegationID    int64  `json:"delegation_id"`
	ParentTaskID    int64  `json:"parent_task_id"`
	ParentRunID     *int64 `json:"parent_run_id,omitempty"`
	DelegationKey   string `json:"delegation_key"`
	Role            string `json:"role"`
	ActionClass     string `json:"action_class"`
	ActionKey       string `json:"action_key"`
	MutationMode    string `json:"mutation_mode"`
	Status          string `json:"status"`
	ConvergenceMode string `json:"convergence_mode"`
	ArtifactTarget  string `json:"artifact_target"`
	Executor        string `json:"executor"`
}

type DelegationCreateReusedPayload struct {
	DelegationID  int64  `json:"delegation_id"`
	ParentTaskID  int64  `json:"parent_task_id"`
	ParentRunID   *int64 `json:"parent_run_id,omitempty"`
	ChildTaskID   *int64 `json:"child_task_id,omitempty"`
	ChildRunID    *int64 `json:"child_run_id,omitempty"`
	DelegationKey string `json:"delegation_key"`
	Role          string `json:"role"`
	Status        string `json:"status"`
	Reason        string `json:"reason"`
}

type DelegationStatusChangedPayload struct {
	DelegationID   int64  `json:"delegation_id"`
	ParentTaskID   int64  `json:"parent_task_id"`
	ParentRunID    *int64 `json:"parent_run_id,omitempty"`
	ChildTaskID    *int64 `json:"child_task_id,omitempty"`
	ChildRunID     *int64 `json:"child_run_id,omitempty"`
	PreviousStatus string `json:"previous_status"`
	Status         string `json:"status"`
}

type DelegationChildAttachedPayload struct {
	DelegationID int64  `json:"delegation_id"`
	ParentTaskID int64  `json:"parent_task_id"`
	ParentRunID  *int64 `json:"parent_run_id,omitempty"`
	ChildTaskID  *int64 `json:"child_task_id,omitempty"`
	ChildRunID   *int64 `json:"child_run_id,omitempty"`
}

type DelegationArtifactRecordedPayload struct {
	DelegationID int64  `json:"delegation_id"`
	ParentTaskID int64  `json:"parent_task_id"`
	ParentRunID  *int64 `json:"parent_run_id,omitempty"`
	ChildTaskID  *int64 `json:"child_task_id,omitempty"`
	ChildRunID   *int64 `json:"child_run_id,omitempty"`
	ArtifactID   int64  `json:"artifact_id"`
	ArtifactType string `json:"artifact_type"`
	Summary      string `json:"summary"`
}

type DelegationRetryPayload struct {
	DelegationID   int64  `json:"delegation_id"`
	ParentTaskID   int64  `json:"parent_task_id"`
	ParentRunID    *int64 `json:"parent_run_id,omitempty"`
	ChildTaskID    *int64 `json:"child_task_id,omitempty"`
	ChildRunID     *int64 `json:"child_run_id,omitempty"`
	PreviousStatus string `json:"previous_status"`
	Status         string `json:"status"`
	Reason         string `json:"reason"`
}

type CapabilitySnapshotPublishedPayload struct {
	PreviousDigest  string `json:"previous_digest,omitempty"`
	Digest          string `json:"digest"`
	CapabilityCount int    `json:"capability_count"`
}

type CapabilitySnapshotRejectedPayload struct {
	PreviousDigest  string `json:"previous_digest,omitempty"`
	Digest          string `json:"digest,omitempty"`
	CapabilityCount int    `json:"capability_count"`
	Reason          string `json:"reason"`
}

func EncodePayload(payload any) (json.RawMessage, error) {
	return json.Marshal(payload)
}

func DecodePayload[T any](payload json.RawMessage) (T, error) {
	var decoded T
	err := json.Unmarshal(payload, &decoded)
	return decoded, err
}
