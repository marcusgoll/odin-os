package recovery

type Diagnoser struct{}

func (Diagnoser) Diagnose(observations []Observation) []Decision {
	decisions := make([]Decision, 0, len(observations))
	for _, observation := range observations {
		if observation.FaultKey == FaultWakePacketInvalid {
			decisions = append(decisions, Decision{
				Observation: observation,
				Mode:        DecisionModeIncidentOnly,
				Reason:      "wake packet envelope is invalid; operator review required",
				NextAction:  "review wake packet evidence",
			})
			continue
		}
		playbook := playbookForFault(observation.FaultKey)
		if playbook == "" {
			decisions = append(decisions, Decision{
				Observation: observation,
				Mode:        DecisionModeIgnore,
				Reason:      "no recovery decision is defined for fault key",
			})
			continue
		}
		decisions = append(decisions, Decision{
			Observation: observation,
			Mode:        DecisionModePlaybook,
			Playbook:    playbook,
			NextAction:  "run deterministic recovery playbook",
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
