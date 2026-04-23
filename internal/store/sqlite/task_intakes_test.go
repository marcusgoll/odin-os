package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestCreateTaskIntake(t *testing.T) {
	ctx := context.Background()
	store := openMigratedTaskIntakeStore(t, "task-intakes.db")
	defer store.Close()

	_, task := seedTaskIntakeTask(t, ctx, store)

	intake, err := store.CreateTaskIntake(ctx, CreateTaskIntakeParams{
		TaskID:      task.ID,
		Source:      "n8n",
		IntakeType:  "ci_failure",
		DedupKey:    "ci_failure:pbs:1234",
		RequestedBy: "n8n",
		PayloadJSON: `{"run_id":"1234"}`,
	})
	if err != nil {
		t.Fatalf("CreateTaskIntake(first) error = %v", err)
	}

	if intake.TaskID != task.ID {
		t.Fatalf("CreateTaskIntake(first).TaskID = %d, want %d", intake.TaskID, task.ID)
	}
	if intake.Source != "n8n" {
		t.Fatalf("CreateTaskIntake(first).Source = %q, want %q", intake.Source, "n8n")
	}
	if intake.IntakeType != "ci_failure" {
		t.Fatalf("CreateTaskIntake(first).IntakeType = %q, want %q", intake.IntakeType, "ci_failure")
	}
	if intake.DedupKey != "ci_failure:pbs:1234" {
		t.Fatalf("CreateTaskIntake(first).DedupKey = %q, want %q", intake.DedupKey, "ci_failure:pbs:1234")
	}
	if intake.PayloadJSON != `{"run_id":"1234"}` {
		t.Fatalf("CreateTaskIntake(first).PayloadJSON = %q, want %q", intake.PayloadJSON, `{"run_id":"1234"}`)
	}

	got, err := store.GetTaskIntake(ctx, intake.ID)
	if err != nil {
		t.Fatalf("GetTaskIntake() error = %v", err)
	}
	if got.ID != intake.ID {
		t.Fatalf("GetTaskIntake().ID = %d, want %d", got.ID, intake.ID)
	}
	if got.RequestedBy != "n8n" {
		t.Fatalf("GetTaskIntake().RequestedBy = %q, want %q", got.RequestedBy, "n8n")
	}

	if _, err := store.CreateTaskIntake(ctx, CreateTaskIntakeParams{
		TaskID:      task.ID,
		Source:      "n8n",
		IntakeType:  "ci_failure",
		DedupKey:    "ci_failure:pbs:1234",
		RequestedBy: "n8n",
		PayloadJSON: `{"run_id":"1234","duplicate":true}`,
	}); !errors.Is(err, ErrTaskIntakeConflict) {
		t.Fatalf("CreateTaskIntake(duplicate) error = %v, want ErrTaskIntakeConflict", err)
	}

	blankFirst, err := store.CreateTaskIntake(ctx, CreateTaskIntakeParams{
		TaskID:      task.ID,
		Source:      "n8n",
		IntakeType:  "manual_followup",
		DedupKey:    "",
		RequestedBy: "n8n",
		PayloadJSON: `{"run_id":"blank-1"}`,
	})
	if err != nil {
		t.Fatalf("CreateTaskIntake(blank first) error = %v", err)
	}

	blankSecond, err := store.CreateTaskIntake(ctx, CreateTaskIntakeParams{
		TaskID:      task.ID,
		Source:      "n8n",
		IntakeType:  "manual_followup",
		DedupKey:    "",
		RequestedBy: "n8n",
		PayloadJSON: `{"run_id":"blank-2"}`,
	})
	if err != nil {
		t.Fatalf("CreateTaskIntake(blank second) error = %v", err)
	}
	if blankSecond.ID == blankFirst.ID {
		t.Fatalf("CreateTaskIntake(blank second).ID = %d, want distinct row", blankSecond.ID)
	}
}

func openMigratedTaskIntakeStore(t *testing.T, name string) *Store {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), name)
	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	if err := store.Migrate(context.Background()); err != nil {
		_ = store.Close()
		t.Fatalf("Migrate() error = %v", err)
	}

	return store
}

func seedTaskIntakeTask(t *testing.T, ctx context.Context, store *Store) (Project, Task) {
	t.Helper()

	project, err := store.CreateProject(ctx, CreateProjectParams{
		Key:           "pbs",
		Name:          "PBS",
		Scope:         "project",
		GitRoot:       "/home/orchestrator/projects/pbs",
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}

	task, err := store.CreateTask(ctx, CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "intake-task",
		Title:       "Handle intake record",
		Status:      "queued",
		Scope:       "project",
		RequestedBy: "n8n",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	return project, task
}
