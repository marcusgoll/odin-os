package runs

import (
	"context"
	"path/filepath"
	"testing"

	memoryroot "odin-os/internal/memory"
	"odin-os/internal/store/sqlite"
)

func TestEpisodeRunMemoryServiceRecordsRunScopedEntries(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openRunMemoryStore(t)
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
	task, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:    project.ID,
		Key:          "episode",
		Title:        "Capture run episode",
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
	run, err := store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:   task.ID,
		Executor: "codex",
		Attempt:  1,
		Status:   "running",
	})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}

	service := Service{Store: store}
	entry, err := service.Record(ctx, Scope{
		WorkspaceID:  workspace.ID,
		InitiativeID: &initiative.ID,
		CompanionID:  &companion.ID,
		TaskID:       &task.ID,
		RunID:        run.ID,
	}, memoryroot.WriteInput{
		EntryType:       memoryroot.EntryTypeEpisode,
		VisibilityScope: memoryroot.VisibilityRun,
		RetentionClass:  memoryroot.RetentionEpisodic,
		Summary:         "Run outcome",
		Content:         "The run produced a reusable follow-up plan.",
		MetadataJSON:    `{"source":"runtime"}`,
	})
	if err != nil {
		t.Fatalf("Record() error = %v", err)
	}

	if entry.RunID == nil || *entry.RunID != run.ID {
		t.Fatalf("entry.RunID = %v, want %d", entry.RunID, run.ID)
	}

	entries, err := service.Recall(ctx, workspace.ID, run.ID, 10)
	if err != nil {
		t.Fatalf("Recall() error = %v", err)
	}
	if len(entries) != 1 || entries[0].ID != entry.ID {
		t.Fatalf("Recall() = %+v, want episode entry %d", entries, entry.ID)
	}
}

func openRunMemoryStore(t *testing.T) *sqlite.Store {
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
