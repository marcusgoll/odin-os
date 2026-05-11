package recovery

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"odin-os/internal/core/workitems"
	"odin-os/internal/executors/contract"
	"odin-os/internal/runtime/health"
	"odin-os/internal/store/sqlite"
	"odin-os/internal/telemetry/logs"
)

type Service struct {
	Store             *sqlite.Store
	RegistryRoot      string
	ExecutorCatalog   map[string]contract.Executor
	HealthConfig      health.Config
	Logger            *logs.Logger
	Monitor           Monitor
	Diagnoser         Diagnoser
	Executor          Executor
	WorkItems         workitems.Service
	Now               func() time.Time
	ShutdownRequested *atomic.Bool
}

type CycleResult struct {
	Observations []Observation
	Decisions    []Decision
	Outcomes     []Outcome
}

func (service Service) RunCycle(ctx context.Context) (CycleResult, error) {
	if service.ShutdownRequested != nil && service.ShutdownRequested.Load() {
		return CycleResult{}, nil
	}
	if service.Store == nil {
		return CycleResult{}, fmt.Errorf("self-heal store is required")
	}

	now := time.Now().UTC()
	if service.Now != nil {
		now = service.Now().UTC()
	}
	healthConfig := service.HealthConfig
	if healthConfig == (health.Config{}) {
		healthConfig = health.DefaultConfig()
	}

	monitor := service.Monitor
	if monitor.DB == nil {
		monitor.DB = service.Store.DB()
	}
	if monitor.Now == nil {
		monitor.Now = func() time.Time { return now }
	}
	if monitor.Config == (Config{}) {
		monitor.Config = Config{
			QueuePressureThreshold:      healthConfig.QueuePressureThreshold,
			ExecutorFreshnessTTL:        healthConfig.ExecutorFreshnessTTL,
			ProjectionFreshnessTTL:      healthConfig.ProjectionFreshnessTTL,
			SourceFreshnessTTL:          healthConfig.SourceFreshnessTTL,
			RepeatedRunFailureThreshold: DefaultConfig().RepeatedRunFailureThreshold,
		}
	}
	if monitor.Config.QueuePressureThreshold == 0 {
		monitor.Config.QueuePressureThreshold = healthConfig.QueuePressureThreshold
	}

	observations, err := monitor.Observe(ctx)
	if err != nil {
		return CycleResult{}, err
	}

	diagnoser := service.Diagnoser
	decisions := diagnoser.Diagnose(observations)

	executor := service.Executor
	if executor.Store == nil {
		executor.Store = service.Store
	}
	if executor.Now == nil {
		executor.Now = func() time.Time { return now }
	}
	if executor.Playbooks == nil {
		executor.Playbooks = NewBuiltinPlaybooks(BuiltinDependencies{
			Store:           service.Store,
			RegistryRoot:    service.RegistryRoot,
			ExecutorCatalog: service.ExecutorCatalog,
			HealthConfig:    healthConfig,
		})
	}

	result := CycleResult{
		Observations: observations,
		Decisions:    decisions,
	}
	for _, decision := range decisions {
		decision = normalizeDecision(decision)
		if decision.Mode == DecisionModeIgnore {
			service.logDecision(now, decision, string(DecisionModeIgnore), "")
			continue
		}
		outcome, err := executor.Execute(ctx, decision)
		if err != nil {
			service.logDecision(now, decision, "error", err.Error())
			return result, err
		}
		result.Outcomes = append(result.Outcomes, outcome)
		service.logDecision(now, decision, outcome.Status, "")
	}

	return result, nil
}

func (service Service) workItemService() workitems.Service {
	if service.WorkItems.Store == nil {
		service.WorkItems.Store = service.Store
	}
	return service.WorkItems
}

func (service Service) logDecision(now time.Time, decision Decision, status string, errMessage string) {
	if service.Logger == nil || service.Logger.Writer == nil {
		return
	}

	level := logs.LevelInfo
	message := "self-heal decision handled"
	if status == "escalated" || status == "suppressed" {
		level = logs.LevelWarn
	}
	if errMessage != "" {
		level = logs.LevelError
		message = "self-heal decision failed"
	}

	fields := map[string]any{
		"fault_key":     decision.Observation.FaultKey,
		"subject_key":   decision.Observation.SubjectKey,
		"decision_mode": decision.Mode,
		"status":        status,
		"error":         errMessage,
		"observed_at":   now.Format(time.RFC3339Nano),
	}
	if decision.Playbook != "" {
		fields["playbook"] = decision.Playbook
	}
	if decision.NextAction != "" {
		fields["next_action"] = decision.NextAction
	}

	_ = service.Logger.Log(logs.Record{
		Level:         level,
		Component:     "self_heal",
		Message:       message,
		CorrelationID: fmt.Sprintf("self-heal:%s:%s", decision.Observation.FaultKey, decision.Observation.SubjectKey),
		Scope:         decision.Observation.Scope,
		ProjectID:     decision.Observation.ProjectID,
		TaskID:        decision.Observation.TaskID,
		RunID:         decision.Observation.RunID,
		Fields:        fields,
	})
}
