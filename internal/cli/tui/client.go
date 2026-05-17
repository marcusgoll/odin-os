package tui

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

var ErrUnavailableTelemetry = errors.New("unavailable telemetry")

var defaultHTTPClient = &http.Client{Timeout: 5 * time.Second}

const (
	defaultPrometheusURL = "http://127.0.0.1:9090"
	defaultMetricsURL    = "http://127.0.0.1:9444/metrics"
	recentLogsQuery      = `{job="docker-containers"} |= "GET /"`
)

const (
	enterAlternateScreen = "\x1b[?1049h\x1b[?25l"
	exitAlternateScreen  = "\x1b[?25h\x1b[?1049l"
	clearScreen          = "\x1b[2J\x1b[H"
)

type Client struct {
	PrometheusURL string
	MetricsURL    string
	LokiURL       string
	HTTPClient    *http.Client
	Provider      ModelProvider
}

type ModelProvider interface {
	EnrichModel(context.Context, *Model) error
}

func Run(ctx context.Context, args []string, stdout io.Writer) error {
	return RunWithProvider(ctx, args, stdout, nil)
}

func RunWithProvider(ctx context.Context, args []string, stdout io.Writer, provider ModelProvider) error {
	flags := flag.NewFlagSet("tui", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	once := flags.Bool("once", false, "render once and exit")
	interval := flags.Duration("interval", 5*time.Second, "refresh interval for continuous mode")
	noClear := flags.Bool("no-clear", false, "do not clear the terminal between continuous refresh frames")
	prometheusURL := flags.String("prometheus-url", defaultPrometheusURL, "Prometheus base URL")
	metricsURL := flags.String("metrics-url", defaultMetricsURL, "Odin metrics URL fallback")
	lokiURL := flags.String("loki-url", "http://127.0.0.1:3100", "Loki base URL")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			_, _ = fmt.Fprintln(stdout, "usage: odin tui [--once] [--interval 5s] [--no-clear] [--prometheus-url URL] [--loki-url URL]")
		}
		return err
	}
	if flags.NArg() > 0 {
		return fmt.Errorf("unexpected odin tui argument: %s", flags.Arg(0))
	}
	if *interval <= 0 {
		return errors.New("odin tui --interval must be greater than zero")
	}

	client := Client{
		PrometheusURL: *prometheusURL,
		MetricsURL:    *metricsURL,
		LokiURL:       *lokiURL,
		Provider:      provider,
	}
	if *once {
		return renderFrame(ctx, client, stdout, false)
	}
	return runContinuous(ctx, client, stdout, *interval, !*noClear)
}

func runContinuous(ctx context.Context, client Client, stdout io.Writer, interval time.Duration, clear bool) error {
	if clear {
		if _, err := io.WriteString(stdout, enterAlternateScreen); err != nil {
			return err
		}
		defer func() {
			_, _ = io.WriteString(stdout, exitAlternateScreen)
		}()
	}

	for {
		if err := renderFrame(ctx, client, stdout, clear); err != nil {
			return err
		}

		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return nil
		case <-timer.C:
		}
	}
}

func renderFrame(ctx context.Context, client Client, stdout io.Writer, clear bool) error {
	model, err := client.QueryOverview(ctx)
	if err != nil {
		if !errors.Is(err, ErrUnavailableTelemetry) {
			return err
		}
		model = Model{
			TelemetryAvailable:   false,
			TelemetryUnavailable: err.Error(),
		}
	}
	logs, err := client.QueryRecentLogs(ctx)
	if err != nil {
		model.LogsUnavailable = err.Error()
	} else {
		model.Logs = logs
	}
	if client.Provider != nil {
		if err := client.Provider.EnrichModel(ctx, &model); err != nil {
			return err
		}
	}
	if clear {
		if _, err := io.WriteString(stdout, clearScreen); err != nil {
			return err
		}
	}
	width, color := renderSettings(stdout)
	_, err = io.WriteString(stdout, RenderOverviewForTerminal(model, width, color))
	return err
}

func (c Client) QueryOverview(ctx context.Context) (Model, error) {
	model, err := c.queryPrometheusOverview(ctx)
	if err != nil {
		if c.prometheusURL() == defaultPrometheusURL && strings.TrimSpace(c.MetricsURL) != "" {
			return c.queryMetricsOverview(ctx)
		}
		return Model{}, err
	}
	return model, nil
}

func (c Client) queryPrometheusOverview(ctx context.Context) (Model, error) {
	healthScore, err := c.queryPrometheusScalar(ctx, "odin_os_health_score")
	if err != nil {
		return Model{}, err
	}
	telemetryStale, err := c.queryPrometheusScalar(ctx, "odin_os_telemetry_stale")
	if err != nil {
		return Model{}, err
	}
	status, err := c.queryPrometheusActiveLabel(ctx, "odin_os_status", "status")
	if err != nil {
		return Model{}, err
	}
	lifecyclePhase, err := c.queryPrometheusActiveLabel(ctx, "odin_os_lifecycle_phase", "phase")
	if err != nil {
		return Model{}, err
	}
	activeRuns, err := c.queryPrometheusScalar(ctx, "odin_active_runs")
	if err != nil {
		return Model{}, err
	}
	blockedItems, err := c.queryPrometheusScalar(ctx, "odin_blocked_items")
	if err != nil {
		return Model{}, err
	}
	approvalsWaiting, err := c.queryPrometheusScalar(ctx, "odin_approvals_waiting")
	if err != nil {
		return Model{}, err
	}
	reviewQueueItems, err := c.queryPrometheusScalar(ctx, "odin_review_queue_items")
	if err != nil {
		return Model{}, err
	}
	failedWorkItems, err := c.queryPrometheusScalar(ctx, "odin_failed_work_items")
	if err != nil {
		return Model{}, err
	}
	recoveryRecommendations, err := c.queryPrometheusScalar(ctx, "odin_recovery_recommendations")
	if err != nil {
		return Model{}, err
	}

	stale := telemetryStale >= 1
	score := int(math.Round(healthScore))
	return Model{
		TelemetryAvailable:      true,
		Status:                  status,
		HealthScore:             score,
		TelemetryStale:          stale,
		LifecyclePhase:          lifecyclePhase,
		ActiveRuns:              int(math.Round(activeRuns)),
		BlockedItems:            int(math.Round(blockedItems)),
		ApprovalsWaiting:        int(math.Round(approvalsWaiting)),
		ReviewQueueItems:        int(math.Round(reviewQueueItems)),
		FailedWorkItems:         int(math.Round(failedWorkItems)),
		RecoveryRecommendations: int(math.Round(recoveryRecommendations)),
	}, nil
}

func (c Client) QueryRecentLogs(ctx context.Context) ([]LogEntry, error) {
	if c.LokiURL == "" || c.LokiURL == "http://127.0.0.1:3100" {
		if logs, err := queryLocalOdinContainerLogs(ctx); err == nil {
			return logs, nil
		}
	}

	if c.LokiURL == "" {
		c.LokiURL = "http://127.0.0.1:3100"
	}

	baseURL, err := url.Parse(c.LokiURL)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid loki url: %v", ErrUnavailableTelemetry, err)
	}
	queryURL := baseURL.JoinPath("/loki/api/v1/query_range")
	values := queryURL.Query()
	values.Set("query", recentLogsQuery)
	values.Set("limit", "10")
	values.Set("direction", "BACKWARD")
	values.Set("end", strconv.FormatInt(time.Now().UnixNano(), 10))
	values.Set("start", strconv.FormatInt(time.Now().Add(-15*time.Minute).UnixNano(), 10))
	queryURL.RawQuery = values.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, queryURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("%w: loki request: %v", ErrUnavailableTelemetry, err)
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: loki query failed: %v", ErrUnavailableTelemetry, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%w: loki query returned HTTP %d", ErrUnavailableTelemetry, resp.StatusCode)
	}

	var decoded lokiQueryResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return nil, fmt.Errorf("%w: loki response decode failed: %v", ErrUnavailableTelemetry, err)
	}
	if decoded.Status != "success" {
		if decoded.Error != "" {
			return nil, fmt.Errorf("%w: loki query status=%s: %s", ErrUnavailableTelemetry, decoded.Status, decoded.Error)
		}
		return nil, fmt.Errorf("%w: loki query status=%s", ErrUnavailableTelemetry, decoded.Status)
	}

	var entries []LogEntry
	for _, stream := range decoded.Data.Result {
		for _, value := range stream.Values {
			if len(value) != 2 {
				continue
			}
			var ts string
			var line string
			if err := json.Unmarshal(value[0], &ts); err != nil {
				continue
			}
			if err := json.Unmarshal(value[1], &line); err != nil {
				continue
			}
			if isTelemetryQueryLog(line) {
				continue
			}
			entries = append(entries, LogEntry{
				Timestamp: ts,
				Line:      line,
				Labels:    stream.Stream,
			})
		}
	}
	return entries, nil
}

func queryLocalOdinContainerLogs(ctx context.Context) ([]LogEntry, error) {
	if _, err := exec.LookPath("docker"); err != nil {
		return nil, err
	}
	command := exec.CommandContext(ctx, "docker", "logs", "--timestamps", "--tail", "10", "odin-overseer")
	output, err := command.CombinedOutput()
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	entries := make([]LogEntry, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		timestamp, message, ok := strings.Cut(line, " ")
		if !ok {
			timestamp = ""
			message = line
		}
		entries = append(entries, LogEntry{
			Timestamp: timestamp,
			Line:      message,
			Labels:    map[string]string{"source": "docker", "container": "odin-overseer"},
		})
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("no odin-overseer logs returned")
	}
	return entries, nil
}

func isTelemetryQueryLog(line string) bool {
	return strings.Contains(line, "component=querier") ||
		strings.Contains(line, "component=frontend") ||
		strings.Contains(line, "caller=metrics.go") ||
		strings.Contains(line, "caller=engine.go") ||
		strings.Contains(line, "caller=roundtrip.go")
}

func (c Client) queryMetricsOverview(ctx context.Context) (Model, error) {
	values, err := c.queryMetricsEndpoint(ctx)
	if err != nil {
		return Model{}, err
	}
	healthScore, err := metricsScalar(values, "odin_os_health_score")
	if err != nil {
		return Model{}, err
	}
	telemetryStale, err := metricsScalar(values, "odin_os_telemetry_stale")
	if err != nil {
		return Model{}, err
	}
	status, err := metricsActiveLabel(values, "odin_os_status", "status")
	if err != nil {
		return Model{}, err
	}
	lifecyclePhase, err := metricsActiveLabel(values, "odin_os_lifecycle_phase", "phase")
	if err != nil {
		return Model{}, err
	}
	activeRuns, err := metricsScalar(values, "odin_active_runs")
	if err != nil {
		return Model{}, err
	}
	blockedItems, err := metricsScalar(values, "odin_blocked_items")
	if err != nil {
		return Model{}, err
	}
	approvalsWaiting, err := metricsScalar(values, "odin_approvals_waiting")
	if err != nil {
		return Model{}, err
	}
	reviewQueueItems, err := metricsScalar(values, "odin_review_queue_items")
	if err != nil {
		return Model{}, err
	}
	failedWorkItems, err := metricsScalar(values, "odin_failed_work_items")
	if err != nil {
		return Model{}, err
	}
	recoveryRecommendations, err := metricsScalar(values, "odin_recovery_recommendations")
	if err != nil {
		return Model{}, err
	}

	return Model{
		TelemetryAvailable:      true,
		Status:                  status,
		HealthScore:             int(math.Round(healthScore)),
		TelemetryStale:          telemetryStale >= 1,
		LifecyclePhase:          lifecyclePhase,
		ActiveRuns:              int(math.Round(activeRuns)),
		BlockedItems:            int(math.Round(blockedItems)),
		ApprovalsWaiting:        int(math.Round(approvalsWaiting)),
		ReviewQueueItems:        int(math.Round(reviewQueueItems)),
		FailedWorkItems:         int(math.Round(failedWorkItems)),
		RecoveryRecommendations: int(math.Round(recoveryRecommendations)),
	}, nil
}

func (c Client) queryMetricsEndpoint(ctx context.Context) (map[string][]metricsSample, error) {
	baseURL, err := url.Parse(c.MetricsURL)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid metrics url: %v", ErrUnavailableTelemetry, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("%w: metrics request: %v", ErrUnavailableTelemetry, err)
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: metrics endpoint failed: %v", ErrUnavailableTelemetry, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%w: metrics endpoint returned HTTP %d", ErrUnavailableTelemetry, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if err != nil {
		return nil, fmt.Errorf("%w: metrics endpoint read failed: %v", ErrUnavailableTelemetry, err)
	}
	return parseMetricsSamples(string(body)), nil
}

type metricsSample struct {
	Labels map[string]string
	Value  float64
}

func parseMetricsSamples(body string) map[string][]metricsSample {
	samples := map[string][]metricsSample{}
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		nameAndLabels, rawValue, ok := strings.Cut(line, " ")
		if !ok {
			continue
		}
		value, err := strconv.ParseFloat(strings.TrimSpace(rawValue), 64)
		if err != nil {
			continue
		}
		name := nameAndLabels
		labels := map[string]string{}
		if before, after, ok := strings.Cut(nameAndLabels, "{"); ok {
			name = before
			labels = parseMetricLabels(strings.TrimSuffix(after, "}"))
		}
		samples[name] = append(samples[name], metricsSample{Labels: labels, Value: value})
	}
	return samples
}

func parseMetricLabels(raw string) map[string]string {
	labels := map[string]string{}
	for _, part := range strings.Split(raw, ",") {
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		labels[strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(value), `"`)
	}
	return labels
}

func metricsScalar(samples map[string][]metricsSample, name string) (float64, error) {
	values := samples[name]
	if len(values) == 0 {
		return 0, fmt.Errorf("%w: metrics endpoint missing %q", ErrUnavailableTelemetry, name)
	}
	return values[0].Value, nil
}

func metricsActiveLabel(samples map[string][]metricsSample, name string, label string) (string, error) {
	for _, sample := range samples[name] {
		if sample.Value > 0 {
			if got := sample.Labels[label]; got != "" {
				return got, nil
			}
		}
	}
	return "", fmt.Errorf("%w: metrics endpoint %q returned no active %s label", ErrUnavailableTelemetry, name, label)
}

func (c Client) queryPrometheusScalar(ctx context.Context, query string) (float64, error) {
	sample, err := c.queryPrometheusOne(ctx, query)
	if err != nil {
		return 0, err
	}
	return sample.floatValue(query)
}

func (c Client) queryPrometheusActiveLabel(ctx context.Context, query string, label string) (string, error) {
	response, err := c.queryPrometheus(ctx, query)
	if err != nil {
		return "", err
	}
	for _, sample := range response.Data.Result {
		value, err := sample.floatValue(query)
		if err != nil {
			return "", err
		}
		if value > 0 {
			if got := sample.Metric[label]; got != "" {
				return got, nil
			}
		}
	}
	return "", fmt.Errorf("%w: prometheus query %q returned no active %s label", ErrUnavailableTelemetry, query, label)
}

func (c Client) queryPrometheusOne(ctx context.Context, query string) (prometheusSample, error) {
	response, err := c.queryPrometheus(ctx, query)
	if err != nil {
		return prometheusSample{}, err
	}
	if len(response.Data.Result) == 0 {
		return prometheusSample{}, fmt.Errorf("%w: prometheus query %q returned no samples", ErrUnavailableTelemetry, query)
	}
	return response.Data.Result[0], nil
}

func (c Client) queryPrometheus(ctx context.Context, query string) (prometheusQueryResponse, error) {
	baseURL, err := url.Parse(c.prometheusURL())
	if err != nil {
		return prometheusQueryResponse{}, fmt.Errorf("%w: invalid prometheus url: %v", ErrUnavailableTelemetry, err)
	}
	queryURL := baseURL.JoinPath("/api/v1/query")
	values := queryURL.Query()
	values.Set("query", query)
	queryURL.RawQuery = values.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, queryURL.String(), nil)
	if err != nil {
		return prometheusQueryResponse{}, fmt.Errorf("%w: prometheus request %q: %v", ErrUnavailableTelemetry, query, err)
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return prometheusQueryResponse{}, fmt.Errorf("%w: prometheus query %q failed: %v", ErrUnavailableTelemetry, query, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		if len(body) > 0 {
			return prometheusQueryResponse{}, fmt.Errorf("%w: prometheus query %q returned HTTP %d: %s", ErrUnavailableTelemetry, query, resp.StatusCode, string(body))
		}
		return prometheusQueryResponse{}, fmt.Errorf("%w: prometheus query %q returned HTTP %d", ErrUnavailableTelemetry, query, resp.StatusCode)
	}

	var decoded prometheusQueryResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return prometheusQueryResponse{}, fmt.Errorf("%w: prometheus query %q decode failed: %v", ErrUnavailableTelemetry, query, err)
	}
	if decoded.Status != "success" {
		if decoded.Error != "" {
			return prometheusQueryResponse{}, fmt.Errorf("%w: prometheus query %q status=%s: %s", ErrUnavailableTelemetry, query, decoded.Status, decoded.Error)
		}
		return prometheusQueryResponse{}, fmt.Errorf("%w: prometheus query %q status=%s", ErrUnavailableTelemetry, query, decoded.Status)
	}
	return decoded, nil
}

func (c Client) prometheusURL() string {
	if strings.TrimSpace(c.PrometheusURL) == "" {
		return defaultPrometheusURL
	}
	return c.PrometheusURL
}

func (c Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return defaultHTTPClient
}

type prometheusQueryResponse struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
	Data   struct {
		ResultType string             `json:"resultType"`
		Result     []prometheusSample `json:"result"`
	} `json:"data"`
}

type prometheusSample struct {
	Metric map[string]string `json:"metric"`
	Value  []json.RawMessage `json:"value"`
}

func (s prometheusSample) floatValue(query string) (float64, error) {
	if len(s.Value) != 2 {
		return 0, fmt.Errorf("%w: prometheus query %q returned malformed value", ErrUnavailableTelemetry, query)
	}
	var value string
	if err := json.Unmarshal(s.Value[1], &value); err != nil {
		return 0, fmt.Errorf("%w: prometheus query %q returned non-string sample value: %v", ErrUnavailableTelemetry, query, err)
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: prometheus query %q returned non-numeric sample value %q", ErrUnavailableTelemetry, query, value)
	}
	return parsed, nil
}

type lokiQueryResponse struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
	Data   struct {
		Result []lokiStream `json:"result"`
	} `json:"data"`
}

type lokiStream struct {
	Stream map[string]string   `json:"stream"`
	Values [][]json.RawMessage `json:"values"`
}
