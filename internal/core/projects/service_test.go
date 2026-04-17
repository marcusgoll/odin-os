package projects

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

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

func TestTransitionServiceAuthorizeMutationDeniesIsolatedMutationWhenNotInLimitedAction(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTransitionServiceStore(t)
	defer store.Close()

	project := createTransitionServiceProject(t, ctx, store, "delta")
	service := Service{Store: store}

	if _, err := service.SetTransitionState(ctx, TransitionStateInput{
		ProjectID:   project.ID,
		Actor:       TransitionControllerOdinOS,
		TargetState: TransitionStateInventory,
		ChangedBy:   "operator",
		Notes:       "read only",
	}); err != nil {
		t.Fatalf("SetTransitionState(inventory) error = %v", err)
	}

	_, err := service.AuthorizeMutation(ctx, ActionInput{
		ProjectID:   project.ID,
		Actor:       TransitionControllerOdinOS,
		ActionClass: ActionClassIsolatedMutation,
		ActionKey:   "branch_proposal",
	}, Manifest{Key: "delta"})
	if !errors.Is(err, ErrTransitionDenied) {
		t.Fatalf("AuthorizeMutation() error = %v, want ErrTransitionDenied", err)
	}
}

func TestTransitionServiceAuthorizeMutationDeniesIsolatedMutationWhenActionIsNotAllowlisted(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTransitionServiceStore(t)
	defer store.Close()

	project := createTransitionServiceProject(t, ctx, store, "epsilon")
	service := Service{Store: store}

	if _, err := service.SetTransitionState(ctx, TransitionStateInput{
		ProjectID:      project.ID,
		Actor:          TransitionControllerOdinOS,
		TargetState:    TransitionStateLimitedAction,
		LimitedActions: []string{"docs_audit_note"},
		ChangedBy:      "operator",
		Notes:          "allow docs note only",
	}); err != nil {
		t.Fatalf("SetTransitionState(limited_action) error = %v", err)
	}

	_, err := service.AuthorizeMutation(ctx, ActionInput{
		ProjectID:   project.ID,
		Actor:       TransitionControllerOdinOS,
		ActionClass: ActionClassIsolatedMutation,
		ActionKey:   "repo_hygiene_note",
	}, Manifest{Key: "epsilon"})
	if !errors.Is(err, ErrTransitionDenied) {
		t.Fatalf("AuthorizeMutation() error = %v, want ErrTransitionDenied", err)
	}
}

func TestTransitionServiceAuthorizeMutationAllowsAllowlistedIsolatedMutation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTransitionServiceStore(t)
	defer store.Close()

	project := createTransitionServiceProject(t, ctx, store, "zeta")
	service := Service{Store: store}

	if _, err := service.SetTransitionState(ctx, TransitionStateInput{
		ProjectID:      project.ID,
		Actor:          TransitionControllerOdinOS,
		TargetState:    TransitionStateLimitedAction,
		LimitedActions: []string{"docs_audit_note"},
		ChangedBy:      "operator",
		Notes:          "allow docs note",
	}); err != nil {
		t.Fatalf("SetTransitionState(limited_action) error = %v", err)
	}

	if _, err := service.AuthorizeMutation(ctx, ActionInput{
		ProjectID:   project.ID,
		Actor:       TransitionControllerOdinOS,
		ActionClass: ActionClassIsolatedMutation,
		ActionKey:   "docs_audit_note",
	}, Manifest{Key: "zeta"}); err != nil {
		t.Fatalf("AuthorizeMutation() error = %v", err)
	}
}

func TestTransitionServiceAuthorizeMutationDeniesGovernanceMutationWhenApprovalIsRequired(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTransitionServiceStore(t)
	defer store.Close()

	project := createTransitionServiceProject(t, ctx, store, "theta")
	service := Service{Store: store}

	if _, err := service.SetTransitionState(ctx, TransitionStateInput{
		ProjectID:   project.ID,
		Actor:       TransitionControllerOdinOS,
		TargetState: TransitionStateCutover,
		ChangedBy:   "operator",
		Notes:       "cutover",
	}); err != nil {
		t.Fatalf("SetTransitionState(cutover) error = %v", err)
	}

	manifest := Manifest{
		Key: "theta",
		Policy: Policy{
			ApprovalGates: ApprovalGates{
				RequireForGovernanceChanges: boolPtr(true),
			},
		},
	}

	_, err := service.AuthorizeMutation(ctx, ActionInput{
		ProjectID:   project.ID,
		Actor:       TransitionControllerOdinOS,
		ActionClass: ActionClassGovernanceMutation,
		ActionKey:   "merge_policy",
	}, manifest)
	if !errors.Is(err, ErrTransitionDenied) {
		t.Fatalf("AuthorizeMutation() error = %v, want ErrTransitionDenied", err)
	}
}

func TestTransitionServiceAuthorizeMutationDeniesDestructiveMutationWhenApprovalIsRequired(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTransitionServiceStore(t)
	defer store.Close()

	project := createTransitionServiceProject(t, ctx, store, "iota")
	service := Service{Store: store}

	if _, err := service.SetTransitionState(ctx, TransitionStateInput{
		ProjectID:   project.ID,
		Actor:       TransitionControllerOdinOS,
		TargetState: TransitionStateCutover,
		ChangedBy:   "operator",
		Notes:       "cutover",
	}); err != nil {
		t.Fatalf("SetTransitionState(cutover) error = %v", err)
	}

	manifest := Manifest{
		Key: "iota",
		Policy: Policy{
			ApprovalGates: ApprovalGates{
				RequireForDestructiveOperations: boolPtr(true),
			},
		},
	}

	_, err := service.AuthorizeMutation(ctx, ActionInput{
		ProjectID:   project.ID,
		Actor:       TransitionControllerOdinOS,
		ActionClass: ActionClassDestructiveMutation,
		ActionKey:   "repo_reset",
	}, manifest)
	if !errors.Is(err, ErrTransitionDenied) {
		t.Fatalf("AuthorizeMutation() error = %v, want ErrTransitionDenied", err)
	}
}

func TestTransitionServiceAuthorizeMutationDeniesSystemProjectMutationWhenApprovalIsRequired(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTransitionServiceStore(t)
	defer store.Close()

	project := createTransitionServiceProject(t, ctx, store, "odin-core")
	service := Service{Store: store}

	if _, err := service.SetTransitionState(ctx, TransitionStateInput{
		ProjectID:   project.ID,
		Actor:       TransitionControllerOdinOS,
		TargetState: TransitionStateCutover,
		ChangedBy:   "operator",
		Notes:       "system cutover",
	}); err != nil {
		t.Fatalf("SetTransitionState(cutover) error = %v", err)
	}

	manifest := Manifest{
		Key:           "odin-core",
		SystemProject: true,
		Policy: Policy{
			ApprovalGates: ApprovalGates{
				RequireForSystemProjectChanges: boolPtr(true),
			},
		},
	}

	_, err := service.AuthorizeMutation(ctx, ActionInput{
		ProjectID:   project.ID,
		Actor:       TransitionControllerOdinOS,
		ActionClass: ActionClassFullMutation,
		ActionKey:   "repo_update",
	}, manifest)
	if !errors.Is(err, ErrTransitionDenied) {
		t.Fatalf("AuthorizeMutation() error = %v, want ErrTransitionDenied", err)
	}
}

func TestTransitionServiceSetTransitionStateRejectsUnsupportedState(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTransitionServiceStore(t)
	defer store.Close()

	project := createTransitionServiceProject(t, ctx, store, "kappa")
	service := Service{Store: store}

	_, err := service.SetTransitionState(ctx, TransitionStateInput{
		ProjectID:   project.ID,
		Actor:       TransitionControllerOdinOS,
		TargetState: TransitionState("unsupported"),
		ChangedBy:   "operator",
		Notes:       "invalid state",
	})
	if !errors.Is(err, ErrTransitionDenied) {
		t.Fatalf("SetTransitionState() error = %v, want ErrTransitionDenied", err)
	}
}

func TestTransitionServiceAuthorizeMutationSurfacesAuditWriteFailure(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTransitionServiceStore(t)
	defer store.Close()

	project := createTransitionServiceProject(t, ctx, store, "lambda")
	service := Service{
		Store: store,
		RecordDenied: func(context.Context, int64, string, string) error {
			return errors.New("audit sink unavailable")
		},
	}

	if _, err := service.SetTransitionState(ctx, TransitionStateInput{
		ProjectID:   project.ID,
		Actor:       TransitionControllerOdinOS,
		TargetState: TransitionStateInventory,
		ChangedBy:   "operator",
		Notes:       "read only",
	}); err != nil {
		t.Fatalf("SetTransitionState(inventory) error = %v", err)
	}

	_, err := service.AuthorizeMutation(ctx, ActionInput{
		ProjectID:   project.ID,
		Actor:       TransitionControllerOdinOS,
		ActionClass: ActionClassFullMutation,
		ActionKey:   "repo_update",
	}, Manifest{Key: "lambda"})
	if !errors.Is(err, ErrTransitionDenied) {
		t.Fatalf("AuthorizeMutation() error = %v, want ErrTransitionDenied", err)
	}
	if !strings.Contains(err.Error(), "audit write failed") {
		t.Fatalf("AuthorizeMutation() error = %v, want audit failure detail", err)
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

func boolPtr(value bool) *bool {
	return &value
}
