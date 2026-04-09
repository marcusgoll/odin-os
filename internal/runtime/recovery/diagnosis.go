package recovery

type Diagnoser struct{}

func (Diagnoser) Diagnose(observations []Observation) []Decision {
	decisions := make([]Decision, 0, len(observations))
	for _, observation := range observations {
		playbook := playbookForFault(observation.FaultKey)
		if playbook == "" {
			continue
		}
		decisions = append(decisions, Decision{
			Observation: observation,
			Playbook:    playbook,
		})
	}
	return decisions
}

func playbookForFault(faultKey FaultKey) string {
	switch faultKey {
	case FaultExecutorHealthStale:
		return "refresh_executor_health"
	case FaultProjectionStale:
		return "refresh_projection_freshness"
	case FaultSourceFreshnessStale:
		return "reload_registry_source"
	case FaultQueuePressureHigh:
		return "escalate_queue_pressure"
	case FaultRunFailureRepeated:
		return "checkpoint_failed_run"
	default:
		return ""
	}
}
