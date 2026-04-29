package actions_test

import (
	"encoding/json"
	"errors"
	"testing"

	"odin-os/internal/runtime/actions"
)

func TestPreparedPayloadHashChangesWhenSubmitPathChanges(t *testing.T) {
	service := actions.Service{}
	first, err := service.HashPreparedPayload(actions.PreparedPayload{
		PayloadJSON:      json.RawMessage(`{"pairing":"W7084C"}`),
		SubmitPath:       "command:/tradeboard post",
		ReadbackPath:     "huginn:flica-my-requests",
		ProofRequirement: "external_readback",
	})
	if err != nil {
		t.Fatalf("HashPreparedPayload() error = %v", err)
	}
	second, err := service.HashPreparedPayload(actions.PreparedPayload{
		PayloadJSON:      json.RawMessage(`{"pairing":"W7084C"}`),
		SubmitPath:       "command:/tradeboard pickup",
		ReadbackPath:     "huginn:flica-my-requests",
		ProofRequirement: "external_readback",
	})
	if err != nil {
		t.Fatalf("HashPreparedPayload() error = %v", err)
	}
	if first == second {
		t.Fatalf("hash did not change when submit path changed")
	}
}

func TestLifecycleRejectsCompletionWithoutReadback(t *testing.T) {
	err := actions.ValidateCompletion(actions.CompletionInput{
		ProofRequirement: "external_readback",
		Events: []actions.EvidenceSummary{
			{Type: actions.EventSubmitted},
			{Type: actions.EventInternallyRecorded},
		},
	})
	if !errors.Is(err, actions.ErrExternalReadbackMissing) {
		t.Fatalf("err = %v, want ErrExternalReadbackMissing", err)
	}
}

func TestLifecycleRejectsInternalRecordCompletionWithoutEvidence(t *testing.T) {
	err := actions.ValidateCompletion(actions.CompletionInput{
		ProofRequirement: actions.ProofInternalRecord,
	})
	if !errors.Is(err, actions.ErrExternalReadbackMissing) {
		t.Fatalf("err = %v, want ErrExternalReadbackMissing", err)
	}
}

func TestLifecycleAllowsInternalRecordCompletionWithEvidence(t *testing.T) {
	err := actions.ValidateCompletion(actions.CompletionInput{
		ProofRequirement: actions.ProofInternalRecord,
		Events: []actions.EvidenceSummary{
			{Type: actions.EventInternallyRecorded},
		},
	})
	if err != nil {
		t.Fatalf("ValidateCompletion() error = %v", err)
	}
}

func TestLifecycleRejectsSubstituteProofCompletionWithoutEvidence(t *testing.T) {
	err := actions.ValidateCompletion(actions.CompletionInput{
		ProofRequirement: actions.ProofSubstitute,
	})
	if !errors.Is(err, actions.ErrSubstituteProofNotDeclared) {
		t.Fatalf("err = %v, want ErrSubstituteProofNotDeclared", err)
	}
}

func TestLifecycleAllowsSubstituteProofCompletionWithEvidence(t *testing.T) {
	err := actions.ValidateCompletion(actions.CompletionInput{
		ProofRequirement: actions.ProofSubstitute,
		Events: []actions.EvidenceSummary{
			{Type: actions.EventSubstituteProof},
		},
	})
	if err != nil {
		t.Fatalf("ValidateCompletion() error = %v", err)
	}
}

func TestPreparedPayloadHashCanonicalizesJSONObjectOrder(t *testing.T) {
	service := actions.Service{}
	first, err := service.HashPreparedPayload(actions.PreparedPayload{
		PayloadJSON:          json.RawMessage(`{"pairing":"W7084C","details":{"seat":"CA","base":"DCA"}}`),
		SubmitPath:           "command:/tradeboard post",
		ReadbackPath:         "huginn:flica-my-requests",
		ProofRequirement:     actions.ProofExternalReadback,
		PayloadSchema:        "fixture.action.v1",
		PayloadSchemaVersion: 1,
	})
	if err != nil {
		t.Fatalf("HashPreparedPayload() error = %v", err)
	}
	second, err := service.HashPreparedPayload(actions.PreparedPayload{
		PayloadJSON:          json.RawMessage(`{"details":{"base":"DCA","seat":"CA"},"pairing":"W7084C"}`),
		SubmitPath:           "command:/tradeboard post",
		ReadbackPath:         "huginn:flica-my-requests",
		ProofRequirement:     actions.ProofExternalReadback,
		PayloadSchema:        "fixture.action.v1",
		PayloadSchemaVersion: 1,
	})
	if err != nil {
		t.Fatalf("HashPreparedPayload() error = %v", err)
	}
	if first != second {
		t.Fatalf("hash changed for semantically equivalent JSON object order")
	}
}

func TestPreparedPayloadHashRejectsUnknownProofRequirement(t *testing.T) {
	_, err := actions.Service{}.HashPreparedPayload(actions.PreparedPayload{
		PayloadJSON:      json.RawMessage(`{"action":"prepare"}`),
		SubmitPath:       "command:/actions prepare",
		ReadbackPath:     "command:/actions readback",
		ProofRequirement: actions.ProofRequirement("unknown_proof"),
	})
	if !errors.Is(err, actions.ErrProofRequirementUnknown) {
		t.Fatalf("err = %v, want ErrProofRequirementUnknown", err)
	}
}

func TestLifecycleRejectsUnsafeTerminalMutation(t *testing.T) {
	err := actions.ValidateTransition(actions.TransitionInput{
		CurrentState: actions.StateCompleted,
		EventType:    actions.EventSubmitted,
	})
	if !errors.Is(err, actions.ErrTerminalActionClosed) {
		t.Fatalf("err = %v, want ErrTerminalActionClosed", err)
	}
}

func TestLifecycleAllowsSubstituteProofAfterSubmission(t *testing.T) {
	err := actions.ValidateTransition(actions.TransitionInput{
		CurrentState: actions.StateSubmitted,
		EventType:    actions.EventSubstituteProof,
	})
	if err != nil {
		t.Fatalf("ValidateTransition() error = %v", err)
	}
}

func TestLifecycleRejectsSubstituteProofBeforeSubmission(t *testing.T) {
	err := actions.ValidateTransition(actions.TransitionInput{
		CurrentState: actions.StateApproved,
		EventType:    actions.EventSubstituteProof,
	})
	if !errors.Is(err, actions.ErrInvalidLifecycleTransition) {
		t.Fatalf("err = %v, want ErrInvalidLifecycleTransition", err)
	}
}

func TestApprovalBindingRejectsPayloadMismatch(t *testing.T) {
	err := actions.ValidateApprovalBinding(actions.ApprovalBindingInput{
		ActionID:            42,
		CurrentPayloadHash:  "sha256:current",
		ApprovalActionID:    42,
		ApprovalPayloadHash: "sha256:old",
	})
	if !errors.Is(err, actions.ErrApprovalPayloadMismatch) {
		t.Fatalf("err = %v, want ErrApprovalPayloadMismatch", err)
	}
}
