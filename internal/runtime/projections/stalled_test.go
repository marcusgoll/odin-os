package projections_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"odin-os/internal/runtime/projections"
	"odin-os/internal/store/sqlite"
)

func TestListStalledRunViewsFiltersOldRunningRuns(t *testing.T) {
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

	if len(views) != 1 {
		t.Fatalf("ListStalledRunViews() len = %d, want 1", len(views))
	}
	if views[0].TaskKey != "stalled-task" {
		t.Fatalf("stalled task key = %q, want stalled-task", views[0].TaskKey)
	}
}
