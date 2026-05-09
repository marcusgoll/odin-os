package sqlite

import (
	"context"
	"reflect"
	"testing"
)

func TestPullRequestHandoffUpsertIsIdempotentAndPersistsReviewResults(t *testing.T) {
	ctx := context.Background()
	store := openMigratedTestStore(t, "pull-request-handoffs.db")
	defer store.Close()

	project, err := store.CreateProject(ctx, CreateProjectParams{
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

	first, err := store.UpsertPullRequestHandoff(ctx, UpsertPullRequestHandoffParams{
		ProjectID:     project.ID,
		Provider:      "github",
		Repo:          "acme/odin-os",
		Number:        74,
		URL:           "https://github.example/acme/odin-os/pull/74",
		State:         "open",
		IssueURL:      "https://github.example/acme/odin-os/issues/74",
		Branch:        "issue/74-pr-adapter",
		Title:         "Add PR adapter",
		Summary:       "Fixture-backed adapter.",
		Tests:         []string{"go test ./internal/review -count=1"},
		Risks:         []string{"live writes not invoked"},
		Blockers:      []string{"human review required"},
		SelectedRoles: []string{"reviewer", "qa"},
		ReviewState:   "pending",
	})
	if err != nil {
		t.Fatalf("UpsertPullRequestHandoff(first) error = %v", err)
	}

	second, err := store.UpsertPullRequestHandoff(ctx, UpsertPullRequestHandoffParams{
		ProjectID:     project.ID,
		Provider:      "github",
		Repo:          "acme/odin-os",
		Number:        74,
		URL:           "https://github.example/acme/odin-os/pull/74",
		State:         "open",
		IssueURL:      "https://github.example/acme/odin-os/issues/74",
		Branch:        "issue/74-pr-adapter",
		Title:         "Add fixture-backed PR adapter",
		Summary:       "Adapter and docs are ready.",
		Tests:         []string{"go test ./internal/review ./internal/tracker/github -count=1"},
		Risks:         []string{"live writes deferred"},
		Blockers:      []string{},
		SelectedRoles: []string{"reviewer", "qa", "security"},
		ReviewState:   "reviewing",
	})
	if err != nil {
		t.Fatalf("UpsertPullRequestHandoff(second) error = %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("second.ID = %d, want idempotent ID %d", second.ID, first.ID)
	}
	if second.Title != "Add fixture-backed PR adapter" || second.Summary != "Adapter and docs are ready." || second.ReviewState != "reviewing" {
		t.Fatalf("updated handoff = %+v, want latest title/summary/review state", second)
	}
	if !reflect.DeepEqual(second.SelectedRoles, []string{"reviewer", "qa", "security"}) {
		t.Fatalf("SelectedRoles = %#v, want updated roles", second.SelectedRoles)
	}

	reviewer, err := store.UpsertPullRequestReviewResult(ctx, UpsertPullRequestReviewResultParams{
		HandoffID: second.ID,
		Role:      "reviewer",
		State:     "completed",
		Summary:   "Looks ready.",
		Comments:  []string{"tests are adequate"},
		Blockers:  []string{},
		Outcome:   "approved_for_human_review",
	})
	if err != nil {
		t.Fatalf("UpsertPullRequestReviewResult(first) error = %v", err)
	}
	updatedReviewer, err := store.UpsertPullRequestReviewResult(ctx, UpsertPullRequestReviewResultParams{
		HandoffID: second.ID,
		Role:      "reviewer",
		State:     "completed",
		Summary:   "Ready after doc update.",
		Comments:  []string{"docs now match implementation"},
		Blockers:  []string{"awaiting human merge"},
		Outcome:   "approved_with_blocker",
	})
	if err != nil {
		t.Fatalf("UpsertPullRequestReviewResult(second) error = %v", err)
	}
	if updatedReviewer.ID != reviewer.ID {
		t.Fatalf("updatedReviewer.ID = %d, want idempotent ID %d", updatedReviewer.ID, reviewer.ID)
	}

	results, err := store.ListPullRequestReviewResults(ctx, second.ID)
	if err != nil {
		t.Fatalf("ListPullRequestReviewResults() error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("ListPullRequestReviewResults() len = %d, want 1: %+v", len(results), results)
	}
	if results[0].Summary != "Ready after doc update." || results[0].Outcome != "approved_with_blocker" {
		t.Fatalf("review result = %+v, want latest review fields", results[0])
	}
	if !reflect.DeepEqual(results[0].Comments, []string{"docs now match implementation"}) {
		t.Fatalf("review comments = %#v, want latest comments", results[0].Comments)
	}

	handoffs, err := store.ListPullRequestHandoffs(ctx, ListPullRequestHandoffsParams{
		Repo:        "acme/odin-os",
		ReviewState: "reviewing",
	})
	if err != nil {
		t.Fatalf("ListPullRequestHandoffs() error = %v", err)
	}
	if len(handoffs) != 1 || handoffs[0].ID != second.ID {
		t.Fatalf("ListPullRequestHandoffs() = %+v, want updated handoff", handoffs)
	}
}
