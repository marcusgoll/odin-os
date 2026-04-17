package users

import (
	"context"
	"path/filepath"
	"testing"

	"odin-os/internal/store/sqlite"
)

func TestServiceListsOnlyGlobalMemory(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	project := createProject(t, ctx, store, "alpha", "project")
	if _, err := store.RecordMemorySummary(ctx, sqlite.RecordMemorySummaryParams{
		ProjectID:   &project.ID,
		Scope:       "project",
		ScopeKey:    project.Key,
		MemoryType:  "project_summary",
		Summary:     "Alpha local convention",
		DetailsJSON: `{"source":"test"}`,
	}); err != nil {
		t.Fatalf("RecordMemorySummary(project) error = %v", err)
	}

	service := Service{Store: store}
	globalEntry, err := service.Remember(ctx, "user_preference", "Prefer concise replies.", `{"source":"test"}`)
	if err != nil {
		t.Fatalf("Remember() error = %v", err)
	}

	entries, err := service.List(ctx)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(entries) != 1 || entries[0].ID != globalEntry.ID {
		t.Fatalf("entries = %+v, want only global memory", entries)
	}
}

func TestServiceListsWorkspaceMemoryBeforeGlobalMemory(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	service := Service{
		Store:          store,
		WorkspaceScope: "workspace",
		WorkspaceKey:   "primary",
	}

	globalEntry, err := store.RecordMemorySummary(ctx, sqlite.RecordMemorySummaryParams{
		Scope:       "global",
		ScopeKey:    "global",
		MemoryType:  "user_preference",
		Summary:     "Prefer concise replies.",
		DetailsJSON: `{"source":"test"}`,
	})
	if err != nil {
		t.Fatalf("RecordMemorySummary(global) error = %v", err)
	}

	workspaceEntry, err := service.Remember(ctx, "operator_preference", "Use workspace defaults.", `{"source":"test"}`)
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
	if entries[0].ID != workspaceEntry.ID || entries[1].ID != globalEntry.ID {
		t.Fatalf("entries = %+v, want workspace then global", entries)
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

func createProject(t *testing.T, ctx context.Context, store *sqlite.Store, key string, scope string) sqlite.Project {
	t.Helper()

	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           key,
		Name:          key,
		Scope:         scope,
		GitRoot:       filepath.Join(t.TempDir(), key),
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject(%s) error = %v", key, err)
	}
	return project
}
