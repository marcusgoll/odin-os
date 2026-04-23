package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"odin-os/internal/store/sqlite"
)

func TestIntakeEnqueueCLI(t *testing.T) {
	t.Parallel()

	sourceRepoRoot := projectRoot(t)
	repoRoot := createCLIRepoRootWithPreferredExecutor(t, "codex_headless")
	runtimeRoot := t.TempDir()
	odinBinary := buildOdinBinary(t, sourceRepoRoot)

	payloadPath := filepath.Join(t.TempDir(), "payload.json")
	if err := os.WriteFile(payloadPath, []byte(`{"workflow_id":"alpha-ci-1","run_id":"42"}`), 0o644); err != nil {
		t.Fatalf("WriteFile(payload) error = %v", err)
	}

	output, err := runOdinCommand(
		t,
		repoRoot,
		odinBinary,
		runtimeRoot,
		nil,
		"",
		"intake", "enqueue",
		"--source", "n8n",
		"--project", "alpha-cli",
		"--title", "Investigate alpha intake",
		"--type", "ci_failure",
		"--dedup-key", "ci_failure:alpha-cli:42",
		"--payload-file", payloadPath,
		"--json",
	)
	if err != nil {
		t.Fatalf("runOdinCommand(intake enqueue) error = %v\n%s", err, output)
	}

	var payload struct {
		Task struct {
			ID     int64  `json:"id"`
			Key    string `json:"key"`
			Status string `json:"status"`
		} `json:"task"`
		Intake struct {
			Source   string `json:"source"`
			Type     string `json:"type"`
			DedupKey string `json:"dedup_key"`
		} `json:"intake"`
	}
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		t.Fatalf("unmarshal intake output = %v\n%s", err, output)
	}
	if payload.Task.ID == 0 {
		t.Fatalf("payload = %+v, want populated task id", payload)
	}
	if payload.Task.Status != "queued" {
		t.Fatalf("task status = %q, want queued", payload.Task.Status)
	}
	if payload.Intake.Source != "n8n" {
		t.Fatalf("intake source = %q, want n8n", payload.Intake.Source)
	}
	if payload.Intake.Type != "ci_failure" {
		t.Fatalf("intake type = %q, want ci_failure", payload.Intake.Type)
	}
	if payload.Intake.DedupKey != "ci_failure:alpha-cli:42" {
		t.Fatalf("intake dedup key = %q, want ci_failure:alpha-cli:42", payload.Intake.DedupKey)
	}

	store := openRuntimeStore(t, runtimeRoot)
	defer store.Close()

	task, err := store.GetTask(context.Background(), payload.Task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if task.Title != "Investigate alpha intake" {
		t.Fatalf("task title = %q, want Investigate alpha intake", task.Title)
	}

	row := store.DB().QueryRowContext(context.Background(), `
		SELECT id
		FROM task_intakes
		WHERE task_id = ?
		ORDER BY id DESC
		LIMIT 1
	`, payload.Task.ID)
	var intakeID int64
	if err := row.Scan(&intakeID); err != nil {
		t.Fatalf("scan intake id error = %v", err)
	}
	intake, err := store.GetTaskIntake(context.Background(), intakeID)
	if err != nil {
		t.Fatalf("GetTaskIntake() error = %v", err)
	}
	if !strings.Contains(intake.PayloadJSON, `"workflow_id":"alpha-ci-1"`) {
		t.Fatalf("intake payload = %q, want workflow payload", intake.PayloadJSON)
	}
}

func TestApprovalsResolveCLI(t *testing.T) {
	t.Parallel()

	sourceRepoRoot := projectRoot(t)
	repoRoot := createCLIRepoRootWithPreferredExecutor(t, "codex_headless")
	runtimeRoot := t.TempDir()
	odinBinary := buildOdinBinary(t, sourceRepoRoot)

	store := openRuntimeStore(t, runtimeRoot)
	defer store.Close()

	ctx := context.Background()
	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "alpha-cli",
		Name:          "Alpha",
		Scope:         "project",
		GitRoot:       filepath.Join(repoRoot, "alpha"),
		DefaultBranch: "main",
		GitHubRepo:    "acme/alpha",
		ManifestPath:  filepath.Join(repoRoot, "config", "projects.yaml"),
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	task, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         "alpha-approval",
		Title:       "Approval gate",
		Status:      "running",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	approval, err := store.RequestApproval(ctx, sqlite.RequestApprovalParams{
		TaskID:      task.ID,
		Status:      "pending",
		RequestedBy: "system",
	})
	if err != nil {
		t.Fatalf("RequestApproval() error = %v", err)
	}

	output, err := runOdinCommand(
		t,
		repoRoot,
		odinBinary,
		runtimeRoot,
		nil,
		"",
		"approvals", "resolve",
		"--id", fmt.Sprintf("%d", approval.ID),
		"--decision", "approve",
		"--reason", "safe to proceed",
		"--by", "operator",
		"--json",
	)
	if err != nil {
		t.Fatalf("runOdinCommand(approvals resolve) error = %v\n%s", err, output)
	}

	var payload struct {
		ID         int64  `json:"id"`
		Status     string `json:"status"`
		DecisionBy string `json:"decision_by"`
		Reason     string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		t.Fatalf("unmarshal approvals resolve output = %v\n%s", err, output)
	}
	if payload.Status != "approved" {
		t.Fatalf("status = %q, want approved", payload.Status)
	}
	if payload.DecisionBy != "operator" {
		t.Fatalf("decision_by = %q, want operator", payload.DecisionBy)
	}
	if payload.Reason != "safe to proceed" {
		t.Fatalf("reason = %q, want safe to proceed", payload.Reason)
	}

	resolved, err := store.GetApproval(ctx, approval.ID)
	if err != nil {
		t.Fatalf("GetApproval() error = %v", err)
	}
	if resolved.Status != "approved" {
		t.Fatalf("approval status = %q, want approved", resolved.Status)
	}
	if resolved.DecisionBy != "operator" {
		t.Fatalf("decision_by = %q, want operator", resolved.DecisionBy)
	}
	if resolved.Reason != "safe to proceed" {
		t.Fatalf("reason = %q, want safe to proceed", resolved.Reason)
	}
}

func TestIntakeEnqueueCLIRejectsMissingPayload(t *testing.T) {
	t.Parallel()

	sourceRepoRoot := projectRoot(t)
	repoRoot := createCLIRepoRootWithPreferredExecutor(t, "codex_headless")
	runtimeRoot := t.TempDir()
	odinBinary := buildOdinBinary(t, sourceRepoRoot)

	_, err := runOdinCommand(
		t,
		repoRoot,
		odinBinary,
		runtimeRoot,
		nil,
		"",
		"intake", "enqueue",
		"--source", "n8n",
		"--project", "alpha-cli",
		"--title", "Investigate alpha intake",
		"--type", "ci_failure",
		"--payload-file", "-",
	)
	if err == nil {
		t.Fatal("runOdinCommand(intake enqueue) error = nil, want payload validation error")
	}
}

func TestIntakeEnqueueCLIAllowsSameTitleBurst(t *testing.T) {
	t.Parallel()

	sourceRepoRoot := projectRoot(t)
	repoRoot := createCLIRepoRootWithPreferredExecutor(t, "codex_headless")
	runtimeRoot := t.TempDir()
	odinBinary := buildOdinBinary(t, sourceRepoRoot)

	payloadOne := filepath.Join(t.TempDir(), "payload-1.json")
	if err := os.WriteFile(payloadOne, []byte(`{"workflow_id":"alpha-ci-1","run_id":"42"}`), 0o644); err != nil {
		t.Fatalf("WriteFile(payloadOne) error = %v", err)
	}
	payloadTwo := filepath.Join(t.TempDir(), "payload-2.json")
	if err := os.WriteFile(payloadTwo, []byte(`{"workflow_id":"alpha-ci-2","run_id":"43"}`), 0o644); err != nil {
		t.Fatalf("WriteFile(payloadTwo) error = %v", err)
	}

	runEnqueue := func(payloadPath string, dedupKey string) string {
		t.Helper()
		output, err := runOdinCommand(
			t,
			repoRoot,
			odinBinary,
			runtimeRoot,
			nil,
			"",
			"intake", "enqueue",
			"--source", "n8n",
			"--project", "alpha-cli",
			"--title", "Investigate alpha intake",
			"--type", "ci_failure",
			"--dedup-key", dedupKey,
			"--payload-file", payloadPath,
			"--json",
		)
		if err != nil {
			t.Fatalf("runOdinCommand(intake enqueue) error = %v\n%s", err, output)
		}
		var payload struct {
			Task struct {
				Key string `json:"key"`
			} `json:"task"`
		}
		if err := json.Unmarshal([]byte(output), &payload); err != nil {
			t.Fatalf("unmarshal intake output = %v\n%s", err, output)
		}
		return payload.Task.Key
	}

	firstKey := runEnqueue(payloadOne, "ci_failure:alpha-cli:42")
	secondKey := runEnqueue(payloadTwo, "ci_failure:alpha-cli:43")
	if firstKey == secondKey {
		t.Fatalf("task keys = %q and %q, want distinct keys for same-title burst", firstKey, secondKey)
	}
}

func TestIntakeEnqueueCLIDedupConflictLeavesNoQueuedTaskWithoutIntake(t *testing.T) {
	t.Parallel()

	sourceRepoRoot := projectRoot(t)
	repoRoot := createCLIRepoRootWithPreferredExecutor(t, "codex_headless")
	runtimeRoot := t.TempDir()
	odinBinary := buildOdinBinary(t, sourceRepoRoot)

	payloadPath := filepath.Join(t.TempDir(), "payload.json")
	if err := os.WriteFile(payloadPath, []byte(`{"workflow_id":"alpha-ci-1","run_id":"42"}`), 0o644); err != nil {
		t.Fatalf("WriteFile(payload) error = %v", err)
	}

	runArgs := []string{
		"intake", "enqueue",
		"--source", "n8n",
		"--project", "alpha-cli",
		"--title", "Investigate alpha intake",
		"--type", "ci_failure",
		"--dedup-key", "ci_failure:alpha-cli:42",
		"--payload-file", payloadPath,
		"--json",
	}
	if output, err := runOdinCommand(t, repoRoot, odinBinary, runtimeRoot, nil, "", runArgs...); err != nil {
		t.Fatalf("runOdinCommand(first intake enqueue) error = %v\n%s", err, output)
	}
	if output, err := runOdinCommand(t, repoRoot, odinBinary, runtimeRoot, nil, "", runArgs...); err == nil {
		t.Fatalf("runOdinCommand(second intake enqueue) error = nil, want dedup conflict\n%s", output)
	}

	store := openRuntimeStore(t, runtimeRoot)
	defer store.Close()

	row := store.DB().QueryRowContext(context.Background(), `
		SELECT COUNT(*)
		FROM tasks t
		LEFT JOIN task_intakes ti ON ti.task_id = t.id
		WHERE t.status = 'queued' AND ti.id IS NULL
	`)
	var queuedWithoutIntake int
	if err := row.Scan(&queuedWithoutIntake); err != nil {
		t.Fatalf("scan queued without intake count error = %v", err)
	}
	if queuedWithoutIntake != 0 {
		t.Fatalf("queued tasks without intake = %d, want 0", queuedWithoutIntake)
	}
}
