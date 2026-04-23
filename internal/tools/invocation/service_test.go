package invocation

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"odin-os/internal/adapters/browserhuman"
	"odin-os/internal/adapters/web"
	"odin-os/internal/store/sqlite"
)

func TestBuiltinToolInvokesRuntimeDriver(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	runtimeRoot := t.TempDir()
	store := openInvocationStore(t, runtimeRoot)
	defer store.Close()

	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "alpha",
		Name:          "Alpha",
		Scope:         "project",
		GitRoot:       runtimeRoot,
		DefaultBranch: "main",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	if _, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "alpha-queued",
		Title:       "Queued runtime task",
		Status:      "queued",
		Scope:       "project",
		RequestedBy: "test",
	}); err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	service := Service{RuntimeRoot: runtimeRoot}
	result, err := service.Invoke(ctx, "project_status", Request{
		Args: map[string]string{"project_key": "alpha"},
	})
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if result.Source != "driver" {
		t.Fatalf("source = %q, want driver", result.Source)
	}
	if result.KeyFacts["project_key"] != "alpha" {
		t.Fatalf("project_key fact = %q, want alpha", result.KeyFacts["project_key"])
	}
	if result.KeyFacts["open_task_count"] != "1" {
		t.Fatalf("open_task_count = %q, want 1", result.KeyFacts["open_task_count"])
	}
	if !strings.Contains(result.RawOutput, "project=alpha") {
		t.Fatalf("raw output = %q, want project marker", result.RawOutput)
	}
}

func TestServicePreservesStructuredArtifacts(t *testing.T) {
	script := writeFixtureDriver(t, `#!/usr/bin/env bash
printf '{"status":"completed","tool_key":"huginn_browser_session","summary":"ok","artifacts":{"session_state":"ready","snapshots":[{"name":"home","url":"https://example.com"}],"labels":["alpha","beta"]},"debug_note":"preserve-me"}'
`)
	t.Setenv("ODIN_BROWSER_HUMAN_DRIVER", script)

	service := Service{}
	result, err := service.BrowserHuman(context.Background(), browserhuman.Request{
		ToolKey: "huginn_browser_session",
		Input:   map[string]any{"url": "https://example.com"},
	})
	if err != nil {
		t.Fatalf("BrowserHuman() error = %v", err)
	}
	if result.ToolKey != "huginn_browser_session" {
		t.Fatalf("ToolKey = %q, want huginn_browser_session", result.ToolKey)
	}
	if result.RawOutput != `{"status":"completed","tool_key":"huginn_browser_session","summary":"ok","artifacts":{"session_state":"ready","snapshots":[{"name":"home","url":"https://example.com"}],"labels":["alpha","beta"]},"debug_note":"preserve-me"}` {
		t.Fatalf("RawOutput = %q, want exact driver stdout", result.RawOutput)
	}
	if got := result.Artifacts["session_state"]; got != "ready" {
		t.Fatalf("Artifacts.session_state = %#v, want ready", got)
	}
}

func TestServiceRobinhoodTransferPreservesStructuredArtifacts(t *testing.T) {
	script := writeFixtureDriver(t, `#!/usr/bin/env bash
printf '{"status":"completed","tool_key":"robinhood_transfer_flow","summary":"Robinhood continuity check failed","artifacts":{"session_state":"resume_verification_failed","prior_session_state":"session_expired","evidence":["driver invoked"]}}'
`)
	t.Setenv("ODIN_HUGINN_ROBINHOOD_TRANSFER_DRIVER", script)

	service := Service{}
	result, err := service.RobinhoodTransfer(context.Background(), web.RobinhoodTransferRequest{
		Input: web.RobinhoodTransferInput{
			Mode:               "submit",
			Direction:          "deposit",
			AmountUSD:          "25.00",
			SourceAccount:      "checking",
			DestinationAccount: "brokerage",
			ResumeFacts: map[string]string{
				"expected_review_state": "review_ready",
			},
		},
	})
	if err != nil {
		t.Fatalf("RobinhoodTransfer() error = %v", err)
	}
	if result.ToolKey != "robinhood_transfer_flow" {
		t.Fatalf("ToolKey = %q, want robinhood_transfer_flow", result.ToolKey)
	}
	if result.RawOutput != `{"status":"completed","tool_key":"robinhood_transfer_flow","summary":"Robinhood continuity check failed","artifacts":{"session_state":"resume_verification_failed","prior_session_state":"session_expired","evidence":["driver invoked"]}}` {
		t.Fatalf("RawOutput = %q, want exact driver stdout", result.RawOutput)
	}
	if got := result.Artifacts["session_state"]; got != "resume_verification_failed" {
		t.Fatalf("Artifacts.session_state = %#v, want resume_verification_failed", got)
	}
	if got := result.Artifacts["prior_session_state"]; got != "session_expired" {
		t.Fatalf("Artifacts.prior_session_state = %#v, want session_expired", got)
	}
}

func TestCloneArtifactsDeepCopiesNestedValues(t *testing.T) {
	source := map[string]any{
		"session_state": "ready",
		"snapshots": []any{
			map[string]any{"name": "home", "url": "https://example.com"},
		},
	}

	cloned := cloneArtifacts(source)
	cloned["session_state"] = "mutated"
	clonedSnapshots := cloned["snapshots"].([]any)
	clonedSnapshot := clonedSnapshots[0].(map[string]any)
	clonedSnapshot["name"] = "changed"

	if got := source["session_state"]; got != "ready" {
		t.Fatalf("source session_state = %#v, want ready", got)
	}
	sourceSnapshots := source["snapshots"].([]any)
	sourceSnapshot := sourceSnapshots[0].(map[string]any)
	if got := sourceSnapshot["name"]; got != "home" {
		t.Fatalf("source snapshots[0].name = %#v, want home", got)
	}
}

func openInvocationStore(t *testing.T, runtimeRoot string) *sqlite.Store {
	t.Helper()

	dataDir := filepath.Join(runtimeRoot, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(data) error = %v", err)
	}
	store, err := sqlite.Open(filepath.Join(dataDir, "odin.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		_ = store.Close()
		t.Fatalf("Migrate() error = %v", err)
	}
	return store
}

func writeFixtureDriver(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "driver.sh")
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("WriteFile(driver) error = %v", err)
	}
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatalf("Chmod(driver) error = %v", err)
	}
	return path
}
