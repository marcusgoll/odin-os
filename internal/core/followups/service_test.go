package followups

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"odin-os/internal/core/workspaces"
	"odin-os/internal/store/sqlite"
)

func TestFollowUpCreateOneTimeObligation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openFollowUpStore(t)
	defer store.Close()

	workspaceID, initiativeID, companionID, _ := seedFollowUpContext(t, ctx, store)
	service := Service{Store: store}
	dueAt := time.Date(2026, 4, 18, 9, 0, 0, 0, time.UTC)

	obligation, err := service.Create(ctx, CreateParams{
		WorkspaceID:  workspaceID,
		InitiativeID: &initiativeID,
		CompanionID:  &companionID,
		Title:        "Review inbox",
		Cadence:      Cadence{Mode: CadenceModeOnce},
		NextDueAt:    dueAt,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if obligation.Status != StatusActive {
		t.Fatalf("Status = %q, want active", obligation.Status)
	}
	if obligation.Cadence.Mode != CadenceModeOnce {
		t.Fatalf("Cadence.Mode = %q, want once", obligation.Cadence.Mode)
	}
	if !obligation.NextDueAt.Equal(dueAt) {
		t.Fatalf("NextDueAt = %v, want %v", obligation.NextDueAt, dueAt)
	}
}

func TestFollowUpCreateRecurringObligation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openFollowUpStore(t)
	defer store.Close()

	workspaceID, initiativeID, companionID, _ := seedFollowUpContext(t, ctx, store)
	service := Service{Store: store}

	obligation, err := service.Create(ctx, CreateParams{
		WorkspaceID:  workspaceID,
		InitiativeID: &initiativeID,
		CompanionID:  &companionID,
		Title:        "Weekly review",
		Cadence: Cadence{
			Mode:     CadenceModeRecurring,
			Interval: CadenceIntervalWeekly,
		},
		NextDueAt: time.Date(2026, 4, 21, 9, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if obligation.Cadence.Mode != CadenceModeRecurring {
		t.Fatalf("Cadence.Mode = %q, want recurring", obligation.Cadence.Mode)
	}
	if obligation.Cadence.Interval != CadenceIntervalWeekly {
		t.Fatalf("Cadence.Interval = %q, want weekly", obligation.Cadence.Interval)
	}
}

func TestFollowUpCreateDefaultsTargetProjectToOdinCoreForRoutineInitiative(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openFollowUpStore(t)
	defer store.Close()

	workspaceID, initiativeID, companionID, projectID := seedFollowUpContext(t, ctx, store)
	service := Service{Store: store}

	obligation, err := service.Create(ctx, CreateParams{
		WorkspaceID:  workspaceID,
		InitiativeID: &initiativeID,
		CompanionID:  &companionID,
		Title:        "Weekly review",
		Cadence: Cadence{
			Mode:     CadenceModeRecurring,
			Interval: CadenceIntervalWeekly,
		},
		NextDueAt: time.Date(2026, 4, 21, 9, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if obligation.TargetProjectID != projectID {
		t.Fatalf("TargetProjectID = %d, want %d", obligation.TargetProjectID, projectID)
	}
}

func TestFollowUpCreateUsesManagedProjectLinkedTargetProject(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openFollowUpStore(t)
	defer store.Close()

	workspaceID, _, companionID, _ := seedFollowUpContext(t, ctx, store)
	service := Service{Store: store}

	linkedProject, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "alpha",
		Name:          "Alpha",
		Scope:         "project",
		GitRoot:       filepath.Join(t.TempDir(), "alpha"),
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	initiative, err := store.UpsertInitiative(ctx, sqlite.UpsertInitiativeParams{
		WorkspaceID:      workspaceID,
		Key:              "alpha",
		Title:            "Alpha",
		Kind:             "managed_project",
		Status:           "active",
		OwnerCompanionID: &companionID,
		LinkedProjectID:  &linkedProject.ID,
	})
	if err != nil {
		t.Fatalf("UpsertInitiative() error = %v", err)
	}

	obligation, err := service.Create(ctx, CreateParams{
		WorkspaceID:  workspaceID,
		InitiativeID: &initiative.ID,
		Title:        "Managed project follow-up",
		Cadence:      Cadence{Mode: CadenceModeOnce},
		NextDueAt:    time.Date(2026, 4, 18, 9, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if obligation.TargetProjectID != linkedProject.ID {
		t.Fatalf("TargetProjectID = %d, want %d", obligation.TargetProjectID, linkedProject.ID)
	}
}

func TestFollowUpCreateRejectsCrossWorkspaceOwnership(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openFollowUpStore(t)
	defer store.Close()

	workspaceID, _, _, _ := seedFollowUpContext(t, ctx, store)
	otherWorkspace, err := store.CreateWorkspace(ctx, sqlite.CreateWorkspaceParams{
		Key:                 "secondary",
		Name:                "Secondary Workspace",
		OwnerRef:            "operator",
		DefaultCompanionKey: "assistant",
		Status:              "active",
		PolicyJSON:          `{}`,
	})
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}

	otherCompanion, err := store.UpsertCompanion(ctx, sqlite.UpsertCompanionParams{
		WorkspaceID: otherWorkspace.ID,
		Key:         "assistant",
		Title:       "Assistant",
		Kind:        "assistant",
		Status:      "active",
	})
	if err != nil {
		t.Fatalf("UpsertCompanion() error = %v", err)
	}
	otherInitiative, err := store.UpsertInitiative(ctx, sqlite.UpsertInitiativeParams{
		WorkspaceID:      otherWorkspace.ID,
		Key:              "other-life-admin",
		Title:            "Other Life Admin",
		Kind:             "routine",
		Status:           "active",
		OwnerCompanionID: &otherCompanion.ID,
	})
	if err != nil {
		t.Fatalf("UpsertInitiative() error = %v", err)
	}

	_, err = Service{Store: store}.Create(ctx, CreateParams{
		WorkspaceID:  workspaceID,
		InitiativeID: &otherInitiative.ID,
		Title:        "Cross workspace follow-up",
		Cadence:      Cadence{Mode: CadenceModeOnce},
		NextDueAt:    time.Date(2026, 4, 18, 9, 0, 0, 0, time.UTC),
	})
	if err == nil {
		t.Fatal("Create() error = nil, want cross-workspace rejection")
	}
}

func TestFollowUpDueStatusUsesCadenceAndTimestamps(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 17, 10, 0, 0, 0, time.UTC)

	oneTime := FollowUpObligation{
		Status:    StatusActive,
		Cadence:   Cadence{Mode: CadenceModeOnce},
		NextDueAt: now.Add(-time.Minute),
	}
	if got := oneTime.DueStatus(now); got != StatusDue {
		t.Fatalf("one-time DueStatus() = %q, want due", got)
	}

	recurring := FollowUpObligation{
		Status:    StatusActive,
		Cadence:   Cadence{Mode: CadenceModeRecurring, Interval: CadenceIntervalWeekly},
		NextDueAt: now.Add(time.Hour),
	}
	if got := recurring.DueStatus(now); got != StatusActive {
		t.Fatalf("recurring DueStatus() = %q, want active", got)
	}
}

func TestFollowUpMaterializeReusesSameOccurrenceTask(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openFollowUpStore(t)
	defer store.Close()

	now := time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC)
	workspaceID, initiativeID, companionID, projectID := seedFollowUpContext(t, ctx, store)
	service := Service{
		Store: store,
		Now: func() time.Time {
			return now
		},
	}

	obligation, err := service.Create(ctx, CreateParams{
		WorkspaceID:  workspaceID,
		InitiativeID: &initiativeID,
		CompanionID:  &companionID,
		Title:        "Review mail",
		Cadence:      Cadence{Mode: CadenceModeOnce},
		NextDueAt:    now.Add(-time.Minute),
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if obligation.TargetProjectID != projectID {
		t.Fatalf("TargetProjectID = %d, want %d", obligation.TargetProjectID, projectID)
	}

	first, err := service.Materialize(ctx, MaterializeParams{
		ObligationID: obligation.ID,
		TaskKey:      "review-mail-1",
		Title:        "Review mail",
		Scope:        "project",
		RequestedBy:  "operator",
	})
	if err != nil {
		t.Fatalf("Materialize(first) error = %v", err)
	}
	if first.Reused {
		t.Fatalf("Materialize(first).Reused = true, want false")
	}
	task, err := store.GetTask(ctx, first.TaskID)
	if err != nil {
		t.Fatalf("GetTask(first) error = %v", err)
	}
	if task.ProjectID != projectID {
		t.Fatalf("Task.ProjectID = %d, want %d", task.ProjectID, projectID)
	}

	second, err := service.Materialize(ctx, MaterializeParams{
		ObligationID: obligation.ID,
		TaskKey:      "review-mail-2",
		Title:        "Review mail again",
		Scope:        "project",
		RequestedBy:  "operator",
	})
	if err != nil {
		t.Fatalf("Materialize(second) error = %v", err)
	}
	if !second.Reused {
		t.Fatalf("Materialize(second).Reused = false, want true")
	}
	if second.TaskID != first.TaskID {
		t.Fatalf("TaskID = %d, want %d", second.TaskID, first.TaskID)
	}
}

func TestFollowUpCompleteRecurringObligationAdvancesNextDueAt(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openFollowUpStore(t)
	defer store.Close()

	now := time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC)
	workspaceID, initiativeID, companionID, _ := seedFollowUpContext(t, ctx, store)
	service := Service{
		Store: store,
		Now: func() time.Time {
			return now
		},
	}

	obligation, err := service.Create(ctx, CreateParams{
		WorkspaceID:  workspaceID,
		InitiativeID: &initiativeID,
		CompanionID:  &companionID,
		Title:        "Weekly review",
		Cadence:      Cadence{Mode: CadenceModeRecurring, Interval: CadenceIntervalWeekly},
		NextDueAt:    now.Add(-time.Minute),
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	updated, err := service.Complete(ctx, workspaceID, obligation.ID)
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if updated.Status != StatusActive {
		t.Fatalf("Status = %q, want active", updated.Status)
	}
	wantNextDue := now.Add(-time.Minute).AddDate(0, 0, 7)
	if !updated.NextDueAt.Equal(wantNextDue) {
		t.Fatalf("NextDueAt = %v, want %v", updated.NextDueAt, wantNextDue)
	}
	if updated.LastCompletedAt == nil || !updated.LastCompletedAt.Equal(now) {
		t.Fatalf("LastCompletedAt = %v, want %v", updated.LastCompletedAt, now)
	}
}

func TestFollowUpSnoozeMovesNextDueAtForward(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openFollowUpStore(t)
	defer store.Close()

	now := time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC)
	workspaceID, initiativeID, companionID, _ := seedFollowUpContext(t, ctx, store)
	service := Service{
		Store: store,
		Now: func() time.Time {
			return now
		},
	}

	obligation, err := service.Create(ctx, CreateParams{
		WorkspaceID:  workspaceID,
		InitiativeID: &initiativeID,
		CompanionID:  &companionID,
		Title:        "Review mail",
		Cadence:      Cadence{Mode: CadenceModeOnce},
		NextDueAt:    now.Add(-time.Minute),
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	until := now.Add(48 * time.Hour)
	updated, err := service.Snooze(ctx, workspaceID, obligation.ID, until)
	if err != nil {
		t.Fatalf("Snooze() error = %v", err)
	}
	if updated.Status != StatusActive {
		t.Fatalf("Status = %q, want active", updated.Status)
	}
	if !updated.NextDueAt.Equal(until) {
		t.Fatalf("NextDueAt = %v, want %v", updated.NextDueAt, until)
	}
	if updated.LastCompletedAt != nil {
		t.Fatalf("LastCompletedAt = %v, want nil", updated.LastCompletedAt)
	}
}

func openFollowUpStore(t *testing.T) *sqlite.Store {
	t.Helper()

	store, err := sqlite.Open(filepath.Join(t.TempDir(), "odin.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	return store
}

func seedFollowUpContext(t *testing.T, ctx context.Context, store *sqlite.Store) (workspaceID, initiativeID, companionID, projectID int64) {
	t.Helper()

	workspace, err := workspaces.Service{Store: store}.BootstrapDefaultWorkspace(ctx)
	if err != nil {
		t.Fatalf("BootstrapDefaultWorkspace() error = %v", err)
	}

	companion, err := store.GetCompanionByKey(ctx, workspace.ID, workspace.DefaultCompanionKey)
	if err != nil {
		t.Fatalf("GetCompanionByKey() error = %v", err)
	}

	initiative, err := store.UpsertInitiative(ctx, sqlite.UpsertInitiativeParams{
		WorkspaceID:      workspace.ID,
		Key:              "life-admin",
		Title:            "Life Admin",
		Kind:             "routine",
		Status:           "active",
		OwnerCompanionID: &companion.ID,
	})
	if err != nil {
		t.Fatalf("UpsertInitiative() error = %v", err)
	}

	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "odin-core",
		Name:          "Odin Core",
		Scope:         "odin-core",
		GitRoot:       filepath.Join(t.TempDir(), "odin-core"),
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}

	return workspace.ID, initiative.ID, companion.ID, project.ID
}

func TestFollowUpMutationRejectsCrossWorkspaceAccess(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openFollowUpStore(t)
	defer store.Close()

	now := time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC)
	workspaceID, _, _, _ := seedFollowUpContext(t, ctx, store)
	otherWorkspace, err := store.CreateWorkspace(ctx, sqlite.CreateWorkspaceParams{
		Key:                 "secondary",
		Name:                "Secondary Workspace",
		OwnerRef:            "operator",
		DefaultCompanionKey: "assistant",
		Status:              "active",
		PolicyJSON:          `{}`,
	})
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}
	otherCompanion, err := store.UpsertCompanion(ctx, sqlite.UpsertCompanionParams{
		WorkspaceID: otherWorkspace.ID,
		Key:         "assistant",
		Title:       "Assistant",
		Kind:        "assistant",
		Status:      "active",
	})
	if err != nil {
		t.Fatalf("UpsertCompanion() error = %v", err)
	}
	otherInitiative, err := store.UpsertInitiative(ctx, sqlite.UpsertInitiativeParams{
		WorkspaceID:      otherWorkspace.ID,
		Key:              "other-life-admin",
		Title:            "Other Life Admin",
		Kind:             "routine",
		Status:           "active",
		OwnerCompanionID: &otherCompanion.ID,
	})
	if err != nil {
		t.Fatalf("UpsertInitiative() error = %v", err)
	}

	service := Service{
		Store: store,
		Now: func() time.Time {
			return now
		},
	}

	obligation, err := service.Create(ctx, CreateParams{
		WorkspaceID:  otherWorkspace.ID,
		InitiativeID: &otherInitiative.ID,
		Title:        "Cross workspace follow-up",
		Cadence:      Cadence{Mode: CadenceModeOnce},
		NextDueAt:    now.Add(-time.Minute),
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if _, err := service.Complete(ctx, workspaceID, obligation.ID); err == nil {
		t.Fatal("Complete() error = nil, want cross-workspace rejection")
	}

	if _, err := service.Snooze(ctx, workspaceID, obligation.ID, now.Add(time.Hour)); err == nil {
		t.Fatal("Snooze() error = nil, want cross-workspace rejection")
	}
}
