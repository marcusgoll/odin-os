package projections_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"odin-os/internal/runtime/projections"
	"odin-os/internal/store/sqlite"
)

func TestObservabilityProjectionsExposeActiveRunsBlockedItemsIncidentsAndRecoveries(t *testing.T) {
	ctx := context.Background()
	store := openObservabilityStore(t)
	defer store.Close()

	project, task, run := seedObservabilityState(t, ctx, store)

	approval, err := store.RequestApproval(ctx, sqlite.RequestApprovalParams{
		TaskID:      task.ID,
		RunID:       &run.ID,
		Status:      "pending",
		RequestedBy: "system",
	})
	if err != nil {
		t.Fatalf("RequestApproval() error = %v", err)
	}

	if _, err := store.CreateContextPacket(ctx, sqlite.CreateContextPacketParams{
		TaskID:        &task.ID,
		RunID:         &run.ID,
		PacketKind:    "wake",
		PacketScope:   "task_wake_packet",
		Trigger:       "approval_wait",
		CheckpointKey: "wake-1",
		Status:        "active",
		Summary:       "waiting on approval",
		PayloadJSON:   fmt.Sprintf(`{"task_id":%d,"task_key":"%s","scope":"project","objective":"Resume work","status":"waiting","trigger":"approval_wait","blocking_reason":"awaiting operator approval","next_steps":["resume once approved"]}`, task.ID, task.Key),
	}); err != nil {
		t.Fatalf("CreateContextPacket() error = %v", err)
	}

	incident, err := store.OpenIncident(ctx, sqlite.OpenIncidentParams{
		RunID:       &run.ID,
		Severity:    "warning",
		Status:      "open",
		Summary:     "executor degraded",
		DetailsJSON: `{"stage":"build"}`,
	})
	if err != nil {
		t.Fatalf("OpenIncident() error = %v", err)
	}

	if _, err := store.StartRecovery(ctx, sqlite.StartRecoveryParams{
		IncidentID:  &incident.ID,
		RunID:       &run.ID,
		Status:      "running",
		Strategy:    "retry-once",
		DetailsJSON: `{"attempt":1}`,
	}); err != nil {
		t.Fatalf("StartRecovery() error = %v", err)
	}

	activeRuns, err := projections.ListActiveRunViews(ctx, store.DB())
	if err != nil {
		t.Fatalf("ListActiveRunViews() error = %v", err)
	}
	if len(activeRuns) != 1 || activeRuns[0].RunID != run.ID {
		t.Fatalf("active runs = %+v, want run %d", activeRuns, run.ID)
	}

	blocked, err := projections.ListBlockedItemViews(ctx, store.DB())
	if err != nil {
		t.Fatalf("ListBlockedItemViews() error = %v", err)
	}
	if len(blocked) != 1 {
		t.Fatalf("blocked items len = %d, want 1 after dedupe", len(blocked))
	}

	approvals, err := projections.ListPendingApprovalViews(ctx, store.DB())
	if err != nil {
		t.Fatalf("ListPendingApprovalViews() error = %v", err)
	}
	if len(approvals) != 1 || approvals[0].ApprovalID != approval.ID {
		t.Fatalf("approvals = %+v, want pending approval %d", approvals, approval.ID)
	}

	incidents, err := projections.ListIncidentViews(ctx, store.DB())
	if err != nil {
		t.Fatalf("ListIncidentViews() error = %v", err)
	}
	if len(incidents) != 1 || incidents[0].IncidentID != incident.ID {
		t.Fatalf("incidents = %+v, want incident %d", incidents, incident.ID)
	}

	recoveries, err := projections.ListRecoveryViews(ctx, store.DB())
	if err != nil {
		t.Fatalf("ListRecoveryViews() error = %v", err)
	}
	if len(recoveries) != 1 || recoveries[0].RunID != run.ID {
		t.Fatalf("recoveries = %+v, want run %d", recoveries, run.ID)
	}

	_ = project
}

func TestObservabilityProjectionsDeduplicateTaskBlockedItems(t *testing.T) {
	ctx := context.Background()
	store := openObservabilityStore(t)
	defer store.Close()

	project, task, run := seedObservabilityState(t, ctx, store)

	if _, err := store.BlockTask(ctx, sqlite.BlockTaskParams{
		TaskID: task.ID,
		Reason: "approval_required",
	}); err != nil {
		t.Fatalf("BlockTask() error = %v", err)
	}
	if _, err := store.CreateContextPacket(ctx, sqlite.CreateContextPacketParams{
		TaskID:        &task.ID,
		RunID:         &run.ID,
		PacketKind:    "wake",
		PacketScope:   "task_wake_packet",
		Trigger:       "approval_wait",
		CheckpointKey: "wake-1",
		Status:        "active",
		Summary:       "waiting on approval",
		PayloadJSON:   `{"blocking_reason":"approval_required"}`,
	}); err != nil {
		t.Fatalf("CreateContextPacket() error = %v", err)
	}

	blocked, err := projections.ListBlockedItemViews(ctx, store.DB())
	if err != nil {
		t.Fatalf("ListBlockedItemViews() error = %v", err)
	}

	var taskEntries int
	for _, item := range blocked {
		if item.TaskID == task.ID {
			taskEntries++
			if item.Source != "task" {
				t.Fatalf("blocked item source = %q, want task", item.Source)
			}
		}
	}
	if taskEntries != 1 {
		t.Fatalf("task blocked entries = %d, want 1; blocked=%+v", taskEntries, blocked)
	}

	_ = project
}

func TestObservabilityProjectionsExposeFreshnessAndPortfolioViews(t *testing.T) {
	ctx := context.Background()
	store := openObservabilityStore(t)
	defer store.Close()

	project, _, run := seedObservabilityState(t, ctx, store)

	if _, err := store.RecordProjectionFreshness(ctx, sqlite.RecordProjectionFreshnessParams{
		Surface:     "doctor",
		Status:      "healthy",
		DetailsJSON: `{"source":"runtime"}`,
	}); err != nil {
		t.Fatalf("RecordProjectionFreshness() error = %v", err)
	}
	if _, err := store.RecordRegistryVersion(ctx, sqlite.RecordRegistryVersionParams{
		Source:      "registry",
		VersionHash: "abc123",
		Notes:       "fresh compile",
	}); err != nil {
		t.Fatalf("RecordRegistryVersion() error = %v", err)
	}
	if _, err := store.RecordExecutorHealth(ctx, sqlite.RecordExecutorHealthParams{
		Executor:    run.Executor,
		Status:      "healthy",
		LatencyMS:   42,
		DetailsJSON: `{"mode":"local"}`,
	}); err != nil {
		t.Fatalf("RecordExecutorHealth() error = %v", err)
	}

	freshness, err := projections.ListFreshnessViews(ctx, store.DB())
	if err != nil {
		t.Fatalf("ListFreshnessViews() error = %v", err)
	}
	if len(freshness) == 0 {
		t.Fatalf("freshness len = 0, want > 0")
	}

	portfolio, err := projections.ListProjectPortfolioViews(ctx, store.DB())
	if err != nil {
		t.Fatalf("ListProjectPortfolioViews() error = %v", err)
	}
	if len(portfolio) != 1 || portfolio[0].ProjectKey != project.Key {
		t.Fatalf("portfolio = %+v, want project %q", portfolio, project.Key)
	}
}

func openObservabilityStore(t *testing.T) *sqlite.Store {
	t.Helper()

	store, err := sqlite.Open(filepath.Join(t.TempDir(), "odin.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	return store
}

func seedObservabilityState(t *testing.T, ctx context.Context, store *sqlite.Store) (sqlite.Project, sqlite.Task, sqlite.Run) {
	t.Helper()

	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "alpha",
		Name:          "Alpha",
		Scope:         "project",
		GitRoot:       "/tmp/alpha",
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}

	task, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "alpha-task",
		Title:       "Alpha task",
		Status:      "running",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	run, err := store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:   task.ID,
		Executor: "codex",
		Attempt:  1,
		Status:   "running",
	})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}

	return project, task, run
}
