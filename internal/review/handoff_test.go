package review

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"odin-os/internal/store/sqlite"
)

func TestHandoffOrchestratorPersistsReadOnlyReviewSelection(t *testing.T) {
	ctx := context.Background()
	store := openReviewTestStore(t, "review-handoff-nonsensitive.db")
	defer store.Close()
	project := createReviewTestProject(t, ctx, store)

	manager := &recordingPullRequestManager{
		pullRequest: PullRequest{
			Provider: "github",
			Repo:     "acme/odin-os",
			Number:   76,
			URL:      "https://github.example/acme/odin-os/pull/76",
			State:    "open",
		},
	}
	orchestrator := HandoffOrchestrator{
		Store:        store,
		PullRequests: manager,
	}

	result, err := orchestrator.Upsert(ctx, PullRequestHandoffRequest{
		ProjectID:    project.ID,
		IssueURL:     "https://github.example/acme/odin-os/issues/76",
		Title:        "Wire review selection",
		Branch:       "issue/76-review-selection",
		Summary:      "Review handoff is ready.",
		Tests:        []string{"go test ./internal/review -count=1"},
		Risks:        []string{"live GitHub writes remain fixture-scoped"},
		Blockers:     []string{"human merge remains required"},
		CommandsRun:  []string{"go test ./internal/review -count=1"},
		ChangedFiles: []string{"docs/brownfield/PR_REVIEW_CONSOLIDATION.md"},
		PostComment:  true,
	})
	if err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}

	if !reflect.DeepEqual(manager.calls, []string{"upsert", "comment"}) {
		t.Fatalf("manager calls = %#v, want PR upsert before review handoff comment", manager.calls)
	}
	if len(manager.upserts) != 1 || len(manager.upserts[0].Labels) != 0 {
		t.Fatalf("upsert request labels = %#v, want no human-ready label posted by review selection", manager.upserts)
	}
	if result.PullRequest.Number != 76 || result.Handoff.Number != 76 {
		t.Fatalf("result PR/handoff = %+v / %+v, want persisted PR 76", result.PullRequest, result.Handoff)
	}
	if !result.Selection.ReadOnly || result.Selection.SecurityRequired {
		t.Fatalf("selection = %+v, want read-only non-sensitive review selection", result.Selection)
	}
	if !reflect.DeepEqual(result.Selection.Roles, []string{RoleReviewer, RoleQA}) {
		t.Fatalf("selection roles = %#v, want reviewer + qa", result.Selection.Roles)
	}
	if result.Handoff.ReviewState != ReviewStateSelected {
		t.Fatalf("ReviewState = %q, want %q", result.Handoff.ReviewState, ReviewStateSelected)
	}
	if !reflect.DeepEqual(result.Handoff.SelectedRoles, []string{RoleReviewer, RoleQA}) {
		t.Fatalf("SelectedRoles = %#v, want persisted reviewer + qa", result.Handoff.SelectedRoles)
	}

	results, err := store.ListPullRequestReviewResults(ctx, result.Handoff.ID)
	if err != nil {
		t.Fatalf("ListPullRequestReviewResults() error = %v", err)
	}
	assertReviewResultRoles(t, results, []string{RoleReviewer, RoleQA})
	for _, reviewResult := range results {
		if reviewResult.State != ReviewRunStateSelected || reviewResult.Outcome != ReviewOutcomeReadOnlyPending {
			t.Fatalf("review result = %+v, want selected read-only pending", reviewResult)
		}
	}

	handoffs, err := store.ListPullRequestHandoffs(ctx, sqlite.ListPullRequestHandoffsParams{ProjectID: &project.ID, ReviewState: ReviewStateSelected})
	if err != nil {
		t.Fatalf("ListPullRequestHandoffs() error = %v", err)
	}
	if len(handoffs) != 1 || handoffs[0].ID != result.Handoff.ID {
		t.Fatalf("handoffs = %+v, want restart-safe persisted handoff", handoffs)
	}
	if len(manager.comments) != 1 || !strings.Contains(manager.comments[0].Body, "Review handoff is ready.") {
		t.Fatalf("comments = %+v, want handoff comment after review selection", manager.comments)
	}
}

func TestHandoffOrchestratorSelectsSecurityForSensitiveChanges(t *testing.T) {
	ctx := context.Background()
	store := openReviewTestStore(t, "review-handoff-sensitive.db")
	defer store.Close()
	project := createReviewTestProject(t, ctx, store)

	manager := &recordingPullRequestManager{
		pullRequest: PullRequest{
			Provider: "github",
			Repo:     "acme/odin-os",
			Number:   77,
			URL:      "https://github.example/acme/odin-os/pull/77",
			State:    "open",
		},
	}
	orchestrator := HandoffOrchestrator{
		Store:        store,
		PullRequests: manager,
	}

	result, err := orchestrator.Upsert(ctx, PullRequestHandoffRequest{
		ProjectID:    project.ID,
		IssueURL:     "https://github.example/acme/odin-os/issues/77",
		Title:        "Harden worktree cleanup",
		Branch:       "issue/77-sensitive-review",
		Summary:      "Sensitive handoff is ready.",
		Tests:        []string{"go test ./internal/review -count=1"},
		Risks:        []string{"security review remains read-only"},
		Blockers:     []string{"human merge remains required"},
		CommandsRun:  []string{"go test ./internal/review -count=1"},
		ChangedFiles: []string{"internal/vcs/worktrees/cleanup.go", ".github/workflows/ci.yml"},
	})
	if err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}

	if !result.Selection.ReadOnly || !result.Selection.SecurityRequired {
		t.Fatalf("selection = %+v, want read-only security-required selection", result.Selection)
	}
	if !reflect.DeepEqual(result.Selection.Roles, []string{RoleReviewer, RoleQA, RoleSecurity}) {
		t.Fatalf("selection roles = %#v, want reviewer + qa + security", result.Selection.Roles)
	}
	if len(result.Selection.SecurityReasons) == 0 {
		t.Fatalf("security reasons empty, want sensitive path reason")
	}

	results, err := store.ListPullRequestReviewResults(ctx, result.Handoff.ID)
	if err != nil {
		t.Fatalf("ListPullRequestReviewResults() error = %v", err)
	}
	assertReviewResultRoles(t, results, []string{RoleReviewer, RoleQA, RoleSecurity})
	var securityResult sqlite.PullRequestReviewResult
	for _, reviewResult := range results {
		if reviewResult.Role == RoleSecurity {
			securityResult = reviewResult
		}
	}
	if securityResult.ID == 0 || len(securityResult.Comments) == 0 || !strings.Contains(strings.Join(securityResult.Comments, "\n"), "security review") {
		t.Fatalf("security result = %+v, want persisted sensitive-path reason", securityResult)
	}
}

type recordingPullRequestManager struct {
	pullRequest PullRequest
	calls       []string
	upserts     []PullRequestRequest
	comments    []PullRequestComment
}

func (manager *recordingPullRequestManager) Upsert(_ context.Context, request PullRequestRequest) (PullRequest, error) {
	manager.calls = append(manager.calls, "upsert")
	manager.upserts = append(manager.upserts, request)
	return manager.pullRequest, nil
}

func (manager *recordingPullRequestManager) AddComment(_ context.Context, request PullRequestComment) error {
	manager.calls = append(manager.calls, "comment")
	manager.comments = append(manager.comments, request)
	return nil
}

func openReviewTestStore(t *testing.T, name string) *sqlite.Store {
	t.Helper()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), name))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	return store
}

func createReviewTestProject(t *testing.T, ctx context.Context, store *sqlite.Store) sqlite.Project {
	t.Helper()
	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "odin-os",
		Name:          "Odin OS",
		Scope:         "project",
		GitRoot:       "/tmp/odin-os",
		DefaultBranch: "main",
		GitHubRepo:    "acme/odin-os",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	return project
}

func assertReviewResultRoles(t *testing.T, results []sqlite.PullRequestReviewResult, want []string) {
	t.Helper()
	got := make([]string, 0, len(results))
	for _, result := range results {
		got = append(got, result.Role)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("review result roles = %#v, want %#v", got, want)
	}
}
