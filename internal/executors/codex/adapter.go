package codex

import (
	"context"
	"fmt"
	"time"

	"odin-os/internal/executors/contract"
)

type headlessExecutor struct{}

func NewHeadless() contract.Executor {
	return headlessExecutor{}
}

func (headlessExecutor) Key() string {
	return "codex_headless"
}

func (headlessExecutor) Class() contract.ExecutorClass {
	return contract.ExecutorClassPlanBackedCLI
}

func (headlessExecutor) Health(context.Context) (contract.HealthReport, error) {
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

func (headlessExecutor) RunTask(_ context.Context, spec contract.TaskSpec) (contract.ExecutionResult, error) {
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

func (headlessExecutor) ResumeTask(ctx context.Context, handle contract.TaskHandle, packet contract.ResumePacket) (contract.ExecutionResult, error) {
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
