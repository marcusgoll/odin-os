package sqlite

import (
	"context"
	"reflect"
	"testing"
)

func TestTaskAcceptanceCriteriaPersistAndUpdate(t *testing.T) {
	ctx := context.Background()
	store := openMigratedTestStore(t, "task-acceptance-criteria.db")
	defer store.Close()

	project, err := store.CreateProject(ctx, CreateProjectParams{
		Key:           "alpha",
		Name:          "Alpha",
		Scope:         "project",
		GitRoot:       "/tmp/alpha",
		DefaultBranch: "main",
		ManifestPath:  "registry/projects/alpha.md",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}

	task, err := store.CreateTask(ctx, CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "WI-68",
		Title:       "Persist criteria",
		Status:      "queued",
		Scope:       "project",
		RequestedBy: "test",
		AcceptanceCriteria: []string{
			"prompt renders stored criteria",
			"  ",
			"criteria survive reload",
		},
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	wantInitial := []string{"prompt renders stored criteria", "criteria survive reload"}
	if !reflect.DeepEqual(task.AcceptanceCriteria, wantInitial) {
		t.Fatalf("CreateTask().AcceptanceCriteria = %#v, want %#v", task.AcceptanceCriteria, wantInitial)
	}

	reloaded, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if !reflect.DeepEqual(reloaded.AcceptanceCriteria, wantInitial) {
		t.Fatalf("GetTask().AcceptanceCriteria = %#v, want %#v", reloaded.AcceptanceCriteria, wantInitial)
	}

	updated, err := store.UpdateTaskAcceptanceCriteria(ctx, task.ID, []string{
		"updated criterion",
		"updated criterion",
		"second criterion",
	})
	if err != nil {
		t.Fatalf("UpdateTaskAcceptanceCriteria() error = %v", err)
	}
	wantUpdated := []string{"updated criterion", "second criterion"}
	if !reflect.DeepEqual(updated.AcceptanceCriteria, wantUpdated) {
		t.Fatalf("UpdateTaskAcceptanceCriteria().AcceptanceCriteria = %#v, want %#v", updated.AcceptanceCriteria, wantUpdated)
	}
}
