package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	runtimeevents "odin-os/internal/runtime/events"
)

func TestGoalLifecyclePersistsAndAuditsTransitions(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "goals.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	store.Now = func() time.Time {
		return time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)
	}

	for _, table := range []string{"goals", "goal_runs", "goal_events", "goal_blockers", "goal_evidence"} {
		exists, err := store.HasTable(ctx, table)
		if err != nil {
			t.Fatalf("HasTable(%s) error = %v", table, err)
		}
		if !exists {
			t.Fatalf("HasTable(%s) = false, want true", table)
		}
	}

	goal, err := store.CreateGoal(ctx, CreateGoalParams{
		Title:       "Durable execution goal",
		Description: "Goal survives process restart",
		CreatedBy:   "operator",
		Source:      "cli",
	})
	if err != nil {
		t.Fatalf("CreateGoal() error = %v", err)
	}
	if goal.Status != GoalStatusCreated {
		t.Fatalf("goal.Status = %q, want %q", goal.Status, GoalStatusCreated)
	}

	for _, status := range []GoalStatus{
		GoalStatusPlanned,
		GoalStatusApprovedForExecution,
		GoalStatusRunning,
		GoalStatusVerifying,
		GoalStatusCompleted,
	} {
		goal, err = store.TransitionGoal(ctx, TransitionGoalParams{
			GoalID: goal.ID,
			Status: status,
			Actor:  "operator",
			Reason: "test lifecycle",
		})
		if err != nil {
			t.Fatalf("TransitionGoal(%s) error = %v", status, err)
		}
		if goal.Status != status {
			t.Fatalf("goal.Status after transition = %q, want %q", goal.Status, status)
		}
	}

	goalEvents, err := store.ListGoalEvents(ctx, ListGoalEventsParams{GoalID: goal.ID})
	if err != nil {
		t.Fatalf("ListGoalEvents() error = %v", err)
	}
	if len(goalEvents) != 6 {
		t.Fatalf("goal events len = %d, want create plus five transitions", len(goalEvents))
	}
	if goalEvents[0].EventType != string(runtimeevents.EventGoalCreated) {
		t.Fatalf("first goal event type = %q, want %q", goalEvents[0].EventType, runtimeevents.EventGoalCreated)
	}

	auditEvents, err := store.ListEvents(ctx, ListEventsParams{})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	counts := map[runtimeevents.Type]int{}
	for _, event := range auditEvents {
		if event.StreamType == runtimeevents.StreamGoal {
			counts[event.Type]++
		}
	}
	if counts[runtimeevents.EventGoalCreated] != 1 {
		t.Fatalf("goal.created events = %d, want 1", counts[runtimeevents.EventGoalCreated])
	}
	if counts[runtimeevents.EventGoalStatusChanged] != 5 {
		t.Fatalf("goal.status_changed events = %d, want 5", counts[runtimeevents.EventGoalStatusChanged])
	}

	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	reopened, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open(reopened) error = %v", err)
	}
	defer reopened.Close()
	if err := reopened.Migrate(ctx); err != nil {
		t.Fatalf("Migrate(reopened) error = %v", err)
	}
	persisted, err := reopened.GetGoal(ctx, goal.ID)
	if err != nil {
		t.Fatalf("GetGoal(reopened) error = %v", err)
	}
	if persisted.Status != GoalStatusCompleted {
		t.Fatalf("persisted.Status = %q, want %q", persisted.Status, GoalStatusCompleted)
	}
}

func TestGoalLifecycleRejectsInvalidTransition(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "goals-invalid.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	goal, err := store.CreateGoal(ctx, CreateGoalParams{
		Title:     "Do not skip approval",
		CreatedBy: "operator",
		Source:    "cli",
	})
	if err != nil {
		t.Fatalf("CreateGoal() error = %v", err)
	}

	if _, err := store.TransitionGoal(ctx, TransitionGoalParams{
		GoalID: goal.ID,
		Status: GoalStatusRunning,
		Actor:  "operator",
		Reason: "skip approval",
	}); !errors.Is(err, ErrInvalidGoalTransition) {
		t.Fatalf("TransitionGoal(created->running) error = %v, want %v", err, ErrInvalidGoalTransition)
	}
}

func TestUpdateGoalPersistsAllowedFieldsAndAudits(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "goals-update.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	goal, err := store.CreateGoal(ctx, CreateGoalParams{
		Title:       "Original title",
		Description: "Original description",
		CreatedBy:   "operator",
		Source:      "cli",
	})
	if err != nil {
		t.Fatalf("CreateGoal() error = %v", err)
	}

	updated, err := store.UpdateGoal(ctx, UpdateGoalParams{
		GoalID:         goal.ID,
		Title:          "Updated title",
		TitleSet:       true,
		Description:    "Updated description",
		DescriptionSet: true,
		Actor:          "operator",
		Reason:         "operator correction",
	})
	if err != nil {
		t.Fatalf("UpdateGoal() error = %v", err)
	}
	if updated.Title != "Updated title" || updated.Description != "Updated description" {
		t.Fatalf("updated goal = %+v, want updated title and description", updated)
	}
	if updated.Status != GoalStatusCreated {
		t.Fatalf("updated.Status = %q, want status unchanged", updated.Status)
	}

	events, err := store.ListEvents(ctx, ListEventsParams{})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	var updatedEvents int
	for _, event := range events {
		if event.StreamType == runtimeevents.StreamGoal && event.Type == runtimeevents.EventGoalUpdated {
			updatedEvents++
		}
	}
	if updatedEvents != 1 {
		t.Fatalf("goal.updated events = %d, want 1", updatedEvents)
	}
}

func TestListGoalsFiltersStatusAndLimit(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "goals-list.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	first, err := store.CreateGoal(ctx, CreateGoalParams{Title: "first"})
	if err != nil {
		t.Fatalf("CreateGoal(first) error = %v", err)
	}
	if _, err := store.TransitionGoal(ctx, TransitionGoalParams{GoalID: first.ID, Status: GoalStatusPlanned}); err != nil {
		t.Fatalf("TransitionGoal(first planned) error = %v", err)
	}
	if _, err := store.CreateGoal(ctx, CreateGoalParams{Title: "second"}); err != nil {
		t.Fatalf("CreateGoal(second) error = %v", err)
	}
	if _, err := store.CreateGoal(ctx, CreateGoalParams{Title: "third"}); err != nil {
		t.Fatalf("CreateGoal(third) error = %v", err)
	}

	created, err := store.ListGoals(ctx, ListGoalsParams{Status: GoalStatusCreated, Limit: 1})
	if err != nil {
		t.Fatalf("ListGoals(created, limit 1) error = %v", err)
	}
	if len(created) != 1 {
		t.Fatalf("created goals len = %d, want 1", len(created))
	}
	if created[0].Status != GoalStatusCreated {
		t.Fatalf("created[0].Status = %q, want created", created[0].Status)
	}
}

func TestGoalRunStorageCreateListActiveAndUpdate(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "goal-runs.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	now := time.Date(2026, 5, 5, 15, 0, 0, 0, time.UTC)
	store.Now = func() time.Time { return now }

	goal, err := store.CreateGoal(ctx, CreateGoalParams{Title: "Runner foundation"})
	if err != nil {
		t.Fatalf("CreateGoal() error = %v", err)
	}
	nextWake := now.Add(30 * time.Minute)
	run, err := store.CreateGoalRun(ctx, CreateGoalRunParams{
		GoalID:         goal.ID,
		Status:         GoalRunStatusRunning,
		Attempts:       1,
		MaxAttempts:    3,
		LastProgressAt: &now,
		NextWakeAt:     &nextWake,
		LeaseOwner:     "runner-1",
	})
	if err != nil {
		t.Fatalf("CreateGoalRun() error = %v", err)
	}
	if run.GoalID != goal.ID || run.Status != GoalRunStatusRunning || run.Attempts != 1 || run.MaxAttempts != 3 || run.LeaseOwner != "runner-1" {
		t.Fatalf("created goal run = %+v, want persisted metadata", run)
	}
	if run.LastProgressAt == nil || !run.LastProgressAt.Equal(now) {
		t.Fatalf("LastProgressAt = %v, want %s", run.LastProgressAt, now)
	}
	if run.NextWakeAt == nil || !run.NextWakeAt.Equal(nextWake) {
		t.Fatalf("NextWakeAt = %v, want %s", run.NextWakeAt, nextWake)
	}

	active, err := store.GetActiveGoalRunByGoalID(ctx, goal.ID)
	if err != nil {
		t.Fatalf("GetActiveGoalRunByGoalID() error = %v", err)
	}
	if active.ID != run.ID {
		t.Fatalf("active.ID = %d, want %d", active.ID, run.ID)
	}

	runs, err := store.ListGoalRunsByGoalID(ctx, goal.ID)
	if err != nil {
		t.Fatalf("ListGoalRunsByGoalID() error = %v", err)
	}
	if len(runs) != 1 || runs[0].ID != run.ID {
		t.Fatalf("runs = %+v, want one created run", runs)
	}

	updatedProgress := now.Add(5 * time.Minute)
	updatedWake := now.Add(time.Hour)
	updated, err := store.UpdateGoalRunStatus(ctx, UpdateGoalRunStatusParams{
		GoalRunID:      run.ID,
		Status:         GoalRunStatusWaitingForExternal,
		Attempts:       2,
		MaxAttempts:    4,
		LastProgressAt: &updatedProgress,
		NextWakeAt:     &updatedWake,
		LeaseOwner:     "runner-2",
		Summary:        "waiting on external input",
	})
	if err != nil {
		t.Fatalf("UpdateGoalRunStatus() error = %v", err)
	}
	if updated.Status != GoalRunStatusWaitingForExternal || updated.Attempts != 2 || updated.MaxAttempts != 4 || updated.LeaseOwner != "runner-2" || updated.Summary != "waiting on external input" {
		t.Fatalf("updated run = %+v, want status and metadata update", updated)
	}
	if updated.EndedAt != nil {
		t.Fatalf("updated.EndedAt = %v, want active run without ended_at", updated.EndedAt)
	}

	events, err := store.ListEvents(ctx, ListEventsParams{})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	counts := map[runtimeevents.Type]int{}
	for _, event := range events {
		if event.StreamType == runtimeevents.StreamGoal {
			counts[event.Type]++
		}
	}
	if counts[runtimeevents.EventGoalRunStarted] != 1 {
		t.Fatalf("goal_run.started events = %d, want 1", counts[runtimeevents.EventGoalRunStarted])
	}
	if counts[runtimeevents.EventGoalRunStatusChanged] != 1 {
		t.Fatalf("goal_run.status_changed events = %d, want 1", counts[runtimeevents.EventGoalRunStatusChanged])
	}
}

func TestCreateGoalRunRejectsDuplicateActiveRun(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "goal-run-duplicate.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	goal, err := store.CreateGoal(ctx, CreateGoalParams{Title: "Single active run"})
	if err != nil {
		t.Fatalf("CreateGoal() error = %v", err)
	}
	if _, err := store.CreateGoalRun(ctx, CreateGoalRunParams{GoalID: goal.ID, Status: GoalRunStatusRunning}); err != nil {
		t.Fatalf("CreateGoalRun(first) error = %v", err)
	}
	if _, err := store.CreateGoalRun(ctx, CreateGoalRunParams{GoalID: goal.ID, Status: GoalRunStatusRunning}); !errors.Is(err, ErrActiveGoalRunExists) {
		t.Fatalf("CreateGoalRun(second) error = %v, want %v", err, ErrActiveGoalRunExists)
	}

	runs, err := store.ListGoalRunsByGoalID(ctx, goal.ID)
	if err != nil {
		t.Fatalf("ListGoalRunsByGoalID() error = %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("runs len = %d, want duplicate active run rejected", len(runs))
	}
}
