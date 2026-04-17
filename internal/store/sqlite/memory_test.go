package sqlite

import (
	"context"
	"path/filepath"
	"testing"
)

func TestMemoryEntryStorePersistsScopedRecords(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openMigratedTestStore(t, "memory.db")
	defer store.Close()

	workspace, err := store.GetWorkspaceByKey(ctx, "default")
	if err != nil {
		t.Fatalf("GetWorkspaceByKey(default) error = %v", err)
	}
	companion, err := store.GetCompanionByKey(ctx, workspace.ID, workspace.DefaultCompanionKey)
	if err != nil {
		t.Fatalf("GetCompanionByKey(default) error = %v", err)
	}

	project, err := store.CreateProject(ctx, CreateProjectParams{
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
	initiative, err := store.UpsertInitiative(ctx, UpsertInitiativeParams{
		WorkspaceID:      workspace.ID,
		Key:              "alpha",
		Title:            "Alpha",
		Kind:             "managed_project",
		Status:           "active",
		Summary:          "",
		OwnerCompanionID: &companion.ID,
		LinkedProjectID:  &project.ID,
	})
	if err != nil {
		t.Fatalf("UpsertInitiative(alpha) error = %v", err)
	}

	task, err := store.CreateTask(ctx, CreateTaskParams{
		ProjectID:    project.ID,
		Key:          "memory-seed",
		Title:        "Seed scoped memory",
		Status:       "running",
		Scope:        "project",
		RequestedBy:  "operator",
		WorkspaceID:  &workspace.ID,
		InitiativeID: &initiative.ID,
		CompanionID:  &companion.ID,
		WorkKind:     "project",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	run, err := store.StartRun(ctx, StartRunParams{
		TaskID:   task.ID,
		Executor: "codex",
		Attempt:  1,
		Status:   "running",
	})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}

	workspaceEntry, err := store.CreateMemoryEntry(ctx, CreateMemoryEntryParams{
		WorkspaceID:     workspace.ID,
		EntryType:       "note",
		VisibilityScope: "workspace",
		RetentionClass:  "durable",
		Summary:         "Marcus preference",
		Content:         "Marcus prefers calendar reviews before noon.",
		MetadataJSON:    `{"source":"operator"}`,
	})
	if err != nil {
		t.Fatalf("CreateMemoryEntry(workspace) error = %v", err)
	}

	initiativeEntry, err := store.CreateMemoryEntry(ctx, CreateMemoryEntryParams{
		WorkspaceID:     workspace.ID,
		InitiativeID:    &initiative.ID,
		CompanionID:     &companion.ID,
		EntryType:       "note",
		VisibilityScope: "initiative",
		RetentionClass:  "durable",
		Summary:         "Alpha convention",
		Content:         "Alpha deploys from reviewed task branches only.",
		MetadataJSON:    `{"source":"project"}`,
	})
	if err != nil {
		t.Fatalf("CreateMemoryEntry(initiative) error = %v", err)
	}

	runEntry, err := store.CreateMemoryEntry(ctx, CreateMemoryEntryParams{
		WorkspaceID:     workspace.ID,
		InitiativeID:    &initiative.ID,
		CompanionID:     &companion.ID,
		TaskID:          &task.ID,
		RunID:           &run.ID,
		EntryType:       "episode",
		VisibilityScope: "run",
		RetentionClass:  "episodic",
		Summary:         "Recent run outcome",
		Content:         "Run completed the planning pass and queued follow-up edits.",
		MetadataJSON:    `{"source":"runtime"}`,
	})
	if err != nil {
		t.Fatalf("CreateMemoryEntry(run) error = %v", err)
	}

	if workspaceEntry.WorkspaceID != workspace.ID {
		t.Fatalf("workspaceEntry.WorkspaceID = %d, want %d", workspaceEntry.WorkspaceID, workspace.ID)
	}
	if initiativeEntry.InitiativeID == nil || *initiativeEntry.InitiativeID != initiative.ID {
		t.Fatalf("initiativeEntry.InitiativeID = %v, want %d", initiativeEntry.InitiativeID, initiative.ID)
	}
	if runEntry.RunID == nil || *runEntry.RunID != run.ID {
		t.Fatalf("runEntry.RunID = %v, want %d", runEntry.RunID, run.ID)
	}

	workspaceEntries, err := store.ListMemoryEntries(ctx, ListMemoryEntriesParams{
		WorkspaceID:     workspace.ID,
		VisibilityScope: "workspace",
	})
	if err != nil {
		t.Fatalf("ListMemoryEntries(workspace) error = %v", err)
	}
	if len(workspaceEntries) != 1 || workspaceEntries[0].ID != workspaceEntry.ID {
		t.Fatalf("workspace entries = %+v, want only workspace entry %d", workspaceEntries, workspaceEntry.ID)
	}

	initiativeEntries, err := store.ListMemoryEntries(ctx, ListMemoryEntriesParams{
		WorkspaceID:     workspace.ID,
		InitiativeID:    &initiative.ID,
		VisibilityScope: "initiative",
	})
	if err != nil {
		t.Fatalf("ListMemoryEntries(initiative) error = %v", err)
	}
	if len(initiativeEntries) != 1 || initiativeEntries[0].ID != initiativeEntry.ID {
		t.Fatalf("initiative entries = %+v, want only initiative entry %d", initiativeEntries, initiativeEntry.ID)
	}

	runEntries, err := store.ListMemoryEntries(ctx, ListMemoryEntriesParams{
		WorkspaceID:     workspace.ID,
		RunID:           &run.ID,
		VisibilityScope: "run",
	})
	if err != nil {
		t.Fatalf("ListMemoryEntries(run) error = %v", err)
	}
	if len(runEntries) != 1 || runEntries[0].ID != runEntry.ID {
		t.Fatalf("run entries = %+v, want only run entry %d", runEntries, runEntry.ID)
	}
}

func TestMemoryEntryStoreRejectsInvalidMetadataJSON(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openMigratedTestStore(t, "memory-invalid-json.db")
	defer store.Close()

	workspace, err := store.GetWorkspaceByKey(ctx, "default")
	if err != nil {
		t.Fatalf("GetWorkspaceByKey(default) error = %v", err)
	}

	_, err = store.CreateMemoryEntry(ctx, CreateMemoryEntryParams{
		WorkspaceID:     workspace.ID,
		EntryType:       "note",
		VisibilityScope: "workspace",
		RetentionClass:  "durable",
		Summary:         "bad metadata",
		Content:         "this should fail",
		MetadataJSON:    `{"source":`,
	})
	if err == nil {
		t.Fatal("CreateMemoryEntry() error = nil, want invalid metadata JSON")
	}
}
