package projections_test

import (
	"context"
	"testing"

	"odin-os/internal/runtime/projections"
	"odin-os/internal/store/sqlite"
)

func TestTaskIntakeEvidenceViewsListLinkedTaskIntakes(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openObservabilityStore(t)
	defer store.Close()

	project, workspace, initiative, companion := seedOperatorReadModelState(t, ctx, store)
	task, err := store.GetTaskByProjectAndKey(ctx, project.ID, "alpha-task")
	if err != nil {
		t.Fatalf("GetTask(alpha-task) error = %v", err)
	}
	intake, err := store.CreateTaskIntake(ctx, sqlite.CreateTaskIntakeParams{
		TaskID:      task.ID,
		Source:      "n8n",
		IntakeType:  "ci_failure",
		DedupKey:    "ci_failure:alpha:42",
		RequestedBy: "n8n",
		PayloadJSON: `{"workflow_id":"alpha-ci","run_id":"42"}`,
	})
	if err != nil {
		t.Fatalf("CreateTaskIntake() error = %v", err)
	}

	views, err := projections.ListTaskIntakeEvidenceViews(ctx, store.DB(), workspace.Key)
	if err != nil {
		t.Fatalf("ListTaskIntakeEvidenceViews() error = %v", err)
	}
	if len(views) != 1 {
		t.Fatalf("intake evidence views len = %d, want 1", len(views))
	}
	view := views[0]
	if view.IntakeID != intake.ID {
		t.Fatalf("intake id = %d, want %d", view.IntakeID, intake.ID)
	}
	if view.Source != "n8n" || view.IntakeType != "ci_failure" || view.DedupKey != "ci_failure:alpha:42" {
		t.Fatalf("intake identity = %+v, want n8n ci_failure ci_failure:alpha:42", view)
	}
	if view.WorkspaceKey != workspace.Key {
		t.Fatalf("workspace key = %q, want %q", view.WorkspaceKey, workspace.Key)
	}
	if view.ProjectKey != "alpha" || view.WorkItemKey != task.Key || view.WorkItemStatus != task.Status {
		t.Fatalf("linked work = %+v, want alpha %s %s", view, task.Key, task.Status)
	}
	if view.InitiativeKey == nil || *view.InitiativeKey != initiative.Key {
		t.Fatalf("initiative key = %v, want %q", view.InitiativeKey, initiative.Key)
	}
	if view.CompanionKey == nil || *view.CompanionKey != companion.Key {
		t.Fatalf("companion key = %v, want %q", view.CompanionKey, companion.Key)
	}
}
