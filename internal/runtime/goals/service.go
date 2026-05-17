package goals

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"odin-os/internal/store/sqlite"
)

type Service struct {
	Store *sqlite.Store
}

type GoalRunState struct {
	Goal      sqlite.Goal
	ActiveRun *sqlite.GoalRun
	Runs      []sqlite.GoalRun
}

type StartDecision struct {
	Allowed     bool
	Reason      string
	ActiveRunID *int64
}

const (
	TickActionObserved = "observed"
	TickActionSkipped  = "skipped"
	TickActionStarted  = "started"
	TickActionBlocked  = "blocked"

	TickReasonCreatedNeedsPlanning = "created_goal_requires_planning"
	TickReasonApprovalRequired     = "approval_required"
	TickReasonApprovedStarted      = "approved_for_execution_started"
	TickReasonAutonomousApproved   = "autonomous_policy_approved"
	TickReasonActiveRunExists      = "active_goal_run_exists"
	TickReasonNextWakePending      = "next_wake_pending"
	TickReasonNoExecutor           = "no_executor"
	TickReasonStatusSkipped        = "status_skipped"
)

type TickResult struct {
	Observed int              `json:"observed"`
	Started  int              `json:"started"`
	Blocked  int              `json:"blocked"`
	Skipped  int              `json:"skipped"`
	Results  []TickGoalResult `json:"results"`
}

type TickGoalResult struct {
	GoalID         int64             `json:"goal_id"`
	PreviousStatus sqlite.GoalStatus `json:"previous_status"`
	Status         sqlite.GoalStatus `json:"status"`
	Action         string            `json:"action"`
	Reason         string            `json:"reason,omitempty"`
	GoalRunID      *int64            `json:"goal_run_id,omitempty"`
}

func NewService(store *sqlite.Store) Service {
	return Service{Store: store}
}

func (service Service) GetGoalRunState(ctx context.Context, goalID int64) (GoalRunState, error) {
	if service.Store == nil {
		return GoalRunState{}, fmt.Errorf("goal service requires store")
	}
	goal, err := service.Store.GetGoal(ctx, goalID)
	if err != nil {
		return GoalRunState{}, err
	}
	runs, err := service.Store.ListGoalRunsByGoalID(ctx, goalID)
	if err != nil {
		return GoalRunState{}, err
	}
	activeRun, err := service.Store.GetActiveGoalRunByGoalID(ctx, goalID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return GoalRunState{}, err
	}
	state := GoalRunState{
		Goal: goal,
		Runs: runs,
	}
	if err == nil {
		state.ActiveRun = &activeRun
	}
	return state, nil
}

func (service Service) CanStartRun(ctx context.Context, goalID int64) (StartDecision, error) {
	state, err := service.GetGoalRunState(ctx, goalID)
	if err != nil {
		return StartDecision{}, err
	}
	if state.ActiveRun != nil {
		return StartDecision{
			Allowed:     false,
			Reason:      "active_goal_run_exists",
			ActiveRunID: &state.ActiveRun.ID,
		}, nil
	}
	return StartDecision{
		Allowed: true,
		Reason:  "no_active_goal_run",
	}, nil
}

func (service Service) Tick(ctx context.Context) (TickResult, error) {
	if service.Store == nil {
		return TickResult{}, fmt.Errorf("goal service requires store")
	}
	goals, err := service.Store.ListGoals(ctx, sqlite.ListGoalsParams{})
	if err != nil {
		return TickResult{}, err
	}
	result := TickResult{
		Results: make([]TickGoalResult, 0, len(goals)),
	}
	for _, goal := range goals {
		goalResult, err := service.tickGoal(ctx, goal)
		if err != nil {
			return TickResult{}, err
		}
		result.Observed++
		switch goalResult.Action {
		case TickActionStarted:
			result.Started++
		case TickActionBlocked:
			result.Blocked++
		case TickActionSkipped:
			result.Skipped++
		}
		result.Results = append(result.Results, goalResult)
	}
	return result, nil
}

func (service Service) tickGoal(ctx context.Context, goal sqlite.Goal) (TickGoalResult, error) {
	switch goal.Status {
	case sqlite.GoalStatusCreated:
		if decision := ClassifyAutoPolicy(goal); decision.AutoStart {
			return service.approveAndStartAutonomousGoal(ctx, goal, decision)
		}
		return service.observe(ctx, goal, TickActionObserved, TickReasonCreatedNeedsPlanning, goal.Status, nil)
	case sqlite.GoalStatusPlanned:
		if decision := ClassifyAutoPolicy(goal); decision.AutoStart {
			return service.approveAndStartAutonomousGoal(ctx, goal, decision)
		}
		return service.observe(ctx, goal, TickActionSkipped, TickReasonApprovalRequired, goal.Status, nil)
	case sqlite.GoalStatusApprovedForExecution:
		return service.startApprovedGoal(ctx, goal)
	case sqlite.GoalStatusRunning:
		return service.blockRunningGoalWithoutExecutor(ctx, goal)
	case sqlite.GoalStatusBlocked:
		return service.recoverBlockedGoalWithExecutor(ctx, goal)
	case sqlite.GoalStatusCompleted, sqlite.GoalStatusWaitingForHuman, sqlite.GoalStatusWaitingForExternal, sqlite.GoalStatusVerifying:
		return service.observe(ctx, goal, TickActionSkipped, TickReasonStatusSkipped, goal.Status, nil)
	default:
		return service.observe(ctx, goal, TickActionSkipped, TickReasonStatusSkipped, goal.Status, nil)
	}
}

func (service Service) approveAndStartAutonomousGoal(ctx context.Context, goal sqlite.Goal, decision AutoPolicyDecision) (TickGoalResult, error) {
	approved, err := service.approveAutonomousGoal(ctx, goal, decision)
	if err != nil {
		return TickGoalResult{}, err
	}
	return service.startApprovedGoal(ctx, approved)
}

func (service Service) approveAutonomousGoal(ctx context.Context, goal sqlite.Goal, decision AutoPolicyDecision) (sqlite.Goal, error) {
	current := goal
	var err error
	if goal.Status == sqlite.GoalStatusCreated {
		current, err = service.Store.TransitionGoal(ctx, sqlite.TransitionGoalParams{
			GoalID: goal.ID,
			Status: sqlite.GoalStatusPlanned,
			Actor:  "goal_runner",
			Reason: TickReasonAutonomousApproved,
		})
		if err != nil {
			return sqlite.Goal{}, err
		}
	}
	if current.Status == sqlite.GoalStatusPlanned {
		current, err = service.Store.TransitionGoal(ctx, sqlite.TransitionGoalParams{
			GoalID: current.ID,
			Status: sqlite.GoalStatusApprovedForExecution,
			Actor:  "goal_runner",
			Reason: TickReasonAutonomousApproved,
		})
		if err != nil {
			return sqlite.Goal{}, err
		}
	}
	payload, err := json.Marshal(map[string]string{
		"reason":           decision.Reason,
		"project_key":      decision.ProjectKey,
		"execution_intent": decision.ExecutionIntent,
	})
	if err != nil {
		return sqlite.Goal{}, err
	}
	if _, err := service.Store.AddGoalEvidence(ctx, sqlite.AddGoalEvidenceParams{
		GoalID:       current.ID,
		EvidenceType: "goal_auto_policy",
		Summary:      "goal runner auto-approved low-risk read-only goal",
		PayloadJSON:  string(payload),
		CreatedBy:    "goal_runner",
	}); err != nil {
		return sqlite.Goal{}, err
	}
	return current, nil
}

func (service Service) startApprovedGoal(ctx context.Context, goal sqlite.Goal) (TickGoalResult, error) {
	decision, err := service.CanStartRun(ctx, goal.ID)
	if err != nil {
		return TickGoalResult{}, err
	}
	if !decision.Allowed {
		return service.observe(ctx, goal, TickActionSkipped, TickReasonActiveRunExists, goal.Status, decision.ActiveRunID)
	}
	run, err := service.Store.CreateGoalRun(ctx, sqlite.CreateGoalRunParams{
		GoalID:      goal.ID,
		Status:      sqlite.GoalRunStatusRunning,
		Executor:    "goal_runner",
		Attempts:    1,
		MaxAttempts: 1,
		LeaseOwner:  "goal_tick",
	})
	if err != nil {
		return TickGoalResult{}, err
	}
	updated, err := service.Store.TransitionGoal(ctx, sqlite.TransitionGoalParams{
		GoalID: goal.ID,
		Status: sqlite.GoalStatusRunning,
		Actor:  "goal_runner",
		Reason: TickReasonApprovedStarted,
	})
	if err != nil {
		return TickGoalResult{}, err
	}
	runID := run.ID
	return service.observe(ctx, goal, TickActionStarted, TickReasonApprovedStarted, updated.Status, &runID)
}

func (service Service) blockRunningGoalWithoutExecutor(ctx context.Context, goal sqlite.Goal) (TickGoalResult, error) {
	var runID *int64
	activeRun, err := service.Store.GetActiveGoalRunByGoalID(ctx, goal.ID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return TickGoalResult{}, err
	}
	if err == nil {
		runID = &activeRun.ID
		if activeRun.NextWakeAt != nil && activeRun.NextWakeAt.After(time.Now().UTC()) {
			return service.observe(ctx, goal, TickActionSkipped, TickReasonNextWakePending, goal.Status, runID)
		}
		if activeRun.Executor != "" {
			return service.observe(ctx, goal, TickActionSkipped, TickReasonActiveRunExists, goal.Status, runID)
		}
		if _, err := service.Store.UpdateGoalRunStatus(ctx, sqlite.UpdateGoalRunStatusParams{
			GoalRunID: activeRun.ID,
			Status:    sqlite.GoalRunStatusWaitingForExternal,
			Summary:   "no executor/action available",
		}); err != nil {
			return TickGoalResult{}, err
		}
	}
	if _, err := service.Store.AddGoalBlocker(ctx, sqlite.AddGoalBlockerParams{
		GoalID:      goal.ID,
		Status:      "open",
		BlockerType: "missing_executor",
		Summary:     "no executor/action available",
		DetailsJSON: `{"reason":"no_executor"}`,
		CreatedBy:   "goal_runner",
	}); err != nil {
		return TickGoalResult{}, err
	}
	if _, err := service.Store.AddGoalEvidence(ctx, sqlite.AddGoalEvidenceParams{
		GoalID:       goal.ID,
		GoalRunID:    runID,
		EvidenceType: "goal_runner_tick",
		Summary:      "deterministic tick observed no executor/action available",
		PayloadJSON:  `{"reason":"no_executor"}`,
		CreatedBy:    "goal_runner",
	}); err != nil {
		return TickGoalResult{}, err
	}
	updated, err := service.Store.TransitionGoal(ctx, sqlite.TransitionGoalParams{
		GoalID: goal.ID,
		Status: sqlite.GoalStatusBlocked,
		Actor:  "goal_runner",
		Reason: TickReasonNoExecutor,
	})
	if err != nil {
		return TickGoalResult{}, err
	}
	return service.observe(ctx, goal, TickActionBlocked, TickReasonNoExecutor, updated.Status, runID)
}

func (service Service) recoverBlockedGoalWithExecutor(ctx context.Context, goal sqlite.Goal) (TickGoalResult, error) {
	activeRun, err := service.Store.GetActiveGoalRunByGoalID(ctx, goal.ID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return service.observe(ctx, goal, TickActionSkipped, TickReasonStatusSkipped, goal.Status, nil)
		}
		return TickGoalResult{}, err
	}
	runID := activeRun.ID
	if activeRun.Executor == "" {
		return service.observe(ctx, goal, TickActionSkipped, TickReasonStatusSkipped, goal.Status, &runID)
	}
	if activeRun.Status != sqlite.GoalRunStatusRunning {
		if _, err := service.Store.UpdateGoalRunStatus(ctx, sqlite.UpdateGoalRunStatusParams{
			GoalRunID: activeRun.ID,
			Status:    sqlite.GoalRunStatusRunning,
			Summary:   "executor-backed goal run recovered",
		}); err != nil {
			return TickGoalResult{}, err
		}
	}
	updated, err := service.Store.TransitionGoal(ctx, sqlite.TransitionGoalParams{
		GoalID: goal.ID,
		Status: sqlite.GoalStatusRunning,
		Actor:  "goal_runner",
		Reason: "executor_backed_run_recovered",
	})
	if err != nil {
		return TickGoalResult{}, err
	}
	if _, err := service.Store.AddGoalEvidence(ctx, sqlite.AddGoalEvidenceParams{
		GoalID:       goal.ID,
		GoalRunID:    &runID,
		EvidenceType: "goal_runner_recovery",
		Summary:      "executor-backed goal run recovered from missing-executor blocker",
		PayloadJSON:  `{"reason":"executor_backed_run_recovered"}`,
		CreatedBy:    "goal_runner",
	}); err != nil {
		return TickGoalResult{}, err
	}
	return service.observe(ctx, goal, TickActionSkipped, TickReasonActiveRunExists, updated.Status, &runID)
}

func (service Service) observe(ctx context.Context, goal sqlite.Goal, action string, reason string, status sqlite.GoalStatus, goalRunID *int64) (TickGoalResult, error) {
	if err := service.Store.RecordGoalRunnerObserved(ctx, sqlite.RecordGoalRunnerObservedParams{
		GoalID: goal.ID,
		Action: action,
		Reason: reason,
		Actor:  "goal_runner",
	}); err != nil {
		return TickGoalResult{}, err
	}
	return TickGoalResult{
		GoalID:         goal.ID,
		PreviousStatus: goal.Status,
		Status:         status,
		Action:         action,
		Reason:         reason,
		GoalRunID:      goalRunID,
	}, nil
}
