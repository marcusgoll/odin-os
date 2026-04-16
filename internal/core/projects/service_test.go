package projects

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"odin-os/internal/core/initiatives"
	runtimeevents "odin-os/internal/runtime/events"
	"odin-os/internal/store/sqlite"
)

func TestTransitionServiceRecordsShadowAndCompareReportsInMatchingStates(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTransitionServiceStore(t)
	defer store.Close()

	project := createTransitionServiceProject(t, ctx, store, "alpha")
	service := Service{Store: store}

	if _, err := service.SetTransitionState(ctx, TransitionStateInput{
		ProjectID:   project.ID,
		Actor:       TransitionControllerOdinOS,
		TargetState: TransitionStateShadow,
		ChangedBy:   "operator",
		Notes:       "observe only",
	}); err != nil {
		t.Fatalf("SetTransitionState(shadow) error = %v", err)
	}

	if _, err := service.RecordShadowObservation(ctx, ReportInput{
		ProjectID:   project.ID,
		Actor:       TransitionControllerOdinOS,
		Summary:     "legacy task observed",
		DetailsJSON: `{"task":"deploy"}`,
	}); err != nil {
		t.Fatalf("RecordShadowObservation() error = %v", err)
	}

	if _, err := service.SetTransitionState(ctx, TransitionStateInput{
		ProjectID:   project.ID,
		Actor:       TransitionControllerOdinOS,
		TargetState: TransitionStateCompare,
		ChangedBy:   "operator",
		Notes:       "compare decisions",
	}); err != nil {
		t.Fatalf("SetTransitionState(compare) error = %v", err)
	}

	if _, err := service.RecordCompareReport(ctx, ReportInput{
		ProjectID:   project.ID,
		Actor:       TransitionControllerOdinOS,
		Summary:     "decision mismatch",
		DetailsJSON: `{"verdict":"mismatch"}`,
	}); err != nil {
		t.Fatalf("RecordCompareReport() error = %v", err)
	}

	reports, err := store.ListProjectTransitionReports(ctx, project.ID)
	if err != nil {
		t.Fatalf("ListProjectTransitionReports() error = %v", err)
	}
	if len(reports) != 2 {
		t.Fatalf("report count = %d, want 2", len(reports))
	}
}

func TestTransitionServiceAuthorizeActionDeniesAndRecordsEvent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTransitionServiceStore(t)
	defer store.Close()

	project := createTransitionServiceProject(t, ctx, store, "beta")
	service := Service{Store: store}

	if _, err := service.SetTransitionState(ctx, TransitionStateInput{
		ProjectID:   project.ID,
		Actor:       TransitionControllerOdinOS,
		TargetState: TransitionStateInventory,
		ChangedBy:   "operator",
		Notes:       "inventory only",
	}); err != nil {
		t.Fatalf("SetTransitionState(inventory) error = %v", err)
	}

	_, err := service.AuthorizeAction(ctx, ActionInput{
		ProjectID:   project.ID,
		Actor:       TransitionControllerOdinOS,
		ActionClass: ActionClassFullMutation,
		ActionKey:   "merge_to_main",
	})
	if !errors.Is(err, ErrTransitionDenied) {
		t.Fatalf("AuthorizeAction() error = %v, want ErrTransitionDenied", err)
	}

	events, err := store.ListEvents(ctx, sqlite.ListEventsParams{ProjectID: &project.ID})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}

	var denied int
	for _, event := range events {
		if event.Type == runtimeevents.EventProjectTransitionDenied {
			denied++
		}
	}
	if denied != 1 {
		t.Fatalf("transition denied event count = %d, want 1", denied)
	}
}

func TestTransitionServiceLimitedActionAllowsOnlyConfiguredLowRiskAction(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTransitionServiceStore(t)
	defer store.Close()

	project := createTransitionServiceProject(t, ctx, store, "gamma")
	service := Service{Store: store}

	if _, err := service.SetTransitionState(ctx, TransitionStateInput{
		ProjectID:      project.ID,
		Actor:          TransitionControllerOdinOS,
		TargetState:    TransitionStateLimitedAction,
		LimitedActions: []string{"branch_proposal"},
		ChangedBy:      "operator",
		Notes:          "allow isolated proposal work",
	}); err != nil {
		t.Fatalf("SetTransitionState(limited_action) error = %v", err)
	}

	if _, err := service.AuthorizeAction(ctx, ActionInput{
		ProjectID:   project.ID,
		Actor:       TransitionControllerOdinOS,
		ActionClass: ActionClassIsolatedMutation,
		ActionKey:   "branch_proposal",
	}); err != nil {
		t.Fatalf("AuthorizeAction(branch_proposal) error = %v", err)
	}

	_, err := service.AuthorizeAction(ctx, ActionInput{
		ProjectID:   project.ID,
		Actor:       TransitionControllerOdinOS,
		ActionClass: ActionClassFullMutation,
		ActionKey:   "merge_to_main",
	})
	if !errors.Is(err, ErrTransitionDenied) {
		t.Fatalf("AuthorizeAction(full_mutation) error = %v, want ErrTransitionDenied", err)
	}
}

func TestProjectServiceRegistersManagedProjectInitiative(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTransitionServiceStore(t)
	defer store.Close()

	manifest := Manifest{
		Key:           "alpha",
		Name:          "Alpha",
		ProjectClass:  ProjectClassGitHubBacked,
		GitRoot:       filepath.Join(t.TempDir(), "alpha"),
		DefaultBranch: "main",
		GitHub:        GitHub{Repo: "acme/alpha"},
		SourcePath:    "config/projects.yaml",
	}

	project, err := Service{Store: store}.RegisterManagedProject(ctx, manifest)
	if err != nil {
		t.Fatalf("RegisterManagedProject() error = %v", err)
	}

	workspace, err := store.GetWorkspaceByKey(ctx, "default")
	if err != nil {
		t.Fatalf("GetWorkspaceByKey(default) error = %v", err)
	}

	initiative, err := store.GetInitiativeByKey(ctx, workspace.ID, manifest.Key)
	if err != nil {
		t.Fatalf("GetInitiativeByKey(alpha) error = %v", err)
	}
	if initiative.Kind != string(initiatives.KindManagedProject) {
		t.Fatalf("initiative.Kind = %q, want %q", initiative.Kind, initiatives.KindManagedProject)
	}
	if initiative.LinkedProjectID == nil || *initiative.LinkedProjectID != project.ID {
		t.Fatalf("initiative.LinkedProjectID = %v, want %d", initiative.LinkedProjectID, project.ID)
	}
	if initiative.Title != manifest.Name {
		t.Fatalf("initiative.Title = %q, want %q", initiative.Title, manifest.Name)
	}
}

func openTransitionServiceStore(t *testing.T) *sqlite.Store {
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

func createTransitionServiceProject(t *testing.T, ctx context.Context, store *sqlite.Store, key string) sqlite.Project {
	t.Helper()

	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           key,
		Name:          key,
		Scope:         "project",
		GitRoot:       filepath.Join(t.TempDir(), key),
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	return project
}
