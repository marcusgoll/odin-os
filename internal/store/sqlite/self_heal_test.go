package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	runtimeevents "odin-os/internal/runtime/events"
)

func TestSelfHealRecordsRecoveryActionAndIncidentEscalation(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "odin.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
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

	task, err := store.CreateTask(ctx, CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "phase-11",
		Title:       "Recover stale projection",
		Status:      "running",
		Scope:       "odin-core",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	run, err := store.StartRun(ctx, StartRunParams{
		TaskID:   task.ID,
		Executor: "codex_headless",
		Attempt:  1,
		Status:   "running",
	})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}

	incident, err := store.OpenIncident(ctx, OpenIncidentParams{
		RunID:       &run.ID,
		Severity:    "warning",
		Status:      "open",
		Summary:     "projection freshness is stale",
		DetailsJSON: `{"fault_key":"projection_stale","subject_key":"doctor"}`,
	})
	if err != nil {
		t.Fatalf("OpenIncident() error = %v", err)
	}

	recovery, err := store.StartRecovery(ctx, StartRecoveryParams{
		IncidentID:  &incident.ID,
		RunID:       &run.ID,
		Status:      "running",
		Strategy:    "refresh_projection_freshness",
		DetailsJSON: `{"fault_key":"projection_stale"}`,
	})
	if err != nil {
		t.Fatalf("StartRecovery() error = %v", err)
	}

	if err := store.RecordRecoveryAction(ctx, RecordRecoveryActionParams{
		RecoveryID:  recovery.ID,
		Playbook:    "refresh_projection_freshness",
		FaultKey:    "projection_stale",
		ActionName:  "refresh_projection_surface",
		Attempt:     1,
		Result:      "completed",
		Description: "refreshed doctor projection freshness",
	}); err != nil {
		t.Fatalf("RecordRecoveryAction() error = %v", err)
	}

	incident, err = store.UpdateIncidentStatus(ctx, UpdateIncidentStatusParams{
		IncidentID:  incident.ID,
		Status:      "escalated",
		Reason:      "repeated stale projection after bounded retries",
		DetailsJSON: `{"fault_key":"projection_stale","escalated_by":"self_heal"}`,
	})
	if err != nil {
		t.Fatalf("UpdateIncidentStatus(escalated) error = %v", err)
	}

	if incident.Status != "escalated" {
		t.Fatalf("incident.Status = %q, want escalated", incident.Status)
	}

	events, err := store.ListEvents(ctx, ListEventsParams{RunID: &run.ID})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}

	var sawRecoveryAction bool
	var sawIncidentEscalated bool
	for _, event := range events {
		switch event.Type {
		case runtimeevents.EventRecoveryActionExecuted:
			payload, err := runtimeevents.DecodePayload[runtimeevents.RecoveryActionExecutedPayload](event.Payload)
			if err != nil {
				t.Fatalf("DecodePayload(RecoveryActionExecutedPayload) error = %v", err)
			}
			if payload.Playbook != "refresh_projection_freshness" || payload.Attempt != 1 {
				t.Fatalf("recovery action payload = %+v, want playbook and attempt recorded", payload)
			}
			sawRecoveryAction = true
		case runtimeevents.EventIncidentEscalated:
			payload, err := runtimeevents.DecodePayload[runtimeevents.IncidentEscalatedPayload](event.Payload)
			if err != nil {
				t.Fatalf("DecodePayload(IncidentEscalatedPayload) error = %v", err)
			}
			if payload.Status != "escalated" {
				t.Fatalf("incident escalated payload = %+v, want escalated status", payload)
			}
			sawIncidentEscalated = true
		}
	}

	if !sawRecoveryAction {
		t.Fatalf("expected recovery action event, got %+v", events)
	}
	if !sawIncidentEscalated {
		t.Fatalf("expected incident escalated event, got %+v", events)
	}
}

func TestSelfHealResolvesIncidentWhenRecoverySucceeds(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "odin.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	project, err := store.CreateProject(ctx, CreateProjectParams{
		Key:           "demo",
		Name:          "Demo",
		Scope:         "project",
		GitRoot:       "/tmp/demo",
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}

	task, err := store.CreateTask(ctx, CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "demo-task",
		Title:       "Refresh source freshness",
		Status:      "running",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	run, err := store.StartRun(ctx, StartRunParams{
		TaskID:   task.ID,
		Executor: "openai_api",
		Attempt:  1,
		Status:   "running",
	})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}

	incident, err := store.OpenIncident(ctx, OpenIncidentParams{
		RunID:       &run.ID,
		Severity:    "warning",
		Status:      "open",
		Summary:     "registry source freshness is stale",
		DetailsJSON: `{"fault_key":"source_freshness_stale"}`,
	})
	if err != nil {
		t.Fatalf("OpenIncident() error = %v", err)
	}

	incident, err = store.UpdateIncidentStatus(ctx, UpdateIncidentStatusParams{
		IncidentID:  incident.ID,
		Status:      "resolved",
		Reason:      "registry reload completed",
		DetailsJSON: `{"fault_key":"source_freshness_stale","resolved_by":"self_heal"}`,
	})
	if err != nil {
		t.Fatalf("UpdateIncidentStatus(resolved) error = %v", err)
	}

	if incident.Status != "resolved" {
		t.Fatalf("incident.Status = %q, want resolved", incident.Status)
	}

	gotIncident, err := store.GetIncident(ctx, incident.ID)
	if err != nil {
		t.Fatalf("GetIncident() error = %v", err)
	}
	if gotIncident.Status != "resolved" {
		t.Fatalf("GetIncident().Status = %q, want resolved", gotIncident.Status)
	}

	events, err := store.ListEvents(ctx, ListEventsParams{RunID: &run.ID})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}

	var sawIncidentResolved bool
	for _, event := range events {
		if event.Type != runtimeevents.EventIncidentResolved {
			continue
		}
		payload, err := runtimeevents.DecodePayload[runtimeevents.IncidentResolvedPayload](event.Payload)
		if err != nil {
			t.Fatalf("DecodePayload(IncidentResolvedPayload) error = %v", err)
		}
		if payload.Status != "resolved" {
			t.Fatalf("incident resolved payload = %+v, want resolved status", payload)
		}
		sawIncidentResolved = true
	}

	if !sawIncidentResolved {
		t.Fatalf("expected incident resolved event, got %+v", events)
	}
}
