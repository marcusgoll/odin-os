package commands

import (
	"context"
	"encoding/json"
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

func TestRunWorkIntakeJSONPerformsStage1TwoPassProofWithoutStartingWork(t *testing.T) {
	ctx := context.Background()
	store := openWorkCommandStore(t)
	defer store.Close()
	projectRegistry := commandProjectRegistry(t)
	t.Setenv("ODIN_DRY_RUN", "true")

	previousFactory := newIntakeTracker
	t.Cleanup(func() { newIntakeTracker = previousFactory })
	newIntakeTracker = func(project projects.Manifest, options trackerintake.SyncOptions) (tracker.Tracker, error) {
		if !options.DryRun {
			return nil, fmt.Errorf("options.DryRun = false, want true from ODIN_DRY_RUN")
		}
		return &commandAuditedFakeTracker{
			issues: []tracker.Issue{{
				Provider: "github",
				Repo:     project.GitHub.Repo,
				Number:   5,
				Title:    "Wire intake",
				Body:     "read-only",
				State:    "open",
				Labels:   []string{tracker.LabelReady},
			}},
			audit: tracker.RequestAudit{Reads: 1},
		}, nil
	}

	var output strings.Builder
	if err := RunWork(ctx, store, projectRegistry, registry.Snapshot{}, []string{"intake", "--project", "alpha", "--json"}, &output); err != nil {
		t.Fatalf("RunWork(intake --json) error = %v", err)
	}

	var report struct {
		Project      string `json:"project"`
		Repo         string `json:"repo"`
		StoredBefore int    `json:"stored_before"`
		StoredAfter  int    `json:"stored_after"`
		Idempotent   bool   `json:"idempotent"`
		GitHubWrites int    `json:"github_writes"`
		FirstPass    struct {
			Fetched   int `json:"fetched"`
			Persisted int `json:"persisted"`
		} `json:"first_pass"`
		SecondPass struct {
			Fetched   int `json:"fetched"`
			Persisted int `json:"persisted"`
		} `json:"second_pass"`
		MethodAudit struct {
			Reads  int `json:"reads"`
			Writes int `json:"writes"`
		} `json:"method_audit"`
	}
	if err := json.Unmarshal([]byte(output.String()), &report); err != nil {
		t.Fatalf("json.Unmarshal() error = %v\noutput:\n%s", err, output.String())
	}
	if report.Project != "alpha" || report.Repo != "acme/alpha" {
		t.Fatalf("report project/repo = %q/%q, want alpha/acme/alpha", report.Project, report.Repo)
	}
	if report.StoredBefore != 0 || report.StoredAfter != 1 || !report.Idempotent {
		t.Fatalf("report storage = before %d after %d idempotent %t, want 0/1/true", report.StoredBefore, report.StoredAfter, report.Idempotent)
	}
	if report.FirstPass.Fetched != 1 || report.FirstPass.Persisted != 1 || report.SecondPass.Fetched != 1 || report.SecondPass.Persisted != 1 {
		t.Fatalf("report passes = %+v/%+v, want fetched=1 persisted=1 for both", report.FirstPass, report.SecondPass)
	}
	if report.GitHubWrites != 0 || report.MethodAudit.Reads != 2 || report.MethodAudit.Writes != 0 {
		t.Fatalf("report audit = writes %d method %+v, want writes=0 reads=2", report.GitHubWrites, report.MethodAudit)
	}

	var taskCount int
	if err := store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM tasks`).Scan(&taskCount); err != nil {
		t.Fatalf("task count query: %v", err)
	}
	if taskCount != 0 {
		t.Fatalf("task count = %d, want no work item creation", taskCount)
	}
}

func TestRunWorkIntakeJSONFailsWhenStage1AuditObservesGitHubWrite(t *testing.T) {
	ctx := context.Background()
	store := openWorkCommandStore(t)
	defer store.Close()
	projectRegistry := commandProjectRegistry(t)
	t.Setenv("ODIN_DRY_RUN", "true")

	previousFactory := newIntakeTracker
	t.Cleanup(func() { newIntakeTracker = previousFactory })
	newIntakeTracker = func(project projects.Manifest, options trackerintake.SyncOptions) (tracker.Tracker, error) {
		return &commandAuditedFakeTracker{
			issues: []tracker.Issue{{
				Provider: "github",
				Repo:     project.GitHub.Repo,
				Number:   5,
				Title:    "Wire intake",
				State:    "open",
				Labels:   []string{tracker.LabelReady},
			}},
			audit: tracker.RequestAudit{
				Writes: 1,
				Forbidden: []tracker.ForbiddenRequest{{
					Method: "POST",
					Path:   "/repos/acme/alpha/issues/5/comments",
				}},
			},
		}, nil
	}

	var output strings.Builder
	err := RunWork(ctx, store, projectRegistry, registry.Snapshot{}, []string{"intake", "--project", "alpha", "--json"}, &output)
	if err == nil {
		t.Fatalf("RunWork(intake --json) error = nil, want forbidden GitHub write failure\noutput:\n%s", output.String())
	}
	if !strings.Contains(err.Error(), "forbidden GitHub write attempted") || strings.Contains(err.Error(), "GITHUB_TOKEN") {
		t.Fatalf("error = %q, want safe forbidden-write message", err.Error())
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

type commandAuditedFakeTracker struct {
	commandFakeTracker
	issues []tracker.Issue
	audit  tracker.RequestAudit
}

func (fake *commandAuditedFakeTracker) FetchEligibleIssues(context.Context) ([]tracker.Issue, error) {
	return fake.issues, nil
}

func (fake *commandAuditedFakeTracker) RequestAudit() tracker.RequestAudit {
	return fake.audit
}
