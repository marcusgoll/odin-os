package projects

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	memoryroot "odin-os/internal/memory"
	memorycompanions "odin-os/internal/memory/companions"
	memoryworkspaces "odin-os/internal/memory/workspaces"
	"odin-os/internal/store/sqlite"
)

func TestMemoryProjectServiceRecallsInitiativeThenWorkspaceEntries(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openProjectMemoryStore(t)
	defer store.Close()

	workspace, err := store.GetWorkspaceByKey(ctx, "default")
	if err != nil {
		t.Fatalf("GetWorkspaceByKey(default) error = %v", err)
	}
	companion, err := store.GetCompanionByKey(ctx, workspace.ID, workspace.DefaultCompanionKey)
	if err != nil {
		t.Fatalf("GetCompanionByKey(default) error = %v", err)
	}
	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "alpha",
		Name:          "Alpha",
		Scope:         "project",
		GitRoot:       filepath.Join(t.TempDir(), "alpha"),
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	initiative, err := store.UpsertInitiative(ctx, sqlite.UpsertInitiativeParams{
		WorkspaceID:      workspace.ID,
		Key:              project.Key,
		Title:            project.Name,
		Kind:             "managed_project",
		Status:           "active",
		Summary:          "",
		OwnerCompanionID: &companion.ID,
		LinkedProjectID:  &project.ID,
	})
	if err != nil {
		t.Fatalf("UpsertInitiative(alpha) error = %v", err)
	}

	if _, err := (memoryworkspaces.Service{Store: store}).Record(ctx, workspace.ID, memoryroot.WriteInput{
		EntryType:       memoryroot.EntryTypeNote,
		VisibilityScope: memoryroot.VisibilityWorkspace,
		RetentionClass:  memoryroot.RetentionDurable,
		Summary:         "Global preference",
		Content:         "Workspace context should trail project-local context.",
		MetadataJSON:    `{"source":"workspace"}`,
	}); err != nil {
		t.Fatalf("workspace Record() error = %v", err)
	}

	service := Service{Store: store}
	projectEntry, err := service.Record(ctx, workspace.ID, initiative.ID, memoryroot.WriteInput{
		EntryType:       memoryroot.EntryTypeNote,
		VisibilityScope: memoryroot.VisibilityInitiative,
		RetentionClass:  memoryroot.RetentionDurable,
		Summary:         "Alpha convention",
		Content:         "Alpha requires reviewed task branches before merge.",
		MetadataJSON:    `{"source":"project"}`,
	})
	if err != nil {
		t.Fatalf("Record() error = %v", err)
	}

	entries, err := service.Recall(ctx, workspace.ID, initiative.ID, 10)
	if err != nil {
		t.Fatalf("Recall() error = %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("Recall() len = %d, want 2", len(entries))
	}
	if entries[0].ID != projectEntry.ID {
		t.Fatalf("Recall()[0].ID = %d, want project entry %d first", entries[0].ID, projectEntry.ID)
	}
}

func TestMemoryProjectServiceRemembersInitiativeScopedFollowUpCompletion(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openProjectMemoryStore(t)
	defer store.Close()

	workspace, err := store.GetWorkspaceByKey(ctx, "default")
	if err != nil {
		t.Fatalf("GetWorkspaceByKey(default) error = %v", err)
	}
	companion, err := store.GetCompanionByKey(ctx, workspace.ID, workspace.DefaultCompanionKey)
	if err != nil {
		t.Fatalf("GetCompanionByKey(default) error = %v", err)
	}
	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "ops",
		Name:          "Ops",
		Scope:         "project",
		GitRoot:       filepath.Join(t.TempDir(), "ops"),
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	initiative, err := store.UpsertInitiative(ctx, sqlite.UpsertInitiativeParams{
		WorkspaceID:      workspace.ID,
		Key:              "life-admin",
		Title:            "Life Admin",
		Kind:             "routine",
		Status:           "active",
		Summary:          "Recurring life admin",
		OwnerCompanionID: &companion.ID,
		LinkedProjectID:  &project.ID,
	})
	if err != nil {
		t.Fatalf("UpsertInitiative(life-admin) error = %v", err)
	}

	service := Service{Store: store}
	summary, err := service.RememberFollowUpCompletion(ctx, workspace.ID, initiative.ID, "Pay rent", time.Date(2026, 4, 17, 9, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("RememberFollowUpCompletion() error = %v", err)
	}
	if summary.Scope != "initiative" {
		t.Fatalf("summary.Scope = %q, want initiative", summary.Scope)
	}
	if summary.ScopeKey != initiative.Key {
		t.Fatalf("summary.ScopeKey = %q, want %q", summary.ScopeKey, initiative.Key)
	}
	if summary.MemoryType != memoryroot.MemoryTypeFollowUpCompletion {
		t.Fatalf("summary.MemoryType = %q, want %q", summary.MemoryType, memoryroot.MemoryTypeFollowUpCompletion)
	}
}

func TestMemoryProjectServiceRecallDoesNotIncludeCompanionEntries(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openProjectMemoryStore(t)
	defer store.Close()

	workspace, err := store.GetWorkspaceByKey(ctx, "default")
	if err != nil {
		t.Fatalf("GetWorkspaceByKey(default) error = %v", err)
	}
	companion, err := store.GetCompanionByKey(ctx, workspace.ID, workspace.DefaultCompanionKey)
	if err != nil {
		t.Fatalf("GetCompanionByKey(default) error = %v", err)
	}
	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "admin",
		Name:          "Admin",
		Scope:         "project",
		GitRoot:       filepath.Join(t.TempDir(), "admin"),
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	initiative, err := store.UpsertInitiative(ctx, sqlite.UpsertInitiativeParams{
		WorkspaceID:      workspace.ID,
		Key:              "life-admin",
		Title:            "Life Admin",
		Kind:             "routine",
		Status:           "active",
		Summary:          "Recurring life admin",
		OwnerCompanionID: &companion.ID,
		LinkedProjectID:  &project.ID,
	})
	if err != nil {
		t.Fatalf("UpsertInitiative(life-admin) error = %v", err)
	}

	companionMemory := memorycompanions.Service{Store: store}
	if _, err := companionMemory.Record(ctx, workspace.ID, companion.ID, memoryroot.WriteInput{
		EntryType:       memoryroot.EntryTypeNote,
		VisibilityScope: memoryroot.VisibilityCompanion,
		RetentionClass:  memoryroot.RetentionDurable,
		Summary:         "Companion only",
		Content:         "This should not appear in initiative recall.",
		MetadataJSON:    `{"source":"companion"}`,
	}); err != nil {
		t.Fatalf("companionMemory.Record() error = %v", err)
	}

	projectMemory := Service{Store: store}
	if _, err := projectMemory.Record(ctx, workspace.ID, initiative.ID, memoryroot.WriteInput{
		EntryType:       memoryroot.EntryTypeNote,
		VisibilityScope: memoryroot.VisibilityInitiative,
		RetentionClass:  memoryroot.RetentionDurable,
		Summary:         "Initiative note",
		Content:         "File tax documents next.",
		MetadataJSON:    `{"source":"initiative"}`,
	}); err != nil {
		t.Fatalf("projectMemory.Record() error = %v", err)
	}

	entries, err := projectMemory.Recall(ctx, workspace.ID, initiative.ID, 10)
	if err != nil {
		t.Fatalf("Recall() error = %v", err)
	}
	for _, entry := range entries {
		if entry.VisibilityScope == string(memoryroot.VisibilityCompanion) {
			t.Fatalf("Recall() leaked companion memory entry: %+v", entry)
		}
	}
}

func openProjectMemoryStore(t *testing.T) *sqlite.Store {
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
