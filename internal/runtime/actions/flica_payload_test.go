package actions_test

import (
	"encoding/json"
	"testing"

	"odin-os/internal/runtime/actions"
)

func TestFLICATradeBoardFixturePayloadIncludesProofFields(t *testing.T) {
	payload := actions.PreparedPayload{
		PayloadSchema:        "flica.tradeboard_action.v1",
		PayloadSchemaVersion: 1,
		PayloadJSON: json.RawMessage(`{
			"action_type":"tradeboard_action",
			"operation":"post",
			"pairing":"W7084C",
			"bcid":"2026-04",
			"split_legs":[9,10],
			"comment":"DFW-DAY-DFW_turn_report_1351_end_2030"
		}`),
		SubmitPath:       "command:/tradeboard post",
		ReadbackPath:     "huginn:flica-my-requests",
		ProofRequirement: actions.ProofExternalReadback,
	}
	hash, err := actions.Service{}.HashPreparedPayload(payload)
	if err != nil {
		t.Fatalf("HashPreparedPayload() error = %v", err)
	}
	if hash == "" {
		t.Fatalf("hash is empty")
	}
}
