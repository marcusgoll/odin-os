package evaluator

import (
	"testing"

	"odin-os/internal/learning/replay"
)

func TestEvaluateReplayFixtureScoresImprovementDeterministically(t *testing.T) {
	t.Parallel()

	service := Service{ApprovalThreshold: 0}
	fixture := replay.Fixture{
		Key:  "router-latency",
		Mode: replay.ModeReplay,
		Baseline: replay.Metrics{
			SuccessRate:           0.93,
			Cost:                  0.021,
			LatencyMS:             220,
			PolicyViolations:      0,
			OperatorInterventions: 1,
		},
		Candidate: replay.Metrics{
			SuccessRate:           0.95,
			Cost:                  0.018,
			LatencyMS:             180,
			PolicyViolations:      0,
			OperatorInterventions: 0,
		},
		Weights: replay.Weights{
			SuccessRate:           3,
			Cost:                  1,
			Latency:               2,
			PolicyViolations:      5,
			OperatorInterventions: 1,
		},
	}

	result, err := service.Evaluate(fixture)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if result.Score <= 0 {
		t.Fatalf("Score = %v, want > 0", result.Score)
	}
	if result.Outcome != "approved" {
		t.Fatalf("Outcome = %q, want %q", result.Outcome, "approved")
	}

	resultAgain, err := service.Evaluate(fixture)
	if err != nil {
		t.Fatalf("Evaluate() second call error = %v", err)
	}
	if result.Score != resultAgain.Score {
		t.Fatalf("scores differ across identical replay fixtures: %v vs %v", result.Score, resultAgain.Score)
	}
}

func TestEvaluateSandboxFixtureRejectsRegressionDeterministically(t *testing.T) {
	t.Parallel()

	service := Service{ApprovalThreshold: 0}
	fixture := replay.Fixture{
		Key:  "retry-regression",
		Mode: replay.ModeSandbox,
		Baseline: replay.Metrics{
			SuccessRate:           0.97,
			Cost:                  0.014,
			LatencyMS:             120,
			PolicyViolations:      0,
			OperatorInterventions: 0,
		},
		Candidate: replay.Metrics{
			SuccessRate:           0.91,
			Cost:                  0.019,
			LatencyMS:             180,
			PolicyViolations:      1,
			OperatorInterventions: 2,
		},
		Weights: replay.Weights{
			SuccessRate:           3,
			Cost:                  1,
			Latency:               1,
			PolicyViolations:      5,
			OperatorInterventions: 2,
		},
	}

	result, err := service.Evaluate(fixture)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if result.Score >= 0 {
		t.Fatalf("Score = %v, want < 0", result.Score)
	}
	if result.Outcome != "rejected" {
		t.Fatalf("Outcome = %q, want %q", result.Outcome, "rejected")
	}
}
