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
	if len(blocked) < 2 {
		t.Fatalf("blocked items len = %d, want >= 2", len(blocked))
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

func TestProjectPortfolioTreatsAwaitingApprovalAsBlockedNotActive(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openObservabilityStore(t)
	defer store.Close()

	project, task, run := seedObservabilityState(t, ctx, store)
	if _, _, _, err := store.AwaitApproval(ctx, sqlite.AwaitApprovalParams{
		TaskID:         task.ID,
		RunID:          run.ID,
		RequestedBy:    "odin_os",
		Summary:        "awaiting operator approval",
		TerminalReason: "awaiting operator approval",
		ArtifactsJSON:  `["runs/artifacts/approval.json"]`,
	}); err != nil {
		t.Fatalf("AwaitApproval() error = %v", err)
	}

	portfolio, err := projections.ListProjectPortfolioViews(ctx, store.DB())
	if err != nil {
		t.Fatalf("ListProjectPortfolioViews() error = %v", err)
	}
	if len(portfolio) != 1 || portfolio[0].ProjectKey != project.Key {
		t.Fatalf("portfolio = %+v, want project %q", portfolio, project.Key)
	}
	if portfolio[0].ActiveRunCount != 0 {
		t.Fatalf("active run count = %d, want 0 for awaiting approval", portfolio[0].ActiveRunCount)
	}
	if portfolio[0].PendingApprovalCount != 1 {
		t.Fatalf("pending approval count = %d, want 1", portfolio[0].PendingApprovalCount)
	}

	activeRuns, err := projections.ListActiveRunViews(ctx, store.DB())
	if err != nil {
		t.Fatalf("ListActiveRunViews() error = %v", err)
	}
	if len(activeRuns) != 0 {
		t.Fatalf("active runs = %+v, want 0 for awaiting approval", activeRuns)
	}
}

func TestProjectViewsTreatInterruptedDeadLetterRecoveryAsTerminalWork(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openObservabilityStore(t)
	defer store.Close()

	project, task, run := seedObservabilityState(t, ctx, store)

	if _, err := store.FinishRun(ctx, sqlite.FinishRunParams{
		RunID:          run.ID,
		Status:         "interrupted",
		Summary:        "stalled run retry budget exhausted",
		TerminalReason: "stalled run retry budget exhausted",
	}); err != nil {
		t.Fatalf("FinishRun() error = %v", err)
	}
	if _, err := store.UpdateTaskStatus(ctx, sqlite.UpdateTaskStatusParams{
		TaskID:         task.ID,
		Status:         "dead_letter",
		Summary:        "retry budget exhausted",
		TerminalReason: "retry budget exhausted",
	}); err != nil {
		t.Fatalf("UpdateTaskStatus(dead_letter) error = %v", err)
	}

	transition, err := projections.ListProjectTransitionViews(ctx, store.DB())
	if err != nil {
		t.Fatalf("ListProjectTransitionViews() error = %v", err)
	}
	if len(transition) != 1 || transition[0].ProjectKey != project.Key {
		t.Fatalf("transition = %+v, want project %q", transition, project.Key)
	}
	if transition[0].OpenTaskCount != 0 {
		t.Fatalf("transition open task count = %d, want 0 for dead-lettered task", transition[0].OpenTaskCount)
	}

	portfolio, err := projections.ListProjectPortfolioViews(ctx, store.DB())
	if err != nil {
		t.Fatalf("ListProjectPortfolioViews() error = %v", err)
	}
	if len(portfolio) != 1 || portfolio[0].ProjectKey != project.Key {
		t.Fatalf("portfolio = %+v, want project %q", portfolio, project.Key)
	}
	if portfolio[0].OpenTaskCount != 0 {
		t.Fatalf("portfolio open task count = %d, want 0 for dead-lettered task", portfolio[0].OpenTaskCount)
	}
	if portfolio[0].ActiveRunCount != 0 {
		t.Fatalf("portfolio active run count = %d, want 0 for interrupted dead-letter recovery", portfolio[0].ActiveRunCount)
	}

	activeRuns, err := projections.ListActiveRunViews(ctx, store.DB())
	if err != nil {
		t.Fatalf("ListActiveRunViews() error = %v", err)
	}
	if len(activeRuns) != 0 {
		t.Fatalf("active runs = %+v, want 0 for interrupted dead-letter recovery", activeRuns)
	}
}

func TestWorkspaceMemoryProjectionsExposeScopedSummaries(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openObservabilityStore(t)
	defer store.Close()

	seedProjectionMemoryFixture(t, ctx, store)

	workspaceViews, err := projections.ListWorkspaceMemoryViews(ctx, store.DB(), projections.WorkspaceMemoryQuery{
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("ListWorkspaceMemoryViews() error = %v", err)
	}
	if len(workspaceViews) != 1 || workspaceViews[0].WorkspaceKey != "marcus" || workspaceViews[0].WorkspaceSummaryCount != 1 {
		t.Fatalf("workspace views = %+v, want marcus workspace summary count", workspaceViews)
	}

	initiativeViews, err := projections.ListInitiativeMemoryViews(ctx, store.DB(), projections.InitiativeMemoryQuery{
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("ListInitiativeMemoryViews() error = %v", err)
	}
	if len(initiativeViews) != 1 || initiativeViews[0].InitiativeKey != "alpha-initiative" || initiativeViews[0].SummaryCount != 1 {
		t.Fatalf("initiative views = %+v, want alpha-initiative summary count", initiativeViews)
	}
	if initiativeViews[0].LastSummary != "Alpha uses worktree isolation." {
		t.Fatalf("initiative last summary = %q, want project summary", initiativeViews[0].LastSummary)
	}

	companionViews, err := projections.ListCompanionMemoryViews(ctx, store.DB(), projections.CompanionMemoryQuery{
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("ListCompanionMemoryViews() error = %v", err)
	}
	if len(companionViews) != 1 || companionViews[0].CompanionKey != "strategist" || companionViews[0].SummaryCount != 1 {
		t.Fatalf("companion views = %+v, want strategist summary count", companionViews)
	}
	if companionViews[0].LastSummary != "Escalate policy-sensitive changes." {
		t.Fatalf("companion last summary = %q, want overlay summary", companionViews[0].LastSummary)
	}
}

func TestMemoryProjectionsHonorScopedQueries(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openObservabilityStore(t)
	defer store.Close()

	marcusWorkspaceID, secondaryWorkspaceID := seedProjectionScopedMemoryFixture(t, ctx, store)

	workspaceViews, err := projections.ListWorkspaceMemoryViews(ctx, store.DB(), projections.WorkspaceMemoryQuery{
		WorkspaceID: &marcusWorkspaceID,
		Limit:       1,
	})
	if err != nil {
		t.Fatalf("ListWorkspaceMemoryViews(filtered) error = %v", err)
	}
	if len(workspaceViews) != 1 || workspaceViews[0].WorkspaceID != marcusWorkspaceID {
		t.Fatalf("workspace views = %+v, want only marcus workspace", workspaceViews)
	}

	initiativeViews, err := projections.ListInitiativeMemoryViews(ctx, store.DB(), projections.InitiativeMemoryQuery{
		WorkspaceID: &marcusWorkspaceID,
		Limit:       1,
	})
	if err != nil {
		t.Fatalf("ListInitiativeMemoryViews(filtered) error = %v", err)
	}
	if len(initiativeViews) != 1 || initiativeViews[0].WorkspaceID != marcusWorkspaceID {
		t.Fatalf("initiative views = %+v, want only marcus initiative view", initiativeViews)
	}

	companionViews, err := projections.ListCompanionMemoryViews(ctx, store.DB(), projections.CompanionMemoryQuery{
		WorkspaceID: &secondaryWorkspaceID,
		Limit:       1,
	})
	if err != nil {
		t.Fatalf("ListCompanionMemoryViews(filtered) error = %v", err)
	}
	if len(companionViews) != 1 || companionViews[0].WorkspaceID != secondaryWorkspaceID {
		t.Fatalf("companion views = %+v, want only secondary companion view", companionViews)
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

func seedProjectionMemoryFixture(t *testing.T, ctx context.Context, store *sqlite.Store) {
	t.Helper()

	workspace, err := store.CreateWorkspace(ctx, sqlite.CreateWorkspaceParams{
		Key:        "marcus",
		Name:       "Marcus",
		OwnerRef:   "marcus",
		Status:     "active",
		PolicyJSON: "{}",
	})
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}
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
	initiative, err := store.CreateInitiative(ctx, sqlite.CreateInitiativeParams{
		WorkspaceID:     workspace.ID,
		Key:             "alpha-initiative",
		Title:           "Alpha Initiative",
		Kind:            "managed_project",
		Status:          "active",
		Summary:         "Alpha delivery",
		LinkedProjectID: &project.ID,
	})
	if err != nil {
		t.Fatalf("CreateInitiative() error = %v", err)
	}
	companion, err := store.CreateCompanion(ctx, sqlite.CreateCompanionParams{
		WorkspaceID:         workspace.ID,
		Key:                 "strategist",
		Title:               "Strategist",
		Kind:                "advisor",
		Charter:             "Guide strategic decisions",
		Status:              "active",
		InitiativeScopeJSON: "[]",
		MemoryPolicyJSON:    "{}",
		PlanningPolicyJSON:  "{}",
		ToolPolicyJSON:      "{}",
	})
	if err != nil {
		t.Fatalf("CreateCompanion() error = %v", err)
	}

	if _, err := store.RecordMemorySummary(ctx, sqlite.RecordMemorySummaryParams{
		WorkspaceID:     &workspace.ID,
		Scope:           "workspace",
		ScopeKey:        workspace.Key,
		VisibilityScope: "workspace",
		RetentionClass:  "durable",
		MemoryType:      "user_preference",
		Summary:         "Prefer concise replies.",
		DetailsJSON:     `{"source":"test"}`,
	}); err != nil {
		t.Fatalf("RecordMemorySummary(workspace) error = %v", err)
	}
	if _, err := store.RecordMemorySummary(ctx, sqlite.RecordMemorySummaryParams{
		WorkspaceID:     &workspace.ID,
		InitiativeID:    &initiative.ID,
		Scope:           "initiative",
		ScopeKey:        initiative.Key,
		VisibilityScope: "initiative",
		RetentionClass:  "durable",
		MemoryType:      "project_summary",
		Summary:         "Alpha uses worktree isolation.",
		DetailsJSON:     `{"source":"test"}`,
	}); err != nil {
		t.Fatalf("RecordMemorySummary(initiative) error = %v", err)
	}
	if _, err := store.RecordMemorySummary(ctx, sqlite.RecordMemorySummaryParams{
		WorkspaceID:     &workspace.ID,
		CompanionID:     &companion.ID,
		Scope:           "companion",
		ScopeKey:        companion.Key,
		VisibilityScope: "companion",
		RetentionClass:  "working",
		MemoryType:      "overlay_note",
		Summary:         "Escalate policy-sensitive changes.",
		DetailsJSON:     `{"source":"test"}`,
	}); err != nil {
		t.Fatalf("RecordMemorySummary(companion) error = %v", err)
	}
}

func seedProjectionScopedMemoryFixture(t *testing.T, ctx context.Context, store *sqlite.Store) (int64, int64) {
	t.Helper()

	seedProjectionMemoryFixture(t, ctx, store)

	workspace, err := store.CreateWorkspace(ctx, sqlite.CreateWorkspaceParams{
		Key:        "secondary",
		Name:       "Secondary",
		OwnerRef:   "secondary",
		Status:     "active",
		PolicyJSON: "{}",
	})
	if err != nil {
		t.Fatalf("CreateWorkspace(secondary) error = %v", err)
	}
	initiative, err := store.CreateInitiative(ctx, sqlite.CreateInitiativeParams{
		WorkspaceID: workspace.ID,
		Key:         "secondary-initiative",
		Title:       "Secondary Initiative",
		Kind:        "delivery",
		Status:      "active",
		Summary:     "Secondary delivery",
	})
	if err != nil {
		t.Fatalf("CreateInitiative(secondary) error = %v", err)
	}
	companion, err := store.CreateCompanion(ctx, sqlite.CreateCompanionParams{
		WorkspaceID:         workspace.ID,
		Key:                 "shadow",
		Title:               "Shadow",
		Kind:                "advisor",
		Charter:             "Secondary helper",
		Status:              "active",
		InitiativeScopeJSON: "[]",
		MemoryPolicyJSON:    "{}",
		PlanningPolicyJSON:  "{}",
		ToolPolicyJSON:      "{}",
	})
	if err != nil {
		t.Fatalf("CreateCompanion(secondary) error = %v", err)
	}
	if _, err := store.RecordMemorySummary(ctx, sqlite.RecordMemorySummaryParams{
		WorkspaceID:     &workspace.ID,
		Scope:           "workspace",
		ScopeKey:        workspace.Key,
		VisibilityScope: "workspace",
		RetentionClass:  "durable",
		MemoryType:      "user_preference",
		Summary:         "Secondary workspace memory.",
		DetailsJSON:     `{"source":"test"}`,
	}); err != nil {
		t.Fatalf("RecordMemorySummary(secondary workspace) error = %v", err)
	}
	if _, err := store.RecordMemorySummary(ctx, sqlite.RecordMemorySummaryParams{
		WorkspaceID:     &workspace.ID,
		InitiativeID:    &initiative.ID,
		Scope:           "initiative",
		ScopeKey:        initiative.Key,
		VisibilityScope: "initiative",
		RetentionClass:  "durable",
		MemoryType:      "project_summary",
		Summary:         "Secondary initiative memory.",
		DetailsJSON:     `{"source":"test"}`,
	}); err != nil {
		t.Fatalf("RecordMemorySummary(secondary initiative) error = %v", err)
	}
	if _, err := store.RecordMemorySummary(ctx, sqlite.RecordMemorySummaryParams{
		WorkspaceID:     &workspace.ID,
		CompanionID:     &companion.ID,
		Scope:           "companion",
		ScopeKey:        companion.Key,
		VisibilityScope: "companion",
		RetentionClass:  "working",
		MemoryType:      "overlay_note",
		Summary:         "Secondary companion memory.",
		DetailsJSON:     `{"source":"test"}`,
	}); err != nil {
		t.Fatalf("RecordMemorySummary(secondary companion) error = %v", err)
	}

	marcusWorkspace, err := store.GetWorkspaceByKey(ctx, "marcus")
	if err != nil {
		t.Fatalf("GetWorkspaceByKey(marcus) error = %v", err)
	}
	return marcusWorkspace.ID, workspace.ID
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
