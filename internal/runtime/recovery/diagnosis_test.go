package recovery_test

import (
	"testing"

	"odin-os/internal/runtime/recovery"
)

func TestDiagnosisSelectsPlaybooksForKnownFaults(t *testing.T) {
	diagnoser := recovery.Diagnoser{}

	observations := []recovery.Observation{
		{FaultKey: recovery.FaultExecutorHealthStale, SubjectKey: "codex_headless", Scope: "global"},
		{FaultKey: recovery.FaultProjectionStale, SubjectKey: "doctor", Scope: "global"},
		{FaultKey: recovery.FaultSourceFreshnessStale, SubjectKey: "registry", Scope: "global"},
		{FaultKey: recovery.FaultQueuePressureHigh, SubjectKey: "task_queue", Scope: "global"},
		{FaultKey: recovery.FaultRunFailureRepeated, SubjectKey: "task:build-app", Scope: "project"},
	}

	decisions := diagnoser.Diagnose(observations)

	if len(decisions) != 5 {
		t.Fatalf("Diagnose() len = %d, want 5", len(decisions))
	}

	assertPlaybook(t, decisions, recovery.FaultExecutorHealthStale, "refresh_executor_health")
	assertPlaybook(t, decisions, recovery.FaultProjectionStale, "refresh_projection_freshness")
	assertPlaybook(t, decisions, recovery.FaultSourceFreshnessStale, "reload_registry_source")
	assertPlaybook(t, decisions, recovery.FaultQueuePressureHigh, "escalate_queue_pressure")
	assertPlaybook(t, decisions, recovery.FaultRunFailureRepeated, "checkpoint_failed_run")
}

func TestDiagnosisEmitsExplicitIgnoreForUnknownFaults(t *testing.T) {
	diagnoser := recovery.Diagnoser{}
	decisions := diagnoser.Diagnose([]recovery.Observation{
		{FaultKey: recovery.FaultKey("unknown_fault"), SubjectKey: "x", Scope: "global"},
	})

	if len(decisions) != 1 {
		t.Fatalf("Diagnose() len = %d, want explicit ignore decision", len(decisions))
	}
	if decisions[0].Mode != recovery.DecisionModeIgnore {
		t.Fatalf("decision.Mode = %q, want %q", decisions[0].Mode, recovery.DecisionModeIgnore)
	}
	if decisions[0].Playbook != "" {
		t.Fatalf("decision.Playbook = %q, want empty for ignored fault", decisions[0].Playbook)
	}
	if decisions[0].Reason == "" {
		t.Fatalf("decision.Reason is empty, want operator-visible ignore reason")
	}
}

func TestDiagnosisUsesIncidentOnlyForInvalidWakePackets(t *testing.T) {
	diagnoser := recovery.Diagnoser{}
	decisions := diagnoser.Diagnose([]recovery.Observation{
		{
			FaultKey:   recovery.FaultWakePacketInvalid,
			SubjectKey: "task:alpha",
			Scope:      "project",
			Severity:   "error",
			Summary:    "wake packet envelope is invalid",
		},
	})

	if len(decisions) != 1 {
		t.Fatalf("Diagnose() len = %d, want one incident-only decision", len(decisions))
	}
	decision := decisions[0]
	if decision.Mode != recovery.DecisionModeIncidentOnly {
		t.Fatalf("decision.Mode = %q, want %q", decision.Mode, recovery.DecisionModeIncidentOnly)
	}
	if decision.Playbook != "" {
		t.Fatalf("decision.Playbook = %q, want no playbook for incident-only fault", decision.Playbook)
	}
	if decision.NextAction == "" {
		t.Fatalf("decision.NextAction is empty, want operator next action")
	}
}

func assertPlaybook(t *testing.T, decisions []recovery.Decision, faultKey recovery.FaultKey, playbook string) {
	t.Helper()
	for _, decision := range decisions {
		if decision.Observation.FaultKey == faultKey {
			if decision.Mode != recovery.DecisionModePlaybook {
				t.Fatalf("mode for %q = %q, want %q", faultKey, decision.Mode, recovery.DecisionModePlaybook)
			}
			if decision.Playbook != playbook {
				t.Fatalf("playbook for %q = %q, want %q", faultKey, decision.Playbook, playbook)
			}
			return
		}
	}
	t.Fatalf("missing decision for %q in %+v", faultKey, decisions)
}
