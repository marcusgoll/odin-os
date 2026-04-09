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

func TestDiagnosisIgnoresUnknownFaults(t *testing.T) {
	diagnoser := recovery.Diagnoser{}
	decisions := diagnoser.Diagnose([]recovery.Observation{
		{FaultKey: recovery.FaultKey("unknown_fault"), SubjectKey: "x", Scope: "global"},
	})

	if len(decisions) != 0 {
		t.Fatalf("Diagnose() = %+v, want no decisions for unknown fault", decisions)
	}
}

func assertPlaybook(t *testing.T, decisions []recovery.Decision, faultKey recovery.FaultKey, playbook string) {
	t.Helper()
	for _, decision := range decisions {
		if decision.Observation.FaultKey == faultKey {
			if decision.Playbook != playbook {
				t.Fatalf("playbook for %q = %q, want %q", faultKey, decision.Playbook, playbook)
			}
			return
		}
	}
	t.Fatalf("missing decision for %q in %+v", faultKey, decisions)
}
