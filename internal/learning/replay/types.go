package replay

type Mode string

const (
	ModeReplay  Mode = "replay"
	ModeSandbox Mode = "sandbox"
)

type Metrics struct {
	SuccessRate           float64
	Cost                  float64
	LatencyMS             float64
	PolicyViolations      int
	OperatorInterventions int
}

type Weights struct {
	SuccessRate           float64
	Cost                  float64
	Latency               float64
	PolicyViolations      float64
	OperatorInterventions float64
}

type Fixture struct {
	Key       string
	Mode      Mode
	Baseline  Metrics
	Candidate Metrics
	Weights   Weights
}

func (fixture Fixture) NormalizedWeights() Weights {
	weights := fixture.Weights
	if weights.SuccessRate == 0 {
		weights.SuccessRate = 1
	}
	if weights.Cost == 0 {
		weights.Cost = 1
	}
	if weights.Latency == 0 {
		weights.Latency = 1
	}
	if weights.PolicyViolations == 0 {
		weights.PolicyViolations = 1
	}
	if weights.OperatorInterventions == 0 {
		weights.OperatorInterventions = 1
	}
	return weights
}
