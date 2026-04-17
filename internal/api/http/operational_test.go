package httpapi_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	httpapi "odin-os/internal/api/http"
	"odin-os/internal/core/initiatives"
	healthsvc "odin-os/internal/runtime/health"
	"odin-os/internal/runtime/projections"
	"odin-os/internal/store/sqlite"
	metricsvc "odin-os/internal/telemetry/metrics"
)

func TestOperationalHandlerExposesHealthReadyAndMetrics(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openStore(t)
	defer store.Close()

	seedHealthyObservability(t, ctx, store)

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

func TestOperationalHandlerDegradesReadyzWhenRuntimeIsNotReady(t *testing.T) {
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
	decodeJSON(t, server.URL+"/workspace", &workspace)
	if workspace.WorkspaceKey != "default" {
		t.Fatalf("/workspace workspace_key = %q, want default", workspace.WorkspaceKey)
	}
	if workspace.ActiveInitiativeCount != 1 {
		t.Fatalf("/workspace active_initiative_count = %d, want 1", workspace.ActiveInitiativeCount)
	}

	var initiatives []projections.InitiativePortfolioView
	decodeJSON(t, server.URL+"/initiatives", &initiatives)
	if len(initiatives) != 1 || initiatives[0].InitiativeKey != "alpha" {
		t.Fatalf("/initiatives = %+v, want alpha initiative", initiatives)
	}

	var companions []projections.CompanionAssignmentView
	decodeJSON(t, server.URL+"/companions", &companions)
	if len(companions) != 1 || companions[0].CompanionKey != "primary" {
		t.Fatalf("/companions = %+v, want primary companion", companions)
	}

	var blocked []projections.BlockedItemView
	decodeJSON(t, server.URL+"/blocked", &blocked)
	if len(blocked) != 1 || blocked[0].InitiativeKey == nil || *blocked[0].InitiativeKey != "alpha" {
		t.Fatalf("/blocked = %+v, want blocked item for alpha", blocked)
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

func decodeJSON(t *testing.T, url string, target any) {
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
