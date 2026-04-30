package commands

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"odin-os/internal/core/projects"
	"odin-os/internal/registry"
	"odin-os/internal/store/sqlite"
	"odin-os/internal/tracker"
	trackerintake "odin-os/internal/tracker/intake"
)

func TestRunWorkIntakeSyncsEligibleIssuesWithoutStartingWork(t *testing.T) {
	ctx := context.Background()
	store := openWorkCommandStore(t)
	defer store.Close()
	projectRegistry := commandProjectRegistry(t)

	previousFactory := newIntakeTracker
	t.Cleanup(func() { newIntakeTracker = previousFactory })
	newIntakeTracker = func(project projects.Manifest, options trackerintake.SyncOptions) (tracker.Tracker, error) {
		if project.GitHub.Repo != "acme/alpha" {
			return nil, fmt.Errorf("repo = %q, want acme/alpha", project.GitHub.Repo)
		}
		return &commandFakeTracker{issues: []tracker.Issue{{
			Provider: "github",
			Repo:     "acme/alpha",
			Number:   5,
			Title:    "Wire intake",
			Body:     "read-only",
			State:    "open",
			Labels:   []string{tracker.LabelReady},
		}}}, nil
	}

	var output strings.Builder
	if err := RunWork(ctx, store, projectRegistry, registry.Snapshot{}, []string{"intake", "--project", "alpha"}, &output); err != nil {
		t.Fatalf("RunWork(intake) error = %v", err)
	}
	for _, want := range []string{
		"project=alpha",
		"repo=acme/alpha",
		"fetched=1",
		"persisted=1",
		"dry_run=false",
		"dispatch=not_started",
		"prs=not_created",
	} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("output = %q, want %q", output.String(), want)
		}
	}

	issues, err := store.ListExternalIssues(ctx, sqlite.ListExternalIssuesParams{Repo: "acme/alpha"})
	if err != nil {
		t.Fatalf("ListExternalIssues() error = %v", err)
	}
	if len(issues) != 1 || issues[0].Title != "Wire intake" {
		t.Fatalf("issues = %+v, want persisted intake issue", issues)
	}

	var taskCount int
	if err := store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM tasks`).Scan(&taskCount); err != nil {
		t.Fatalf("task count query: %v", err)
	}
	if taskCount != 0 {
		t.Fatalf("task count = %d, want no scheduler dispatch/work item creation", taskCount)
	}
}

func openWorkCommandStore(t *testing.T) *sqlite.Store {
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

func commandProjectRegistry(t *testing.T) projects.Registry {
	t.Helper()

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir git root: %v", err)
	}
	path := filepath.Join(root, "projects.yaml")
	if err := os.WriteFile(path, []byte(`
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
`), 0o644); err != nil {
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

type commandFakeTracker struct {
	issues []tracker.Issue
}

func (fake *commandFakeTracker) FetchEligibleIssues(context.Context) ([]tracker.Issue, error) {
	return fake.issues, nil
}

func (fake *commandFakeTracker) FetchIssueByID(context.Context, tracker.IssueID) (tracker.Issue, error) {
	return tracker.Issue{}, fmt.Errorf("unexpected lookup")
}

func (fake *commandFakeTracker) MarkInProgress(context.Context, tracker.IssueID) error {
	return fmt.Errorf("unexpected mutation")
}

func (fake *commandFakeTracker) MarkBlocked(context.Context, tracker.IssueID, string) error {
	return fmt.Errorf("unexpected mutation")
}

func (fake *commandFakeTracker) MarkFailed(context.Context, tracker.IssueID, string) error {
	return fmt.Errorf("unexpected mutation")
}

func (fake *commandFakeTracker) MarkReadyForReview(context.Context, tracker.IssueID) error {
	return fmt.Errorf("unexpected mutation")
}

func (fake *commandFakeTracker) MarkDone(context.Context, tracker.IssueID) error {
	return fmt.Errorf("unexpected mutation")
}

func (fake *commandFakeTracker) AddComment(context.Context, tracker.IssueID, string) error {
	return fmt.Errorf("unexpected mutation")
}

func (fake *commandFakeTracker) CreateFollowUpIssue(context.Context, tracker.FollowUpIssue) (tracker.Issue, error) {
	return tracker.Issue{}, fmt.Errorf("unexpected mutation")
}
