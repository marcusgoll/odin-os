package companions

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	corecompanions "odin-os/internal/core/companions"
	"odin-os/internal/store/sqlite"
)

func TestCompanionMemoryRecallIncludesWorkspaceAndUserPreferenceScopes(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openCompanionMemoryStore(t)
	defer store.Close()

	workspace, companion := seedCompanionMemoryContext(t, ctx, store)
	mustRememberCompanionMemory(t, ctx, store, sqlite.CreateMemoryEntryParams{
		ScopeType:       "user_preference_memory",
		ScopeKey:        "user:marcus",
		SourceScope:     "user",
		VisibilityScope: "workspace",
		RetentionIntent: "durable",
		Summary:         "Marcus wants terse companion replies.",
	})
	mustRememberCompanionMemory(t, ctx, store, sqlite.CreateMemoryEntryParams{
		ScopeType:       "workspace_memory",
		ScopeKey:        "workspace:" + workspace.Key,
		SourceScope:     "workspace",
		VisibilityScope: "workspace",
		RetentionIntent: "durable",
		Summary:         "Workspace memory for active operating rhythm.",
	})
	mustRememberCompanionMemory(t, ctx, store, sqlite.CreateMemoryEntryParams{
		ScopeType:       "companion_memory",
		ScopeKey:        fmt.Sprintf("workspace:%s/companion:%s", workspace.Key, companion.Key),
		SourceScope:     "companion",
		VisibilityScope: "companion",
		RetentionIntent: "durable",
		Summary:         "Companion-specific planning habit.",
	})

	entries, err := Service{Store: store}.Recall(ctx, workspace.Key, companion.Key)
	if err != nil {
		t.Fatalf("Recall() error = %v", err)
	}

	assertCompanionSummaries(t, entries,
		"Companion-specific planning habit.",
		"Workspace memory for active operating rhythm.",
		"Marcus wants terse companion replies.",
	)
}

func openCompanionMemoryStore(t *testing.T) *sqlite.Store {
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

func seedCompanionMemoryContext(t *testing.T, ctx context.Context, store *sqlite.Store) (sqlite.Workspace, sqlite.Companion) {
	t.Helper()

	workspace, err := store.CreateWorkspace(ctx, sqlite.CreateWorkspaceParams{
		Key:      "ops",
		Name:     "Ops Workspace",
		OwnerRef: "marcus",
		Status:   "active",
	})
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}
	companion, err := store.CreateCompanion(ctx, sqlite.CreateCompanionParams{
		WorkspaceID:         workspace.ID,
		Key:                 "operator",
		Title:               "Operator",
		Kind:                corecompanions.KindOperator,
		Charter:             "Run the workspace operating rhythm.",
		Status:              corecompanions.StatusActive,
		InitiativeScopeJSON: `{"mode":"all"}`,
		ToolPolicyJSON:      `{"mode":"deny","allowed":[]}`,
		MemoryPolicyJSON:    `{"retention":"workspace"}`,
		PlanningPolicyJSON:  `{"mode":"stepwise"}`,
	})
	if err != nil {
		t.Fatalf("CreateCompanion() error = %v", err)
	}
	return workspace, companion
}

func mustRememberCompanionMemory(t *testing.T, ctx context.Context, store *sqlite.Store, params sqlite.CreateMemoryEntryParams) {
	t.Helper()

	if _, err := store.CreateMemoryEntry(ctx, params); err != nil {
		t.Fatalf("CreateMemoryEntry() error = %v", err)
	}
}

func assertCompanionSummaries(t *testing.T, entries []sqlite.MemoryEntry, want ...string) {
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
