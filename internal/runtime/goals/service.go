package goals

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
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
	TickActionPlanned  = "planned"

	TickReasonCreatedNeedsPlanning = "created_goal_requires_planning"
	TickReasonPlanRecorded         = "implementation_plan_recorded"
	TickReasonApprovalRequired     = "approval_required"
	TickReasonApprovedStarted      = "approved_for_execution_started"
	TickReasonAutonomousApproved   = "autonomous_policy_approved"
	TickReasonActiveRunExists      = "active_goal_run_exists"
	TickReasonNextWakePending      = "next_wake_pending"
	TickReasonNoExecutor           = "no_executor"
	TickReasonStatusSkipped        = "status_skipped"
	TickReasonWorkItemMaterialized = "work_item_materialized"
)

type TickResult struct {
	Observed int              `json:"observed"`
	Planned  int              `json:"planned,omitempty"`
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
	WorkItemID     *int64            `json:"work_item_id,omitempty"`
	WorkItemKey    string            `json:"work_item_key,omitempty"`
}

type goalWorkItemRef struct {
	ID  int64
	Key string
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
		case TickActionPlanned:
			result.Planned++
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
		decision := ClassifyAutoPolicy(goal)
		if decision.AutoStart {
			return service.approveAndStartAutonomousGoal(ctx, goal, decision)
		}
		return service.planCreatedGoal(ctx, goal, decision)
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

func (service Service) planCreatedGoal(ctx context.Context, goal sqlite.Goal, decision AutoPolicyDecision) (TickGoalResult, error) {
	payload, err := json.Marshal(map[string]any{
		"reason":              decision.Reason,
		"project_key":         decision.ProjectKey,
		"execution_intent":    "approval_gated",
		"approval_review_id":  fmt.Sprintf("goal-approval:%d", goal.ID),
		"generated_by":        "goal_runner",
		"implementation_plan": defaultGoalImplementationPlan(goal, decision),
	})
	if err != nil {
		return TickGoalResult{}, err
	}
	if _, err := service.Store.AddGoalEvidence(ctx, sqlite.AddGoalEvidenceParams{
		GoalID:       goal.ID,
		EvidenceType: "goal_implementation_plan",
		Summary:      "goal runner recorded approval-gated implementation plan",
		PayloadJSON:  string(payload),
		CreatedBy:    "goal_runner",
	}); err != nil {
		return TickGoalResult{}, err
	}
	planned, err := service.Store.TransitionGoal(ctx, sqlite.TransitionGoalParams{
		GoalID: goal.ID,
		Status: sqlite.GoalStatusPlanned,
		Actor:  "goal_runner",
		Reason: TickReasonPlanRecorded,
	})
	if err != nil {
		return TickGoalResult{}, err
	}
	return service.observe(ctx, goal, TickActionPlanned, TickReasonPlanRecorded, planned.Status, nil)
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

func defaultGoalImplementationPlan(goal sqlite.Goal, decision AutoPolicyDecision) []string {
	reviewID := fmt.Sprintf("goal-approval:%d", goal.ID)
	return []string{
		fmt.Sprintf("Treat this as %s and keep execution approval-gated.", decision.Reason),
		"Audit the target repo authority docs, existing commands, tests, and runtime contracts before implementation.",
		"Implement the smallest change that satisfies the goal while preserving approval and governance boundaries.",
		"Run repo-owned verification plus real odin proof for the affected operator path.",
		fmt.Sprintf("Keep execution blocked until an operator approves %s.", reviewID),
	}
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
	workItem, err := service.materializeGoalWorkItem(ctx, goal, run)
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
	return service.observeWithWorkItem(ctx, goal, TickActionStarted, TickReasonApprovedStarted, updated.Status, &runID, workItem)
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
			workItem, err := service.materializeGoalWorkItem(ctx, goal, activeRun)
			if err != nil {
				return TickGoalResult{}, err
			}
			if workItem != nil {
				return service.observeWithWorkItem(ctx, goal, TickActionStarted, TickReasonWorkItemMaterialized, goal.Status, runID, workItem)
			}
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
	return service.observeWithWorkItem(ctx, goal, action, reason, status, goalRunID, nil)
}

func (service Service) observeWithWorkItem(ctx context.Context, goal sqlite.Goal, action string, reason string, status sqlite.GoalStatus, goalRunID *int64, workItem *goalWorkItemRef) (TickGoalResult, error) {
	if err := service.Store.RecordGoalRunnerObserved(ctx, sqlite.RecordGoalRunnerObservedParams{
		GoalID: goal.ID,
		Action: action,
		Reason: reason,
		Actor:  "goal_runner",
	}); err != nil {
		return TickGoalResult{}, err
	}
	result := TickGoalResult{
		GoalID:         goal.ID,
		PreviousStatus: goal.Status,
		Status:         status,
		Action:         action,
		Reason:         reason,
		GoalRunID:      goalRunID,
	}
	if workItem != nil {
		id := workItem.ID
		result.WorkItemID = &id
		result.WorkItemKey = workItem.Key
	}
	return result, nil
}

func (service Service) materializeGoalWorkItem(ctx context.Context, goal sqlite.Goal, run sqlite.GoalRun) (*goalWorkItemRef, error) {
	key := goalWorkItemKey(goal.ID, run.ID)
	existing, err := service.lookupGoalWorkItem(ctx, key)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, nil
	}

	decision := ClassifyAutoPolicy(goal)
	project, err := service.ensureGoalProject(ctx, decision.ProjectKey)
	if err != nil {
		return nil, err
	}
	executionIntent := decision.ExecutionIntent
	if strings.TrimSpace(executionIntent) == "" {
		executionIntent = "mutation"
	}
	artifacts, err := json.Marshal(map[string]any{
		"goal_id":       goal.ID,
		"goal_run_id":   run.ID,
		"goal_source":   goal.Source,
		"goal_runner":   true,
		"policy_reason": decision.Reason,
	})
	if err != nil {
		return nil, err
	}
	task, err := service.Store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID: project.ID,
		Key:       key,
		Title:     goalWorkItemTitle(goal),
		AcceptanceCriteria: []string{
			fmt.Sprintf("Advance Odin Goal #%d through the canonical Work Item execution path.", goal.ID),
			"Preserve Odin approval, audit, runtime, and executor boundaries.",
			"Record verification evidence or a deterministic blocker before closing the work item.",
		},
		ActionKey:             "run_task",
		Status:                "queued",
		Scope:                 "project",
		RequestedBy:           "goal_runner",
		WorkKind:              "project",
		ExecutionIntent:       executionIntent,
		ExecutionIntentSource: "goal_runner:" + decision.Reason,
		ArtifactsJSON:         string(artifacts),
	})
	if err != nil {
		return nil, err
	}
	if _, err := service.Store.UpdateGoalRunStatus(ctx, sqlite.UpdateGoalRunStatusParams{
		GoalRunID: run.ID,
		Status:    sqlite.GoalRunStatusRunning,
		Summary:   "materialized work item " + task.Key,
	}); err != nil {
		return nil, err
	}
	if _, err := service.Store.AddGoalEvidence(ctx, sqlite.AddGoalEvidenceParams{
		GoalID:       goal.ID,
		GoalRunID:    &run.ID,
		EvidenceType: "goal_work_item_materialized",
		Summary:      "goal runner materialized canonical work item",
		PayloadJSON:  fmt.Sprintf(`{"task_id":%d,"task_key":%q,"project_key":%q}`, task.ID, task.Key, project.Key),
		CreatedBy:    "goal_runner",
	}); err != nil {
		return nil, err
	}
	return &goalWorkItemRef{ID: task.ID, Key: task.Key}, nil
}

func (service Service) lookupGoalWorkItem(ctx context.Context, key string) (*goalWorkItemRef, error) {
	row := service.Store.DB().QueryRowContext(ctx, `SELECT id, key FROM tasks WHERE key = ?`, key)
	var ref goalWorkItemRef
	if err := row.Scan(&ref.ID, &ref.Key); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &ref, nil
}

func (service Service) ensureGoalProject(ctx context.Context, key string) (sqlite.Project, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		key = AutoPolicyDefaultProjectKey
	}
	project, err := service.Store.GetProjectByKey(ctx, key)
	if err == nil {
		return project, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return sqlite.Project{}, err
	}
	return service.Store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           key,
		Name:          key,
		Scope:         "project",
		GitRoot:       ".",
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
}

func goalWorkItemKey(goalID int64, runID int64) string {
	return fmt.Sprintf("goal-%d-run-%d", goalID, runID)
}

func goalWorkItemTitle(goal sqlite.Goal) string {
	title := strings.TrimSpace(goal.Title)
	if title == "" {
		title = fmt.Sprintf("goal-%d", goal.ID)
	}
	return strings.TrimSpace(fmt.Sprintf("Advance goal #%d: %s", goal.ID, truncateGoalTitle(title, 120)))
}

func truncateGoalTitle(title string, limit int) string {
	title = regexp.MustCompile(`\s+`).ReplaceAllString(strings.TrimSpace(title), " ")
	if len(title) <= limit {
		return title
	}
	return strings.TrimSpace(title[:limit])
}
