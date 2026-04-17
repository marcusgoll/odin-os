package workspaces

import (
	"context"
	"path/filepath"
	"testing"

	memoryroot "odin-os/internal/memory"
	"odin-os/internal/store/sqlite"
)

func TestMemoryWorkspaceServiceRecordsWorkspaceEntries(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openWorkspaceMemoryStore(t)
	defer store.Close()

	workspace, err := store.GetWorkspaceByKey(ctx, "default")
	if err != nil {
		t.Fatalf("GetWorkspaceByKey(default) error = %v", err)
	}

	service := Service{Store: store}
	entry, err := service.Record(ctx, workspace.ID, memoryroot.WriteInput{
		EntryType:       memoryroot.EntryTypeNote,
		VisibilityScope: memoryroot.VisibilityWorkspace,
		RetentionClass:  memoryroot.RetentionDurable,
		Summary:         "Operator preference",
		Content:         "Marcus wants status summaries first.",
		MetadataJSON:    `{"source":"operator"}`,
	})
	if err != nil {
		t.Fatalf("Record() error = %v", err)
	}

	if entry.VisibilityScope != string(memoryroot.VisibilityWorkspace) {
		t.Fatalf("entry.VisibilityScope = %q, want %q", entry.VisibilityScope, memoryroot.VisibilityWorkspace)
	}

	entries, err := service.Recall(ctx, workspace.ID, 10)
	if err != nil {
		t.Fatalf("Recall() error = %v", err)
	}
	if len(entries) != 1 || entries[0].ID != entry.ID {
		t.Fatalf("Recall() = %+v, want entry %d", entries, entry.ID)
	}
}

func openWorkspaceMemoryStore(t *testing.T) *sqlite.Store {
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
