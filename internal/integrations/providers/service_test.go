package providers

import (
	"context"
	"testing"
	"time"

	"odin-os/internal/executors/contract"
)

func TestProviderServiceBuildsCapabilityProfile(t *testing.T) {
	t.Parallel()

	service := Service{
		Executors: map[string]contract.Executor{
			"openai_api": contract.NewStaticExecutor(
				"openai_api",
				contract.ExecutorClassAPI,
				contract.HealthReport{Status: contract.HealthStatusHealthy, CheckedAt: time.Now().UTC()},
				contract.Capabilities{
					ExecutorClass:        contract.ExecutorClassAPI,
					SupportsResume:       true,
					SupportsCancel:       true,
					SupportsTools:        true,
					SupportsStreaming:    true,
					SupportsCostEstimate: true,
					TaskKinds:            []contract.TaskKind{contract.TaskKindResearch},
					Scopes:               []string{"global"},
				},
			),
		},
	}

	profile, err := service.CapabilityProfile(context.Background(), "openai_api")
	if err != nil {
		t.Fatalf("CapabilityProfile() error = %v", err)
	}
	if profile.ProviderKey != "openai_api" {
		t.Fatalf("ProviderKey = %q, want openai_api", profile.ProviderKey)
	}
	if !profile.SupportsStreaming {
		t.Fatalf("SupportsStreaming = false, want true")
	}
	if !profile.Matches(contract.TaskSpec{
		ID:    "stream-1",
		Kind:  contract.TaskKindResearch,
		Scope: "global",
		Requirements: contract.Requirements{
			AllowedClasses: []contract.ExecutorClass{contract.ExecutorClassAPI},
			NeedsStreaming: true,
		},
	}) {
		t.Fatalf("Matches() = false, want true")
	}
}

func TestProviderServiceExecuteReturnsStreamingEnvelope(t *testing.T) {
	t.Parallel()

	service := Service{
		Executors: map[string]contract.Executor{
			"codex_headless": &providerTestExecutor{},
		},
	}

	result, events, err := service.Execute(context.Background(), ExecutionRequest{
		ProviderKey: "codex_headless",
		Stream:      true,
		Spec: contract.TaskSpec{
			ID:     "task-1",
			Kind:   contract.TaskKindPlan,
			Scope:  "project",
			Prompt: "Produce a safe plan.",
		},
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Status != "completed" {
		t.Fatalf("result.Status = %q, want completed", result.Status)
	}
	if len(events) == 0 || events[len(events)-1].EventType != "completed" {
		t.Fatalf("events = %+v, want completed envelope", events)
	}
}

type providerTestExecutor struct{}

func (providerTestExecutor) Key() string { return "codex_headless" }

func (providerTestExecutor) Class() contract.ExecutorClass {
	return contract.ExecutorClassPlanBackedCLI
}

func (providerTestExecutor) Health(context.Context) (contract.HealthReport, error) {
	return contract.HealthReport{Status: contract.HealthStatusHealthy, CheckedAt: time.Now().UTC()}, nil
}

func (providerTestExecutor) Capabilities(context.Context) (contract.Capabilities, error) {
	return contract.Capabilities{
		ExecutorClass:        contract.ExecutorClassPlanBackedCLI,
		SupportsResume:       true,
		SupportsCancel:       true,
		SupportsTools:        true,
		SupportsStreaming:    true,
		SupportsHeadlessPlan: true,
		TaskKinds:            []contract.TaskKind{contract.TaskKindPlan},
		Scopes:               []string{"project"},
	}, nil
}

func (providerTestExecutor) RunTask(context.Context, contract.TaskSpec) (contract.ExecutionResult, error) {
	return contract.ExecutionResult{
		Handle: contract.TaskHandle{
			ExecutorKey: "codex_headless",
			ExternalID:  "task-1",
			Status:      "completed",
		},
		Status: "completed",
		Output: "plan ready",
	}, nil
}

func (providerTestExecutor) ResumeTask(context.Context, contract.TaskHandle, contract.ResumePacket) (contract.ExecutionResult, error) {
	return contract.ExecutionResult{}, contract.ErrNotImplemented
}

func (providerTestExecutor) CancelTask(context.Context, contract.TaskHandle) error {
	return contract.ErrNotImplemented
}

func (providerTestExecutor) EstimateCost(context.Context, contract.TaskSpec) (contract.CostEstimate, error) {
	return contract.CostEstimate{}, contract.ErrNotImplemented
}
