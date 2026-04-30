package recovery_test

import (
	"encoding/json"
	"strings"
	"testing"

	"odin-os/internal/runtime/recovery"
)

func TestAnalyzeFailureDistinguishesTicketReadinessFromCodeFailures(t *testing.T) {
	t.Parallel()

	analysis := recovery.AnalyzeFailure(recovery.FailureInput{
		Step:                       "codex_run",
		TicketTitle:                "Build GitHub intake",
		AcceptanceCriteriaRequired: true,
		AcceptanceCriteria:         nil,
		ErrorText:                  "go test ./... failed after dispatch",
	})

	if analysis.Category != recovery.FailureMissingAcceptanceCriteria {
		t.Fatalf("Category = %q, want %q", analysis.Category, recovery.FailureMissingAcceptanceCriteria)
	}
	if analysis.NextStepTarget != recovery.NextStepPrompt {
		t.Fatalf("NextStepTarget = %q, want prompt", analysis.NextStepTarget)
	}
	if analysis.AutoApplyWorkflowChange {
		t.Fatal("AutoApplyWorkflowChange = true, want false")
	}
	if !strings.Contains(analysis.SuggestedFix, "acceptance criteria") {
		t.Fatalf("SuggestedFix = %q, want acceptance criteria guidance", analysis.SuggestedFix)
	}
}

func TestAnalyzeFailureClassifiesTestFailuresWithImplementationFollowUp(t *testing.T) {
	t.Parallel()

	analysis := recovery.AnalyzeFailure(recovery.FailureInput{
		Step:                  "review",
		TicketTitle:           "Repair executor panic handling",
		AcceptanceCriteria:    []string{"go test ./... passes"},
		ExistingBehaviorKnown: true,
		ErrorText:             "go test ./internal/runtime/jobs failed: TestExecuteNextQueuedRecordsWorkerPanicAsFailedRun",
	})

	if analysis.Category != recovery.FailureTestFailure {
		t.Fatalf("Category = %q, want %q", analysis.Category, recovery.FailureTestFailure)
	}
	if analysis.NextStepTarget != recovery.NextStepImplementation {
		t.Fatalf("NextStepTarget = %q, want implementation", analysis.NextStepTarget)
	}
	if !analysis.FollowUp.Recommended {
		t.Fatalf("FollowUp.Recommended = false, want true")
	}
	if analysis.FollowUp.Title == "" {
		t.Fatal("FollowUp.Title is empty")
	}
}

func TestAnalyzeFailureClassifiesUnsafeShimBeforeUnknown(t *testing.T) {
	t.Parallel()

	analysis := recovery.AnalyzeFailure(recovery.FailureInput{
		Step:                  "migration",
		TicketTitle:           "Normalize shell shim",
		AcceptanceCriteria:    []string{"No issue text is executed as shell"},
		ExistingBehaviorKnown: true,
		ErrorText:             "unsafe shim behavior: script tried to run issue body through bash -c",
	})

	if analysis.Category != recovery.FailureUnsafeShimBehavior {
		t.Fatalf("Category = %q, want %q", analysis.Category, recovery.FailureUnsafeShimBehavior)
	}
	if analysis.NextStepTarget != recovery.NextStepShim {
		t.Fatalf("NextStepTarget = %q, want shim", analysis.NextStepTarget)
	}
	if !strings.Contains(strings.Join(analysis.FollowUp.Labels, ","), "agent:security") {
		t.Fatalf("FollowUp.Labels = %+v, want security label", analysis.FollowUp.Labels)
	}
}

func TestAnalyzeFailureStopsRetryRecommendationAtMaxAttempts(t *testing.T) {
	t.Parallel()

	analysis := recovery.AnalyzeFailure(recovery.FailureInput{
		Step:                  "codex_run",
		TicketTitle:           "Refactor runner",
		AcceptanceCriteria:    []string{"worker completes"},
		ExistingBehaviorKnown: true,
		ErrorText:             "context deadline exceeded",
		RetryCount:            3,
		MaxAttempts:           3,
	})

	if analysis.Category != recovery.FailureCodexTimeout {
		t.Fatalf("Category = %q, want %q", analysis.Category, recovery.FailureCodexTimeout)
	}
	if analysis.RetryRecommended {
		t.Fatal("RetryRecommended = true, want false after max attempts")
	}
	if !analysis.MaxAttemptsReached {
		t.Fatal("MaxAttemptsReached = false, want true")
	}
}

func TestFailureAnalysisArtifactIsDeterministicJSON(t *testing.T) {
	t.Parallel()

	analysis := recovery.AnalyzeFailure(recovery.FailureInput{
		Step:                  "codex_run",
		TicketTitle:           "Deploy Odin",
		AcceptanceCriteria:    []string{"systemd healthcheck passes"},
		ExistingBehaviorKnown: true,
		ErrorText:             "systemd service failed during deployment",
	})

	payload, err := recovery.MarshalFailureAnalysisArtifact(analysis)
	if err != nil {
		t.Fatalf("MarshalFailureAnalysisArtifact() error = %v", err)
	}

	var decoded struct {
		FailureAnalysis recovery.FailureAnalysis `json:"failure_analysis"`
	}
	if err := json.Unmarshal([]byte(payload), &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v\n%s", err, payload)
	}
	if decoded.FailureAnalysis.Category != recovery.FailureDeploymentFailure {
		t.Fatalf("decoded category = %q, want deployment_failure", decoded.FailureAnalysis.Category)
	}
	if payload != strings.TrimSpace(payload) {
		t.Fatalf("payload has surrounding whitespace: %q", payload)
	}
}
