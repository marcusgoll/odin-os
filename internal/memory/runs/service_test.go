package runs

import (
	"context"
	"path/filepath"
	"testing"

	"odin-os/internal/store/sqlite"
)

func TestTranscriptRunServiceRecordsRunLinkedTranscriptsAndEpisodes(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	workspace, project, initiative, task, run := createRunFixture(t, ctx, store)
	service := Service{
		Store:        store,
		WorkspaceID:  workspace.ID,
		InitiativeID: initiative.ID,
		ProjectID:    project.ID,
		ProjectKey:   project.Key,
		TaskID:       task.ID,
		RunID:        run.ID,
	}

	transcript, err := service.RecordTranscript(ctx, "act", "implement memory", "completed", `{"executor":"codex_headless"}`, "codex_headless")
	if err != nil {
		t.Fatalf("RecordTranscript() error = %v", err)
	}
	episode, err := service.RememberEpisode(ctx, "The run completed successfully.", `{"source":"test"}`, &transcript.ID)
	if err != nil {
		t.Fatalf("RememberEpisode() error = %v", err)
	}

	episodes, err := service.ListEpisodes(ctx)
	if err != nil {
		t.Fatalf("ListEpisodes() error = %v", err)
	}
	if len(episodes) != 1 || episodes[0].ID != episode.ID {
		t.Fatalf("episodes = %+v, want recorded episode", episodes)
	}
	if episodes[0].WorkspaceID == nil || *episodes[0].WorkspaceID != workspace.ID {
		t.Fatalf("episodes[0].WorkspaceID = %v, want %d", episodes[0].WorkspaceID, workspace.ID)
	}
	if episodes[0].InitiativeID == nil || *episodes[0].InitiativeID != initiative.ID {
		t.Fatalf("episodes[0].InitiativeID = %v, want %d", episodes[0].InitiativeID, initiative.ID)
	}
	if episodes[0].Scope != "project" {
		t.Fatalf("episodes[0].Scope = %q, want %q", episodes[0].Scope, "project")
	}

	transcripts, err := store.ListConversationTranscripts(ctx, sqlite.ListConversationTranscriptsParams{
		WorkspaceID:  &workspace.ID,
		InitiativeID: &initiative.ID,
		ProjectID:    &project.ID,
		TaskID:       &task.ID,
		RunID:        &run.ID,
		Scope:        "project",
		ScopeKey:     project.Key,
	})
	if err != nil {
		t.Fatalf("ListConversationTranscripts() error = %v", err)
	}
	if len(transcripts) != 1 || transcripts[0].ID != transcript.ID {
		t.Fatalf("transcripts = %+v, want recorded transcript", transcripts)
	}
	if transcripts[0].WorkspaceID == nil || *transcripts[0].WorkspaceID != workspace.ID {
		t.Fatalf("transcripts[0].WorkspaceID = %v, want %d", transcripts[0].WorkspaceID, workspace.ID)
	}
	if transcripts[0].InitiativeID == nil || *transcripts[0].InitiativeID != initiative.ID {
		t.Fatalf("transcripts[0].InitiativeID = %v, want %d", transcripts[0].InitiativeID, initiative.ID)
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

func createRunFixture(t *testing.T, ctx context.Context, store *sqlite.Store) (sqlite.Workspace, sqlite.Project, sqlite.Initiative, sqlite.Task, sqlite.Run) {
	t.Helper()

	workspace, err := store.CreateWorkspace(ctx, sqlite.CreateWorkspaceParams{
		Key:        "workspace-a",
		Name:       "workspace-a",
		OwnerRef:   "workspace-a",
		Status:     "active",
		PolicyJSON: "{}",
	})
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}
	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "alpha",
		Name:          "alpha",
		Scope:         "project",
		GitRoot:       filepath.Join(t.TempDir(), "alpha"),
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	initiative, err := store.CreateInitiative(ctx, sqlite.CreateInitiativeParams{
		WorkspaceID:     workspace.ID,
		Key:             "initiative-a",
		Title:           "initiative-a",
		Kind:            "delivery",
		Status:          "active",
		Summary:         "initiative summary",
		LinkedProjectID: &project.ID,
	})
	if err != nil {
		t.Fatalf("CreateInitiative() error = %v", err)
	}
	task, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "alpha-task",
		Title:       "task",
		Status:      "running",
		Scope:       "project",
		RequestedBy: "test",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	run, err := store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:   task.ID,
		Executor: "codex_headless",
		Attempt:  1,
		Status:   "running",
	})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	return workspace, project, initiative, task, run
}
