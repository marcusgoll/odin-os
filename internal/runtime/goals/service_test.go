package goals

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	runtimeevents "odin-os/internal/runtime/events"
	"odin-os/internal/store/sqlite"
)

func TestServiceCanStartRunAndReadsGoalRunState(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "runtime-goals.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	goal, err := store.CreateGoal(ctx, sqlite.CreateGoalParams{Title: "Runtime guardrails"})
	if err != nil {
		t.Fatalf("CreateGoal() error = %v", err)
	}

	service := NewService(store)
	decision, err := service.CanStartRun(ctx, goal.ID)
	if err != nil {
		t.Fatalf("CanStartRun(empty) error = %v", err)
	}
	if !decision.Allowed || decision.Reason != "no_active_goal_run" {
		t.Fatalf("empty decision = %+v, want allowed no_active_goal_run", decision)
	}

	run, err := store.CreateGoalRun(ctx, sqlite.CreateGoalRunParams{
		GoalID:      goal.ID,
		Status:      sqlite.GoalRunStatusRunning,
		Attempts:    1,
		MaxAttempts: 3,
		LeaseOwner:  "runner-1",
	})
	if err != nil {
		t.Fatalf("CreateGoalRun() error = %v", err)
	}

	state, err := service.GetGoalRunState(ctx, goal.ID)
	if err != nil {
		t.Fatalf("GetGoalRunState() error = %v", err)
	}
	if state.Goal.ID != goal.ID {
		t.Fatalf("state.Goal.ID = %d, want %d", state.Goal.ID, goal.ID)
	}
	if state.ActiveRun == nil || state.ActiveRun.ID != run.ID {
		t.Fatalf("state.ActiveRun = %+v, want run %d", state.ActiveRun, run.ID)
	}
	if len(state.Runs) != 1 {
		t.Fatalf("state.Runs len = %d, want 1", len(state.Runs))
	}

	decision, err = service.CanStartRun(ctx, goal.ID)
	if err != nil {
		t.Fatalf("CanStartRun(active) error = %v", err)
	}
	if decision.Allowed || decision.Reason != "active_goal_run_exists" || decision.ActiveRunID == nil || *decision.ActiveRunID != run.ID {
		t.Fatalf("active decision = %+v, want blocked by active run", decision)
	}
}

func TestServiceRequiresStore(t *testing.T) {
	ctx := context.Background()
	service := NewService(nil)
	if _, err := service.CanStartRun(ctx, 1); err == nil {
		t.Fatal("CanStartRun(nil store) error = nil, want error")
	}
	if _, err := service.GetGoalRunState(ctx, 1); err == nil {
		t.Fatal("GetGoalRunState(nil store) error = nil, want error")
	}
	if _, err := service.Tick(ctx); err == nil {
		t.Fatal("Tick(nil store) error = nil, want error")
	}
}

func TestTickLeavesPlannedGoalUnrunAndAuditsObservation(t *testing.T) {
	ctx := context.Background()
	store := openGoalServiceTestStore(t, "tick-planned.db")
	defer store.Close()

	goal, err := store.CreateGoal(ctx, sqlite.CreateGoalParams{Title: "Plan first"})
	if err != nil {
		t.Fatalf("CreateGoal() error = %v", err)
	}
	if _, err := store.TransitionGoal(ctx, sqlite.TransitionGoalParams{GoalID: goal.ID, Status: sqlite.GoalStatusPlanned}); err != nil {
		t.Fatalf("TransitionGoal(planned) error = %v", err)
	}

	result, err := NewService(store).Tick(ctx)
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if result.Observed != 1 || result.Skipped != 1 || result.Started != 0 || result.Blocked != 0 {
		t.Fatalf("tick result = %+v, want observed/skipped planned goal only", result)
	}
	if len(result.Results) != 1 || result.Results[0].Action != TickActionSkipped || result.Results[0].Reason != TickReasonApprovalRequired {
		t.Fatalf("tick goal result = %+v, want approval-required skip", result.Results)
	}
	persisted, err := store.GetGoal(ctx, goal.ID)
	if err != nil {
		t.Fatalf("GetGoal() error = %v", err)
	}
	if persisted.Status != sqlite.GoalStatusPlanned {
		t.Fatalf("persisted.Status = %q, want planned", persisted.Status)
	}
	runs, err := store.ListGoalRunsByGoalID(ctx, goal.ID)
	if err != nil {
		t.Fatalf("ListGoalRunsByGoalID() error = %v", err)
	}
	if len(runs) != 0 {
		t.Fatalf("runs len = %d, want no run for unapproved planned goal", len(runs))
	}
	counts := countGoalRuntimeEvents(t, store)
	if counts[runtimeevents.EventGoalRunnerObserved] != 1 {
		t.Fatalf("goal_runner.observed events = %d, want 1", counts[runtimeevents.EventGoalRunnerObserved])
	}
	if counts[runtimeevents.EventGoalRunStarted] != 0 {
		t.Fatalf("goal_run.started events = %d, want 0", counts[runtimeevents.EventGoalRunStarted])
	}
}

func TestTickObservesCreatedGoalWithoutRun(t *testing.T) {
	ctx := context.Background()
	store := openGoalServiceTestStore(t, "tick-created.db")
	defer store.Close()

	goal, err := store.CreateGoal(ctx, sqlite.CreateGoalParams{Title: "Observe only"})
	if err != nil {
		t.Fatalf("CreateGoal() error = %v", err)
	}

	result, err := NewService(store).Tick(ctx)
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if result.Observed != 1 || result.Started != 0 || result.Blocked != 0 || result.Skipped != 0 {
		t.Fatalf("tick result = %+v, want created goal observed only", result)
	}
	if len(result.Results) != 1 || result.Results[0].Action != TickActionObserved || result.Results[0].Reason != TickReasonCreatedNeedsPlanning {
		t.Fatalf("tick goal result = %+v, want created observation", result.Results)
	}
	persisted, err := store.GetGoal(ctx, goal.ID)
	if err != nil {
		t.Fatalf("GetGoal() error = %v", err)
	}
	if persisted.Status != sqlite.GoalStatusCreated {
		t.Fatalf("persisted.Status = %q, want created", persisted.Status)
	}
	runs, err := store.ListGoalRunsByGoalID(ctx, goal.ID)
	if err != nil {
		t.Fatalf("ListGoalRunsByGoalID() error = %v", err)
	}
	if len(runs) != 0 {
		t.Fatalf("runs len = %d, want no run for created goal", len(runs))
	}
}

func TestTickAutoStartsReadOnlyCreatedGoal(t *testing.T) {
	ctx := context.Background()
	store := openGoalServiceTestStore(t, "tick-read-only-created.db")
	defer store.Close()

	goal, err := store.CreateGoal(ctx, sqlite.CreateGoalParams{
		Title:       "Review latest PBS bidding package",
		Description: "Read the latest bid package and report improvements.",
	})
	if err != nil {
		t.Fatalf("CreateGoal() error = %v", err)
	}

	result, err := NewService(store).Tick(ctx)
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if result.Observed != 1 || result.Started != 1 || result.Blocked != 0 || result.Skipped != 0 {
		t.Fatalf("tick result = %+v, want read-only goal auto-started", result)
	}
	if len(result.Results) != 1 || result.Results[0].Action != TickActionStarted || result.Results[0].GoalRunID == nil {
		t.Fatalf("tick goal result = %+v, want started run id", result.Results)
	}
	persisted, err := store.GetGoal(ctx, goal.ID)
	if err != nil {
		t.Fatalf("GetGoal() error = %v", err)
	}
	if persisted.Status != sqlite.GoalStatusRunning || persisted.CurrentRunID == nil {
		t.Fatalf("persisted goal = %+v, want running with current run", persisted)
	}
	runs, err := store.ListGoalRunsByGoalID(ctx, goal.ID)
	if err != nil {
		t.Fatalf("ListGoalRunsByGoalID() error = %v", err)
	}
	if len(runs) != 1 || runs[0].Executor != "goal_runner" {
		t.Fatalf("runs = %+v, want one goal_runner run", runs)
	}
	counts := countGoalRuntimeEvents(t, store)
	if counts[runtimeevents.EventGoalRunStarted] != 1 {
		t.Fatalf("goal_run.started events = %d, want 1", counts[runtimeevents.EventGoalRunStarted])
	}
	if counts[runtimeevents.EventGoalEvidenceRecorded] != 1 {
		t.Fatalf("goal.evidence_recorded events = %d, want 1", counts[runtimeevents.EventGoalEvidenceRecorded])
	}
}

func TestTickAutoStartsReadOnlyPlannedGoal(t *testing.T) {
	ctx := context.Background()
	store := openGoalServiceTestStore(t, "tick-read-only-planned.db")
	defer store.Close()

	goal, err := store.CreateGoal(ctx, sqlite.CreateGoalParams{Title: "Audit CFIPros staging errors"})
	if err != nil {
		t.Fatalf("CreateGoal() error = %v", err)
	}
	if _, err := store.TransitionGoal(ctx, sqlite.TransitionGoalParams{GoalID: goal.ID, Status: sqlite.GoalStatusPlanned}); err != nil {
		t.Fatalf("TransitionGoal(planned) error = %v", err)
	}

	result, err := NewService(store).Tick(ctx)
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if result.Observed != 1 || result.Started != 1 || result.Blocked != 0 || result.Skipped != 0 {
		t.Fatalf("tick result = %+v, want planned read-only goal auto-started", result)
	}
	persisted, err := store.GetGoal(ctx, goal.ID)
	if err != nil {
		t.Fatalf("GetGoal() error = %v", err)
	}
	if persisted.Status != sqlite.GoalStatusRunning || persisted.CurrentRunID == nil {
		t.Fatalf("persisted goal = %+v, want running with current run", persisted)
	}
}

func TestTickKeepsMutationGoalInReview(t *testing.T) {
	ctx := context.Background()
	store := openGoalServiceTestStore(t, "tick-mutation-review.db")
	defer store.Close()

	goal, err := store.CreateGoal(ctx, sqlite.CreateGoalParams{Title: "Build autonomous worker shim"})
	if err != nil {
		t.Fatalf("CreateGoal() error = %v", err)
	}

	result, err := NewService(store).Tick(ctx)
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if result.Observed != 1 || result.Started != 0 || result.Blocked != 0 || result.Skipped != 0 {
		t.Fatalf("tick result = %+v, want mutation goal observed only", result)
	}
	persisted, err := store.GetGoal(ctx, goal.ID)
	if err != nil {
		t.Fatalf("GetGoal() error = %v", err)
	}
	if persisted.Status != sqlite.GoalStatusCreated || persisted.CurrentRunID != nil {
		t.Fatalf("persisted goal = %+v, want created without run", persisted)
	}
	runs, err := store.ListGoalRunsByGoalID(ctx, goal.ID)
	if err != nil {
		t.Fatalf("ListGoalRunsByGoalID() error = %v", err)
	}
	if len(runs) != 0 {
		t.Fatalf("runs len = %d, want no run for mutation goal", len(runs))
	}
}

func TestTickKeepsExternalAccountGoalInReview(t *testing.T) {
	ctx := context.Background()
	store := openGoalServiceTestStore(t, "tick-external-review.db")
	defer store.Close()

	goal, err := store.CreateGoal(ctx, sqlite.CreateGoalParams{Title: "X attended login smoke test"})
	if err != nil {
		t.Fatalf("CreateGoal() error = %v", err)
	}

	result, err := NewService(store).Tick(ctx)
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if result.Observed != 1 || result.Started != 0 || result.Blocked != 0 || result.Skipped != 0 {
		t.Fatalf("tick result = %+v, want external account goal observed only", result)
	}
	persisted, err := store.GetGoal(ctx, goal.ID)
	if err != nil {
		t.Fatalf("GetGoal() error = %v", err)
	}
	if persisted.Status != sqlite.GoalStatusCreated || persisted.CurrentRunID != nil {
		t.Fatalf("persisted goal = %+v, want created without run", persisted)
	}
}

func TestTickDoesNotTreatEmbeddedXAsExternalAccountGoal(t *testing.T) {
	ctx := context.Background()
	store := openGoalServiceTestStore(t, "tick-embedded-x.db")
	defer store.Close()

	goal, err := store.CreateGoal(ctx, sqlite.CreateGoalParams{
		Title:       "Review latest PBS bidding package for next-month improvements",
		Description: "May W700x open-time pairings are per-period evidence and must not be reused.",
	})
	if err != nil {
		t.Fatalf("CreateGoal() error = %v", err)
	}

	result, err := NewService(store).Tick(ctx)
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if result.Observed != 1 || result.Started != 1 || result.Blocked != 0 || result.Skipped != 0 {
		t.Fatalf("tick result = %+v, want embedded x read-only goal auto-started", result)
	}
	persisted, err := store.GetGoal(ctx, goal.ID)
	if err != nil {
		t.Fatalf("GetGoal() error = %v", err)
	}
	if persisted.Status != sqlite.GoalStatusRunning || persisted.CurrentRunID == nil {
		t.Fatalf("persisted goal = %+v, want running with current run", persisted)
	}
}

func TestTickStartsApprovedGoalOnceThenKeepsExecutorBackedRunActive(t *testing.T) {
	ctx := context.Background()
	store := openGoalServiceTestStore(t, "tick-approved.db")
	defer store.Close()

	goal, err := store.CreateGoal(ctx, sqlite.CreateGoalParams{Title: "Approved deterministic run"})
	if err != nil {
		t.Fatalf("CreateGoal() error = %v", err)
	}
	for _, status := range []sqlite.GoalStatus{sqlite.GoalStatusPlanned, sqlite.GoalStatusApprovedForExecution} {
		if _, err := store.TransitionGoal(ctx, sqlite.TransitionGoalParams{GoalID: goal.ID, Status: status}); err != nil {
			t.Fatalf("TransitionGoal(%s) error = %v", status, err)
		}
	}

	service := NewService(store)
	first, err := service.Tick(ctx)
	if err != nil {
		t.Fatalf("Tick(first) error = %v", err)
	}
	if first.Observed != 1 || first.Started != 1 || first.Blocked != 0 || first.Skipped != 0 {
		t.Fatalf("first tick result = %+v, want one started approved goal", first)
	}
	if len(first.Results) != 1 || first.Results[0].Action != TickActionStarted || first.Results[0].GoalRunID == nil {
		t.Fatalf("first tick goal result = %+v, want started run id", first.Results)
	}
	afterStart, err := store.GetGoal(ctx, goal.ID)
	if err != nil {
		t.Fatalf("GetGoal(after start) error = %v", err)
	}
	if afterStart.Status != sqlite.GoalStatusRunning {
		t.Fatalf("afterStart.Status = %q, want running", afterStart.Status)
	}
	runs, err := store.ListGoalRunsByGoalID(ctx, goal.ID)
	if err != nil {
		t.Fatalf("ListGoalRunsByGoalID(after start) error = %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("runs len after first tick = %d, want 1", len(runs))
	}

	second, err := service.Tick(ctx)
	if err != nil {
		t.Fatalf("Tick(second) error = %v", err)
	}
	if second.Observed != 1 || second.Started != 0 || second.Blocked != 0 || second.Skipped != 1 {
		t.Fatalf("second tick result = %+v, want active executor-backed run skipped", second)
	}
	if len(second.Results) != 1 || second.Results[0].Action != TickActionSkipped || second.Results[0].Reason != TickReasonActiveRunExists {
		t.Fatalf("second tick goal result = %+v, want active run exists", second.Results)
	}
	afterSecond, err := store.GetGoal(ctx, goal.ID)
	if err != nil {
		t.Fatalf("GetGoal(after second tick) error = %v", err)
	}
	if afterSecond.Status != sqlite.GoalStatusRunning {
		t.Fatalf("afterSecond.Status = %q, want running", afterSecond.Status)
	}
	runs, err = store.ListGoalRunsByGoalID(ctx, goal.ID)
	if err != nil {
		t.Fatalf("ListGoalRunsByGoalID(after second tick) error = %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("runs len after second tick = %d, want no duplicate active run", len(runs))
	}
	if runs[0].Executor != "goal_runner" {
		t.Fatalf("run.Executor = %q, want goal_runner", runs[0].Executor)
	}
	counts := countGoalRuntimeEvents(t, store)
	if counts[runtimeevents.EventGoalRunnerObserved] != 2 {
		t.Fatalf("goal_runner.observed events = %d, want 2", counts[runtimeevents.EventGoalRunnerObserved])
	}
	if counts[runtimeevents.EventGoalRunStarted] != 1 {
		t.Fatalf("goal_run.started events = %d, want 1", counts[runtimeevents.EventGoalRunStarted])
	}
	if counts[runtimeevents.EventGoalStatusChanged] != 3 {
		t.Fatalf("goal.status_changed events = %d, want planned/approved/running transitions", counts[runtimeevents.EventGoalStatusChanged])
	}
	if counts[runtimeevents.EventGoalBlockerRecorded] != 0 {
		t.Fatalf("goal.blocker_recorded events = %d, want no missing-executor blocker", counts[runtimeevents.EventGoalBlockerRecorded])
	}
	if counts[runtimeevents.EventGoalEvidenceRecorded] != 0 {
		t.Fatalf("goal.evidence_recorded events = %d, want no missing-executor evidence", counts[runtimeevents.EventGoalEvidenceRecorded])
	}
}

func TestTickBlocksLegacyRunningGoalWithoutExecutor(t *testing.T) {
	ctx := context.Background()
	store := openGoalServiceTestStore(t, "tick-legacy-no-executor.db")
	defer store.Close()

	goal, err := store.CreateGoal(ctx, sqlite.CreateGoalParams{Title: "Legacy blank executor run"})
	if err != nil {
		t.Fatalf("CreateGoal() error = %v", err)
	}
	for _, status := range []sqlite.GoalStatus{sqlite.GoalStatusPlanned, sqlite.GoalStatusApprovedForExecution} {
		if _, err := store.TransitionGoal(ctx, sqlite.TransitionGoalParams{GoalID: goal.ID, Status: status}); err != nil {
			t.Fatalf("TransitionGoal(%s) error = %v", status, err)
		}
	}
	run, err := store.CreateGoalRun(ctx, sqlite.CreateGoalRunParams{
		GoalID: goal.ID,
		Status: sqlite.GoalRunStatusRunning,
	})
	if err != nil {
		t.Fatalf("CreateGoalRun() error = %v", err)
	}
	if _, err := store.TransitionGoal(ctx, sqlite.TransitionGoalParams{GoalID: goal.ID, Status: sqlite.GoalStatusRunning}); err != nil {
		t.Fatalf("TransitionGoal(running) error = %v", err)
	}

	result, err := NewService(store).Tick(ctx)
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if result.Observed != 1 || result.Blocked != 1 || len(result.Results) != 1 || result.Results[0].Reason != TickReasonNoExecutor {
		t.Fatalf("Tick() = %+v, want one missing-executor block", result)
	}
	if result.Results[0].GoalRunID == nil || *result.Results[0].GoalRunID != run.ID {
		t.Fatalf("GoalRunID = %v, want %d", result.Results[0].GoalRunID, run.ID)
	}
	persisted, err := store.GetGoal(ctx, goal.ID)
	if err != nil {
		t.Fatalf("GetGoal() error = %v", err)
	}
	if persisted.Status != sqlite.GoalStatusBlocked {
		t.Fatalf("persisted.Status = %q, want blocked", persisted.Status)
	}
}

func TestTickRespectsFutureNextWakeAt(t *testing.T) {
	ctx := context.Background()
	store := openGoalServiceTestStore(t, "tick-next-wake.db")
	defer store.Close()

	goal, err := store.CreateGoal(ctx, sqlite.CreateGoalParams{Title: "Wait for wake"})
	if err != nil {
		t.Fatalf("CreateGoal() error = %v", err)
	}
	for _, status := range []sqlite.GoalStatus{sqlite.GoalStatusPlanned, sqlite.GoalStatusApprovedForExecution} {
		if _, err := store.TransitionGoal(ctx, sqlite.TransitionGoalParams{GoalID: goal.ID, Status: status}); err != nil {
			t.Fatalf("TransitionGoal(%s) error = %v", status, err)
		}
	}
	wakeAt := time.Now().UTC().Add(time.Hour)
	run, err := store.CreateGoalRun(ctx, sqlite.CreateGoalRunParams{
		GoalID:     goal.ID,
		Status:     sqlite.GoalRunStatusRunning,
		NextWakeAt: &wakeAt,
	})
	if err != nil {
		t.Fatalf("CreateGoalRun() error = %v", err)
	}
	if _, err := store.TransitionGoal(ctx, sqlite.TransitionGoalParams{GoalID: goal.ID, Status: sqlite.GoalStatusRunning}); err != nil {
		t.Fatalf("TransitionGoal(running) error = %v", err)
	}

	result, err := NewService(store).Tick(ctx)
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if result.Observed != 1 || result.Skipped != 1 || result.Blocked != 0 || result.Started != 0 {
		t.Fatalf("tick result = %+v, want next-wake skip", result)
	}
	if len(result.Results) != 1 || result.Results[0].Action != TickActionSkipped || result.Results[0].Reason != TickReasonNextWakePending || result.Results[0].GoalRunID == nil || *result.Results[0].GoalRunID != run.ID {
		t.Fatalf("tick goal result = %+v, want next-wake skip for run %d", result.Results, run.ID)
	}
	persisted, err := store.GetGoal(ctx, goal.ID)
	if err != nil {
		t.Fatalf("GetGoal() error = %v", err)
	}
	if persisted.Status != sqlite.GoalStatusRunning {
		t.Fatalf("persisted.Status = %q, want running while next_wake_at pending", persisted.Status)
	}
	counts := countGoalRuntimeEvents(t, store)
	if counts[runtimeevents.EventGoalBlockerRecorded] != 0 {
		t.Fatalf("goal.blocker_recorded events = %d, want no block before next_wake_at", counts[runtimeevents.EventGoalBlockerRecorded])
	}
}

func TestTickSkipsBlockedCompletedAndWaitingGoals(t *testing.T) {
	ctx := context.Background()
	store := openGoalServiceTestStore(t, "tick-skips.db")
	defer store.Close()

	blocked, err := store.CreateGoal(ctx, sqlite.CreateGoalParams{Title: "Already blocked"})
	if err != nil {
		t.Fatalf("CreateGoal(blocked) error = %v", err)
	}
	if _, err := store.TransitionGoal(ctx, sqlite.TransitionGoalParams{GoalID: blocked.ID, Status: sqlite.GoalStatusBlocked}); err != nil {
		t.Fatalf("TransitionGoal(blocked) error = %v", err)
	}
	waiting, err := store.CreateGoal(ctx, sqlite.CreateGoalParams{Title: "Waiting"})
	if err != nil {
		t.Fatalf("CreateGoal(waiting) error = %v", err)
	}
	if _, err := store.TransitionGoal(ctx, sqlite.TransitionGoalParams{GoalID: waiting.ID, Status: sqlite.GoalStatusWaitingForExternal}); err != nil {
		t.Fatalf("TransitionGoal(waiting) error = %v", err)
	}
	completed, err := store.CreateGoal(ctx, sqlite.CreateGoalParams{Title: "Completed"})
	if err != nil {
		t.Fatalf("CreateGoal(completed) error = %v", err)
	}
	for _, status := range []sqlite.GoalStatus{
		sqlite.GoalStatusPlanned,
		sqlite.GoalStatusApprovedForExecution,
		sqlite.GoalStatusRunning,
		sqlite.GoalStatusVerifying,
		sqlite.GoalStatusCompleted,
	} {
		if _, err := store.TransitionGoal(ctx, sqlite.TransitionGoalParams{GoalID: completed.ID, Status: status}); err != nil {
			t.Fatalf("TransitionGoal(completed %s) error = %v", status, err)
		}
	}

	result, err := NewService(store).Tick(ctx)
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if result.Observed != 3 || result.Skipped != 3 || result.Started != 0 || result.Blocked != 0 {
		t.Fatalf("tick result = %+v, want blocked/completed/waiting skipped", result)
	}
	for _, goalResult := range result.Results {
		if goalResult.Action != TickActionSkipped || goalResult.Reason != TickReasonStatusSkipped {
			t.Fatalf("goal result = %+v, want status-skipped action", goalResult)
		}
	}
}

func openGoalServiceTestStore(t *testing.T, name string) *sqlite.Store {
	t.Helper()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), name))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	return store
}

func countGoalRuntimeEvents(t *testing.T, store *sqlite.Store) map[runtimeevents.Type]int {
	t.Helper()
	events, err := store.ListEvents(context.Background(), sqlite.ListEventsParams{})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	counts := map[runtimeevents.Type]int{}
	for _, event := range events {
		if event.StreamType == runtimeevents.StreamGoal {
			counts[event.Type]++
		}
	}
	return counts
}
