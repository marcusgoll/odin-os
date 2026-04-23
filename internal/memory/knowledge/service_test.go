package knowledge

import (
	"context"
	"path/filepath"
	"testing"

	"odin-os/internal/store/sqlite"
)

func TestServiceMergesProjectAndGlobalKnowledge(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	project := createProject(t, ctx, store, "alpha")
	service := Service{Store: store}

	globalEntry, err := service.Record(ctx, Scope{Value: "global", Key: "global"}, "knowledge", "Global preference", `{"source":"test"}`, nil)
	if err != nil {
		t.Fatalf("Record(global) error = %v", err)
	}
	projectEntry, err := service.Record(ctx, Scope{ProjectID: &project.ID, Value: "project", Key: project.Key}, "knowledge", "Project convention", `{"source":"test"}`, nil)
	if err != nil {
		t.Fatalf("Record(project) error = %v", err)
	}

	globalEntries, err := service.List(ctx, Scope{Value: "global", Key: "global"}, "knowledge")
	if err != nil {
		t.Fatalf("List(global) error = %v", err)
	}
	if len(globalEntries) != 1 || globalEntries[0].ID != globalEntry.ID {
		t.Fatalf("global entries = %+v, want only global knowledge", globalEntries)
	}

	projectEntries, err := service.List(ctx, Scope{ProjectID: &project.ID, Value: "project", Key: project.Key}, "knowledge")
	if err != nil {
		t.Fatalf("List(project) error = %v", err)
	}
	if len(projectEntries) != 2 {
		t.Fatalf("project entries len = %d, want 2", len(projectEntries))
	}
	if projectEntries[0].ID != projectEntry.ID {
		t.Fatalf("project entries[0] = %+v, want project knowledge first", projectEntries[0])
	}
	if projectEntries[1].ID != globalEntry.ID {
		t.Fatalf("project entries[1] = %+v, want global knowledge fallback second", projectEntries[1])
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
