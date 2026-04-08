package events

import (
	"encoding/json"
	"time"
)

type StreamType string

const (
	StreamProject         StreamType = "project"
	StreamTask            StreamType = "task"
	StreamRun             StreamType = "run"
	StreamApproval        StreamType = "approval"
	StreamIncident        StreamType = "incident"
	StreamRecovery        StreamType = "recovery"
	StreamRegistryVersion StreamType = "registry_version"
	StreamExecutorHealth  StreamType = "executor_health"
	StreamContextPacket   StreamType = "context_packet"
)

type Type string

const (
	EventProjectCreated          Type = "project.created"
	EventTaskCreated             Type = "task.created"
	EventTaskStatusChanged       Type = "task.status_changed"
	EventRunStarted              Type = "run.started"
	EventRunFinished             Type = "run.finished"
	EventApprovalRequested       Type = "approval.requested"
	EventApprovalResolved        Type = "approval.resolved"
	EventIncidentOpened          Type = "incident.opened"
	EventRecoveryStarted         Type = "recovery.started"
	EventRecoveryCompleted       Type = "recovery.completed"
	EventRegistryVersionRecorded Type = "registry_version.recorded"
	EventExecutorHealthRecorded  Type = "executor_health.recorded"
	EventContextPacketCreated    Type = "context_packet.created"
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

type RecoveryStartedPayload struct {
	Status   string `json:"status"`
	Strategy string `json:"strategy"`
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
	PacketKind string `json:"packet_kind"`
	Summary    string `json:"summary"`
}

func EncodePayload(payload any) (json.RawMessage, error) {
	return json.Marshal(payload)
}

func DecodePayload[T any](payload json.RawMessage) (T, error) {
	var decoded T
	err := json.Unmarshal(payload, &decoded)
	return decoded, err
}
