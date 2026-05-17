package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestRunContinuousModeRefreshesUntilContextCanceled(t *testing.T) {
	t.Parallel()

	prometheus := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writePrometheusQueryResponse(t, w, r.URL.Query().Get("query"))
	}))
	defer prometheus.Close()
	loki := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "success",
			"data": map[string]any{
				"result": []any{},
			},
		})
	}))
	defer loki.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	writer := &cancelAfterWritesWriter{cancel: cancel, after: 2}

	err := Run(ctx, []string{
		"--interval", "1ms",
		"--no-clear",
		"--prometheus-url", prometheus.URL,
		"--loki-url", loki.URL,
	}, writer)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	output := writer.String()
	if count := strings.Count(output, "ODIN OBSERVABILITY"); count < 2 {
		t.Fatalf("Run() rendered %d frame(s), want at least 2:\n%s", count, output)
	}
	if strings.Contains(output, "\x1b[2J") {
		t.Fatalf("Run() output contains clear-screen escape despite --no-clear:\n%q", output)
	}
}

func TestRunContinuousModeUsesAlternateScreenByDefault(t *testing.T) {
	t.Parallel()

	prometheus := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writePrometheusQueryResponse(t, w, r.URL.Query().Get("query"))
	}))
	defer prometheus.Close()
	loki := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "success",
			"data": map[string]any{
				"result": []any{},
			},
		})
	}))
	defer loki.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	writer := &cancelAfterWritesWriter{cancel: cancel, after: 1}

	err := Run(ctx, []string{
		"--interval", "1ms",
		"--prometheus-url", prometheus.URL,
		"--loki-url", loki.URL,
	}, writer)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	output := writer.String()
	if !strings.HasPrefix(output, enterAlternateScreen+clearScreen) {
		t.Fatalf("Run() output = %q, want alternate-screen and clear-screen prefix", output)
	}
	if !strings.HasSuffix(output, exitAlternateScreen) {
		t.Fatalf("Run() output = %q, want alternate-screen restore suffix", output)
	}
}

func TestClientQueryOverviewUsesPrometheusInstantQueries(t *testing.T) {
	t.Parallel()

	seen := map[string]bool{}
	prometheus := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/query" {
			t.Fatalf("path = %s, want /api/v1/query", r.URL.Path)
		}
		query := r.URL.Query().Get("query")
		seen[query] = true
		writePrometheusQueryResponse(t, w, query)
	}))
	defer prometheus.Close()

	client := Client{PrometheusURL: prometheus.URL}
	model, err := client.QueryOverview(context.Background())
	if err != nil {
		t.Fatalf("QueryOverview() error = %v", err)
	}
	if model.HealthScore != 87 || model.Status != "degraded" || model.LifecyclePhase != "run" {
		t.Fatalf("model = %+v, want health=87 status=degraded lifecycle=run", model)
	}
	if model.TelemetryStale {
		t.Fatalf("model.TelemetryStale = true, want false")
	}
	if model.ActiveRuns != 3 {
		t.Fatalf("model.ActiveRuns = %d, want 3", model.ActiveRuns)
	}
	if model.BlockedItems != 2 || model.ApprovalsWaiting != 4 || model.ReviewQueueItems != 6 || model.FailedWorkItems != 1 || model.RecoveryRecommendations != 1 {
		t.Fatalf("model action-required counts = %+v, want blocked=2 approvals=4 review=6 failed=1 recovery=1", model)
	}
	for _, want := range []string{
		"odin_os_health_score",
		"odin_os_telemetry_stale",
		"odin_os_status",
		"odin_os_lifecycle_phase",
		"odin_active_runs",
		"odin_blocked_items",
		"odin_approvals_waiting",
		"odin_review_queue_items",
		"odin_failed_work_items",
		"odin_recovery_recommendations",
	} {
		if !seen[want] {
			t.Fatalf("prometheus query %q was not issued; seen=%v", want, seen)
		}
	}
}

func TestClientQueryOverviewUsesExportedStatusMetric(t *testing.T) {
	t.Parallel()

	prometheus := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("query")
		var result []any
		switch query {
		case "odin_os_health_score":
			result = []any{prometheusSampleFixture(nil, "69")}
		case "odin_os_telemetry_stale":
			result = []any{prometheusSampleFixture(nil, "0")}
		case "odin_os_status":
			result = []any{
				prometheusSampleFixture(map[string]string{"status": "healthy"}, "0"),
				prometheusSampleFixture(map[string]string{"status": "critical"}, "1"),
			}
		case "odin_os_lifecycle_phase":
			result = []any{prometheusSampleFixture(map[string]string{"phase": "run"}, "1")}
		case "odin_active_runs", "odin_blocked_items", "odin_approvals_waiting", "odin_review_queue_items", "odin_failed_work_items", "odin_recovery_recommendations":
			result = []any{prometheusSampleFixture(nil, "0")}
		default:
			t.Fatalf("unexpected query %q", query)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "success",
			"data": map[string]any{
				"resultType": "vector",
				"result":     result,
			},
		})
	}))
	defer prometheus.Close()

	client := Client{PrometheusURL: prometheus.URL}
	model, err := client.QueryOverview(context.Background())
	if err != nil {
		t.Fatalf("QueryOverview() error = %v", err)
	}
	if model.Status != "critical" {
		t.Fatalf("model.Status = %q, want exported critical status", model.Status)
	}
}

func TestClientQueryOverviewMissingMetricReturnsUnavailableTelemetry(t *testing.T) {
	t.Parallel()

	prometheus := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "success",
			"data": map[string]any{
				"resultType": "vector",
				"result":     []any{},
			},
		})
	}))
	defer prometheus.Close()

	client := Client{PrometheusURL: prometheus.URL}
	_, err := client.QueryOverview(context.Background())
	if !errors.Is(err, ErrUnavailableTelemetry) {
		t.Fatalf("QueryOverview() error = %v, want ErrUnavailableTelemetry", err)
	}
}

func TestClientQueryRecentLogsHandlesEmptyAndUnavailableLokiHonestly(t *testing.T) {
	t.Parallel()

	loki := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/loki/api/v1/query_range" {
			t.Fatalf("path = %s, want /loki/api/v1/query_range", r.URL.Path)
		}
		if query := r.URL.Query().Get("query"); query != recentLogsQuery {
			t.Fatalf("loki query = %q, want %q", query, recentLogsQuery)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "success",
			"data": map[string]any{
				"result": []any{},
			},
		})
	}))
	defer loki.Close()

	client := Client{LokiURL: loki.URL}
	logs, err := client.QueryRecentLogs(context.Background())
	if err != nil {
		t.Fatalf("QueryRecentLogs() error = %v", err)
	}
	if len(logs) != 0 {
		t.Fatalf("logs = %+v, want empty", logs)
	}

	client = Client{LokiURL: "http://127.0.0.1:1"}
	_, err = client.QueryRecentLogs(context.Background())
	if !errors.Is(err, ErrUnavailableTelemetry) {
		t.Fatalf("QueryRecentLogs() error = %v, want ErrUnavailableTelemetry", err)
	}
}

func TestClientQueryRecentLogsParsesLokiStreams(t *testing.T) {
	t.Parallel()

	loki := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "success",
			"data": map[string]any{
				"result": []any{
					map[string]any{
						"stream": map[string]string{"job": "docker-containers"},
						"values": [][]string{{"1714521600000000000", `{"level":"info","message":"ready"}`}},
					},
					map[string]any{
						"stream": map[string]string{"job": "docker-containers"},
						"values": [][]string{{"1714521600000000001", `level=info caller=metrics.go component=querier query="{job=\"docker-containers\"} |= \"GET /\""`}},
					},
				},
			},
		})
	}))
	defer loki.Close()

	client := Client{LokiURL: loki.URL}
	logs, err := client.QueryRecentLogs(context.Background())
	if err != nil {
		t.Fatalf("QueryRecentLogs() error = %v", err)
	}
	if len(logs) != 1 || !strings.Contains(logs[0].Line, "ready") || logs[0].Labels["job"] != "docker-containers" {
		t.Fatalf("logs = %+v, want parsed odin log entry", logs)
	}
}

func TestRunWithProviderEnrichesRenderedFrame(t *testing.T) {
	t.Parallel()

	prometheus := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writePrometheusQueryResponse(t, w, r.URL.Query().Get("query"))
	}))
	defer prometheus.Close()
	loki := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "success",
			"data": map[string]any{
				"result": []any{},
			},
		})
	}))
	defer loki.Close()

	var stdout strings.Builder
	err := RunWithProvider(context.Background(), []string{
		"--once",
		"--prometheus-url", prometheus.URL,
		"--loki-url", loki.URL,
	}, &stdout, providerFunc(func(_ context.Context, model *Model) error {
		model.Name = "Odin Core"
		model.Agents = []AgentRow{{Name: "codex", Task: "goal-7", Project: "odin-os", Status: "running"}}
		return nil
	}))
	if err != nil {
		t.Fatalf("RunWithProvider() error = %v", err)
	}
	for _, want := range []string{
		"│ NAME          Odin Core",
		"│ codex task=goal-7 project=odin-os status=running",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
}

func TestRunWithProviderRendersWhenTelemetryIsUnavailable(t *testing.T) {
	t.Parallel()

	var stdout strings.Builder
	err := RunWithProvider(context.Background(), []string{
		"--once",
		"--prometheus-url", "http://127.0.0.1:1",
		"--loki-url", "http://127.0.0.1:1",
	}, &stdout, providerFunc(func(_ context.Context, model *Model) error {
		model.Name = "Odin Core"
		model.Goals = []GoalRow{{ID: 7, Title: "Keep overview visible", Status: "running"}}
		return nil
	}))
	if err != nil {
		t.Fatalf("RunWithProvider() error = %v", err)
	}
	for _, want := range []string{
		"│ NAME          Odin Core",
		"│ HEALTH        UNKNOWN",
		"│ SCORE         unknown",
		"│ TELEMETRY     unavailable",
		"│ goal=7 status=running run=none title=Keep overview visible",
		"unavailable: unavailable telemetry",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
}

func TestRunWithProviderReturnsProviderError(t *testing.T) {
	t.Parallel()

	prometheus := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writePrometheusQueryResponse(t, w, r.URL.Query().Get("query"))
	}))
	defer prometheus.Close()
	loki := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "success",
			"data": map[string]any{
				"result": []any{},
			},
		})
	}))
	defer loki.Close()

	wantErr := fmt.Errorf("provider failed")
	var stdout strings.Builder
	err := RunWithProvider(context.Background(), []string{
		"--once",
		"--prometheus-url", prometheus.URL,
		"--loki-url", loki.URL,
	}, &stdout, providerFunc(func(context.Context, *Model) error {
		return wantErr
	}))
	if !errors.Is(err, wantErr) {
		t.Fatalf("RunWithProvider() error = %v, want %v", err, wantErr)
	}
}

func writePrometheusQueryResponse(t *testing.T, w http.ResponseWriter, query string) {
	t.Helper()

	var result []any
	switch query {
	case "odin_os_health_score":
		result = []any{prometheusSampleFixture(nil, "87")}
	case "odin_os_telemetry_stale":
		result = []any{prometheusSampleFixture(nil, "0")}
	case "odin_os_status":
		result = []any{
			prometheusSampleFixture(map[string]string{"status": "healthy"}, "0"),
			prometheusSampleFixture(map[string]string{"status": "degraded"}, "1"),
		}
	case "odin_os_lifecycle_phase":
		result = []any{
			prometheusSampleFixture(map[string]string{"phase": "boot"}, "0"),
			prometheusSampleFixture(map[string]string{"phase": "run"}, "1"),
		}
	case "odin_active_runs":
		result = []any{prometheusSampleFixture(nil, "3")}
	case "odin_blocked_items":
		result = []any{prometheusSampleFixture(nil, "2")}
	case "odin_approvals_waiting":
		result = []any{prometheusSampleFixture(nil, "4")}
	case "odin_review_queue_items":
		result = []any{prometheusSampleFixture(nil, "6")}
	case "odin_failed_work_items":
		result = []any{prometheusSampleFixture(nil, "1")}
	case "odin_recovery_recommendations":
		result = []any{prometheusSampleFixture(nil, "1")}
	default:
		t.Fatalf("unexpected query %q", query)
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": "success",
		"data": map[string]any{
			"resultType": "vector",
			"result":     result,
		},
	})
}

func prometheusSampleFixture(labels map[string]string, value string) map[string]any {
	if labels == nil {
		labels = map[string]string{}
	}
	return map[string]any{
		"metric": labels,
		"value":  []any{1714521600.0, value},
	}
}

type cancelAfterWritesWriter struct {
	mu     sync.Mutex
	buffer strings.Builder
	cancel context.CancelFunc
	after  int
	writes int
}

func (writer *cancelAfterWritesWriter) Write(data []byte) (int, error) {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	writer.writes++
	if _, err := writer.buffer.Write(data); err != nil {
		return 0, err
	}
	if writer.writes >= writer.after {
		writer.cancel()
	}
	return len(data), nil
}

func (writer *cancelAfterWritesWriter) String() string {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	return writer.buffer.String()
}

var _ io.Writer = (*cancelAfterWritesWriter)(nil)

type providerFunc func(context.Context, *Model) error

func (fn providerFunc) EnrichModel(ctx context.Context, model *Model) error {
	return fn(ctx, model)
}
