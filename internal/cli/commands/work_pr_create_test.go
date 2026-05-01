package commands

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"odin-os/internal/registry"
)

func TestRunWorkPRCreateCreatesDraftPRWithBoundedCIAndDeploymentAudit(t *testing.T) {
	ctx := context.Background()
	store := openWorkCommandStore(t)
	defer store.Close()
	repoRoot := initStage6DocsOnlyRepo(t)
	remoteRoot := initStage6Remote(t, repoRoot)
	_ = remoteRoot
	projectRegistry := workerDryRunProjectRegistry(t, repoRoot)
	t.Setenv("GITHUB_TOKEN", "github_pat_1234567890abcdefghijklmnopqrstuvwxyz")
	t.Setenv("ODIN_ROOT", t.TempDir())

	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatalf("ReadAll() error = %v", err)
		}
		requests = append(requests, request.Method+" "+request.URL.Path+"?"+request.URL.RawQuery+" "+string(body))
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/repos/marcusgoll/odin-os/issues/123":
			fmt.Fprint(response, `{"number":123,"title":"Stage 6 docs proof","html_url":"https://github.example/marcusgoll/odin-os/issues/123","state":"open","labels":[]}`)
		case request.Method == http.MethodGet && request.URL.Path == "/repos/marcusgoll/odin-os/pulls":
			fmt.Fprint(response, `[]`)
		case request.Method == http.MethodPost && request.URL.Path == "/repos/marcusgoll/odin-os/pulls":
			if !strings.Contains(string(body), `"draft":true`) {
				t.Fatalf("create PR body = %s, want draft=true", string(body))
			}
			if strings.Contains(strings.ToLower(string(body)), "closes #123") {
				t.Fatalf("create PR body used closing keyword: %s", string(body))
			}
			fmt.Fprint(response, `{"number":77,"html_url":"https://github.example/marcusgoll/odin-os/pull/77","state":"open","draft":true,"title":"Stage 6 docs proof","head":{"ref":"stage6-docs-proof"},"base":{"ref":"main"}}`)
		case request.Method == http.MethodGet && request.URL.Path == "/repos/marcusgoll/odin-os/issues/77/comments":
			fmt.Fprint(response, `[]`)
		case request.Method == http.MethodPost && request.URL.Path == "/repos/marcusgoll/odin-os/issues/77/comments":
			if !strings.Contains(string(body), "odin-stage6") || !strings.Contains(string(body), "diff_sha256=") {
				t.Fatalf("comment body = %s, want Stage 6 marker and diff hash", string(body))
			}
			fmt.Fprint(response, `{"html_url":"https://github.example/marcusgoll/odin-os/pull/77#issuecomment-1","body":"ok"}`)
		case request.Method == http.MethodGet && request.URL.Path == "/repos/marcusgoll/odin-os/actions/runs":
			fmt.Fprint(response, `{"workflow_runs":[{"id":10,"name":"Odin E2E","path":".github/workflows/odin-e2e.yml","html_url":"https://github.example/marcusgoll/odin-os/actions/runs/10","status":"completed","conclusion":"success","head_branch":"stage6-docs-proof","event":"pull_request"},{"id":11,"name":"ci","path":".github/workflows/ci.yml","html_url":"https://github.example/marcusgoll/odin-os/actions/runs/11","status":"completed","conclusion":"success","head_branch":"stage6-docs-proof","event":"pull_request"}]}`)
		default:
			t.Fatalf("unexpected request: %s %s?%s body=%s", request.Method, request.URL.Path, request.URL.RawQuery, string(body))
		}
	}))
	defer server.Close()
	t.Setenv("ODIN_GITHUB_API_BASE_URL", server.URL)

	var output strings.Builder
	err := RunWork(ctx, store, projectRegistry, registry.Snapshot{}, []string{
		"pr-create",
		"--issue", "123",
		"--approved-target", "marcusgoll/odin-os#123",
		"--worktree", repoRoot,
		"--base", "main",
		"--wait-ci",
		"--ci-timeout", "1s",
		"--json",
	}, &output)
	if err != nil {
		t.Fatalf("RunWork(pr-create) error = %v\noutput:\n%s", err, output.String())
	}
	if strings.Contains(output.String(), "github_pat_1234567890abcdefghijklmnopqrstuvwxyz") {
		t.Fatalf("output leaked token:\n%s", output.String())
	}

	var report struct {
		Project        string `json:"project"`
		Repo           string `json:"repo"`
		Issue          int    `json:"issue"`
		ApprovedTarget string `json:"approved_target"`
		Diff           struct {
			DocsOnly bool     `json:"docs_only"`
			SHA256   string   `json:"sha256"`
			Files    []string `json:"files"`
		} `json:"diff"`
		Branch struct {
			Name   string `json:"name"`
			Pushed bool   `json:"pushed"`
			Reused bool   `json:"reused"`
		} `json:"branch"`
		PR struct {
			Number  int    `json:"number"`
			URL     string `json:"url"`
			Draft   bool   `json:"draft"`
			Created bool   `json:"created"`
			Reused  bool   `json:"reused"`
		} `json:"pr"`
		EvidenceComments []struct {
			Marker  string `json:"marker"`
			Created bool   `json:"created"`
			Reused  bool   `json:"reused"`
		} `json:"evidence_comments"`
		CI struct {
			Waited     bool   `json:"waited"`
			TimedOut   bool   `json:"timed_out"`
			URL        string `json:"url"`
			Conclusion string `json:"conclusion"`
		} `json:"ci"`
		DeploymentAudit struct {
			NoDeploymentWorkflows bool `json:"no_deployment_workflows"`
			Dispatches            int  `json:"dispatches"`
			Mutations             int  `json:"mutations"`
		} `json:"deployment_audit"`
		GitHubWrites   int    `json:"github_writes"`
		Merge          string `json:"merge"`
		Deployment     string `json:"deployment"`
		Dispatch       string `json:"dispatch"`
		CodexExecution string `json:"codex_execution"`
		DurableState   struct {
			Created bool `json:"created"`
		} `json:"durable_state"`
	}
	if err := json.Unmarshal([]byte(output.String()), &report); err != nil {
		t.Fatalf("json.Unmarshal() error = %v\noutput:\n%s", err, output.String())
	}
	if report.Project != "odin-core" || report.Repo != "marcusgoll/odin-os" || report.Issue != 123 || report.ApprovedTarget != "marcusgoll/odin-os#123" {
		t.Fatalf("target = %+v, want odin-core marcusgoll/odin-os#123", report)
	}
	if !report.Diff.DocsOnly || report.Diff.SHA256 == "" || !containsString(report.Diff.Files, "docs/stage6-proof.md") {
		t.Fatalf("diff = %+v, want docs-only docs/stage6-proof.md", report.Diff)
	}
	if report.Branch.Name != "stage6-docs-proof" || !report.Branch.Pushed || report.Branch.Reused {
		t.Fatalf("branch = %+v, want pushed new stage6-docs-proof", report.Branch)
	}
	if !report.PR.Created || report.PR.Reused || !report.PR.Draft || report.PR.Number != 77 || report.PR.URL == "" {
		t.Fatalf("pr = %+v, want created draft PR #77", report.PR)
	}
	if len(report.EvidenceComments) != 2 {
		t.Fatalf("evidence comments = %+v, want two Stage 6 comments", report.EvidenceComments)
	}
	for _, comment := range report.EvidenceComments {
		if !comment.Created || comment.Reused || !strings.Contains(comment.Marker, "odin-stage6") {
			t.Fatalf("evidence comment = %+v, want created stable marker", comment)
		}
	}
	if !report.CI.Waited || report.CI.TimedOut || report.CI.Conclusion != "success" || !strings.Contains(report.CI.URL, "/actions/runs/10") {
		t.Fatalf("ci = %+v, want successful bounded wait", report.CI)
	}
	if !report.DeploymentAudit.NoDeploymentWorkflows || report.DeploymentAudit.Dispatches != 0 || report.DeploymentAudit.Mutations != 0 {
		t.Fatalf("deployment audit = %+v, want no deployment workflows and zero mutations", report.DeploymentAudit)
	}
	if report.GitHubWrites != 3 || report.Merge != "not_merged" || report.Deployment != "not_started" || report.Dispatch != "not_started" || report.CodexExecution != "not_started" || report.DurableState.Created {
		t.Fatalf("side effects = writes %d merge %q deployment %q dispatch %q codex %q durable %+v", report.GitHubWrites, report.Merge, report.Deployment, report.Dispatch, report.CodexExecution, report.DurableState)
	}
	for _, forbidden := range []string{"/labels", "/merge", "/reviews", "/actions/workflows", "/dispatches"} {
		for _, request := range requests {
			if strings.Contains(request, forbidden) {
				t.Fatalf("request %q contains forbidden path fragment %q", request, forbidden)
			}
		}
	}
	for _, table := range []string{"external_issues", "tasks", "runs", "approvals", "worktree_leases"} {
		var count int
		if err := store.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("%s count = %d, want no durable Stage 6 state", table, count)
		}
	}
}

func TestRunWorkPRCreateRerunReusesDraftPRAndEvidenceComments(t *testing.T) {
	ctx := context.Background()
	store := openWorkCommandStore(t)
	defer store.Close()
	repoRoot := initStage6DocsOnlyRepo(t)
	initStage6Remote(t, repoRoot)
	runStage6Git(t, repoRoot, "push", "origin", "HEAD:refs/heads/stage6-docs-proof")
	projectRegistry := workerDryRunProjectRegistry(t, repoRoot)
	diffHash := stage6DiffHashForTest(t, repoRoot, "main")

	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatalf("ReadAll() error = %v", err)
		}
		requests = append(requests, request.Method+" "+request.URL.Path+"?"+request.URL.RawQuery+" "+string(body))
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/repos/marcusgoll/odin-os/issues/123":
			fmt.Fprint(response, `{"number":123,"title":"Stage 6 docs proof","html_url":"https://github.example/marcusgoll/odin-os/issues/123","state":"open","labels":[]}`)
		case request.Method == http.MethodGet && request.URL.Path == "/repos/marcusgoll/odin-os/pulls":
			fmt.Fprint(response, `[{"number":77,"html_url":"https://github.example/marcusgoll/odin-os/pull/77","state":"open","draft":true,"title":"Stage 6 docs proof","head":{"ref":"stage6-docs-proof"},"base":{"ref":"main"}}]`)
		case request.Method == http.MethodGet && request.URL.Path == "/repos/marcusgoll/odin-os/issues/77/comments":
			fmt.Fprintf(response, `[{"html_url":"https://github.example/marcusgoll/odin-os/pull/77#issuecomment-1","body":"<!-- odin-stage6-review-evidence -->\ndiff_sha256=%s\nreview evidence"},{"html_url":"https://github.example/marcusgoll/odin-os/pull/77#issuecomment-2","body":"<!-- odin-stage6-human-review-handoff -->\ndiff_sha256=%s\nhandoff evidence"}]`, diffHash, diffHash)
		case request.Method == http.MethodGet && request.URL.Path == "/repos/marcusgoll/odin-os/actions/runs":
			fmt.Fprint(response, `{"workflow_runs":[{"id":10,"name":"Odin E2E","path":".github/workflows/odin-e2e.yml","html_url":"https://github.example/marcusgoll/odin-os/actions/runs/10","status":"completed","conclusion":"success","head_branch":"stage6-docs-proof","event":"pull_request"}]}`)
		default:
			t.Fatalf("unexpected request on idempotent rerun: %s %s?%s body=%s", request.Method, request.URL.Path, request.URL.RawQuery, string(body))
		}
	}))
	defer server.Close()
	t.Setenv("ODIN_GITHUB_API_BASE_URL", server.URL)
	t.Setenv("GITHUB_TOKEN", "github_pat_1234567890abcdefghijklmnopqrstuvwxyz")

	var output strings.Builder
	err := RunWork(ctx, store, projectRegistry, registry.Snapshot{}, []string{
		"pr-create",
		"--issue", "123",
		"--approved-target", "marcusgoll/odin-os#123",
		"--worktree", repoRoot,
		"--base", "main",
		"--wait-ci",
		"--ci-timeout", "1s",
		"--json",
	}, &output)
	if err != nil {
		t.Fatalf("RunWork(pr-create idempotent) error = %v\noutput:\n%s", err, output.String())
	}

	var report struct {
		Branch struct {
			Pushed bool `json:"pushed"`
			Reused bool `json:"reused"`
		} `json:"branch"`
		PR struct {
			Created bool `json:"created"`
			Reused  bool `json:"reused"`
		} `json:"pr"`
		EvidenceComments []struct {
			Created bool `json:"created"`
			Reused  bool `json:"reused"`
		} `json:"evidence_comments"`
		GitHubWrites int `json:"github_writes"`
	}
	if err := json.Unmarshal([]byte(output.String()), &report); err != nil {
		t.Fatalf("json.Unmarshal() error = %v\noutput:\n%s", err, output.String())
	}
	if report.Branch.Pushed || !report.Branch.Reused || report.PR.Created || !report.PR.Reused || report.GitHubWrites != 0 {
		t.Fatalf("idempotency = branch %+v pr %+v writes %d, want reuse with zero writes", report.Branch, report.PR, report.GitHubWrites)
	}
	for _, comment := range report.EvidenceComments {
		if comment.Created || !comment.Reused {
			t.Fatalf("comment = %+v, want reused", comment)
		}
	}
	for _, request := range requests {
		if !strings.HasPrefix(request, "GET ") {
			t.Fatalf("request = %q, want only reads on idempotent rerun", request)
		}
	}
}

func TestRunWorkPRCreateRejectsNonDocsDiffBeforeGitHubWrites(t *testing.T) {
	ctx := context.Background()
	store := openWorkCommandStore(t)
	defer store.Close()
	repoRoot := initWorkerDryRunGitRepo(t)
	initStage6Remote(t, repoRoot)
	runWorkerDryRunGit(t, repoRoot, "checkout", "-b", "stage6-code-change")
	if err := os.WriteFile(filepath.Join(repoRoot, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write code file: %v", err)
	}
	runWorkerDryRunGit(t, repoRoot, "add", "main.go")
	runWorkerDryRunGit(t, repoRoot, "commit", "-m", "code change")
	projectRegistry := workerDryRunProjectRegistry(t, repoRoot)
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests = append(requests, request.Method+" "+request.URL.Path)
		t.Fatalf("unexpected GitHub request before docs-only validation: %s %s", request.Method, request.URL.Path)
	}))
	defer server.Close()
	t.Setenv("ODIN_GITHUB_API_BASE_URL", server.URL)

	var output strings.Builder
	err := RunWork(ctx, store, projectRegistry, registry.Snapshot{}, []string{
		"pr-create",
		"--issue", "123",
		"--approved-target", "marcusgoll/odin-os#123",
		"--worktree", repoRoot,
		"--base", "main",
		"--wait-ci",
		"--json",
	}, &output)
	if err == nil || !strings.Contains(err.Error(), "docs-only") {
		t.Fatalf("RunWork(pr-create non-docs) error = %v, want docs-only failure", err)
	}
	if len(requests) != 0 {
		t.Fatalf("GitHub requests = %#v, want none", requests)
	}
}

func initStage6DocsOnlyRepo(t *testing.T) string {
	t.Helper()

	root := initWorkerDryRunGitRepo(t)
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}
	runWorkerDryRunGit(t, root, "checkout", "-b", "stage6-docs-proof")
	if err := os.WriteFile(filepath.Join(root, "docs", "stage6-proof.md"), []byte("# Stage 6 proof\n"), 0o644); err != nil {
		t.Fatalf("write docs file: %v", err)
	}
	runWorkerDryRunGit(t, root, "add", "docs/stage6-proof.md")
	runWorkerDryRunGit(t, root, "commit", "-m", "docs: stage 6 proof")
	return root
}

func initStage6Remote(t *testing.T, repoRoot string) string {
	t.Helper()

	remoteRoot := filepath.Join(t.TempDir(), "origin.git")
	command := exec.Command("git", "init", "--bare", remoteRoot)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git init --bare: %v\n%s", err, string(output))
	}
	runStage6Git(t, repoRoot, "remote", "add", "origin", remoteRoot)
	runStage6Git(t, repoRoot, "push", "origin", "main")
	return remoteRoot
}

func runStage6Git(t *testing.T, dir string, args ...string) {
	t.Helper()

	command := exec.Command("git", append([]string{"-C", dir}, args...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, string(output))
	}
}

func stage6DiffHashForTest(t *testing.T, repoRoot string, base string) string {
	t.Helper()

	stat := stage6GitOutputForTest(t, repoRoot, "diff", "--stat", base)
	nameStatus := stage6GitOutputForTest(t, repoRoot, "diff", "--name-status", base)
	sum := sha256.Sum256([]byte(strings.TrimSpace(stat) + "\n---\n" + strings.TrimSpace(nameStatus)))
	return hex.EncodeToString(sum[:])
}

func stage6GitOutputForTest(t *testing.T, repoRoot string, args ...string) string {
	t.Helper()

	command := exec.Command("git", append([]string{"-C", repoRoot}, args...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, string(output))
	}
	return string(output)
}
