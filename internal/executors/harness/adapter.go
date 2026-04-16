package harness

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"odin-os/internal/executors/contract"
)

type DriverRequest struct {
	ExecutorKey string            `json:"executor_key"`
	Backend     string            `json:"backend"`
	Task        contract.TaskSpec `json:"task"`
}

type DriverResponse struct {
	Status     string            `json:"status"`
	Output     string            `json:"output"`
	ExternalID string            `json:"external_id"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

type driverExecutor struct {
	key     string
	envVar  string
	backend string
}

func NewDriver(key string, envVar string, backend string) contract.Executor {
	return driverExecutor{
		key:     key,
		envVar:  envVar,
		backend: backend,
	}
}

func (executor driverExecutor) Key() string {
	return executor.key
}

func (executor driverExecutor) Class() contract.ExecutorClass {
	return contract.ExecutorClassPlanBackedCLI
}

func (executor driverExecutor) Health(context.Context) (contract.HealthReport, error) {
	command := strings.TrimSpace(os.Getenv(executor.envVar))
	if command == "" {
		return contract.HealthReport{
			Status:    contract.HealthStatusUnavailable,
			Details:   "driver command not configured",
			CheckedAt: time.Now().UTC(),
		}, nil
	}
	return contract.HealthReport{
		Status:    contract.HealthStatusHealthy,
		Details:   "external harness driver configured",
		CheckedAt: time.Now().UTC(),
	}, nil
}

func (executor driverExecutor) Capabilities(context.Context) (contract.Capabilities, error) {
	return contract.Capabilities{
		ExecutorClass:        contract.ExecutorClassPlanBackedCLI,
		SupportsTools:        true,
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

func (executor driverExecutor) RunTask(ctx context.Context, spec contract.TaskSpec) (contract.ExecutionResult, error) {
	command := strings.TrimSpace(os.Getenv(executor.envVar))
	if command == "" {
		return contract.ExecutionResult{}, fmt.Errorf("driver command not configured")
	}

	requestBytes, err := json.Marshal(DriverRequest{
		ExecutorKey: executor.key,
		Backend:     executor.backend,
		Task:        spec,
	})
	if err != nil {
		return contract.ExecutionResult{}, err
	}

	commandParts := strings.Fields(command)
	if len(commandParts) == 0 {
		return contract.ExecutionResult{}, fmt.Errorf("driver command not configured")
	}

	cmd := exec.CommandContext(ctx, commandParts[0], commandParts[1:]...)
	cmd.Stdin = bytes.NewReader(requestBytes)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return contract.ExecutionResult{}, fmt.Errorf("driver command failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	var response DriverResponse
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		return contract.ExecutionResult{}, fmt.Errorf("decode driver response: %w", err)
	}

	status := strings.TrimSpace(response.Status)
	if status == "" {
		status = "completed"
	}
	externalID := strings.TrimSpace(response.ExternalID)
	if externalID == "" {
		externalID = spec.ID
	}

	return contract.ExecutionResult{
		Handle: contract.TaskHandle{
			ExecutorKey: executor.key,
			ExternalID:  externalID,
			Status:      status,
		},
		Status:   status,
		Output:   response.Output,
		Metadata: response.Metadata,
	}, nil
}

func (executor driverExecutor) ResumeTask(context.Context, contract.TaskHandle, contract.ResumePacket) (contract.ExecutionResult, error) {
	return contract.ExecutionResult{}, contract.ErrNotImplemented
}

func (executor driverExecutor) CancelTask(context.Context, contract.TaskHandle) error {
	return contract.ErrNotImplemented
}

func (executor driverExecutor) EstimateCost(context.Context, contract.TaskSpec) (contract.CostEstimate, error) {
	return contract.CostEstimate{}, contract.ErrNotImplemented
}
