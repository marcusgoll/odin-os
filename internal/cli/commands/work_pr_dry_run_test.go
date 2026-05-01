package commands

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"odin-os/internal/registry"
)

func TestRunWorkPRDryRunGeneratesReviewHandoffWithoutPushOrMerge(t *testing.T) {
	ctx := context.Background()
	store := openWorkCommandStore(t)
	defer store.Close()
	repoRoot := initWorkerDryRunGitRepo(t)
	runWorkerDryRunGit(t, repoRoot, "checkout", "-b", "stage5-proof")
	if err := os.WriteFile(filepath.Join(repoRoot, "stage5.txt"), []byte("stage 5\n"), 0o644); err != nil {
		t.Fatalf("write stage5 file: %v", err)
	}
	runWorkerDryRunGit(t, repoRoot, "add", "stage5.txt")
	runWorkerDryRunGit(t, repoRoot, "commit", "-m", "stage 5 proof")
	odinRoot := t.TempDir()
	t.Setenv("ODIN_ROOT", odinRoot)

	var output strings.Builder
	err := RunWork(ctx, store, workerDryRunProjectRegistry(t, repoRoot), registry.Snapshot{}, []string{
		"pr-dry-run",
		"--worktree", repoRoot,
		"--base", "main",
		"--json",
	}, &output)
	if err != nil {
		t.Fatalf("RunWork(pr-dry-run) error = %v", err)
	}

	var report struct {
		Worktree    string `json:"worktree"`
		Base        string `json:"base"`
		CurrentHead string `json:"current_head"`
		DiffSummary struct {
			Generated bool     `json:"generated"`
			Files     []string `json:"files"`
			Text      string   `json:"text"`
		} `json:"diff_summary"`
		PRBody struct {
			Generated        bool   `json:"generated"`
			Path             string `json:"path"`
			SHA256           string `json:"sha256"`
			TemplateVerified bool   `json:"template_verified"`
		} `json:"pr_body"`
		Checklist struct {
			Path   string   `json:"path"`
			SHA256 string   `json:"sha256"`
			Items  []string `json:"items"`
		} `json:"human_checklist"`
		Artifacts []struct {
			Path   string `json:"path"`
			SHA256 string `json:"sha256"`
			Kind   string `json:"kind"`
			Label  string `json:"label"`
		} `json:"artifacts"`
		GitHubWrites int    `json:"github_writes"`
		Push         string `json:"push"`
		Merge        string `json:"merge"`
		PRs          string `json:"prs"`
		Dispatch     string `json:"dispatch"`
	}
	if err := json.Unmarshal([]byte(output.String()), &report); err != nil {
		t.Fatalf("json.Unmarshal() error = %v\noutput:\n%s", err, output.String())
	}
	if report.Worktree != repoRoot || report.Base != "main" || report.CurrentHead == "" {
		t.Fatalf("target = worktree %q base %q head %q", report.Worktree, report.Base, report.CurrentHead)
	}
	if !report.DiffSummary.Generated || !containsString(report.DiffSummary.Files, "stage5.txt") || !strings.Contains(report.DiffSummary.Text, "stage5.txt") {
		t.Fatalf("diff summary = %+v, want stage5.txt", report.DiffSummary)
	}
	if !report.PRBody.Generated || report.PRBody.Path == "" || report.PRBody.SHA256 == "" || !report.PRBody.TemplateVerified {
		t.Fatalf("pr body = %+v, want generated and template verified", report.PRBody)
	}
	bodyContent, err := os.ReadFile(report.PRBody.Path)
	if err != nil {
		t.Fatalf("read PR body: %v", err)
	}
	for _, heading := range []string{"## Summary", "## Proven", "## Unproven", "## Commands Run"} {
		if !strings.Contains(string(bodyContent), heading) {
			t.Fatalf("PR body missing heading %s:\n%s", heading, string(bodyContent))
		}
	}
	checklistContent, err := os.ReadFile(report.Checklist.Path)
	if err != nil {
		t.Fatalf("read checklist: %v", err)
	}
	for _, item := range []string{
		"Review diff summary",
		"Confirm tests listed under Commands Run are sufficient",
		"Confirm Unproven items are acceptable",
		"Confirm no push occurred during dry-run",
		"Confirm no live PR was created or updated",
		"Confirm no merge occurred",
	} {
		if !containsString(report.Checklist.Items, item) || !strings.Contains(string(checklistContent), item) {
			t.Fatalf("checklist missing %q: report=%#v file=\n%s", item, report.Checklist.Items, string(checklistContent))
		}
	}
	if len(report.Artifacts) != 3 {
		t.Fatalf("artifacts = %+v, want pr body, checklist, and diff summary", report.Artifacts)
	}
	for _, artifact := range report.Artifacts {
		if !strings.HasPrefix(artifact.Path, filepath.Join(odinRoot, "runs", "pr-dry-run")+string(filepath.Separator)) || artifact.SHA256 == "" || artifact.Label != "draft artifact, not durable PR handoff state" {
			t.Fatalf("artifact = %+v, want local draft under ODIN_ROOT", artifact)
		}
	}
	if report.GitHubWrites != 0 || report.Push != "not_pushed" || report.Merge != "not_merged" || report.PRs != "not_created_or_updated" || report.Dispatch != "not_started" {
		t.Fatalf("side effects = writes %d push %q merge %q prs %q dispatch %q", report.GitHubWrites, report.Push, report.Merge, report.PRs, report.Dispatch)
	}
	for _, table := range []string{"external_issues", "tasks", "runs", "approvals", "worktree_leases"} {
		var count int
		if err := store.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("%s count = %d, want no durable PR dry-run state", table, count)
		}
	}
}

func TestRunWorkPRDryRunFailsWhenThereIsNoDiff(t *testing.T) {
	ctx := context.Background()
	store := openWorkCommandStore(t)
	defer store.Close()
	repoRoot := initWorkerDryRunGitRepo(t)
	t.Setenv("ODIN_ROOT", t.TempDir())

	var output strings.Builder
	err := RunWork(ctx, store, workerDryRunProjectRegistry(t, repoRoot), registry.Snapshot{}, []string{
		"pr-dry-run",
		"--worktree", repoRoot,
		"--base", "main",
		"--json",
	}, &output)
	if err == nil || !strings.Contains(err.Error(), "no diff") {
		t.Fatalf("RunWork(pr-dry-run) error = %v, want no diff failure", err)
	}
}
