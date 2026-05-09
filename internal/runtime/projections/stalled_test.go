package projections_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"odin-os/internal/runtime/projections"
	"odin-os/internal/store/sqlite"
)

func TestListStalledRunViewsFiltersOldRunningAndExecutingRuns(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "odin.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	now := time.Now().UTC()
	store.Now = func() time.Time { return now.Add(-2 * time.Hour) }

	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "alpha",
		Name:          "Alpha",
		Scope:         "project",
		GitRoot:       "/tmp/alpha",
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}

	stalledTask, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "stalled-task",
		Title:       "Stalled task",
		Status:      "running",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask(stalled) error = %v", err)
	}
	if _, err := store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:   stalledTask.ID,
		Executor: "codex",
		Attempt:  1,
		Status:   "running",
	}); err != nil {
		t.Fatalf("StartRun(stalled) error = %v", err)
	}

	stalledExecutingTask, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "stalled-executing-task",
		Title:       "Stalled executing task",
		Status:      "running",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask(stalled executing) error = %v", err)
	}
	if _, err := store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:   stalledExecutingTask.ID,
		Executor: "codex",
		Attempt:  1,
		Status:   "executing",
	}); err != nil {
		t.Fatalf("StartRun(stalled executing) error = %v", err)
	}

	recentTask, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "recent-task",
		Title:       "Recent task",
		Status:      "running",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask(recent) error = %v", err)
	}
	store.Now = func() time.Time { return now }
	if _, err := store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:   recentTask.ID,
		Executor: "codex",
		Attempt:  1,
		Status:   "running",
	}); err != nil {
		t.Fatalf("StartRun(recent) error = %v", err)
	}

	views, err := projections.ListStalledRunViews(ctx, store.DB(), now.Add(-30*time.Minute))
	if err != nil {
		t.Fatalf("ListStalledRunViews() error = %v", err)
	}

	if len(views) != 2 {
		t.Fatalf("ListStalledRunViews() len = %d, want 2", len(views))
	}
	wantKeys := map[string]bool{
		"stalled-task":           false,
		"stalled-executing-task": false,
	}
	for _, view := range views {
		_, ok := wantKeys[view.TaskKey]
		if !ok {
			t.Fatalf("stalled task key = %q, want one of %#v", view.TaskKey, wantKeys)
		}
		wantKeys[view.TaskKey] = true
	}
	for key, seen := range wantKeys {
		if !seen {
			t.Fatalf("ListStalledRunViews() missing task key %q; views=%+v", key, views)
		}
	}
}
