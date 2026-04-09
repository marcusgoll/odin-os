package evaluator

import (
	"fmt"

	"odin-os/internal/learning/replay"
)

type Service struct {
	ApprovalThreshold float64
}

type Result struct {
	Score   float64
	Outcome string
}

func (service Service) Evaluate(fixture replay.Fixture) (Result, error) {
	if fixture.Key == "" {
		return Result{}, fmt.Errorf("evaluation fixture key is required")
	}

	weights := fixture.NormalizedWeights()
	score := 0.0
	score += weights.SuccessRate * (fixture.Candidate.SuccessRate - fixture.Baseline.SuccessRate)
	score += weights.Cost * normalizedImprovement(fixture.Baseline.Cost, fixture.Candidate.Cost)
	score += weights.Latency * normalizedImprovement(fixture.Baseline.LatencyMS, fixture.Candidate.LatencyMS)
	score += weights.PolicyViolations * float64(fixture.Baseline.PolicyViolations-fixture.Candidate.PolicyViolations)
	score += weights.OperatorInterventions * float64(fixture.Baseline.OperatorInterventions-fixture.Candidate.OperatorInterventions)

	outcome := "rejected"
	if score > service.ApprovalThreshold {
		outcome = "approved"
	}

	return Result{
		Score:   score,
		Outcome: outcome,
	}, nil
}

func normalizedImprovement(baseline float64, candidate float64) float64 {
	if baseline <= 0 {
		return baseline - candidate
	}
	return (baseline - candidate) / baseline
}
