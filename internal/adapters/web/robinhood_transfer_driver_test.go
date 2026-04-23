package web

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRobinhoodTransferDriverPrepareReturnsReviewReady(t *testing.T) {
	requestPath := filepath.Join(t.TempDir(), "request.json")
	script := writeFixtureDriver(t, `#!/usr/bin/env bash
cat >"$ODIN_DRIVER_REQUEST_PATH"
printf '{"status":"completed","tool_key":"robinhood_transfer_flow","summary":"Robinhood review ready","artifacts":{"session_state":"review_ready"}}'
`)
	t.Setenv("ODIN_HUGINN_ROBINHOOD_TRANSFER_DRIVER", script)
	t.Setenv("ODIN_DRIVER_REQUEST_PATH", requestPath)

	driver := NewRobinhoodTransferDriver()
	response, err := driver.Invoke(context.Background(), RobinhoodTransferRequest{
		Input: RobinhoodTransferInput{
			Mode:               "prepare",
			Direction:          "deposit",
			AmountUSD:          "25.00",
			SourceAccount:      "checking",
			DestinationAccount: "brokerage",
			Memo:               "test",
		},
	})
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if got := stringArtifactValue(response.Artifacts, "session_state"); got != "review_ready" {
		t.Fatalf("artifacts.session_state = %q, want review_ready", got)
	}

	requestBytes, err := os.ReadFile(requestPath)
	if err != nil {
		t.Fatalf("ReadFile(request) error = %v", err)
	}
	var request struct {
		ToolKey string                 `json:"tool_key"`
		Input   RobinhoodTransferInput `json:"input"`
	}
	if err := json.Unmarshal(requestBytes, &request); err != nil {
		t.Fatalf("request json = %v", err)
	}
	if request.ToolKey != "robinhood_transfer_flow" {
		t.Fatalf("request.ToolKey = %q, want robinhood_transfer_flow", request.ToolKey)
	}
	if request.Input.Mode != "prepare" {
		t.Fatalf("request.Input.Mode = %q, want prepare", request.Input.Mode)
	}
	if request.Input.Direction != "deposit" {
		t.Fatalf("request.Input.Direction = %q, want deposit", request.Input.Direction)
	}
}

func TestRobinhoodTransferDriverSubmitCanReturnResumeVerificationFailedWithPriorSessionState(t *testing.T) {
	script := writeFixtureDriver(t, `#!/usr/bin/env bash
printf '{"status":"completed","tool_key":"robinhood_transfer_flow","summary":"Robinhood review continuity could not be verified","artifacts":{"session_state":"resume_verification_failed","prior_session_state":"session_expired"}}'
`)
	t.Setenv("ODIN_HUGINN_ROBINHOOD_TRANSFER_DRIVER", script)

	driver := NewRobinhoodTransferDriver()
	response, err := driver.Invoke(context.Background(), RobinhoodTransferRequest{
		Input: RobinhoodTransferInput{
			Mode:               "submit",
			Direction:          "deposit",
			AmountUSD:          "25.00",
			SourceAccount:      "checking",
			DestinationAccount: "brokerage",
			ResumeFacts: map[string]string{
				"expected_review_state": "review_ready",
			},
		},
	})
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if got := stringArtifactValue(response.Artifacts, "session_state"); got != "resume_verification_failed" {
		t.Fatalf("artifacts.session_state = %q, want resume_verification_failed", got)
	}
	if got := stringArtifactValue(response.Artifacts, "prior_session_state"); got != "session_expired" {
		t.Fatalf("artifacts.prior_session_state = %q, want session_expired", got)
	}
}

func stringArtifactValue(artifacts map[string]any, key string) string {
	value, ok := artifacts[key]
	if !ok {
		return ""
	}
	text, _ := value.(string)
	return text
}
