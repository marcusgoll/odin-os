package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"odin-os/internal/core/projects"
	"odin-os/internal/registry"
	"odin-os/internal/store/sqlite"
)

func TestRunWorkSuperviseE2ERequiresJSON(t *testing.T) {
	ctx := context.Background()
	store := openWorkCommandStore(t)
	defer store.Close()

	err, output := runWorkSuperviseE2EForError(t, ctx, store, []string{"supervise", "e2e", "prepare-issue", "--project", "alpha"})
	if err == nil {
		t.Fatalf("RunWork(supervise e2e prepare-issue without --json) error = nil, want required JSON error\noutput:\n%s", output)
	}
	if !strings.Contains(err.Error(), "--json is required for work supervise in this slice") {
		t.Fatalf("error = %q, want required JSON error", err.Error())
	}
	assertNoSuperviseSideEffects(t, ctx, store)
}

func TestRunWorkSuperviseE2EUsageShowsPrepareIssueJSON(t *testing.T) {
	ctx := context.Background()
	store := openWorkCommandStore(t)
	defer store.Close()

	var output strings.Builder
	if err := RunWork(ctx, store, commandProjectRegistry(t), registry.Snapshot{}, []string{"supervise", "e2e", "prepare-issue"}, &output); err == nil {
		t.Fatalf("RunWork(supervise e2e prepare-issue usage probe) error = nil, want required JSON error\noutput:\n%s", output.String())
	} else if !strings.Contains(err.Error(), "e2e prepare-issue --project <key> --json") {
		t.Fatalf("error = %q, want prepare-issue usage to include --json", err.Error())
	}

	var workOutput strings.Builder
	if err := RunWork(ctx, store, commandProjectRegistry(t), registry.Snapshot{}, []string{"help"}, &workOutput); err != nil {
		t.Fatalf("RunWork(help) error = %v", err)
	}
	if !strings.Contains(workOutput.String(), "e2e prepare-issue --project <key> --json") {
		t.Fatalf("work usage = %q, want prepare-issue usage to include --json", workOutput.String())
	}
}

func TestRunWorkSuperviseE2EPrepareIssueRequiresProject(t *testing.T) {
	ctx := context.Background()
	store := openWorkCommandStore(t)
	defer store.Close()

	err, output := runWorkSuperviseE2EForError(t, ctx, store, []string{"supervise", "e2e", "prepare-issue", "--json"})
	if err == nil {
		t.Fatalf("RunWork(supervise e2e prepare-issue without --project) error = nil, want project error\noutput:\n%s", output)
	}
	if !strings.Contains(err.Error(), "missing --project for work supervise e2e prepare-issue") {
		t.Fatalf("error = %q, want missing project error", err.Error())
	}

	err, output = runWorkSuperviseE2EForError(t, ctx, store, []string{"supervise", "e2e", "prepare-issue", "--project", "missing", "--json"})
	if err == nil {
		t.Fatalf("RunWork(supervise e2e prepare-issue unknown project) error = nil, want unknown project\noutput:\n%s", output)
	}
	if !strings.Contains(err.Error(), "unknown project \"missing\"") {
		t.Fatalf("error = %q, want unknown project after validation", err.Error())
	}
	assertNoSuperviseSideEffects(t, ctx, store)
}

func TestRunWorkSuperviseE2EPrepareIssueCreatesLabeledDocsIssue(t *testing.T) {
	ctx := context.Background()
	store := openWorkCommandStore(t)
	defer store.Close()
	odinRoot := t.TempDir()
	t.Setenv("ODIN_ROOT", odinRoot)
	token := "github_pat_1234567890abcdefghijklmnopqrstuvwxyz"
	t.Setenv("GITHUB_TOKEN", token)

	requests := 0
	var requestBody struct {
		Title  string   `json:"title"`
		Body   string   `json:"body"`
		Labels []string `json:"labels"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests++
		if request.Method != http.MethodPost || request.URL.Path != "/repos/marcusgoll/odin-os/issues" {
			t.Fatalf("unexpected request: %s %s", request.Method, request.URL.Path)
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatalf("ReadAll() error = %v", err)
		}
		if err := json.Unmarshal(body, &requestBody); err != nil {
			t.Fatalf("Unmarshal(request body) error = %v\nbody=%s", err, string(body))
		}
		if !strings.Contains(requestBody.Title, "Stage 7 supervised E2E docs proof") {
			t.Fatalf("title = %q, want Stage 7 supervised E2E docs proof", requestBody.Title)
		}
		if !containsString(requestBody.Labels, "odin:ready") || !containsString(requestBody.Labels, "safety:low-risk") {
			t.Fatalf("labels = %#v, want odin:ready and safety:low-risk", requestBody.Labels)
		}
		if !strings.Contains(requestBody.Body, "Planned scope: docs/operations/stage-7-supervised-e2e-") {
			t.Fatalf("body = %q, want planned docs/operations scope", requestBody.Body)
		}
		fmt.Fprint(response, `{"number":321,"title":"Stage 7 supervised E2E docs proof","html_url":"https://github.example/marcusgoll/odin-os/issues/321","state":"open","labels":[{"name":"odin:ready"},{"name":"safety:low-risk"}]}`)
	}))
	defer server.Close()
	t.Setenv("ODIN_GITHUB_API_BASE_URL", server.URL)

	var output strings.Builder
	err := RunWork(ctx, store, stage7E2EProjectRegistry(t), registry.Snapshot{}, []string{
		"supervise", "e2e", "prepare-issue", "--project", "odin-core", "--json",
	}, &output)
	if err != nil {
		t.Fatalf("RunWork(supervise e2e prepare-issue) error = %v\noutput:\n%s", err, output.String())
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want one GitHub issue create request", requests)
	}

	var report superviseE2EPrepareIssueReport
	if err := json.Unmarshal([]byte(output.String()), &report); err != nil {
		t.Fatalf("Unmarshal(output) error = %v\noutput:\n%s", err, output.String())
	}
	if report.Phase != "prepared" || report.Status != "prepared" || report.Project != "odin-core" || report.Repo != "marcusgoll/odin-os" {
		t.Fatalf("report target/status = %+v, want prepared odin-core marcusgoll/odin-os", report)
	}
	if report.Issue.Number != 321 || report.Issue.URL != "https://github.example/marcusgoll/odin-os/issues/321" {
		t.Fatalf("issue = %+v, want created issue 321", report.Issue)
	}
	if !strings.HasPrefix(report.Issue.PlannedPath, "docs/operations/stage-7-supervised-e2e-") || !strings.HasSuffix(report.Issue.PlannedPath, ".md") {
		t.Fatalf("planned_path = %q, want docs/operations/stage-7-supervised-e2e-*.md", report.Issue.PlannedPath)
	}
	if report.PRs != "not_created" || report.Merge != "not_performed" || report.Deployment != "not_started" || !report.HumanMergeRequired {
		t.Fatalf("completion boundaries = %+v, want no PR/merge/deploy and human merge required", report)
	}

	finalReportPath := filepath.Join(odinRoot, "runs", "supervised-e2e", report.RunID, "final-report.json")
	preparedIssuePath := filepath.Join(odinRoot, "runs", "supervised-e2e", report.RunID, "prepared-issue.json")
	assertFileContains(t, finalReportPath, `"status": "prepared"`)
	assertFileContains(t, finalReportPath, `"prs": "not_created"`)
	assertFileContains(t, preparedIssuePath, `"planned_path": "docs/operations/stage-7-supervised-e2e-`)
	assertFileNotContains(t, finalReportPath, token)
	assertFileNotContains(t, preparedIssuePath, token)
	if strings.Contains(output.String(), token) {
		t.Fatalf("output leaked token:\n%s", output.String())
	}
}

func TestRunWorkSuperviseE2EPrepareIssueRedactsToken(t *testing.T) {
	ctx := context.Background()
	store := openWorkCommandStore(t)
	defer store.Close()
	odinRoot := t.TempDir()
	token := "github_pat_1234567890abcdefghijklmnopqrstuvwxyz"
	t.Setenv("ODIN_ROOT", odinRoot)
	t.Setenv("GITHUB_TOKEN", token)

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(response, `{"message":"token %s failed"}`, token)
	}))
	defer server.Close()
	t.Setenv("ODIN_GITHUB_API_BASE_URL", server.URL)

	var output strings.Builder
	err := RunWork(ctx, store, stage7E2EProjectRegistry(t), registry.Snapshot{}, []string{
		"supervise", "e2e", "prepare-issue", "--project", "odin-core", "--json",
	}, &output)
	if err == nil {
		t.Fatalf("RunWork(supervise e2e prepare-issue) error = nil, want GitHub error")
	}
	if strings.Contains(err.Error(), token) || strings.Contains(output.String(), token) {
		t.Fatalf("token leaked in error/output:\nerr=%v\noutput=%s", err, output.String())
	}
	if !strings.Contains(err.Error(), "[REDACTED]") {
		t.Fatalf("error = %q, want redaction marker", err.Error())
	}
	entries, err := os.ReadDir(filepath.Join(odinRoot, "runs", "supervised-e2e"))
	if err == nil && len(entries) != 0 {
		t.Fatalf("artifact run dirs = %d, want none after failed issue creation", len(entries))
	}
}

func TestRunWorkSuperviseE2EPrepareIssueRequiresODINRootBeforeGitHubPost(t *testing.T) {
	ctx := context.Background()
	store := openWorkCommandStore(t)
	defer store.Close()

	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests++
		fmt.Fprint(response, `{"number":321,"html_url":"https://github.example/marcusgoll/odin-os/issues/321","state":"open","labels":[]}`)
	}))
	defer server.Close()
	t.Setenv("ODIN_GITHUB_API_BASE_URL", server.URL)
	t.Setenv("GITHUB_TOKEN", "github_pat_1234567890abcdefghijklmnopqrstuvwxyz")

	var output strings.Builder
	err := RunWork(ctx, store, stage7E2EProjectRegistry(t), registry.Snapshot{}, []string{
		"supervise", "e2e", "prepare-issue", "--project", "odin-core", "--json",
	}, &output)
	if err == nil {
		t.Fatalf("RunWork(supervise e2e prepare-issue without ODIN_ROOT) error = nil, want explicit runtime root error\noutput:\n%s", output.String())
	}
	if !strings.Contains(err.Error(), "ODIN_ROOT is required for work supervise e2e prepare-issue") {
		t.Fatalf("error = %q, want ODIN_ROOT required error", err.Error())
	}
	if requests != 0 {
		t.Fatalf("GitHub requests = %d, want zero before artifact root is explicit", requests)
	}
}

func TestRunWorkSuperviseE2EPrepareIssuePreflightsArtifactsBeforeGitHubPost(t *testing.T) {
	ctx := context.Background()
	store := openWorkCommandStore(t)
	defer store.Close()

	odinRootFile := filepath.Join(t.TempDir(), "odin-root-file")
	if err := os.WriteFile(odinRootFile, []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("WriteFile(ODIN_ROOT file) error = %v", err)
	}
	t.Setenv("ODIN_ROOT", odinRootFile)
	t.Setenv("GITHUB_TOKEN", "github_pat_1234567890abcdefghijklmnopqrstuvwxyz")

	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests++
		fmt.Fprint(response, `{"number":321,"html_url":"https://github.example/marcusgoll/odin-os/issues/321","state":"open","labels":[]}`)
	}))
	defer server.Close()
	t.Setenv("ODIN_GITHUB_API_BASE_URL", server.URL)

	var output strings.Builder
	err := RunWork(ctx, store, stage7E2EProjectRegistry(t), registry.Snapshot{}, []string{
		"supervise", "e2e", "prepare-issue", "--project", "odin-core", "--json",
	}, &output)
	if err == nil {
		t.Fatalf("RunWork(supervise e2e prepare-issue with invalid ODIN_ROOT) error = nil, want artifact preflight error\noutput:\n%s", output.String())
	}
	if !strings.Contains(err.Error(), "preflight supervised e2e artifacts") {
		t.Fatalf("error = %q, want artifact preflight error", err.Error())
	}
	if requests != 0 {
		t.Fatalf("GitHub requests = %d, want zero before artifact preflight succeeds", requests)
	}
}

func TestRunWorkSuperviseE2ERunOnceRequiresExplicitIssue(t *testing.T) {
	ctx := context.Background()
	store := openWorkCommandStore(t)
	defer store.Close()

	err, output := runWorkSuperviseE2EForError(t, ctx, store, []string{"supervise", "e2e", "run-once", "--project", "alpha", "--json"})
	if err == nil {
		t.Fatalf("RunWork(supervise e2e run-once without --issue) error = nil, want issue error\noutput:\n%s", output)
	}
	if !strings.Contains(err.Error(), "missing --issue for work supervise e2e run-once") {
		t.Fatalf("error = %q, want missing issue error", err.Error())
	}

	err, output = runWorkSuperviseE2EForError(t, ctx, store, []string{"supervise", "e2e", "run-once", "--project", "alpha", "--issue", "42", "--json"})
	if err == nil {
		t.Fatalf("RunWork(supervise e2e run-once) error = nil, want not_implemented\noutput:\n%s", output)
	}
	if !strings.Contains(err.Error(), "not_implemented: work supervise e2e run-once") {
		t.Fatalf("error = %q, want not_implemented after validation", err.Error())
	}
	assertNoSuperviseSideEffects(t, ctx, store)
}

func runWorkSuperviseE2EForError(t *testing.T, ctx context.Context, store *sqlite.Store, args []string) (error, string) {
	t.Helper()

	var output strings.Builder
	err := RunWork(ctx, store, commandProjectRegistry(t), registry.Snapshot{}, args, &output)
	return err, output.String()
}

type superviseE2EPrepareIssueReport struct {
	Mode    string `json:"mode"`
	Phase   string `json:"phase"`
	Status  string `json:"status"`
	Project string `json:"project"`
	Repo    string `json:"repo"`
	RunID   string `json:"run_id"`
	Issue   struct {
		Number      int    `json:"number"`
		URL         string `json:"url"`
		PlannedPath string `json:"planned_path"`
	} `json:"issue"`
	PRs                string `json:"prs"`
	Merge              string `json:"merge"`
	Deployment         string `json:"deployment"`
	HumanMergeRequired bool   `json:"human_merge_required"`
}

func stage7E2EProjectRegistry(t *testing.T) projects.Registry {
	t.Helper()

	repoRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repoRoot, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir git root: %v", err)
	}
	return workerDryRunProjectRegistry(t, repoRoot)
}

func assertFileContains(t *testing.T, path string, want string) {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", path, err)
	}
	if !strings.Contains(string(content), want) {
		t.Fatalf("%s = %s, want %q", path, string(content), want)
	}
}

func assertFileNotContains(t *testing.T, path string, forbidden string) {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", path, err)
	}
	if strings.Contains(string(content), forbidden) {
		t.Fatalf("%s leaked forbidden value", path)
	}
}
