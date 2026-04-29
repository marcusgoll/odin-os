package actions

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

type Service struct{}

type preparedPayloadIdentity struct {
	PayloadJSON          json.RawMessage  `json:"payload_json"`
	PayloadSchema        string           `json:"payload_schema"`
	PayloadSchemaVersion int              `json:"payload_schema_version"`
	ProofRequirement     ProofRequirement `json:"proof_requirement"`
	ReadbackPath         string           `json:"readback_path"`
	SubmitPath           string           `json:"submit_path"`
}

func (Service) HashPreparedPayload(payload PreparedPayload) (string, error) {
	if payload.SubmitPath == "" {
		return "", ErrSubmitPathMissing
	}
	if payload.ProofRequirement == "" {
		return "", ErrSubstituteProofNotDeclared
	}
	if payload.ProofRequirement == ProofExternalReadback && payload.ReadbackPath == "" {
		return "", ErrReadbackPathMissing
	}

	canonicalPayload, err := canonicalJSON(payload.PayloadJSON)
	if err != nil {
		return "", err
	}
	identity := preparedPayloadIdentity{
		PayloadJSON:          canonicalPayload,
		PayloadSchema:        payload.PayloadSchema,
		PayloadSchemaVersion: payload.PayloadSchemaVersion,
		ProofRequirement:     payload.ProofRequirement,
		ReadbackPath:         payload.ReadbackPath,
		SubmitPath:           payload.SubmitPath,
	}
	identityJSON, err := json.Marshal(identity)
	if err != nil {
		return "", err
	}

	sum := sha256.Sum256(identityJSON)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func ValidateTransition(input TransitionInput) error {
	if input.EventType == EventCorrected {
		return nil
	}
	if isTerminal(input.CurrentState) {
		return ErrTerminalActionClosed
	}

	allowed, knownEvent := allowedCurrentStates(input.EventType)
	if !knownEvent {
		return ErrInvalidLifecycleTransition
	}
	for _, state := range allowed {
		if input.CurrentState == state {
			return nil
		}
	}
	return fmt.Errorf("%w: %s cannot apply %s", ErrInvalidLifecycleTransition, input.CurrentState, input.EventType)
}

func ValidateApprovalBinding(input ApprovalBindingInput) error {
	if input.ApprovalActionID == 0 || input.ApprovalPayloadHash == "" {
		return ErrApprovalMissing
	}
	if input.ActionID == 0 || input.CurrentPayloadHash == "" {
		return ErrApprovalPayloadMismatch
	}
	if input.ActionID != input.ApprovalActionID {
		return fmt.Errorf("%w: action_id=%d approval_action_id=%d", ErrApprovalPayloadMismatch, input.ActionID, input.ApprovalActionID)
	}
	if input.CurrentPayloadHash != input.ApprovalPayloadHash {
		return errors.Join(ErrPayloadChangedAfterApproval, ErrApprovalPayloadMismatch)
	}
	return nil
}

func ValidateCompletion(input CompletionInput) error {
	switch input.ProofRequirement {
	case ProofExternalReadback:
		if !hasEvent(input.Events, EventExternallyReadBack) {
			return ErrExternalReadbackMissing
		}
	case ProofSubstitute, ProofInternalRecord:
		return nil
	default:
		return ErrSubstituteProofNotDeclared
	}
	return nil
}

func canonicalJSON(raw json.RawMessage) (json.RawMessage, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, fmt.Errorf("payload_json: empty")
	}

	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()

	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, fmt.Errorf("payload_json: trailing tokens")
		}
		return nil, err
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return encoded, nil
}

func allowedCurrentStates(eventType EventType) ([]LifecycleState, bool) {
	switch eventType {
	case EventPrepared:
		return []LifecycleState{""}, true
	case EventPreflighted:
		return []LifecycleState{StatePrepared}, true
	case EventApproved:
		return []LifecycleState{StatePrepared, StatePreflighted}, true
	case EventSubmitted:
		return []LifecycleState{StateApproved}, true
	case EventInternallyRecorded:
		return []LifecycleState{StateSubmitted}, true
	case EventExternallyReadBack:
		return []LifecycleState{StateSubmitted, StateInternallyRecorded}, true
	case EventCompleted:
		return []LifecycleState{StateInternallyRecorded, StateExternallyReadBack}, true
	case EventFailed:
		return []LifecycleState{"", StatePrepared, StatePreflighted, StateApproved, StateSubmitted, StateInternallyRecorded, StateExternallyReadBack}, true
	case EventAbandoned:
		return []LifecycleState{"", StatePrepared, StatePreflighted, StateApproved, StateSubmitted, StateInternallyRecorded, StateExternallyReadBack}, true
	default:
		return nil, false
	}
}

func hasEvent(events []EvidenceSummary, eventType EventType) bool {
	for _, event := range events {
		if event.Type == eventType {
			return true
		}
	}
	return false
}

func isTerminal(state LifecycleState) bool {
	return state == StateCompleted || state == StateFailed || state == StateAbandoned
}
