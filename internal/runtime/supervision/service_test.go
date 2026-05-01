package supervision

import (
	"context"
	"database/sql"
	"testing"

	"odin-os/internal/store/sqlite"
)

func TestServiceStartRecordsEnabledControlState(t *testing.T) {
	ctx := context.Background()
	store := openServiceTestStore(t, "supervision-service-start.db")
	defer store.Close()

	service := NewService(store, DefaultConfig())
	report, err := service.Start(ctx, "operator")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if report.Control.Status != ControlStatusEnabled || report.Control.KillSwitchActive {
		t.Fatalf("Start().Control = %+v, want enabled without kill switch", report.Control)
	}
	if report.Control.MaxConcurrentTasks != 1 || report.Control.DryRun || !report.Control.RequireHumanApproval {
		t.Fatalf("Start().Control guarded defaults = %+v, want Stage 7 guarded defaults", report.Control)
	}
	assertSideEffectsNotStarted(t, report.SideEffects)

	got, err := store.GetSupervisionControl(ctx, ModeKeyStage7SupervisedAgency)
	if err != nil {
		t.Fatalf("GetSupervisionControl() error = %v", err)
	}
	if got.Status != ControlStatusEnabled || got.KillSwitchActive || got.UpdatedBy != "operator" {
		t.Fatalf("persisted control = %+v, want enabled by operator", got)
	}
}

func TestServiceStopRecordsKillSwitchAndStoppedState(t *testing.T) {
	ctx := context.Background()
	store := openServiceTestStore(t, "supervision-service-stop.db")
	defer store.Close()

	service := NewService(store, DefaultConfig())
	report, err := service.Stop(ctx, "operator")
	if err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	if report.Control.Status != ControlStatusStopped || !report.Control.KillSwitchActive {
		t.Fatalf("Stop().Control = %+v, want stopped with kill switch", report.Control)
	}
	assertSideEffectsNotStarted(t, report.SideEffects)

	got, err := store.GetSupervisionControl(ctx, ModeKeyStage7SupervisedAgency)
	if err != nil {
		t.Fatalf("GetSupervisionControl() error = %v", err)
	}
	if got.Status != ControlStatusStopped || !got.KillSwitchActive || got.UpdatedBy != "operator" {
		t.Fatalf("persisted control = %+v, want stopped by operator", got)
	}
}

func TestServiceQueueRecordsDecisionsAndPlannedClaimsWithoutCreatingTasksOrRuns(t *testing.T) {
	ctx := context.Background()
	store := openServiceTestStore(t, "supervision-service-queue.db")
	defer store.Close()
	project := createServiceProject(t, ctx, store)
	service := NewService(store, DefaultConfig())
	if _, err := service.Start(ctx, "operator"); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	report, err := service.Queue(ctx, Project{
		ID:   project.ID,
		Key:  project.Key,
		Repo: project.GitHubRepo,
	}, []Issue{
		{
			Repo:         project.GitHubRepo,
			Number:       101,
			Title:        "Update Stage 7 docs",
			Labels:       []string{"odin:ready", "safety:low-risk"},
			ChangedPaths: []string{"docs/stage7.md"},
		},
		{
			Repo:         project.GitHubRepo,
			Number:       102,
			Title:        "Change deployment workflow",
			Labels:       []string{"odin:ready", "safety:low-risk"},
			ChangedPaths: []string{".github/workflows/deploy-production.yml"},
		},
	})
	if err != nil {
		t.Fatalf("Queue() error = %v", err)
	}

	if len(report.Decisions) != 2 {
		t.Fatalf("Queue().Decisions len = %d, want 2", len(report.Decisions))
	}
	if !report.Decisions[0].Eligible || report.Decisions[0].ClaimKey == "" {
		t.Fatalf("eligible decision = %+v, want planned claim", report.Decisions[0])
	}
	if report.Decisions[1].Eligible || report.Decisions[1].RefusalReason != RefusalForbiddenPath {
		t.Fatalf("refused decision = %+v, want forbidden_path refusal", report.Decisions[1])
	}
	assertSideEffectsNotStarted(t, report.SideEffects)

	decisions, err := store.ListSupervisionQueueDecisions(ctx, sqlite.ListSupervisionQueueDecisionsParams{
		ProjectID: &project.ID,
		Repo:      project.GitHubRepo,
	})
	if err != nil {
		t.Fatalf("ListSupervisionQueueDecisions() error = %v", err)
	}
	if len(decisions) != 2 {
		t.Fatalf("persisted decisions len = %d, want 2", len(decisions))
	}
	claims, err := store.ListSupervisionDispatchClaims(ctx, sqlite.ListSupervisionDispatchClaimsParams{
		ProjectID: &project.ID,
		Repo:      project.GitHubRepo,
	})
	if err != nil {
		t.Fatalf("ListSupervisionDispatchClaims() error = %v", err)
	}
	if len(claims) != 1 || claims[0].Status != ClaimStatusReserved {
		t.Fatalf("claims = %+v, want one reserved planned claim", claims)
	}
	assertTableCount(t, store, "tasks", 0)
	assertTableCount(t, store, "runs", 0)
}

func TestServiceRecoverReportsCleanWithNoStaleClaims(t *testing.T) {
	ctx := context.Background()
	store := openServiceTestStore(t, "supervision-service-recover-clean.db")
	defer store.Close()
	service := NewService(store, DefaultConfig())
	if _, err := service.Start(ctx, "operator"); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	report, err := service.Recover(ctx)
	if err != nil {
		t.Fatalf("Recover() error = %v", err)
	}

	if report.Recovery.Status != RecoveryStatusClean || report.Recovery.Reason != RecoveryReasonNoStaleClaims {
		t.Fatalf("Recover().Recovery = %+v, want clean no stale claims", report.Recovery)
	}
	assertSideEffectsNotStarted(t, report.SideEffects)
}

func TestServiceRecoverBlocksWhenConfigHashChangedAgainstActiveClaims(t *testing.T) {
	ctx := context.Background()
	store := openServiceTestStore(t, "supervision-service-recover-blocked.db")
	defer store.Close()
	project := createServiceProject(t, ctx, store)
	service := NewService(store, DefaultConfig())
	if _, err := service.Start(ctx, "operator"); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	report, err := service.Queue(ctx, Project{
		ID:   project.ID,
		Key:  project.Key,
		Repo: project.GitHubRepo,
	}, []Issue{eligibleIssue("docs/stage7.md")})
	if err != nil {
		t.Fatalf("Queue() error = %v", err)
	}
	if len(report.Claims) != 1 {
		t.Fatalf("Queue().Claims len = %d, want 1", len(report.Claims))
	}

	changedConfig := DefaultConfig()
	changedConfig.AllowedPathPrefixes = append(changedConfig.AllowedPathPrefixes, "examples/")
	changedService := NewService(store, changedConfig)
	recovery, err := changedService.Recover(ctx)
	if err != nil {
		t.Fatalf("Recover() error = %v", err)
	}

	if recovery.Recovery.Status != RecoveryStatusBlocked || recovery.Recovery.Reason != RefusalRecoveryBlocked {
		t.Fatalf("Recover().Recovery = %+v, want blocked recovery", recovery.Recovery)
	}
	assertSideEffectsNotStarted(t, recovery.SideEffects)
}

func openServiceTestStore(t *testing.T, name string) *sqlite.Store {
	t.Helper()

	store, err := sqlite.Open(t.TempDir() + "/" + name)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		_ = store.Close()
		t.Fatalf("Migrate() error = %v", err)
	}
	return store
}

func createServiceProject(t *testing.T, ctx context.Context, store *sqlite.Store) sqlite.Project {
	t.Helper()

	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "odin-core",
		Name:          "Odin Core",
		Scope:         "project",
		GitRoot:       "/home/orchestrator/odin-os",
		DefaultBranch: "main",
		GitHubRepo:    "marcusgoll/odin-os",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	return project
}

func assertSideEffectsNotStarted(t *testing.T, sideEffects SideEffects) {
	t.Helper()

	if sideEffects.CodexExecution != SideEffectNotStarted ||
		sideEffects.PRs != SideEffectNotCreated ||
		sideEffects.Merge != SideEffectNotMerged ||
		sideEffects.Deployment != SideEffectNotStarted {
		t.Fatalf("side effects = %+v, want all worker/PR/merge/deploy actions not started", sideEffects)
	}
}

func assertTableCount(t *testing.T, store *sqlite.Store, table string, want int) {
	t.Helper()

	var got int
	if err := store.DB().QueryRow("SELECT COUNT(*) FROM " + table).Scan(&got); err != nil && err != sql.ErrNoRows {
		t.Fatalf("count %s error = %v", table, err)
	}
	if got != want {
		t.Fatalf("%s count = %d, want %d", table, got, want)
	}
}
