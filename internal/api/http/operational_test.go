package httpapi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	httpapi "odin-os/internal/api/http"
	coremedia "odin-os/internal/core/media"
	"odin-os/internal/core/initiatives"
	"odin-os/internal/core/workspaces"
	healthsvc "odin-os/internal/runtime/health"
	"odin-os/internal/runtime/projections"
	"odin-os/internal/store/sqlite"
	metricsvc "odin-os/internal/telemetry/metrics"
)

func TestReadyzReturnsHealthyWhenRuntimeIsReady(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openStore(t)
	defer store.Close()

	seedHealthyObservability(t, ctx, store)
	seedRuntimeState(t, ctx, store, "ready")

	server := httptest.NewServer(httpapi.NewOperationalHandler(httpapi.Dependencies{
		Health: healthsvc.Service{DB: store.DB()},
		Metrics: metricsvc.Service{
			DB: store.DB(),
		},
		ReadModels:      store.DB(),
		RegistryHealthy: true,
	}))
	defer server.Close()

	assertReportStatus(t, server.URL+"/healthz", http.StatusOK, "healthy")
	assertReportStatus(t, server.URL+"/readyz", http.StatusOK, "healthy")

	response, err := http.Get(server.URL + "/metrics")
	if err != nil {
		t.Fatalf("GET /metrics error = %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("/metrics status = %d, want %d", response.StatusCode, http.StatusOK)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("ReadAll(/metrics) error = %v", err)
	}
	if !strings.Contains(string(body), "odin_active_runs") {
		t.Fatalf("/metrics body = %q, want odin_active_runs metric", string(body))
	}
}

func TestReadyzFailsClosedWhenRuntimeIsNotReady(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openStore(t)
	defer store.Close()

	seedHealthyObservability(t, ctx, store)
	seedRuntimeState(t, ctx, store, "booting")

	server := httptest.NewServer(httpapi.NewOperationalHandler(httpapi.Dependencies{
		Health: healthsvc.Service{DB: store.DB()},
		Metrics: metricsvc.Service{
			DB: store.DB(),
		},
		RegistryHealthy: true,
	}))
	defer server.Close()

	assertReportStatus(t, server.URL+"/healthz", http.StatusOK, "healthy")
	assertReportStatus(t, server.URL+"/readyz", http.StatusServiceUnavailable, "healthy")
}

func TestReadyzFailsClosedWhenRuntimeHeartbeatIsStale(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	ctx := context.Background()
	store := openStore(t)
	defer store.Close()

	seedHealthyObservability(t, ctx, store)
	seedRuntimeStateWithHeartbeat(t, ctx, store, "ready", now.Add(-2*time.Hour))

	server := httptest.NewServer(httpapi.NewOperationalHandler(httpapi.Dependencies{
		Health: healthsvc.Service{
			DB: store.DB(),
			Config: healthsvc.Config{
				RuntimeHeartbeatTTL: 1 * time.Minute,
			},
			Now: func() time.Time { return now },
		},
		Metrics: metricsvc.Service{
			DB: store.DB(),
		},
		RegistryHealthy: true,
	}))
	defer server.Close()

	assertReportStatus(t, server.URL+"/healthz", http.StatusOK, "healthy")
	assertReportStatus(t, server.URL+"/readyz", http.StatusServiceUnavailable, "healthy")
}

func TestReadyzIsUnavailableWhenDoctorIsDegraded(t *testing.T) {
	t.Parallel()

	store := openStore(t)
	defer store.Close()

	server := httptest.NewServer(httpapi.NewOperationalHandler(httpapi.Dependencies{
		Health: healthsvc.Service{DB: store.DB()},
		Metrics: metricsvc.Service{
			DB: store.DB(),
		},
		ReadModels:      store.DB(),
		RegistryHealthy: true,
	}))
	defer server.Close()

	assertReportStatus(t, server.URL+"/healthz", http.StatusOK, "degraded")
	assertReportStatus(t, server.URL+"/readyz", http.StatusServiceUnavailable, "degraded")
}

func TestReadyzFailsClosedWhenMediaProfileFailsClosed(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openStore(t)
	defer store.Close()

	seedHealthyObservability(t, ctx, store)
	seedRuntimeState(t, ctx, store, "ready")

	server := httptest.NewServer(httpapi.NewOperationalHandler(httpapi.Dependencies{
		Health: healthsvc.Service{
			DB: store.DB(),
			Media: &healthsvc.MediaChecks{
				Config:       healthMediaConfig(),
				ProbeCommand: fixtureMediaProbePath(t, "media-probe-mount-mismatch.sh"),
			},
		},
		Metrics: metricsvc.Service{
			DB: store.DB(),
		},
		RegistryHealthy: true,
	}))
	defer server.Close()

	assertReportStatus(t, server.URL+"/healthz", http.StatusOK, "failed")
	assertReportStatus(t, server.URL+"/readyz", http.StatusServiceUnavailable, "failed")
}

func TestReadyzFailsClosedWhenMediaProbeCommandFails(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openStore(t)
	defer store.Close()

	seedHealthyObservability(t, ctx, store)
	seedRuntimeState(t, ctx, store, "ready")

	server := httptest.NewServer(httpapi.NewOperationalHandler(httpapi.Dependencies{
		Health: healthsvc.Service{
			DB: store.DB(),
			Media: &healthsvc.MediaChecks{
				Config:       healthMediaConfig(),
				ProbeCommand: "/definitely/missing/media-probe-command",
			},
		},
		Metrics: metricsvc.Service{
			DB: store.DB(),
		},
		RegistryHealthy: true,
	}))
	defer server.Close()

	assertReportStatus(t, server.URL+"/healthz", http.StatusOK, "failed")
	assertReportStatus(t, server.URL+"/readyz", http.StatusServiceUnavailable, "failed")
}

func TestOperationalHandlerExposesWorkspaceInitiativeCompanionAndBlockedReadModels(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openStore(t)
	defer store.Close()

	seedHealthyObservability(t, ctx, store)
	seedOperatorReadModels(t, ctx, store)

	server := httptest.NewServer(httpapi.NewOperationalHandler(httpapi.Dependencies{
		Health: healthsvc.Service{DB: store.DB()},
		Metrics: metricsvc.Service{
			DB: store.DB(),
		},
		ReadModels:      store.DB(),
		RegistryHealthy: true,
	}))
	defer server.Close()

	var workspace projections.WorkspaceOverviewView
	decodeURLJSON(t, server.URL+"/workspace", &workspace)
	if workspace.WorkspaceKey != "default" {
		t.Fatalf("/workspace workspace_key = %q, want default", workspace.WorkspaceKey)
	}
	if workspace.ActiveInitiativeCount != 1 {
		t.Fatalf("/workspace active_initiative_count = %d, want 1", workspace.ActiveInitiativeCount)
	}

	var initiatives []projections.InitiativePortfolioView
	decodeURLJSON(t, server.URL+"/initiatives", &initiatives)
	if len(initiatives) != 1 || initiatives[0].InitiativeKey != "alpha" {
		t.Fatalf("/initiatives = %+v, want alpha initiative", initiatives)
	}

	var companions []projections.CompanionAssignmentView
	decodeURLJSON(t, server.URL+"/companions", &companions)
	if len(companions) != 1 || companions[0].CompanionKey != "primary" {
		t.Fatalf("/companions = %+v, want primary companion", companions)
	}

	var blocked []projections.BlockedItemView
	decodeURLJSON(t, server.URL+"/blocked", &blocked)
	if len(blocked) != 1 || blocked[0].InitiativeKey == nil || *blocked[0].InitiativeKey != "alpha" {
		t.Fatalf("/blocked = %+v, want blocked item for alpha", blocked)
	}
}

func TestOperationalHandlerExposesAgendaReadModel(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, 4, 17, 9, 0, 0, 0, time.UTC)
	store := openStore(t)
	defer store.Close()

	seedHealthyObservability(t, ctx, store)
	seedOperatorReadModels(t, ctx, store)
	seedAgendaReadModels(t, ctx, store, now)

	server := httptest.NewServer(httpapi.NewOperationalHandler(httpapi.Dependencies{
		Health: healthsvc.Service{DB: store.DB()},
		Metrics: metricsvc.Service{
			DB: store.DB(),
		},
		ReadModels:      store.DB(),
		RegistryHealthy: true,
		Now: func() time.Time {
			return now
		},
	}))
	defer server.Close()

	var agenda projections.AgendaView
	decodeURLJSON(t, server.URL+"/agenda", &agenda)
	if agenda.WorkspaceKey != "default" {
		t.Fatalf("/agenda workspace_key = %q, want default", agenda.WorkspaceKey)
	}
	if len(agenda.DueWork) != 2 {
		t.Fatalf("/agenda due_work = %+v, want 2 entries", agenda.DueWork)
	}
	if len(agenda.BlockedWork) < 2 {
		t.Fatalf("/agenda blocked_work = %+v, want at least 2 entries", agenda.BlockedWork)
	}
	if len(agenda.Approvals) != 1 || agenda.Approvals[0].TaskKey != "alpha-task" {
		t.Fatalf("/agenda approvals = %+v, want alpha-task approval", agenda.Approvals)
	}
}

func TestOperationalHandlerExposesDefaultWorkspaceMemoryReadModels(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openStore(t)
	defer store.Close()

	seedHealthyObservability(t, ctx, store)
	seedMemoryReadModelState(t, ctx, store)

	server := httptest.NewServer(httpapi.NewOperationalHandler(httpapi.Dependencies{
		Health: healthsvc.Service{DB: store.DB()},
		Metrics: metricsvc.Service{
			DB: store.DB(),
		},
		ReadModels:      store.DB(),
		RegistryHealthy: true,
	}))
	defer server.Close()

	var report struct {
		Workspace []struct {
			WorkspaceKey         string `json:"workspace_key"`
			WorkspaceEntryCount  int    `json:"workspace_entry_count"`
			InitiativeEntryCount int    `json:"initiative_entry_count"`
			CompanionEntryCount  int    `json:"companion_entry_count"`
		} `json:"workspace"`
		Initiatives []struct {
			InitiativeKey string `json:"initiative_key"`
			EntryCount    int    `json:"entry_count"`
			LastSummary   string `json:"last_summary"`
		} `json:"initiatives"`
		Companions []struct {
			CompanionKey string `json:"companion_key"`
			EntryCount   int    `json:"entry_count"`
			LastSummary  string `json:"last_summary"`
		} `json:"companions"`
	}
	decodeURLJSON(t, server.URL+"/memoryz", &report)

	if len(report.Workspace) != 1 || report.Workspace[0].WorkspaceKey != workspaces.DefaultWorkspaceKey {
		t.Fatalf("/memoryz workspace = %+v, want only default workspace", report.Workspace)
	}
	if report.Workspace[0].WorkspaceEntryCount != 1 || report.Workspace[0].InitiativeEntryCount != 1 || report.Workspace[0].CompanionEntryCount != 1 {
		t.Fatalf("/memoryz workspace counts = %+v, want one entry per scope", report.Workspace[0])
	}
	if len(report.Initiatives) != 1 || report.Initiatives[0].InitiativeKey != "alpha" || report.Initiatives[0].EntryCount != 1 {
		t.Fatalf("/memoryz initiatives = %+v, want alpha initiative memory", report.Initiatives)
	}
	if report.Initiatives[0].LastSummary != "Alpha memory summary" {
		t.Fatalf("/memoryz initiative last summary = %q, want alpha memory summary", report.Initiatives[0].LastSummary)
	}
	if len(report.Companions) != 1 || report.Companions[0].CompanionKey != workspaces.DefaultWorkspaceCompanionKey || report.Companions[0].EntryCount != 1 {
		t.Fatalf("/memoryz companions = %+v, want primary companion memory", report.Companions)
	}
	if report.Companions[0].LastSummary != "Primary companion memory" {
		t.Fatalf("/memoryz companion last summary = %q, want primary companion memory", report.Companions[0].LastSummary)
	}
}

func openStore(t *testing.T) *sqlite.Store {
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

func seedHealthyObservability(t *testing.T, ctx context.Context, store *sqlite.Store) {
	t.Helper()

	if _, err := store.RecordRegistryVersion(ctx, sqlite.RecordRegistryVersionParams{
		Source:      "registry",
		VersionHash: "phase-15",
		Notes:       "healthy test sample",
	}); err != nil {
		t.Fatalf("RecordRegistryVersion() error = %v", err)
	}
	if _, err := store.RecordExecutorHealth(ctx, sqlite.RecordExecutorHealthParams{
		Executor:    "codex_headless",
		Status:      "healthy",
		LatencyMS:   10,
		DetailsJSON: `{"status":"healthy"}`,
	}); err != nil {
		t.Fatalf("RecordExecutorHealth() error = %v", err)
	}
	if _, err := store.RecordProjectionFreshness(ctx, sqlite.RecordProjectionFreshnessParams{
		Surface:     "active_runs",
		Status:      "current",
		DetailsJSON: `{"source":"test"}`,
	}); err != nil {
		t.Fatalf("RecordProjectionFreshness() error = %v", err)
	}
}

func seedRuntimeState(t *testing.T, ctx context.Context, store *sqlite.Store, status string) {
	t.Helper()

	seedRuntimeStateWithHeartbeat(t, ctx, store, status, time.Now().UTC())
}

func seedRuntimeStateWithHeartbeat(t *testing.T, ctx context.Context, store *sqlite.Store, status string, heartbeatAt time.Time) {
	t.Helper()

	readyAt := (*time.Time)(nil)
	if status == "ready" {
		readyValue := heartbeatAt
		readyAt = &readyValue
	}
	if _, err := store.UpsertRuntimeState(ctx, sqlite.UpsertRuntimeStateParams{
		BootID:             "boot-test",
		Status:             status,
		PID:                1234,
		StartedAt:          heartbeatAt,
		ReadyAt:            readyAt,
		LastHeartbeatAt:    heartbeatAt,
		LastShutdownReason: "",
		LastError:          "",
		UpdatedAt:          heartbeatAt,
	}, sqlite.RuntimeStateWriteOptions{}); err != nil {
		t.Fatalf("UpsertRuntimeState() error = %v", err)
	}
}

func assertReportStatus(t *testing.T, url string, wantCode int, wantStatus string) {
	t.Helper()

	response, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s error = %v", url, err)
	}
	defer response.Body.Close()
	if response.StatusCode != wantCode {
		t.Fatalf("%s status = %d, want %d", url, response.StatusCode, wantCode)
	}
	var report struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(response.Body).Decode(&report); err != nil {
		t.Fatalf("Decode(%s) error = %v", url, err)
	}
	if report.Status != wantStatus {
		t.Fatalf("%s report status = %q, want %q", url, report.Status, wantStatus)
	}
}

func decodeURLJSON(t *testing.T, url string, target any) {
	t.Helper()

	response, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s error = %v", url, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("%s status = %d, want %d", url, response.StatusCode, http.StatusOK)
	}
	if err := json.NewDecoder(response.Body).Decode(target); err != nil {
		t.Fatalf("Decode(%s) error = %v", url, err)
	}
}

func seedOperatorReadModels(t *testing.T, ctx context.Context, store *sqlite.Store) {
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

	workspace, err := store.GetWorkspaceByKey(ctx, "default")
	if err != nil {
		t.Fatalf("GetWorkspaceByKey(default) error = %v", err)
	}
	companion, err := store.GetCompanionByKey(ctx, workspace.ID, workspace.DefaultCompanionKey)
	if err != nil {
		t.Fatalf("GetCompanionByKey(default) error = %v", err)
	}
	initiative, err := store.UpsertInitiative(ctx, sqlite.UpsertInitiativeParams{
		WorkspaceID:      workspace.ID,
		Key:              project.Key,
		Title:            project.Name,
		Kind:             string(initiatives.KindManagedProject),
		Status:           "active",
		Summary:          "Alpha initiative",
		OwnerCompanionID: &companion.ID,
		LinkedProjectID:  &project.ID,
	})
	if err != nil {
		t.Fatalf("UpsertInitiative() error = %v", err)
	}

	task, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:    project.ID,
		Key:          "alpha-task",
		Title:        "Alpha task",
		Status:       "blocked",
		Scope:        "project",
		RequestedBy:  "operator",
		WorkspaceID:  &workspace.ID,
		InitiativeID: &initiative.ID,
		CompanionID:  &companion.ID,
		WorkKind:     "automation",
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

	if _, err := store.RequestApproval(ctx, sqlite.RequestApprovalParams{
		TaskID:      task.ID,
		RunID:       &run.ID,
		Status:      "pending",
		RequestedBy: "system",
	}); err != nil {
		t.Fatalf("RequestApproval() error = %v", err)
	}
}

func seedAgendaReadModels(t *testing.T, ctx context.Context, store *sqlite.Store, now time.Time) {
	t.Helper()

	project, err := store.GetProjectByKey(ctx, "alpha")
	if err != nil {
		t.Fatalf("GetProjectByKey(alpha) error = %v", err)
	}
	workspace, err := store.GetWorkspaceByKey(ctx, "default")
	if err != nil {
		t.Fatalf("GetWorkspaceByKey(default) error = %v", err)
	}
	companion, err := store.GetCompanionByKey(ctx, workspace.ID, workspace.DefaultCompanionKey)
	if err != nil {
		t.Fatalf("GetCompanionByKey(default) error = %v", err)
	}
	initiative, err := store.GetInitiativeByKey(ctx, workspace.ID, "alpha")
	if err != nil {
		t.Fatalf("GetInitiativeByKey(alpha) error = %v", err)
	}

	createAgendaFollowUpObligation(t, ctx, store, project.ID, workspace.ID, initiative.ID, companion.ID, "Review mail", now)
	createAgendaFollowUpObligation(t, ctx, store, project.ID, workspace.ID, initiative.ID, companion.ID, "File taxes", now.Add(-48*time.Hour))

	wakeTask, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:    project.ID,
		Key:          "alpha-wake",
		Title:        "Resume wake packet",
		Status:       "blocked",
		Scope:        "project",
		RequestedBy:  "operator",
		WorkspaceID:  &workspace.ID,
		InitiativeID: &initiative.ID,
		CompanionID:  &companion.ID,
		WorkKind:     "follow_up",
	})
	if err != nil {
		t.Fatalf("CreateTask(alpha-wake) error = %v", err)
	}
	if _, err := store.CreateContextPacket(ctx, sqlite.CreateContextPacketParams{
		TaskID:        &wakeTask.ID,
		PacketKind:    "wake",
		PacketScope:   "task_wake_packet",
		Trigger:       "follow_up_wait",
		CheckpointKey: "agenda-wake",
		Status:        "active",
		Summary:       "waiting on follow-up context",
		PayloadJSON:   fmt.Sprintf(`{"task_id":%d,"task_key":"%s","scope":"project","objective":"Resume wake work","status":"waiting","trigger":"follow_up_wait","blocking_reason":"waiting on supporting context"}`, wakeTask.ID, wakeTask.Key),
	}); err != nil {
		t.Fatalf("CreateContextPacket() error = %v", err)
	}
}

func createAgendaFollowUpObligation(t *testing.T, ctx context.Context, store *sqlite.Store, projectID, workspaceID, initiativeID, companionID int64, title string, nextDueAt time.Time) {
	t.Helper()

	if _, err := store.CreateFollowUpObligation(ctx, sqlite.CreateFollowUpObligationParams{
		WorkspaceID:     workspaceID,
		InitiativeID:    &initiativeID,
		CompanionID:     &companionID,
		TargetProjectID: projectID,
		Title:           title,
		Status:          "active",
		CadenceJSON:     `{"mode":"once"}`,
		NextDueAt:       nextDueAt,
		PolicyJSON:      `{}`,
	}); err != nil {
		t.Fatalf("CreateFollowUpObligation(%s) error = %v", title, err)
	}
}

func seedMemoryReadModelState(t *testing.T, ctx context.Context, store *sqlite.Store) {
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

	workspace, err := store.GetWorkspaceByKey(ctx, workspaces.DefaultWorkspaceKey)
	if err != nil {
		t.Fatalf("GetWorkspaceByKey(default) error = %v", err)
	}
	companion, err := store.GetCompanionByKey(ctx, workspace.ID, workspace.DefaultCompanionKey)
	if err != nil {
		t.Fatalf("GetCompanionByKey(default) error = %v", err)
	}
	initiative, err := store.UpsertInitiative(ctx, sqlite.UpsertInitiativeParams{
		WorkspaceID:      workspace.ID,
		Key:              project.Key,
		Title:            project.Name,
		Kind:             string(initiatives.KindManagedProject),
		Status:           "active",
		Summary:          "Alpha initiative",
		OwnerCompanionID: &companion.ID,
		LinkedProjectID:  &project.ID,
	})
	if err != nil {
		t.Fatalf("UpsertInitiative() error = %v", err)
	}
	if _, err := store.CreateMemoryEntry(ctx, sqlite.CreateMemoryEntryParams{
		WorkspaceID:     workspace.ID,
		EntryType:       "note",
		VisibilityScope: "workspace",
		RetentionClass:  "durable",
		Summary:         "Workspace memory summary",
		Content:         "Workspace memory content",
	}); err != nil {
		t.Fatalf("CreateMemoryEntry(workspace) error = %v", err)
	}
	if _, err := store.CreateMemoryEntry(ctx, sqlite.CreateMemoryEntryParams{
		WorkspaceID:     workspace.ID,
		InitiativeID:    &initiative.ID,
		EntryType:       "summary",
		VisibilityScope: "initiative",
		RetentionClass:  "durable",
		Summary:         "Alpha memory summary",
		Content:         "Alpha initiative memory content",
	}); err != nil {
		t.Fatalf("CreateMemoryEntry(initiative) error = %v", err)
	}
	if _, err := store.CreateMemoryEntry(ctx, sqlite.CreateMemoryEntryParams{
		WorkspaceID:     workspace.ID,
		CompanionID:     &companion.ID,
		EntryType:       "note",
		VisibilityScope: "companion",
		RetentionClass:  "working",
		Summary:         "Primary companion memory",
		Content:         "Primary companion memory content",
	}); err != nil {
		t.Fatalf("CreateMemoryEntry(companion) error = %v", err)
	}

	secondary, err := store.CreateWorkspace(ctx, sqlite.CreateWorkspaceParams{
		Key:                 "secondary",
		Name:                "Secondary Workspace",
		OwnerRef:            "secondary",
		DefaultCompanionKey: "secondary-primary",
		Status:              "active",
		PolicyJSON:          "{}",
	})
	if err != nil {
		t.Fatalf("CreateWorkspace(secondary) error = %v", err)
	}
	if err := store.EnsureDefaultCompanion(ctx, secondary.ID, secondary.DefaultCompanionKey); err != nil {
		t.Fatalf("EnsureDefaultCompanion(secondary) error = %v", err)
	}
	if _, err := store.CreateMemoryEntry(ctx, sqlite.CreateMemoryEntryParams{
		WorkspaceID:     secondary.ID,
		EntryType:       "note",
		VisibilityScope: "workspace",
		RetentionClass:  "durable",
		Summary:         "Secondary workspace memory",
		Content:         "Secondary workspace memory content",
	}); err != nil {
		t.Fatalf("CreateMemoryEntry(secondary workspace) error = %v", err)
	}
}

func healthMediaConfig() *coremedia.Config {
	return &coremedia.Config{
		Enabled: true,
		Services: []coremedia.StackService{
			{
				Name: "plex",
				Kind: coremedia.ServiceKindPlex,
			},
		},
	}
}

func fixtureMediaProbePath(t *testing.T, name string) string {
	t.Helper()

	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("runtime.Caller() failed")
	}
	return filepath.Clean(path.Join(filepath.Dir(currentFile), "..", "..", "..", "scripts", "tests", "fixtures", name))
}
