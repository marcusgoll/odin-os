package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"odin-os/internal/store/sqlite"
)

func TestApprovalSupportFiltersE2E(t *testing.T) {
	repoRoot := projectRoot(t)
	binaryPath := buildOdinBinary(t, repoRoot)
	runtimeRoot := t.TempDir()
	driverPath := writeRobinhoodShellFixtureDriver(t, `{"status":"completed","tool_key":"robinhood_transfer_flow","summary":"Robinhood transfer submitted","artifacts":{"session_state":"submitted","current_url":"https://robinhood.com/transfers","next_action":"verify transfer status"}}`)

	prepareOutput, err := runOdinCommand(t, repoRoot, binaryPath, runtimeRoot, map[string]string{
		"ODIN_HUGINN_ROBINHOOD_TRANSFER_DRIVER": driverPath,
	}, "/project pbs\n/transfer prepare direction=deposit amount_usd=25.00 source_account=checking destination_account=brokerage memo=filter-e2e\n/scope global\n", "repl")
	if err != nil {
		t.Fatalf("prepare supported approval error = %v\n%s", err, prepareOutput)
	}
	if !strings.Contains(prepareOutput, "summary=review prepared and awaiting approval") {
		t.Fatalf("prepare output = %q, want prepared approval summary", prepareOutput)
	}

	store := openRuntimeStore(t, runtimeRoot)
	unsupportedApprovalID, unsupportedRunID := seedUnsupportedApprovalForSupportFilterE2E(t, context.Background(), store, runtimeRoot)
	if err := store.Close(); err != nil {
		t.Fatalf("Close(store) error = %v", err)
	}

	supportedJSON, err := runOdinCommand(t, repoRoot, binaryPath, runtimeRoot, nil, "", "approvals", "supported", "--json")
	if err != nil {
		t.Fatalf("approvals supported --json error = %v\n%s", err, supportedJSON)
	}
	var supportedPayload struct {
		Approvals []struct {
			ApprovalID      int64  `json:"approval_id"`
			TaskKey         string `json:"task_key"`
			Status          string `json:"status"`
			ResolverSupport string `json:"resolver_support"`
			RunID           *int64 `json:"run_id,omitempty"`
		} `json:"approvals"`
	}
	if err := json.Unmarshal([]byte(supportedJSON), &supportedPayload); err != nil {
		t.Fatalf("supported JSON parse error = %v\n%s", err, supportedJSON)
	}
	if len(supportedPayload.Approvals) != 1 {
		t.Fatalf("supported approvals len = %d, want 1\n%s", len(supportedPayload.Approvals), supportedJSON)
	}
	supported := supportedPayload.Approvals[0]
	if !strings.HasPrefix(supported.TaskKey, "robinhood-transfer-") {
		t.Fatalf("supported task key = %q, want robinhood-transfer prefix", supported.TaskKey)
	}
	if supported.Status != "pending" {
		t.Fatalf("supported status = %q, want pending", supported.Status)
	}
	if supported.ResolverSupport != "supported" {
		t.Fatalf("supported resolver = %q, want supported", supported.ResolverSupport)
	}
	if supported.RunID == nil {
		t.Fatal("supported run id = nil, want prepare run id")
	}

	unsupportedText, err := runOdinCommand(t, repoRoot, binaryPath, runtimeRoot, nil, "", "approvals", "unsupported")
	if err != nil {
		t.Fatalf("approvals unsupported error = %v\n%s", err, unsupportedText)
	}
	for _, want := range []string{
		fmt.Sprintf("approval=%d", unsupportedApprovalID),
		"task=manual-unsupported-filter-e2e",
		fmt.Sprintf("run=%d", unsupportedRunID),
		"status=pending",
		"resolver=unsupported",
	} {
		if !strings.Contains(unsupportedText, want) {
			t.Fatalf("unsupported output = %q, want %q", unsupportedText, want)
		}
	}
	if strings.Contains(unsupportedText, "task=robinhood-transfer-") || strings.Contains(unsupportedText, "resolver=supported") {
		t.Fatalf("unsupported output = %q, should not include supported approval", unsupportedText)
	}

	tuiOutput, err := runOdinCommand(t, repoRoot, binaryPath, runtimeRoot, nil, "/scope global\n/approvals supported\n/approvals unsupported\n", "repl")
	if err != nil {
		t.Fatalf("filtered REPL approvals error = %v\n%s", err, tuiOutput)
	}
	for _, want := range []string{
		"approval=1 task=robinhood-transfer-",
		"resolver=supported",
		fmt.Sprintf("approval=%d task=manual-unsupported-filter-e2e", unsupportedApprovalID),
		"resolver=unsupported",
	} {
		if !strings.Contains(tuiOutput, want) {
			t.Fatalf("TUI output = %q, want %q", tuiOutput, want)
		}
	}
}

func seedUnsupportedApprovalForSupportFilterE2E(t *testing.T, ctx context.Context, store *sqlite.Store, runtimeRoot string) (int64, int64) {
	t.Helper()

	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "approval-filter-unsupported",
		Name:          "Approval Filter Unsupported",
		Scope:         "project",
		GitRoot:       filepath.Join(runtimeRoot, "repos", "approval-filter-unsupported"),
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject(unsupported) error = %v", err)
	}
	task, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "manual-unsupported-filter-e2e",
		Title:       "Manual unsupported approval filter E2E",
		Status:      "blocked",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask(unsupported) error = %v", err)
	}
	run, err := store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:   task.ID,
		Executor: "codex_headless",
		Attempt:  1,
		Status:   "blocked",
	})
	if err != nil {
		t.Fatalf("StartRun(unsupported) error = %v", err)
	}
	approval, err := store.RequestApproval(ctx, sqlite.RequestApprovalParams{
		TaskID:      task.ID,
		RunID:       &run.ID,
		Status:      "pending",
		RequestedBy: "system",
	})
	if err != nil {
		t.Fatalf("RequestApproval(unsupported) error = %v", err)
	}
	return approval.ID, run.ID
}
