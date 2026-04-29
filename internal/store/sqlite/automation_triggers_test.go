package sqlite

import (
	"context"
	"testing"
	"time"

	runtimeevents "odin-os/internal/runtime/events"
)

func TestAutomationTriggerFireMaterializesGovernedWorkItemIdempotently(t *testing.T) {
	ctx := context.Background()
	store := openMigratedTaskIntakeStore(t, "automation-triggers.db")
	defer store.Close()

	store.Now = func() time.Time {
		return time.Date(2026, 4, 25, 12, 0, 0, 0, time.UTC)
	}

	project, err := store.CreateProject(ctx, CreateProjectParams{
		Key:           "odin-core",
		Name:          "Odin Core",
		Scope:         "odin-core",
		GitRoot:       "/home/orchestrator/odin-os",
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}

	trigger, err := store.UpsertAutomationTrigger(ctx, UpsertAutomationTriggerParams{
		WorkspaceID:   "default",
		Key:           "nightly-ops",
		ProjectID:     project.ID,
		InitiativeKey: "odin-core",
		Kind:          "schedule",
		Status:        "enabled",
		RuleJSON:      `{"cadence":"manual"}`,
		RuleSummary:   "manual cadence for native trigger proof",
		WorkItemTitle: "Run nightly operator review",
	})
	if err != nil {
		t.Fatalf("UpsertAutomationTrigger() error = %v", err)
	}
	if trigger.Kind != "schedule" || trigger.Status != "enabled" {
		t.Fatalf("trigger = %+v, want enabled schedule trigger", trigger)
	}

	listed, err := store.ListAutomationTriggers(ctx, ListAutomationTriggersParams{WorkspaceID: "default"})
	if err != nil {
		t.Fatalf("ListAutomationTriggers() error = %v", err)
	}
	if len(listed) != 1 || listed[0].Key != "nightly-ops" {
		t.Fatalf("ListAutomationTriggers() = %+v, want nightly-ops trigger", listed)
	}

	first, err := store.FireAutomationTrigger(ctx, FireAutomationTriggerParams{
		WorkspaceID: "default",
		Key:         "nightly-ops",
		Reason:      "operator-check",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("FireAutomationTrigger(first) error = %v", err)
	}
	if !first.CreatedWorkItem {
		t.Fatalf("CreatedWorkItem = false, want first fire to create governed work")
	}
	if first.WorkItem.Status != "queued" {
		t.Fatalf("WorkItem.Status = %q, want queued", first.WorkItem.Status)
	}
	if first.WorkItem.Scope != "odin-core" {
		t.Fatalf("WorkItem.Scope = %q, want odin-core", first.WorkItem.Scope)
	}
	if first.WorkItem.RequestedBy != "automation_trigger:nightly-ops" {
		t.Fatalf("WorkItem.RequestedBy = %q, want automation trigger provenance", first.WorkItem.RequestedBy)
	}
	if first.Materialization.MaterializationKey != "default:nightly-ops:manual:operator-check" {
		t.Fatalf("MaterializationKey = %q, want deterministic trigger materialization key", first.Materialization.MaterializationKey)
	}

	second, err := store.FireAutomationTrigger(ctx, FireAutomationTriggerParams{
		WorkspaceID: "default",
		Key:         "nightly-ops",
		Reason:      "operator-check",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("FireAutomationTrigger(second) error = %v", err)
	}
	if second.CreatedWorkItem {
		t.Fatalf("CreatedWorkItem = true on duplicate fire, want existing materialization reused")
	}
	if second.WorkItem.ID != first.WorkItem.ID {
		t.Fatalf("duplicate fire WorkItem.ID = %d, want original %d", second.WorkItem.ID, first.WorkItem.ID)
	}

	events, err := store.ListEvents(ctx, ListEventsParams{})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	counts := map[runtimeevents.Type]int{}
	for _, event := range events {
		counts[event.Type]++
	}
	for _, want := range []runtimeevents.Type{
		runtimeevents.EventAutomationTriggerCreated,
		runtimeevents.EventAutomationTriggerFireRequested,
		runtimeevents.EventAutomationTriggerEvaluated,
		runtimeevents.EventAutomationTriggerMaterialized,
		runtimeevents.EventTaskCreated,
	} {
		if counts[want] == 0 {
			t.Fatalf("event counts = %#v, missing %s", counts, want)
		}
	}
	if counts[runtimeevents.EventAutomationTriggerMaterialized] != 1 {
		t.Fatalf("materialized events = %d, want one idempotent materialization", counts[runtimeevents.EventAutomationTriggerMaterialized])
	}
}
