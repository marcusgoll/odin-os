package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"odin-os/internal/executors/contract"
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

type headlessExecutor struct{}

func NewHeadless() contract.Executor {
	return headlessExecutor{}
}

func (headlessExecutor) Key() string {
	return executorKey
}

func (headlessExecutor) Class() contract.ExecutorClass {
	return contract.ExecutorClassPlanBackedCLI
}

func (headlessExecutor) Health(ctx context.Context) (contract.HealthReport, error) {
	if _, ok := driverPath(); !ok {
		return contract.HealthReport{
			Status:    contract.HealthStatusUnavailable,
			Details:   "ODIN_CODEX_DRIVER is unset",
			CheckedAt: time.Now().UTC(),
		}, nil
	}

	response, _, err := invokeDriver(ctx, driverRequest{
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

func (headlessExecutor) RunTask(ctx context.Context, spec contract.TaskSpec) (contract.ExecutionResult, error) {
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

	response, payload, err := invokeDriver(ctx, request)
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

func invokeDriver(ctx context.Context, request driverRequest) (driverResponse, []byte, error) {
	driver, ok := driverPath()
	if !ok {
		return driverResponse{}, nil, fmt.Errorf("codex driver unavailable: ODIN_CODEX_DRIVER is unset")
	}
	if err := validateDriverPath(driver); err != nil {
		return driverResponse{}, nil, fmt.Errorf("codex driver unavailable: %w", err)
	}

	payload, err := json.Marshal(request)
	if err != nil {
		return driverResponse{}, nil, err
	}

	driverCtx, cancel, timeoutLabel := boundedDriverContext(ctx, request.Action)
	defer cancel()

	cmd := exec.CommandContext(driverCtx, driver)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
			return err
		}
		return nil
	}
	cmd.Stdin = strings.NewReader(string(payload))
	output, err := cmd.Output()
	if err != nil {
		if errors.Is(driverCtx.Err(), context.DeadlineExceeded) {
			if timeoutLabel != "" {
				return driverResponse{}, nil, fmt.Errorf("codex driver timed out after %s: %w", timeoutLabel, driverCtx.Err())
			}
			return driverResponse{}, nil, fmt.Errorf("codex driver timed out: %w", driverCtx.Err())
		}
		if exitErr, ok := err.(*exec.ExitError); ok && len(exitErr.Stderr) > 0 {
			return driverResponse{}, nil, fmt.Errorf("codex driver failed: %w: %s", err, strings.TrimSpace(string(exitErr.Stderr)))
		}
		return driverResponse{}, nil, fmt.Errorf("codex driver failed: %w", err)
	}

	var response driverResponse
	if err := json.Unmarshal(output, &response); err != nil {
		return driverResponse{}, nil, fmt.Errorf("codex driver returned invalid JSON: %w", err)
	}
	return response, payload, nil
}

func boundedDriverContext(ctx context.Context, action string) (context.Context, context.CancelFunc, string) {
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}, ""
	}

	timeout := runDriverTimeout
	if action == "health" {
		timeout = healthDriverTimeout
	}
	boundedCtx, cancel := context.WithTimeout(ctx, timeout)
	return boundedCtx, cancel, timeout.String()
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

func driverPath() (string, bool) {
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
