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

func TestOperationalHandlerRejectsReadyWhenOnlyOtherExecutorIsHealthy(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openStore(t)
	defer store.Close()

	if _, err := store.RecordRegistryVersion(ctx, sqlite.RecordRegistryVersionParams{
		Source:      "registry",
		VersionHash: "phase-15",
		Notes:       "healthy test sample",
	}); err != nil {
		t.Fatalf("RecordRegistryVersion() error = %v", err)
	}
	if _, err := store.RecordExecutorHealth(ctx, sqlite.RecordExecutorHealthParams{
		Executor:    "openai_api",
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

	server := httptest.NewServer(httpapi.NewOperationalHandler(httpapi.Dependencies{
		Health: healthsvc.Service{
			DB:                store.DB(),
			ExpectedExecutors: []string{"codex_headless"},
		},
		Metrics: metricsvc.Service{
			DB: store.DB(),
		},
		RegistryHealthy: true,
	}))
	defer server.Close()

	assertReportStatus(t, server.URL+"/healthz", http.StatusOK, "degraded")
	assertReportStatus(t, server.URL+"/readyz", http.StatusServiceUnavailable, "degraded")
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
