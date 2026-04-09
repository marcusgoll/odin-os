package replay

import "testing"

func TestFixtureNormalizedWeightsDefaultsMissingValues(t *testing.T) {
	t.Parallel()

	fixture := Fixture{
		Key:  "router-fixture",
		Mode: ModeReplay,
		Weights: Weights{
			SuccessRate: 2,
		},
	}

	normalized := fixture.NormalizedWeights()
	if normalized.SuccessRate != 2 {
		t.Fatalf("SuccessRate = %v, want 2", normalized.SuccessRate)
	}
	if normalized.Cost != 1 {
		t.Fatalf("Cost = %v, want 1", normalized.Cost)
	}
	if normalized.Latency != 1 {
		t.Fatalf("Latency = %v, want 1", normalized.Latency)
	}
	if normalized.PolicyViolations != 1 {
		t.Fatalf("PolicyViolations = %v, want 1", normalized.PolicyViolations)
	}
	if normalized.OperatorInterventions != 1 {
		t.Fatalf("OperatorInterventions = %v, want 1", normalized.OperatorInterventions)
	}
}
