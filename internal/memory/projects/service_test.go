package projects

import (
	"context"
	"path/filepath"
	"testing"

	"odin-os/internal/memory/users"
	"odin-os/internal/store/sqlite"
)

func TestServiceListsProjectMemoryBeforeGlobalMemory(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	project := createProject(t, ctx, store, "alpha")
	globalService := users.Service{Store: store}
	globalEntry, err := globalService.Remember(ctx, "user_preference", "Prefer concise replies.", `{"source":"test"}`)
	if err != nil {
		t.Fatalf("global Remember() error = %v", err)
	}

	service := Service{Store: store, ProjectID: project.ID, ProjectKey: project.Key}
	projectEntry, err := service.Remember(ctx, "project_summary", "Alpha uses worktree isolation.", `{"source":"test"}`, nil)
	if err != nil {
		t.Fatalf("Remember() error = %v", err)
	}

	entries, err := service.List(ctx)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries len = %d, want 2", len(entries))
	}
	if entries[0].ID != projectEntry.ID || entries[1].ID != globalEntry.ID {
		t.Fatalf("entries order = %+v, want project then global", entries)
	}
}

func openTestStore(t *testing.T) *sqlite.Store {
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

func createProject(t *testing.T, ctx context.Context, store *sqlite.Store, key string) sqlite.Project {
	t.Helper()

	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           key,
		Name:          key,
		Scope:         "project",
		GitRoot:       filepath.Join(t.TempDir(), key),
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject(%s) error = %v", key, err)
	}
	return project
}
