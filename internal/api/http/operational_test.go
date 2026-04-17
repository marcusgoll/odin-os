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
	"odin-os/internal/core/companions"
	"odin-os/internal/core/controlscope"
	"odin-os/internal/core/workitems"
	"odin-os/internal/core/workspaces"
	"odin-os/internal/runtime/checkpoints"
	healthsvc "odin-os/internal/runtime/health"
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
		RegistryHealthy: true,
	}))
	defer server.Close()

	assertReportStatus(t, server.URL+"/healthz", http.StatusOK, "degraded")
	assertReportStatus(t, server.URL+"/readyz", http.StatusServiceUnavailable, "degraded")
}

func TestOperationalHandlerExposesWorkspaceHome(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openStore(t)
	defer store.Close()

	workspaceKey, initiativeKey, companionKey, taskKey := seedWorkspaceHTTPState(t, ctx, store)

	server := httptest.NewServer(httpapi.NewOperationalHandler(httpapi.Dependencies{
		Store:  store,
		Health: healthsvc.Service{DB: store.DB()},
		Metrics: metricsvc.Service{
			DB: store.DB(),
		},
		RegistryHealthy: true,
	}))
	defer server.Close()

	response, err := http.Get(server.URL + "/workspace")
	if err != nil {
		t.Fatalf("GET /workspace error = %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("/workspace status = %d, want %d", response.StatusCode, http.StatusOK)
	}

	var payload struct {
		Workspace struct {
			Key                  string `json:"key"`
			InitiativeCount      int    `json:"initiative_count"`
			CompanionCount       int    `json:"companion_count"`
			PendingApprovalCount int    `json:"pending_approval_count"`
			BlockedItemCount     int    `json:"blocked_item_count"`
		} `json:"workspace"`
		Initiatives []struct {
			Key               string `json:"key"`
			OwnerCompanionKey string `json:"owner_companion_key"`
		} `json:"initiatives"`
		InitiativeWorkItems []struct {
			InitiativeKey string `json:"initiative_key"`
			TaskKey       string `json:"task_key"`
			Status        string `json:"status"`
		} `json:"initiative_work_items"`
		BlockedItems []struct {
			TaskKey      string `json:"task_key"`
			CompanionKey string `json:"companion_key"`
			NextStep     string `json:"next_step"`
		} `json:"blocked_items"`
		PendingApprovals []struct {
			ProjectKey string `json:"project_key"`
			TaskKey    string `json:"task_key"`
			Status     string `json:"status"`
		} `json:"pending_approvals"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode(/workspace) error = %v", err)
	}

	if payload.Workspace.Key != workspaceKey || payload.Workspace.InitiativeCount != 1 || payload.Workspace.CompanionCount != 1 || payload.Workspace.PendingApprovalCount != 1 || payload.Workspace.BlockedItemCount != 1 {
		t.Fatalf("/workspace payload = %+v, want workspace counts for %s", payload.Workspace, workspaceKey)
	}
	if len(payload.Initiatives) != 1 || payload.Initiatives[0].Key != initiativeKey || payload.Initiatives[0].OwnerCompanionKey != companionKey {
		t.Fatalf("/workspace initiatives = %+v, want initiative %s owned by %s", payload.Initiatives, initiativeKey, companionKey)
	}
	if len(payload.InitiativeWorkItems) != 1 || payload.InitiativeWorkItems[0].InitiativeKey != initiativeKey || payload.InitiativeWorkItems[0].TaskKey != taskKey || payload.InitiativeWorkItems[0].Status != "blocked" {
		t.Fatalf("/workspace initiative work items = %+v, want blocked work item for %s", payload.InitiativeWorkItems, initiativeKey)
	}
	if len(payload.BlockedItems) != 1 || payload.BlockedItems[0].TaskKey != taskKey || payload.BlockedItems[0].CompanionKey != companionKey || payload.BlockedItems[0].NextStep != "resume once approved" {
		t.Fatalf("/workspace blocked items = %+v, want blocked item for %s", payload.BlockedItems, taskKey)
	}
	if len(payload.PendingApprovals) != 1 || payload.PendingApprovals[0].ProjectKey != "odin-core" || payload.PendingApprovals[0].TaskKey != taskKey || payload.PendingApprovals[0].Status != "pending" {
		t.Fatalf("/workspace approvals = %+v, want pending approval for %s", payload.PendingApprovals, taskKey)
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

func seedWorkspaceHTTPState(t *testing.T, ctx context.Context, store *sqlite.Store) (workspaceKey string, initiativeKey string, companionKey string, taskKey string) {
	t.Helper()

	bootstrapped, err := workspaces.Service{Store: store}.BootstrapDefault(ctx)
	if err != nil {
		t.Fatalf("BootstrapDefault() error = %v", err)
	}
	workspace, err := store.GetWorkspaceByKey(ctx, bootstrapped.Key)
	if err != nil {
		t.Fatalf("GetWorkspaceByKey(%s) error = %v", bootstrapped.Key, err)
	}
	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "odin-core",
		Name:          "Odin Core",
		Scope:         "odin-core",
		GitRoot:       filepath.Join(t.TempDir(), "odin-core"),
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject(odin-core) error = %v", err)
	}
	companion, err := store.CreateCompanion(ctx, sqlite.CreateCompanionParams{
		WorkspaceID:         workspace.ID,
		Key:                 "operator",
		Title:               "Operator",
		Kind:                companions.KindOperator,
		Charter:             "Run the workspace rhythm.",
		Status:              companions.StatusActive,
		InitiativeScopeJSON: `{"mode":"all"}`,
		ToolPolicyJSON:      `{"mode":"deny","allowed":[]}`,
		MemoryPolicyJSON:    `{"retention":"workspace"}`,
		PlanningPolicyJSON:  `{"mode":"stepwise"}`,
	})
	if err != nil {
		t.Fatalf("CreateCompanion() error = %v", err)
	}
	initiative, err := store.GetInitiativeByProjectID(ctx, project.ID)
	if err != nil {
		t.Fatalf("GetInitiativeByProjectID() error = %v", err)
	}
	if err := store.AssignInitiativeCompanion(ctx, initiative.ID, companion.ID); err != nil {
		t.Fatalf("AssignInitiativeCompanion() error = %v", err)
	}
	initiative, err = store.GetInitiative(ctx, initiative.ID)
	if err != nil {
		t.Fatalf("GetInitiative() error = %v", err)
	}
	workItem, err := workitems.Service{Store: store}.Create(ctx, controlscope.Service{}.ResolveInitiative(workspace.Key, initiative.Key), "Follow up on approvals")
	if err != nil {
		t.Fatalf("Create(work item) error = %v", err)
	}
	task, err := store.GetTask(ctx, workItem.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	run, err := store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:   task.ID,
		Executor: "codex_headless",
		Attempt:  1,
		Status:   "running",
	})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	if _, err := store.UpdateTaskStatus(ctx, sqlite.UpdateTaskStatusParams{
		TaskID: task.ID,
		Status: "blocked",
	}); err != nil {
		t.Fatalf("UpdateTaskStatus(blocked) error = %v", err)
	}
	if _, err := store.RequestApproval(ctx, sqlite.RequestApprovalParams{
		TaskID:      task.ID,
		RunID:       &run.ID,
		Status:      "pending",
		RequestedBy: "system",
	}); err != nil {
		t.Fatalf("RequestApproval() error = %v", err)
	}
	if _, err := (checkpoints.Service{Store: store}).Compact(ctx, checkpoints.CompactParams{
		TaskID:          task.ID,
		RunID:           &run.ID,
		Trigger:         checkpoints.TriggerApprovalWait,
		CheckpointKey:   "workspace-home",
		Objective:       task.Title,
		TaskStatus:      "blocked",
		BlockingReason:  "awaiting operator approval",
		NextSteps:       []string{"resume once approved"},
		ManifestSummary: "workspace task",
		PolicySummary:   "approval required",
		OpenTaskSummary: "one blocked task",
		ApprovalSummary: "one pending approval",
	}); err != nil {
		t.Fatalf("Compact() error = %v", err)
	}

	return workspace.Key, initiative.Key, companion.Key, task.Key
}
