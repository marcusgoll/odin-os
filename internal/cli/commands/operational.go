package commands

import (
	"encoding/json"
	"io"
)

type StatusView struct {
	Health           string `json:"health"`
	PendingApprovals int    `json:"pending_approvals"`
	RegistryHealthy  bool   `json:"registry_healthy"`
}

type ProjectListView struct {
	Current  string   `json:"current"`
	Projects []string `json:"projects"`
}

type ScopeView struct {
	Scope string `json:"scope"`
}

type JobView struct {
	ProjectKey            string `json:"project_key"`
	ProjectID             int64  `json:"project_id,omitempty"`
	TaskID                int64  `json:"task_id,omitempty"`
	TaskKey               string `json:"task_key"`
	Status                string `json:"status"`
	ExecutionIntent       string `json:"execution_intent,omitempty"`
	ExecutionIntentSource string `json:"execution_intent_source,omitempty"`
	BlockedReason         string `json:"blocked_reason,omitempty"`
	CurrentRunID          *int64 `json:"current_run_id,omitempty"`
	CurrentRunStatus      string `json:"current_run_status,omitempty"`
}

type JobsView struct {
	Jobs []JobView `json:"jobs"`
}

type RunView struct {
	RunID                 int64  `json:"run_id,omitempty"`
	TaskID                int64  `json:"task_id,omitempty"`
	TaskKey               string `json:"task_key"`
	ProjectKey            string `json:"project_key,omitempty"`
	RepoRoot              string `json:"repo_root,omitempty"`
	WorktreePath          string `json:"worktree_path,omitempty"`
	BranchName            string `json:"branch_name,omitempty"`
	ExecutionIntent       string `json:"execution_intent,omitempty"`
	ExecutionIntentSource string `json:"execution_intent_source,omitempty"`
	Executor              string `json:"executor"`
	Status                string `json:"status"`
	Attempt               int    `json:"attempt,omitempty"`
}

type RunsView struct {
	Runs []RunView `json:"runs"`
}

type ApprovalView struct {
	ApprovalID      int64  `json:"approval_id"`
	TaskKey         string `json:"task_key"`
	RunID           *int64 `json:"run_id,omitempty"`
	Status          string `json:"status"`
	ResolverSupport string `json:"resolver_support"`
	DecisionBy      string `json:"decision_by,omitempty"`
	Reason          string `json:"reason,omitempty"`
}

type ApprovalsView struct {
	Approvals []ApprovalView `json:"approvals"`
}

type LogView struct {
	ID         int64           `json:"id"`
	StreamType string          `json:"stream_type"`
	StreamID   int64           `json:"stream_id"`
	Type       string          `json:"type"`
	Scope      string          `json:"scope"`
	ProjectID  *int64          `json:"project_id,omitempty"`
	TaskID     *int64          `json:"task_id,omitempty"`
	RunID      *int64          `json:"run_id,omitempty"`
	OccurredAt string          `json:"occurred_at"`
	Payload    json.RawMessage `json:"payload,omitempty"`
}

type LogsView struct {
	Logs []LogView `json:"logs"`
}

func WriteStatusJSON(w io.Writer, view StatusView) error {
	return WriteJSON(w, view)
}

func WriteJSON(w io.Writer, payload any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(payload)
}
