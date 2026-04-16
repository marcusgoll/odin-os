package capabilities

import (
	"encoding/json"

	"odin-os/internal/registry"
)

type Descriptor = registry.Item

type Snapshot struct {
	Digest       string
	Diagnostics  []registry.Diagnostic
	Capabilities map[string]Descriptor
}

type CapabilityCard struct {
	ID      string
	Kind    registry.Kind
	Name    string
	Title   string
	Version string
	Scope   string
	Summary string
	Status  string
}

type ScopeRef struct {
	Kind       string `json:"kind,omitempty"`
	ProjectKey string `json:"project_key,omitempty"`
}

type CallerRef struct {
	Kind string `json:"kind,omitempty"`
	ID   string `json:"id,omitempty"`
}

type ExecutionRequest struct {
	Mode       string `json:"mode,omitempty"`
	Timeout    string `json:"timeout,omitempty"`
	RetryLimit int    `json:"retry_limit,omitempty"`
}

type Artifact struct {
	Name string `json:"name,omitempty"`
	Type string `json:"type,omitempty"`
	URI  string `json:"uri,omitempty"`
}

type RunError struct {
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

type RunEnvelope struct {
	RunID     string          `json:"run_id,omitempty"`
	Status    string          `json:"status,omitempty"`
	Output    json.RawMessage `json:"output,omitempty"`
	Artifacts []Artifact      `json:"artifacts,omitempty"`
	Error     *RunError       `json:"error,omitempty"`
}

type InvokeRequest struct {
	RequestID         string
	CapabilityID      string
	CapabilityVersion string
	Scope             ScopeRef
	Caller            CallerRef
	Input             json.RawMessage
	Execution         ExecutionRequest
}

type InvokeResponse struct {
	RunID     string
	Status    string
	Output    json.RawMessage
	Artifacts []Artifact
	Error     *RunError
}
