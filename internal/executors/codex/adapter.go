package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"odin-os/internal/executors/contract"
)

const executorKey = "codex_headless"

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
	if _, ok := os.LookupEnv("ODIN_CODEX_DRIVER"); !ok {
		return contract.HealthReport{
			Status:    contract.HealthStatusUnavailable,
			Details:   "ODIN_CODEX_DRIVER is unset",
			CheckedAt: time.Now().UTC(),
		}, nil
	}

	response, err := invokeDriver(ctx, driverRequest{
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
	if _, ok := os.LookupEnv("ODIN_CODEX_DRIVER"); !ok {
		return contract.ExecutionResult{}, fmt.Errorf("codex driver unavailable: ODIN_CODEX_DRIVER is unset")
	}

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

	response, err := invokeDriver(ctx, request)
	if err != nil {
		return contract.ExecutionResult{}, err
	}
	runStatus, err := validateRunStatus(response.Status)
	if err != nil {
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
		Metadata: response.Metadata,
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

func invokeDriver(ctx context.Context, request driverRequest) (driverResponse, error) {
	driver, ok := os.LookupEnv("ODIN_CODEX_DRIVER")
	if !ok || strings.TrimSpace(driver) == "" {
		return driverResponse{}, fmt.Errorf("codex driver unavailable: ODIN_CODEX_DRIVER is unset")
	}

	input, err := json.Marshal(request)
	if err != nil {
		return driverResponse{}, err
	}

	cmd := exec.CommandContext(ctx, driver)
	cmd.Stdin = strings.NewReader(string(input))
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && len(exitErr.Stderr) > 0 {
			return driverResponse{}, fmt.Errorf("codex driver failed: %w: %s", err, strings.TrimSpace(string(exitErr.Stderr)))
		}
		return driverResponse{}, fmt.Errorf("codex driver failed: %w", err)
	}

	var response driverResponse
	if err := json.Unmarshal(output, &response); err != nil {
		return driverResponse{}, fmt.Errorf("codex driver returned invalid JSON: %w", err)
	}
	return response, nil
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
