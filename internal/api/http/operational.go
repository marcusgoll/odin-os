package httpapi

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	coreprojects "odin-os/internal/core/projects"
	coreworkspace "odin-os/internal/core/workspace"
	"odin-os/internal/core/workspaces"
	"odin-os/internal/registry"
	healthsvc "odin-os/internal/runtime/health"
	"odin-os/internal/runtime/projections"
	"odin-os/internal/runtime/triggers"
	"odin-os/internal/store/sqlite"
	metricsvc "odin-os/internal/telemetry/metrics"
)

type Dependencies struct {
	Health               healthsvc.Service
	Metrics              metricsvc.Service
	Store                *sqlite.Store
	ReadModels           projections.Queryer
	RegistryHealthy      bool
	Now                  func() time.Time
	AdminToken           string
	EmailActionSecret    string
	EmailActionBaseURL   string
	EmailActionRecipient string
	EmailActionFrom      string
	EmailActionSendmail  string
	Admin                AdminActions
	Tmux                 TmuxStatusProvider
	GitHubWebhookSecret  string
	GitHubIssueIngester  GitHubIssueIngester
	RegistrySnapshot     registry.Snapshot
	Registry             coreprojects.Registry
}

const operationalProbeCacheFreshAge = 45 * time.Second
const operationalProbeCacheMaxStaleAge = 5 * time.Minute
const operationalProbeRefreshTimeout = 10 * time.Second

type operationalProbeCache struct {
	mu                sync.Mutex
	readiness         cachedReadinessProbe
	metrics           cachedMetricsProbe
	metricsRefreshing bool
}

type cachedReadinessProbe struct {
	report     healthsvc.Report
	statusCode int
	cachedAt   time.Time
	ok         bool
}

type cachedMetricsProbe struct {
	snapshot metricsvc.Snapshot
	cachedAt time.Time
	ok       bool
}

func (cache *operationalProbeCache) storeReadiness(cachedAt time.Time, report healthsvc.Report, ready bool) {
	if !ready {
		return
	}
	statusCode := http.StatusOK

	cache.mu.Lock()
	defer cache.mu.Unlock()
	cache.readiness = cachedReadinessProbe{
		report:     report,
		statusCode: statusCode,
		cachedAt:   cachedAt.UTC(),
		ok:         true,
	}
}

func (cache *operationalProbeCache) recentReadiness(now time.Time, maxAge time.Duration) (healthsvc.Report, int, bool) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if !cache.readiness.ok || !recentProbeCacheHit(now, cache.readiness.cachedAt, maxAge) {
		return healthsvc.Report{}, 0, false
	}
	return cache.readiness.report, cache.readiness.statusCode, true
}

func (cache *operationalProbeCache) storeMetrics(cachedAt time.Time, snapshot metricsvc.Snapshot) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	cache.metrics = cachedMetricsProbe{
		snapshot: snapshot,
		cachedAt: cachedAt.UTC(),
		ok:       true,
	}
}

func (cache *operationalProbeCache) recentMetrics(now time.Time, maxAge time.Duration) (metricsvc.Snapshot, bool) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if !cache.metrics.ok || !recentProbeCacheHit(now, cache.metrics.cachedAt, maxAge) {
		return metricsvc.Snapshot{}, false
	}
	return cache.metrics.snapshot, true
}

func (cache *operationalProbeCache) refreshMetricsOnce(refresh func()) {
	cache.mu.Lock()
	if cache.metricsRefreshing {
		cache.mu.Unlock()
		return
	}
	cache.metricsRefreshing = true
	cache.mu.Unlock()

	go func() {
		defer func() {
			cache.mu.Lock()
			cache.metricsRefreshing = false
			cache.mu.Unlock()
		}()
		refresh()
	}()
}

func recentProbeCacheHit(now time.Time, cachedAt time.Time, maxAge time.Duration) bool {
	age := now.UTC().Sub(cachedAt.UTC())
	return age >= 0 && age <= maxAge
}

type AdminActions interface {
	KillSwitchOn(context.Context) error
	KillSwitchOff(context.Context) error
	PauseIssue(context.Context, int64) error
	ResumeIssue(context.Context, int64) error
}

type TmuxStatusProvider interface {
	Status(context.Context) (TmuxStatus, error)
}

type WorkspaceStatusLister interface {
	List(context.Context) ([]coreworkspace.Status, error)
}

type GitHubIssueIngester interface {
	IngestGitHubIssue(context.Context, triggers.GitHubIssueIngestParams) (triggers.GitHubIssueIngestResult, error)
}

type TmuxStatus struct {
	Available        bool                   `json:"available"`
	Source           string                 `json:"source"`
	Error            string                 `json:"error,omitempty"`
	LiveSessions     int                    `json:"live_sessions,omitempty"`
	AttachedSessions int                    `json:"attached_sessions,omitempty"`
	Sessions         []TmuxWorkspaceSession `json:"sessions,omitempty"`
}

type TmuxWorkspaceSession struct {
	ProjectKey    string `json:"project_key"`
	SessionName   string `json:"session_name"`
	State         string `json:"state"`
	FactsSource   string `json:"facts_source"`
	AttachedCount int    `json:"attached_count"`
}

type WorkspaceTmuxStatusProvider struct {
	Workspaces WorkspaceStatusLister
}

var ErrAdminActionNotImplemented = errors.New("admin action not implemented")
var ErrAdminTargetNotFound = errors.New("admin target not found")
var ErrAdminActionConflict = errors.New("admin action conflict")

func (provider WorkspaceTmuxStatusProvider) Status(ctx context.Context) (TmuxStatus, error) {
	if provider.Workspaces == nil {
		return TmuxStatus{Available: false, Source: "not_configured"}, nil
	}
	statuses, err := provider.Workspaces.List(ctx)
	if err != nil {
		return TmuxStatus{}, err
	}

	tmuxStatus := TmuxStatus{
		Source:   "workspace_sessions",
		Sessions: make([]TmuxWorkspaceSession, 0),
	}
	for _, status := range statuses {
		if status.State != coreworkspace.StateLive {
			continue
		}
		session := TmuxWorkspaceSession{
			ProjectKey:    status.ProjectKey,
			SessionName:   status.SessionName,
			State:         string(status.State),
			FactsSource:   string(status.FactsSource),
			AttachedCount: status.AttachedCount,
		}
		tmuxStatus.Sessions = append(tmuxStatus.Sessions, session)
		tmuxStatus.LiveSessions++
		if status.AttachedCount > 0 {
			tmuxStatus.AttachedSessions++
		}
	}
	tmuxStatus.Available = tmuxStatus.LiveSessions > 0
	return tmuxStatus, nil
}

func NewOperationalHandler(deps Dependencies) http.Handler {
	now := deps.Now
	if now == nil {
		now = func() time.Time {
			return time.Now().UTC()
		}
	}
	probeCache := &operationalProbeCache{}

	mux := http.NewServeMux()
	healthHandler := func(writer http.ResponseWriter, request *http.Request) {
		ctx, cancel := context.WithTimeout(request.Context(), 5*time.Second)
		defer cancel()

		report, err := deps.Health.Doctor(ctx, deps.RegistryHealthy)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusServiceUnavailable)
			return
		}
		writeJSON(writer, http.StatusOK, report)
	}
	mux.HandleFunc("/health", healthHandler)
	mux.HandleFunc("/healthz", healthHandler)
	mux.HandleFunc("/readyz", func(writer http.ResponseWriter, request *http.Request) {
		if cachedReport, statusCode, ok := probeCache.recentReadiness(now(), operationalProbeCacheFreshAge); ok {
			writer.Header().Set("X-Odin-Health-Source", "cache")
			writeJSON(writer, statusCode, cachedReport)
			return
		}

		ctx, cancel := context.WithTimeout(request.Context(), 5*time.Second)
		defer cancel()

		report, ready, err := deps.Health.Readiness(ctx, deps.RegistryHealthy)
		if err != nil {
			if cachedReport, statusCode, ok := probeCache.recentReadiness(now(), operationalProbeCacheMaxStaleAge); ok {
				writer.Header().Set("X-Odin-Health-Source", "stale-cache")
				writeJSON(writer, statusCode, cachedReport)
				return
			}
			http.Error(writer, err.Error(), http.StatusServiceUnavailable)
			return
		}

		statusCode := http.StatusOK
		if !ready {
			statusCode = http.StatusServiceUnavailable
		}
		probeCache.storeReadiness(now(), report, ready)
		writeJSON(writer, statusCode, report)
	})
	mux.HandleFunc("GET /status", func(writer http.ResponseWriter, request *http.Request) {
		payload, err := buildStatusPayload(request.Context(), deps, now)
		if err != nil {
			writeAPIError(writer, http.StatusServiceUnavailable, "status_unavailable", err.Error())
			return
		}
		writeJSON(writer, http.StatusOK, payload)
	})
	mux.HandleFunc("GET /issues", func(writer http.ResponseWriter, request *http.Request) {
		if deps.ReadModels == nil {
			writeAPIError(writer, http.StatusServiceUnavailable, "read_models_unavailable", "read models unavailable")
			return
		}
		options, err := parseDashboardListOptions(request)
		if err != nil {
			writeAPIError(writer, http.StatusBadRequest, "invalid_query", err.Error())
			return
		}
		query := request.URL.Query()
		issues, err := listDashboardIssues(request.Context(), deps.ReadModels, options, dashboardIssueFilters{
			Provider:   query.Get("provider"),
			Repo:       query.Get("repo"),
			State:      query.Get("state"),
			SyncStatus: query.Get("sync_status"),
		})
		if err != nil {
			writeAPIError(writer, http.StatusServiceUnavailable, "issues_unavailable", err.Error())
			return
		}
		writeJSON(writer, http.StatusOK, issues)
	})
	mux.HandleFunc("GET /runs", func(writer http.ResponseWriter, request *http.Request) {
		if deps.ReadModels == nil {
			writeAPIError(writer, http.StatusServiceUnavailable, "read_models_unavailable", "read models unavailable")
			return
		}
		options, err := parseDashboardListOptions(request)
		if err != nil {
			writeAPIError(writer, http.StatusBadRequest, "invalid_query", err.Error())
			return
		}
		query := request.URL.Query()
		var taskID *int64
		if rawTaskID := strings.TrimSpace(query.Get("task_id")); rawTaskID != "" {
			parsedTaskID, err := strconv.ParseInt(rawTaskID, 10, 64)
			if err != nil || parsedTaskID <= 0 {
				writeAPIError(writer, http.StatusBadRequest, "invalid_query", "task_id must be a positive integer")
				return
			}
			taskID = &parsedTaskID
		}
		views, err := listDashboardRuns(request.Context(), deps.ReadModels, options, dashboardRunFilters{
			Status:   query.Get("status"),
			Executor: query.Get("executor"),
			TaskID:   taskID,
		})
		if err != nil {
			writeAPIError(writer, http.StatusServiceUnavailable, "runs_unavailable", err.Error())
			return
		}
		writeJSON(writer, http.StatusOK, views)
	})
	mux.HandleFunc("GET /runs/{run_id}", func(writer http.ResponseWriter, request *http.Request) {
		if deps.ReadModels == nil {
			writeAPIError(writer, http.StatusServiceUnavailable, "read_models_unavailable", "read models unavailable")
			return
		}
		runID, err := strconv.ParseInt(request.PathValue("run_id"), 10, 64)
		if err != nil {
			writeAPIError(writer, http.StatusBadRequest, "invalid_run_id", "run id must be an integer")
			return
		}
		run, err := getDashboardRun(request.Context(), deps.ReadModels, runID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeAPIError(writer, http.StatusNotFound, "run_not_found", "run not found")
				return
			}
			writeAPIError(writer, http.StatusServiceUnavailable, "run_unavailable", err.Error())
			return
		}
		writeJSON(writer, http.StatusOK, run)
	})
	mux.HandleFunc("GET /browser/session/handoff", func(writer http.ResponseWriter, request *http.Request) {
		handleBrowserSessionHandoffShow(writer, request, deps)
	})
	mux.HandleFunc("GET /browser/session/handoff/viewer", func(writer http.ResponseWriter, request *http.Request) {
		handleBrowserSessionHandoffViewer(writer, request, deps)
	})
	mux.HandleFunc("/browser/session/handoff/viewer/proxy/", func(writer http.ResponseWriter, request *http.Request) {
		handleBrowserSessionHandoffViewerProxy(writer, request, deps)
	})
	mux.HandleFunc("POST /browser/session/handoff/complete", func(writer http.ResponseWriter, request *http.Request) {
		handleBrowserSessionHandoffComplete(writer, request, deps)
	})
	mux.HandleFunc("GET /pwa", func(writer http.ResponseWriter, request *http.Request) {
		handlePWAShell(writer, request)
	})
	mux.HandleFunc("GET /manifest.webmanifest", func(writer http.ResponseWriter, request *http.Request) {
		handlePWAManifest(writer, request)
	})
	mux.HandleFunc("GET /favicon.ico", func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, "/app/icons/icon-192.svg", http.StatusMovedPermanently)
	})
	mux.HandleFunc("GET /service-worker.js", func(writer http.ResponseWriter, request *http.Request) {
		handlePWAServiceWorker(writer, request)
	})
	mux.HandleFunc("GET /notifications", func(writer http.ResponseWriter, request *http.Request) {
		handleNotificationsList(writer, request, deps)
	})
	mux.HandleFunc("GET /notifications/preferences", func(writer http.ResponseWriter, request *http.Request) {
		handleNotificationPreferences(writer, request, deps)
	})
	mux.HandleFunc("GET /notifications/devices", func(writer http.ResponseWriter, request *http.Request) {
		handleNotificationDevices(writer, request, deps)
	})
	mux.HandleFunc("POST /notifications/subscriptions", func(writer http.ResponseWriter, request *http.Request) {
		handleNotificationSubscriptionCreate(writer, request, deps)
	})
	mux.HandleFunc("DELETE /notifications/devices/{device_id}", func(writer http.ResponseWriter, request *http.Request) {
		handleNotificationDeviceDelete(writer, request, deps)
	})
	mux.HandleFunc("POST /notifications/test-event", func(writer http.ResponseWriter, request *http.Request) {
		handleNotificationTestEvent(writer, request, deps)
	})
	mux.HandleFunc("GET /email-actions/preview", func(writer http.ResponseWriter, request *http.Request) {
		handleEmailActionPreview(writer, request, deps, now)
	})
	mux.HandleFunc("POST /email-actions/send", func(writer http.ResponseWriter, request *http.Request) {
		handleEmailActionSend(writer, request, deps, now)
	})
	mux.HandleFunc("GET /email-actions/open/{token}", func(writer http.ResponseWriter, request *http.Request) {
		handleEmailActionOpen(writer, request, deps, now)
	})
	mux.HandleFunc("GET /email-actions/{token}", func(writer http.ResponseWriter, request *http.Request) {
		handleEmailActionApply(writer, request, deps, now)
	})
	mux.HandleFunc("POST /kill-switch/on", func(writer http.ResponseWriter, request *http.Request) {
		handleAdminAction(writer, request, deps, "kill_switch_on", func(ctx context.Context, admin AdminActions) error {
			return admin.KillSwitchOn(ctx)
		})
	})
	mux.HandleFunc("POST /kill-switch/off", func(writer http.ResponseWriter, request *http.Request) {
		handleAdminAction(writer, request, deps, "kill_switch_off", func(ctx context.Context, admin AdminActions) error {
			return admin.KillSwitchOff(ctx)
		})
	})
	mux.HandleFunc("POST /issues/{issue_id}/pause", func(writer http.ResponseWriter, request *http.Request) {
		issueID, err := strconv.ParseInt(request.PathValue("issue_id"), 10, 64)
		if err != nil || issueID <= 0 {
			writeAPIError(writer, http.StatusBadRequest, "invalid_issue_id", "issue id must be a positive integer")
			return
		}
		handleAdminAction(writer, request, deps, "pause_issue", func(ctx context.Context, admin AdminActions) error {
			return admin.PauseIssue(ctx, issueID)
		})
	})
	mux.HandleFunc("POST /issues/{issue_id}/resume", func(writer http.ResponseWriter, request *http.Request) {
		issueID, err := strconv.ParseInt(request.PathValue("issue_id"), 10, 64)
		if err != nil || issueID <= 0 {
			writeAPIError(writer, http.StatusBadRequest, "invalid_issue_id", "issue id must be a positive integer")
			return
		}
		handleAdminAction(writer, request, deps, "resume_issue", func(ctx context.Context, admin AdminActions) error {
			return admin.ResumeIssue(ctx, issueID)
		})
	})
	mux.HandleFunc("POST /webhooks/github/issues", func(writer http.ResponseWriter, request *http.Request) {
		handleGitHubIssuesWebhook(writer, request, deps)
	})
	mux.HandleFunc("/metrics", func(writer http.ResponseWriter, request *http.Request) {
		if cachedSnapshot, ok := probeCache.recentMetrics(now(), operationalProbeCacheFreshAge); ok {
			writer.Header().Set("X-Odin-Metrics-Source", "cache")
			writer.Header().Set("Content-Type", "text/plain; version=0.0.4")
			writer.WriteHeader(http.StatusOK)
			_, _ = writer.Write([]byte(metricsvc.Render(cachedSnapshot)))
			return
		}
		if cachedSnapshot, ok := probeCache.recentMetrics(now(), operationalProbeCacheMaxStaleAge); ok {
			probeCache.refreshMetricsOnce(func() {
				ctx, cancel := context.WithTimeout(context.Background(), operationalProbeRefreshTimeout)
				defer cancel()
				snapshot, err := deps.Metrics.Collect(ctx)
				if err == nil {
					probeCache.storeMetrics(now(), snapshot)
				}
			})
			writer.Header().Set("X-Odin-Metrics-Source", "stale-cache")
			writer.Header().Set("Content-Type", "text/plain; version=0.0.4")
			writer.WriteHeader(http.StatusOK)
			_, _ = writer.Write([]byte(metricsvc.Render(cachedSnapshot)))
			return
		}

		ctx, cancel := context.WithTimeout(request.Context(), 3*time.Second)
		defer cancel()

		snapshot, err := deps.Metrics.Collect(ctx)
		if err != nil {
			if cachedSnapshot, ok := probeCache.recentMetrics(now(), operationalProbeCacheMaxStaleAge); ok {
				writer.Header().Set("X-Odin-Metrics-Source", "cache")
				writer.Header().Set("Content-Type", "text/plain; version=0.0.4")
				writer.WriteHeader(http.StatusOK)
				_, _ = writer.Write([]byte(metricsvc.Render(cachedSnapshot)))
				return
			}
			http.Error(writer, err.Error(), http.StatusServiceUnavailable)
			return
		}

		probeCache.storeMetrics(now(), snapshot)
		writer.Header().Set("Content-Type", "text/plain; version=0.0.4")
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte(metricsvc.Render(snapshot)))
	})
	mux.HandleFunc("/workspace", func(writer http.ResponseWriter, request *http.Request) {
		if deps.ReadModels == nil {
			http.Error(writer, "read models unavailable", http.StatusServiceUnavailable)
			return
		}
		view, err := projections.GetWorkspaceOverviewView(request.Context(), deps.ReadModels, workspaces.DefaultWorkspaceKey)
		if err != nil {
			if err == sql.ErrNoRows {
				http.NotFound(writer, request)
				return
			}
			http.Error(writer, err.Error(), http.StatusServiceUnavailable)
			return
		}
		writeJSON(writer, http.StatusOK, view)
	})
	mux.HandleFunc("/initiatives", func(writer http.ResponseWriter, request *http.Request) {
		if deps.ReadModels == nil {
			http.Error(writer, "read models unavailable", http.StatusServiceUnavailable)
			return
		}
		views, err := projections.ListInitiativePortfolioViews(request.Context(), deps.ReadModels, workspaces.DefaultWorkspaceKey)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusServiceUnavailable)
			return
		}
		writeJSON(writer, http.StatusOK, views)
	})
	mux.HandleFunc("/companions", func(writer http.ResponseWriter, request *http.Request) {
		if deps.ReadModels == nil {
			http.Error(writer, "read models unavailable", http.StatusServiceUnavailable)
			return
		}
		views, err := projections.ListCompanionAssignmentViews(request.Context(), deps.ReadModels, workspaces.DefaultWorkspaceKey)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusServiceUnavailable)
			return
		}
		writeJSON(writer, http.StatusOK, views)
	})
	mux.HandleFunc("/memoryz", func(writer http.ResponseWriter, request *http.Request) {
		if deps.ReadModels == nil {
			http.Error(writer, "read models unavailable", http.StatusServiceUnavailable)
			return
		}
		workspaceViews, err := projections.ListWorkspaceMemoryViews(request.Context(), deps.ReadModels, projections.WorkspaceMemoryQuery{
			WorkspaceKey: workspaces.DefaultWorkspaceKey,
			Limit:        1,
		})
		if err != nil {
			http.Error(writer, err.Error(), http.StatusServiceUnavailable)
			return
		}
		initiativeViews, err := projections.ListInitiativeMemoryViews(request.Context(), deps.ReadModels, projections.InitiativeMemoryQuery{
			WorkspaceKey: workspaces.DefaultWorkspaceKey,
			Limit:        50,
		})
		if err != nil {
			http.Error(writer, err.Error(), http.StatusServiceUnavailable)
			return
		}
		companionViews, err := projections.ListCompanionMemoryViews(request.Context(), deps.ReadModels, projections.CompanionMemoryQuery{
			WorkspaceKey: workspaces.DefaultWorkspaceKey,
			Limit:        50,
		})
		if err != nil {
			http.Error(writer, err.Error(), http.StatusServiceUnavailable)
			return
		}
		writeJSON(writer, http.StatusOK, map[string]any{
			"workspace":   workspaceViews,
			"initiatives": initiativeViews,
			"companions":  companionViews,
		})
	})
	mux.HandleFunc("/blocked", func(writer http.ResponseWriter, request *http.Request) {
		if deps.ReadModels == nil {
			http.Error(writer, "read models unavailable", http.StatusServiceUnavailable)
			return
		}
		views, err := projections.ListBlockedItemViews(request.Context(), deps.ReadModels)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusServiceUnavailable)
			return
		}
		writeJSON(writer, http.StatusOK, views)
	})
	mux.HandleFunc("/agenda", func(writer http.ResponseWriter, request *http.Request) {
		if deps.ReadModels == nil {
			http.Error(writer, "read models unavailable", http.StatusServiceUnavailable)
			return
		}
		view, err := projections.GetAgendaView(request.Context(), deps.ReadModels, workspaces.DefaultWorkspaceKey, now().UTC())
		if err != nil {
			if err == sql.ErrNoRows {
				http.NotFound(writer, request)
				return
			}
			http.Error(writer, err.Error(), http.StatusServiceUnavailable)
			return
		}
		writeJSON(writer, http.StatusOK, view)
	})
	registerMobileRoutes(mux, deps, now)
	registerPWAHandlers(mux, deps)
	return withOperationalSecurity(mux)
}

func withOperationalSecurity(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("X-Content-Type-Options", "nosniff")
		writer.Header().Set("X-Frame-Options", "DENY")
		writer.Header().Set("Referrer-Policy", "no-referrer")
		writer.Header().Set("Permissions-Policy", "camera=(self), microphone=(self), geolocation=()")
		writer.Header().Set("Content-Security-Policy", "default-src 'self'; connect-src 'self' https://cloudflareinsights.com; img-src 'self' blob: data:; media-src 'self' blob:; script-src 'self' https://static.cloudflareinsights.com; style-src 'self'; manifest-src 'self'; base-uri 'self'; frame-ancestors 'none'")
		applyLockedDownCORS(writer, request)
		if request.Method == http.MethodOptions {
			if request.Header.Get("Access-Control-Request-Method") != "" {
				writer.WriteHeader(http.StatusNoContent)
				return
			}
		}
		next.ServeHTTP(writer, request)
	})
}

func applyLockedDownCORS(writer http.ResponseWriter, request *http.Request) {
	origin := strings.TrimSpace(request.Header.Get("Origin"))
	if origin == "" {
		return
	}
	writer.Header().Add("Vary", "Origin")
	if !sameOriginRequest(origin, request.Host) {
		return
	}
	writer.Header().Set("Access-Control-Allow-Origin", origin)
	writer.Header().Set("Access-Control-Allow-Credentials", "true")
	writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Odin-CSRF")
	writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
}

func sameOriginRequest(origin string, host string) bool {
	parsed, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return strings.EqualFold(parsed.Host, host) && (parsed.Scheme == "https" || parsed.Scheme == "http")
}

type browserSessionHandoffResponse struct {
	Handoff browserSessionHandoffView `json:"handoff"`
}

type browserSessionHandoffCompletionResponse struct {
	Completion browserSessionHandoffCompletionView `json:"completion"`
}

type browserSessionHandoffView struct {
	HandoffID           string `json:"handoff_id"`
	LoginRequestID      int64  `json:"login_request_id"`
	SessionID           int64  `json:"session_id"`
	SessionName         string `json:"session_name"`
	Domain              string `json:"domain"`
	AccountHint         string `json:"account_hint"`
	ExpiresAt           string `json:"expires_at"`
	Status              string `json:"status"`
	AllowedActions      string `json:"allowed_actions"`
	RunnerStatus        string `json:"runner_status,omitempty"`
	RunnerID            string `json:"runner_id,omitempty"`
	ViewerURL           string `json:"viewer_url,omitempty"`
	ProtectedViewerURL  string `json:"protected_viewer_url,omitempty"`
	RealBrowserEvidence bool   `json:"real_browser_evidence,omitempty"`
}

type browserSessionHandoffCompletionView struct {
	HandoffID          string `json:"handoff_id"`
	LoginRequestID     int64  `json:"login_request_id"`
	SessionID          int64  `json:"session_id"`
	SessionName        string `json:"session_name"`
	Domain             string `json:"domain"`
	AccountHint        string `json:"account_hint"`
	SessionStatus      string `json:"session_status"`
	LoginRequestStatus string `json:"login_request_status"`
	CompletedAt        string `json:"completed_at"`
	AllowedActions     string `json:"allowed_actions"`
}

type browserSessionHandoffViewerResponse struct {
	Viewer browserSessionHandoffViewerView `json:"viewer"`
}

type browserSessionHandoffViewerView struct {
	HandoffID          string `json:"handoff_id"`
	LoginRequestID     int64  `json:"login_request_id"`
	SessionID          int64  `json:"session_id"`
	RunnerID           int64  `json:"runner_id"`
	Status             string `json:"status"`
	ProtectedViewerURL string `json:"protected_viewer_url"`
	ProxyViewerURL     string `json:"proxy_viewer_url"`
	UpstreamViewerURL  string `json:"upstream_viewer_url,omitempty"`
	AllowedActions     string `json:"allowed_actions"`
}

func handleBrowserSessionHandoffShow(writer http.ResponseWriter, request *http.Request, deps Dependencies) {
	if deps.Store == nil {
		writeAPIError(writer, http.StatusServiceUnavailable, "browser_handoff_unavailable", "browser session handoff store unavailable")
		return
	}
	handoffID := strings.TrimSpace(request.URL.Query().Get("handoff_id"))
	if handoffID == "" {
		writeAPIError(writer, http.StatusBadRequest, "browser_handoff_id_required", "handoff_id is required")
		return
	}
	handoff, err := deps.Store.GetBrowserSessionLoginHandoff(request.Context(), handoffID)
	if err != nil {
		statusCode, code := browserSessionHandoffErrorStatus(err)
		writeAPIError(writer, statusCode, code, err.Error())
		return
	}
	view, err := newBrowserSessionHandoffView(request.Context(), deps.Store, handoff)
	if err != nil {
		writeAPIError(writer, http.StatusServiceUnavailable, "browser_handoff_runner_unavailable", err.Error())
		return
	}
	if wantsBrowserSessionHandoffHTML(request) {
		writeBrowserSessionHandoffHTML(writer, view)
		return
	}
	writeJSON(writer, http.StatusOK, browserSessionHandoffResponse{Handoff: view})
}

func handleBrowserSessionHandoffViewer(writer http.ResponseWriter, request *http.Request, deps Dependencies) {
	if statusCode, ok := authorizeAdmin(request, deps.AdminToken); !ok {
		writeAdminAuthorizationError(writer, statusCode)
		return
	}
	if deps.Store == nil {
		writeAPIError(writer, http.StatusServiceUnavailable, "browser_handoff_viewer_unavailable", "browser session handoff store unavailable")
		return
	}
	handoffID := strings.TrimSpace(request.URL.Query().Get("handoff_id"))
	if handoffID == "" {
		writeAPIError(writer, http.StatusBadRequest, "browser_handoff_id_required", "handoff_id is required")
		return
	}
	handoff, err := deps.Store.GetBrowserSessionLoginHandoff(request.Context(), handoffID)
	if err != nil {
		statusCode, code := browserSessionHandoffErrorStatus(err)
		writeAPIError(writer, statusCode, code, err.Error())
		return
	}
	runner, ok := activeBrowserSessionHandoffViewerRunner(request.Context(), deps.Store, handoff)
	if !ok || runner.ViewerURL == nil || strings.TrimSpace(*runner.ViewerURL) == "" {
		writeAPIError(writer, http.StatusConflict, "browser_handoff_viewer_unavailable", "no active protected browser viewer is available for this handoff")
		return
	}
	view := browserSessionHandoffViewerView{
		HandoffID:          handoff.HandoffID,
		LoginRequestID:     handoff.LoginRequest.ID,
		SessionID:          handoff.Session.ID,
		RunnerID:           runner.ID,
		Status:             string(runner.Status),
		ProtectedViewerURL: protectedBrowserSessionHandoffViewerURL(handoff.HandoffID),
		ProxyViewerURL:     protectedBrowserSessionHandoffViewerProxyURL(handoff.HandoffID),
		UpstreamViewerURL:  strings.TrimSpace(*runner.ViewerURL),
		AllowedActions:     "manual_login_only",
	}
	if strings.EqualFold(strings.TrimSpace(request.URL.Query().Get("format")), "json") || strings.Contains(strings.ToLower(request.Header.Get("Accept")), "application/json") {
		writeJSON(writer, http.StatusOK, browserSessionHandoffViewerResponse{Viewer: view})
		return
	}
	setBrowserSessionViewerCookie(writer, handoff.HandoffID, deps.AdminToken, handoff.LoginRequest.ExpiresAt)
	writeBrowserSessionViewerHTML(writer, view)
}

func handleBrowserSessionHandoffViewerProxy(writer http.ResponseWriter, request *http.Request, deps Dependencies) {
	if deps.Store == nil {
		writeAPIError(writer, http.StatusServiceUnavailable, "browser_handoff_viewer_unavailable", "browser session handoff store unavailable")
		return
	}
	handoffID := strings.TrimSpace(request.URL.Query().Get("handoff_id"))
	if handoffID == "" {
		writeAPIError(writer, http.StatusBadRequest, "browser_handoff_id_required", "handoff_id is required")
		return
	}
	if statusCode, ok := authorizeAdmin(request, deps.AdminToken); !ok {
		if !validBrowserSessionViewerCookie(request, handoffID, deps.AdminToken) {
			writeAdminAuthorizationError(writer, statusCode)
			return
		}
	}
	handoff, err := deps.Store.GetBrowserSessionLoginHandoff(request.Context(), handoffID)
	if err != nil {
		statusCode, code := browserSessionHandoffErrorStatus(err)
		writeAPIError(writer, statusCode, code, err.Error())
		return
	}
	runner, ok := activeBrowserSessionHandoffViewerRunner(request.Context(), deps.Store, handoff)
	if !ok || runner.ViewerURL == nil || strings.TrimSpace(*runner.ViewerURL) == "" {
		writeAPIError(writer, http.StatusConflict, "browser_handoff_viewer_unavailable", "no active protected browser viewer is available for this handoff")
		return
	}
	upstream, err := url.Parse(strings.TrimSpace(*runner.ViewerURL))
	if err != nil || upstream.Scheme == "" || upstream.Host == "" {
		writeAPIError(writer, http.StatusConflict, "browser_handoff_viewer_invalid_upstream", "active browser viewer upstream is invalid")
		return
	}
	proxy := httputil.NewSingleHostReverseProxy(&url.URL{Scheme: upstream.Scheme, Host: upstream.Host})
	originalDirector := proxy.Director
	proxy.Director = func(outbound *http.Request) {
		originalDirector(outbound)
		outbound.URL.Scheme = upstream.Scheme
		outbound.URL.Host = upstream.Host
		outbound.URL.Path = joinBrowserSessionViewerProxyPath(upstream.EscapedPath(), strings.TrimPrefix(request.URL.EscapedPath(), "/browser/session/handoff/viewer/proxy"))
		outbound.URL.RawQuery = mergeBrowserSessionViewerProxyQuery(upstream.RawQuery, request.URL.Query())
		outbound.Host = upstream.Host
		outbound.Header.Del("Authorization")
		outbound.Header.Del("X-Odin-Admin-Token")
	}
	proxy.ModifyResponse = func(response *http.Response) error {
		if location := strings.TrimSpace(response.Header.Get("Location")); location != "" {
			if rewritten := rewriteBrowserSessionViewerLocation(location, upstream, handoffID); rewritten != "" {
				response.Header.Set("Location", rewritten)
			}
		}
		return nil
	}
	proxy.ServeHTTP(writer, request)
}

func handleBrowserSessionHandoffComplete(writer http.ResponseWriter, request *http.Request, deps Dependencies) {
	if statusCode, ok := authorizeAdmin(request, deps.AdminToken); !ok {
		writeAdminAuthorizationError(writer, statusCode)
		return
	}
	if deps.Store == nil {
		writeAPIError(writer, http.StatusServiceUnavailable, "browser_handoff_unavailable", "browser session handoff store unavailable")
		return
	}
	handoffID, formPost, err := parseBrowserSessionHandoffCompletionID(writer, request)
	if err != nil {
		writeAPIError(writer, http.StatusBadRequest, "browser_handoff_invalid_request", err.Error())
		return
	}
	if strings.TrimSpace(handoffID) == "" {
		writeAPIError(writer, http.StatusBadRequest, "browser_handoff_id_required", "handoff_id is required")
		return
	}
	handoff, err := deps.Store.GetBrowserSessionLoginHandoff(request.Context(), handoffID)
	if err != nil {
		statusCode, code := browserSessionHandoffErrorStatus(err)
		writeAPIError(writer, statusCode, code, err.Error())
		return
	}
	session, completed, err := deps.Store.VerifyBrowserSession(request.Context(), sqlite.VerifyBrowserSessionParams{
		SessionID:      handoff.Session.ID,
		LoginRequestID: handoff.LoginRequest.ID,
		Actor:          "operator",
		Reason:         "operator attested manual browser handoff completion",
	})
	if err != nil {
		statusCode, code := browserSessionHandoffErrorStatus(err)
		writeAPIError(writer, statusCode, code, err.Error())
		return
	}
	if completed == nil {
		writeAPIError(writer, http.StatusInternalServerError, "browser_handoff_completion_failed", "login request completion was not recorded")
		return
	}
	view := newBrowserSessionHandoffCompletionView(handoff.HandoffID, session, *completed)
	if formPost || wantsBrowserSessionHandoffHTML(request) {
		writeBrowserSessionHandoffCompletionHTML(writer, view)
		return
	}
	writeJSON(writer, http.StatusOK, browserSessionHandoffCompletionResponse{Completion: view})
}

func parseBrowserSessionHandoffCompletionID(writer http.ResponseWriter, request *http.Request) (string, bool, error) {
	contentType := strings.ToLower(request.Header.Get("Content-Type"))
	if strings.Contains(contentType, "application/json") {
		var payload struct {
			HandoffID string `json:"handoff_id"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 1<<20)).Decode(&payload); err != nil {
			return "", false, fmt.Errorf("invalid JSON handoff completion payload: %w", err)
		}
		return payload.HandoffID, false, nil
	}
	if err := request.ParseForm(); err != nil {
		return "", false, fmt.Errorf("invalid form handoff completion payload: %w", err)
	}
	return request.FormValue("handoff_id"), true, nil
}

func newBrowserSessionHandoffView(ctx context.Context, store *sqlite.Store, handoff sqlite.BrowserSessionLoginHandoff) (browserSessionHandoffView, error) {
	view := browserSessionHandoffView{
		HandoffID:      handoff.HandoffID,
		LoginRequestID: handoff.LoginRequest.ID,
		SessionID:      handoff.Session.ID,
		SessionName:    handoff.Session.Name,
		Domain:         handoff.Session.Domain,
		AccountHint:    handoff.Session.AccountHint,
		ExpiresAt:      handoff.LoginRequest.ExpiresAt.UTC().Format(time.RFC3339),
		Status:         string(handoff.LoginRequest.Status),
		AllowedActions: "manual_login_only",
	}
	if store == nil {
		return view, nil
	}
	runners, err := store.ListBrowserHandoffRunners(ctx, sqlite.ListBrowserHandoffRunnersParams{LoginRequestID: handoff.LoginRequest.ID})
	if err != nil {
		return browserSessionHandoffView{}, err
	}
	for index := len(runners) - 1; index >= 0; index-- {
		runner := runners[index]
		if runner.Status != sqlite.BrowserHandoffRunnerStatusStarted || runner.ViewerURL == nil || strings.TrimSpace(*runner.ViewerURL) == "" {
			continue
		}
		view.RunnerStatus = string(runner.Status)
		view.RunnerID = mobileOptionalString(runner.RunnerID)
		view.ViewerURL = strings.TrimSpace(*runner.ViewerURL)
		view.ProtectedViewerURL = protectedBrowserSessionHandoffViewerURL(handoff.HandoffID)
		view.RealBrowserEvidence = strings.HasPrefix(view.RunnerID, "novnc-real-")
		view.AllowedActions = "manual_login_via_protected_viewer"
		break
	}
	return view, nil
}

func activeBrowserSessionHandoffViewerRunner(ctx context.Context, store *sqlite.Store, handoff sqlite.BrowserSessionLoginHandoff) (sqlite.BrowserHandoffRunner, bool) {
	if store == nil || handoff.LoginRequest.ID <= 0 {
		return sqlite.BrowserHandoffRunner{}, false
	}
	runners, err := store.ListBrowserHandoffRunners(ctx, sqlite.ListBrowserHandoffRunnersParams{LoginRequestID: handoff.LoginRequest.ID})
	if err != nil {
		return sqlite.BrowserHandoffRunner{}, false
	}
	for index := len(runners) - 1; index >= 0; index-- {
		runner := runners[index]
		if runner.Status != sqlite.BrowserHandoffRunnerStatusStarted {
			continue
		}
		if runner.ViewerURL == nil || strings.TrimSpace(*runner.ViewerURL) == "" {
			continue
		}
		return runner, true
	}
	return sqlite.BrowserHandoffRunner{}, false
}

func protectedBrowserSessionHandoffViewerURL(handoffID string) string {
	return "/browser/session/handoff/viewer?handoff_id=" + url.QueryEscape(strings.TrimSpace(handoffID))
}

func protectedBrowserSessionHandoffViewerProxyURL(handoffID string) string {
	return "/browser/session/handoff/viewer/proxy/?handoff_id=" + url.QueryEscape(strings.TrimSpace(handoffID))
}

const browserSessionViewerCookieName = "odin_browser_handoff_viewer"

func setBrowserSessionViewerCookie(writer http.ResponseWriter, handoffID string, adminToken string, expiresAt time.Time) {
	expires := expiresAt.UTC()
	if expires.IsZero() || expires.After(time.Now().UTC().Add(10*time.Minute)) {
		expires = time.Now().UTC().Add(10 * time.Minute)
	}
	http.SetCookie(writer, &http.Cookie{
		Name:     browserSessionViewerCookieName,
		Value:    browserSessionViewerCookieValue(handoffID, adminToken),
		Path:     "/browser/session/handoff/viewer/proxy/",
		Expires:  expires,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
}

func validBrowserSessionViewerCookie(request *http.Request, handoffID string, adminToken string) bool {
	cookie, err := request.Cookie(browserSessionViewerCookieName)
	if err != nil {
		return false
	}
	expected := browserSessionViewerCookieValue(handoffID, adminToken)
	return subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(expected)) == 1
}

func browserSessionViewerCookieValue(handoffID string, adminToken string) string {
	handoffID = strings.TrimSpace(handoffID)
	mac := hmac.New(sha256.New, []byte(strings.TrimSpace(adminToken)))
	_, _ = mac.Write([]byte("browser-viewer:"))
	_, _ = mac.Write([]byte(handoffID))
	return handoffID + "." + hex.EncodeToString(mac.Sum(nil))
}

func joinBrowserSessionViewerProxyPath(upstreamBasePath string, proxySuffix string) string {
	proxySuffix = strings.TrimSpace(proxySuffix)
	if proxySuffix == "" || proxySuffix == "/" {
		return "/vnc.html"
	}
	return "/" + strings.TrimLeft(proxySuffix, "/")
}

func mergeBrowserSessionViewerProxyQuery(upstreamRawQuery string, query url.Values) string {
	merged := url.Values{}
	if upstreamRawQuery != "" {
		if parsed, err := url.ParseQuery(upstreamRawQuery); err == nil {
			for key, values := range parsed {
				for _, value := range values {
					merged.Add(key, value)
				}
			}
		}
	}
	for key, values := range query {
		if key == "handoff_id" || key == "format" {
			continue
		}
		for _, value := range values {
			merged.Add(key, value)
		}
	}
	return merged.Encode()
}

func rewriteBrowserSessionViewerLocation(location string, upstream *url.URL, handoffID string) string {
	parsed, err := url.Parse(location)
	if err != nil {
		return ""
	}
	if parsed.IsAbs() && !strings.EqualFold(parsed.Scheme, upstream.Scheme) {
		return ""
	}
	if parsed.IsAbs() && !strings.EqualFold(parsed.Host, upstream.Host) {
		return ""
	}
	relativePath := strings.TrimPrefix(parsed.EscapedPath(), strings.TrimRight(upstream.EscapedPath(), "/"))
	rewritten := protectedBrowserSessionHandoffViewerProxyURL(handoffID)
	if relativePath != "" && relativePath != "/" {
		rewritten = "/browser/session/handoff/viewer/proxy/" + strings.TrimLeft(relativePath, "/") + "?handoff_id=" + url.QueryEscape(strings.TrimSpace(handoffID))
	}
	if parsed.RawQuery != "" {
		separator := "&"
		if !strings.Contains(rewritten, "?") {
			separator = "?"
		}
		rewritten += separator + parsed.RawQuery
	}
	return rewritten
}

func newBrowserSessionHandoffCompletionView(handoffID string, session sqlite.BrowserSession, request sqlite.BrowserSessionLoginRequest) browserSessionHandoffCompletionView {
	return browserSessionHandoffCompletionView{
		HandoffID:          handoffID,
		LoginRequestID:     request.ID,
		SessionID:          session.ID,
		SessionName:        session.Name,
		Domain:             session.Domain,
		AccountHint:        session.AccountHint,
		SessionStatus:      string(session.Status),
		LoginRequestStatus: string(request.Status),
		CompletedAt:        formatOptionalBrowserSessionHandoffTime(request.CompletedAt),
		AllowedActions:     "manual_login_only",
	}
}

func formatOptionalBrowserSessionHandoffTime(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func browserSessionHandoffErrorStatus(err error) (int, string) {
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "handoff id is required"):
		return http.StatusBadRequest, "browser_handoff_id_required"
	case strings.Contains(message, "not found"):
		return http.StatusNotFound, "browser_handoff_not_found"
	case strings.Contains(message, "expired"):
		return http.StatusGone, "browser_handoff_expired"
	case strings.Contains(message, "revoked"):
		return http.StatusConflict, "browser_handoff_session_revoked"
	case strings.Contains(message, "status"):
		return http.StatusConflict, "browser_handoff_unavailable"
	default:
		return http.StatusBadRequest, "browser_handoff_invalid"
	}
}

func wantsBrowserSessionHandoffHTML(request *http.Request) bool {
	format := strings.ToLower(strings.TrimSpace(request.URL.Query().Get("format")))
	if format == "html" {
		return true
	}
	if format == "json" {
		return false
	}
	return strings.Contains(strings.ToLower(request.Header.Get("Accept")), "text/html")
}

var browserSessionHandoffHTMLTemplate = template.Must(template.New("browser_session_handoff").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Browser Login Handoff</title>
  <style>
    body { margin: 0; font-family: system-ui, sans-serif; color: #111827; background: #f8fafc; }
    main { max-width: 720px; margin: 48px auto; padding: 0 24px; }
    section { background: #ffffff; border: 1px solid #d1d5db; border-radius: 8px; padding: 24px; }
    h1 { margin: 0 0 16px; font-size: 1.5rem; }
    dl { display: grid; grid-template-columns: 160px 1fr; gap: 10px 16px; margin: 20px 0; }
    dt { color: #4b5563; font-weight: 600; }
    dd { margin: 0; overflow-wrap: anywhere; }
    .notice { border-left: 4px solid #2563eb; background: #eff6ff; padding: 12px 16px; }
  </style>
</head>
<body>
  <main>
    <section>
      <h1>Browser Login Handoff</h1>
      {{if .ProtectedViewerURL}}<p class="notice">A protected attended browser viewer is available. Odin is not collecting credentials. Login and 2FA stay manual in the visible browser.</p>{{else}}<p class="notice">No browser session is launched yet. Odin is not collecting credentials. Login and 2FA will be manual in a future handoff step.</p>{{end}}
      <dl>
        <dt>Session</dt><dd>{{.SessionName}}</dd>
        <dt>Domain</dt><dd>{{.Domain}}</dd>
        {{if .AccountHint}}<dt>Account hint</dt><dd>{{.AccountHint}}</dd>{{end}}
        {{if .ProtectedViewerURL}}<dt>Protected viewer</dt><dd><a href="{{.ProtectedViewerURL}}">{{.ProtectedViewerURL}}</a></dd>{{end}}
        {{if .RunnerID}}<dt>Runner</dt><dd>{{.RunnerID}}</dd>{{end}}
        <dt>Expires at</dt><dd>{{.ExpiresAt}}</dd>
        <dt>Status</dt><dd>{{.Status}}</dd>
        <dt>Allowed action</dt><dd>{{.AllowedActions}}</dd>
      </dl>
    </section>
  </main>
</body>
</html>
`))

var browserSessionHandoffCompletionHTMLTemplate = template.Must(template.New("browser_session_handoff_completion").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Browser Login Handoff Complete</title>
  <style>
    body { margin: 0; font-family: system-ui, sans-serif; color: #111827; background: #f8fafc; }
    main { max-width: 720px; margin: 48px auto; padding: 0 24px; }
    section { background: #ffffff; border: 1px solid #d1d5db; border-radius: 8px; padding: 24px; }
    h1 { margin: 0 0 16px; font-size: 1.5rem; }
    dl { display: grid; grid-template-columns: 180px 1fr; gap: 10px 16px; margin: 20px 0; }
    dt { color: #4b5563; font-weight: 600; }
    dd { margin: 0; overflow-wrap: anywhere; }
    .notice { border-left: 4px solid #16a34a; background: #f0fdf4; padding: 12px 16px; }
  </style>
</head>
<body>
  <main>
    <section>
      <h1>Browser Login Handoff Complete</h1>
      <p class="notice">Operator-attested completion only. No browser was launched by Odin. No credentials or profile bytes were collected.</p>
      <dl>
        <dt>Session</dt><dd>{{.SessionName}}</dd>
        <dt>Domain</dt><dd>{{.Domain}}</dd>
        {{if .AccountHint}}<dt>Account hint</dt><dd>{{.AccountHint}}</dd>{{end}}
        <dt>Session status</dt><dd>{{.SessionStatus}}</dd>
        <dt>Login request status</dt><dd>{{.LoginRequestStatus}}</dd>
        <dt>Completed at</dt><dd>{{.CompletedAt}}</dd>
        <dt>Allowed action</dt><dd>{{.AllowedActions}}</dd>
      </dl>
    </section>
  </main>
</body>
</html>
`))

var browserSessionViewerHTMLTemplate = template.Must(template.New("browser_session_viewer").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Protected Browser Viewer</title>
  <style>
    html, body { margin: 0; width: 100%; height: 100%; font-family: system-ui, sans-serif; color: #111827; background: #0f172a; }
    header { height: 48px; display: flex; align-items: center; gap: 16px; padding: 0 16px; color: #f8fafc; background: #111827; border-bottom: 1px solid #334155; }
    header strong { font-size: 0.95rem; }
    header span { color: #cbd5e1; font-size: 0.85rem; }
    iframe { width: 100%; height: calc(100% - 49px); border: 0; background: #000000; }
  </style>
</head>
<body>
  <header>
    <strong>Protected Browser Viewer</strong>
    <span>handoff {{.HandoffID}}</span>
    <span>{{.AllowedActions}}</span>
  </header>
  <iframe src="{{.ProxyViewerURL}}" title="Protected browser session"></iframe>
</body>
</html>
`))

func writeBrowserSessionHandoffHTML(writer http.ResponseWriter, view browserSessionHandoffView) {
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	writer.WriteHeader(http.StatusOK)
	_ = browserSessionHandoffHTMLTemplate.Execute(writer, view)
}

func writeBrowserSessionHandoffCompletionHTML(writer http.ResponseWriter, view browserSessionHandoffCompletionView) {
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	writer.WriteHeader(http.StatusOK)
	_ = browserSessionHandoffCompletionHTMLTemplate.Execute(writer, view)
}

func writeBrowserSessionViewerHTML(writer http.ResponseWriter, view browserSessionHandoffViewerView) {
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	writer.WriteHeader(http.StatusOK)
	_ = browserSessionViewerHTMLTemplate.Execute(writer, view)
}

type githubIssuesWebhookPayload struct {
	Action     string `json:"action"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
	Issue struct {
		Number  int    `json:"number"`
		Title   string `json:"title"`
		Body    string `json:"body"`
		HTMLURL string `json:"html_url"`
		Labels  []struct {
			Name string `json:"name"`
		} `json:"labels"`
	} `json:"issue"`
}

type githubIssuesWebhookResponse struct {
	DeliveryMode     string `json:"delivery_mode"`
	Verified         bool   `json:"verified"`
	Source           string `json:"source"`
	EventType        string `json:"event_type"`
	ExternalEventKey string `json:"external_event_key"`
	ProjectKey       string `json:"project_key"`
	Repo             string `json:"repo"`
	Number           int    `json:"number"`
	Action           string `json:"action"`
}

func handleGitHubIssuesWebhook(writer http.ResponseWriter, request *http.Request, deps Dependencies) {
	if deps.GitHubIssueIngester == nil {
		writeAPIError(writer, http.StatusServiceUnavailable, "github_webhook_unavailable", "github issue ingester unavailable")
		return
	}
	secret := strings.TrimSpace(deps.GitHubWebhookSecret)
	if secret == "" {
		writeAPIError(writer, http.StatusServiceUnavailable, "github_webhook_unconfigured", "github webhook secret is not configured")
		return
	}
	if !strings.EqualFold(strings.TrimSpace(request.Header.Get("X-GitHub-Event")), "issues") {
		writeAPIError(writer, http.StatusBadRequest, "unsupported_github_event", "only GitHub issues events are supported")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(writer, request.Body, 1<<20))
	if err != nil {
		writeAPIError(writer, http.StatusBadRequest, "invalid_webhook_body", err.Error())
		return
	}
	if !validGitHubWebhookSignature(body, secret, request.Header.Get("X-Hub-Signature-256")) {
		writeAPIError(writer, http.StatusUnauthorized, "invalid_github_signature", "GitHub webhook signature verification failed")
		return
	}
	var payload githubIssuesWebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		writeAPIError(writer, http.StatusBadRequest, "invalid_github_payload", err.Error())
		return
	}
	labels := make([]string, 0, len(payload.Issue.Labels))
	for _, label := range payload.Issue.Labels {
		if name := strings.TrimSpace(label.Name); name != "" {
			labels = append(labels, name)
		}
	}
	result, err := deps.GitHubIssueIngester.IngestGitHubIssue(request.Context(), triggers.GitHubIssueIngestParams{
		ProjectKey: strings.TrimSpace(request.URL.Query().Get("project")),
		Repo:       payload.Repository.FullName,
		Number:     payload.Issue.Number,
		Action:     payload.Action,
		Title:      payload.Issue.Title,
		Body:       payload.Issue.Body,
		URL:        payload.Issue.HTMLURL,
		Labels:     strings.Join(labels, ","),
	})
	if err != nil {
		writeAPIError(writer, http.StatusBadRequest, "github_issue_ingest_failed", err.Error())
		return
	}
	writeJSON(writer, http.StatusAccepted, githubIssuesWebhookResponse{
		DeliveryMode:     "github_webhook",
		Verified:         true,
		Source:           result.Source,
		EventType:        result.EventType,
		ExternalEventKey: result.ExternalEventKey,
		ProjectKey:       result.ProjectKey,
		Repo:             result.Issue.Repo,
		Number:           result.Issue.Number,
		Action:           result.Action,
	})
}

func validGitHubWebhookSignature(body []byte, secret string, signature string) bool {
	signature = strings.TrimSpace(signature)
	signature = strings.TrimPrefix(signature, "sha256=")
	if signature == "" {
		return false
	}
	decoded, err := hex.DecodeString(signature)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	return hmac.Equal(decoded, mac.Sum(nil))
}

type dashboardStatus struct {
	GeneratedAt    string                         `json:"generated_at"`
	HealthStatus   string                         `json:"health_status"`
	Ready          bool                           `json:"ready"`
	Runtime        runtimeStatus                  `json:"runtime"`
	WorkerDispatch healthsvc.WorkerDispatchStatus `json:"worker_dispatch"`
	Counts         dashboardCounts                `json:"counts"`
	Tmux           TmuxStatus                     `json:"tmux"`
}

type runtimeStatus struct {
	Status          string `json:"status"`
	BootID          string `json:"boot_id,omitempty"`
	LastHeartbeatAt string `json:"last_heartbeat_at,omitempty"`
	LastError       string `json:"last_error,omitempty"`
}

type dashboardCounts struct {
	WorkItems                 int `json:"work_items"`
	OpenWorkItems             int `json:"open_work_items"`
	ActiveRunAttempts         int `json:"active_run_attempts"`
	PendingApprovals          int `json:"pending_approvals"`
	ReviewQueueItems          int `json:"review_queue_items"`
	BlockedWorkItems          int `json:"blocked_work_items"`
	FailedWorkItems           int `json:"failed_work_items"`
	RecoveryRecommendations   int `json:"recovery_recommendations"`
	IntakeReviewItems         int `json:"intake_review_items"`
	KnowledgeReviewItems      int `json:"knowledge_review_items"`
	SkillArtifactReviewItems  int `json:"skill_artifact_review_items"`
	AutomationTriggers        int `json:"automation_triggers"`
	EnabledAutomationTriggers int `json:"enabled_automation_triggers"`
	DeliveryProfiles          int `json:"delivery_profiles"`
	ExplicitIntentWorkItems   int `json:"explicit_intent_work_items"`
	FallbackIntentWorkItems   int `json:"fallback_intent_work_items"`
	ActionRequiredItems       int `json:"action_required_items"`
}

type dashboardIssue struct {
	ID           int64    `json:"id"`
	ProjectID    int64    `json:"project_id"`
	Provider     string   `json:"provider"`
	Repo         string   `json:"repo"`
	Number       int      `json:"number"`
	Title        string   `json:"title"`
	URL          string   `json:"url"`
	State        string   `json:"state"`
	Labels       []string `json:"labels"`
	SyncStatus   string   `json:"sync_status"`
	LastSyncedAt string   `json:"last_synced_at"`
}

type dashboardRun struct {
	ID             int64   `json:"id"`
	TaskID         int64   `json:"task_id"`
	Executor       string  `json:"executor"`
	Status         string  `json:"status"`
	Attempt        int     `json:"attempt"`
	StartedAt      string  `json:"started_at"`
	FinishedAt     *string `json:"finished_at,omitempty"`
	Summary        string  `json:"summary"`
	TerminalReason string  `json:"terminal_reason"`
	ArtifactsJSON  string  `json:"artifacts_json"`
}

const (
	dashboardDefaultLimit = 100
	dashboardMaxLimit     = 500
)

type dashboardListOptions struct {
	Limit  int
	Offset int
}

type dashboardIssueFilters struct {
	Provider   string
	Repo       string
	State      string
	SyncStatus string
}

type dashboardRunFilters struct {
	Status   string
	Executor string
	TaskID   *int64
}

func buildStatusPayload(ctx context.Context, deps Dependencies, now func() time.Time) (dashboardStatus, error) {
	report, err := deps.Health.Doctor(ctx, deps.RegistryHealthy)
	if err != nil {
		return dashboardStatus{}, err
	}
	_, ready, err := deps.Health.Readiness(ctx, deps.RegistryHealthy)
	if err != nil {
		return dashboardStatus{}, err
	}

	status := dashboardStatus{
		GeneratedAt:  now().UTC().Format(time.RFC3339),
		HealthStatus: string(report.Status),
		Ready:        ready,
		Runtime:      runtimeStatus{Status: "unknown"},
		Tmux:         TmuxStatus{Available: false, Source: "not_configured"},
	}

	if deps.ReadModels != nil {
		runtimeState, err := getRuntimeStatus(ctx, deps.ReadModels)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return dashboardStatus{}, err
		}
		if err == nil {
			status.Runtime = runtimeState
		}

		actualUse, err := projections.GetActualUseSummaryView(ctx, deps.ReadModels, workspaces.DefaultWorkspaceKey)
		if err != nil {
			return dashboardStatus{}, err
		}
		status.Counts = dashboardCounts{
			WorkItems:                 actualUse.WorkItems,
			OpenWorkItems:             actualUse.OpenWorkItems,
			ActiveRunAttempts:         actualUse.ActiveRunAttempts,
			PendingApprovals:          actualUse.PendingApprovals,
			ReviewQueueItems:          actualUse.ReviewQueueItems,
			BlockedWorkItems:          actualUse.BlockedWorkItems,
			FailedWorkItems:           actualUse.FailedWorkItems,
			RecoveryRecommendations:   actualUse.RecoveryRecommendations,
			IntakeReviewItems:         actualUse.IntakeReviewItems,
			KnowledgeReviewItems:      actualUse.KnowledgeReviewItems,
			SkillArtifactReviewItems:  actualUse.SkillArtifactReviewItems,
			AutomationTriggers:        actualUse.AutomationTriggers,
			EnabledAutomationTriggers: actualUse.EnabledAutomationTriggers,
			DeliveryProfiles:          countDeliveryProfiles(deps.RegistrySnapshot),
			ExplicitIntentWorkItems:   actualUse.ExplicitIntentWorkItems,
			FallbackIntentWorkItems:   actualUse.FallbackIntentWorkItems,
			ActionRequiredItems:       actualUse.ActionRequiredItems,
		}
	}

	if deps.Tmux != nil {
		tmuxStatus, err := deps.Tmux.Status(ctx)
		if err != nil {
			status.Tmux = TmuxStatus{Available: false, Source: "tmux", Error: err.Error()}
		} else {
			status.Tmux = tmuxStatus
		}
	}

	status.WorkerDispatch = healthsvc.NewWorkerDispatchStatus(ready, status.Runtime.Status, report.Status)

	return status, nil
}

func countDeliveryProfiles(snapshot registry.Snapshot) int {
	count := 0
	for _, item := range snapshot.Items {
		if item.Kind != registry.KindWorkflow {
			continue
		}
		for _, tag := range item.Tags {
			if strings.EqualFold(strings.TrimSpace(tag), "delivery_profile") {
				count++
				break
			}
		}
	}
	return count
}

func getRuntimeStatus(ctx context.Context, queryer projections.Queryer) (runtimeStatus, error) {
	rows, err := queryer.QueryContext(ctx, `
		SELECT boot_id, status, last_heartbeat_at, last_error
		FROM runtime_state
		ORDER BY updated_at DESC
		LIMIT 1
	`)
	if err != nil {
		return runtimeStatus{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		return runtimeStatus{}, sql.ErrNoRows
	}
	var status runtimeStatus
	var heartbeat sql.NullString
	var lastError sql.NullString
	if err := rows.Scan(&status.BootID, &status.Status, &heartbeat, &lastError); err != nil {
		return runtimeStatus{}, err
	}
	if heartbeat.Valid {
		status.LastHeartbeatAt = heartbeat.String
	}
	if lastError.Valid {
		status.LastError = lastError.String
	}
	return status, rows.Err()
}

func parseDashboardListOptions(request *http.Request) (dashboardListOptions, error) {
	values := request.URL.Query()
	options := dashboardListOptions{Limit: dashboardDefaultLimit}
	if rawLimit := strings.TrimSpace(values.Get("limit")); rawLimit != "" {
		limit, err := strconv.Atoi(rawLimit)
		if err != nil || limit <= 0 {
			return dashboardListOptions{}, fmt.Errorf("limit must be a positive integer")
		}
		if limit > dashboardMaxLimit {
			return dashboardListOptions{}, fmt.Errorf("limit must be less than or equal to %d", dashboardMaxLimit)
		}
		options.Limit = limit
	}
	if rawOffset := strings.TrimSpace(values.Get("offset")); rawOffset != "" {
		offset, err := strconv.Atoi(rawOffset)
		if err != nil || offset < 0 {
			return dashboardListOptions{}, fmt.Errorf("offset must be a non-negative integer")
		}
		options.Offset = offset
	}
	return options, nil
}

func listDashboardIssues(ctx context.Context, queryer projections.Queryer, options dashboardListOptions, filters dashboardIssueFilters) ([]dashboardIssue, error) {
	var query strings.Builder
	query.WriteString(`
		SELECT id, project_id, provider, repo, number, title, url, state, labels_json, sync_status, last_synced_at
		FROM external_issues
	`)

	clauses := make([]string, 0, 4)
	args := make([]any, 0, 6)
	if provider := strings.TrimSpace(filters.Provider); provider != "" {
		clauses = append(clauses, "provider = ?")
		args = append(args, provider)
	}
	if repo := strings.TrimSpace(filters.Repo); repo != "" {
		clauses = append(clauses, "repo = ?")
		args = append(args, repo)
	}
	if state := strings.TrimSpace(filters.State); state != "" {
		clauses = append(clauses, "state = ?")
		args = append(args, state)
	}
	if syncStatus := strings.TrimSpace(filters.SyncStatus); syncStatus != "" {
		clauses = append(clauses, "sync_status = ?")
		args = append(args, syncStatus)
	}
	if len(clauses) > 0 {
		query.WriteString(" WHERE ")
		query.WriteString(strings.Join(clauses, " AND "))
	}
	query.WriteString(" ORDER BY repo ASC, number ASC, id ASC LIMIT ? OFFSET ?")
	args = append(args, options.Limit, options.Offset)

	rows, err := queryer.QueryContext(ctx, query.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	issues := make([]dashboardIssue, 0)
	for rows.Next() {
		var issue dashboardIssue
		var labelsJSON string
		if err := rows.Scan(
			&issue.ID,
			&issue.ProjectID,
			&issue.Provider,
			&issue.Repo,
			&issue.Number,
			&issue.Title,
			&issue.URL,
			&issue.State,
			&labelsJSON,
			&issue.SyncStatus,
			&issue.LastSyncedAt,
		); err != nil {
			return nil, err
		}
		issue.Labels = parseStringList(labelsJSON)
		issues = append(issues, issue)
	}
	return issues, rows.Err()
}

func listDashboardRuns(ctx context.Context, queryer projections.Queryer, options dashboardListOptions, filters dashboardRunFilters) ([]projections.RunSummaryView, error) {
	var query strings.Builder
	query.WriteString(`
		SELECT
			r.id,
			r.task_id,
			t.key,
			r.executor,
			r.status,
			r.attempt,
			r.started_at,
			r.finished_at
		FROM runs r
		JOIN tasks t ON t.id = r.task_id
	`)

	clauses := make([]string, 0, 3)
	args := make([]any, 0, 5)
	if status := strings.TrimSpace(filters.Status); status != "" {
		clauses = append(clauses, "r.status = ?")
		args = append(args, status)
	}
	if executor := strings.TrimSpace(filters.Executor); executor != "" {
		clauses = append(clauses, "r.executor = ?")
		args = append(args, executor)
	}
	if filters.TaskID != nil {
		clauses = append(clauses, "r.task_id = ?")
		args = append(args, *filters.TaskID)
	}
	if len(clauses) > 0 {
		query.WriteString(" WHERE ")
		query.WriteString(strings.Join(clauses, " AND "))
	}
	query.WriteString(" ORDER BY r.id ASC LIMIT ? OFFSET ?")
	args = append(args, options.Limit, options.Offset)

	rows, err := queryer.QueryContext(ctx, query.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var views []projections.RunSummaryView
	for rows.Next() {
		var view projections.RunSummaryView
		var finishedAt sql.NullString
		if err := rows.Scan(
			&view.RunID,
			&view.TaskID,
			&view.TaskKey,
			&view.Executor,
			&view.Status,
			&view.Attempt,
			&view.StartedAt,
			&finishedAt,
		); err != nil {
			return nil, err
		}
		if finishedAt.Valid {
			view.FinishedAt = &finishedAt.String
		}
		views = append(views, view)
	}
	return views, rows.Err()
}

func getDashboardRun(ctx context.Context, queryer projections.Queryer, runID int64) (dashboardRun, error) {
	rows, err := queryer.QueryContext(ctx, `
		SELECT id, task_id, executor, status, attempt, started_at, finished_at, summary, terminal_reason, artifacts_json
		FROM runs
		WHERE id = ?
	`, runID)
	if err != nil {
		return dashboardRun{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		return dashboardRun{}, sql.ErrNoRows
	}

	var run dashboardRun
	var finishedAt sql.NullString
	if err := rows.Scan(
		&run.ID,
		&run.TaskID,
		&run.Executor,
		&run.Status,
		&run.Attempt,
		&run.StartedAt,
		&finishedAt,
		&run.Summary,
		&run.TerminalReason,
		&run.ArtifactsJSON,
	); err != nil {
		return dashboardRun{}, err
	}
	if finishedAt.Valid {
		run.FinishedAt = &finishedAt.String
	}
	return run, rows.Err()
}

func parseStringList(raw string) []string {
	var values []string
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return nil
	}
	return values
}

func writeAdminAuthorizationError(writer http.ResponseWriter, statusCode int) {
	switch statusCode {
	case http.StatusServiceUnavailable:
		writeAPIError(writer, statusCode, "admin_disabled", "admin actions are disabled")
	case http.StatusUnauthorized:
		writeAPIError(writer, statusCode, "admin_auth_required", "admin authentication is required")
	default:
		writeAPIError(writer, statusCode, "admin_auth_failed", "admin authentication failed")
	}
}

func handleAdminAction(writer http.ResponseWriter, request *http.Request, deps Dependencies, action string, call func(context.Context, AdminActions) error) {
	if statusCode, ok := authorizeAdmin(request, deps.AdminToken); !ok {
		writeAdminAuthorizationError(writer, statusCode)
		return
	}
	if deps.Admin == nil {
		writeAPIError(writer, http.StatusServiceUnavailable, "admin_unavailable", "admin actions are unavailable")
		return
	}
	if err := call(request.Context(), deps.Admin); err != nil {
		if errors.Is(err, ErrAdminActionNotImplemented) {
			writeAPIError(writer, http.StatusNotImplemented, "admin_action_not_implemented", err.Error())
			return
		}
		if errors.Is(err, ErrAdminTargetNotFound) {
			writeAPIError(writer, http.StatusNotFound, "admin_target_not_found", err.Error())
			return
		}
		if errors.Is(err, ErrAdminActionConflict) {
			writeAPIError(writer, http.StatusConflict, "admin_action_conflict", err.Error())
			return
		}
		writeAPIError(writer, http.StatusServiceUnavailable, "admin_action_failed", err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, map[string]string{
		"status": "accepted",
		"action": action,
	})
}

func authorizeAdmin(request *http.Request, adminToken string) (int, bool) {
	adminToken = strings.TrimSpace(adminToken)
	if adminToken == "" {
		return http.StatusServiceUnavailable, false
	}

	token := strings.TrimSpace(request.Header.Get("X-Odin-Admin-Token"))
	if token == "" {
		const prefix = "Bearer "
		authorization := strings.TrimSpace(request.Header.Get("Authorization"))
		if strings.HasPrefix(authorization, prefix) {
			token = strings.TrimSpace(strings.TrimPrefix(authorization, prefix))
		}
	}
	if token == "" {
		return http.StatusUnauthorized, false
	}
	if subtle.ConstantTimeCompare([]byte(token), []byte(adminToken)) != 1 {
		return http.StatusForbidden, false
	}
	return http.StatusOK, true
}

func writeJSON(writer http.ResponseWriter, statusCode int, payload any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(statusCode)
	_ = json.NewEncoder(writer).Encode(payload)
}
