package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	httpapi "odin-os/internal/api/http"
	clioverview "odin-os/internal/cli/overview"
	"odin-os/internal/cli/scope"
	"odin-os/internal/core/initiatives"
	coremedia "odin-os/internal/core/media"
	coreworkspace "odin-os/internal/core/workspace"
	"odin-os/internal/core/workspaces"
	"odin-os/internal/registry"
	runtimeevents "odin-os/internal/runtime/events"
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

	assertReportStatus(t, server.URL+"/health", http.StatusOK, "healthy")
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
	if !strings.Contains(string(body), "odin_os_health_score") {
		t.Fatalf("/metrics body = %q, want odin_os_health_score metric", string(body))
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

func TestOperationalHandlerExposesDashboardStatusWithoutSecretsOrTmuxDependency(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openStore(t)
	defer store.Close()

	seedHealthyObservability(t, ctx, store)
	seedRuntimeState(t, ctx, store, "ready")
	seedOperatorReadModels(t, ctx, store)
	project, err := store.GetProjectByKey(ctx, "alpha")
	if err != nil {
		t.Fatalf("GetProjectByKey(alpha) error = %v", err)
	}
	if _, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:             project.ID,
		Key:                   "explicit-intent-task",
		Title:                 "Explicit intent task",
		Status:                "queued",
		Scope:                 "project",
		RequestedBy:           "test",
		ExecutionIntent:       "deliver_with_evidence",
		ExecutionIntentSource: "test",
	}); err != nil {
		t.Fatalf("CreateTask(explicit intent) error = %v", err)
	}
	if _, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "failed-task",
		Title:       "Failed task",
		Status:      "failed",
		Scope:       "project",
		RequestedBy: "test",
	}); err != nil {
		t.Fatalf("CreateTask(failed) error = %v", err)
	}

	const adminToken = "ghp_dashboard_secret_token"
	server := httptest.NewServer(httpapi.NewOperationalHandler(httpapi.Dependencies{
		Health: healthsvc.Service{DB: store.DB()},
		Metrics: metricsvc.Service{
			DB: store.DB(),
		},
		ReadModels:      store.DB(),
		RegistryHealthy: true,
		AdminToken:      adminToken,
		RegistrySnapshot: registry.Snapshot{Items: []registry.Item{{
			Kind: registry.KindWorkflow,
			Key:  "delivery-profile-fixture",
			Tags: []string{"delivery_profile"},
		}}},
	}))
	defer server.Close()

	response, err := http.Get(server.URL + "/status")
	if err != nil {
		t.Fatalf("GET /status error = %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("/status status = %d, want %d", response.StatusCode, http.StatusOK)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("ReadAll(/status) error = %v", err)
	}
	if strings.Contains(string(body), adminToken) {
		t.Fatalf("/status leaked admin token in body: %s", string(body))
	}

	var status struct {
		HealthStatus string `json:"health_status"`
		Ready        bool   `json:"ready"`
		Runtime      struct {
			Status string `json:"status"`
		} `json:"runtime"`
		WorkerDispatch struct {
			Mode     string `json:"mode"`
			Enabled  bool   `json:"enabled"`
			DryRun   bool   `json:"dry_run"`
			ReadOnly bool   `json:"read_only"`
			Source   string `json:"source"`
			Reason   string `json:"reason"`
		} `json:"worker_dispatch"`
		Counts struct {
			WorkItems               int `json:"work_items"`
			ActiveRunAttempts       int `json:"active_run_attempts"`
			PendingApprovals        int `json:"pending_approvals"`
			ReviewQueueItems        int `json:"review_queue_items"`
			BlockedWorkItems        int `json:"blocked_work_items"`
			FailedWorkItems         int `json:"failed_work_items"`
			RecoveryRecommendations int `json:"recovery_recommendations"`
			DeliveryProfiles        int `json:"delivery_profiles"`
			ExplicitIntentWorkItems int `json:"explicit_intent_work_items"`
			FallbackIntentWorkItems int `json:"fallback_intent_work_items"`
			ActionRequiredItems     int `json:"action_required_items"`
		} `json:"counts"`
		Tmux struct {
			Available bool   `json:"available"`
			Source    string `json:"source"`
		} `json:"tmux"`
	}
	if err := json.Unmarshal(body, &status); err != nil {
		t.Fatalf("Unmarshal(/status) error = %v", err)
	}
	if status.HealthStatus != "healthy" || !status.Ready || status.Runtime.Status != "ready" {
		t.Fatalf("/status = %+v, want healthy ready runtime", status)
	}
	if status.WorkerDispatch.Mode != "live" || !status.WorkerDispatch.Enabled || status.WorkerDispatch.DryRun || status.WorkerDispatch.ReadOnly || status.WorkerDispatch.Source != "runtime_readiness" || status.WorkerDispatch.Reason != "" {
		t.Fatalf("/status worker_dispatch = %+v, want live non-dry-run non-read-only runtime readiness status", status.WorkerDispatch)
	}
	if status.Counts.WorkItems == 0 || status.Counts.ActiveRunAttempts == 0 || status.Counts.PendingApprovals == 0 {
		t.Fatalf("/status counts = %+v, want runtime-state-backed counts", status.Counts)
	}
	if status.Counts.ReviewQueueItems < 2 || status.Counts.BlockedWorkItems != 1 || status.Counts.FailedWorkItems != 1 || status.Counts.RecoveryRecommendations != 1 {
		t.Fatalf("/status counts = %+v, want action-required counts", status.Counts)
	}
	if status.Counts.DeliveryProfiles != 1 || status.Counts.ExplicitIntentWorkItems != 1 || status.Counts.FallbackIntentWorkItems == 0 || status.Counts.ActionRequiredItems < status.Counts.ReviewQueueItems {
		t.Fatalf("/status counts = %+v, want delivery/intent/action rollups", status.Counts)
	}
	if status.Tmux.Available || status.Tmux.Source != "not_configured" {
		t.Fatalf("/status tmux = %+v, want absence reported without daemon failure", status.Tmux)
	}
}

func TestOperationalHandlerIncludesTmuxProviderStatus(t *testing.T) {
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
		Tmux: fakeTmuxStatusProvider{status: httpapi.TmuxStatus{
			Available:        true,
			Source:           "workspace_sessions",
			LiveSessions:     1,
			AttachedSessions: 1,
			Sessions: []httpapi.TmuxWorkspaceSession{
				{
					ProjectKey:    "alpha",
					SessionName:   "odin-workspace-alpha",
					State:         "live",
					FactsSource:   "live",
					AttachedCount: 1,
				},
			},
		}},
	}))
	defer server.Close()

	response, err := http.Get(server.URL + "/status")
	if err != nil {
		t.Fatalf("GET /status error = %v", err)
	}
	defer response.Body.Close()

	var status struct {
		Tmux struct {
			Available        bool   `json:"available"`
			Source           string `json:"source"`
			LiveSessions     int    `json:"live_sessions"`
			AttachedSessions int    `json:"attached_sessions"`
			Sessions         []struct {
				ProjectKey    string `json:"project_key"`
				SessionName   string `json:"session_name"`
				State         string `json:"state"`
				FactsSource   string `json:"facts_source"`
				AttachedCount int    `json:"attached_count"`
			} `json:"sessions"`
		} `json:"tmux"`
	}
	if err := json.NewDecoder(response.Body).Decode(&status); err != nil {
		t.Fatalf("decode /status: %v", err)
	}
	if !status.Tmux.Available || status.Tmux.Source != "workspace_sessions" || status.Tmux.LiveSessions != 1 || status.Tmux.AttachedSessions != 1 {
		t.Fatalf("/status tmux = %+v, want provider summary", status.Tmux)
	}
	if len(status.Tmux.Sessions) != 1 || status.Tmux.Sessions[0].SessionName != "odin-workspace-alpha" {
		t.Fatalf("/status tmux sessions = %+v, want workspace session", status.Tmux.Sessions)
	}
}

func TestOperationalHandlerReportsTmuxProviderErrorWithoutFailingStatus(t *testing.T) {
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
		Tmux:            fakeTmuxStatusProvider{err: errors.New("tmux unavailable")},
	}))
	defer server.Close()

	response, err := http.Get(server.URL + "/status")
	if err != nil {
		t.Fatalf("GET /status error = %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("/status status = %d, want %d", response.StatusCode, http.StatusOK)
	}

	var status struct {
		Tmux struct {
			Available bool   `json:"available"`
			Source    string `json:"source"`
			Error     string `json:"error"`
		} `json:"tmux"`
	}
	if err := json.NewDecoder(response.Body).Decode(&status); err != nil {
		t.Fatalf("decode /status: %v", err)
	}
	if status.Tmux.Available || status.Tmux.Source != "tmux" || status.Tmux.Error != "tmux unavailable" {
		t.Fatalf("/status tmux = %+v, want non-fatal provider error", status.Tmux)
	}
}

func TestOperationalHandlerReportsWorkspaceTmuxAbsentWithoutFailingStatus(t *testing.T) {
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
		Tmux: httpapi.WorkspaceTmuxStatusProvider{
			Workspaces: fakeWorkspaceStatusLister{err: errors.New(`exec: "tmux": executable file not found in $PATH`)},
		},
	}))
	defer server.Close()

	response, err := http.Get(server.URL + "/status")
	if err != nil {
		t.Fatalf("GET /status error = %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("/status status = %d, want %d", response.StatusCode, http.StatusOK)
	}

	var status struct {
		Tmux struct {
			Available bool   `json:"available"`
			Source    string `json:"source"`
			Error     string `json:"error"`
		} `json:"tmux"`
	}
	if err := json.NewDecoder(response.Body).Decode(&status); err != nil {
		t.Fatalf("decode /status: %v", err)
	}
	if status.Tmux.Available || status.Tmux.Source != "tmux" || !strings.Contains(status.Tmux.Error, "executable file not found") {
		t.Fatalf("/status tmux = %+v, want non-fatal tmux absence", status.Tmux)
	}
}

func TestWorkspaceTmuxStatusProviderSummarizesWorkspaceSessions(t *testing.T) {
	t.Parallel()

	provider := httpapi.WorkspaceTmuxStatusProvider{
		Workspaces: fakeWorkspaceStatusLister{statuses: []coreworkspace.Status{
			{
				ProjectKey:    "alpha",
				SessionName:   "odin-workspace-alpha",
				State:         coreworkspace.StateLive,
				FactsSource:   coreworkspace.FactsSourceLive,
				AttachedCount: 2,
			},
			{
				ProjectKey:  "beta",
				SessionName: "odin-workspace-beta",
				State:       coreworkspace.StateStopped,
				FactsSource: coreworkspace.FactsSourceLastKnown,
			},
		}},
	}

	status, err := provider.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if !status.Available || status.Source != "workspace_sessions" || status.LiveSessions != 1 || status.AttachedSessions != 1 {
		t.Fatalf("Status() = %+v, want one live attached workspace session", status)
	}
	if len(status.Sessions) != 1 || status.Sessions[0].ProjectKey != "alpha" || status.Sessions[0].AttachedCount != 2 {
		t.Fatalf("Status().Sessions = %+v, want alpha live session", status.Sessions)
	}
}

type fakeTmuxStatusProvider struct {
	status httpapi.TmuxStatus
	err    error
}

func (provider fakeTmuxStatusProvider) Status(context.Context) (httpapi.TmuxStatus, error) {
	if provider.err != nil {
		return httpapi.TmuxStatus{}, provider.err
	}
	return provider.status, nil
}

type fakeWorkspaceStatusLister struct {
	statuses []coreworkspace.Status
	err      error
}

func (lister fakeWorkspaceStatusLister) List(context.Context) ([]coreworkspace.Status, error) {
	if lister.err != nil {
		return nil, lister.err
	}
	return lister.statuses, nil
}

func TestOperationalHandlerExposesIssuesAndRunsFromRuntimeState(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openStore(t)
	defer store.Close()

	seedHealthyObservability(t, ctx, store)
	seedRuntimeState(t, ctx, store, "ready")
	seedOperatorReadModels(t, ctx, store)
	seedExternalIssue(t, ctx, store)

	server := httptest.NewServer(httpapi.NewOperationalHandler(httpapi.Dependencies{
		Health: healthsvc.Service{DB: store.DB()},
		Metrics: metricsvc.Service{
			DB: store.DB(),
		},
		ReadModels:      store.DB(),
		RegistryHealthy: true,
	}))
	defer server.Close()

	var issues []struct {
		Provider   string   `json:"provider"`
		Repo       string   `json:"repo"`
		Number     int      `json:"number"`
		Title      string   `json:"title"`
		Labels     []string `json:"labels"`
		SyncStatus string   `json:"sync_status"`
	}
	decodeURLJSON(t, server.URL+"/issues", &issues)
	if len(issues) != 1 || issues[0].Repo != "owner/alpha" || issues[0].Number != 42 || issues[0].Labels[0] != "odin:ready" {
		t.Fatalf("/issues = %+v, want persisted external issue", issues)
	}

	var runs []projections.RunSummaryView
	decodeURLJSON(t, server.URL+"/runs", &runs)
	if len(runs) != 1 || runs[0].Status != "running" {
		t.Fatalf("/runs = %+v, want one running run", runs)
	}

	var runDetail struct {
		ID       int64  `json:"id"`
		TaskID   int64  `json:"task_id"`
		Executor string `json:"executor"`
		Status   string `json:"status"`
		Attempt  int    `json:"attempt"`
	}
	decodeURLJSON(t, fmt.Sprintf("%s/runs/%d", server.URL, runs[0].RunID), &runDetail)
	if runDetail.ID != runs[0].RunID || runDetail.Executor != "codex" || runDetail.Status != "running" {
		t.Fatalf("/runs/{id} = %+v, want codex running run", runDetail)
	}
}

func TestOperationalHandlerPaginatesAndFiltersDashboardIssues(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openStore(t)
	defer store.Close()

	seedHealthyObservability(t, ctx, store)
	seedOperatorReadModels(t, ctx, store)
	for number := 1; number <= 105; number++ {
		repo := "owner/alpha"
		if number%2 == 0 {
			repo = "owner/beta"
		}
		syncStatus := "eligible"
		if number%3 == 0 {
			syncStatus = "paused"
		}
		seedDashboardIssue(t, ctx, store, repo, number, "github", "open", syncStatus)
	}

	server := httptest.NewServer(httpapi.NewOperationalHandler(httpapi.Dependencies{
		Health:          healthsvc.Service{DB: store.DB()},
		ReadModels:      store.DB(),
		RegistryHealthy: true,
	}))
	defer server.Close()

	var defaultIssues []struct {
		Number int `json:"number"`
	}
	decodeURLJSON(t, server.URL+"/issues", &defaultIssues)
	if len(defaultIssues) != 100 {
		t.Fatalf("default /issues count = %d, want bounded 100", len(defaultIssues))
	}

	var filtered []struct {
		Repo       string `json:"repo"`
		Number     int    `json:"number"`
		SyncStatus string `json:"sync_status"`
	}
	decodeURLJSON(t, server.URL+"/issues?repo=owner%2Falpha&sync_status=eligible&limit=2&offset=1", &filtered)
	if len(filtered) != 2 {
		t.Fatalf("filtered /issues count = %d, want 2: %+v", len(filtered), filtered)
	}
	if filtered[0].Number != 5 || filtered[1].Number != 7 {
		t.Fatalf("filtered /issues = %+v, want owner/alpha eligible numbers 5 and 7", filtered)
	}
	for _, issue := range filtered {
		if issue.Repo != "owner/alpha" || issue.SyncStatus != "eligible" {
			t.Fatalf("filtered /issues includes wrong row: %+v", issue)
		}
	}
}

func TestOperationalHandlerRejectsInvalidDashboardIssuePagination(t *testing.T) {
	t.Parallel()

	store := openStore(t)
	defer store.Close()

	server := httptest.NewServer(httpapi.NewOperationalHandler(httpapi.Dependencies{
		Health:          healthsvc.Service{DB: store.DB()},
		ReadModels:      store.DB(),
		RegistryHealthy: true,
	}))
	defer server.Close()

	assertGETStatus(t, server.URL+"/issues?limit=0", http.StatusBadRequest)
	assertGETStatus(t, server.URL+"/issues?offset=-1", http.StatusBadRequest)
	assertGETStatus(t, server.URL+"/issues?limit=501", http.StatusBadRequest)
}

func TestOperationalHandlerPaginatesAndFiltersDashboardRuns(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openStore(t)
	defer store.Close()

	seedHealthyObservability(t, ctx, store)
	seedOperatorReadModels(t, ctx, store)
	project := mustProject(t, ctx, store, "alpha")
	for number := 1; number <= 105; number++ {
		task := seedDashboardTask(t, ctx, store, project.ID, fmt.Sprintf("run-filter-task-%03d", number))
		executor := "codex"
		if number%2 == 0 {
			executor = "gemini"
		}
		run, err := store.StartRun(ctx, sqlite.StartRunParams{
			TaskID:   task.ID,
			Executor: executor,
			Attempt:  1,
			Status:   "running",
		})
		if err != nil {
			t.Fatalf("StartRun(%d) error = %v", number, err)
		}
		if number%2 == 0 {
			if _, err := store.FinishRun(ctx, sqlite.FinishRunParams{
				RunID:          run.ID,
				Status:         "completed",
				Summary:        "done",
				TerminalReason: "completed",
			}); err != nil {
				t.Fatalf("FinishRun(%d) error = %v", number, err)
			}
		}
	}

	server := httptest.NewServer(httpapi.NewOperationalHandler(httpapi.Dependencies{
		Health:          healthsvc.Service{DB: store.DB()},
		ReadModels:      store.DB(),
		RegistryHealthy: true,
	}))
	defer server.Close()

	var defaultRuns []projections.RunSummaryView
	decodeURLJSON(t, server.URL+"/runs", &defaultRuns)
	if len(defaultRuns) != 100 {
		t.Fatalf("default /runs count = %d, want bounded 100", len(defaultRuns))
	}

	var filtered []projections.RunSummaryView
	decodeURLJSON(t, server.URL+"/runs?status=completed&executor=gemini&limit=2&offset=1", &filtered)
	if len(filtered) != 2 {
		t.Fatalf("filtered /runs count = %d, want 2: %+v", len(filtered), filtered)
	}
	if filtered[0].TaskKey != "run-filter-task-004" || filtered[1].TaskKey != "run-filter-task-006" {
		t.Fatalf("filtered /runs = %+v, want tasks 004 and 006", filtered)
	}
	for _, run := range filtered {
		if run.Status != "completed" || run.Executor != "gemini" {
			t.Fatalf("filtered /runs includes wrong row: %+v", run)
		}
	}

	taskID := filtered[0].TaskID
	var taskFiltered []projections.RunSummaryView
	decodeURLJSON(t, fmt.Sprintf("%s/runs?task_id=%d", server.URL, taskID), &taskFiltered)
	if len(taskFiltered) != 1 || taskFiltered[0].TaskID != taskID {
		t.Fatalf("task_id filtered /runs = %+v, want task_id %d", taskFiltered, taskID)
	}
}

func TestOperationalHandlerRejectsInvalidDashboardRunPagination(t *testing.T) {
	t.Parallel()

	store := openStore(t)
	defer store.Close()

	server := httptest.NewServer(httpapi.NewOperationalHandler(httpapi.Dependencies{
		Health:          healthsvc.Service{DB: store.DB()},
		ReadModels:      store.DB(),
		RegistryHealthy: true,
	}))
	defer server.Close()

	assertGETStatus(t, server.URL+"/runs?limit=0", http.StatusBadRequest)
	assertGETStatus(t, server.URL+"/runs?offset=-1", http.StatusBadRequest)
	assertGETStatus(t, server.URL+"/runs?task_id=abc", http.StatusBadRequest)
}

func TestOperationalHandlerProtectsAdminActions(t *testing.T) {
	t.Parallel()

	admin := &recordingAdminActions{}
	server := httptest.NewServer(httpapi.NewOperationalHandler(httpapi.Dependencies{
		Health:          healthsvc.Service{},
		RegistryHealthy: true,
		AdminToken:      "dashboard-secret",
		Admin:           admin,
	}))
	defer server.Close()

	res := mustPost(t, server.URL+"/kill-switch/on", "")
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("POST /kill-switch/on without token status = %d, want %d", res.StatusCode, http.StatusUnauthorized)
	}
	res.Body.Close()

	res = mustPost(t, server.URL+"/kill-switch/on", "wrong-token")
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("POST /kill-switch/on wrong token status = %d, want %d", res.StatusCode, http.StatusForbidden)
	}
	res.Body.Close()

	res = mustPost(t, server.URL+"/kill-switch/on", "dashboard-secret")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("POST /kill-switch/on status = %d, want %d", res.StatusCode, http.StatusOK)
	}
	res.Body.Close()
	if admin.killSwitchOnCalls != 1 {
		t.Fatalf("KillSwitchOn calls = %d, want 1", admin.killSwitchOnCalls)
	}

	res = mustPost(t, server.URL+"/issues/not-a-number/pause", "dashboard-secret")
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("POST /issues/not-a-number/pause status = %d, want %d", res.StatusCode, http.StatusBadRequest)
	}
	res.Body.Close()
	if admin.pauseIssueID != 0 {
		t.Fatalf("PauseIssue called for invalid issue id %d, want no call", admin.pauseIssueID)
	}

	res = mustPost(t, server.URL+"/issues/42/pause", "")
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("POST /issues/42/pause without token status = %d, want %d", res.StatusCode, http.StatusUnauthorized)
	}
	res.Body.Close()
	if admin.pauseIssueID != 0 {
		t.Fatalf("PauseIssue called without token for issue id %d, want no call", admin.pauseIssueID)
	}

	res = mustPost(t, server.URL+"/issues/42/pause", "dashboard-secret")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("POST /issues/42/pause status = %d, want %d", res.StatusCode, http.StatusOK)
	}
	res.Body.Close()
	if admin.pauseIssueID != 42 {
		t.Fatalf("PauseIssue issue id = %d, want 42", admin.pauseIssueID)
	}

	res = mustPost(t, server.URL+"/issues/42/resume", "dashboard-secret")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("POST /issues/42/resume status = %d, want %d", res.StatusCode, http.StatusOK)
	}
	res.Body.Close()
	if admin.resumeIssueID != 42 {
		t.Fatalf("ResumeIssue issue id = %d, want 42", admin.resumeIssueID)
	}
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
	if len(agenda.CompanionSwarms) != 3 {
		t.Fatalf("/agenda companion swarms = %+v, want 3 entries", agenda.CompanionSwarms)
	}
	var hasApprovalBlocked, hasBudgetBlocked bool
	for _, swarm := range agenda.CompanionSwarms {
		switch swarm.BlockedReason {
		case "approval_required":
			hasApprovalBlocked = true
		case "budget_exhausted":
			hasBudgetBlocked = true
		}
	}
	if !hasApprovalBlocked {
		t.Fatalf("/agenda companion swarms = %+v, want approval-blocked swarm", agenda.CompanionSwarms)
	}
	if !hasBudgetBlocked {
		t.Fatalf("/agenda companion swarms = %+v, want budget-blocked swarm", agenda.CompanionSwarms)
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

func TestOperationalHandlerBrowserSessionHandoffShowIsReadOnly(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	defer store.Close()
	now := time.Date(2026, 5, 6, 20, 0, 0, 0, time.UTC)
	store.Now = func() time.Time { return now }
	store.BrowserSessionHandoffID = func() (string, error) { return "opaque-http-handoff", nil }

	session, err := store.CreateBrowserSession(ctx, sqlite.CreateBrowserSessionParams{
		Name:           "google-main",
		Domain:         "google.com",
		AccountHint:    "marcus",
		PermissionTier: sqlite.BrowserSessionPermissionTierAuthenticatedReadOnly,
	})
	if err != nil {
		t.Fatalf("CreateBrowserSession() error = %v", err)
	}
	request, err := store.CreateBrowserSessionLoginRequest(ctx, sqlite.CreateBrowserSessionLoginRequestParams{
		SessionID: session.ID,
		ExpiresAt: now.Add(10 * time.Minute),
	})
	if err != nil {
		t.Fatalf("CreateBrowserSessionLoginRequest() error = %v", err)
	}
	eventsBefore := countBrowserSessionEvents(t, ctx, store)

	server := httptest.NewServer(httpapi.NewOperationalHandler(httpapi.Dependencies{
		Store: store,
	}))
	defer server.Close()

	response, err := http.Get(server.URL + "/browser/session/handoff?handoff_id=" + request.HandoffID)
	if err != nil {
		t.Fatalf("GET handoff error = %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("handoff status = %d body=%s, want %d", response.StatusCode, string(body), http.StatusOK)
	}
	if contentType := response.Header.Get("Content-Type"); !strings.Contains(contentType, "application/json") {
		t.Fatalf("handoff Content-Type = %q, want application/json", contentType)
	}
	var payload struct {
		Handoff struct {
			HandoffID      string `json:"handoff_id"`
			LoginRequestID int64  `json:"login_request_id"`
			SessionID      int64  `json:"session_id"`
			SessionName    string `json:"session_name"`
			Domain         string `json:"domain"`
			AccountHint    string `json:"account_hint"`
			ExpiresAt      string `json:"expires_at"`
			Status         string `json:"status"`
			AllowedActions string `json:"allowed_actions"`
		} `json:"handoff"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode handoff response error = %v", err)
	}
	if payload.Handoff.HandoffID != request.HandoffID || payload.Handoff.LoginRequestID != request.ID || payload.Handoff.SessionID != session.ID {
		t.Fatalf("handoff payload = %+v, want linked handoff/request/session", payload.Handoff)
	}
	if payload.Handoff.SessionName != "google-main" || payload.Handoff.Domain != "google.com" || payload.Handoff.AccountHint != "marcus" || payload.Handoff.Status != "requested" || payload.Handoff.AllowedActions != "manual_login_only" || payload.Handoff.ExpiresAt == "" {
		t.Fatalf("handoff payload = %+v, want safe manual login metadata", payload.Handoff)
	}
	if eventsAfter := countBrowserSessionEvents(t, ctx, store); eventsAfter != eventsBefore {
		t.Fatalf("browser session event count changed from %d to %d on read-only handoff lookup", eventsBefore, eventsAfter)
	}
}

func TestOperationalHandlerBrowserSessionHandoffReturnsStaticEscapedHTML(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	defer store.Close()
	now := time.Date(2026, 5, 6, 20, 0, 0, 0, time.UTC)
	store.Now = func() time.Time { return now }
	store.BrowserSessionHandoffID = func() (string, error) { return "opaque-html-handoff", nil }

	session, err := store.CreateBrowserSession(ctx, sqlite.CreateBrowserSessionParams{
		Name:           `google <script>alert(1)</script>`,
		Domain:         "google.com",
		AccountHint:    `marcus"><input name=password>`,
		PermissionTier: sqlite.BrowserSessionPermissionTierAuthenticatedReadOnly,
	})
	if err != nil {
		t.Fatalf("CreateBrowserSession() error = %v", err)
	}
	request, err := store.CreateBrowserSessionLoginRequest(ctx, sqlite.CreateBrowserSessionLoginRequestParams{
		SessionID: session.ID,
		ExpiresAt: now.Add(10 * time.Minute),
	})
	if err != nil {
		t.Fatalf("CreateBrowserSessionLoginRequest() error = %v", err)
	}

	server := httptest.NewServer(httpapi.NewOperationalHandler(httpapi.Dependencies{
		Store: store,
	}))
	defer server.Close()

	httpRequest, err := http.NewRequest(http.MethodGet, server.URL+"/browser/session/handoff?handoff_id="+url.QueryEscape(request.HandoffID), nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	httpRequest.Header.Set("Accept", "text/html")
	response, err := http.DefaultClient.Do(httpRequest)
	if err != nil {
		t.Fatalf("GET handoff html error = %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("handoff html status = %d body=%s, want %d", response.StatusCode, string(body), http.StatusOK)
	}
	if contentType := response.Header.Get("Content-Type"); !strings.Contains(contentType, "text/html") {
		t.Fatalf("handoff html Content-Type = %q, want text/html", contentType)
	}
	bodyBytes, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("ReadAll(handoff html) error = %v", err)
	}
	body := string(bodyBytes)
	for _, want := range []string{
		"Browser Login Handoff",
		"google &lt;script&gt;alert(1)&lt;/script&gt;",
		"google.com",
		"manual_login_only",
		"No browser session is launched yet.",
		"Odin is not collecting credentials.",
		"Login and 2FA will be manual in a future handoff step.",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("handoff html body missing %q:\n%s", want, body)
		}
	}
	for _, forbidden := range []string{
		`google <script>alert(1)</script>`,
		`<script`,
		`<form`,
		`<input`,
		`type="password"`,
		`<textarea`,
	} {
		if strings.Contains(strings.ToLower(body), strings.ToLower(forbidden)) {
			t.Fatalf("handoff html body contains forbidden %q:\n%s", forbidden, body)
		}
	}

	formatResponse, err := http.Get(server.URL + "/browser/session/handoff?format=html&handoff_id=" + url.QueryEscape(request.HandoffID))
	if err != nil {
		t.Fatalf("GET handoff format html error = %v", err)
	}
	defer formatResponse.Body.Close()
	if formatResponse.StatusCode != http.StatusOK || !strings.Contains(formatResponse.Header.Get("Content-Type"), "text/html") {
		body, _ := io.ReadAll(formatResponse.Body)
		t.Fatalf("format=html status/content-type/body = %d/%q/%s, want HTML 200", formatResponse.StatusCode, formatResponse.Header.Get("Content-Type"), string(body))
	}
}

func TestOperationalHandlerBrowserSessionHandoffRejectsInvalidStates(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	defer store.Close()
	now := time.Date(2026, 5, 6, 20, 0, 0, 0, time.UTC)
	store.Now = func() time.Time { return now }

	session, err := store.CreateBrowserSession(ctx, sqlite.CreateBrowserSessionParams{
		Name:           "google-main",
		Domain:         "google.com",
		PermissionTier: sqlite.BrowserSessionPermissionTierAuthenticatedReadOnly,
	})
	if err != nil {
		t.Fatalf("CreateBrowserSession() error = %v", err)
	}
	handoffIDs := []string{"completed-http-handoff", "expired-http-handoff", "revoked-http-handoff"}
	store.BrowserSessionHandoffID = func() (string, error) {
		next := handoffIDs[0]
		handoffIDs = handoffIDs[1:]
		return next, nil
	}
	completed, err := store.CreateBrowserSessionLoginRequest(ctx, sqlite.CreateBrowserSessionLoginRequestParams{
		SessionID: session.ID,
		ExpiresAt: now.Add(10 * time.Minute),
	})
	if err != nil {
		t.Fatalf("CreateBrowserSessionLoginRequest(completed) error = %v", err)
	}
	if _, err := store.CompleteBrowserSessionLoginRequest(ctx, sqlite.CompleteBrowserSessionLoginRequestParams{RequestID: completed.ID}); err != nil {
		t.Fatalf("CompleteBrowserSessionLoginRequest() error = %v", err)
	}
	expired, err := store.CreateBrowserSessionLoginRequest(ctx, sqlite.CreateBrowserSessionLoginRequestParams{
		SessionID: session.ID,
		ExpiresAt: now.Add(10 * time.Minute),
	})
	if err != nil {
		t.Fatalf("CreateBrowserSessionLoginRequest(expired) error = %v", err)
	}
	revokedSession, err := store.CreateBrowserSession(ctx, sqlite.CreateBrowserSessionParams{
		Name:           "github-main",
		Domain:         "github.com",
		PermissionTier: sqlite.BrowserSessionPermissionTierAuthenticatedReadOnly,
	})
	if err != nil {
		t.Fatalf("CreateBrowserSession(revoked) error = %v", err)
	}
	revokedRequest, err := store.CreateBrowserSessionLoginRequest(ctx, sqlite.CreateBrowserSessionLoginRequestParams{
		SessionID: revokedSession.ID,
		ExpiresAt: now.Add(10 * time.Minute),
	})
	if err != nil {
		t.Fatalf("CreateBrowserSessionLoginRequest(revoked) error = %v", err)
	}
	if _, err := store.RevokeBrowserSession(ctx, sqlite.RevokeBrowserSessionParams{
		SessionID: revokedSession.ID,
		Actor:     "operator",
		Reason:    "test revocation",
	}); err != nil {
		t.Fatalf("RevokeBrowserSession() error = %v", err)
	}

	server := httptest.NewServer(httpapi.NewOperationalHandler(httpapi.Dependencies{
		Store: store,
	}))
	defer server.Close()

	assertHandoffStatus(t, server.URL+"/browser/session/handoff", http.StatusBadRequest)
	assertHandoffStatus(t, server.URL+"/browser/session/handoff?handoff_id=missing-http-handoff", http.StatusNotFound)
	assertHandoffStatus(t, server.URL+"/browser/session/handoff?handoff_id="+completed.HandoffID, http.StatusConflict)
	assertHandoffStatus(t, server.URL+"/browser/session/handoff?format=html&handoff_id="+completed.HandoffID, http.StatusConflict)
	store.Now = func() time.Time { return now.Add(11 * time.Minute) }
	assertHandoffStatus(t, server.URL+"/browser/session/handoff?handoff_id="+expired.HandoffID, http.StatusGone)
	store.Now = func() time.Time { return now }
	assertHandoffStatus(t, server.URL+"/browser/session/handoff?handoff_id="+revokedRequest.HandoffID, http.StatusConflict)
	assertHandoffStatus(t, server.URL+"/browser/session/handoff?format=html&handoff_id="+revokedRequest.HandoffID, http.StatusConflict)
}

func TestOperationalHandlerBrowserSessionHandoffCompleteJSONVerifiesSessionAndRequest(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	defer store.Close()
	now := time.Date(2026, 5, 6, 20, 0, 0, 0, time.UTC)
	store.Now = func() time.Time { return now }
	store.BrowserSessionHandoffID = func() (string, error) { return "complete-json-handoff", nil }

	session, err := store.CreateBrowserSession(ctx, sqlite.CreateBrowserSessionParams{
		Name:           "google-main",
		Domain:         "google.com",
		AccountHint:    "marcus",
		PermissionTier: sqlite.BrowserSessionPermissionTierAuthenticatedReadOnly,
	})
	if err != nil {
		t.Fatalf("CreateBrowserSession() error = %v", err)
	}
	loginRequest, err := store.CreateBrowserSessionLoginRequest(ctx, sqlite.CreateBrowserSessionLoginRequestParams{
		SessionID: session.ID,
		ExpiresAt: now.Add(10 * time.Minute),
	})
	if err != nil {
		t.Fatalf("CreateBrowserSessionLoginRequest() error = %v", err)
	}
	store.Now = func() time.Time { return now.Add(2 * time.Minute) }

	server := httptest.NewServer(httpapi.NewOperationalHandler(httpapi.Dependencies{
		Store:      store,
		AdminToken: "secret",
	}))
	defer server.Close()

	body := bytes.NewBufferString(`{"handoff_id":"` + loginRequest.HandoffID + `"}`)
	request, err := http.NewRequest(http.MethodPost, server.URL+"/browser/session/handoff/complete", body)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer secret")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("POST handoff complete error = %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("handoff complete status = %d body=%s, want %d", response.StatusCode, string(body), http.StatusOK)
	}
	if contentType := response.Header.Get("Content-Type"); !strings.Contains(contentType, "application/json") {
		t.Fatalf("handoff complete Content-Type = %q, want application/json", contentType)
	}
	var payload struct {
		Completion struct {
			HandoffID          string `json:"handoff_id"`
			SessionID          int64  `json:"session_id"`
			LoginRequestID     int64  `json:"login_request_id"`
			SessionStatus      string `json:"session_status"`
			LoginRequestStatus string `json:"login_request_status"`
			AllowedActions     string `json:"allowed_actions"`
		} `json:"completion"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode handoff complete response error = %v", err)
	}
	if payload.Completion.HandoffID != loginRequest.HandoffID || payload.Completion.SessionID != session.ID || payload.Completion.LoginRequestID != loginRequest.ID {
		t.Fatalf("completion payload = %+v, want linked handoff/session/request", payload.Completion)
	}
	if payload.Completion.SessionStatus != string(sqlite.BrowserSessionStatusVerified) || payload.Completion.LoginRequestStatus != string(sqlite.BrowserSessionLoginRequestStatusCompleted) || payload.Completion.AllowedActions != "manual_login_only" {
		t.Fatalf("completion payload = %+v, want verified/completed metadata only response", payload.Completion)
	}

	persistedSession, err := store.GetBrowserSession(ctx, session.ID)
	if err != nil {
		t.Fatalf("GetBrowserSession() error = %v", err)
	}
	if persistedSession.Status != sqlite.BrowserSessionStatusVerified || persistedSession.LastVerifiedAt == nil {
		t.Fatalf("persistedSession = %+v, want verified with last verified time", persistedSession)
	}
	persistedRequest, err := store.GetBrowserSessionLoginRequest(ctx, loginRequest.ID)
	if err != nil {
		t.Fatalf("GetBrowserSessionLoginRequest() error = %v", err)
	}
	if persistedRequest.Status != sqlite.BrowserSessionLoginRequestStatusCompleted || persistedRequest.CompletedAt == nil {
		t.Fatalf("persistedRequest = %+v, want completed metadata", persistedRequest)
	}
	counts := countBrowserSessionEventTypes(t, ctx, store)
	if counts[runtimeevents.EventBrowserSessionStatusChanged] != 2 || counts[runtimeevents.EventBrowserSessionVerified] != 1 || counts[runtimeevents.EventBrowserSessionLoginCompleted] != 1 {
		t.Fatalf("browser session event counts = %#v, want login_requested+verified status changes, verified, and login_completed", counts)
	}
}

func TestOperationalHandlerBrowserSessionHandoffCompleteFormReturnsEscapedHTML(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	defer store.Close()
	now := time.Date(2026, 5, 6, 20, 0, 0, 0, time.UTC)
	store.Now = func() time.Time { return now }
	store.BrowserSessionHandoffID = func() (string, error) { return "complete-form-handoff", nil }

	session, err := store.CreateBrowserSession(ctx, sqlite.CreateBrowserSessionParams{
		Name:           `google <script>alert(1)</script>`,
		Domain:         "google.com",
		AccountHint:    `marcus"><input name=password>`,
		PermissionTier: sqlite.BrowserSessionPermissionTierAuthenticatedReadOnly,
	})
	if err != nil {
		t.Fatalf("CreateBrowserSession() error = %v", err)
	}
	loginRequest, err := store.CreateBrowserSessionLoginRequest(ctx, sqlite.CreateBrowserSessionLoginRequestParams{
		SessionID: session.ID,
		ExpiresAt: now.Add(10 * time.Minute),
	})
	if err != nil {
		t.Fatalf("CreateBrowserSessionLoginRequest() error = %v", err)
	}

	server := httptest.NewServer(httpapi.NewOperationalHandler(httpapi.Dependencies{
		Store:      store,
		AdminToken: "secret",
	}))
	defer server.Close()

	form := url.Values{"handoff_id": []string{loginRequest.HandoffID}}
	request, err := http.NewRequest(http.MethodPost, server.URL+"/browser/session/handoff/complete", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Authorization", "Bearer secret")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("POST handoff complete form error = %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("handoff complete form status = %d body=%s, want %d", response.StatusCode, string(body), http.StatusOK)
	}
	if contentType := response.Header.Get("Content-Type"); !strings.Contains(contentType, "text/html") {
		t.Fatalf("handoff complete form Content-Type = %q, want text/html", contentType)
	}
	bodyBytes, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("ReadAll(completion html) error = %v", err)
	}
	body := string(bodyBytes)
	for _, want := range []string{
		"Browser Login Handoff Complete",
		"google &lt;script&gt;alert(1)&lt;/script&gt;",
		"google.com",
		"verified",
		"completed",
		"Operator-attested completion only.",
		"No browser was launched by Odin.",
		"No credentials or profile bytes were collected.",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("completion html body missing %q:\n%s", want, body)
		}
	}
	for _, forbidden := range []string{
		`google <script>alert(1)</script>`,
		`<script`,
		`<form`,
		`<input`,
		`type="password"`,
		`<textarea`,
	} {
		if strings.Contains(strings.ToLower(body), strings.ToLower(forbidden)) {
			t.Fatalf("completion html body contains forbidden %q:\n%s", forbidden, body)
		}
	}
}

func TestOperationalHandlerBrowserSessionHandoffCompleteRejectsInvalidStates(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	defer store.Close()
	now := time.Date(2026, 5, 6, 20, 0, 0, 0, time.UTC)
	store.Now = func() time.Time { return now }

	session, err := store.CreateBrowserSession(ctx, sqlite.CreateBrowserSessionParams{
		Name:           "google-main",
		Domain:         "google.com",
		PermissionTier: sqlite.BrowserSessionPermissionTierAuthenticatedReadOnly,
	})
	if err != nil {
		t.Fatalf("CreateBrowserSession() error = %v", err)
	}
	handoffIDs := []string{"completed-complete-handoff", "expired-complete-handoff", "revoked-complete-handoff"}
	store.BrowserSessionHandoffID = func() (string, error) {
		next := handoffIDs[0]
		handoffIDs = handoffIDs[1:]
		return next, nil
	}
	completed, err := store.CreateBrowserSessionLoginRequest(ctx, sqlite.CreateBrowserSessionLoginRequestParams{
		SessionID: session.ID,
		ExpiresAt: now.Add(10 * time.Minute),
	})
	if err != nil {
		t.Fatalf("CreateBrowserSessionLoginRequest(completed) error = %v", err)
	}
	if _, err := store.CompleteBrowserSessionLoginRequest(ctx, sqlite.CompleteBrowserSessionLoginRequestParams{RequestID: completed.ID}); err != nil {
		t.Fatalf("CompleteBrowserSessionLoginRequest() error = %v", err)
	}
	expired, err := store.CreateBrowserSessionLoginRequest(ctx, sqlite.CreateBrowserSessionLoginRequestParams{
		SessionID: session.ID,
		ExpiresAt: now.Add(10 * time.Minute),
	})
	if err != nil {
		t.Fatalf("CreateBrowserSessionLoginRequest(expired) error = %v", err)
	}
	revokedSession, err := store.CreateBrowserSession(ctx, sqlite.CreateBrowserSessionParams{
		Name:           "github-main",
		Domain:         "github.com",
		PermissionTier: sqlite.BrowserSessionPermissionTierAuthenticatedReadOnly,
	})
	if err != nil {
		t.Fatalf("CreateBrowserSession(revoked) error = %v", err)
	}
	revokedRequest, err := store.CreateBrowserSessionLoginRequest(ctx, sqlite.CreateBrowserSessionLoginRequestParams{
		SessionID: revokedSession.ID,
		ExpiresAt: now.Add(10 * time.Minute),
	})
	if err != nil {
		t.Fatalf("CreateBrowserSessionLoginRequest(revoked) error = %v", err)
	}
	if _, err := store.RevokeBrowserSession(ctx, sqlite.RevokeBrowserSessionParams{
		SessionID: revokedSession.ID,
		Actor:     "operator",
		Reason:    "test revocation",
	}); err != nil {
		t.Fatalf("RevokeBrowserSession() error = %v", err)
	}

	server := httptest.NewServer(httpapi.NewOperationalHandler(httpapi.Dependencies{
		Store:      store,
		AdminToken: "secret",
	}))
	defer server.Close()

	assertHandoffCompleteJSONStatus(t, server.URL, "secret", "", http.StatusBadRequest)
	assertHandoffCompleteJSONStatus(t, server.URL, "secret", "missing-complete-handoff", http.StatusNotFound)
	assertHandoffCompleteJSONStatus(t, server.URL, "secret", completed.HandoffID, http.StatusConflict)
	store.Now = func() time.Time { return now.Add(11 * time.Minute) }
	assertHandoffCompleteJSONStatus(t, server.URL, "secret", expired.HandoffID, http.StatusGone)
	store.Now = func() time.Time { return now }
	assertHandoffCompleteJSONStatus(t, server.URL, "secret", revokedRequest.HandoffID, http.StatusConflict)
	assertHandoffCompleteJSONStatus(t, server.URL, "", revokedRequest.HandoffID, http.StatusUnauthorized)
}

func TestMobileAPIRequiresAuthForReadsAndMutations(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openStore(t)
	defer store.Close()

	seedHealthyObservability(t, ctx, store)
	seedRuntimeState(t, ctx, store, "ready")

	server := httptest.NewServer(httpapi.NewOperationalHandler(httpapi.Dependencies{
		Health:          healthsvc.Service{DB: store.DB()},
		Metrics:         metricsvc.Service{DB: store.DB()},
		Store:           store,
		ReadModels:      store.DB(),
		RegistryHealthy: true,
		AdminToken:      "secret",
	}))
	defer server.Close()

	res := mustRequest(t, server, http.MethodGet, "/mobile/status", nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("GET /mobile/status unauthenticated status = %d, want %d", res.StatusCode, http.StatusUnauthorized)
	}

	res = mustRequestWithHeaders(t, server, http.MethodPost, "/mobile/intake/raw", bytes.NewBufferString(`{"kind":"idea","content":"ship mobile"}`), map[string]string{
		"Authorization": "Bearer wrong",
	})
	defer res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("POST /mobile/intake/raw wrong-token status = %d, want %d", res.StatusCode, http.StatusForbidden)
	}
}

func TestMobileOverviewUsesCanonicalOverviewProjection(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openStore(t)
	defer store.Close()

	seedHealthyObservability(t, ctx, store)
	seedRuntimeState(t, ctx, store, "ready")
	seedOperatorReadModels(t, ctx, store)
	project := mustProject(t, ctx, store, "alpha")
	seedDashboardTask(t, ctx, store, project.ID, "mobile-open-work")

	server := httptest.NewServer(httpapi.NewOperationalHandler(httpapi.Dependencies{
		Health:          healthsvc.Service{DB: store.DB()},
		Metrics:         metricsvc.Service{DB: store.DB()},
		Store:           store,
		ReadModels:      store.DB(),
		RegistryHealthy: true,
		AdminToken:      "secret",
	}))
	defer server.Close()

	res := mustMobileRequest(t, server, http.MethodGet, "/mobile/overview", "secret", nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("GET /mobile/overview status = %d body=%s, want %d", res.StatusCode, string(body), http.StatusOK)
	}
	var got struct {
		ActualUse struct {
			WorkItemCount        int `json:"work_item_count"`
			OpenWorkItemCount    int `json:"open_work_item_count"`
			PendingApprovalCount int `json:"pending_approval_count"`
			ReviewQueueCount     int `json:"review_queue_count"`
		} `json:"actual_use"`
	}
	decodeJSON(t, res.Body, &got)

	expected, err := (clioverview.Service{
		Store:           store,
		ReadinessStatus: "ready",
		HealthStatus:    "healthy",
	}).Build(ctx, scope.Resolution{Kind: scope.ScopeGlobal})
	if err != nil {
		t.Fatalf("overview.Build() error = %v", err)
	}
	if got.ActualUse.WorkItemCount != expected.ActualUse.WorkItemCount ||
		got.ActualUse.OpenWorkItemCount != expected.ActualUse.OpenWorkItemCount ||
		got.ActualUse.PendingApprovalCount != expected.ActualUse.PendingApprovalCount ||
		got.ActualUse.ReviewQueueCount != expected.ActualUse.ReviewQueueCount {
		t.Fatalf("mobile overview actual_use = %+v, want canonical %+v", got.ActualUse, expected.ActualUse)
	}
}

func TestMobileApprovalDecisionUsesApprovalServiceAndEmitsAudit(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openStore(t)
	defer store.Close()

	seedOperatorReadModels(t, ctx, store)
	project := mustProject(t, ctx, store, "alpha")
	task := seedDashboardTask(t, ctx, store, project.ID, "mobile-approval")
	_, approval, err := store.BlockTaskAndRequestApproval(ctx, sqlite.BlockTaskAndRequestApprovalParams{
		TaskID:      task.ID,
		RequestedBy: "test",
	})
	if err != nil {
		t.Fatalf("BlockTaskAndRequestApproval() error = %v", err)
	}
	if _, err := store.UpdateTaskQueueState(ctx, sqlite.UpdateTaskQueueStateParams{
		TaskID:        task.ID,
		Status:        "blocked",
		BlockedReason: "approval_required",
	}); err != nil {
		t.Fatalf("UpdateTaskQueueState(approval_required) error = %v", err)
	}

	server := httptest.NewServer(httpapi.NewOperationalHandler(httpapi.Dependencies{
		Store:      store,
		ReadModels: store.DB(),
		AdminToken: "secret",
	}))
	defer server.Close()

	body := bytes.NewBufferString(`{"action":"approve","reason":"mobile approval test","decision_by":"mobile-test"}`)
	res := mustMobileRequest(t, server, http.MethodPost, fmt.Sprintf("/mobile/approvals/%d/decision", approval.ID), "secret", body)
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(res.Body)
		t.Fatalf("POST mobile approval decision status = %d body=%s, want %d", res.StatusCode, string(raw), http.StatusOK)
	}
	var response struct {
		ApprovalID int64  `json:"approval_id"`
		Status     string `json:"status"`
	}
	decodeJSON(t, res.Body, &response)
	if response.ApprovalID != approval.ID || response.Status != "approved" {
		t.Fatalf("approval decision response = %+v, want approval %d approved", response, approval.ID)
	}

	events, err := store.ListEvents(ctx, sqlite.ListEventsParams{})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	if !hasRuntimeEventType(events, runtimeevents.EventApprovalResolved) {
		t.Fatalf("events = %+v, want approval.resolved audit event", events)
	}
}

func TestMobileRawIntakeCreateUsesCanonicalIntakePath(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openStore(t)
	defer store.Close()

	server := httptest.NewServer(httpapi.NewOperationalHandler(httpapi.Dependencies{
		Store:      store,
		ReadModels: store.DB(),
		AdminToken: "secret",
	}))
	defer server.Close()

	res := mustMobileRequest(t, server, http.MethodPost, "/mobile/intake/raw", "secret", bytes.NewBufferString(`{"kind":"idea","title":"Mobile idea","content":"Build a governed mobile API"}`))
	defer res.Body.Close()
	if res.StatusCode != http.StatusAccepted {
		raw, _ := io.ReadAll(res.Body)
		t.Fatalf("POST /mobile/intake/raw status = %d body=%s, want %d", res.StatusCode, string(raw), http.StatusAccepted)
	}
	rawBody, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if strings.Contains(string(rawBody), "Build a governed mobile API") {
		t.Fatalf("mobile intake response echoed raw content: %s", string(rawBody))
	}
	var response struct {
		IntakeItem struct {
			ID         int64  `json:"id"`
			Status     string `json:"status"`
			Source     string `json:"source"`
			IntakeType string `json:"intake_type"`
			Subject    string `json:"subject"`
		} `json:"intake_item"`
	}
	if err := json.Unmarshal(rawBody, &response); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if response.IntakeItem.ID == 0 || response.IntakeItem.Status != "received" || response.IntakeItem.Source != "mobile_api" || response.IntakeItem.IntakeType != "idea" {
		t.Fatalf("mobile intake response = %+v, want received mobile idea", response.IntakeItem)
	}

	items, err := store.ListIntakeItems(ctx, sqlite.ListIntakeItemsParams{WorkspaceID: "default"})
	if err != nil {
		t.Fatalf("ListIntakeItems() error = %v", err)
	}
	if len(items) != 1 || items[0].Subject != "Mobile idea" {
		t.Fatalf("intake items = %+v, want one Mobile idea", items)
	}
	events, err := store.ListEvents(ctx, sqlite.ListEventsParams{})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	if !hasRuntimeEventType(events, runtimeevents.EventIntakeItemCreated) {
		t.Fatalf("events = %+v, want intake.item_created audit event", events)
	}
}

type recordingAdminActions struct {
	killSwitchOnCalls int
	pauseIssueID      int64
	resumeIssueID     int64
}

func (admin *recordingAdminActions) KillSwitchOn(context.Context) error {
	admin.killSwitchOnCalls++
	return nil
}

func (admin *recordingAdminActions) KillSwitchOff(context.Context) error {
	return nil
}

func (admin *recordingAdminActions) PauseIssue(_ context.Context, issueID int64) error {
	admin.pauseIssueID = issueID
	return nil
}

func (admin *recordingAdminActions) ResumeIssue(_ context.Context, issueID int64) error {
	admin.resumeIssueID = issueID
	return nil
}

func mustPost(t *testing.T, url string, token string) *http.Response {
	t.Helper()

	request, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		t.Fatalf("NewRequest(%s) error = %v", url, err)
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("POST %s error = %v", url, err)
	}
	return response
}

func mustMobileRequest(t *testing.T, server *httptest.Server, method string, target string, token string, body io.Reader) *http.Response {
	t.Helper()

	request, err := http.NewRequest(method, server.URL+target, body)
	if err != nil {
		t.Fatalf("NewRequest(%s %s) error = %v", method, target, err)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("%s %s error = %v", method, target, err)
	}
	return response
}

func assertHandoffStatus(t *testing.T, url string, want int) {
	t.Helper()
	response, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s error = %v", url, err)
	}
	defer response.Body.Close()
	if response.StatusCode != want {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("GET %s status = %d body=%s, want %d", url, response.StatusCode, string(body), want)
	}
}

func assertGETStatus(t *testing.T, url string, want int) {
	t.Helper()
	response, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s error = %v", url, err)
	}
	defer response.Body.Close()
	if response.StatusCode != want {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("GET %s status = %d body=%s, want %d", url, response.StatusCode, string(body), want)
	}
}

func assertHandoffCompleteJSONStatus(t *testing.T, serverURL string, token string, handoffID string, want int) {
	t.Helper()
	body := bytes.NewBufferString(`{"handoff_id":"` + handoffID + `"}`)
	request, err := http.NewRequest(http.MethodPost, serverURL+"/browser/session/handoff/complete", body)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("POST handoff complete error = %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != want {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("POST handoff complete status = %d body=%s, want %d", response.StatusCode, string(body), want)
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

func mustProject(t *testing.T, ctx context.Context, store *sqlite.Store, key string) sqlite.Project {
	t.Helper()
	project, err := store.GetProjectByKey(ctx, key)
	if err != nil {
		t.Fatalf("GetProjectByKey(%s) error = %v", key, err)
	}
	return project
}

func countBrowserSessionEvents(t *testing.T, ctx context.Context, store *sqlite.Store) int {
	t.Helper()
	events, err := store.ListEvents(ctx, sqlite.ListEventsParams{})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	count := 0
	for _, event := range events {
		if event.StreamType == "browser_session" {
			count++
		}
	}
	return count
}

func countBrowserSessionEventTypes(t *testing.T, ctx context.Context, store *sqlite.Store) map[runtimeevents.Type]int {
	t.Helper()
	events, err := store.ListEvents(ctx, sqlite.ListEventsParams{})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	counts := map[runtimeevents.Type]int{}
	for _, event := range events {
		if event.StreamType == "browser_session" {
			counts[event.Type]++
		}
	}
	return counts
}

func hasRuntimeEventType(events []runtimeevents.Record, target runtimeevents.Type) bool {
	for _, event := range events {
		if event.Type == target {
			return true
		}
	}
	return false
}

func seedDashboardIssue(t *testing.T, ctx context.Context, store *sqlite.Store, repo string, number int, provider string, state string, syncStatus string) {
	t.Helper()

	project := mustProject(t, ctx, store, "alpha")
	if _, err := store.UpsertExternalIssue(ctx, sqlite.UpsertExternalIssueParams{
		ProjectID:  project.ID,
		Provider:   provider,
		Repo:       repo,
		Number:     number,
		Title:      fmt.Sprintf("Issue %d", number),
		BodyHash:   fmt.Sprintf("sha256:%d", number),
		URL:        fmt.Sprintf("https://github.com/%s/issues/%d", repo, number),
		State:      state,
		LabelsJSON: `[]`,
		SyncStatus: syncStatus,
	}); err != nil {
		t.Fatalf("UpsertExternalIssue(%s#%d) error = %v", repo, number, err)
	}
}

func seedExternalIssue(t *testing.T, ctx context.Context, store *sqlite.Store) {
	t.Helper()

	project, err := store.GetProjectByKey(ctx, "alpha")
	if err != nil {
		t.Fatalf("GetProjectByKey(alpha) error = %v", err)
	}
	if _, err := store.UpsertExternalIssue(ctx, sqlite.UpsertExternalIssueParams{
		ProjectID:  project.ID,
		Provider:   "github",
		Repo:       "owner/alpha",
		Number:     42,
		Title:      "Wire dashboard status",
		BodyHash:   "sha256:test",
		URL:        "https://github.com/owner/alpha/issues/42",
		State:      "open",
		LabelsJSON: `["odin:ready","agent:backend"]`,
		SyncStatus: "eligible",
	}); err != nil {
		t.Fatalf("UpsertExternalIssue() error = %v", err)
	}
}

func seedDashboardTask(t *testing.T, ctx context.Context, store *sqlite.Store, projectID int64, key string) sqlite.Task {
	t.Helper()
	task, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   projectID,
		Key:         key,
		Title:       key,
		Status:      "queued",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask(%s) error = %v", key, err)
	}
	return task
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

	activeSwarmTask, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:    project.ID,
		Key:          "alpha-swarm-active",
		Title:        "Active companion swarm",
		Status:       "running",
		Scope:        "project",
		RequestedBy:  "operator",
		WorkspaceID:  &workspace.ID,
		InitiativeID: &initiative.ID,
		CompanionID:  &companion.ID,
		WorkKind:     "delivery",
	})
	if err != nil {
		t.Fatalf("CreateTask(alpha-swarm-active) error = %v", err)
	}
	activeDelegation, err := store.CreateDelegation(ctx, sqlite.CreateDelegationParams{
		ParentTaskID:    activeSwarmTask.ID,
		ProjectID:       project.ID,
		Scope:           activeSwarmTask.Scope,
		DelegationKey:   "alpha-swarm-active-child",
		Role:            "builder",
		ActionClass:     "mutation",
		ActionKey:       "implement",
		MutationMode:    "read_only",
		Status:          "queued",
		ConvergenceMode: "merge",
		ArtifactTarget:  "branch",
		Executor:        "codex",
		DetailsJSON:     `{"objective":"active swarm","swarm":{"requested_budget":2,"max_children":2}}`,
	})
	if err != nil {
		t.Fatalf("CreateDelegation(active swarm) error = %v", err)
	}
	activeChild, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "alpha-swarm-active-child",
		Title:       "Active child task",
		Status:      "running",
		Scope:       "project",
		RequestedBy: "supervisor",
	})
	if err != nil {
		t.Fatalf("CreateTask(alpha-swarm-active-child) error = %v", err)
	}
	activeRun, err := store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:     activeChild.ID,
		Executor:   "codex",
		Attempt:    1,
		Status:     "running",
		TaskStatus: "running",
	})
	if err != nil {
		t.Fatalf("StartRun(alpha-swarm-active-child) error = %v", err)
	}
	if _, err := store.AttachDelegationChildTask(ctx, sqlite.AttachDelegationChildTaskParams{
		DelegationID: activeDelegation.ID,
		ChildTaskID:  activeChild.ID,
		ChildRunID:   &activeRun.ID,
	}); err != nil {
		t.Fatalf("AttachDelegationChildTask(alpha-swarm-active) error = %v", err)
	}

	approvalSwarmTask, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:    project.ID,
		Key:          "alpha-swarm-approval",
		Title:        "Approval blocked swarm",
		Status:       "running",
		Scope:        "project",
		RequestedBy:  "operator",
		WorkspaceID:  &workspace.ID,
		InitiativeID: &initiative.ID,
		CompanionID:  &companion.ID,
		WorkKind:     "delivery",
	})
	if err != nil {
		t.Fatalf("CreateTask(alpha-swarm-approval) error = %v", err)
	}
	approvalDelegation, err := store.CreateDelegation(ctx, sqlite.CreateDelegationParams{
		ParentTaskID:    approvalSwarmTask.ID,
		ProjectID:       project.ID,
		Scope:           approvalSwarmTask.Scope,
		DelegationKey:   "alpha-swarm-approval-child",
		Role:            "builder",
		ActionClass:     "mutation",
		ActionKey:       "implement",
		MutationMode:    "read_only",
		Status:          "queued",
		ConvergenceMode: "review_gate",
		ArtifactTarget:  "branch",
		Executor:        "codex",
		DetailsJSON:     `{"objective":"approval blocked","swarm":{"requested_budget":1,"max_children":1}}`,
	})
	if err != nil {
		t.Fatalf("CreateDelegation(alpha-swarm-approval) error = %v", err)
	}
	approvalChild, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "alpha-swarm-approval-child",
		Title:       "Approval child task",
		Status:      "running",
		Scope:       "project",
		RequestedBy: "supervisor",
	})
	if err != nil {
		t.Fatalf("CreateTask(alpha-swarm-approval-child) error = %v", err)
	}
	if _, _, err := store.BlockTaskAndRequestApproval(ctx, sqlite.BlockTaskAndRequestApprovalParams{
		TaskID:      approvalChild.ID,
		RunID:       nil,
		RequestedBy: "system",
	}); err != nil {
		t.Fatalf("BlockTaskAndRequestApproval(alpha-swarm-approval-child) error = %v", err)
	}
	if _, err := store.AttachDelegationChildTask(ctx, sqlite.AttachDelegationChildTaskParams{
		DelegationID: approvalDelegation.ID,
		ChildTaskID:  approvalChild.ID,
	}); err != nil {
		t.Fatalf("AttachDelegationChildTask(alpha-swarm-approval) error = %v", err)
	}

	budgetSwarmTask, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:    project.ID,
		Key:          "alpha-swarm-budget",
		Title:        "Budget blocked swarm",
		Status:       "blocked",
		Scope:        "project",
		RequestedBy:  "operator",
		WorkspaceID:  &workspace.ID,
		InitiativeID: &initiative.ID,
		CompanionID:  &companion.ID,
		WorkKind:     "delivery",
	})
	if err != nil {
		t.Fatalf("CreateTask(alpha-swarm-budget) error = %v", err)
	}
	if _, err := store.BlockTask(ctx, sqlite.BlockTaskParams{
		TaskID: budgetSwarmTask.ID,
		Reason: "budget_exhausted",
	}); err != nil {
		t.Fatalf("BlockTask(alpha-swarm-budget) error = %v", err)
	}
	budgetDelegation, err := store.CreateDelegation(ctx, sqlite.CreateDelegationParams{
		ParentTaskID:    budgetSwarmTask.ID,
		ProjectID:       project.ID,
		Scope:           budgetSwarmTask.Scope,
		DelegationKey:   "alpha-swarm-budget-child",
		Role:            "reviewer",
		ActionClass:     "analysis",
		ActionKey:       "review",
		MutationMode:    "read_only",
		Status:          "queued",
		ConvergenceMode: "merge",
		ArtifactTarget:  "report",
		Executor:        "codex",
		DetailsJSON:     `{"objective":"budget blocked","swarm":{"requested_budget":3,"max_children":1}}`,
	})
	if err != nil {
		t.Fatalf("CreateDelegation(alpha-swarm-budget) error = %v", err)
	}
	budgetChild, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "alpha-swarm-budget-child",
		Title:       "Budget child task",
		Status:      "queued",
		Scope:       "project",
		RequestedBy: "supervisor",
	})
	if err != nil {
		t.Fatalf("CreateTask(alpha-swarm-budget-child) error = %v", err)
	}
	if _, err := store.AttachDelegationChildTask(ctx, sqlite.AttachDelegationChildTaskParams{
		DelegationID: budgetDelegation.ID,
		ChildTaskID:  budgetChild.ID,
	}); err != nil {
		t.Fatalf("AttachDelegationChildTask(alpha-swarm-budget) error = %v", err)
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
