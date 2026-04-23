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
)

type headlessExecutor struct {
	repoRoot string
}

const driverEnvVar = "ODIN_CODEX_DRIVER"

func NewHeadless() contract.Executor {
	return headlessExecutor{}
}

func NewHeadlessWithRepoRoot(repoRoot string) contract.Executor {
	return headlessExecutor{repoRoot: strings.TrimSpace(repoRoot)}
}

func (headlessExecutor) Key() string {
	return "codex_headless"
}

func (headlessExecutor) Class() contract.ExecutorClass {
	return contract.ExecutorClassPlanBackedCLI
}

func (executor headlessExecutor) Health(context.Context) (contract.HealthReport, error) {
	switch {
	case strings.TrimSpace(os.Getenv(driverEnvVar)) != "":
		return contract.HealthReport{
			Status:    contract.HealthStatusHealthy,
			Details:   "external codex driver configured",
			CheckedAt: time.Now().UTC(),
		}, nil
	case executor.driverPath() != "":
		return contract.HealthReport{
			Status:    contract.HealthStatusHealthy,
			Details:   "repo-local codex driver available",
			CheckedAt: time.Now().UTC(),
		}, nil
	}
	return contract.HealthReport{
		Status:    contract.HealthStatusHealthy,
		Details:   "local deterministic alpha lane",
		CheckedAt: time.Now().UTC(),
	}, nil
}

func (headlessExecutor) Capabilities(context.Context) (contract.Capabilities, error) {
	return contract.Capabilities{
		ExecutorClass:        contract.ExecutorClassPlanBackedCLI,
		SupportsResume:       true,
		SupportsCancel:       true,
		SupportsTools:        true,
		SupportsCostEstimate: true,
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
	if driver := executor.driverPath(); driver != "" {
		return runDriver(ctx, driverRequest{
			Operation: "run_task",
			Task:      &spec,
		}, driver)
	}
	return contract.ExecutionResult{
		Handle: contract.TaskHandle{
			ExecutorKey: "codex_headless",
			ExternalID:  spec.ID,
			Status:      "completed",
		},
		Status: "completed",
		Output: fmt.Sprintf("codex_headless completed %s task %s in %s scope: %s", spec.Kind, spec.ID, spec.Scope, spec.Prompt),
		Metadata: map[string]string{
			"executor_class": string(contract.ExecutorClassPlanBackedCLI),
			"lane":           "local_deterministic_alpha",
		},
	}, nil
}

func (executor headlessExecutor) ResumeTask(ctx context.Context, handle contract.TaskHandle, packet contract.ResumePacket) (contract.ExecutionResult, error) {
	if driver := executor.driverPath(); driver != "" {
		return runDriver(ctx, driverRequest{
			Operation: "resume_task",
			Handle:    &handle,
			Packet:    &packet,
		}, driver)
	}
	return contract.ExecutionResult{
		Handle: contract.TaskHandle{
			ExecutorKey: "codex_headless",
			ExternalID:  handle.ExternalID,
			Status:      "completed",
		},
		Status: "completed",
		Output: fmt.Sprintf("codex_headless resumed %s with %s", handle.ExternalID, packet.Summary),
		Metadata: map[string]string{
			"resume_kind": packet.Kind,
		},
	}, nil
}

func (headlessExecutor) CancelTask(context.Context, contract.TaskHandle) error {
	return nil
}

func (headlessExecutor) EstimateCost(context.Context, contract.TaskSpec) (contract.CostEstimate, error) {
	return contract.CostEstimate{
		InputTokens:  0,
		OutputTokens: 0,
		EstimatedUSD: 0,
		Currency:     "USD",
	}, nil
}

type driverRequest struct {
	Operation string                 `json:"operation"`
	Task      *contract.TaskSpec     `json:"task,omitempty"`
	Handle    *contract.TaskHandle   `json:"handle,omitempty"`
	Packet    *contract.ResumePacket `json:"packet,omitempty"`
}

type driverResponse struct {
	Status     string            `json:"status"`
	Output     string            `json:"output"`
	ExternalID string            `json:"external_id"`
	Metadata   map[string]string `json:"metadata"`
}

func (executor headlessExecutor) driverPath() string {
	if driver := strings.TrimSpace(os.Getenv(driverEnvVar)); driver != "" {
		return driver
	}
	if executor.repoRoot == "" {
		return ""
	}

	driver := filepath.Join(executor.repoRoot, "scripts", "drivers", "codex-headless.sh")
	info, err := os.Stat(driver)
	if err != nil || info.IsDir() {
		return ""
	}
	return driver
}

func runDriver(ctx context.Context, request driverRequest, driver string) (contract.ExecutionResult, error) {
	if strings.TrimSpace(driver) == "" {
		return contract.ExecutionResult{}, fmt.Errorf("codex driver is not configured")
	}

	payload, err := json.Marshal(request)
	if err != nil {
		return contract.ExecutionResult{}, err
	}

	cmd := exec.CommandContext(ctx, driver)
	cmd.Stdin = bytes.NewReader(payload)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return contract.ExecutionResult{}, fmt.Errorf("codex driver failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	var response driverResponse
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		return contract.ExecutionResult{}, fmt.Errorf("decode codex driver response: %w", err)
	}
	if response.Status == "" {
		return contract.ExecutionResult{}, fmt.Errorf("codex driver response missing status")
	}
	externalID := response.ExternalID
	if externalID == "" {
		switch {
		case request.Task != nil:
			externalID = request.Task.ID
		case request.Handle != nil:
			externalID = request.Handle.ExternalID
		}
	}

	return contract.ExecutionResult{
		Handle: contract.TaskHandle{
			ExecutorKey: "codex_headless",
			ExternalID:  externalID,
			Status:      response.Status,
		},
		Status:   response.Status,
		Output:   response.Output,
		Metadata: response.Metadata,
	}, nil
}
