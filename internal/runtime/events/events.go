package events

import (
	"encoding/json"
	"time"
)

type StreamType string

const (
	StreamProject            StreamType = "project"
	StreamTask               StreamType = "task"
	StreamRun                StreamType = "run"
	StreamApproval           StreamType = "approval"
	StreamIncident           StreamType = "incident"
	StreamRecovery           StreamType = "recovery"
	StreamRegistryVersion    StreamType = "registry_version"
	StreamExecutorHealth     StreamType = "executor_health"
	StreamContextPacket      StreamType = "context_packet"
	StreamConversation       StreamType = "conversation"
	StreamMemorySummary      StreamType = "memory_summary"
	StreamLearningProposal   StreamType = "learning_proposal"
	StreamLearningEvaluation StreamType = "learning_evaluation"
	StreamLearningPromotion  StreamType = "learning_promotion"
)

type Type string

const (
	EventProjectCreated                   Type = "project.created"
	EventTaskCreated                      Type = "task.created"
	EventTaskStatusChanged                Type = "task.status_changed"
	EventRunStarted                       Type = "run.started"
	EventRunFinished                      Type = "run.finished"
	EventApprovalRequested                Type = "approval.requested"
	EventApprovalResolved                 Type = "approval.resolved"
	EventIncidentOpened                   Type = "incident.opened"
	EventIncidentResolved                 Type = "incident.resolved"
	EventIncidentEscalated                Type = "incident.escalated"
	EventRecoveryStarted                  Type = "recovery.started"
	EventRecoveryActionExecuted           Type = "recovery.action_executed"
	EventRecoveryCompleted                Type = "recovery.completed"
	EventRegistryVersionRecorded          Type = "registry_version.recorded"
	EventExecutorHealthRecorded           Type = "executor_health.recorded"
	EventContextPacketCreated             Type = "context_packet.created"
	EventConversationTranscriptRecorded   Type = "conversation.transcript_recorded"
	EventMemorySummaryRecorded            Type = "memory_summary.recorded"
	EventProjectTransitionChanged         Type = "project.transition_changed"
	EventProjectShadowObservationRecorded Type = "project.shadow_observation_recorded"
	EventProjectCompareReportRecorded     Type = "project.compare_report_recorded"
	EventProjectTransitionDenied          Type = "project.transition_denied"
	EventLearningProposalCreated          Type = "learning.proposal_created"
	EventLearningProposalSubmitted        Type = "learning.proposal_submitted"
	EventLearningProposalPromotionReady   Type = "learning.proposal_promotion_ready"
	EventLearningProposalRejected         Type = "learning.proposal_rejected"
	EventLearningEvaluationRecorded       Type = "learning.evaluation_recorded"
	EventLearningPromotionApplied         Type = "learning.promotion_applied"
	EventLearningPromotionRolledBack      Type = "learning.promotion_rolled_back"
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
	Key         string `json:"key"`
	Title       string `json:"title"`
	Status      string `json:"status"`
	Scope       string `json:"scope"`
	RequestedBy string `json:"requested_by"`
}

type TaskStatusChangedPayload struct {
	PreviousStatus string `json:"previous_status"`
	Status         string `json:"status"`
}

type RunStartedPayload struct {
	TaskID   int64  `json:"task_id"`
	Executor string `json:"executor"`
	Attempt  int    `json:"attempt"`
	Status   string `json:"status"`
}

type RunFinishedPayload struct {
	Status  string `json:"status"`
	Summary string `json:"summary"`
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
	Executor string `json:"executor"`
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

func EncodePayload(payload any) (json.RawMessage, error) {
	return json.Marshal(payload)
}

func DecodePayload[T any](payload json.RawMessage) (T, error) {
	var decoded T
	err := json.Unmarshal(payload, &decoded)
	return decoded, err
}
