package lifecycle

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"odin-os/internal/app/bootstrap"
	"odin-os/internal/core/workspaces"
	runtimeknowledge "odin-os/internal/runtime/knowledge"
	"odin-os/internal/store/sqlite"
)

func TestReviewQueueSourceCompositionUsesGovernedSources(t *testing.T) {
	sources := defaultReviewQueueSources()
	got := make([]string, 0, len(sources))
	for _, source := range sources {
		got = append(got, source.Name())
	}

	want := []string{
		"intake",
		"goal",
		"approval",
		"skill_artifact",
		"context_pack",
		"memory_proposal",
		"failed_work",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("defaultReviewQueueSources() = %#v, want %#v", got, want)
	}
}

func TestReviewQueueIncludesAllGovernedDecisionSources(t *testing.T) {
	ctx := context.Background()
	app := newLifecycleReviewTestApp(t, ctx)
	seedReviewQueueSourceFixture(t, ctx, app)

	entries, err := listReviewQueueEntries(ctx, app)
	if err != nil {
		t.Fatalf("listReviewQueueEntries() error = %v", err)
	}

	bySource := map[string]bool{}
	for _, entry := range entries {
		bySource[entry.SourceType] = true
	}

	required := []string{
		"intake_review",
		"intake_approval",
		"intake_goal_conversion",
		"goal",
		"task_approval",
		"skill_artifact",
		"context_pack",
		"failed_work",
	}
	for _, source := range required {
		if !bySource[source] {
			t.Fatalf("review queue missing source %q; got %#v", source, bySource)
		}
	}
}

func newLifecycleReviewTestApp(t *testing.T, ctx context.Context) bootstrap.App {
	t.Helper()

	store, err := sqlite.Open(filepath.Join(t.TempDir(), "odin.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("store.Close() error = %v", err)
		}
	})
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	if _, err := bootstrap.BootstrapWorkspaceRuntimeState(ctx, store); err != nil {
		t.Fatalf("BootstrapWorkspaceRuntimeState() error = %v", err)
	}
	return bootstrap.App{Store: store}
}

func seedReviewQueueSourceFixture(t *testing.T, ctx context.Context, app bootstrap.App) {
	t.Helper()

	project, err := app.Store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "review-fixture",
		Name:          "Review Fixture",
		Scope:         "project",
		GitRoot:       t.TempDir(),
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}

	if _, err := app.Store.CreateIntakeItem(ctx, sqlite.CreateIntakeItemParams{
		WorkspaceID:         workspaces.DefaultWorkspaceKey,
		SourceFamily:        "operator",
		ExternalObjectID:    "review-intake",
		EventKind:           "request",
		Subject:             "Review an intake item",
		DedupeKey:           "review-intake",
		DedupeRecipeVersion: "test",
		SourceFactsJSON:     `{}`,
		Status:              "review_required",
		Scope:               "project",
		ScopeKey:            project.Key,
		Summary:             "Review an intake item",
	}); err != nil {
		t.Fatalf("CreateIntakeItem(review) error = %v", err)
	}
	if _, err := app.Store.CreateIntakeItem(ctx, sqlite.CreateIntakeItemParams{
		WorkspaceID:         workspaces.DefaultWorkspaceKey,
		SourceFamily:        "operator",
		ExternalObjectID:    "approval-intake",
		EventKind:           "request",
		Subject:             "Approve an intake item",
		DedupeKey:           "approval-intake",
		DedupeRecipeVersion: "test",
		SourceFactsJSON:     `{}`,
		Status:              "approval_required",
		Scope:               "project",
		ScopeKey:            project.Key,
		Summary:             "Approve an intake item",
	}); err != nil {
		t.Fatalf("CreateIntakeItem(approval) error = %v", err)
	}

	convertedGoal, err := app.Store.CreateGoal(ctx, sqlite.CreateGoalParams{Title: "Converted intake goal"})
	if err != nil {
		t.Fatalf("CreateGoal(converted) error = %v", err)
	}
	convertedIntake, err := app.Store.CreateIntakeItem(ctx, sqlite.CreateIntakeItemParams{
		WorkspaceID:         workspaces.DefaultWorkspaceKey,
		SourceFamily:        "operator",
		ExternalObjectID:    "goal-intake",
		EventKind:           "request",
		Subject:             "Convert intake to goal",
		DedupeKey:           "goal-intake",
		DedupeRecipeVersion: "test",
		SourceFactsJSON:     `{}`,
		Status:              "review_required",
		Scope:               "project",
		ScopeKey:            project.Key,
		Summary:             "Convert intake to goal",
	})
	if err != nil {
		t.Fatalf("CreateIntakeItem(goal) error = %v", err)
	}
	if _, err := app.Store.ProcessIntakeItem(ctx, sqlite.ProcessIntakeItemParams{
		ID:      convertedIntake.ID,
		Status:  "review_required",
		Summary: convertedIntake.Summary,
		GoalID:  &convertedGoal.ID,
	}); err != nil {
		t.Fatalf("ProcessIntakeItem(goal) error = %v", err)
	}

	if _, err := app.Store.CreateGoal(ctx, sqlite.CreateGoalParams{Title: "Manual goal review"}); err != nil {
		t.Fatalf("CreateGoal(manual) error = %v", err)
	}

	approvalTask, err := app.Store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "approval-task",
		Title:       "Approval task",
		Status:      "blocked",
		Scope:       "project",
		RequestedBy: "test",
	})
	if err != nil {
		t.Fatalf("CreateTask(approval) error = %v", err)
	}
	if _, err := app.Store.RequestApproval(ctx, sqlite.RequestApprovalParams{
		TaskID:      approvalTask.ID,
		Status:      "pending",
		RequestedBy: "test",
	}); err != nil {
		t.Fatalf("RequestApproval() error = %v", err)
	}

	if _, err := app.Store.CreateSkillArtifact(ctx, sqlite.CreateSkillArtifactParams{
		SkillKey:         "review-fixture-skill",
		Scope:            "project",
		ProjectID:        &project.ID,
		Status:           "review_required",
		ArtifactType:     "proposal",
		Summary:          "Review fixture skill artifact",
		OutputJSON:       `{"title":"Review fixture skill artifact"}`,
		RawOutput:        `{"title":"Review fixture skill artifact"}`,
		HandlerRef:       "fixture",
		ExecutionProfile: "test",
		PermissionsJSON:  `[]`,
	}); err != nil {
		t.Fatalf("CreateSkillArtifact() error = %v", err)
	}

	contextTask, err := app.Store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "context-task",
		Title:       "Context task",
		Status:      "queued",
		Scope:       "project",
		RequestedBy: "test",
	})
	if err != nil {
		t.Fatalf("CreateTask(context) error = %v", err)
	}
	if _, err := (runtimeknowledge.Service{Store: app.Store}).ProposeContextPack(ctx, runtimeknowledge.ContextPackParams{
		TaskRef:    contextTask.Key,
		ProjectKey: project.Key,
	}); err != nil {
		t.Fatalf("ProposeContextPack() error = %v", err)
	}

	if _, err := app.Store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "failed-task",
		Title:       "Failed task",
		Status:      "failed",
		Scope:       "project",
		RequestedBy: "test",
	}); err != nil {
		t.Fatalf("CreateTask(failed) error = %v", err)
	}
}
