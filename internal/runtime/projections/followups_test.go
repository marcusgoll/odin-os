package projections_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"odin-os/internal/runtime/projections"
	"odin-os/internal/store/sqlite"
)

func TestFollowUpSummaryViewsListDueFollowUps(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openObservabilityStore(t)
	defer store.Close()

	project, workspace, initiative, companion := seedOperatorReadModelState(t, ctx, store)
	now := time.Date(2026, 4, 17, 9, 0, 0, 0, time.UTC)

	due := createFollowUpObligation(t, ctx, store, project.ID, workspace.ID, initiative.ID, companion.ID, "Review mail", now)
	createFollowUpObligation(t, ctx, store, project.ID, workspace.ID, initiative.ID, companion.ID, "Future review", now.Add(24*time.Hour))

	views, err := projections.ListDueFollowUpSummaryViews(ctx, store.DB(), workspace.Key, now)
	if err != nil {
		t.Fatalf("ListDueFollowUpSummaryViews() error = %v", err)
	}
	if len(views) != 1 {
		t.Fatalf("due views len = %d, want 1", len(views))
	}
	if views[0].ObligationID != due.ID {
		t.Fatalf("due obligation id = %d, want %d", views[0].ObligationID, due.ID)
	}
	if views[0].DueStatus != "due" {
		t.Fatalf("due status = %q, want due", views[0].DueStatus)
	}
}

func TestFollowUpSummaryViewsListOverdueFollowUps(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openObservabilityStore(t)
	defer store.Close()

	project, workspace, initiative, companion := seedOperatorReadModelState(t, ctx, store)
	now := time.Date(2026, 4, 17, 9, 0, 0, 0, time.UTC)

	overdue := createFollowUpObligation(t, ctx, store, project.ID, workspace.ID, initiative.ID, companion.ID, "File taxes", now.Add(-48*time.Hour))
	createFollowUpObligation(t, ctx, store, project.ID, workspace.ID, initiative.ID, companion.ID, "Future review", now.Add(24*time.Hour))

	views, err := projections.ListOverdueFollowUpSummaryViews(ctx, store.DB(), workspace.Key, now)
	if err != nil {
		t.Fatalf("ListOverdueFollowUpSummaryViews() error = %v", err)
	}
	if len(views) != 1 {
		t.Fatalf("overdue views len = %d, want 1", len(views))
	}
	if views[0].ObligationID != overdue.ID {
		t.Fatalf("overdue obligation id = %d, want %d", views[0].ObligationID, overdue.ID)
	}
	if views[0].DueStatus != "overdue" {
		t.Fatalf("due status = %q, want overdue", views[0].DueStatus)
	}
}

func TestAgendaViewIncludesDueWorkBlockedWorkAndApprovals(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openObservabilityStore(t)
	defer store.Close()

	project, workspace, initiative, companion := seedOperatorReadModelState(t, ctx, store)
	now := time.Date(2026, 4, 17, 9, 0, 0, 0, time.UTC)

	createFollowUpObligation(t, ctx, store, project.ID, workspace.ID, initiative.ID, companion.ID, "Review mail", now)
	createFollowUpObligation(t, ctx, store, project.ID, workspace.ID, initiative.ID, companion.ID, "File taxes", now.Add(-48*time.Hour))

	wakeTask, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:    project.ID,
		Key:          "alpha-wake",
		Title:        "Resume wake packet",
		Status:       "blocked",
		Scope:        "project",
		RequestedBy:  "operator",
		WorkspaceID:  &workspace.ID,
		InitiativeID: &initiative.ID,
		CompanionID:  &companion.ID,
		WorkKind:     "follow_up",
	})
	if err != nil {
		t.Fatalf("CreateTask(alpha-wake) error = %v", err)
	}
	if _, err := store.CreateContextPacket(ctx, sqlite.CreateContextPacketParams{
		TaskID:        &wakeTask.ID,
		PacketKind:    "wake",
		PacketScope:   "task_wake_packet",
		Trigger:       "follow_up_wait",
		CheckpointKey: "agenda-wake",
		Status:        "active",
		Summary:       "waiting on follow-up context",
		PayloadJSON:   fmt.Sprintf(`{"task_id":%d,"task_key":"%s","scope":"project","objective":"Resume wake work","status":"waiting","trigger":"follow_up_wait","blocking_reason":"waiting on supporting context"}`, wakeTask.ID, wakeTask.Key),
	}); err != nil {
		t.Fatalf("CreateContextPacket() error = %v", err)
	}

	agenda, err := projections.GetAgendaView(ctx, store.DB(), workspace.Key, now)
	if err != nil {
		t.Fatalf("GetAgendaView() error = %v", err)
	}
	if agenda.WorkspaceKey != workspace.Key {
		t.Fatalf("agenda workspace key = %q, want %q", agenda.WorkspaceKey, workspace.Key)
	}
	if len(agenda.DueWork) != 2 {
		t.Fatalf("agenda due work len = %d, want 2", len(agenda.DueWork))
	}
	if agenda.DueWork[0].DueStatus != "overdue" || agenda.DueWork[1].DueStatus != "due" {
		t.Fatalf("agenda due work = %+v, want overdue followed by due", agenda.DueWork)
	}
	if len(agenda.BlockedWork) < 2 {
		t.Fatalf("agenda blocked work len = %d, want at least 2", len(agenda.BlockedWork))
	}
	if len(agenda.Approvals) != 1 {
		t.Fatalf("agenda approvals len = %d, want 1", len(agenda.Approvals))
	}
	if agenda.Approvals[0].TaskKey != "alpha-task" {
		t.Fatalf("agenda approval task = %q, want alpha-task", agenda.Approvals[0].TaskKey)
	}
}

func createFollowUpObligation(t *testing.T, ctx context.Context, store *sqlite.Store, projectID, workspaceID, initiativeID, companionID int64, title string, nextDueAt time.Time) sqlite.FollowUpObligation {
	t.Helper()

	obligation, err := store.CreateFollowUpObligation(ctx, sqlite.CreateFollowUpObligationParams{
		WorkspaceID:     workspaceID,
		InitiativeID:    &initiativeID,
		CompanionID:     &companionID,
		TargetProjectID: projectID,
		Title:           title,
		Status:          "active",
		CadenceJSON:     `{"mode":"once"}`,
		NextDueAt:       nextDueAt,
		PolicyJSON:      `{}`,
	})
	if err != nil {
		t.Fatalf("CreateFollowUpObligation(%s) error = %v", title, err)
	}
	return obligation
}
