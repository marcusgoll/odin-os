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

func TestOperationalHandlerExposesWorkspaceMemoryView(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openStore(t)
	defer store.Close()

	seedHealthyObservability(t, ctx, store)
	seedOperationalMemoryFixture(t, ctx, store)

	server := httptest.NewServer(httpapi.NewOperationalHandler(httpapi.Dependencies{
		Health: healthsvc.Service{DB: store.DB()},
		Metrics: metricsvc.Service{
			DB: store.DB(),
		},
		RegistryHealthy: true,
	}))
	defer server.Close()

	response, err := http.Get(server.URL + "/memoryz")
	if err != nil {
		t.Fatalf("GET /memoryz error = %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("/memoryz status = %d, want %d", response.StatusCode, http.StatusOK)
	}

	var report struct {
		Workspace []struct {
			WorkspaceKey          string `json:"workspace_key"`
			WorkspaceSummaryCount int    `json:"workspace_summary_count"`
		} `json:"workspace"`
		Initiatives []struct {
			InitiativeKey string `json:"initiative_key"`
			SummaryCount  int    `json:"summary_count"`
		} `json:"initiatives"`
		Companions []struct {
			CompanionKey string `json:"companion_key"`
			SummaryCount int    `json:"summary_count"`
		} `json:"companions"`
	}
	if err := json.NewDecoder(response.Body).Decode(&report); err != nil {
		t.Fatalf("Decode(/memoryz) error = %v", err)
	}

	if len(report.Workspace) != 1 || report.Workspace[0].WorkspaceKey != "marcus" || report.Workspace[0].WorkspaceSummaryCount != 1 {
		t.Fatalf("workspace report = %+v, want marcus workspace summary count", report.Workspace)
	}
	if len(report.Initiatives) != 1 || report.Initiatives[0].InitiativeKey != "alpha-initiative" || report.Initiatives[0].SummaryCount != 1 {
		t.Fatalf("initiative report = %+v, want alpha-initiative summary count", report.Initiatives)
	}
	if len(report.Companions) != 1 || report.Companions[0].CompanionKey != "strategist" || report.Companions[0].SummaryCount != 1 {
		t.Fatalf("companion report = %+v, want strategist summary count", report.Companions)
	}
}

func TestOperationalMemoryViewFiltersToMarcusWorkspace(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openStore(t)
	defer store.Close()

	seedHealthyObservability(t, ctx, store)
	seedOperationalMemoryFixture(t, ctx, store)
	seedSecondaryOperationalMemoryFixture(t, ctx, store)

	server := httptest.NewServer(httpapi.NewOperationalHandler(httpapi.Dependencies{
		Health: healthsvc.Service{DB: store.DB()},
		Metrics: metricsvc.Service{
			DB: store.DB(),
		},
		RegistryHealthy: true,
	}))
	defer server.Close()

	response, err := http.Get(server.URL + "/memoryz")
	if err != nil {
		t.Fatalf("GET /memoryz error = %v", err)
	}
	defer response.Body.Close()

	var report struct {
		Workspace []struct {
			WorkspaceKey string `json:"workspace_key"`
		} `json:"workspace"`
		Initiatives []struct {
			InitiativeKey string `json:"initiative_key"`
		} `json:"initiatives"`
		Companions []struct {
			CompanionKey string `json:"companion_key"`
		} `json:"companions"`
	}
	if err := json.NewDecoder(response.Body).Decode(&report); err != nil {
		t.Fatalf("Decode(/memoryz) error = %v", err)
	}

	if len(report.Workspace) != 1 || report.Workspace[0].WorkspaceKey != "marcus" {
		t.Fatalf("workspace report = %+v, want only marcus", report.Workspace)
	}
	if len(report.Initiatives) != 1 || report.Initiatives[0].InitiativeKey != "alpha-initiative" {
		t.Fatalf("initiative report = %+v, want only alpha-initiative", report.Initiatives)
	}
	if len(report.Companions) != 1 || report.Companions[0].CompanionKey != "strategist" {
		t.Fatalf("companion report = %+v, want only strategist", report.Companions)
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

func seedOperationalMemoryFixture(t *testing.T, ctx context.Context, store *sqlite.Store) {
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

func seedSecondaryOperationalMemoryFixture(t *testing.T, ctx context.Context, store *sqlite.Store) {
	t.Helper()

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
		Summary:     "Secondary summary",
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
