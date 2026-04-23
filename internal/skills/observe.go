package skills

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"odin-os/internal/telemetry/logs"
)

type Operation string

const (
	OperationList   Operation = "list"
	OperationGet    Operation = "get"
	OperationCreate Operation = "create"
	OperationUpdate Operation = "update"
	OperationDelete Operation = "delete"
	OperationInvoke Operation = "invoke"
)

type Outcome string

const (
	OutcomeSuccess Outcome = "success"
	OutcomeFailure Outcome = "failure"
)

type Event struct {
	Operation        Operation
	Outcome          Outcome
	SkillKey         string
	Scope            string
	ExecutionProfile string
	Version          string
	HandlerType      string
	HandlerRef       string
	Permissions      []string
	Duration         time.Duration
	ErrorCode        string
	ErrorText        string
}

type Observer interface {
	RecordSkillEvent(context.Context, Event)
}

type ObserverFunc func(context.Context, Event)

func (fn ObserverFunc) RecordSkillEvent(ctx context.Context, event Event) {
	fn(ctx, event)
}

type MultiObserver []Observer

func (observers MultiObserver) RecordSkillEvent(ctx context.Context, event Event) {
	for _, observer := range observers {
		if observer == nil {
			continue
		}
		observer.RecordSkillEvent(ctx, event)
	}
}

type LoggerObserver struct {
	Logger        logs.Logger
	Scope         string
	Component     string
	CorrelationID string
}

func (observer LoggerObserver) RecordSkillEvent(_ context.Context, event Event) {
	if observer.Logger.Writer == nil {
		return
	}

	component := observer.Component
	if strings.TrimSpace(component) == "" {
		component = "skills"
	}
	scope := observer.Scope
	if strings.TrimSpace(event.Scope) != "" {
		scope = event.Scope
	}
	if strings.TrimSpace(scope) == "" {
		scope = "repo"
	}

	fields := map[string]any{
		"operation":        event.Operation,
		"outcome":          event.Outcome,
		"skill_key":        event.SkillKey,
		"duration_ms":      event.Duration.Milliseconds(),
		"version":          event.Version,
		"handler_type":     event.HandlerType,
		"handler_ref":      event.HandlerRef,
		"permission_count": len(event.Permissions),
	}
	if event.ExecutionProfile != "" {
		fields["execution_profile"] = event.ExecutionProfile
	}
	if event.ErrorCode != "" {
		fields["error_code"] = event.ErrorCode
	}
	if event.ErrorText != "" {
		fields["error"] = event.ErrorText
	}

	level := logs.LevelInfo
	message := fmt.Sprintf("skill %s %s", event.Operation, event.Outcome)
	if event.Outcome == OutcomeFailure {
		level = logs.LevelWarn
	}

	_ = observer.Logger.Log(logs.Record{
		Level:         level,
		Component:     component,
		Message:       message,
		CorrelationID: observer.CorrelationID,
		Scope:         scope,
		Fields:        fields,
	})
}

type CounterObserver struct {
	mu     sync.Mutex
	counts map[Operation]map[Outcome]int64
}

func NewCounterObserver() *CounterObserver {
	return &CounterObserver{
		counts: make(map[Operation]map[Outcome]int64),
	}
}

func (observer *CounterObserver) RecordSkillEvent(_ context.Context, event Event) {
	if observer == nil {
		return
	}
	observer.mu.Lock()
	defer observer.mu.Unlock()

	outcomes := observer.counts[event.Operation]
	if outcomes == nil {
		outcomes = make(map[Outcome]int64)
		observer.counts[event.Operation] = outcomes
	}
	outcomes[event.Outcome]++
}

func (observer *CounterObserver) Snapshot() map[Operation]map[Outcome]int64 {
	if observer == nil {
		return nil
	}
	observer.mu.Lock()
	defer observer.mu.Unlock()

	snapshot := make(map[Operation]map[Outcome]int64, len(observer.counts))
	for operation, outcomes := range observer.counts {
		cloned := make(map[Outcome]int64, len(outcomes))
		for outcome, count := range outcomes {
			cloned[outcome] = count
		}
		snapshot[operation] = cloned
	}
	return snapshot
}
