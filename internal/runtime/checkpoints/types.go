package checkpoints

type PacketScope string

const (
	PacketScopeProjectContext PacketScope = "project_context"
	PacketScopeRunContext     PacketScope = "run_context"
	PacketScopeTaskWake       PacketScope = "task_wake_packet"
)

type Trigger string

const (
	TriggerHandoff        Trigger = "handoff"
	TriggerModelSwitch    Trigger = "model_switch"
	TriggerApprovalWait   Trigger = "approval_wait"
	TriggerTokenThreshold Trigger = "token_threshold"
	TriggerIdlePause      Trigger = "idle_pause"
	TriggerCompletion     Trigger = "completion"
	TriggerRestart        Trigger = "restart"
)

type PacketStatus string

const (
	PacketStatusActive     PacketStatus = "active"
	PacketStatusSuperseded PacketStatus = "superseded"
	PacketStatusSealed     PacketStatus = "sealed"
)

type ProjectContext struct {
	ProjectID       int64             `json:"project_id"`
	ProjectKey      string            `json:"project_key"`
	Scope           string            `json:"scope"`
	ManifestSummary string            `json:"manifest_summary"`
	PolicySummary   string            `json:"policy_summary"`
	OpenTaskSummary string            `json:"open_task_summary"`
	Facts           map[string]string `json:"facts,omitempty"`
}

type RunContext struct {
	RunID           int64             `json:"run_id"`
	TaskID          int64             `json:"task_id"`
	Executor        string            `json:"executor"`
	Attempt         int               `json:"attempt"`
	Status          string            `json:"status"`
	ApprovalSummary string            `json:"approval_summary"`
	ToolResults     []ToolResult      `json:"tool_results,omitempty"`
	Facts           map[string]string `json:"facts,omitempty"`
}

type TaskWakePacket struct {
	TaskID                 int64      `json:"task_id"`
	TaskKey                string     `json:"task_key"`
	Scope                  string     `json:"scope"`
	Objective              string     `json:"objective"`
	Status                 string     `json:"status"`
	Trigger                Trigger    `json:"trigger"`
	BlockingReason         string     `json:"blocking_reason,omitempty"`
	LastCompletedStep      string     `json:"last_completed_step,omitempty"`
	NextSteps              []string   `json:"next_steps,omitempty"`
	Constraints            []string   `json:"constraints,omitempty"`
	SelectedCapabilities   []string   `json:"selected_capabilities,omitempty"`
	Evidence               []Evidence `json:"evidence,omitempty"`
	ProjectContextPacketID *int64     `json:"project_context_packet_id,omitempty"`
	RunContextPacketID     *int64     `json:"run_context_packet_id,omitempty"`
}

type ToolResult struct {
	Key     string            `json:"key"`
	Summary string            `json:"summary"`
	Facts   map[string]string `json:"facts,omitempty"`
	RawRef  string            `json:"raw_ref,omitempty"`
}

type Evidence struct {
	Kind    string `json:"kind"`
	Summary string `json:"summary"`
	Ref     string `json:"ref,omitempty"`
}

type ResumeState struct {
	TaskID          int64
	TaskKey         string
	Scope           string
	Objective       string
	Status          string
	Trigger         Trigger
	BlockingReason  string
	NextSteps       []string
	Constraints     []string
	Capabilities    []string
	ProjectContext  *ProjectContext
	RunContext      *RunContext
	WakePacketID    int64
	ProjectPacketID *int64
	RunPacketID     *int64
}
