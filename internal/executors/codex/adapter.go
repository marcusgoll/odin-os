package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"odin-os/internal/executors/contract"
	"odin-os/internal/executors/drivers"
)

const executorKey = "codex_headless"

var (
	healthDriverTimeout = 5 * time.Second
	runDriverTimeout    = 30 * time.Second
)

type driverRequest struct {
	Action string            `json:"action"`
	Task   *driverTask       `json:"task,omitempty"`
	Meta   map[string]string `json:"meta,omitempty"`
	Mode   string            `json:"mode,omitempty"`
}

type driverTask struct {
	ID           string                `json:"id"`
	Kind         contract.TaskKind     `json:"kind"`
	Scope        string                `json:"scope"`
	Prompt       string                `json:"prompt"`
	Budget       contract.BudgetHints  `json:"budget,omitempty"`
	Tools        contract.ToolPolicy   `json:"tools,omitempty"`
	Metadata     map[string]string     `json:"metadata,omitempty"`
	Requirements contract.Requirements `json:"requirements"`
}

type driverResponse struct {
	Status   string               `json:"status"`
	Details  string               `json:"details,omitempty"`
	Output   string               `json:"output"`
	Metadata map[string]string    `json:"metadata,omitempty"`
	Handle   *contract.TaskHandle `json:"handle,omitempty"`
}

type headlessExecutor struct {
	repoRoot string
}

func NewHeadless() contract.Executor {
	return headlessExecutor{}
}

func NewHeadlessWithRepoRoot(repoRoot string) contract.Executor {
	return headlessExecutor{repoRoot: strings.TrimSpace(repoRoot)}
}

func (headlessExecutor) Key() string {
	return executorKey
}

func (headlessExecutor) Class() contract.ExecutorClass {
	return contract.ExecutorClassPlanBackedCLI
}

func (executor headlessExecutor) Health(ctx context.Context) (contract.HealthReport, error) {
	if driver, ok := explicitDriverPath(); ok {
		if report, ok := executor.jsonDriverHealth(ctx, driver); ok {
			return report, nil
		}
		if err := validateDriverPath(driver); err != nil {
			return contract.HealthReport{
				Status:    contract.HealthStatusUnavailable,
				Details:   fmt.Sprintf("codex legacy driver is unavailable: %v", err),
				CheckedAt: time.Now().UTC(),
			}, nil
		}
		return contract.HealthReport{
			Status:    contract.HealthStatusHealthy,
			Details:   fmt.Sprintf("codex legacy driver ready at %s", driver),
			CheckedAt: time.Now().UTC(),
		}, nil
	}

	if _, ok := executor.driverPath(); !ok {
		return contract.HealthReport{
			Status:    contract.HealthStatusUnavailable,
			Details:   "codex driver is unavailable",
			CheckedAt: time.Now().UTC(),
		}, nil
	}

	response, _, err := executor.invokeDriver(ctx, driverRequest{
		Action: "health",
		Mode:   "headless",
	})
	if err != nil {
		return contract.HealthReport{
			Status:    contract.HealthStatusUnavailable,
			Details:   err.Error(),
			CheckedAt: time.Now().UTC(),
		}, nil
	}

	status, ok := validateHealthStatus(response.Status)
	if !ok {
		return contract.HealthReport{
			Status:    contract.HealthStatusUnavailable,
			Details:   fmt.Sprintf("invalid health status %q", response.Status),
			CheckedAt: time.Now().UTC(),
		}, nil
	}
	return contract.HealthReport{
		Status:    status,
		Details:   response.Details,
		CheckedAt: time.Now().UTC(),
	}, nil
}

func (headlessExecutor) Capabilities(context.Context) (contract.Capabilities, error) {
	return contract.Capabilities{
		ExecutorClass:        contract.ExecutorClassPlanBackedCLI,
		SupportsHeadlessPlan: true,
		TaskKinds: []contract.TaskKind{
			contract.TaskKindGeneral,
			contract.TaskKindPlan,
			contract.TaskKindBuild,
			contract.TaskKindReview,
			contract.TaskKindQA,
			contract.TaskKindResearch,
		},
		Scopes: []string{"global", "odin-core", "project", "new-project"},
	}, nil
}

func (executor headlessExecutor) RunTask(ctx context.Context, spec contract.TaskSpec) (contract.ExecutionResult, error) {
	if driver, ok := explicitDriverPath(); ok {
		if _, ok := executor.jsonDriverHealth(ctx, driver); ok {
			return executor.runJSONDriver(ctx, spec)
		}
		return executor.runLegacyDriver(ctx, driver, spec)
	}

	return executor.runJSONDriver(ctx, spec)
}

func (executor headlessExecutor) runJSONDriver(ctx context.Context, spec contract.TaskSpec) (contract.ExecutionResult, error) {
	request := driverRequest{
		Action: "run",
		Task: &driverTask{
			ID:           spec.ID,
			Kind:         spec.Kind,
			Scope:        spec.Scope,
			Prompt:       spec.Prompt,
			Budget:       spec.Budget,
			Tools:        spec.Tools,
			Metadata:     spec.Metadata,
			Requirements: spec.Requirements,
		},
		Meta: map[string]string{},
		Mode: "headless",
	}
	for key, value := range spec.Metadata {
		request.Meta[key] = value
	}

	response, payload, err := executor.invokeDriver(ctx, request)
	if err != nil {
		return contract.ExecutionResult{}, err
	}
	runStatus, err := validateRunStatus(response.Status)
	if err != nil {
		return contract.ExecutionResult{}, err
	}

	metadata := response.Metadata
	if metadata == nil {
		metadata = map[string]string{}
	}
	if err := ensureArtifactMetadata(spec, payload, metadata); err != nil {
		return contract.ExecutionResult{}, err
	}

	handle := contract.TaskHandle{
		ExecutorKey: executorKey,
		ExternalID:  spec.ID,
		Status:      runStatus,
	}
	if response.Handle != nil {
		if response.Handle.ExecutorKey != "" {
			handle.ExecutorKey = response.Handle.ExecutorKey
		}
		if response.Handle.ExternalID != "" {
			handle.ExternalID = response.Handle.ExternalID
		}
	}

	return contract.ExecutionResult{
		Handle:   handle,
		Status:   runStatus,
		Output:   response.Output,
		Metadata: metadata,
	}, nil
}

func (executor headlessExecutor) jsonDriverHealth(ctx context.Context, driver string) (contract.HealthReport, bool) {
	response, _, err := executor.invokeDriverPath(ctx, driver, driverRequest{
		Action: "health",
		Mode:   "headless",
	})
	if err != nil {
		return contract.HealthReport{}, false
	}

	status, ok := validateHealthStatus(response.Status)
	if !ok {
		return contract.HealthReport{}, false
	}
	return contract.HealthReport{
		Status:    status,
		Details:   response.Details,
		CheckedAt: time.Now().UTC(),
	}, true
}

func (executor headlessExecutor) runLegacyDriver(ctx context.Context, driver string, spec contract.TaskSpec) (contract.ExecutionResult, error) {
	if err := validateDriverPath(driver); err != nil {
		return contract.ExecutionResult{}, fmt.Errorf("codex legacy driver unavailable: %w", err)
	}

	payload, err := json.Marshal(spec)
	if err != nil {
		return contract.ExecutionResult{}, err
	}

	driverCtx, cancel := context.WithTimeout(ctx, runDriverTimeout)
	defer cancel()

	cmd := exec.CommandContext(driverCtx, driver)
	cmd.Env = drivers.AllowlistedEnvironment("ODIN_CODEX_DRIVER_ACTION=run")
	cmd.Stdin = bytes.NewReader(payload)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err != nil {
		details := strings.TrimSpace(stderr.String())
		if details == "" {
			details = strings.TrimSpace(string(output))
		}
		if details != "" {
			return contract.ExecutionResult{}, fmt.Errorf("codex legacy driver failed: %w: %s", err, details)
		}
		return contract.ExecutionResult{}, fmt.Errorf("codex legacy driver failed: %w", err)
	}

	var response driverResponse
	if err := json.Unmarshal(output, &response); err != nil {
		return contract.ExecutionResult{}, fmt.Errorf("codex legacy driver returned invalid JSON: %w", err)
	}
	runStatus, err := validateRunStatus(response.Status)
	if err != nil {
		return contract.ExecutionResult{}, err
	}

	metadata := response.Metadata
	if metadata == nil {
		metadata = map[string]string{}
	}
	if err := ensureArtifactMetadata(spec, output, metadata); err != nil {
		return contract.ExecutionResult{}, err
	}

	handle := contract.TaskHandle{
		ExecutorKey: executorKey,
		ExternalID:  spec.ID,
		Status:      runStatus,
	}
	if response.Handle != nil {
		if response.Handle.ExecutorKey != "" {
			handle.ExecutorKey = response.Handle.ExecutorKey
		}
		if response.Handle.ExternalID != "" {
			handle.ExternalID = response.Handle.ExternalID
		}
	}

	return contract.ExecutionResult{
		Handle:   handle,
		Status:   runStatus,
		Output:   response.Output,
		Metadata: metadata,
	}, nil
}

func validateHealthStatus(status string) (contract.HealthStatus, bool) {
	switch contract.HealthStatus(status) {
	case contract.HealthStatusHealthy, contract.HealthStatusDegraded, contract.HealthStatusUnavailable, contract.HealthStatusUnknown:
		return contract.HealthStatus(status), true
	default:
		return contract.HealthStatusUnavailable, false
	}
}

func validateRunStatus(status string) (string, error) {
	normalized := strings.TrimSpace(status)
	switch normalized {
	case "completed", "failed", "interrupted":
		return normalized, nil
	default:
		return "", fmt.Errorf("invalid run status %q", status)
	}
}

func (executor headlessExecutor) invokeDriver(ctx context.Context, request driverRequest) (driverResponse, []byte, error) {
	driver, ok := executor.driverPath()
	if !ok {
		return driverResponse{}, nil, fmt.Errorf("codex driver unavailable")
	}
	return executor.invokeDriverPath(ctx, driver, request)
}

func (executor headlessExecutor) invokeDriverPath(ctx context.Context, driver string, request driverRequest) (driverResponse, []byte, error) {
	var response driverResponse
	payload, err := drivers.Invoke(ctx, drivers.Options{
		DriverPath: driver,
		Label:      "codex",
		Timeout:    driverTimeout(request.Action),
	}, request, &response)
	if err != nil {
		return driverResponse{}, nil, err
	}
	return response, payload, nil
}

func driverTimeout(action string) time.Duration {
	if action == "health" {
		return healthDriverTimeout
	}
	return runDriverTimeout
}

func (headlessExecutor) ResumeTask(context.Context, contract.TaskHandle, contract.ResumePacket) (contract.ExecutionResult, error) {
	return contract.ExecutionResult{}, contract.ErrNotImplemented
}

func (headlessExecutor) CancelTask(context.Context, contract.TaskHandle) error {
	return contract.ErrNotImplemented
}

func (headlessExecutor) EstimateCost(context.Context, contract.TaskSpec) (contract.CostEstimate, error) {
	return contract.CostEstimate{}, contract.ErrNotImplemented
}

func (executor headlessExecutor) driverPath() (string, bool) {
	if driver, ok := explicitDriverPath(); ok {
		return driver, true
	}
	if strings.TrimSpace(executor.repoRoot) == "" {
		return "", false
	}

	driver := filepath.Join(executor.repoRoot, "scripts", "drivers", "codex-headless.sh")
	return filepath.Clean(driver), true
}

func explicitDriverPath() (string, bool) {
	driver := strings.TrimSpace(os.Getenv("ODIN_CODEX_DRIVER"))
	if driver == "" {
		return "", false
	}
	return filepath.Clean(driver), true
}

func validateDriverPath(driverPath string) error {
	info, err := os.Stat(driverPath)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("%s is a directory", driverPath)
	}
	if info.Mode()&0o111 == 0 {
		return fmt.Errorf("%s is not executable", driverPath)
	}
	return nil
}

func ensureArtifactMetadata(spec contract.TaskSpec, payload []byte, metadata map[string]string) error {
	if strings.TrimSpace(metadata["artifacts_json"]) != "" || strings.TrimSpace(metadata["artifact_path"]) != "" {
		return nil
	}

	baseDir := strings.TrimSpace(spec.Metadata["runtime_root"])
	if baseDir == "" {
		baseDir = strings.TrimSpace(spec.Metadata["worktree_path"])
	}
	if baseDir == "" {
		baseDir = strings.TrimSpace(spec.Metadata["repo_root"])
	}
	if baseDir == "" {
		return nil
	}

	artifactPath, err := writeDriverArtifact(baseDir, artifactFileKey(spec), payload)
	if err != nil {
		return err
	}

	metadata["artifact_path"] = artifactPath
	encoded, err := json.Marshal([]string{artifactPath})
	if err != nil {
		return err
	}
	metadata["artifacts_json"] = string(encoded)
	return nil
}

func writeDriverArtifact(baseDir, artifactKey string, payload []byte) (string, error) {
	artifactDir := filepath.Join(baseDir, "runs", "artifacts")
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		return "", err
	}

	artifactPath := filepath.Join(artifactDir, sanitizeArtifactName(artifactKey)+".json")
	if err := os.WriteFile(artifactPath, payload, 0o644); err != nil {
		return "", err
	}
	return artifactPath, nil
}

func artifactFileKey(spec contract.TaskSpec) string {
	taskID := spec.ID
	if taskID == "" {
		taskID = "codex-headless-run"
	}
	return taskID
}

func sanitizeArtifactName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "codex-headless-run"
	}

	var builder strings.Builder
	for _, char := range value {
		switch {
		case char >= 'a' && char <= 'z':
			builder.WriteRune(char)
		case char >= 'A' && char <= 'Z':
			builder.WriteRune(char + ('a' - 'A'))
		case char >= '0' && char <= '9':
			builder.WriteRune(char)
		case char == '-' || char == '_':
			builder.WriteRune(char)
		default:
			builder.WriteByte('-')
		}
	}

	result := strings.Trim(builder.String(), "-_")
	if result == "" {
		return "codex-headless-run"
	}
	return result
}
