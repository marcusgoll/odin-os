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
	"strconv"
	"strings"
	"testing"
	"time"

	"odin-os/internal/core/projects"
	"odin-os/internal/registry"
	"odin-os/internal/runtime/supervision"
	"odin-os/internal/store/sqlite"
	"odin-os/internal/tracker"
	trackergithub "odin-os/internal/tracker/github"
	trackerintake "odin-os/internal/tracker/intake"
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

func TestFindStage6E2ERunRequiresOdinE2EWorkflow(t *testing.T) {
	_, ok := findStage6E2ERun([]trackergithub.WorkflowRun{
		{
			Name:       "ci",
			Path:       ".github/workflows/ci.yml",
			URL:        "https://github.example/actions/runs/1",
			Status:     "completed",
			Conclusion: "success",
		},
	})
	if ok {
		t.Fatalf("findStage6E2ERun accepted generic CI, want Odin E2E workflow only")
	}

	run, ok := findStage6E2ERun([]trackergithub.WorkflowRun{
		{
			Name:       "Odin E2E",
			Path:       ".github/workflows/odin-e2e.yml",
			URL:        "https://github.example/actions/runs/2",
			Status:     "completed",
			Conclusion: "success",
		},
	})
	if !ok || run.URL != "https://github.example/actions/runs/2" {
		t.Fatalf("findStage6E2ERun = %+v, %v; want Odin E2E run", run, ok)
	}
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
		t.Fatalf("RunWork(supervise e2e run-once) error = nil, want ODIN_ROOT error\noutput:\n%s", output)
	}
	if !strings.Contains(err.Error(), "ODIN_ROOT is required for work supervise e2e run-once") {
		t.Fatalf("error = %q, want ODIN_ROOT required after validation", err.Error())
	}
	assertNoSuperviseSideEffects(t, ctx, store)
}

func TestRunWorkSuperviseE2ERunOnceQueuesExactIssueAndClaims(t *testing.T) {
	ctx := context.Background()
	store := openWorkCommandStore(t)
	defer store.Close()
	odinRoot := t.TempDir()
	repoRoot := initWorkerDryRunGitRepo(t)
	initStage6Remote(t, repoRoot)
	t.Setenv("ODIN_ROOT", odinRoot)
	t.Setenv("GITHUB_TOKEN", "github_pat_1234567890abcdefghijklmnopqrstuvwxyz")

	plannedPath := "docs/operations/stage-7-task-3.md"
	fake := &superviseE2EFakeTracker{
		issue: tracker.Issue{
			Provider: "github",
			Repo:     "acme/alpha",
			Number:   42,
			Title:    "Supervised E2E exact issue",
			Body:     "Planned scope: " + plannedPath,
			URL:      "https://github.example/acme/alpha/issues/42",
			State:    "open",
			Labels:   []string{"odin:ready", "safety:low-risk"},
		},
	}
	installSuperviseE2EFakeTracker(t, fake)
	workerCalls := 0
	installSuperviseE2EFakeWorker(t, func(_ context.Context, request supervisedE2EWorkerRequest) (supervisedE2EWorkerResult, error) {
		workerCalls++
		path := filepath.Join(request.WorktreePath, filepath.FromSlash(plannedPath))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll(planned dir) error = %v", err)
		}
		if err := os.WriteFile(path, []byte("# Stage 7 task 3\n\nRun make odin-e2e-local before handoff.\n"), 0o644); err != nil {
			t.Fatalf("WriteFile(planned path) error = %v", err)
		}
		return supervisedE2EWorkerResult{Output: "worker complete: make odin-e2e-local"}, nil
	})
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatalf("ReadAll() error = %v", err)
		}
		requests = append(requests, request.Method+" "+request.URL.Path+"?"+request.URL.RawQuery+" "+string(body))
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/repos/acme/alpha/pulls":
			fmt.Fprint(response, `[]`)
		case request.Method == http.MethodPost && request.URL.Path == "/repos/acme/alpha/pulls":
			if !strings.Contains(string(body), "odin work supervise e2e run-once") {
				t.Fatalf("create PR body = %s, want Stage 7 supervised run-once provenance", string(body))
			}
			fmt.Fprint(response, `{"number":76,"html_url":"https://github.example/acme/alpha/pull/76","state":"open","draft":true,"title":"Stage 7 supervised E2E handoff","head":{"ref":"stage7"},"base":{"ref":"main"}}`)
		case request.Method == http.MethodGet && request.URL.Path == "/repos/acme/alpha/issues/76/comments":
			fmt.Fprint(response, `[]`)
		case request.Method == http.MethodPost && request.URL.Path == "/repos/acme/alpha/issues/76/comments":
			if !strings.Contains(string(body), "odin-stage7-supervised-e2e") || !strings.Contains(string(body), "odin work supervise e2e run-once") {
				t.Fatalf("comment body = %s, want Stage 7 supervised run-once evidence", string(body))
			}
			fmt.Fprint(response, `{"html_url":"https://github.example/acme/alpha/pull/76#issuecomment-1","body":"ok"}`)
		case request.Method == http.MethodGet && request.URL.Path == "/repos/acme/alpha/actions/runs":
			fmt.Fprint(response, `{"workflow_runs":[{"id":10,"name":"Odin E2E","path":".github/workflows/odin-e2e.yml","html_url":"https://github.example/acme/alpha/actions/runs/10","status":"completed","conclusion":"success","head_branch":"stage7","event":"pull_request"}]}`)
		default:
			t.Fatalf("unexpected request: %s %s?%s body=%s", request.Method, request.URL.Path, request.URL.RawQuery, string(body))
		}
	}))
	defer server.Close()
	t.Setenv("ODIN_GITHUB_API_BASE_URL", server.URL)

	_ = runWorkSuperviseJSON(t, ctx, store, []string{"supervise", "start", "--json"})
	var output strings.Builder
	if err := RunWork(ctx, store, superviseE2EProjectRegistry(t, repoRoot), registry.Snapshot{}, []string{
		"supervise", "e2e", "run-once", "--project", "alpha", "--issue", "42", "--json",
	}, &output); err != nil {
		t.Fatalf("RunWork(supervise e2e run-once) error = %v\noutput:\n%s", err, output.String())
	}
	if fake.fetchByIDCalls != 1 {
		t.Fatalf("FetchIssueByID calls = %d, want 1", fake.fetchByIDCalls)
	}
	if fake.fetchEligibleCalls != 0 {
		t.Fatalf("FetchEligibleIssues calls = %d, want 0", fake.fetchEligibleCalls)
	}
	if fake.lastID != (tracker.IssueID{Provider: "github", Repo: "acme/alpha", Number: 42}) {
		t.Fatalf("FetchIssueByID id = %+v, want github acme/alpha #42", fake.lastID)
	}

	report := decodeSuperviseE2ERunOnceReport(t, output.String())
	if report.Phase != "review_handoff" || report.Status != "passed" || report.Project != "alpha" || report.Repo != "acme/alpha" {
		t.Fatalf("report = %+v, want review_handoff/passed alpha acme/alpha", report)
	}
	if report.Issue.Number != 42 || report.Issue.PlannedPath != plannedPath {
		t.Fatalf("issue = %+v, want exact issue and planned docs/operations path", report.Issue)
	}
	if len(report.Queue) != 1 {
		t.Fatalf("queue len = %d, want 1: %+v", len(report.Queue), report.Queue)
	}
	if report.Queue[0].Decision != supervision.DecisionEligible || !report.Queue[0].Eligible || report.Queue[0].ClaimKey == "" {
		t.Fatalf("queue decision = %+v, want eligible with claim key", report.Queue[0])
	}
	if len(report.Claims) != 1 || report.Claims[0].IssueNumber != 42 || report.Claims[0].Status != supervision.ClaimStatusReserved {
		t.Fatalf("claims = %+v, want one reserved claim for issue 42", report.Claims)
	}
	if report.CodexExecution != "completed" ||
		report.PRs != "draft_created" ||
		report.Merge != supervision.SideEffectNotMerged ||
		report.Deployment != supervision.SideEffectNotStarted ||
		report.Dispatch != supervision.SideEffectNotStarted ||
		!report.HumanMergeRequired {
		t.Fatalf("side effects = %+v, want draft PR handoff and no merge/deploy/dispatch", report)
	}

	project, err := store.GetProjectByKey(ctx, "alpha")
	if err != nil {
		t.Fatalf("GetProjectByKey(alpha) error = %v", err)
	}
	decisions, err := store.ListSupervisionQueueDecisions(ctx, sqlite.ListSupervisionQueueDecisionsParams{
		ProjectID: &project.ID,
		Repo:      "acme/alpha",
	})
	if err != nil {
		t.Fatalf("ListSupervisionQueueDecisions() error = %v", err)
	}
	if len(decisions) != 1 || decisions[0].IssueNumber != 42 || decisions[0].Decision != supervision.DecisionEligible {
		t.Fatalf("persisted decisions = %+v, want one eligible decision for issue 42", decisions)
	}
	claims, err := store.ListSupervisionDispatchClaims(ctx, sqlite.ListSupervisionDispatchClaimsParams{
		ProjectID: &project.ID,
		Repo:      "acme/alpha",
	})
	if err != nil {
		t.Fatalf("ListSupervisionDispatchClaims() error = %v", err)
	}
	if len(claims) != 1 || claims[0].IssueNumber != 42 || claims[0].Status != supervision.ClaimStatusReserved {
		t.Fatalf("persisted claims = %+v, want one reserved claim for issue 42", claims)
	}

	queueReportPath := filepath.Join(odinRoot, "runs", "supervised-e2e", report.RunID, "queue-report.json")
	finalReportPath := filepath.Join(odinRoot, "runs", "supervised-e2e", report.RunID, "final-report.json")
	assertFileContains(t, queueReportPath, `"decision": "eligible"`)
	assertFileContains(t, finalReportPath, `"status": "passed"`)
	assertFileContains(t, finalReportPath, `"phase": "review_handoff"`)
	assertNoForbiddenSuperviseE2EGitHubMutations(t, requests)
	assertNoSuperviseSideEffects(t, ctx, store)
}

func TestRunWorkSuperviseE2ERunOnceKillSwitchBlocksWorker(t *testing.T) {
	ctx := context.Background()
	store := openWorkCommandStore(t)
	defer store.Close()
	odinRoot := t.TempDir()
	t.Setenv("ODIN_ROOT", odinRoot)

	fake := &superviseE2EFakeTracker{
		issue: tracker.Issue{
			Provider: "github",
			Repo:     "acme/alpha",
			Number:   43,
			Title:    "Kill switch exact issue",
			Body:     "Planned scope: docs/operations/stage-7-kill-switch.md",
			URL:      "https://github.example/acme/alpha/issues/43",
			State:    "open",
			Labels:   []string{"odin:ready", "safety:low-risk"},
		},
	}
	installSuperviseE2EFakeTracker(t, fake)

	var output strings.Builder
	err := RunWork(ctx, store, commandProjectRegistry(t), registry.Snapshot{}, []string{
		"supervise", "e2e", "run-once", "--project", "alpha", "--issue", "43", "--json",
	}, &output)
	if err == nil {
		t.Fatalf("RunWork(supervise e2e run-once with kill switch) error = nil, want refused failure\noutput:\n%s", output.String())
	}
	if !strings.Contains(err.Error(), supervision.RefusalKillSwitchActive) {
		t.Fatalf("error = %q, want kill switch refusal", err.Error())
	}
	if fake.fetchByIDCalls != 1 || fake.fetchEligibleCalls != 0 {
		t.Fatalf("tracker calls = byID %d eligible %d, want exact issue fetch only", fake.fetchByIDCalls, fake.fetchEligibleCalls)
	}

	runID := newestSuperviseE2ERunID(t, odinRoot)
	queueReportPath := filepath.Join(odinRoot, "runs", "supervised-e2e", runID, "queue-report.json")
	finalReportPath := filepath.Join(odinRoot, "runs", "supervised-e2e", runID, "final-report.json")
	assertFileContains(t, queueReportPath, `"decision": "refused"`)
	assertFileContains(t, queueReportPath, `"refusal_reason": "kill_switch_active"`)
	assertFileContains(t, finalReportPath, `"status": "refused"`)
	assertFileContains(t, finalReportPath, `"codex_execution": "not_started"`)

	project, err := store.GetProjectByKey(ctx, "alpha")
	if err != nil {
		t.Fatalf("GetProjectByKey(alpha) error = %v", err)
	}
	claims, err := store.ListSupervisionDispatchClaims(ctx, sqlite.ListSupervisionDispatchClaimsParams{
		ProjectID: &project.ID,
		Repo:      "acme/alpha",
	})
	if err != nil {
		t.Fatalf("ListSupervisionDispatchClaims() error = %v", err)
	}
	if len(claims) != 0 {
		t.Fatalf("claims = %+v, want none while kill switch is active", claims)
	}
	assertNoSuperviseSideEffects(t, ctx, store)
}

func TestRunWorkSuperviseE2ERunOnceMismatchedIssueFailsClosed(t *testing.T) {
	ctx := context.Background()
	store := openWorkCommandStore(t)
	defer store.Close()
	odinRoot := t.TempDir()
	t.Setenv("ODIN_ROOT", odinRoot)

	fake := &superviseE2EFakeTracker{
		issue: tracker.Issue{
			Provider: "github",
			Repo:     "acme/alpha",
			Number:   44,
			Title:    "Mismatched issue",
			Body:     "Planned scope: docs/operations/stage-7-mismatch.md",
			URL:      "https://github.example/acme/alpha/issues/44",
			State:    "open",
			Labels:   []string{"odin:ready", "safety:low-risk"},
		},
	}
	installSuperviseE2EFakeTracker(t, fake)

	_ = runWorkSuperviseJSON(t, ctx, store, []string{"supervise", "start", "--json"})
	var output strings.Builder
	err := RunWork(ctx, store, commandProjectRegistry(t), registry.Snapshot{}, []string{
		"supervise", "e2e", "run-once", "--project", "alpha", "--issue", "45", "--json",
	}, &output)
	if err == nil {
		t.Fatalf("RunWork(supervise e2e run-once mismatched issue) error = nil, want fail-closed mismatch\noutput:\n%s", output.String())
	}
	if !strings.Contains(err.Error(), "fetched issue number 44 differs from requested --issue 45") {
		t.Fatalf("error = %q, want mismatched issue failure", err.Error())
	}
	if fake.fetchByIDCalls != 1 || fake.fetchEligibleCalls != 0 {
		t.Fatalf("tracker calls = byID %d eligible %d, want exact issue fetch only", fake.fetchByIDCalls, fake.fetchEligibleCalls)
	}
	assertSuperviseTableCount(t, ctx, store, "supervision_queue_decisions", 0)
	assertSuperviseTableCount(t, ctx, store, "supervision_dispatch_claims", 0)

	runID := newestSuperviseE2ERunID(t, odinRoot)
	finalReportPath := filepath.Join(odinRoot, "runs", "supervised-e2e", runID, "final-report.json")
	assertFileContains(t, finalReportPath, `"status": "failed_closed"`)
	assertFileContains(t, finalReportPath, `"requested_issue": 45`)
	assertFileContains(t, finalReportPath, `"fetched_issue": 44`)
	assertNoSuperviseSideEffects(t, ctx, store)
}

func TestRunWorkSuperviseE2ERunOnceRejectsNonOperationsPathBeforeQueue(t *testing.T) {
	ctx := context.Background()
	store := openWorkCommandStore(t)
	defer store.Close()
	odinRoot := t.TempDir()
	t.Setenv("ODIN_ROOT", odinRoot)

	fake := &superviseE2EFakeTracker{
		issue: tracker.Issue{
			Provider: "github",
			Repo:     "acme/alpha",
			Number:   47,
			Title:    "Wrong docs path",
			Body:     "Planned scope: docs/not-operations.md",
			URL:      "https://github.example/acme/alpha/issues/47",
			State:    "open",
			Labels:   []string{"odin:ready", "safety:low-risk"},
		},
	}
	installSuperviseE2EFakeTracker(t, fake)

	_ = runWorkSuperviseJSON(t, ctx, store, []string{"supervise", "start", "--json"})
	var output strings.Builder
	err := RunWork(ctx, store, commandProjectRegistry(t), registry.Snapshot{}, []string{
		"supervise", "e2e", "run-once", "--project", "alpha", "--issue", "47", "--json",
	}, &output)
	if err == nil {
		t.Fatalf("RunWork(supervise e2e run-once with non-operations path) error = nil, want fail-closed path gate\noutput:\n%s", output.String())
	}
	if !strings.Contains(err.Error(), "requires Planned scope under docs/operations/") {
		t.Fatalf("error = %q, want docs/operations path gate", err.Error())
	}
	assertSuperviseTableCount(t, ctx, store, "supervision_queue_decisions", 0)
	assertSuperviseTableCount(t, ctx, store, "supervision_dispatch_claims", 0)

	runID := newestSuperviseE2ERunID(t, odinRoot)
	finalReportPath := filepath.Join(odinRoot, "runs", "supervised-e2e", runID, "final-report.json")
	assertFileContains(t, finalReportPath, `"status": "failed_closed"`)
	assertFileContains(t, finalReportPath, `"changed_paths": [`)
	assertFileContains(t, finalReportPath, `"docs/not-operations.md"`)
	assertNoSuperviseSideEffects(t, ctx, store)
}

func TestRunWorkSuperviseE2ERunOnceRejectsEscapedOperationsPathBeforeQueue(t *testing.T) {
	ctx := context.Background()
	store := openWorkCommandStore(t)
	defer store.Close()
	odinRoot := t.TempDir()
	t.Setenv("ODIN_ROOT", odinRoot)

	fake := &superviseE2EFakeTracker{
		issue: tracker.Issue{
			Provider: "github",
			Repo:     "acme/alpha",
			Number:   48,
			Title:    "Escaped docs path",
			Body:     "Planned scope: docs/operations/../stage-7-escaped.md",
			URL:      "https://github.example/acme/alpha/issues/48",
			State:    "open",
			Labels:   []string{"odin:ready", "safety:low-risk"},
		},
	}
	installSuperviseE2EFakeTracker(t, fake)

	_ = runWorkSuperviseJSON(t, ctx, store, []string{"supervise", "start", "--json"})
	var output strings.Builder
	err := RunWork(ctx, store, commandProjectRegistry(t), registry.Snapshot{}, []string{
		"supervise", "e2e", "run-once", "--project", "alpha", "--issue", "48", "--json",
	}, &output)
	if err == nil {
		t.Fatalf("RunWork(supervise e2e run-once with escaped operations path) error = nil, want fail-closed path gate\noutput:\n%s", output.String())
	}
	if !strings.Contains(err.Error(), "requires Planned scope under docs/operations/") {
		t.Fatalf("error = %q, want docs/operations path gate", err.Error())
	}
	assertSuperviseTableCount(t, ctx, store, "supervision_queue_decisions", 0)
	assertSuperviseTableCount(t, ctx, store, "supervision_dispatch_claims", 0)

	runID := newestSuperviseE2ERunID(t, odinRoot)
	finalReportPath := filepath.Join(odinRoot, "runs", "supervised-e2e", runID, "final-report.json")
	assertFileContains(t, finalReportPath, `"status": "failed_closed"`)
	assertFileContains(t, finalReportPath, `"docs/operations/../stage-7-escaped.md"`)
	assertNoSuperviseSideEffects(t, ctx, store)
}

func TestRunWorkSuperviseE2ERunOnceRejectsWhitespacePlannedPathBeforeQueue(t *testing.T) {
	ctx := context.Background()
	store := openWorkCommandStore(t)
	defer store.Close()
	odinRoot := t.TempDir()
	t.Setenv("ODIN_ROOT", odinRoot)

	fake := &superviseE2EFakeTracker{
		issue: tracker.Issue{
			Provider: "github",
			Repo:     "acme/alpha",
			Number:   55,
			Title:    "Whitespace docs path",
			Body:     "Planned scope: docs/operations/stage 7 whitespace.md",
			URL:      "https://github.example/acme/alpha/issues/55",
			State:    "open",
			Labels:   []string{"odin:ready", "safety:low-risk"},
		},
	}
	installSuperviseE2EFakeTracker(t, fake)

	_ = runWorkSuperviseJSON(t, ctx, store, []string{"supervise", "start", "--json"})
	var output strings.Builder
	err := RunWork(ctx, store, commandProjectRegistry(t), registry.Snapshot{}, []string{
		"supervise", "e2e", "run-once", "--project", "alpha", "--issue", "55", "--json",
	}, &output)
	if err == nil {
		t.Fatalf("RunWork(supervise e2e run-once with whitespace planned path) error = nil, want fail-closed path gate\noutput:\n%s", output.String())
	}
	if !strings.Contains(err.Error(), "requires exactly one raw Planned scope token") {
		t.Fatalf("error = %q, want raw path token gate before queue", err.Error())
	}
	assertSuperviseTableCount(t, ctx, store, "supervision_queue_decisions", 0)
	assertSuperviseTableCount(t, ctx, store, "supervision_dispatch_claims", 0)

	runID := newestSuperviseE2ERunID(t, odinRoot)
	finalReportPath := filepath.Join(odinRoot, "runs", "supervised-e2e", runID, "final-report.json")
	assertFileContains(t, finalReportPath, `"status": "failed_closed"`)
	assertNoSuperviseSideEffects(t, ctx, store)
}

func TestRunWorkSuperviseE2ERunOnceRejectsClosedBlockedAndPullRequestBeforeQueue(t *testing.T) {
	cases := []struct {
		name        string
		issue       tracker.Issue
		wantMessage string
	}{
		{
			name: "closed",
			issue: tracker.Issue{
				Provider: "github",
				Repo:     "acme/alpha",
				Number:   49,
				Title:    "Closed issue",
				Body:     "Planned scope: docs/operations/stage-7-closed.md",
				URL:      "https://github.example/acme/alpha/issues/49",
				State:    "closed",
				Labels:   []string{"odin:ready", "safety:low-risk"},
			},
			wantMessage: "requires an open issue",
		},
		{
			name: "blocked",
			issue: tracker.Issue{
				Provider: "github",
				Repo:     "acme/alpha",
				Number:   50,
				Title:    "Blocked issue",
				Body:     "Planned scope: docs/operations/stage-7-blocked.md",
				URL:      "https://github.example/acme/alpha/issues/50",
				State:    "open",
				Labels:   []string{"odin:ready", "safety:low-risk", "odin:blocked"},
			},
			wantMessage: "refuses blocked issue",
		},
		{
			name: "pull-request",
			issue: tracker.Issue{
				Provider:    "github",
				Repo:        "acme/alpha",
				Number:      51,
				Title:       "Pull request",
				Body:        "Planned scope: docs/operations/stage-7-pr.md",
				URL:         "https://github.example/acme/alpha/pull/51",
				State:       "open",
				Labels:      []string{"odin:ready", "safety:low-risk"},
				PullRequest: true,
			},
			wantMessage: "not a pull request",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			store := openWorkCommandStore(t)
			defer store.Close()
			odinRoot := t.TempDir()
			t.Setenv("ODIN_ROOT", odinRoot)

			fake := &superviseE2EFakeTracker{issue: tc.issue}
			installSuperviseE2EFakeTracker(t, fake)

			_ = runWorkSuperviseJSON(t, ctx, store, []string{"supervise", "start", "--json"})
			var output strings.Builder
			err := RunWork(ctx, store, commandProjectRegistry(t), registry.Snapshot{}, []string{
				"supervise", "e2e", "run-once", "--project", "alpha", "--issue", strconv.Itoa(tc.issue.Number), "--json",
			}, &output)
			if err == nil {
				t.Fatalf("RunWork(supervise e2e run-once %s) error = nil, want fail-closed issue gate\noutput:\n%s", tc.name, output.String())
			}
			if !strings.Contains(err.Error(), tc.wantMessage) {
				t.Fatalf("error = %q, want %q", err.Error(), tc.wantMessage)
			}
			assertSuperviseTableCount(t, ctx, store, "supervision_queue_decisions", 0)
			assertSuperviseTableCount(t, ctx, store, "supervision_dispatch_claims", 0)

			runID := newestSuperviseE2ERunID(t, odinRoot)
			finalReportPath := filepath.Join(odinRoot, "runs", "supervised-e2e", runID, "final-report.json")
			assertFileContains(t, finalReportPath, `"status": "failed_closed"`)
			assertNoSuperviseSideEffects(t, ctx, store)
		})
	}
}

func TestRunWorkSuperviseE2ERunOnceDuplicateActiveClaimPreservesExistingClaim(t *testing.T) {
	ctx := context.Background()
	store := openWorkCommandStore(t)
	defer store.Close()
	odinRoot := t.TempDir()
	repoRoot := initWorkerDryRunGitRepo(t)
	initStage6Remote(t, repoRoot)
	t.Setenv("ODIN_ROOT", odinRoot)
	t.Setenv("GITHUB_TOKEN", "github_pat_1234567890abcdefghijklmnopqrstuvwxyz")

	plannedPath := "docs/operations/stage-7-idempotent.md"
	fake := &superviseE2EFakeTracker{
		issue: tracker.Issue{
			Provider: "github",
			Repo:     "acme/alpha",
			Number:   46,
			Title:    "Duplicate exact issue",
			Body:     "Planned scope: " + plannedPath,
			URL:      "https://github.example/acme/alpha/issues/46",
			State:    "open",
			Labels:   []string{"odin:ready", "safety:low-risk"},
		},
	}
	installSuperviseE2EFakeTracker(t, fake)
	workerCalls := 0
	installSuperviseE2EFakeWorker(t, func(_ context.Context, request supervisedE2EWorkerRequest) (supervisedE2EWorkerResult, error) {
		workerCalls++
		path := filepath.Join(request.WorktreePath, filepath.FromSlash(plannedPath))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll(planned dir) error = %v", err)
		}
		if err := os.WriteFile(path, []byte("# Stage 7 idempotent\n\nRun make odin-e2e-local before handoff.\n"), 0o644); err != nil {
			t.Fatalf("WriteFile(planned path) error = %v", err)
		}
		return supervisedE2EWorkerResult{Output: "worker complete: make odin-e2e-local"}, nil
	})
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatalf("ReadAll() error = %v", err)
		}
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/repos/acme/alpha/pulls":
			fmt.Fprint(response, `[]`)
		case request.Method == http.MethodPost && request.URL.Path == "/repos/acme/alpha/pulls":
			fmt.Fprint(response, `{"number":76,"html_url":"https://github.example/acme/alpha/pull/76","state":"open","draft":true,"title":"Stage 7 supervised E2E handoff","head":{"ref":"stage7"},"base":{"ref":"main"}}`)
		case request.Method == http.MethodGet && request.URL.Path == "/repos/acme/alpha/issues/76/comments":
			fmt.Fprint(response, `[]`)
		case request.Method == http.MethodPost && request.URL.Path == "/repos/acme/alpha/issues/76/comments":
			fmt.Fprint(response, `{"html_url":"https://github.example/acme/alpha/pull/76#issuecomment-1","body":"ok"}`)
		case request.Method == http.MethodGet && request.URL.Path == "/repos/acme/alpha/actions/runs":
			fmt.Fprint(response, `{"workflow_runs":[{"id":10,"name":"Odin E2E","path":".github/workflows/odin-e2e.yml","html_url":"https://github.example/acme/alpha/actions/runs/10","status":"completed","conclusion":"success","head_branch":"stage7","event":"pull_request"}]}`)
		default:
			t.Fatalf("unexpected request: %s %s?%s body=%s", request.Method, request.URL.Path, request.URL.RawQuery, string(body))
		}
	}))
	defer server.Close()
	t.Setenv("ODIN_GITHUB_API_BASE_URL", server.URL)

	_ = runWorkSuperviseJSON(t, ctx, store, []string{"supervise", "start", "--json"})
	firstOutput := runWorkSuperviseE2ERunOnceOutputWithRegistry(t, ctx, store, superviseE2EProjectRegistry(t, repoRoot), []string{
		"supervise", "e2e", "run-once", "--project", "alpha", "--issue", "46", "--json",
	})
	var secondOutput strings.Builder
	secondErr := RunWork(ctx, store, superviseE2EProjectRegistry(t, repoRoot), registry.Snapshot{}, []string{
		"supervise", "e2e", "run-once", "--project", "alpha", "--issue", "46", "--json",
	}, &secondOutput)
	if secondErr == nil {
		t.Fatalf("second RunWork(supervise e2e run-once) error = nil, want existing-claim refusal before worker\noutput:\n%s", secondOutput.String())
	}
	if !strings.Contains(secondErr.Error(), "preserved existing claim before worker behavior") {
		t.Fatalf("second error = %q, want existing-claim refusal before worker", secondErr.Error())
	}
	if workerCalls != 1 {
		t.Fatalf("worker calls = %d, want exactly one launch across duplicate run-once calls", workerCalls)
	}
	first := decodeSuperviseE2ERunOnceReport(t, firstOutput)
	secondRunID := newestSuperviseE2ERunID(t, odinRoot)
	secondFinalReportPath := filepath.Join(odinRoot, "runs", "supervised-e2e", secondRunID, "final-report.json")
	assertFileContains(t, secondFinalReportPath, `"status": "claim_exists"`)
	assertFileContains(t, secondFinalReportPath, `"codex_execution": "not_started"`)
	if len(first.Claims) != 1 {
		t.Fatalf("claims = first %+v, want one claim reported by first run", first.Claims)
	}
	project, err := store.GetProjectByKey(ctx, "alpha")
	if err != nil {
		t.Fatalf("GetProjectByKey(alpha) error = %v", err)
	}
	claims, err := store.ListSupervisionDispatchClaims(ctx, sqlite.ListSupervisionDispatchClaimsParams{
		ProjectID: &project.ID,
		Repo:      "acme/alpha",
	})
	if err != nil {
		t.Fatalf("ListSupervisionDispatchClaims() error = %v", err)
	}
	if len(claims) != 1 || claims[0].ClaimKey != first.Claims[0].ClaimKey {
		t.Fatalf("persisted claims = %+v, want one preserved claim", claims)
	}
	assertNoSuperviseSideEffects(t, ctx, store)
}

func TestRunWorkSuperviseE2ERunOncePreservesPreexistingActiveClaim(t *testing.T) {
	ctx := context.Background()
	store := openWorkCommandStore(t)
	defer store.Close()
	odinRoot := t.TempDir()
	repoRoot := initWorkerDryRunGitRepo(t)
	t.Setenv("ODIN_ROOT", odinRoot)

	projectRegistry := superviseE2EProjectRegistry(t, repoRoot)
	manifest, ok := projectRegistry.Lookup("alpha")
	if !ok {
		t.Fatalf("Lookup(alpha) = false")
	}
	project, err := ensureWorkSuperviseProject(ctx, store, manifest)
	if err != nil {
		t.Fatalf("ensureWorkSuperviseProject() error = %v", err)
	}
	if _, err := store.UpsertSupervisionDispatchClaim(ctx, sqlite.UpsertSupervisionDispatchClaimParams{
		ProjectID:   project.ID,
		Repo:        "acme/alpha",
		IssueNumber: 47,
		ClaimKey:    supervision.ModeKeyStage7SupervisedAgency + ":alpha:47",
		Status:      supervision.ClaimStatusActive,
		ConfigHash:  "sha256:active",
		ClaimedBy:   "supervision-service",
	}); err != nil {
		t.Fatalf("UpsertSupervisionDispatchClaim(active) error = %v", err)
	}

	plannedPath := "docs/operations/stage-7-active-claim.md"
	fake := &superviseE2EFakeTracker{
		issue: tracker.Issue{
			Provider: "github",
			Repo:     "acme/alpha",
			Number:   47,
			Title:    "Active exact issue",
			Body:     "Planned scope: " + plannedPath,
			URL:      "https://github.example/acme/alpha/issues/47",
			State:    "open",
			Labels:   []string{"odin:ready", "safety:low-risk"},
		},
	}
	installSuperviseE2EFakeTracker(t, fake)
	workerCalls := 0
	installSuperviseE2EFakeWorker(t, func(context.Context, supervisedE2EWorkerRequest) (supervisedE2EWorkerResult, error) {
		workerCalls++
		return supervisedE2EWorkerResult{}, fmt.Errorf("worker should not launch for preexisting active claim")
	})

	_ = runWorkSuperviseJSON(t, ctx, store, []string{"supervise", "start", "--json"})
	var output strings.Builder
	err = RunWork(ctx, store, projectRegistry, registry.Snapshot{}, []string{
		"supervise", "e2e", "run-once", "--project", "alpha", "--issue", "47", "--json",
	}, &output)
	if err == nil {
		t.Fatalf("RunWork(supervise e2e run-once) error = nil, want existing active claim refusal\noutput:\n%s", output.String())
	}
	if !strings.Contains(err.Error(), "preserved existing claim before worker behavior") {
		t.Fatalf("error = %q, want existing-claim refusal before worker", err.Error())
	}
	if workerCalls != 0 {
		t.Fatalf("worker calls = %d, want no launch for preexisting active claim", workerCalls)
	}

	runID := newestSuperviseE2ERunID(t, odinRoot)
	finalReportPath := filepath.Join(odinRoot, "runs", "supervised-e2e", runID, "final-report.json")
	assertFileContains(t, finalReportPath, `"status": "claim_exists"`)
	assertFileContains(t, finalReportPath, `"codex_execution": "not_started"`)
	assertFileContains(t, finalReportPath, `"status": "active"`)

	claims, err := store.ListSupervisionDispatchClaims(ctx, sqlite.ListSupervisionDispatchClaimsParams{
		ProjectID: &project.ID,
		Repo:      "acme/alpha",
	})
	if err != nil {
		t.Fatalf("ListSupervisionDispatchClaims() error = %v", err)
	}
	if len(claims) != 1 || claims[0].Status != supervision.ClaimStatusActive {
		t.Fatalf("persisted claims = %+v, want one active claim preserved", claims)
	}
	assertNoSuperviseSideEffects(t, ctx, store)
}

func TestRunWorkSuperviseE2ERunOnceWorkerEditsOnlyPlannedPath(t *testing.T) {
	ctx := context.Background()
	store := openWorkCommandStore(t)
	defer store.Close()
	odinRoot := t.TempDir()
	worktreeRoot := filepath.Join(t.TempDir(), "worktrees")
	repoRoot := initWorkerDryRunGitRepo(t)
	initStage6Remote(t, repoRoot)
	t.Setenv("ODIN_ROOT", odinRoot)
	t.Setenv("ODIN_WORKTREE_ROOT", worktreeRoot)
	t.Setenv("GITHUB_TOKEN", "github_pat_1234567890abcdefghijklmnopqrstuvwxyz")

	plannedPath := "docs/operations/stage-7-worker-success.md"
	fake := &superviseE2EFakeTracker{
		issue: tracker.Issue{
			Provider: "github",
			Repo:     "acme/alpha",
			Number:   52,
			Title:    "Worker exact diff",
			Body:     "Planned scope: " + plannedPath,
			URL:      "https://github.example/acme/alpha/issues/52",
			State:    "open",
			Labels:   []string{"odin:ready", "safety:low-risk"},
		},
	}
	installSuperviseE2EFakeTracker(t, fake)
	installSuperviseE2EFakeWorker(t, func(_ context.Context, request supervisedE2EWorkerRequest) (supervisedE2EWorkerResult, error) {
		if !strings.Contains(request.Prompt, "Audit the existing repo before editing.") ||
			!strings.Contains(request.Prompt, "Only edit this exact planned path: "+plannedPath) ||
			!strings.Contains(request.Prompt, "Do not change runner, security, workspace, token, deploy, CI, scheduler, PR, or merge behavior.") ||
			!strings.Contains(request.Prompt, "make odin-e2e-local") {
			t.Fatalf("worker prompt missing required guardrails:\n%s", request.Prompt)
		}
		if request.Command.Path != "codex" || !containsString(request.Command.Args, "exec") || !containsString(request.Command.Args, "workspace-write") {
			t.Fatalf("command = %+v, want codex exec workspace-write", request.Command)
		}
		path := filepath.Join(request.WorktreePath, filepath.FromSlash(plannedPath))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll(planned dir) error = %v", err)
		}
		if err := os.WriteFile(path, []byte("# Stage 7 worker success\n\nRun make odin-e2e-local before handoff.\n"), 0o644); err != nil {
			t.Fatalf("WriteFile(planned path) error = %v", err)
		}
		return supervisedE2EWorkerResult{Output: "worker complete: make odin-e2e-local"}, nil
	})
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatalf("ReadAll() error = %v", err)
		}
		requests = append(requests, request.Method+" "+request.URL.Path+"?"+request.URL.RawQuery+" "+string(body))
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/repos/acme/alpha/pulls":
			fmt.Fprint(response, `[]`)
		case request.Method == http.MethodPost && request.URL.Path == "/repos/acme/alpha/pulls":
			if !strings.Contains(string(body), `"draft":true`) || !strings.Contains(string(body), `"base":"main"`) || !strings.Contains(string(body), "odin work supervise e2e run-once") {
				t.Fatalf("create PR body = %s, want draft PR against main with Stage 7 provenance", string(body))
			}
			fmt.Fprint(response, `{"number":77,"html_url":"https://github.example/acme/alpha/pull/77","state":"open","draft":true,"title":"Stage 7 supervised E2E handoff","head":{"ref":"stage7"},"base":{"ref":"main"}}`)
		case request.Method == http.MethodGet && request.URL.Path == "/repos/acme/alpha/issues/77/comments":
			fmt.Fprint(response, `[]`)
		case request.Method == http.MethodPost && request.URL.Path == "/repos/acme/alpha/issues/77/comments":
			if !strings.Contains(string(body), "odin-stage7-supervised-e2e") || !strings.Contains(string(body), "odin work supervise e2e run-once") || !strings.Contains(string(body), "human") {
				t.Fatalf("comment body = %s, want Stage 7 evidence marker and human handoff", string(body))
			}
			fmt.Fprint(response, `{"html_url":"https://github.example/acme/alpha/pull/77#issuecomment-1","body":"ok"}`)
		case request.Method == http.MethodGet && request.URL.Path == "/repos/acme/alpha/actions/runs":
			fmt.Fprint(response, `{"workflow_runs":[{"id":10,"name":"Odin E2E","path":".github/workflows/odin-e2e.yml","html_url":"https://github.example/acme/alpha/actions/runs/10","status":"completed","conclusion":"success","head_branch":"stage7","event":"pull_request"}]}`)
		default:
			t.Fatalf("unexpected request: %s %s?%s body=%s", request.Method, request.URL.Path, request.URL.RawQuery, string(body))
		}
	}))
	defer server.Close()
	t.Setenv("ODIN_GITHUB_API_BASE_URL", server.URL)

	_ = runWorkSuperviseJSON(t, ctx, store, []string{"supervise", "start", "--json"})
	var output strings.Builder
	if err := RunWork(ctx, store, superviseE2EProjectRegistry(t, repoRoot), registry.Snapshot{}, []string{
		"supervise", "e2e", "run-once", "--project", "alpha", "--issue", "52", "--json",
	}, &output); err != nil {
		t.Fatalf("RunWork(supervise e2e run-once worker success) error = %v\noutput:\n%s", err, output.String())
	}

	report := decodeSuperviseE2ERunOnceReport(t, output.String())
	if report.Phase != "review_handoff" || report.Status != "passed" || report.CodexExecution != "completed" {
		t.Fatalf("report phase/status/codex = %+v, want review_handoff/passed/completed", report)
	}
	if report.PRs != "draft_created" || report.Merge != supervision.SideEffectNotMerged || report.Deployment != supervision.SideEffectNotStarted || report.Dispatch != supervision.SideEffectNotStarted || !report.HumanMergeRequired {
		t.Fatalf("handoff boundaries = %+v, want draft PR and no merge/deploy/dispatch", report)
	}
	if report.Worktree.Path == "" || report.Worktree.Branch == "" || !strings.HasPrefix(report.Worktree.Path, worktreeRoot) {
		t.Fatalf("worktree = %+v, want kept worktree under configured root", report.Worktree)
	}
	if report.Diff.Files == nil || len(report.Diff.Files) != 1 || report.Diff.Files[0] != plannedPath || report.Diff.SHA256 == "" {
		t.Fatalf("diff = %+v, want only planned path with fingerprint", report.Diff)
	}
	if !report.PR.Draft || !report.PR.Created || report.PR.Number != 77 || report.PR.URL == "" {
		t.Fatalf("pr = %+v, want created draft PR #77", report.PR)
	}
	if !report.CI.Waited || report.CI.TimedOut || report.CI.Conclusion != "success" || report.CI.URL == "" {
		t.Fatalf("ci = %+v, want successful Odin E2E wait", report.CI)
	}
	if len(report.EvidenceComments) != 2 {
		t.Fatalf("evidence comments = %+v, want two handoff comments", report.EvidenceComments)
	}
	if !report.DeploymentAudit.NoDeploymentWorkflows || report.DeploymentAudit.Dispatches != 0 || report.DeploymentAudit.Mutations != 0 {
		t.Fatalf("deployment audit = %+v, want no deployment workflow", report.DeploymentAudit)
	}
	runDir := filepath.Join(odinRoot, "runs", "supervised-e2e", report.RunID)
	for _, name := range []string{"worker-prompt.md", "worker-command.json", "worker-output.txt", "diff-summary.md", "queue-report.json", "pr-report.json", "ci-report.json", "review-evidence.json", "final-report.json"} {
		if _, err := os.Stat(filepath.Join(runDir, name)); err != nil {
			t.Fatalf("expected artifact %s: %v", name, err)
		}
	}
	assertFileContains(t, filepath.Join(runDir, "worker-command.json"), `"sandbox_mode": "workspace-write"`)
	assertFileContains(t, filepath.Join(runDir, "diff-summary.md"), plannedPath)
	assertFileContains(t, filepath.Join(runDir, "final-report.json"), `"codex_execution": "completed"`)
	assertFileContains(t, filepath.Join(runDir, "final-report.json"), `"phase": "review_handoff"`)
	assertFileContains(t, filepath.Join(runDir, "final-report.json"), `"human_merge_required": true`)
	assertFileContains(t, filepath.Join(runDir, "pr-report.json"), `"draft": true`)
	assertFileContains(t, filepath.Join(runDir, "ci-report.json"), `"conclusion": "success"`)
	assertFileContains(t, filepath.Join(runDir, "review-evidence.json"), `odin-stage7-supervised-e2e-review-evidence`)
	assertFileNotContains(t, filepath.Join(runDir, "final-report.json"), "github_pat_1234567890abcdefghijklmnopqrstuvwxyz")
	assertNoForbiddenSuperviseE2EGitHubMutations(t, requests)
}

func TestRunWorkSuperviseE2ERunOnceCreatesDraftPRAndHandoff(t *testing.T) {
	ctx := context.Background()
	store := openWorkCommandStore(t)
	defer store.Close()
	odinRoot := t.TempDir()
	repoRoot := initWorkerDryRunGitRepo(t)
	initStage6Remote(t, repoRoot)
	t.Setenv("ODIN_ROOT", odinRoot)
	t.Setenv("ODIN_WORKTREE_ROOT", filepath.Join(t.TempDir(), "worktrees"))
	t.Setenv("GITHUB_TOKEN", "github_pat_1234567890abcdefghijklmnopqrstuvwxyz")

	plannedPath := "docs/operations/stage-7-pr-handoff.md"
	fake := &superviseE2EFakeTracker{
		issue: tracker.Issue{
			Provider: "github",
			Repo:     "acme/alpha",
			Number:   58,
			Title:    "Worker PR handoff",
			Body:     "Planned scope: " + plannedPath,
			URL:      "https://github.example/acme/alpha/issues/58",
			State:    "open",
			Labels:   []string{"odin:ready", "safety:low-risk"},
		},
	}
	installSuperviseE2EFakeTracker(t, fake)
	workerCompleted := false
	installSuperviseE2EFakeWorker(t, func(_ context.Context, request supervisedE2EWorkerRequest) (supervisedE2EWorkerResult, error) {
		path := filepath.Join(request.WorktreePath, filepath.FromSlash(plannedPath))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll(planned dir) error = %v", err)
		}
		if err := os.WriteFile(path, []byte("# Stage 7 PR handoff\n\nRun make odin-e2e-local before handoff.\n"), 0o644); err != nil {
			t.Fatalf("WriteFile(planned path) error = %v", err)
		}
		workerCompleted = true
		return supervisedE2EWorkerResult{Output: "worker complete: make odin-e2e-local"}, nil
	})

	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatalf("ReadAll() error = %v", err)
		}
		requests = append(requests, request.Method+" "+request.URL.Path+"?"+request.URL.RawQuery+" "+string(body))
		if strings.Contains(request.URL.Path, "/pulls") && !workerCompleted {
			t.Fatalf("PR request arrived before worker diff audit completed: %s %s", request.Method, request.URL.Path)
		}
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/repos/acme/alpha/pulls":
			fmt.Fprint(response, `[]`)
		case request.Method == http.MethodPost && request.URL.Path == "/repos/acme/alpha/pulls":
			if !strings.Contains(string(body), `"draft":true`) || !strings.Contains(string(body), `"head":"`) || !strings.Contains(string(body), "odin work supervise e2e run-once") {
				t.Fatalf("create PR body = %s, want draft PR with supervised branch and Stage 7 provenance", string(body))
			}
			fmt.Fprint(response, `{"number":88,"html_url":"https://github.example/acme/alpha/pull/88","state":"open","draft":true,"title":"Stage 7 supervised E2E handoff","head":{"ref":"stage7"},"base":{"ref":"main"}}`)
		case request.Method == http.MethodGet && request.URL.Path == "/repos/acme/alpha/issues/88/comments":
			fmt.Fprint(response, `[]`)
		case request.Method == http.MethodPost && request.URL.Path == "/repos/acme/alpha/issues/88/comments":
			if !strings.Contains(string(body), "diff_sha256=") || !strings.Contains(string(body), "odin-stage7-supervised-e2e") || !strings.Contains(string(body), "odin work supervise e2e run-once") {
				t.Fatalf("comment body = %s, want Stage 7 diff hash and handoff evidence", string(body))
			}
			fmt.Fprint(response, `{"html_url":"https://github.example/acme/alpha/pull/88#issuecomment-1","body":"ok"}`)
		case request.Method == http.MethodGet && request.URL.Path == "/repos/acme/alpha/actions/runs":
			fmt.Fprint(response, `{"workflow_runs":[{"id":10,"name":"Odin E2E","path":".github/workflows/odin-e2e.yml","html_url":"https://github.example/acme/alpha/actions/runs/10","status":"completed","conclusion":"success","head_branch":"stage7","event":"pull_request"}]}`)
		default:
			t.Fatalf("unexpected request: %s %s?%s body=%s", request.Method, request.URL.Path, request.URL.RawQuery, string(body))
		}
	}))
	defer server.Close()
	t.Setenv("ODIN_GITHUB_API_BASE_URL", server.URL)

	_ = runWorkSuperviseJSON(t, ctx, store, []string{"supervise", "start", "--json"})
	output := runWorkSuperviseE2ERunOnceOutputWithRegistry(t, ctx, store, superviseE2EProjectRegistry(t, repoRoot), []string{
		"supervise", "e2e", "run-once", "--project", "alpha", "--issue", "58", "--ci-timeout", "1s", "--json",
	})
	report := decodeSuperviseE2ERunOnceReport(t, output)
	if report.Phase != "review_handoff" || report.Status != "passed" || report.PRs != "draft_created" {
		t.Fatalf("report = %+v, want passed draft handoff", report)
	}
	if !report.PR.Draft || !report.PR.Created || report.PR.Number != 88 {
		t.Fatalf("pr = %+v, want created draft PR #88", report.PR)
	}
	if !report.CI.Waited || report.CI.TimedOut || report.CI.Conclusion != "success" {
		t.Fatalf("ci = %+v, want successful bounded CI", report.CI)
	}
	if report.Merge != supervision.SideEffectNotMerged || report.Deployment != supervision.SideEffectNotStarted || report.Dispatch != supervision.SideEffectNotStarted || !report.HumanMergeRequired {
		t.Fatalf("side effects = %+v, want no merge/deploy/dispatch and human merge required", report)
	}
	runDir := filepath.Join(odinRoot, "runs", "supervised-e2e", report.RunID)
	assertFileContains(t, filepath.Join(runDir, "final-report.json"), `"phase": "review_handoff"`)
	assertFileContains(t, filepath.Join(runDir, "final-report.json"), `"status": "passed"`)
	assertFileContains(t, filepath.Join(runDir, "final-report.json"), `"deployment": "not_started"`)
	assertNoForbiddenSuperviseE2EGitHubMutations(t, requests)
}

func TestRunWorkSuperviseE2ERunOnceCITimeoutLeavesDraftUnmerged(t *testing.T) {
	ctx := context.Background()
	store := openWorkCommandStore(t)
	defer store.Close()
	odinRoot := t.TempDir()
	repoRoot := initWorkerDryRunGitRepo(t)
	initStage6Remote(t, repoRoot)
	t.Setenv("ODIN_ROOT", odinRoot)
	t.Setenv("ODIN_WORKTREE_ROOT", filepath.Join(t.TempDir(), "worktrees"))
	t.Setenv("GITHUB_TOKEN", "github_pat_1234567890abcdefghijklmnopqrstuvwxyz")

	plannedPath := "docs/operations/stage-7-ci-timeout.md"
	fake := &superviseE2EFakeTracker{
		issue: tracker.Issue{
			Provider: "github",
			Repo:     "acme/alpha",
			Number:   59,
			Title:    "Worker CI timeout",
			Body:     "Planned scope: " + plannedPath,
			URL:      "https://github.example/acme/alpha/issues/59",
			State:    "open",
			Labels:   []string{"odin:ready", "safety:low-risk"},
		},
	}
	installSuperviseE2EFakeTracker(t, fake)
	installSuperviseE2EFakeWorker(t, func(_ context.Context, request supervisedE2EWorkerRequest) (supervisedE2EWorkerResult, error) {
		path := filepath.Join(request.WorktreePath, filepath.FromSlash(plannedPath))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll(planned dir) error = %v", err)
		}
		if err := os.WriteFile(path, []byte("# Stage 7 CI timeout\n\nRun make odin-e2e-local before handoff.\n"), 0o644); err != nil {
			t.Fatalf("WriteFile(planned path) error = %v", err)
		}
		return supervisedE2EWorkerResult{Output: "worker complete: make odin-e2e-local"}, nil
	})

	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatalf("ReadAll() error = %v", err)
		}
		requests = append(requests, request.Method+" "+request.URL.Path+"?"+request.URL.RawQuery+" "+string(body))
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/repos/acme/alpha/pulls":
			fmt.Fprint(response, `[]`)
		case request.Method == http.MethodPost && request.URL.Path == "/repos/acme/alpha/pulls":
			fmt.Fprint(response, `{"number":89,"html_url":"https://github.example/acme/alpha/pull/89","state":"open","draft":true,"title":"Stage 7 supervised E2E handoff","head":{"ref":"stage7"},"base":{"ref":"main"}}`)
		case request.Method == http.MethodGet && request.URL.Path == "/repos/acme/alpha/issues/89/comments":
			fmt.Fprint(response, `[]`)
		case request.Method == http.MethodPost && request.URL.Path == "/repos/acme/alpha/issues/89/comments":
			fmt.Fprint(response, `{"html_url":"https://github.example/acme/alpha/pull/89#issuecomment-1","body":"ok"}`)
		case request.Method == http.MethodGet && request.URL.Path == "/repos/acme/alpha/actions/runs":
			fmt.Fprint(response, `{"workflow_runs":[]}`)
		default:
			t.Fatalf("unexpected request: %s %s?%s body=%s", request.Method, request.URL.Path, request.URL.RawQuery, string(body))
		}
	}))
	defer server.Close()
	t.Setenv("ODIN_GITHUB_API_BASE_URL", server.URL)

	_ = runWorkSuperviseJSON(t, ctx, store, []string{"supervise", "start", "--json"})
	var output strings.Builder
	err := RunWork(ctx, store, superviseE2EProjectRegistry(t, repoRoot), registry.Snapshot{}, []string{
		"supervise", "e2e", "run-once", "--project", "alpha", "--issue", "59", "--ci-timeout", "1ms", "--json",
	}, &output)
	if err == nil {
		t.Fatalf("RunWork(supervise e2e run-once CI timeout) error = nil, want fail-closed timeout\noutput:\n%s", output.String())
	}
	if !strings.Contains(err.Error(), "timed out waiting for Odin E2E CI") {
		t.Fatalf("error = %q, want CI timeout", err.Error())
	}
	runID := newestSuperviseE2ERunID(t, odinRoot)
	finalReportPath := filepath.Join(odinRoot, "runs", "supervised-e2e", runID, "final-report.json")
	assertFileContains(t, finalReportPath, `"status": "failed_closed"`)
	assertFileContains(t, finalReportPath, `"phase": "review_handoff"`)
	assertFileContains(t, finalReportPath, `"timed_out": true`)
	assertFileContains(t, finalReportPath, `"merge": "not_merged"`)
	assertFileContains(t, finalReportPath, `"deployment": "not_started"`)
	assertNoForbiddenSuperviseE2EGitHubMutations(t, requests)
}

func TestRunWorkSuperviseE2ERunOnceDeploymentWorkflowFailsClosed(t *testing.T) {
	ctx := context.Background()
	store := openWorkCommandStore(t)
	defer store.Close()
	odinRoot := t.TempDir()
	repoRoot := initWorkerDryRunGitRepo(t)
	initStage6Remote(t, repoRoot)
	t.Setenv("ODIN_ROOT", odinRoot)
	t.Setenv("ODIN_WORKTREE_ROOT", filepath.Join(t.TempDir(), "worktrees"))
	t.Setenv("GITHUB_TOKEN", "github_pat_1234567890abcdefghijklmnopqrstuvwxyz")

	plannedPath := "docs/operations/stage-7-deployment-block.md"
	fake := &superviseE2EFakeTracker{
		issue: tracker.Issue{
			Provider: "github",
			Repo:     "acme/alpha",
			Number:   60,
			Title:    "Worker deployment fail closed",
			Body:     "Planned scope: " + plannedPath,
			URL:      "https://github.example/acme/alpha/issues/60",
			State:    "open",
			Labels:   []string{"odin:ready", "safety:low-risk"},
		},
	}
	installSuperviseE2EFakeTracker(t, fake)
	installSuperviseE2EFakeWorker(t, func(_ context.Context, request supervisedE2EWorkerRequest) (supervisedE2EWorkerResult, error) {
		path := filepath.Join(request.WorktreePath, filepath.FromSlash(plannedPath))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll(planned dir) error = %v", err)
		}
		if err := os.WriteFile(path, []byte("# Stage 7 deployment block\n\nRun make odin-e2e-local before handoff.\n"), 0o644); err != nil {
			t.Fatalf("WriteFile(planned path) error = %v", err)
		}
		return supervisedE2EWorkerResult{Output: "worker complete: make odin-e2e-local"}, nil
	})

	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatalf("ReadAll() error = %v", err)
		}
		requests = append(requests, request.Method+" "+request.URL.Path+"?"+request.URL.RawQuery+" "+string(body))
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/repos/acme/alpha/pulls":
			fmt.Fprint(response, `[]`)
		case request.Method == http.MethodPost && request.URL.Path == "/repos/acme/alpha/pulls":
			fmt.Fprint(response, `{"number":90,"html_url":"https://github.example/acme/alpha/pull/90","state":"open","draft":true,"title":"Stage 7 supervised E2E handoff","head":{"ref":"stage7"},"base":{"ref":"main"}}`)
		case request.Method == http.MethodGet && request.URL.Path == "/repos/acme/alpha/issues/90/comments":
			fmt.Fprint(response, `[]`)
		case request.Method == http.MethodPost && request.URL.Path == "/repos/acme/alpha/issues/90/comments":
			fmt.Fprint(response, `{"html_url":"https://github.example/acme/alpha/pull/90#issuecomment-1","body":"ok"}`)
		case request.Method == http.MethodGet && request.URL.Path == "/repos/acme/alpha/actions/runs":
			fmt.Fprint(response, `{"workflow_runs":[{"id":10,"name":"Odin E2E","path":".github/workflows/odin-e2e.yml","html_url":"https://github.example/acme/alpha/actions/runs/10","status":"completed","conclusion":"success","head_branch":"stage7","event":"pull_request"},{"id":11,"name":"Production Deploy","path":".github/workflows/deploy-production.yml","html_url":"https://github.example/acme/alpha/actions/runs/11","status":"completed","conclusion":"success","head_branch":"stage7","event":"pull_request"}]}`)
		default:
			t.Fatalf("unexpected request: %s %s?%s body=%s", request.Method, request.URL.Path, request.URL.RawQuery, string(body))
		}
	}))
	defer server.Close()
	t.Setenv("ODIN_GITHUB_API_BASE_URL", server.URL)

	_ = runWorkSuperviseJSON(t, ctx, store, []string{"supervise", "start", "--json"})
	var output strings.Builder
	err := RunWork(ctx, store, superviseE2EProjectRegistry(t, repoRoot), registry.Snapshot{}, []string{
		"supervise", "e2e", "run-once", "--project", "alpha", "--issue", "60", "--ci-timeout", "1s", "--json",
	}, &output)
	if err == nil {
		t.Fatalf("RunWork(supervise e2e run-once deployment workflow) error = nil, want fail-closed deployment audit\noutput:\n%s", output.String())
	}
	if !strings.Contains(err.Error(), "deployment-class workflow ran during Stage 7 supervised e2e proof") {
		t.Fatalf("error = %q, want deployment fail-closed", err.Error())
	}
	runID := newestSuperviseE2ERunID(t, odinRoot)
	finalReportPath := filepath.Join(odinRoot, "runs", "supervised-e2e", runID, "final-report.json")
	assertFileContains(t, finalReportPath, `"status": "failed_closed"`)
	assertFileContains(t, finalReportPath, `"phase": "review_handoff"`)
	assertFileContains(t, finalReportPath, `"no_deployment_workflows": false`)
	assertFileContains(t, finalReportPath, `"deployment": "not_started"`)
	assertNoForbiddenSuperviseE2EGitHubMutations(t, requests)
}

func TestRunWorkSuperviseE2ERunOnceCreatedNonDraftPRReportsSideEffect(t *testing.T) {
	ctx := context.Background()
	store := openWorkCommandStore(t)
	defer store.Close()
	odinRoot := t.TempDir()
	repoRoot := initWorkerDryRunGitRepo(t)
	initStage6Remote(t, repoRoot)
	t.Setenv("ODIN_ROOT", odinRoot)
	t.Setenv("ODIN_WORKTREE_ROOT", filepath.Join(t.TempDir(), "worktrees"))
	t.Setenv("GITHUB_TOKEN", "github_pat_1234567890abcdefghijklmnopqrstuvwxyz")

	plannedPath := "docs/operations/stage-7-non-draft-pr.md"
	installSuperviseE2EFakeTracker(t, &superviseE2EFakeTracker{issue: tracker.Issue{
		Provider: "github",
		Repo:     "acme/alpha",
		Number:   61,
		Title:    "Worker non-draft PR",
		Body:     "Planned scope: " + plannedPath,
		URL:      "https://github.example/acme/alpha/issues/61",
		State:    "open",
		Labels:   []string{"odin:ready", "safety:low-risk"},
	}})
	installSuperviseE2EFakeWorker(t, func(_ context.Context, request supervisedE2EWorkerRequest) (supervisedE2EWorkerResult, error) {
		path := filepath.Join(request.WorktreePath, filepath.FromSlash(plannedPath))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll(planned dir) error = %v", err)
		}
		if err := os.WriteFile(path, []byte("# Stage 7 non-draft PR\n\nRun make odin-e2e-local before handoff.\n"), 0o644); err != nil {
			t.Fatalf("WriteFile(planned path) error = %v", err)
		}
		return supervisedE2EWorkerResult{Output: "worker complete: make odin-e2e-local"}, nil
	})

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatalf("ReadAll() error = %v", err)
		}
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/repos/acme/alpha/pulls":
			fmt.Fprint(response, `[]`)
		case request.Method == http.MethodPost && request.URL.Path == "/repos/acme/alpha/pulls":
			fmt.Fprint(response, `{"number":91,"html_url":"https://github.example/acme/alpha/pull/91","state":"open","draft":false,"title":"Stage 7 supervised E2E handoff","head":{"ref":"stage7"},"base":{"ref":"main"}}`)
		default:
			t.Fatalf("unexpected request: %s %s?%s body=%s", request.Method, request.URL.Path, request.URL.RawQuery, string(body))
		}
	}))
	defer server.Close()
	t.Setenv("ODIN_GITHUB_API_BASE_URL", server.URL)

	_ = runWorkSuperviseJSON(t, ctx, store, []string{"supervise", "start", "--json"})
	var output strings.Builder
	err := RunWork(ctx, store, superviseE2EProjectRegistry(t, repoRoot), registry.Snapshot{}, []string{
		"supervise", "e2e", "run-once", "--project", "alpha", "--issue", "61", "--ci-timeout", "1s", "--json",
	}, &output)
	if err == nil {
		t.Fatalf("RunWork(supervise e2e run-once non-draft PR) error = nil, want fail-closed non-draft\noutput:\n%s", output.String())
	}
	if !strings.Contains(err.Error(), "not a draft PR") {
		t.Fatalf("error = %q, want non-draft PR failure", err.Error())
	}
	runID := newestSuperviseE2ERunID(t, odinRoot)
	finalReportPath := filepath.Join(odinRoot, "runs", "supervised-e2e", runID, "final-report.json")
	assertFileContains(t, finalReportPath, `"status": "failed_closed"`)
	assertFileContains(t, finalReportPath, `"prs": "created_non_draft"`)
	assertFileContains(t, finalReportPath, `"number": 91`)
	assertFileContains(t, finalReportPath, `"created": true`)
	assertFileContains(t, finalReportPath, `"draft": false`)
	assertFileContains(t, finalReportPath, `"merge": "not_merged"`)
	assertFileContains(t, finalReportPath, `"deployment": "not_started"`)
}

func TestRunWorkSuperviseE2ERunOncePartialCommentFailureReportsCreatedComment(t *testing.T) {
	ctx := context.Background()
	store := openWorkCommandStore(t)
	defer store.Close()
	odinRoot := t.TempDir()
	repoRoot := initWorkerDryRunGitRepo(t)
	initStage6Remote(t, repoRoot)
	t.Setenv("ODIN_ROOT", odinRoot)
	t.Setenv("ODIN_WORKTREE_ROOT", filepath.Join(t.TempDir(), "worktrees"))
	t.Setenv("GITHUB_TOKEN", "github_pat_1234567890abcdefghijklmnopqrstuvwxyz")

	plannedPath := "docs/operations/stage-7-partial-comment.md"
	installSuperviseE2EFakeTracker(t, &superviseE2EFakeTracker{issue: tracker.Issue{
		Provider: "github",
		Repo:     "acme/alpha",
		Number:   62,
		Title:    "Worker partial comment",
		Body:     "Planned scope: " + plannedPath,
		URL:      "https://github.example/acme/alpha/issues/62",
		State:    "open",
		Labels:   []string{"odin:ready", "safety:low-risk"},
	}})
	installSuperviseE2EFakeWorker(t, func(_ context.Context, request supervisedE2EWorkerRequest) (supervisedE2EWorkerResult, error) {
		path := filepath.Join(request.WorktreePath, filepath.FromSlash(plannedPath))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll(planned dir) error = %v", err)
		}
		if err := os.WriteFile(path, []byte("# Stage 7 partial comment\n\nRun make odin-e2e-local before handoff.\n"), 0o644); err != nil {
			t.Fatalf("WriteFile(planned path) error = %v", err)
		}
		return supervisedE2EWorkerResult{Output: "worker complete: make odin-e2e-local"}, nil
	})

	commentPosts := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatalf("ReadAll() error = %v", err)
		}
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/repos/acme/alpha/pulls":
			fmt.Fprint(response, `[]`)
		case request.Method == http.MethodPost && request.URL.Path == "/repos/acme/alpha/pulls":
			fmt.Fprint(response, `{"number":92,"html_url":"https://github.example/acme/alpha/pull/92","state":"open","draft":true,"title":"Stage 7 supervised E2E handoff","head":{"ref":"stage7"},"base":{"ref":"main"}}`)
		case request.Method == http.MethodGet && request.URL.Path == "/repos/acme/alpha/issues/92/comments":
			fmt.Fprint(response, `[]`)
		case request.Method == http.MethodPost && request.URL.Path == "/repos/acme/alpha/issues/92/comments":
			commentPosts++
			if commentPosts == 1 {
				fmt.Fprint(response, `{"html_url":"https://github.example/acme/alpha/pull/92#issuecomment-1","body":"ok"}`)
				return
			}
			http.Error(response, `{"message":"comment failed"}`, http.StatusInternalServerError)
		default:
			t.Fatalf("unexpected request: %s %s?%s body=%s", request.Method, request.URL.Path, request.URL.RawQuery, string(body))
		}
	}))
	defer server.Close()
	t.Setenv("ODIN_GITHUB_API_BASE_URL", server.URL)

	_ = runWorkSuperviseJSON(t, ctx, store, []string{"supervise", "start", "--json"})
	var output strings.Builder
	err := RunWork(ctx, store, superviseE2EProjectRegistry(t, repoRoot), registry.Snapshot{}, []string{
		"supervise", "e2e", "run-once", "--project", "alpha", "--issue", "62", "--ci-timeout", "1s", "--json",
	}, &output)
	if err == nil {
		t.Fatalf("RunWork(supervise e2e run-once partial comment failure) error = nil, want fail-closed comment error\noutput:\n%s", output.String())
	}
	runID := newestSuperviseE2ERunID(t, odinRoot)
	finalReportPath := filepath.Join(odinRoot, "runs", "supervised-e2e", runID, "final-report.json")
	assertFileContains(t, finalReportPath, `"status": "failed_closed"`)
	assertFileContains(t, finalReportPath, `"prs": "draft_created"`)
	assertFileContains(t, finalReportPath, `"marker": "\u003c!-- odin-stage7-supervised-e2e-review-evidence --\u003e"`)
	assertFileContains(t, finalReportPath, `"url": "https://github.example/acme/alpha/pull/92#issuecomment-1"`)
	assertFileContains(t, finalReportPath, `"created": true`)
	assertFileContains(t, finalReportPath, `"merge": "not_merged"`)
	assertFileContains(t, finalReportPath, `"deployment": "not_started"`)
}

func TestRunWorkSuperviseE2ERunOnceReusedPRRequiresValidTemplate(t *testing.T) {
	ctx := context.Background()
	store := openWorkCommandStore(t)
	defer store.Close()
	odinRoot := t.TempDir()
	repoRoot := initWorkerDryRunGitRepo(t)
	initStage6Remote(t, repoRoot)
	t.Setenv("ODIN_ROOT", odinRoot)
	t.Setenv("ODIN_WORKTREE_ROOT", filepath.Join(t.TempDir(), "worktrees"))
	t.Setenv("GITHUB_TOKEN", "github_pat_1234567890abcdefghijklmnopqrstuvwxyz")

	plannedPath := "docs/operations/stage-7-reused-pr-template.md"
	installSuperviseE2EFakeTracker(t, &superviseE2EFakeTracker{issue: tracker.Issue{
		Provider: "github",
		Repo:     "acme/alpha",
		Number:   63,
		Title:    "Worker reused PR template",
		Body:     "Planned scope: " + plannedPath,
		URL:      "https://github.example/acme/alpha/issues/63",
		State:    "open",
		Labels:   []string{"odin:ready", "safety:low-risk"},
	}})
	installSuperviseE2EFakeWorker(t, func(_ context.Context, request supervisedE2EWorkerRequest) (supervisedE2EWorkerResult, error) {
		path := filepath.Join(request.WorktreePath, filepath.FromSlash(plannedPath))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll(planned dir) error = %v", err)
		}
		if err := os.WriteFile(path, []byte("# Stage 7 reused PR template\n\nRun make odin-e2e-local before handoff.\n"), 0o644); err != nil {
			t.Fatalf("WriteFile(planned path) error = %v", err)
		}
		return supervisedE2EWorkerResult{Output: "worker complete: make odin-e2e-local"}, nil
	})

	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatalf("ReadAll() error = %v", err)
		}
		requests = append(requests, request.Method+" "+request.URL.Path+"?"+request.URL.RawQuery+" "+string(body))
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/repos/acme/alpha/pulls":
			_ = json.NewEncoder(response).Encode([]map[string]any{{
				"number":   93,
				"html_url": "https://github.example/acme/alpha/pull/93",
				"state":    "open",
				"draft":    true,
				"title":    "Stage 7 supervised E2E handoff",
				"body":     "missing required PR template headings",
				"head":     map[string]string{"ref": "stage7"},
				"base":     map[string]string{"ref": "main"},
			}})
		default:
			t.Fatalf("unexpected request: %s %s?%s body=%s", request.Method, request.URL.Path, request.URL.RawQuery, string(body))
		}
	}))
	defer server.Close()
	t.Setenv("ODIN_GITHUB_API_BASE_URL", server.URL)

	_ = runWorkSuperviseJSON(t, ctx, store, []string{"supervise", "start", "--json"})
	var output strings.Builder
	err := RunWork(ctx, store, superviseE2EProjectRegistry(t, repoRoot), registry.Snapshot{}, []string{
		"supervise", "e2e", "run-once", "--project", "alpha", "--issue", "63", "--ci-timeout", "1s", "--json",
	}, &output)
	if err == nil {
		t.Fatalf("RunWork(supervise e2e run-once reused invalid PR body) error = nil, want template failure\noutput:\n%s", output.String())
	}
	if !strings.Contains(err.Error(), "verify PR template") {
		t.Fatalf("error = %q, want PR template validation failure", err.Error())
	}
	runID := newestSuperviseE2ERunID(t, odinRoot)
	finalReportPath := filepath.Join(odinRoot, "runs", "supervised-e2e", runID, "final-report.json")
	assertFileContains(t, finalReportPath, `"status": "failed_closed"`)
	assertFileContains(t, finalReportPath, `"prs": "draft_reused"`)
	assertFileContains(t, finalReportPath, `"number": 93`)
	assertFileContains(t, finalReportPath, `"reused": true`)
	for _, request := range requests {
		if !strings.HasPrefix(request, "GET /repos/acme/alpha/pulls") {
			t.Fatalf("request = %q, want no comments or CI after invalid reused PR body", request)
		}
	}
}

func TestRunWorkSuperviseE2ERunOnceReusedPRRequiresStage7Provenance(t *testing.T) {
	ctx := context.Background()
	store := openWorkCommandStore(t)
	defer store.Close()
	odinRoot := t.TempDir()
	repoRoot := initWorkerDryRunGitRepo(t)
	initStage6Remote(t, repoRoot)
	t.Setenv("ODIN_ROOT", odinRoot)
	t.Setenv("ODIN_WORKTREE_ROOT", filepath.Join(t.TempDir(), "worktrees"))
	t.Setenv("GITHUB_TOKEN", "github_pat_1234567890abcdefghijklmnopqrstuvwxyz")

	plannedPath := "docs/operations/stage-7-reused-pr-provenance.md"
	installSuperviseE2EFakeTracker(t, &superviseE2EFakeTracker{issue: tracker.Issue{
		Provider: "github",
		Repo:     "acme/alpha",
		Number:   64,
		Title:    "Worker reused PR provenance",
		Body:     "Planned scope: " + plannedPath,
		URL:      "https://github.example/acme/alpha/issues/64",
		State:    "open",
		Labels:   []string{"odin:ready", "safety:low-risk"},
	}})
	installSuperviseE2EFakeWorker(t, func(_ context.Context, request supervisedE2EWorkerRequest) (supervisedE2EWorkerResult, error) {
		path := filepath.Join(request.WorktreePath, filepath.FromSlash(plannedPath))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll(planned dir) error = %v", err)
		}
		if err := os.WriteFile(path, []byte("# Stage 7 reused PR provenance\n\nRun make odin-e2e-local before handoff.\n"), 0o644); err != nil {
			t.Fatalf("WriteFile(planned path) error = %v", err)
		}
		return supervisedE2EWorkerResult{Output: "worker complete: make odin-e2e-local"}, nil
	})

	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatalf("ReadAll() error = %v", err)
		}
		requests = append(requests, request.Method+" "+request.URL.Path+"?"+request.URL.RawQuery+" "+string(body))
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/repos/acme/alpha/pulls":
			_ = json.NewEncoder(response).Encode([]map[string]any{{
				"number":   94,
				"html_url": "https://github.example/acme/alpha/pull/94",
				"state":    "open",
				"draft":    true,
				"title":    "Stage 7 supervised E2E handoff",
				"body":     renderPRCreateBody(64, []string{plannedPath}),
				"head":     map[string]string{"ref": "stage7"},
				"base":     map[string]string{"ref": "main"},
			}})
		default:
			t.Fatalf("unexpected request: %s %s?%s body=%s", request.Method, request.URL.Path, request.URL.RawQuery, string(body))
		}
	}))
	defer server.Close()
	t.Setenv("ODIN_GITHUB_API_BASE_URL", server.URL)

	_ = runWorkSuperviseJSON(t, ctx, store, []string{"supervise", "start", "--json"})
	var output strings.Builder
	err := RunWork(ctx, store, superviseE2EProjectRegistry(t, repoRoot), registry.Snapshot{}, []string{
		"supervise", "e2e", "run-once", "--project", "alpha", "--issue", "64", "--ci-timeout", "1s", "--json",
	}, &output)
	if err == nil {
		t.Fatalf("RunWork(supervise e2e run-once reused stale PR body) error = nil, want provenance failure\noutput:\n%s", output.String())
	}
	if !strings.Contains(err.Error(), "missing Stage 7 run-once provenance") && !strings.Contains(err.Error(), "stale Stage 6 provenance") {
		t.Fatalf("error = %q, want Stage 7 provenance validation failure", err.Error())
	}
	runID := newestSuperviseE2ERunID(t, odinRoot)
	finalReportPath := filepath.Join(odinRoot, "runs", "supervised-e2e", runID, "final-report.json")
	assertFileContains(t, finalReportPath, `"status": "failed_closed"`)
	assertFileContains(t, finalReportPath, `"prs": "draft_reused"`)
	assertFileContains(t, finalReportPath, `"number": 94`)
	for _, request := range requests {
		if !strings.HasPrefix(request, "GET /repos/acme/alpha/pulls") {
			t.Fatalf("request = %q, want no comments or CI after stale reused PR body", request)
		}
	}
}

func TestRunWorkSuperviseE2ERunOnceCITimeoutCancelsSlowGitHubRequest(t *testing.T) {
	ctx := context.Background()
	store := openWorkCommandStore(t)
	defer store.Close()
	odinRoot := t.TempDir()
	repoRoot := initWorkerDryRunGitRepo(t)
	initStage6Remote(t, repoRoot)
	t.Setenv("ODIN_ROOT", odinRoot)
	t.Setenv("ODIN_WORKTREE_ROOT", filepath.Join(t.TempDir(), "worktrees"))
	t.Setenv("GITHUB_TOKEN", "github_pat_1234567890abcdefghijklmnopqrstuvwxyz")

	plannedPath := "docs/operations/stage-7-slow-ci.md"
	installSuperviseE2EFakeTracker(t, &superviseE2EFakeTracker{issue: tracker.Issue{
		Provider: "github",
		Repo:     "acme/alpha",
		Number:   64,
		Title:    "Worker slow CI",
		Body:     "Planned scope: " + plannedPath,
		URL:      "https://github.example/acme/alpha/issues/64",
		State:    "open",
		Labels:   []string{"odin:ready", "safety:low-risk"},
	}})
	installSuperviseE2EFakeWorker(t, func(_ context.Context, request supervisedE2EWorkerRequest) (supervisedE2EWorkerResult, error) {
		path := filepath.Join(request.WorktreePath, filepath.FromSlash(plannedPath))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll(planned dir) error = %v", err)
		}
		if err := os.WriteFile(path, []byte("# Stage 7 slow CI\n\nRun make odin-e2e-local before handoff.\n"), 0o644); err != nil {
			t.Fatalf("WriteFile(planned path) error = %v", err)
		}
		return supervisedE2EWorkerResult{Output: "worker complete: make odin-e2e-local"}, nil
	})

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatalf("ReadAll() error = %v", err)
		}
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/repos/acme/alpha/pulls":
			fmt.Fprint(response, `[]`)
		case request.Method == http.MethodPost && request.URL.Path == "/repos/acme/alpha/pulls":
			fmt.Fprint(response, `{"number":94,"html_url":"https://github.example/acme/alpha/pull/94","state":"open","draft":true,"title":"Stage 7 supervised E2E handoff","head":{"ref":"stage7"},"base":{"ref":"main"}}`)
		case request.Method == http.MethodGet && request.URL.Path == "/repos/acme/alpha/issues/94/comments":
			fmt.Fprint(response, `[]`)
		case request.Method == http.MethodPost && request.URL.Path == "/repos/acme/alpha/issues/94/comments":
			fmt.Fprint(response, `{"html_url":"https://github.example/acme/alpha/pull/94#issuecomment-1","body":"ok"}`)
		case request.Method == http.MethodGet && request.URL.Path == "/repos/acme/alpha/actions/runs":
			<-request.Context().Done()
		default:
			t.Fatalf("unexpected request: %s %s?%s body=%s", request.Method, request.URL.Path, request.URL.RawQuery, string(body))
		}
	}))
	defer server.Close()
	t.Setenv("ODIN_GITHUB_API_BASE_URL", server.URL)

	_ = runWorkSuperviseJSON(t, ctx, store, []string{"supervise", "start", "--json"})
	started := time.Now()
	var output strings.Builder
	err := RunWork(ctx, store, superviseE2EProjectRegistry(t, repoRoot), registry.Snapshot{}, []string{
		"supervise", "e2e", "run-once", "--project", "alpha", "--issue", "64", "--ci-timeout", "10ms", "--json",
	}, &output)
	elapsed := time.Since(started)
	if err == nil {
		t.Fatalf("RunWork(supervise e2e run-once slow CI) error = nil, want timeout\noutput:\n%s", output.String())
	}
	if elapsed > time.Second {
		t.Fatalf("slow CI timeout took %s, want hard timeout near --ci-timeout", elapsed)
	}
	runID := newestSuperviseE2ERunID(t, odinRoot)
	finalReportPath := filepath.Join(odinRoot, "runs", "supervised-e2e", runID, "final-report.json")
	assertFileContains(t, finalReportPath, `"status": "failed_closed"`)
	assertFileContains(t, finalReportPath, `"timed_out": true`)
	assertFileContains(t, finalReportPath, `"merge": "not_merged"`)
	assertFileContains(t, finalReportPath, `"deployment": "not_started"`)
}

func TestRunWorkSuperviseE2ERunOnceForbiddenDiffBlocksPR(t *testing.T) {
	ctx := context.Background()
	store := openWorkCommandStore(t)
	defer store.Close()
	odinRoot := t.TempDir()
	repoRoot := initWorkerDryRunGitRepo(t)
	t.Setenv("ODIN_ROOT", odinRoot)
	t.Setenv("ODIN_WORKTREE_ROOT", filepath.Join(t.TempDir(), "worktrees"))

	plannedPath := "docs/operations/stage-7-worker-forbidden.md"
	fake := &superviseE2EFakeTracker{
		issue: tracker.Issue{
			Provider: "github",
			Repo:     "acme/alpha",
			Number:   53,
			Title:    "Worker forbidden diff",
			Body:     "Planned scope: " + plannedPath,
			URL:      "https://github.example/acme/alpha/issues/53",
			State:    "open",
			Labels:   []string{"odin:ready", "safety:low-risk"},
		},
	}
	installSuperviseE2EFakeTracker(t, fake)
	installSuperviseE2EFakeWorker(t, func(_ context.Context, request supervisedE2EWorkerRequest) (supervisedE2EWorkerResult, error) {
		planned := filepath.Join(request.WorktreePath, filepath.FromSlash(plannedPath))
		if err := os.MkdirAll(filepath.Dir(planned), 0o755); err != nil {
			t.Fatalf("MkdirAll(planned dir) error = %v", err)
		}
		if err := os.WriteFile(planned, []byte("# Planned\n"), 0o644); err != nil {
			t.Fatalf("WriteFile(planned) error = %v", err)
		}
		if err := os.WriteFile(filepath.Join(request.WorktreePath, "README.md"), []byte("# Changed outside plan\n"), 0o644); err != nil {
			t.Fatalf("WriteFile(forbidden) error = %v", err)
		}
		return supervisedE2EWorkerResult{Output: "worker touched forbidden diff"}, nil
	})

	_ = runWorkSuperviseJSON(t, ctx, store, []string{"supervise", "start", "--json"})
	var output strings.Builder
	err := RunWork(ctx, store, superviseE2EProjectRegistry(t, repoRoot), registry.Snapshot{}, []string{
		"supervise", "e2e", "run-once", "--project", "alpha", "--issue", "53", "--json",
	}, &output)
	if err == nil {
		t.Fatalf("RunWork(supervise e2e run-once forbidden diff) error = nil, want blocked PR\noutput:\n%s", output.String())
	}
	if !strings.Contains(err.Error(), "worker diff changed forbidden files") {
		t.Fatalf("error = %q, want forbidden diff audit failure", err.Error())
	}
	runID := newestSuperviseE2ERunID(t, odinRoot)
	finalReportPath := filepath.Join(odinRoot, "runs", "supervised-e2e", runID, "final-report.json")
	assertFileContains(t, finalReportPath, `"status": "failed_closed"`)
	assertFileContains(t, finalReportPath, `"phase": "diff_audit"`)
	assertFileContains(t, finalReportPath, `"prs": "not_created"`)
	assertFileContains(t, finalReportPath, `"README.md"`)
}

func TestRunWorkSuperviseE2ERunOnceTokenInDiffBlocksPR(t *testing.T) {
	ctx := context.Background()
	store := openWorkCommandStore(t)
	defer store.Close()
	odinRoot := t.TempDir()
	repoRoot := initWorkerDryRunGitRepo(t)
	t.Setenv("ODIN_ROOT", odinRoot)
	t.Setenv("ODIN_WORKTREE_ROOT", filepath.Join(t.TempDir(), "worktrees"))

	plannedPath := "docs/operations/stage-7-worker-leak.md"
	fake := &superviseE2EFakeTracker{
		issue: tracker.Issue{
			Provider: "github",
			Repo:     "acme/alpha",
			Number:   54,
			Title:    "Worker token diff",
			Body:     "Planned scope: " + plannedPath,
			URL:      "https://github.example/acme/alpha/issues/54",
			State:    "open",
			Labels:   []string{"odin:ready", "safety:low-risk"},
		},
	}
	installSuperviseE2EFakeTracker(t, fake)
	installSuperviseE2EFakeWorker(t, func(_ context.Context, request supervisedE2EWorkerRequest) (supervisedE2EWorkerResult, error) {
		path := filepath.Join(request.WorktreePath, filepath.FromSlash(plannedPath))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll(planned dir) error = %v", err)
		}
		if err := os.WriteFile(path, []byte("leaked token ghp_1234567890abcdefghijklmnopqrst\n"), 0o644); err != nil {
			t.Fatalf("WriteFile(planned path) error = %v", err)
		}
		return supervisedE2EWorkerResult{Output: "worker complete"}, nil
	})

	_ = runWorkSuperviseJSON(t, ctx, store, []string{"supervise", "start", "--json"})
	var output strings.Builder
	err := RunWork(ctx, store, superviseE2EProjectRegistry(t, repoRoot), registry.Snapshot{}, []string{
		"supervise", "e2e", "run-once", "--project", "alpha", "--issue", "54", "--json",
	}, &output)
	if err == nil {
		t.Fatalf("RunWork(supervise e2e run-once token diff) error = nil, want token audit failure\noutput:\n%s", output.String())
	}
	if !strings.Contains(err.Error(), "token-shaped string detected") {
		t.Fatalf("error = %q, want token audit failure", err.Error())
	}
	runID := newestSuperviseE2ERunID(t, odinRoot)
	runDir := filepath.Join(odinRoot, "runs", "supervised-e2e", runID)
	assertFileContains(t, filepath.Join(runDir, "final-report.json"), `"status": "failed_closed"`)
	assertFileContains(t, filepath.Join(runDir, "final-report.json"), `"phase": "token_audit"`)
	assertFileContains(t, filepath.Join(runDir, "final-report.json"), `"prs": "not_created"`)
	assertFileNotContains(t, filepath.Join(runDir, "final-report.json"), "ghp_1234567890abcdefghijklmnopqrst")
	assertFileNotContains(t, filepath.Join(runDir, "diff-summary.md"), "ghp_1234567890abcdefghijklmnopqrst")
}

func TestRunWorkSuperviseE2ERunOnceWorkerSetupFailureRewritesFinalReport(t *testing.T) {
	ctx := context.Background()
	store := openWorkCommandStore(t)
	defer store.Close()
	odinRoot := t.TempDir()
	repoRoot := initWorkerDryRunGitRepo(t)
	blockedWorktreeRoot := filepath.Join(t.TempDir(), "worktree-root-file")
	if err := os.WriteFile(blockedWorktreeRoot, []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("WriteFile(blocked worktree root) error = %v", err)
	}
	t.Setenv("ODIN_ROOT", odinRoot)
	t.Setenv("ODIN_WORKTREE_ROOT", blockedWorktreeRoot)

	plannedPath := "docs/operations/stage-7-worker-setup-failure.md"
	fake := &superviseE2EFakeTracker{
		issue: tracker.Issue{
			Provider: "github",
			Repo:     "acme/alpha",
			Number:   56,
			Title:    "Worker setup failure",
			Body:     "Planned scope: " + plannedPath,
			URL:      "https://github.example/acme/alpha/issues/56",
			State:    "open",
			Labels:   []string{"odin:ready", "safety:low-risk"},
		},
	}
	installSuperviseE2EFakeTracker(t, fake)
	workerCalls := 0
	installSuperviseE2EFakeWorker(t, func(context.Context, supervisedE2EWorkerRequest) (supervisedE2EWorkerResult, error) {
		workerCalls++
		return supervisedE2EWorkerResult{}, nil
	})

	_ = runWorkSuperviseJSON(t, ctx, store, []string{"supervise", "start", "--json"})
	var output strings.Builder
	err := RunWork(ctx, store, superviseE2EProjectRegistry(t, repoRoot), registry.Snapshot{}, []string{
		"supervise", "e2e", "run-once", "--project", "alpha", "--issue", "56", "--json",
	}, &output)
	if err == nil {
		t.Fatalf("RunWork(supervise e2e run-once worker setup failure) error = nil, want fail-closed setup error\noutput:\n%s", output.String())
	}
	if workerCalls != 0 {
		t.Fatalf("worker calls = %d, want none when worktree setup fails", workerCalls)
	}
	runID := newestSuperviseE2ERunID(t, odinRoot)
	finalReportPath := filepath.Join(odinRoot, "runs", "supervised-e2e", runID, "final-report.json")
	assertFileContains(t, finalReportPath, `"status": "failed_closed"`)
	assertFileContains(t, finalReportPath, `"phase": "worker_setup"`)
	assertFileContains(t, finalReportPath, `"codex_execution": "not_started"`)
	assertFileContains(t, finalReportPath, `"prs": "not_created"`)
}

func TestRunSupervisedE2EWorkerArtifactWriteFailuresRewriteFinalReport(t *testing.T) {
	tests := []struct {
		name           string
		brokenArtifact string
		wantPhase      string
		wantExecution  string
		wantWorkerCall bool
	}{
		{
			name:           "worker prompt",
			brokenArtifact: "worker_prompt",
			wantPhase:      "worker_command",
			wantExecution:  supervision.SideEffectNotStarted,
			wantWorkerCall: false,
		},
		{
			name:           "worker output",
			brokenArtifact: "worker_output",
			wantPhase:      "worker_execution",
			wantExecution:  "attempted",
			wantWorkerCall: true,
		},
		{
			name:           "diff summary",
			brokenArtifact: "diff_summary",
			wantPhase:      "diff_audit",
			wantExecution:  "attempted",
			wantWorkerCall: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			repoRoot := initWorkerDryRunGitRepo(t)
			t.Setenv("ODIN_WORKTREE_ROOT", filepath.Join(t.TempDir(), "worktrees"))

			plannedPath := "docs/operations/stage-7-artifact-failure.md"
			artifactDir := t.TempDir()
			brokenPath := filepath.Join(artifactDir, "broken-artifact")
			if err := os.Mkdir(brokenPath, 0o755); err != nil {
				t.Fatalf("Mkdir(broken artifact) error = %v", err)
			}
			artifacts := workSuperviseE2EArtifactRefs{
				QueueReport:   filepath.Join(artifactDir, "queue-report.json"),
				FinalReport:   filepath.Join(artifactDir, "final-report.json"),
				WorkerPrompt:  filepath.Join(artifactDir, "worker-prompt.md"),
				WorkerCommand: filepath.Join(artifactDir, "worker-command.json"),
				WorkerOutput:  filepath.Join(artifactDir, "worker-output.txt"),
				DiffSummary:   filepath.Join(artifactDir, "diff-summary.md"),
			}
			switch test.brokenArtifact {
			case "worker_prompt":
				artifacts.WorkerPrompt = brokenPath
			case "worker_output":
				artifacts.WorkerOutput = brokenPath
			case "diff_summary":
				artifacts.DiffSummary = brokenPath
			}

			workerCalls := 0
			installSuperviseE2EFakeWorker(t, func(_ context.Context, request supervisedE2EWorkerRequest) (supervisedE2EWorkerResult, error) {
				workerCalls++
				path := filepath.Join(request.WorktreePath, filepath.FromSlash(plannedPath))
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatalf("MkdirAll(planned dir) error = %v", err)
				}
				if err := os.WriteFile(path, []byte("# Stage 7 artifact failure\n"), 0o644); err != nil {
					t.Fatalf("WriteFile(planned path) error = %v", err)
				}
				return supervisedE2EWorkerResult{Output: "worker complete"}, nil
			})

			report := workSuperviseE2EReport{
				Mode:           "supervised_e2e",
				Phase:          "queued",
				Status:         "claimed",
				Project:        "alpha",
				Repo:           "acme/alpha",
				RunID:          "artifact-failure",
				CodexExecution: supervision.SideEffectNotStarted,
				PRs:            supervision.SideEffectNotCreated,
				Merge:          supervision.SideEffectNotMerged,
				Deployment:     supervision.SideEffectNotStarted,
				Artifacts:      artifacts,
			}
			issue := supervision.Issue{
				Provider:     "github",
				Repo:         "acme/alpha",
				Number:       57,
				Title:        "Artifact write failure",
				Body:         "Planned scope: " + plannedPath,
				URL:          "https://github.example/acme/alpha/issues/57",
				State:        "open",
				Labels:       []string{"odin:ready", "safety:low-risk"},
				ChangedPaths: []string{plannedPath},
			}
			manifest := projects.Manifest{
				Key:           "alpha",
				Name:          "Alpha",
				GitRoot:       repoRoot,
				DefaultBranch: "main",
				GitHub:        projects.GitHub{Repo: "acme/alpha"},
			}

			_, err := runSupervisedE2EWorkerAndAudit(ctx, manifest, issue, report, artifacts)
			if err == nil {
				t.Fatalf("runSupervisedE2EWorkerAndAudit() error = nil, want fail-closed artifact write error")
			}
			if test.wantWorkerCall && workerCalls != 1 {
				t.Fatalf("worker calls = %d, want one launch before artifact failure", workerCalls)
			}
			if !test.wantWorkerCall && workerCalls != 0 {
				t.Fatalf("worker calls = %d, want no launch before artifact failure", workerCalls)
			}
			assertFileContains(t, artifacts.FinalReport, `"status": "failed_closed"`)
			assertFileContains(t, artifacts.FinalReport, `"phase": "`+test.wantPhase+`"`)
			assertFileContains(t, artifacts.FinalReport, `"codex_execution": "`+test.wantExecution+`"`)
			assertFileContains(t, artifacts.FinalReport, `"prs": "not_created"`)
		})
	}
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

type superviseE2ERunOnceReport struct {
	Mode     string                      `json:"mode"`
	Phase    string                      `json:"phase"`
	Status   string                      `json:"status"`
	Project  string                      `json:"project"`
	Repo     string                      `json:"repo"`
	RunID    string                      `json:"run_id"`
	Issue    workSuperviseE2EIssueReport `json:"issue"`
	Queue    []supervision.QueueDecision `json:"queue"`
	Claims   []supervision.PlannedClaim  `json:"claims"`
	Worktree struct {
		Path   string `json:"path"`
		Branch string `json:"branch"`
	} `json:"worktree"`
	Diff struct {
		Files  []string `json:"files"`
		SHA256 string   `json:"sha256"`
	} `json:"diff"`
	Branch             workPRCreateBranchReport            `json:"branch"`
	PR                 workPRCreatePullRequestReport       `json:"pr"`
	EvidenceComments   []workPRCreateEvidenceCommentReport `json:"evidence_comments"`
	CI                 workPRCreateCIReport                `json:"ci"`
	DeploymentAudit    workPRCreateDeploymentAuditReport   `json:"deployment_audit"`
	CodexExecution     string                              `json:"codex_execution"`
	PRs                string                              `json:"prs"`
	Merge              string                              `json:"merge"`
	Deployment         string                              `json:"deployment"`
	Dispatch           string                              `json:"dispatch"`
	HumanMergeRequired bool                                `json:"human_merge_required"`
	Artifacts          workSuperviseE2EArtifactRefs        `json:"artifacts"`
}

type superviseE2EFakeTracker struct {
	issue              tracker.Issue
	err                error
	lastID             tracker.IssueID
	fetchByIDCalls     int
	fetchEligibleCalls int
}

func (fake *superviseE2EFakeTracker) FetchEligibleIssues(context.Context) ([]tracker.Issue, error) {
	fake.fetchEligibleCalls++
	return nil, fmt.Errorf("unexpected FetchEligibleIssues call")
}

func (fake *superviseE2EFakeTracker) FetchIssueByID(_ context.Context, id tracker.IssueID) (tracker.Issue, error) {
	fake.fetchByIDCalls++
	fake.lastID = id
	if fake.err != nil {
		return tracker.Issue{}, fake.err
	}
	return fake.issue, nil
}

func (fake *superviseE2EFakeTracker) MarkInProgress(context.Context, tracker.IssueID) error {
	return fmt.Errorf("unexpected mutation")
}

func (fake *superviseE2EFakeTracker) MarkBlocked(context.Context, tracker.IssueID, string) error {
	return fmt.Errorf("unexpected mutation")
}

func (fake *superviseE2EFakeTracker) MarkFailed(context.Context, tracker.IssueID, string) error {
	return fmt.Errorf("unexpected mutation")
}

func (fake *superviseE2EFakeTracker) MarkReadyForReview(context.Context, tracker.IssueID) error {
	return fmt.Errorf("unexpected mutation")
}

func (fake *superviseE2EFakeTracker) MarkDone(context.Context, tracker.IssueID) error {
	return fmt.Errorf("unexpected mutation")
}

func (fake *superviseE2EFakeTracker) AddComment(context.Context, tracker.IssueID, string) error {
	return fmt.Errorf("unexpected mutation")
}

func (fake *superviseE2EFakeTracker) CreateFollowUpIssue(context.Context, tracker.FollowUpIssue) (tracker.Issue, error) {
	return tracker.Issue{}, fmt.Errorf("unexpected mutation")
}

func installSuperviseE2EFakeTracker(t *testing.T, fake *superviseE2EFakeTracker) {
	t.Helper()

	previousFactory := newIntakeTracker
	t.Cleanup(func() { newIntakeTracker = previousFactory })
	newIntakeTracker = func(project projects.Manifest, options trackerintake.SyncOptions) (tracker.Tracker, error) {
		if project.GitHub.Repo != "acme/alpha" {
			return nil, fmt.Errorf("repo = %q, want acme/alpha", project.GitHub.Repo)
		}
		return fake, nil
	}
}

func installSuperviseE2EFakeWorker(t *testing.T, fake func(context.Context, supervisedE2EWorkerRequest) (supervisedE2EWorkerResult, error)) {
	t.Helper()

	previous := runSupervisedE2EWorker
	t.Cleanup(func() { runSupervisedE2EWorker = previous })
	runSupervisedE2EWorker = fake
}

func superviseE2EProjectRegistry(t *testing.T, repoRoot string) projects.Registry {
	t.Helper()

	path := filepath.Join(t.TempDir(), "projects.yaml")
	if err := os.WriteFile(path, []byte(`
version: 1
projects:
  - key: alpha
    name: Alpha
    project_class: github_backed_project
    git_root: `+repoRoot+`
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

func runWorkSuperviseE2ERunOnceOutput(t *testing.T, ctx context.Context, store *sqlite.Store, args []string) string {
	t.Helper()

	var output strings.Builder
	if err := RunWork(ctx, store, commandProjectRegistry(t), registry.Snapshot{}, args, &output); err != nil {
		t.Fatalf("RunWork(%v) error = %v\noutput:\n%s", args, err, output.String())
	}
	return output.String()
}

func runWorkSuperviseE2ERunOnceOutputWithRegistry(t *testing.T, ctx context.Context, store *sqlite.Store, projectRegistry projects.Registry, args []string) string {
	t.Helper()

	var output strings.Builder
	if err := RunWork(ctx, store, projectRegistry, registry.Snapshot{}, args, &output); err != nil {
		t.Fatalf("RunWork(%v) error = %v\noutput:\n%s", args, err, output.String())
	}
	return output.String()
}

func decodeSuperviseE2ERunOnceReport(t *testing.T, output string) superviseE2ERunOnceReport {
	t.Helper()

	var report superviseE2ERunOnceReport
	if err := json.Unmarshal([]byte(output), &report); err != nil {
		t.Fatalf("json.Unmarshal(run-once output) error = %v\noutput:\n%s", err, output)
	}
	return report
}

func newestSuperviseE2ERunID(t *testing.T, odinRoot string) string {
	t.Helper()

	entries, err := os.ReadDir(filepath.Join(odinRoot, "runs", "supervised-e2e"))
	if err != nil {
		t.Fatalf("ReadDir(supervised-e2e) error = %v", err)
	}
	if len(entries) == 0 {
		t.Fatalf("supervised-e2e run dirs = 0, want at least one")
	}
	latest := entries[0].Name()
	for _, entry := range entries[1:] {
		if entry.Name() > latest {
			latest = entry.Name()
		}
	}
	return latest
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

func assertNoForbiddenSuperviseE2EGitHubMutations(t *testing.T, requests []string) {
	t.Helper()

	for _, forbidden := range []string{"/labels", "/merge", "/reviews", "/actions/workflows", "/dispatches", "/deployments"} {
		for _, request := range requests {
			if strings.Contains(request, forbidden) {
				t.Fatalf("request %q contains forbidden path fragment %q", request, forbidden)
			}
		}
	}
}
