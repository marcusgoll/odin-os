package contract

import (
	"context"
	"errors"
	"time"
)

type ExecutorClass string

const (
	ExecutorClassPlanBackedCLI ExecutorClass = "plan_backed_cli"
	ExecutorClassAPI           ExecutorClass = "api_executor"
	ExecutorClassBroker        ExecutorClass = "broker_executor"
)

type TaskKind string

const (
	TaskKindGeneral  TaskKind = "general"
	TaskKindPlan     TaskKind = "plan"
	TaskKindBuild    TaskKind = "build"
	TaskKindReview   TaskKind = "review"
	TaskKindQA       TaskKind = "qa"
	TaskKindResearch TaskKind = "research"
)

type HealthStatus string

const (
	HealthStatusHealthy     HealthStatus = "healthy"
	HealthStatusDegraded    HealthStatus = "degraded"
	HealthStatusUnavailable HealthStatus = "unavailable"
	HealthStatusUnknown     HealthStatus = "unknown"
)

var ErrNotImplemented = errors.New("executor method not implemented")

type BudgetHints struct {
	MaxInputTokens  int
	MaxOutputTokens int
	MaxCostUSD      float64
}

type ToolPolicy struct {
	Mode    string
	Allowed []string
}

type Requirements struct {
	AllowedClasses      []ExecutorClass
	NeedsResume         bool
	NeedsCancel         bool
	NeedsTools          bool
	NeedsStreaming      bool
	NeedsCostEstimate   bool
	NeedsHeadlessPlan   bool
	NeedsBrokerFallback bool
}

type TaskSpec struct {
	ID           string
	Kind         TaskKind
	Scope        string
	Prompt       string
	Budget       BudgetHints
	Tools        ToolPolicy
	Metadata     map[string]string
	Requirements Requirements
}

type ResumePacket struct {
	Kind    string
	Summary string
	Payload map[string]string
}

type TaskHandle struct {
	ExecutorKey string
	ExternalID  string
	Status      string
}

type ExecutionResult struct {
	Handle   TaskHandle
	Status   string
	Output   string
	Metadata map[string]string
}

type HealthReport struct {
	Status    HealthStatus
	Details   string
	CheckedAt time.Time
}

type CostEstimate struct {
	InputTokens  int
	OutputTokens int
	EstimatedUSD float64
	Currency     string
}

type Capabilities struct {
	ExecutorClass         ExecutorClass
	SupportsResume        bool
	SupportsCancel        bool
	SupportsTools         bool
	SupportsStreaming     bool
	SupportsCostEstimate  bool
	SupportsHeadlessPlan  bool
	SupportsBrokerRouting bool
	TaskKinds             []TaskKind
	Scopes                []string
}

func (capabilities Capabilities) Matches(spec TaskSpec) bool {
	if len(spec.Requirements.AllowedClasses) > 0 && !containsExecutorClass(spec.Requirements.AllowedClasses, capabilities.ExecutorClass) {
		return false
	}
	if len(capabilities.TaskKinds) > 0 && !containsTaskKind(capabilities.TaskKinds, spec.Kind) {
		return false
	}
	if len(capabilities.Scopes) > 0 && !containsString(capabilities.Scopes, spec.Scope) {
		return false
	}
	if spec.Requirements.NeedsResume && !capabilities.SupportsResume {
		return false
	}
	if spec.Requirements.NeedsCancel && !capabilities.SupportsCancel {
		return false
	}
	if spec.Requirements.NeedsTools && !capabilities.SupportsTools {
		return false
	}
	if spec.Requirements.NeedsStreaming && !capabilities.SupportsStreaming {
		return false
	}
	if spec.Requirements.NeedsCostEstimate && !capabilities.SupportsCostEstimate {
		return false
	}
	if spec.Requirements.NeedsHeadlessPlan && !capabilities.SupportsHeadlessPlan {
		return false
	}
	if spec.Requirements.NeedsBrokerFallback && !capabilities.SupportsBrokerRouting {
		return false
	}
	return true
}

type Executor interface {
	Key() string
	Class() ExecutorClass
	Health(context.Context) (HealthReport, error)
	Capabilities(context.Context) (Capabilities, error)
	RunTask(context.Context, TaskSpec) (ExecutionResult, error)
	ResumeTask(context.Context, TaskHandle, ResumePacket) (ExecutionResult, error)
	CancelTask(context.Context, TaskHandle) error
	EstimateCost(context.Context, TaskSpec) (CostEstimate, error)
}

type StaticExecutor struct {
	key          string
	class        ExecutorClass
	health       HealthReport
	capabilities Capabilities
}

func NewStaticExecutor(key string, class ExecutorClass, health HealthReport, capabilities Capabilities) *StaticExecutor {
	capabilities.ExecutorClass = class
	return &StaticExecutor{
		key:          key,
		class:        class,
		health:       health,
		capabilities: capabilities,
	}
}

func (executor *StaticExecutor) Key() string {
	return executor.key
}

func (executor *StaticExecutor) Class() ExecutorClass {
	return executor.class
}

func (executor *StaticExecutor) Health(context.Context) (HealthReport, error) {
	return executor.health, nil
}

func (executor *StaticExecutor) Capabilities(context.Context) (Capabilities, error) {
	return executor.capabilities, nil
}

func (executor *StaticExecutor) RunTask(context.Context, TaskSpec) (ExecutionResult, error) {
	return ExecutionResult{}, ErrNotImplemented
}

func (executor *StaticExecutor) ResumeTask(context.Context, TaskHandle, ResumePacket) (ExecutionResult, error) {
	return ExecutionResult{}, ErrNotImplemented
}

func (executor *StaticExecutor) CancelTask(context.Context, TaskHandle) error {
	return ErrNotImplemented
}

func (executor *StaticExecutor) EstimateCost(context.Context, TaskSpec) (CostEstimate, error) {
	return CostEstimate{}, ErrNotImplemented
}

func containsExecutorClass(items []ExecutorClass, value ExecutorClass) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}

func containsTaskKind(items []TaskKind, value TaskKind) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}

func containsString(items []string, value string) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}
