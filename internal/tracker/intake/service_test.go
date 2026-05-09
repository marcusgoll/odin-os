package intake

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"odin-os/internal/core/projects"
	"odin-os/internal/store/sqlite"
	"odin-os/internal/tracker"
)

func TestServiceFetchesEligibleIssuesAndPersistsThemIdempotently(t *testing.T) {
	ctx := context.Background()
	store := openMigratedStore(t)
	defer store.Close()

	registry := testProjectRegistry(t)
	fake := &fakeTracker{
		issues: []tracker.Issue{
			{
				Provider: "github",
				Repo:     "acme/alpha",
				Number:   11,
				Title:    "Implement read-only intake",
				Body:     "body v1",
				URL:      "https://github.example/acme/alpha/issues/11",
				State:    "open",
				Labels:   []string{tracker.LabelReady, tracker.AgentLabelBackend},
			},
		},
	}
	service := Service{
		Store:    store,
		Registry: registry,
		NewTracker: func(project projects.Manifest, options SyncOptions) (tracker.Tracker, error) {
			if project.GitHub.Repo != "acme/alpha" {
				t.Fatalf("project.GitHub.Repo = %q, want acme/alpha", project.GitHub.Repo)
			}
			return fake, nil
		},
	}

	first, err := service.SyncProject(ctx, SyncOptions{ProjectKey: "alpha"})
	if err != nil {
		t.Fatalf("SyncProject(first) error = %v", err)
	}
	second, err := service.SyncProject(ctx, SyncOptions{ProjectKey: "alpha"})
	if err != nil {
		t.Fatalf("SyncProject(second) error = %v", err)
	}

	if first.Fetched != 1 || first.Persisted != 1 || second.Fetched != 1 || second.Persisted != 1 {
		t.Fatalf("summaries first=%+v second=%+v, want one fetched/persisted each time", first, second)
	}
	if fake.fetchCalls != 2 {
		t.Fatalf("fetchCalls = %d, want 2", fake.fetchCalls)
	}
	if fake.mutationCalls != 0 {
		t.Fatalf("mutationCalls = %d, want 0 read-only intake", fake.mutationCalls)
	}

	issues, err := store.ListExternalIssues(ctx, sqlite.ListExternalIssuesParams{Repo: "acme/alpha", SyncStatus: "eligible"})
	if err != nil {
		t.Fatalf("ListExternalIssues() error = %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("ListExternalIssues() len = %d, want idempotent 1: %+v", len(issues), issues)
	}
}

func TestServiceDryRunFetchesEligibleIssuesWithoutPersisting(t *testing.T) {
	ctx := context.Background()
	store := openMigratedStore(t)
	defer store.Close()

	fake := &fakeTracker{
		issues: []tracker.Issue{{
			Provider: "github",
			Repo:     "acme/alpha",
			Number:   12,
			Title:    "Dry run intake",
			Body:     "body",
			State:    "open",
			Labels:   []string{tracker.LabelReady},
		}},
	}
	service := Service{
		Store:    store,
		Registry: testProjectRegistry(t),
		NewTracker: func(project projects.Manifest, options SyncOptions) (tracker.Tracker, error) {
			if !options.DryRun {
				t.Fatal("options.DryRun = false, want true")
			}
			return fake, nil
		},
	}

	summary, err := service.SyncProject(ctx, SyncOptions{ProjectKey: "alpha", DryRun: true})
	if err != nil {
		t.Fatalf("SyncProject(dry-run) error = %v", err)
	}
	if summary.Fetched != 1 || summary.Persisted != 0 || !summary.DryRun {
		t.Fatalf("summary = %+v, want fetched=1 persisted=0 dry_run=true", summary)
	}

	issues, err := store.ListExternalIssues(ctx, sqlite.ListExternalIssuesParams{})
	if err != nil {
		t.Fatalf("ListExternalIssues() error = %v", err)
	}
	if len(issues) != 0 {
		t.Fatalf("ListExternalIssues() len = %d, want 0 in dry-run", len(issues))
	}
}

func TestNewGitHubTrackerUsesProjectManifestRepoTokenEnvAndDryRun(t *testing.T) {
	const token = "ghp_manifesttoken1234567890abcdef"

	t.Setenv("GITHUB_TOKEN", token)

	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests++
		if request.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", request.Method)
		}
		if request.URL.Path != "/repos/acme/manifest-repo/issues" {
			t.Fatalf("path = %s, want /repos/acme/manifest-repo/issues", request.URL.Path)
		}
		if got := request.URL.Query().Get("state"); got != "open" {
			t.Fatalf("state query = %q, want open", got)
		}
		if got := request.Header.Get("Authorization"); got != "Bearer "+token {
			t.Fatalf("Authorization = %q, want bearer token from env", got)
		}
		fmt.Fprint(response, `[{"number":17,"title":"ready","body":"body","html_url":"https://github.example/acme/manifest-repo/issues/17","state":"open","labels":[{"name":"odin:ready"}]}]`)
	}))
	defer server.Close()
	t.Setenv("ODIN_GITHUB_API_BASE_URL", server.URL)

	source, err := NewGitHubTracker(projects.Manifest{
		Key:          "alpha",
		ProjectClass: projects.ProjectClassGitHubBacked,
		GitHub:       projects.GitHub{Repo: "acme/manifest-repo"},
	}, SyncOptions{DryRun: true})
	if err != nil {
		t.Fatalf("NewGitHubTracker() error = %v", err)
	}

	issues, err := source.FetchEligibleIssues(context.Background())
	if err != nil {
		t.Fatalf("FetchEligibleIssues() error = %v", err)
	}
	if len(issues) != 1 || issues[0].Repo != "acme/manifest-repo" || issues[0].Number != 17 {
		t.Fatalf("issues = %+v, want manifest repo issue #17", issues)
	}

	if err := source.MarkInProgress(context.Background(), tracker.IssueID{Provider: "github", Repo: "acme/manifest-repo", Number: 17}); err != nil {
		t.Fatalf("dry-run MarkInProgress() error = %v", err)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want only the read-only fetch request", requests)
	}
}

func TestServiceRejectsProjectsWithoutGitHubMetadataBeforeTrackerConstruction(t *testing.T) {
	ctx := context.Background()
	store := openMigratedStore(t)
	defer store.Close()

	calls := 0
	service := Service{
		Store: store,
		Registry: testProjectRegistryFromYAML(t, `
version: 1
projects:
  - key: local
    name: Local
    project_class: local_git_project
    git_root: .
    default_branch: main
    policy:
      allowed_commands: [status]
      branch_rules:
        protected_branches: [main]
        require_worktree: true
        require_task_branch: true
        allow_default_branch_mutation: false
      approval_gates:
        require_for_governance_changes: true
        require_for_destructive_operations: true
        require_for_system_project_changes: true
      merge_policy:
        mode: squash
        allow_direct_to_default_branch: false
      destructive_operations:
        allow_reset: false
        allow_clean: false
        allow_force_push: false
        require_explicit_approval: true
`),
		NewTracker: func(project projects.Manifest, options SyncOptions) (tracker.Tracker, error) {
			calls++
			return &fakeTracker{}, nil
		},
	}

	_, err := service.SyncProject(ctx, SyncOptions{ProjectKey: "local"})
	if err == nil || !strings.Contains(err.Error(), `project "local" is not a GitHub-backed intake source`) {
		t.Fatalf("SyncProject(local) error = %v, want GitHub-backed source error", err)
	}
	if calls != 0 {
		t.Fatalf("tracker factory calls = %d, want 0 for missing GitHub metadata", calls)
	}
}

func TestNewGitHubTrackerRejectsInvalidGitHubRepoMetadata(t *testing.T) {
	for _, repo := range []string{"missing-slash", "/repo", "owner/"} {
		t.Run(repo, func(t *testing.T) {
			_, err := NewGitHubTracker(projects.Manifest{
				Key:          "alpha",
				ProjectClass: projects.ProjectClassGitHubBacked,
				GitHub:       projects.GitHub{Repo: repo},
			}, SyncOptions{})
			if err == nil || !strings.Contains(err.Error(), "invalid GitHub repo") {
				t.Fatalf("NewGitHubTracker(%q) error = %v, want invalid GitHub repo", repo, err)
			}
		})
	}
}

func openMigratedStore(t *testing.T) *sqlite.Store {
	t.Helper()

	store, err := sqlite.Open(filepath.Join(t.TempDir(), "odin.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		_ = store.Close()
		t.Fatalf("Migrate() error = %v", err)
	}
	return store
}

func testProjectRegistry(t *testing.T) projects.Registry {
	t.Helper()

	return testProjectRegistryFromYAML(t, `
version: 1
projects:
  - key: alpha
    name: Alpha
    project_class: github_backed_project
    git_root: .
    default_branch: main
    github:
      repo: acme/alpha
    policy:
      allowed_commands: [status]
      branch_rules:
        protected_branches: [main]
        require_worktree: true
        require_task_branch: true
        allow_default_branch_mutation: false
      approval_gates:
        require_for_governance_changes: true
        require_for_destructive_operations: true
        require_for_system_project_changes: true
      merge_policy:
        mode: squash
        allow_direct_to_default_branch: false
      destructive_operations:
        allow_reset: false
        allow_clean: false
        allow_force_push: false
        require_explicit_approval: true
`)
}

func testProjectRegistryFromYAML(t *testing.T, content string) projects.Registry {
	t.Helper()

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir git root: %v", err)
	}
	path := filepath.Join(root, "projects.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write projects: %v", err)
	}
	registry, diagnostics, err := projects.Register(path)
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %+v, want none", diagnostics)
	}
	return registry
}

type fakeTracker struct {
	issues        []tracker.Issue
	fetchCalls    int
	mutationCalls int
}

func (fake *fakeTracker) FetchEligibleIssues(context.Context) ([]tracker.Issue, error) {
	fake.fetchCalls++
	return fake.issues, nil
}

func (fake *fakeTracker) FetchIssueByID(context.Context, tracker.IssueID) (tracker.Issue, error) {
	fake.mutationCalls++
	return tracker.Issue{}, errors.New("unexpected issue lookup")
}

func (fake *fakeTracker) MarkInProgress(context.Context, tracker.IssueID) error {
	fake.mutationCalls++
	return errors.New("unexpected mutation")
}

func (fake *fakeTracker) MarkBlocked(context.Context, tracker.IssueID, string) error {
	fake.mutationCalls++
	return errors.New("unexpected mutation")
}

func (fake *fakeTracker) MarkFailed(context.Context, tracker.IssueID, string) error {
	fake.mutationCalls++
	return errors.New("unexpected mutation")
}

func (fake *fakeTracker) MarkReadyForReview(context.Context, tracker.IssueID) error {
	fake.mutationCalls++
	return errors.New("unexpected mutation")
}

func (fake *fakeTracker) MarkDone(context.Context, tracker.IssueID) error {
	fake.mutationCalls++
	return errors.New("unexpected mutation")
}

func (fake *fakeTracker) AddComment(context.Context, tracker.IssueID, string) error {
	fake.mutationCalls++
	return errors.New("unexpected mutation")
}

func (fake *fakeTracker) CreateFollowUpIssue(context.Context, tracker.FollowUpIssue) (tracker.Issue, error) {
	fake.mutationCalls++
	return tracker.Issue{}, errors.New("unexpected mutation")
}
