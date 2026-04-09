package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	runtimeevents "odin-os/internal/runtime/events"
	"odin-os/internal/runtime/projections"
)

func TestStoreMigrateLifecycleAndReopen(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "odin.db")

	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() first run error = %v", err)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() second run error = %v", err)
	}

	project, err := store.CreateProject(ctx, CreateProjectParams{
		Key:           "odin-core",
		Name:          "Odin Core",
		Scope:         "odin-core",
		GitRoot:       "/home/orchestrator/odin-os",
		DefaultBranch: "main",
		GitHubRepo:    "example/odin-os",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}

	task, err := store.CreateTask(ctx, CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "phase-03",
		Title:       "Implement runtime store",
		Status:      "queued",
		Scope:       "odin-core",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	task, err = store.UpdateTaskStatus(ctx, UpdateTaskStatusParams{
		TaskID: task.ID,
		Status: "running",
	})
	if err != nil {
		t.Fatalf("UpdateTaskStatus(running) error = %v", err)
	}

	run, err := store.StartRun(ctx, StartRunParams{
		TaskID:   task.ID,
		Executor: "codex",
		Attempt:  1,
		Status:   "running",
	})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}

	run, err = store.FinishRun(ctx, FinishRunParams{
		RunID:   run.ID,
		Status:  "completed",
		Summary: "store baseline complete",
	})
	if err != nil {
		t.Fatalf("FinishRun() error = %v", err)
	}

	task, err = store.UpdateTaskStatus(ctx, UpdateTaskStatusParams{
		TaskID: task.ID,
		Status: "completed",
	})
	if err != nil {
		t.Fatalf("UpdateTaskStatus(completed) error = %v", err)
	}

	approval, err := store.RequestApproval(ctx, RequestApprovalParams{
		TaskID:      task.ID,
		RunID:       &run.ID,
		Status:      "pending",
		RequestedBy: "system",
	})
	if err != nil {
		t.Fatalf("RequestApproval() error = %v", err)
	}

	approval, err = store.ResolveApproval(ctx, ResolveApprovalParams{
		ApprovalID: approval.ID,
		Status:     "approved",
		DecisionBy: "operator",
		Reason:     "safe to proceed",
	})
	if err != nil {
		t.Fatalf("ResolveApproval() error = %v", err)
	}

	incident, err := store.OpenIncident(ctx, OpenIncidentParams{
		RunID:       &run.ID,
		Severity:    "warning",
		Status:      "open",
		Summary:     "transient issue observed",
		DetailsJSON: `{"stage":"verification"}`,
	})
	if err != nil {
		t.Fatalf("OpenIncident() error = %v", err)
	}

	recovery, err := store.StartRecovery(ctx, StartRecoveryParams{
		IncidentID:  &incident.ID,
		RunID:       &run.ID,
		Status:      "running",
		Strategy:    "retry-once",
		DetailsJSON: `{"attempt":1}`,
	})
	if err != nil {
		t.Fatalf("StartRecovery() error = %v", err)
	}

	recovery, err = store.CompleteRecovery(ctx, CompleteRecoveryParams{
		RecoveryID:  recovery.ID,
		Status:      "completed",
		DetailsJSON: `{"result":"success"}`,
	})
	if err != nil {
		t.Fatalf("CompleteRecovery() error = %v", err)
	}

	if _, err := store.RecordRegistryVersion(ctx, RecordRegistryVersionParams{
		Source:      "registry",
		VersionHash: "abc123",
		Notes:       "phase 02 baseline",
	}); err != nil {
		t.Fatalf("RecordRegistryVersion() error = %v", err)
	}

	if _, err := store.RecordExecutorHealth(ctx, RecordExecutorHealthParams{
		Executor:    "codex",
		Status:      "healthy",
		LatencyMS:   42,
		DetailsJSON: `{"mode":"local"}`,
	}); err != nil {
		t.Fatalf("RecordExecutorHealth() error = %v", err)
	}

	if _, err := store.CreateContextPacket(ctx, CreateContextPacketParams{
		TaskID:      &task.ID,
		RunID:       &run.ID,
		PacketKind:  "wake",
		Summary:     "handoff state",
		PayloadJSON: `{"task":"phase-03"}`,
	}); err != nil {
		t.Fatalf("CreateContextPacket() error = %v", err)
	}

	allEvents, err := store.ListEvents(ctx, ListEventsParams{})
	if err != nil {
		t.Fatalf("ListEvents(all) error = %v", err)
	}

	if len(allEvents) != 14 {
		t.Fatalf("ListEvents(all) len = %d, want 14", len(allEvents))
	}

	if allEvents[0].Type != runtimeevents.EventProjectCreated {
		t.Fatalf("first event type = %q, want %q", allEvents[0].Type, runtimeevents.EventProjectCreated)
	}

	packetEventPayload, err := runtimeevents.DecodePayload[runtimeevents.ContextPacketCreatedPayload](allEvents[len(allEvents)-1].Payload)
	if err != nil {
		t.Fatalf("DecodePayload(ContextPacketCreatedPayload) error = %v", err)
	}
	if packetEventPayload.PacketScope != "task_wake_packet" {
		t.Fatalf("context packet event scope = %q, want %q", packetEventPayload.PacketScope, "task_wake_packet")
	}
	if packetEventPayload.Trigger != "handoff" {
		t.Fatalf("context packet event trigger = %q, want %q", packetEventPayload.Trigger, "handoff")
	}

	views, err := projections.ListTaskStatusViews(ctx, store.DB())
	if err != nil {
		t.Fatalf("ListTaskStatusViews() error = %v", err)
	}
	if len(views) != 1 || views[0].Status != "completed" {
		t.Fatalf("task views = %+v, want one completed task", views)
	}

	pendingApprovals, err := projections.ListPendingApprovalViews(ctx, store.DB())
	if err != nil {
		t.Fatalf("ListPendingApprovalViews() error = %v", err)
	}
	if len(pendingApprovals) != 0 {
		t.Fatalf("pending approvals = %d, want 0", len(pendingApprovals))
	}

	runViews, err := projections.ListRunSummaryViews(ctx, store.DB())
	if err != nil {
		t.Fatalf("ListRunSummaryViews() error = %v", err)
	}
	if len(runViews) != 1 || runViews[0].Status != "completed" {
		t.Fatalf("run views = %+v, want one completed run", runViews)
	}

	projectViews, err := projections.ListProjectTransitionViews(ctx, store.DB())
	if err != nil {
		t.Fatalf("ListProjectTransitionViews() error = %v", err)
	}
	if len(projectViews) != 1 || projectViews[0].TaskCount != 1 {
		t.Fatalf("project views = %+v, want one project with one task", projectViews)
	}

	var migrationCount int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations`).Scan(&migrationCount); err != nil {
		t.Fatalf("schema_migrations count query error = %v", err)
	}
	if migrationCount != 2 {
		t.Fatalf("schema_migrations count = %d, want 2", migrationCount)
	}

	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	reopened, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open(reopen) error = %v", err)
	}
	defer reopened.Close()

	if err := reopened.Migrate(ctx); err != nil {
		t.Fatalf("Migrate(reopen) error = %v", err)
	}

	gotTask, err := reopened.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if gotTask.Status != "completed" {
		t.Fatalf("GetTask().Status = %q, want %q", gotTask.Status, "completed")
	}

	gotRun, err := reopened.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if gotRun.Status != "completed" {
		t.Fatalf("GetRun().Status = %q, want %q", gotRun.Status, "completed")
	}

	gotApproval, err := reopened.GetApproval(ctx, approval.ID)
	if err != nil {
		t.Fatalf("GetApproval() error = %v", err)
	}
	if gotApproval.Status != "approved" {
		t.Fatalf("GetApproval().Status = %q, want %q", gotApproval.Status, "approved")
	}
}
