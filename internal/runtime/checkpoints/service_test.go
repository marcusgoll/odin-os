package checkpoints

import (
	"context"
	"path/filepath"
	"testing"

	"odin-os/internal/store/sqlite"
)

func TestCompactApprovalWaitCreatesLinkedPackets(t *testing.T) {
	ctx := context.Background()
	store := openCheckpointTestStore(t, "approval-wait.db")
	defer store.Close()

	project, task, run := seedCheckpointTask(t, ctx, store)
	service := Service{Store: store}

	result, err := service.Compact(ctx, CompactParams{
		TaskID:               task.ID,
		RunID:                &run.ID,
		Trigger:              TriggerApprovalWait,
		CheckpointKey:        "approval-1",
		Objective:            "Resume after approval",
		TaskStatus:           "waiting",
		BlockingReason:       "awaiting operator approval",
		LastCompletedStep:    "Prepared the patch",
		NextSteps:            []string{"resume once approved", "run verification"},
		Constraints:          []string{"do not mutate unrelated files"},
		SelectedCapabilities: []string{"task_list"},
		Evidence:             []Evidence{{Kind: "tool", Summary: "Captured task inventory"}},
		ManifestSummary:      "Managed git project",
		PolicySummary:        "Destructive operations require approval",
		OpenTaskSummary:      "1 active task",
		ApprovalSummary:      "1 pending approval",
		ToolResults:          []ToolResult{{Key: "task_list", Summary: "One active task"}},
	})
	if err != nil {
		t.Fatalf("Compact() error = %v", err)
	}

	if result.ProjectPacket.PacketScope != string(PacketScopeProjectContext) {
		t.Fatalf("ProjectPacket.PacketScope = %q, want %q", result.ProjectPacket.PacketScope, PacketScopeProjectContext)
	}
	if result.RunPacket == nil || result.RunPacket.PacketScope != string(PacketScopeRunContext) {
		t.Fatalf("RunPacket = %+v, want run_context packet", result.RunPacket)
	}
	if result.WakePacket.PacketScope != string(PacketScopeTaskWake) {
		t.Fatalf("WakePacket.PacketScope = %q, want %q", result.WakePacket.PacketScope, PacketScopeTaskWake)
	}
	if result.Wake.ProjectContextPacketID == nil || *result.Wake.ProjectContextPacketID != result.ProjectPacket.ID {
		t.Fatalf("Wake.ProjectContextPacketID = %v, want %d", result.Wake.ProjectContextPacketID, result.ProjectPacket.ID)
	}
	if result.Wake.RunContextPacketID == nil || *result.Wake.RunContextPacketID != result.RunPacket.ID {
		t.Fatalf("Wake.RunContextPacketID = %v, want %d", result.Wake.RunContextPacketID, result.RunPacket.ID)
	}

	latest, err := store.GetLatestTaskWakePacket(ctx, project.ID, task.ID)
	if err != nil {
		t.Fatalf("GetLatestTaskWakePacket() error = %v", err)
	}
	if latest.ID != result.WakePacket.ID {
		t.Fatalf("GetLatestTaskWakePacket().ID = %d, want %d", latest.ID, result.WakePacket.ID)
	}
}

func TestCompactRestartSupersedesPriorWakePacket(t *testing.T) {
	ctx := context.Background()
	store := openCheckpointTestStore(t, "restart.db")
	defer store.Close()

	_, task, run := seedCheckpointTask(t, ctx, store)
	service := Service{Store: store}

	first, err := service.Compact(ctx, CompactParams{
		TaskID:        task.ID,
		RunID:         &run.ID,
		Trigger:       TriggerApprovalWait,
		CheckpointKey: "approval-1",
		Objective:     "Resume after approval",
		TaskStatus:    "waiting",
		NextSteps:     []string{"resume once approved"},
	})
	if err != nil {
		t.Fatalf("Compact(first) error = %v", err)
	}

	second, err := service.Compact(ctx, CompactParams{
		TaskID:            task.ID,
		RunID:             &run.ID,
		Trigger:           TriggerRestart,
		CheckpointKey:     "restart-1",
		Objective:         "Resume after restart",
		TaskStatus:        "running",
		LastCompletedStep: "Reloaded workspace",
		NextSteps:         []string{"continue implementation"},
	})
	if err != nil {
		t.Fatalf("Compact(second) error = %v", err)
	}

	if second.WakePacket.SupersedesPacketID == nil || *second.WakePacket.SupersedesPacketID != first.WakePacket.ID {
		t.Fatalf("WakePacket.SupersedesPacketID = %v, want %d", second.WakePacket.SupersedesPacketID, first.WakePacket.ID)
	}
}

func TestLoadResumeStateRehydratesLatestWakePacket(t *testing.T) {
	ctx := context.Background()
	store := openCheckpointTestStore(t, "resume.db")
	defer store.Close()

	project, task, run := seedCheckpointTask(t, ctx, store)
	service := Service{Store: store}

	if _, err := service.Compact(ctx, CompactParams{
		TaskID:               task.ID,
		RunID:                &run.ID,
		Trigger:              TriggerApprovalWait,
		CheckpointKey:        "approval-1",
		Objective:            "Resume after approval",
		TaskStatus:           "waiting",
		BlockingReason:       "awaiting operator approval",
		NextSteps:            []string{"resume once approved", "run verification"},
		Constraints:          []string{"no destructive operations"},
		SelectedCapabilities: []string{"task_list"},
		ManifestSummary:      "Managed git project",
		PolicySummary:        "Approval gate enabled",
		OpenTaskSummary:      "1 active task",
		ApprovalSummary:      "1 pending approval",
		ToolResults:          []ToolResult{{Key: "task_list", Summary: "One active task"}},
	}); err != nil {
		t.Fatalf("Compact() error = %v", err)
	}

	state, err := service.LoadResumeState(ctx, project.ID, task.ID)
	if err != nil {
		t.Fatalf("LoadResumeState() error = %v", err)
	}

	if state.Objective != "Resume after approval" {
		t.Fatalf("LoadResumeState().Objective = %q, want %q", state.Objective, "Resume after approval")
	}
	if len(state.NextSteps) != 2 {
		t.Fatalf("LoadResumeState().NextSteps len = %d, want 2", len(state.NextSteps))
	}
	if state.ProjectContext == nil || state.ProjectContext.ProjectKey != project.Key {
		t.Fatalf("LoadResumeState().ProjectContext = %+v, want project %q", state.ProjectContext, project.Key)
	}
	if state.RunContext == nil || state.RunContext.Executor != run.Executor {
		t.Fatalf("LoadResumeState().RunContext = %+v, want executor %q", state.RunContext, run.Executor)
	}
	if state.BlockingReason != "awaiting operator approval" {
		t.Fatalf("LoadResumeState().BlockingReason = %q, want %q", state.BlockingReason, "awaiting operator approval")
	}
}

func TestCompactIncludesProjectAndRunFactsWhenProvided(t *testing.T) {
	ctx := context.Background()
	store := openCheckpointTestStore(t, "facts.db")
	defer store.Close()

	_, task, run := seedCheckpointTask(t, ctx, store)
	service := Service{Store: store}

	result, err := service.Compact(ctx, CompactParams{
		TaskID:        task.ID,
		RunID:         &run.ID,
		Trigger:       TriggerHandoff,
		CheckpointKey: "handoff-1",
		Objective:     "Hand off workspace progress",
		TaskStatus:    "queued",
		ProjectFacts: map[string]string{
			"branch":      "main",
			"head":        "abc123",
			"current_cwd": "/tmp/repo/docs",
		},
		RunFacts: map[string]string{
			"session_name": "odin-workspace-alpha",
		},
	})
	if err != nil {
		t.Fatalf("Compact() error = %v", err)
	}

	if result.Project.Facts["branch"] != "main" {
		t.Fatalf("Project.Facts = %#v, want branch=main", result.Project.Facts)
	}
	if result.Run == nil || result.Run.Facts["session_name"] != "odin-workspace-alpha" {
		t.Fatalf("Run.Facts = %#v, want session_name", result.Run)
	}
}

func openCheckpointTestStore(t *testing.T, name string) *sqlite.Store {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), name)
	store, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	if err := store.Migrate(context.Background()); err != nil {
		_ = store.Close()
		t.Fatalf("Migrate() error = %v", err)
	}

	return store
}

func seedCheckpointTask(t *testing.T, ctx context.Context, store *sqlite.Store) (sqlite.Project, sqlite.Task, sqlite.Run) {
	t.Helper()

	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "odin-core",
		Name:          "Odin Core",
		Scope:         "odin-core",
		GitRoot:       "/home/orchestrator/odin-os",
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}

	task, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "phase-08",
		Title:       "Implement wake packets",
		Status:      "running",
		Scope:       "odin-core",
		RequestedBy: "operator",
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

	return project, task, run
}
