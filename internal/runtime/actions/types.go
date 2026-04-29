package actions

import (
	"encoding/json"
	"errors"
)

type LifecycleState string

const (
	StatePrepared           LifecycleState = "prepared"
	StatePreflighted        LifecycleState = "preflighted"
	StateApproved           LifecycleState = "approved"
	StateSubmitted          LifecycleState = "submitted"
	StateInternallyRecorded LifecycleState = "internally_recorded"
	StateExternallyReadBack LifecycleState = "externally_read_back"
	StateCompleted          LifecycleState = "completed"
	StateFailed             LifecycleState = "failed"
	StateAbandoned          LifecycleState = "abandoned"
)

type EventType string

const (
	EventPrepared           EventType = "action.prepared"
	EventPreflighted        EventType = "action.preflighted"
	EventApproved           EventType = "action.approved"
	EventSubmitted          EventType = "action.submitted"
	EventInternallyRecorded EventType = "action.internally_recorded"
	EventExternallyReadBack EventType = "action.externally_read_back"
	EventSubstituteProof    EventType = "action.substitute_proof"
	EventCompleted          EventType = "action.completed"
	EventFailed             EventType = "action.failed"
	EventAbandoned          EventType = "action.abandoned"
	EventCorrected          EventType = "action.corrected"
)

type ProofRequirement string

const (
	ProofExternalReadback ProofRequirement = "external_readback"
	ProofSubstitute       ProofRequirement = "substitute_proof"
	ProofInternalRecord   ProofRequirement = "internal_record"
)

var (
	ErrPayloadChangedAfterApproval = errors.New("payload_changed_after_approval")
	ErrApprovalMissing             = errors.New("approval_missing")
	ErrApprovalPayloadMismatch     = errors.New("approval_payload_mismatch")
	ErrInvalidLifecycleTransition  = errors.New("invalid_lifecycle_transition")
	ErrSchedulePreflightMissing    = errors.New("schedule_preflight_missing")
	ErrSchedulePreflightStale      = errors.New("schedule_preflight_stale")
	ErrSubmitPathMissing           = errors.New("submit_path_missing")
	ErrReadbackPathMissing         = errors.New("readback_path_missing")
	ErrExternalReadbackMissing     = errors.New("external_readback_missing")
	ErrSubstituteProofNotDeclared  = errors.New("substitute_proof_not_declared")
	ErrTerminalActionClosed        = errors.New("terminal_action_closed")
)

type PreparedPayload struct {
	PayloadJSON          json.RawMessage
	SubmitPath           string
	ReadbackPath         string
	ProofRequirement     ProofRequirement
	PayloadSchema        string
	PayloadSchemaVersion int
}

type TransitionInput struct {
	CurrentState LifecycleState
	EventType    EventType
}

type ApprovalBindingInput struct {
	ActionID            int64
	CurrentPayloadHash  string
	ApprovalActionID    int64
	ApprovalPayloadHash string
}

type CompletionInput struct {
	ProofRequirement ProofRequirement
	Events           []EvidenceSummary
}

type EvidenceSummary struct {
	Type EventType
}
