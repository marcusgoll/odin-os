package overview

import (
	"context"
	"path/filepath"
	"testing"

	"odin-os/internal/cli/scope"
	"odin-os/internal/core/initiatives"
	knowledgememory "odin-os/internal/memory/knowledge"
	"odin-os/internal/registry"
	"odin-os/internal/store/sqlite"
)

func TestBuildReturnsCanonicalOverviewFromCurrentAuthority(t *testing.T) {
	ctx := context.Background()
	env := newOverviewTestEnvironment(t)

	view, err := Service{
		Store:            env.store,
		RegistrySnapshot: env.snapshot,
	}.Build(ctx, scope.Resolution{Kind: scope.ScopeGlobal})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	if view.Workspace.Wiring != WiringLive {
		t.Fatalf("Workspace wiring = %q, want %q", view.Workspace.Wiring, WiringLive)
	}
	if view.Workspace.WorkspaceKey != "default" {
		t.Fatalf("Workspace key = %q, want default", view.Workspace.WorkspaceKey)
	}
	if len(view.Initiatives) != 1 {
		t.Fatalf("Initiatives len = %d, want 1", len(view.Initiatives))
	}
	if view.Initiatives[0].InitiativeKey != "alpha" {
		t.Fatalf("Initiative key = %q, want alpha", view.Initiatives[0].InitiativeKey)
	}
	if view.Companions.Wiring != WiringLive {
		t.Fatalf("Companions wiring = %q, want %q", view.Companions.Wiring, WiringLive)
	}
	if len(view.Companions.Items) != 1 {
		t.Fatalf("Companion items len = %d, want 1", len(view.Companions.Items))
	}
	if view.CapabilityCatalog.AgentDefinitionCount != 1 || view.CapabilityCatalog.SkillCount != 1 || view.CapabilityCatalog.WorkflowCount != 1 || view.CapabilityCatalog.CommandCount != 1 {
		t.Fatalf("Capability catalog = %+v, want one item per registry kind", view.CapabilityCatalog)
	}
	if view.CapabilityCatalog.ToolCount == 0 {
		t.Fatalf("Tool count = 0, want builtin tools")
	}
	if len(view.WorkItems) != 1 {
		t.Fatalf("Work items len = %d, want 1", len(view.WorkItems))
	}
	if len(view.Approvals) != 1 {
		t.Fatalf("Approvals len = %d, want 1", len(view.Approvals))
	}
	if len(view.Observability.ActiveRuns) != 1 {
		t.Fatalf("Active runs len = %d, want 1", len(view.Observability.ActiveRuns))
	}
	if len(view.Memory.Recent) != 1 || view.Memory.Count != 1 {
		t.Fatalf("Memory = %+v, want one recent entry", view.Memory)
	}
	if view.IntakeInbox.Wiring != WiringNotYetWired {
		t.Fatalf("Intake wiring = %q, want %q", view.IntakeInbox.Wiring, WiringNotYetWired)
	}
	if view.AutomationTriggers.Wiring != WiringNotYetWired {
		t.Fatalf("Automation wiring = %q, want %q", view.AutomationTriggers.Wiring, WiringNotYetWired)
	}
}

type overviewTestEnvironment struct {
	store    *sqlite.Store
	snapshot registry.Snapshot
}

func newOverviewTestEnvironment(t *testing.T) overviewTestEnvironment {
	t.Helper()

	ctx := context.Background()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "odin.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	t.Cleanup(func() {
		store.Close()
	})

	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "alpha",
		Name:          "Alpha",
		Scope:         "project",
		GitRoot:       "/tmp/alpha",
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}

	workspace, err := store.GetWorkspaceByKey(ctx, "default")
	if err != nil {
		t.Fatalf("GetWorkspaceByKey(default) error = %v", err)
	}
	companion, err := store.GetCompanionByKey(ctx, workspace.ID, workspace.DefaultCompanionKey)
	if err != nil {
		t.Fatalf("GetCompanionByKey(default) error = %v", err)
	}
	initiative, err := store.UpsertInitiative(ctx, sqlite.UpsertInitiativeParams{
		WorkspaceID:      workspace.ID,
		Key:              project.Key,
		Title:            project.Name,
		Kind:             string(initiatives.KindManagedProject),
		Status:           "active",
		Summary:          "Alpha initiative",
		OwnerCompanionID: &companion.ID,
		LinkedProjectID:  &project.ID,
	})
	if err != nil {
		t.Fatalf("UpsertInitiative() error = %v", err)
	}

	task, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:    project.ID,
		Key:          "alpha-task",
		Title:        "Alpha task",
		Status:       "blocked",
		Scope:        "project",
		RequestedBy:  "operator",
		WorkspaceID:  &workspace.ID,
		InitiativeID: &initiative.ID,
		CompanionID:  &companion.ID,
		WorkKind:     "automation",
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

	if _, err := store.RequestApproval(ctx, sqlite.RequestApprovalParams{
		TaskID:      task.ID,
		RunID:       &run.ID,
		Status:      "pending",
		RequestedBy: "system",
	}); err != nil {
		t.Fatalf("RequestApproval() error = %v", err)
	}

	if _, err := (knowledgememory.Service{Store: store}).Record(ctx, knowledgememory.Scope{
		Value: "global",
		Key:   "global",
	}, "operator_note", "Remember this overview state", `{"source":"test"}`, nil); err != nil {
		t.Fatalf("Record() error = %v", err)
	}

	items := []registry.Item{
		{Kind: registry.KindAgent, Key: "finance-advisor", Title: "Finance Advisor"},
		{Kind: registry.KindSkill, Key: "triage-skill", Title: "Triage Skill"},
		{Kind: registry.KindWorkflow, Key: "daily-workflow", Title: "Daily Workflow"},
		{Kind: registry.KindCommand, Key: "approve-command", Title: "Approve Command"},
	}
	snapshot := registry.Snapshot{
		Items:  append([]registry.Item(nil), items...),
		ByKey:  make(map[string]registry.Item, len(items)),
		ByKind: make(map[registry.Kind][]registry.Item),
	}
	for _, item := range items {
		snapshot.ByKey[item.Key] = item
		snapshot.ByKind[item.Kind] = append(snapshot.ByKind[item.Kind], item)
	}

	return overviewTestEnvironment{
		store:    store,
		snapshot: snapshot,
	}
}
