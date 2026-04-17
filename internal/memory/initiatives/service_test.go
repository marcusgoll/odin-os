package initiatives

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"odin-os/internal/store/sqlite"
)

func TestInitiativeMemoryRecallIncludesWorkspaceAndUserPreferenceScopes(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openInitiativeMemoryStore(t)
	defer store.Close()

	workspace, initiative := seedInitiativeMemoryContext(t, ctx, store, "alpha")
	mustRememberMemory(t, ctx, store, sqlite.CreateMemoryEntryParams{
		ScopeType:       "user_preference_memory",
		ScopeKey:        "user:marcus",
		SourceScope:     "user",
		VisibilityScope: "workspace",
		RetentionIntent: "durable",
		Summary:         "Marcus prefers concise follow-ups.",
	})
	mustRememberMemory(t, ctx, store, sqlite.CreateMemoryEntryParams{
		ScopeType:       "workspace_memory",
		ScopeKey:        "workspace:" + workspace.Key,
		SourceScope:     "workspace",
		VisibilityScope: "workspace",
		RetentionIntent: "durable",
		Summary:         "Workspace context for current obligations.",
	})
	mustRememberMemory(t, ctx, store, sqlite.CreateMemoryEntryParams{
		ScopeType:       "initiative_memory",
		ScopeKey:        fmt.Sprintf("workspace:%s/initiative:%s", workspace.Key, initiative.Key),
		SourceScope:     "initiative",
		VisibilityScope: "initiative",
		RetentionIntent: "durable",
		Summary:         "Initiative-specific delivery preference.",
	})

	entries, err := Service{Store: store}.Recall(ctx, workspace.Key, initiative.Key)
	if err != nil {
		t.Fatalf("Recall() error = %v", err)
	}

	assertMemorySummaries(t, entries,
		"Initiative-specific delivery preference.",
		"Workspace context for current obligations.",
		"Marcus prefers concise follow-ups.",
	)
}

func TestMemoryScopeBlocksProjectMemoryFromUnrelatedInitiativeViews(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openInitiativeMemoryStore(t)
	defer store.Close()

	workspace, _ := seedInitiativeMemoryContext(t, ctx, store, "alpha")
	otherInitiative, err := store.CreateInitiative(ctx, sqlite.CreateInitiativeParams{
		WorkspaceID: workspace.ID,
		Key:         "desk-admin",
		Title:       "Desk Admin",
		Kind:        "routine",
		Status:      "active",
		Summary:     "Administrative follow-up.",
	})
	if err != nil {
		t.Fatalf("CreateInitiative() error = %v", err)
	}

	mustRememberMemory(t, ctx, store, sqlite.CreateMemoryEntryParams{
		ScopeType:       "project_memory",
		ScopeKey:        "project:alpha",
		SourceScope:     "project",
		VisibilityScope: "project",
		RetentionIntent: "durable",
		Summary:         "Alpha project memory should stay local.",
	})

	entries, err := Service{Store: store}.Recall(ctx, workspace.Key, otherInitiative.Key)
	if err != nil {
		t.Fatalf("Recall() error = %v", err)
	}

	for _, entry := range entries {
		if entry.Summary == "Alpha project memory should stay local." {
			t.Fatalf("Recall() leaked project memory into initiative view: %+v", entries)
		}
	}
}

func openInitiativeMemoryStore(t *testing.T) *sqlite.Store {
	t.Helper()

	store, err := sqlite.Open(filepath.Join(t.TempDir(), "odin.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		_ = store.Close()
		t.Fatalf("Migrate() error = %v", err)
	}
	return store
}

func seedInitiativeMemoryContext(t *testing.T, ctx context.Context, store *sqlite.Store, projectKey string) (sqlite.Workspace, sqlite.Initiative) {
	t.Helper()

	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           projectKey,
		Name:          "Alpha",
		Scope:         "project",
		GitRoot:       filepath.Join("/tmp", projectKey),
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}

	workspace, err := store.GetWorkspaceByKey(ctx, "marcus")
	if err != nil {
		t.Fatalf("GetWorkspaceByKey(marcus) error = %v", err)
	}
	initiative, err := store.GetInitiativeByProjectID(ctx, project.ID)
	if err != nil {
		t.Fatalf("GetInitiativeByProjectID() error = %v", err)
	}

	return workspace, initiative
}

func mustRememberMemory(t *testing.T, ctx context.Context, store *sqlite.Store, params sqlite.CreateMemoryEntryParams) {
	t.Helper()

	if _, err := store.CreateMemoryEntry(ctx, params); err != nil {
		t.Fatalf("CreateMemoryEntry() error = %v", err)
	}
}

func assertMemorySummaries(t *testing.T, entries []sqlite.MemoryEntry, want ...string) {
	t.Helper()

	if len(entries) != len(want) {
		t.Fatalf("len(entries) = %d, want %d (%+v)", len(entries), len(want), entries)
	}
	for index, summary := range want {
		if entries[index].Summary != summary {
			t.Fatalf("entries[%d].Summary = %q, want %q", index, entries[index].Summary, summary)
		}
	}
}
