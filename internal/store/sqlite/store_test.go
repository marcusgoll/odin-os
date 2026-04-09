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
	if migrationCount != 5 {
		t.Fatalf("schema_migrations count = %d, want 5", migrationCount)
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

func TestProjectTransitionStateLifecycle(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "odin.db")

	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	project, err := store.CreateProject(ctx, CreateProjectParams{
		Key:           "cfipros",
		Name:          "CFIPros",
		Scope:         "project",
		GitRoot:       "/tmp/cfipros",
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}

	transition, err := store.SetProjectTransition(ctx, SetProjectTransitionParams{
		ProjectID:          project.ID,
		State:              "inventory",
		Controller:         "legacy_odin",
		LimitedActionsJSON: "",
		Notes:              "initial enrollment",
		ChangedBy:          "operator",
	})
	if err != nil {
		t.Fatalf("SetProjectTransition(inventory) error = %v", err)
	}

	if transition.State != "inventory" {
		t.Fatalf("transition.State = %q, want %q", transition.State, "inventory")
	}
	if transition.Controller != "legacy_odin" {
		t.Fatalf("transition.Controller = %q, want %q", transition.Controller, "legacy_odin")
	}

	transition, err = store.SetProjectTransition(ctx, SetProjectTransitionParams{
		ProjectID:          project.ID,
		State:              "limited_action",
		Controller:         "odin_os",
		LimitedActionsJSON: `["isolated_mutation"]`,
		Notes:              "allow proposal work only",
		ChangedBy:          "operator",
	})
	if err != nil {
		t.Fatalf("SetProjectTransition(limited_action) error = %v", err)
	}

	if transition.State != "limited_action" {
		t.Fatalf("transition.State = %q, want %q", transition.State, "limited_action")
	}
	if transition.Controller != "odin_os" {
		t.Fatalf("transition.Controller = %q, want %q", transition.Controller, "odin_os")
	}
	if transition.LimitedActionsJSON != `["isolated_mutation"]` {
		t.Fatalf("transition.LimitedActionsJSON = %q, want %q", transition.LimitedActionsJSON, `["isolated_mutation"]`)
	}

	got, err := store.GetProjectTransition(ctx, project.ID)
	if err != nil {
		t.Fatalf("GetProjectTransition() error = %v", err)
	}

	if got.State != "limited_action" {
		t.Fatalf("GetProjectTransition().State = %q, want %q", got.State, "limited_action")
	}

	projectEvents, err := store.ListEvents(ctx, ListEventsParams{
		ProjectID: &project.ID,
	})
	if err != nil {
		t.Fatalf("ListEvents(project) error = %v", err)
	}

	var transitionEvents int
	for _, event := range projectEvents {
		if event.Type == runtimeevents.EventProjectTransitionChanged {
			transitionEvents++
		}
	}
	if transitionEvents != 2 {
		t.Fatalf("transition event count = %d, want 2", transitionEvents)
	}
}

func TestProjectTransitionReportsAreAppendOnly(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "odin.db")

	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	project, err := store.CreateProject(ctx, CreateProjectParams{
		Key:           "cfipros",
		Name:          "CFIPros",
		Scope:         "project",
		GitRoot:       "/tmp/cfipros",
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}

	if _, err := store.SetProjectTransition(ctx, SetProjectTransitionParams{
		ProjectID:  project.ID,
		State:      "compare",
		Controller: "legacy_odin",
		ChangedBy:  "operator",
		Notes:      "compare before cutover",
	}); err != nil {
		t.Fatalf("SetProjectTransition(compare) error = %v", err)
	}

	shadowReport, err := store.RecordProjectTransitionReport(ctx, RecordProjectTransitionReportParams{
		ProjectID:   project.ID,
		ReportType:  "shadow_observation",
		Summary:     "legacy run observed",
		DetailsJSON: `{"task":"deploy","status":"completed"}`,
	})
	if err != nil {
		t.Fatalf("RecordProjectTransitionReport(shadow) error = %v", err)
	}

	compareReport, err := store.RecordProjectTransitionReport(ctx, RecordProjectTransitionReportParams{
		ProjectID:   project.ID,
		ReportType:  "compare_report",
		Summary:     "decision mismatch",
		DetailsJSON: `{"legacy_summary":"ship","odin_summary":"hold","verdict":"mismatch"}`,
	})
	if err != nil {
		t.Fatalf("RecordProjectTransitionReport(compare) error = %v", err)
	}

	if shadowReport.ID == compareReport.ID {
		t.Fatalf("report ids should differ, both were %d", shadowReport.ID)
	}

	reports, err := store.ListProjectTransitionReports(ctx, project.ID)
	if err != nil {
		t.Fatalf("ListProjectTransitionReports() error = %v", err)
	}

	if len(reports) != 2 {
		t.Fatalf("ListProjectTransitionReports() len = %d, want 2", len(reports))
	}
	if reports[0].ReportType != "shadow_observation" {
		t.Fatalf("reports[0].ReportType = %q, want %q", reports[0].ReportType, "shadow_observation")
	}
	if reports[1].ReportType != "compare_report" {
		t.Fatalf("reports[1].ReportType = %q, want %q", reports[1].ReportType, "compare_report")
	}

	projectEvents, err := store.ListEvents(ctx, ListEventsParams{
		ProjectID: &project.ID,
	})
	if err != nil {
		t.Fatalf("ListEvents(project) error = %v", err)
	}

	var shadowEvents int
	var compareEvents int
	for _, event := range projectEvents {
		switch event.Type {
		case runtimeevents.EventProjectShadowObservationRecorded:
			shadowEvents++
		case runtimeevents.EventProjectCompareReportRecorded:
			compareEvents++
		}
	}

	if shadowEvents != 1 {
		t.Fatalf("shadow event count = %d, want 1", shadowEvents)
	}
	if compareEvents != 1 {
		t.Fatalf("compare event count = %d, want 1", compareEvents)
	}
}
