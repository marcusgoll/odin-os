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
	ProjectKey string `json:"project_key"`
	TaskKey    string `json:"task_key"`
	Status     string `json:"status"`
}

type JobsView struct {
	Jobs []JobView `json:"jobs"`
}

type RunView struct {
	TaskKey  string `json:"task_key"`
	Executor string `json:"executor"`
	Status   string `json:"status"`
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
}

type ApprovalsView struct {
	Approvals []ApprovalView `json:"approvals"`
}

type LogView struct {
	ID      int64           `json:"id"`
	Type    string          `json:"type"`
	Scope   string          `json:"scope"`
	Payload json.RawMessage `json:"payload,omitempty"`
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
