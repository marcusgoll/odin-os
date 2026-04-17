package supervision

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"odin-os/internal/core/workitems"
	runtimejobs "odin-os/internal/runtime/jobs"
	"odin-os/internal/store/sqlite"
)

const (
	TriggerParallelResearch = "parallel_research"
	TriggerBuildPlusReview  = "build_plus_review"
	TriggerMultiArtifact    = "multi_artifact"
	TriggerMonitorTriage    = "monitor_triage"
)

var (
	ErrSwarmTriggerNotAdmitted      = errors.New("no valid swarm trigger")
	ErrSwarmRecursiveDelegation     = errors.New("task is already a child delegation")
	ErrSwarmParentCompanionRequired = errors.New("swarm parent must be companion-owned")
	ErrInvalidDelegationPlan        = errors.New("invalid delegation plan")
)

type PlanSwarmParams struct {
	ParentTaskID    int64
	ParentRunID     *int64
	Trigger         string
	ConvergenceMode string
	RequestedBudget int
	RetryBudget     int
	DelegationPlans []DelegationPlan
}

type DelegationPlan struct {
	DelegationKey         string
	Role                  string
	ActionClass           string
	ActionKey             string
	MutationMode          string
	ArtifactTarget        string
	Objective             string
	AcceptanceCriteria    []string
	RequestedTools        []string
	RequestedMemoryScopes []string
}

type SwarmPlan struct {
	ParentTask  sqlite.Task
	ParentRunID *int64
	Trigger     string
	MaxChildren int
	Delegations []sqlite.Delegation
}

type MaterializedSwarm struct {
	Plan        SwarmPlan
	Delegations []sqlite.Delegation
	Tasks       []sqlite.Task
}

type planningPolicy struct {
	Swarm struct {
		MaxChildren int `json:"max_children"`
	} `json:"swarm"`
}

func (service Service) PlanSwarm(ctx context.Context, params PlanSwarmParams) (SwarmPlan, error) {
	if service.Store == nil {
		return SwarmPlan{}, fmt.Errorf("supervision store is required")
	}
	if params.ParentTaskID <= 0 {
		return SwarmPlan{}, fmt.Errorf("parent task is required")
	}

	parentTask, err := service.Store.GetTask(ctx, params.ParentTaskID)
	if err != nil {
		return SwarmPlan{}, err
	}
	if err := service.ensureNotDelegatedChild(ctx, parentTask.ID); err != nil {
		return SwarmPlan{}, err
	}
	if !isSupportedConvergenceMode(params.ConvergenceMode) {
		return SwarmPlan{}, fmt.Errorf("unsupported convergence mode %q", params.ConvergenceMode)
	}

	parentRunID, err := service.resolveParentRunID(ctx, parentTask, params.ParentRunID)
	if err != nil {
		return SwarmPlan{}, err
	}

	companion, err := service.parentCompanion(ctx, parentTask.CompanionID)
	if err != nil {
		return SwarmPlan{}, err
	}

	maxChildren, err := resolveMaxChildren(companion.PlanningPolicyJSON, params.RequestedBudget, len(params.DelegationPlans))
	if err != nil {
		return SwarmPlan{}, err
	}
	selectedPlans := boundedDelegationPlans(params.DelegationPlans, maxChildren)
	if err := validateDelegationPlans(selectedPlans); err != nil {
		return SwarmPlan{}, err
	}
	if err := validateSwarmTrigger(params.Trigger, selectedPlans); err != nil {
		return SwarmPlan{}, err
	}

	jobService := service.Jobs
	if jobService.Store == nil {
		jobService.Store = service.Store
	}

	createParams := make([]sqlite.CreateDelegationParams, 0, len(selectedPlans))
	for _, plan := range selectedPlans {
		admission, err := jobService.NarrowDelegationAdmission(runtimejobs.DelegationAdmissionInput{
			ParentTask:            parentTask,
			ParentRunID:           parentRunID,
			Companion:             companion,
			RequestedTools:        plan.RequestedTools,
			RequestedMemoryScopes: plan.RequestedMemoryScopes,
		})
		if err != nil {
			return SwarmPlan{}, err
		}

		detailsJSON, err := marshalDelegationDetails(parentTask, parentRunID, params, maxChildren, plan, admission)
		if err != nil {
			return SwarmPlan{}, err
		}

		createParams = append(createParams, sqlite.CreateDelegationParams{
			ParentTaskID:    parentTask.ID,
			ParentRunID:     parentRunID,
			ProjectID:       parentTask.ProjectID,
			Scope:           parentTask.Scope,
			DelegationKey:   strings.TrimSpace(plan.DelegationKey),
			Role:            strings.TrimSpace(plan.Role),
			ActionClass:     strings.TrimSpace(plan.ActionClass),
			ActionKey:       strings.TrimSpace(plan.ActionKey),
			MutationMode:    strings.TrimSpace(plan.MutationMode),
			Status:          "queued",
			ConvergenceMode: params.ConvergenceMode,
			ArtifactTarget:  strings.TrimSpace(plan.ArtifactTarget),
			Executor:        admission.Executor,
			DetailsJSON:     detailsJSON,
		})
	}

	delegations, err := service.Store.CreateDelegations(ctx, createParams)
	if err != nil {
		return SwarmPlan{}, err
	}

	plan := SwarmPlan{
		ParentTask:  parentTask,
		ParentRunID: parentRunID,
		Trigger:     params.Trigger,
		MaxChildren: maxChildren,
		Delegations: delegations,
	}

	materialized, err := service.MaterializeSwarm(ctx, plan)
	if err != nil {
		return SwarmPlan{}, err
	}
	return materialized.Plan, nil
}

func (service Service) MaterializeSwarm(ctx context.Context, plan SwarmPlan) (MaterializedSwarm, error) {
	if service.Store == nil {
		return MaterializedSwarm{}, fmt.Errorf("supervision store is required")
	}
	if plan.ParentTask.ID <= 0 {
		return MaterializedSwarm{}, fmt.Errorf("swarm parent task is required")
	}

	workItemService := workitems.Service{Store: service.Store}
	result := MaterializedSwarm{
		Plan:        plan,
		Delegations: make([]sqlite.Delegation, 0, len(plan.Delegations)),
		Tasks:       make([]sqlite.Task, 0, len(plan.Delegations)),
	}

	for _, delegation := range plan.Delegations {
		task, updatedDelegation, err := service.materializeDelegationTask(ctx, workItemService, plan.ParentTask, delegation)
		if err != nil {
			return MaterializedSwarm{}, err
		}
		result.Tasks = append(result.Tasks, task)
		result.Delegations = append(result.Delegations, updatedDelegation)
	}
	result.Plan.Delegations = append([]sqlite.Delegation(nil), result.Delegations...)
	return result, nil
}

func (service Service) ensureNotDelegatedChild(ctx context.Context, taskID int64) error {
	delegations, err := service.Store.ListDelegations(ctx, sqlite.ListDelegationsParams{
		ChildTaskID: &taskID,
	})
	if err != nil {
		return err
	}
	if len(delegations) > 0 {
		return ErrSwarmRecursiveDelegation
	}
	return nil
}

func (service Service) resolveParentRunID(ctx context.Context, parentTask sqlite.Task, requested *int64) (*int64, error) {
	if requested == nil {
		return nil, nil
	}
	run, err := service.Store.GetRun(ctx, *requested)
	if err != nil {
		return nil, err
	}
	if run.TaskID != parentTask.ID {
		return nil, fmt.Errorf("run %d does not belong to task %d", run.ID, parentTask.ID)
	}
	return requested, nil
}

func (service Service) parentCompanion(ctx context.Context, companionID *int64) (sqlite.Companion, error) {
	if companionID == nil {
		return sqlite.Companion{}, ErrSwarmParentCompanionRequired
	}
	return service.Store.GetCompanionByID(ctx, *companionID)
}

func resolveMaxChildren(rawPlanningPolicy string, requestedBudget int, availablePlans int) (int, error) {
	if requestedBudget <= 0 {
		return 0, fmt.Errorf("%w: requested child budget is required", ErrInvalidDelegationPlan)
	}
	policy := planningPolicy{}
	trimmed := strings.TrimSpace(rawPlanningPolicy)
	if trimmed != "" && trimmed != "{}" {
		if err := json.Unmarshal([]byte(trimmed), &policy); err != nil {
			return 0, fmt.Errorf("invalid companion planning policy JSON: %w", err)
		}
	}

	maxChildren := requestedBudget
	if policy.Swarm.MaxChildren > 0 && (maxChildren <= 0 || policy.Swarm.MaxChildren < maxChildren) {
		maxChildren = policy.Swarm.MaxChildren
	}
	if maxChildren <= 0 {
		return 0, fmt.Errorf("swarm child budget is required")
	}
	if availablePlans > 0 && maxChildren > availablePlans {
		maxChildren = availablePlans
	}
	return maxChildren, nil
}

func boundedDelegationPlans(plans []DelegationPlan, maxChildren int) []DelegationPlan {
	if len(plans) <= maxChildren {
		return append([]DelegationPlan(nil), plans...)
	}
	return append([]DelegationPlan(nil), plans[:maxChildren]...)
}

func validateSwarmTrigger(trigger string, plans []DelegationPlan) error {
	trigger = strings.TrimSpace(trigger)
	if len(plans) < 2 {
		return ErrSwarmTriggerNotAdmitted
	}

	switch trigger {
	case TriggerParallelResearch:
		return nil
	case TriggerBuildPlusReview:
		for _, plan := range plans {
			role := strings.ToLower(strings.TrimSpace(plan.Role))
			actionKey := strings.ToLower(strings.TrimSpace(plan.ActionKey))
			if strings.Contains(role, "review") || strings.Contains(actionKey, "review") {
				return nil
			}
		}
		return ErrSwarmTriggerNotAdmitted
	case TriggerMultiArtifact:
		seen := make(map[string]struct{}, len(plans))
		for _, plan := range plans {
			target := strings.TrimSpace(plan.ArtifactTarget)
			if target == "" {
				continue
			}
			seen[target] = struct{}{}
		}
		if len(seen) < 2 {
			return ErrSwarmTriggerNotAdmitted
		}
		return nil
	case TriggerMonitorTriage:
		seenRoles := make(map[string]struct{}, len(plans))
		for _, plan := range plans {
			role := strings.TrimSpace(plan.Role)
			if role == "" {
				continue
			}
			seenRoles[role] = struct{}{}
		}
		if len(seenRoles) < 2 {
			return ErrSwarmTriggerNotAdmitted
		}
		return nil
	default:
		return ErrSwarmTriggerNotAdmitted
	}
}

func isSupportedConvergenceMode(mode string) bool {
	switch strings.TrimSpace(mode) {
	case "merge", "review_gate", "rank", "quorum":
		return true
	default:
		return false
	}
}

func validateDelegationPlans(plans []DelegationPlan) error {
	for _, plan := range plans {
		if strings.TrimSpace(plan.DelegationKey) == "" {
			return fmt.Errorf("%w: delegation_key is required", ErrInvalidDelegationPlan)
		}
		if strings.TrimSpace(plan.Role) == "" {
			return fmt.Errorf("%w: role is required", ErrInvalidDelegationPlan)
		}
		if strings.TrimSpace(plan.ActionClass) == "" {
			return fmt.Errorf("%w: action_class is required", ErrInvalidDelegationPlan)
		}
		if strings.TrimSpace(plan.ActionKey) == "" {
			return fmt.Errorf("%w: action_key is required", ErrInvalidDelegationPlan)
		}
		if strings.TrimSpace(plan.MutationMode) == "" {
			return fmt.Errorf("%w: mutation_mode is required", ErrInvalidDelegationPlan)
		}
		if strings.TrimSpace(plan.ArtifactTarget) == "" {
			return fmt.Errorf("%w: artifact_target is required", ErrInvalidDelegationPlan)
		}
		if strings.TrimSpace(plan.Objective) == "" {
			return fmt.Errorf("%w: objective is required", ErrInvalidDelegationPlan)
		}
	}
	return nil
}

func (service Service) materializeDelegationTask(ctx context.Context, workItemService workitems.Service, parentTask sqlite.Task, delegation sqlite.Delegation) (sqlite.Task, sqlite.Delegation, error) {
	if delegation.ChildTaskID != nil {
		task, err := service.Store.GetTask(ctx, *delegation.ChildTaskID)
		if err != nil {
			return sqlite.Task{}, sqlite.Delegation{}, err
		}
		return task, delegation, nil
	}

	task, err := workItemService.QueueDelegatedChild(ctx, workitems.QueueDelegatedChildParams{
		ParentTask: parentTask,
		Delegation: delegation,
		Objective:  delegationObjective(delegation.DetailsJSON),
	})
	if err != nil {
		return sqlite.Task{}, sqlite.Delegation{}, err
	}

	updatedDelegation, err := service.Store.AttachDelegationChildTask(ctx, sqlite.AttachDelegationChildTaskParams{
		DelegationID: delegation.ID,
		ChildTaskID:  task.ID,
	})
	if err != nil {
		return sqlite.Task{}, sqlite.Delegation{}, err
	}
	return task, updatedDelegation, nil
}

func marshalDelegationDetails(parentTask sqlite.Task, parentRunID *int64, params PlanSwarmParams, maxChildren int, plan DelegationPlan, admission runtimejobs.DelegationAdmissionProfile) (string, error) {
	type parentRef struct {
		TaskID       int64  `json:"task_id"`
		RunID        *int64 `json:"run_id,omitempty"`
		WorkspaceID  *int64 `json:"workspace_id,omitempty"`
		InitiativeID *int64 `json:"initiative_id,omitempty"`
		CompanionID  *int64 `json:"companion_id,omitempty"`
	}
	payload := map[string]any{
		"objective":           strings.TrimSpace(plan.Objective),
		"acceptance_criteria": uniqueNonEmptyStrings(plan.AcceptanceCriteria),
		"swarm": map[string]any{
			"trigger":          strings.TrimSpace(params.Trigger),
			"max_children":     maxChildren,
			"requested_budget": params.RequestedBudget,
			"retry_budget":     params.RetryBudget,
			"convergence_mode": strings.TrimSpace(params.ConvergenceMode),
		},
		"admission": map[string]any{
			"executor":      admission.Executor,
			"allowed_tools": admission.AllowedTools,
			"memory_view": map[string]any{
				"mode":   admission.MemoryView.Mode,
				"scopes": admission.MemoryView.Scopes,
			},
		},
		"parent": parentRef{
			TaskID:       parentTask.ID,
			RunID:        parentRunID,
			WorkspaceID:  parentTask.WorkspaceID,
			InitiativeID: parentTask.InitiativeID,
			CompanionID:  parentTask.CompanionID,
		},
	}
	return marshalJSON(payload)
}

func marshalJSON(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func uniqueNonEmptyStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	filtered := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		filtered = append(filtered, value)
	}
	return filtered
}

func delegationObjective(detailsJSON string) string {
	type payload struct {
		Objective string `json:"objective"`
	}

	var decoded payload
	if err := json.Unmarshal([]byte(detailsJSON), &decoded); err != nil {
		return ""
	}
	return strings.TrimSpace(decoded.Objective)
}
