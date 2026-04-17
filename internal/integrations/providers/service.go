package providers

import (
	"context"
	"fmt"

	"odin-os/internal/executors/contract"
)

type Service struct {
	Executors map[string]contract.Executor
}

func (service Service) CapabilityProfile(ctx context.Context, providerKey string) (CapabilityProfile, error) {
	executor, err := service.lookup(providerKey)
	if err != nil {
		return CapabilityProfile{}, err
	}

	capabilities, err := executor.Capabilities(ctx)
	if err != nil {
		return CapabilityProfile{}, ProviderError{
			ProviderKey: providerKey,
			Code:        "capabilities_unavailable",
			Message:     err.Error(),
		}
	}

	return CapabilityProfile{
		ProviderKey:           providerKey,
		ExecutorClass:         capabilities.ExecutorClass,
		SupportsResume:        capabilities.SupportsResume,
		SupportsCancel:        capabilities.SupportsCancel,
		SupportsTools:         capabilities.SupportsTools,
		SupportsStreaming:     capabilities.SupportsStreaming,
		SupportsCostEstimate:  capabilities.SupportsCostEstimate,
		SupportsHeadlessPlan:  capabilities.SupportsHeadlessPlan,
		SupportsBrokerRouting: capabilities.SupportsBrokerRouting,
		TaskKinds:             append([]contract.TaskKind(nil), capabilities.TaskKinds...),
		Scopes:                append([]string(nil), capabilities.Scopes...),
	}, nil
}

func (service Service) Health(ctx context.Context, providerKey string) (contract.HealthReport, error) {
	executor, err := service.lookup(providerKey)
	if err != nil {
		return contract.HealthReport{}, err
	}

	report, err := executor.Health(ctx)
	if err != nil {
		return contract.HealthReport{}, ProviderError{
			ProviderKey: providerKey,
			Code:        "health_unavailable",
			Message:     err.Error(),
			Retryable:   true,
		}
	}
	return report, nil
}

func (service Service) Execute(ctx context.Context, request ExecutionRequest) (contract.ExecutionResult, []StreamingEventEnvelope, error) {
	executor, err := service.lookup(request.ProviderKey)
	if err != nil {
		return contract.ExecutionResult{}, nil, err
	}

	result, err := executor.RunTask(ctx, request.Spec)
	if err != nil {
		return contract.ExecutionResult{}, nil, ProviderError{
			ProviderKey: request.ProviderKey,
			Code:        "execution_failed",
			Message:     err.Error(),
			Retryable:   true,
		}
	}

	if !request.Stream {
		return result, nil, nil
	}

	events := []StreamingEventEnvelope{
		{
			ProviderKey: request.ProviderKey,
			EventType:   "started",
			Sequence:    1,
			Content:     request.Spec.ID,
			Metadata:    map[string]string{"task_kind": string(request.Spec.Kind)},
		},
		{
			ProviderKey: request.ProviderKey,
			EventType:   "completed",
			Sequence:    2,
			Content:     result.Output,
			Metadata:    map[string]string{"status": result.Status},
		},
	}
	return result, events, nil
}

func (service Service) lookup(providerKey string) (contract.Executor, error) {
	if providerKey == "" {
		return nil, ProviderError{Code: "provider_required", Message: "provider key is required"}
	}
	executor, ok := service.Executors[providerKey]
	if !ok {
		return nil, ProviderError{
			ProviderKey: providerKey,
			Code:        "provider_not_registered",
			Message:     fmt.Sprintf("provider %q is not registered", providerKey),
		}
	}
	return executor, nil
}
