package companions

import (
	"context"
	"path/filepath"
	"testing"

	memoryroot "odin-os/internal/memory"
	memoryworkspaces "odin-os/internal/memory/workspaces"
	"odin-os/internal/store/sqlite"
)

func TestMemoryCompanionServiceRecallsCompanionThenWorkspaceEntries(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openCompanionMemoryStore(t)
	defer store.Close()

	workspace, err := store.GetWorkspaceByKey(ctx, "default")
	if err != nil {
		t.Fatalf("GetWorkspaceByKey(default) error = %v", err)
	}
	companion, err := store.GetCompanionByKey(ctx, workspace.ID, workspace.DefaultCompanionKey)
	if err != nil {
		t.Fatalf("GetCompanionByKey(default) error = %v", err)
	}

	workspaceService := memoryworkspaces.Service{Store: store}
	if _, err := workspaceService.Record(ctx, workspace.ID, memoryroot.WriteInput{
		EntryType:       memoryroot.EntryTypeNote,
		VisibilityScope: memoryroot.VisibilityWorkspace,
		RetentionClass:  memoryroot.RetentionDurable,
		Summary:         "Workspace default",
		Content:         "Workspace context comes after companion-specific context.",
		MetadataJSON:    `{"source":"workspace"}`,
	}); err != nil {
		t.Fatalf("workspace Record() error = %v", err)
	}

	service := Service{Store: store}
	companionEntry, err := service.Record(ctx, workspace.ID, companion.ID, memoryroot.WriteInput{
		EntryType:       memoryroot.EntryTypeNote,
		VisibilityScope: memoryroot.VisibilityCompanion,
		RetentionClass:  memoryroot.RetentionDurable,
		Summary:         "Companion focus",
		Content:         "This companion owns Marcus's daily operating cadence.",
		MetadataJSON:    `{"source":"companion"}`,
	})
	if err != nil {
		t.Fatalf("Record() error = %v", err)
	}

	entries, err := service.Recall(ctx, workspace.ID, companion.ID, 10)
	if err != nil {
		t.Fatalf("Recall() error = %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("Recall() len = %d, want 2", len(entries))
	}
	if entries[0].ID != companionEntry.ID {
		t.Fatalf("Recall()[0].ID = %d, want companion entry %d first", entries[0].ID, companionEntry.ID)
	}
}

func openCompanionMemoryStore(t *testing.T) *sqlite.Store {
	t.Helper()

	store, err := sqlite.Open(filepath.Join(t.TempDir(), "odin.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	return store
}
