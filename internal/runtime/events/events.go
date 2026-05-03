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
	StreamAutomationTrigger  StreamType = "automation_trigger"
	StreamLearningProposal   StreamType = "learning_proposal"
	StreamLearningEvaluation StreamType = "learning_evaluation"
	StreamLearningPromotion  StreamType = "learning_promotion"
	StreamSkill              StreamType = "skill"
	StreamDelegation         StreamType = "delegation"
	StreamCapability         StreamType = "capability"
	StreamFollowUp           StreamType = "follow_up"
)

type Type string

const (
	EventServiceLifecycleChanged            Type = "service.lifecycle_changed"
	EventServiceHeartbeatRecorded           Type = "service.heartbeat_recorded"
	EventProjectCreated                     Type = "project.created"
	EventTaskCreated                        Type = "task.created"
	EventTaskDispatchRequested              Type = "task.dispatch_requested"
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
	EventConversationTranscriptRecorded     Type = "conversation.transcript_recorded"
	EventMemorySummaryRecorded              Type = "memory.summary_recorded"
	EventMemorySummaryUpdated               Type = "memory.summary_updated"
	EventIntakeItemCreated                  Type = "intake.item_created"
	EventIntakeProcessingStarted            Type = "intake.processing_started"
	EventIntakeClassified                   Type = "intake.classified"
	EventIntakeDedupeReviewed               Type = "intake.dedupe_reviewed"
	EventIntakeRouted                       Type = "intake.routed"
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
	EventAutomationTriggerCreated           Type = "automation_trigger.created"
	EventAutomationTriggerFireRequested     Type = "automation_trigger.fire_requested"
	EventAutomationTriggerEvaluated         Type = "automation_trigger.evaluated"
	EventAutomationTriggerMaterialized      Type = "automation_trigger.materialized"
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
	EventDelegationCreated                  Type = "delegation.created"
	EventDelegationStatusChanged            Type = "delegation.status_changed"
	EventDelegationChildAttached            Type = "delegation.child_attached"
	EventDelegationArtifactRecorded         Type = "delegation.artifact_recorded"
	EventCapabilitySnapshotPublished        Type = "capability.snapshot_published"
	EventCapabilitySnapshotRejected         Type = "capability.snapshot_rejected"
	EventFollowUpMaterialized               Type = "follow_up.materialized"
	EventFollowUpPaused                     Type = "follow_up.paused"
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
	Key            string `json:"key"`
	Title          string `json:"title"`
	ActionKey      string `json:"action_key,omitempty"`
	Status         string `json:"status"`
	Scope          string `json:"scope"`
	RequestedBy    string `json:"requested_by"`
	NextEligibleAt string `json:"next_eligible_at,omitempty"`
	Priority       int    `json:"priority,omitempty"`
	RetryCount     int    `json:"retry_count,omitempty"`
	MaxAttempts    int    `json:"max_attempts,omitempty"`
	LastError      string `json:"last_error,omitempty"`
	BlockedReason  string `json:"blocked_reason,omitempty"`
}

type TaskDispatchRequestedPayload struct {
	TaskID   int64  `json:"task_id"`
	Executor string `json:"executor"`
	Attempt  int    `json:"attempt"`
	Status   string `json:"status"`
}

type TaskStatusChangedPayload struct {
	PreviousStatus string `json:"previous_status"`
	Status         string `json:"status"`
	Summary        string `json:"summary,omitempty"`
	TerminalReason string `json:"terminal_reason,omitempty"`
	ArtifactsJSON  string `json:"artifacts_json,omitempty"`
}

type TaskQueueStateChangedPayload struct {
	PreviousStatus string `json:"previous_status"`
	Status         string `json:"status"`
	NextEligibleAt string `json:"next_eligible_at"`
	Priority       int    `json:"priority"`
	RetryCount     int    `json:"retry_count"`
	MaxAttempts    int    `json:"max_attempts"`
	LastError      string `json:"last_error,omitempty"`
	BlockedReason  string `json:"blocked_reason,omitempty"`
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
	IntakeItemID       int64  `json:"intake_item_id"`
	Status             string `json:"status,omitempty"`
	Stage              string `json:"stage"`
	Result             string `json:"result,omitempty"`
	RoutedOutcome      string `json:"routed_outcome,omitempty"`
	CanonicalIntakeID  *int64 `json:"canonical_intake_id,omitempty"`
	DraftArtifactKind  string `json:"draft_artifact_kind,omitempty"`
	ClarificationState string `json:"clarification_state,omitempty"`
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

type AutomationTriggerCreatedPayload struct {
	WorkspaceID   string `json:"workspace_id"`
	Key           string `json:"key"`
	InitiativeKey string `json:"initiative_key,omitempty"`
	Kind          string `json:"kind"`
	Status        string `json:"status"`
}

type AutomationTriggerFireRequestedPayload struct {
	WorkspaceID        string `json:"workspace_id"`
	Key                string `json:"key"`
	MaterializationKey string `json:"materialization_key"`
	Reason             string `json:"reason,omitempty"`
	RequestedBy        string `json:"requested_by,omitempty"`
}

type AutomationTriggerEvaluatedPayload struct {
	WorkspaceID        string `json:"workspace_id"`
	Key                string `json:"key"`
	MaterializationKey string `json:"materialization_key"`
	Status             string `json:"status"`
	CreatedWorkItem    bool   `json:"created_work_item"`
}

type AutomationTriggerMaterializedPayload struct {
	WorkspaceID        string `json:"workspace_id"`
	Key                string `json:"key"`
	MaterializationKey string `json:"materialization_key"`
	TaskID             int64  `json:"task_id"`
	TaskKey            string `json:"task_key"`
	RequestedBy        string `json:"requested_by,omitempty"`
}

type AutomationTriggerErroredPayload struct {
	WorkspaceID string `json:"workspace_id"`
	Key         string `json:"key"`
	Reason      string `json:"reason,omitempty"`
	Error       string `json:"error"`
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
