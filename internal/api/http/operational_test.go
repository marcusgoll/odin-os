package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
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
	"odin-os/internal/core/initiatives"
	coremedia "odin-os/internal/core/media"
	"odin-os/internal/core/workspaces"
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

	const adminToken = "ghp_dashboard_secret_token"
	server := httptest.NewServer(httpapi.NewOperationalHandler(httpapi.Dependencies{
		Health: healthsvc.Service{DB: store.DB()},
		Metrics: metricsvc.Service{
			DB: store.DB(),
		},
		ReadModels:      store.DB(),
		RegistryHealthy: true,
		AdminToken:      adminToken,
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
		Counts struct {
			WorkItems         int `json:"work_items"`
			ActiveRunAttempts int `json:"active_run_attempts"`
			PendingApprovals  int `json:"pending_approvals"`
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
	if status.Counts.WorkItems == 0 || status.Counts.ActiveRunAttempts == 0 || status.Counts.PendingApprovals == 0 {
		t.Fatalf("/status counts = %+v, want runtime-state-backed counts", status.Counts)
	}
	if status.Tmux.Available || status.Tmux.Source != "not_configured" {
		t.Fatalf("/status tmux = %+v, want absence reported without daemon failure", status.Tmux)
	}
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

	res = mustPost(t, server.URL+"/issues/42/pause", "dashboard-secret")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("POST /issues/42/pause status = %d, want %d", res.StatusCode, http.StatusOK)
	}
	res.Body.Close()
	if admin.pauseIssueID != 42 {
		t.Fatalf("PauseIssue issue id = %d, want 42", admin.pauseIssueID)
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
	if counts[runtimeevents.EventBrowserSessionStatusChanged] != 1 || counts[runtimeevents.EventBrowserSessionVerified] != 1 || counts[runtimeevents.EventBrowserSessionLoginCompleted] != 1 {
		t.Fatalf("browser session event counts = %#v, want status_changed, verified, and login_completed", counts)
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

type recordingAdminActions struct {
	killSwitchOnCalls int
	pauseIssueID      int64
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

func (admin *recordingAdminActions) ResumeIssue(context.Context, int64) error {
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
