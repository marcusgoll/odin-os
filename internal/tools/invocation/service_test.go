package invocation

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"odin-os/internal/store/sqlite"
)

func TestBuiltinToolInvokesRuntimeDriver(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	runtimeRoot := t.TempDir()
	store := openInvocationStore(t, runtimeRoot)
	defer store.Close()

	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "alpha",
		Name:          "Alpha",
		Scope:         "project",
		GitRoot:       runtimeRoot,
		DefaultBranch: "main",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	if _, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "alpha-queued",
		Title:       "Queued runtime task",
		Status:      "queued",
		Scope:       "project",
		RequestedBy: "test",
	}); err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	service := Service{RuntimeRoot: runtimeRoot}
	result, err := service.Invoke(ctx, "project_status", Request{
		Args: map[string]string{"project_key": "alpha"},
	})
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if result.Source != "driver" {
		t.Fatalf("source = %q, want driver", result.Source)
	}
	if result.KeyFacts["project_key"] != "alpha" {
		t.Fatalf("project_key fact = %q, want alpha", result.KeyFacts["project_key"])
	}
	if result.KeyFacts["open_task_count"] != "1" {
		t.Fatalf("open_task_count = %q, want 1", result.KeyFacts["open_task_count"])
	}
	if !strings.Contains(result.RawOutput, "project=alpha") {
		t.Fatalf("raw output = %q, want project marker", result.RawOutput)
	}
}

func openInvocationStore(t *testing.T, runtimeRoot string) *sqlite.Store {
	t.Helper()

	dataDir := filepath.Join(runtimeRoot, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(data) error = %v", err)
	}
	store, err := sqlite.Open(filepath.Join(dataDir, "odin.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		_ = store.Close()
		t.Fatalf("Migrate() error = %v", err)
	}
	return store
}
