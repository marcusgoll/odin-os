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
	"odin-os/internal/runtime/supervision"
	"odin-os/internal/store/sqlite"
	"odin-os/internal/tracker"
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
	t.Setenv("ODIN_ROOT", odinRoot)

	fake := &superviseE2EFakeTracker{
		issue: tracker.Issue{
			Provider: "github",
			Repo:     "acme/alpha",
			Number:   42,
			Title:    "Supervised E2E exact issue",
			Body:     "Planned scope: docs/operations/stage-7-task-3.md",
			URL:      "https://github.example/acme/alpha/issues/42",
			State:    "open",
			Labels:   []string{"odin:ready", "safety:low-risk"},
		},
	}
	installSuperviseE2EFakeTracker(t, fake)

	_ = runWorkSuperviseJSON(t, ctx, store, []string{"supervise", "start", "--json"})
	var output strings.Builder
	if err := RunWork(ctx, store, commandProjectRegistry(t), registry.Snapshot{}, []string{
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
	if report.Phase != "queued" || report.Status != "claimed" || report.Project != "alpha" || report.Repo != "acme/alpha" {
		t.Fatalf("report = %+v, want queued/claimed alpha acme/alpha", report)
	}
	if report.Issue.Number != 42 || report.Issue.PlannedPath != "docs/operations/stage-7-task-3.md" {
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
	if report.CodexExecution != supervision.SideEffectNotStarted ||
		report.PRs != supervision.SideEffectNotCreated ||
		report.Merge != supervision.SideEffectNotMerged ||
		report.Deployment != supervision.SideEffectNotStarted {
		t.Fatalf("side effects = %+v, want no worker/PR/merge/deploy", report)
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
	assertFileContains(t, finalReportPath, `"status": "claimed"`)
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

func TestRunWorkSuperviseE2ERunOnceDuplicateActiveClaimPreservesExistingClaim(t *testing.T) {
	ctx := context.Background()
	store := openWorkCommandStore(t)
	defer store.Close()
	odinRoot := t.TempDir()
	t.Setenv("ODIN_ROOT", odinRoot)

	fake := &superviseE2EFakeTracker{
		issue: tracker.Issue{
			Provider: "github",
			Repo:     "acme/alpha",
			Number:   46,
			Title:    "Duplicate exact issue",
			Body:     "Planned scope: docs/operations/stage-7-idempotent.md",
			URL:      "https://github.example/acme/alpha/issues/46",
			State:    "open",
			Labels:   []string{"odin:ready", "safety:low-risk"},
		},
	}
	installSuperviseE2EFakeTracker(t, fake)

	_ = runWorkSuperviseJSON(t, ctx, store, []string{"supervise", "start", "--json"})
	firstOutput := runWorkSuperviseE2ERunOnceOutput(t, ctx, store, []string{
		"supervise", "e2e", "run-once", "--project", "alpha", "--issue", "46", "--json",
	})
	secondOutput := runWorkSuperviseE2ERunOnceOutput(t, ctx, store, []string{
		"supervise", "e2e", "run-once", "--project", "alpha", "--issue", "46", "--json",
	})
	first := decodeSuperviseE2ERunOnceReport(t, firstOutput)
	second := decodeSuperviseE2ERunOnceReport(t, secondOutput)
	if len(first.Claims) != 1 || len(second.Claims) != 1 {
		t.Fatalf("claims = first %+v second %+v, want one claim reported by each run", first.Claims, second.Claims)
	}
	if first.Claims[0].ClaimKey != second.Claims[0].ClaimKey {
		t.Fatalf("claim keys = first %q second %q, want existing claim preserved", first.Claims[0].ClaimKey, second.Claims[0].ClaimKey)
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
	Mode           string                       `json:"mode"`
	Phase          string                       `json:"phase"`
	Status         string                       `json:"status"`
	Project        string                       `json:"project"`
	Repo           string                       `json:"repo"`
	RunID          string                       `json:"run_id"`
	Issue          workSuperviseE2EIssueReport  `json:"issue"`
	Queue          []supervision.QueueDecision  `json:"queue"`
	Claims         []supervision.PlannedClaim   `json:"claims"`
	CodexExecution string                       `json:"codex_execution"`
	PRs            string                       `json:"prs"`
	Merge          string                       `json:"merge"`
	Deployment     string                       `json:"deployment"`
	Artifacts      workSuperviseE2EArtifactRefs `json:"artifacts"`
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

func runWorkSuperviseE2ERunOnceOutput(t *testing.T, ctx context.Context, store *sqlite.Store, args []string) string {
	t.Helper()

	var output strings.Builder
	if err := RunWork(ctx, store, commandProjectRegistry(t), registry.Snapshot{}, args, &output); err != nil {
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
