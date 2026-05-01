package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRunRejectsContinuousMode(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	err := Run(context.Background(), nil, &stdout)
	if err == nil || !strings.Contains(err.Error(), "--once only") {
		t.Fatalf("Run() error = %v, want --once only refusal", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want no render for refused continuous mode", stdout.String())
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
	for _, want := range []string{
		"odin_os_health_score",
		"odin_os_telemetry_stale",
		"odin_os_status",
		"odin_os_lifecycle_phase",
		"odin_active_runs",
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
		case "odin_active_runs":
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
