package leases

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"odin-os/internal/store/sqlite"
	"odin-os/internal/telemetry/logs"
)

func TestManagerPrepareMutableAllocatesBranchAndWorktree(t *testing.T) {
	ctx := context.Background()
	store, project, task, run := openLeaseManagerStore(t)
	defer store.Close()

	var logOutput bytes.Buffer
	git := &fakeGit{}
	worktreeRoot := t.TempDir()
	manager := Manager{
		Store:        store,
		Git:          git,
		WorktreeRoot: worktreeRoot,
		Logger:       &logs.Logger{Writer: &logOutput},
	}

	assignment, err := manager.Prepare(ctx, Request{
		Mutating:      true,
		ProjectID:     project.ID,
		ProjectKey:    project.Key,
		TaskID:        task.ID,
		RunID:         run.ID,
		RepoRoot:      project.GitRoot,
		DefaultBranch: project.DefaultBranch,
		Try:           1,
	})
	if err != nil {
		t.Fatalf("Prepare(mutable) error = %v", err)
	}

	if assignment.Mode != "mutable" {
		t.Fatalf("Prepare(mutable).Mode = %q, want %q", assignment.Mode, "mutable")
	}
	wantBranch := fmt.Sprintf("odin/%s/task-%d/run-%d/try-1", project.Key, task.ID, run.ID)
	if assignment.BranchName != wantBranch {
		t.Fatalf("Prepare(mutable).BranchName = %q", assignment.BranchName)
	}
	wantPath := filepath.ToSlash(filepath.Join(worktreeRoot, project.Key, fmt.Sprintf("task-%d", task.ID), fmt.Sprintf("run-%d", run.ID), "try-1"))
	if assignment.WorktreePath != wantPath {
		t.Fatalf("Prepare(mutable).WorktreePath = %q", assignment.WorktreePath)
	}
	if git.createBranchCalls != 1 || git.addWorktreeCalls != 1 {
		t.Fatalf("git calls = create:%d add:%d, want 1/1", git.createBranchCalls, git.addWorktreeCalls)
	}
	record := decodeSingleLeaseLogRecord(t, logOutput.Bytes())
	if record.Message != "workspace lease prepared" {
		t.Fatalf("log message = %q, want workspace lease prepared", record.Message)
	}
	if record.ProjectID == nil || *record.ProjectID != project.ID {
		t.Fatalf("log project_id = %v, want %d", record.ProjectID, project.ID)
	}
	if record.TaskID == nil || *record.TaskID != task.ID {
		t.Fatalf("log task_id = %v, want %d", record.TaskID, task.ID)
	}
	if record.RunID == nil || *record.RunID != run.ID {
		t.Fatalf("log run_id = %v, want %d", record.RunID, run.ID)
	}
	for key, want := range map[string]any{
		"operation":     "prepare",
		"outcome":       "prepared",
		"project_key":   project.Key,
		"branch_name":   wantBranch,
		"worktree_path": wantPath,
		"repo_root":     project.GitRoot,
	} {
		if got := record.Fields[key]; got != want {
			t.Fatalf("log field %s = %v, want %v", key, got, want)
		}
	}
	if record.Fields["lease_id"] == nil {
		t.Fatalf("log fields = %#v, want lease_id", record.Fields)
	}
}

func TestManagerIsCanonicalWorkspaceManager(t *testing.T) {
	t.Parallel()

	var _ WorkspaceManager = Manager{}
}

func TestManagerPrepareReadOnlySkipsMutableAllocation(t *testing.T) {
	t.Parallel()

	manager := Manager{
		Git:          &fakeGit{},
		WorktreeRoot: t.TempDir(),
	}

	assignment, err := manager.Prepare(context.Background(), Request{
		Mutating: false,
		RepoRoot: "/home/orchestrator/projects/cfipros",
	})
	if err != nil {
		t.Fatalf("Prepare(read-only) error = %v", err)
	}
	if assignment.Mode != "read_only" {
		t.Fatalf("Prepare(read-only).Mode = %q, want %q", assignment.Mode, "read_only")
	}
	if assignment.WorktreePath != "/home/orchestrator/projects/cfipros" {
		t.Fatalf("Prepare(read-only).WorktreePath = %q", assignment.WorktreePath)
	}
}

func TestManagerPrepareMutableReusesActiveLeaseForSameTaskRun(t *testing.T) {
	ctx := context.Background()
	store, project, task, run := openLeaseManagerStore(t)
	defer store.Close()

	var logOutput bytes.Buffer
	git := &fakeGit{}
	manager := Manager{
		Store:        store,
		Git:          git,
		WorktreeRoot: t.TempDir(),
		Logger:       &logs.Logger{Writer: &logOutput},
	}

	first, err := manager.Prepare(ctx, Request{
		Mutating:      true,
		ProjectID:     project.ID,
		ProjectKey:    project.Key,
		TaskID:        task.ID,
		RunID:         run.ID,
		RepoRoot:      project.GitRoot,
		DefaultBranch: project.DefaultBranch,
		Try:           1,
	})
	if err != nil {
		t.Fatalf("Prepare(first) error = %v", err)
	}

	second, err := manager.Prepare(ctx, Request{
		Mutating:      true,
		ProjectID:     project.ID,
		ProjectKey:    project.Key,
		TaskID:        task.ID,
		RunID:         run.ID,
		RepoRoot:      project.GitRoot,
		DefaultBranch: project.DefaultBranch,
		Try:           1,
	})
	if err != nil {
		t.Fatalf("Prepare(second) error = %v", err)
	}

	if !second.Reused {
		t.Fatalf("Prepare(second).Reused = false, want true")
	}
	if second.LeaseID == nil || first.LeaseID == nil || *second.LeaseID != *first.LeaseID {
		t.Fatalf("Prepare(second).LeaseID = %v, want %v", second.LeaseID, first.LeaseID)
	}
	if git.addWorktreeCalls != 1 {
		t.Fatalf("git add worktree calls = %d, want 1", git.addWorktreeCalls)
	}
	records := decodeLeaseLogRecords(t, logOutput.Bytes())
	if len(records) != 2 {
		t.Fatalf("log record count = %d, want 2", len(records))
	}
	reuse := records[1]
	if reuse.Message != "workspace lease reused" {
		t.Fatalf("reuse log message = %q, want workspace lease reused", reuse.Message)
	}
	for key, want := range map[string]any{
		"operation":     "prepare",
		"outcome":       "reused",
		"project_key":   project.Key,
		"branch_name":   first.BranchName,
		"worktree_path": first.WorktreePath,
		"repo_root":     project.GitRoot,
	} {
		if got := reuse.Fields[key]; got != want {
			t.Fatalf("reuse log field %s = %v, want %v", key, got, want)
		}
	}
}

func TestManagerPrepareMutableLogsFailureWithSecretRedaction(t *testing.T) {
	ctx := context.Background()
	store, project, task, run := openLeaseManagerStore(t)
	defer store.Close()

	const token = "ghp_1234567890abcdefghijklmnopqrstuvwx"
	var logOutput bytes.Buffer
	manager := Manager{
		Store: store,
		Git: &fakeGit{
			branchExistsErr: fmt.Errorf("git failed token=%s", token),
		},
		WorktreeRoot: t.TempDir(),
		Logger:       &logs.Logger{Writer: &logOutput},
	}

	_, err := manager.Prepare(ctx, Request{
		Mutating:      true,
		ProjectID:     project.ID,
		ProjectKey:    project.Key,
		TaskID:        task.ID,
		RunID:         run.ID,
		RepoRoot:      project.GitRoot,
		DefaultBranch: project.DefaultBranch,
		Try:           1,
	})
	if err == nil {
		t.Fatal("Prepare(branch failure) error = nil, want error")
	}
	if strings.Contains(logOutput.String(), token) {
		t.Fatalf("log output leaked token: %s", logOutput.String())
	}
	record := decodeSingleLeaseLogRecord(t, logOutput.Bytes())
	if record.Level != logs.LevelWarn {
		t.Fatalf("log level = %q, want warn", record.Level)
	}
	if record.Message != "workspace lease prepare failed" {
		t.Fatalf("log message = %q, want workspace lease prepare failed", record.Message)
	}
	if record.Fields["outcome"] != "failed" || record.Fields["reason"] != "branch_exists_failed" {
		t.Fatalf("log fields = %#v, want failed branch_exists_failed", record.Fields)
	}
	if record.Fields["error"] == "" {
		t.Fatalf("log fields = %#v, want error field", record.Fields)
	}
}

func TestManagerPrepareMutablePropagatesLeaseConflict(t *testing.T) {
	ctx := context.Background()
	store, project, task, run := openLeaseManagerStore(t)
	defer store.Close()

	if _, err := store.CreateWorktreeLease(ctx, sqlite.CreateWorktreeLeaseParams{
		ProjectID:    project.ID,
		TaskID:       task.ID,
		RunID:        run.ID,
		Mode:         "mutable",
		BranchName:   fmt.Sprintf("odin/%s/task-%d/run-%d/try-1", project.Key, task.ID, run.ID),
		WorktreePath: filepath.ToSlash(fmt.Sprintf("/var/tmp/odin-worktrees/%s/task-%d/run-%d/try-1", project.Key, task.ID, run.ID)),
		RepoRoot:     project.GitRoot,
		State:        "active",
	}); err != nil {
		t.Fatalf("CreateWorktreeLease(seed conflict) error = %v", err)
	}

	conflictingRun, err := store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:   task.ID,
		Executor: "codex",
		Attempt:  2,
		Status:   "running",
	})
	if err != nil {
		t.Fatalf("StartRun(conflicting) error = %v", err)
	}

	manager := Manager{
		Store:        store,
		Git:          &fakeGit{},
		WorktreeRoot: t.TempDir(),
	}

	_, err = manager.Prepare(ctx, Request{
		Mutating:      true,
		ProjectID:     project.ID,
		ProjectKey:    project.Key,
		TaskID:        task.ID,
		RunID:         conflictingRun.ID,
		RepoRoot:      project.GitRoot,
		DefaultBranch: project.DefaultBranch,
		Try:           1,
	})
	if err == nil {
		t.Fatalf("Prepare(conflict) error = nil, want conflict")
	}
	if !errors.Is(err, sqlite.ErrWorktreeLeaseConflict) {
		t.Fatalf("Prepare(conflict) error = %v, want ErrWorktreeLeaseConflict", err)
	}
}

func TestManagerPrepareMutableSkipsExistingFilesystemPathByAdvancingTry(t *testing.T) {
	ctx := context.Background()
	store, project, task, run := openLeaseManagerStore(t)
	defer store.Close()

	worktreeRoot := t.TempDir()
	stalePath := filepath.Join(worktreeRoot, project.Key, fmt.Sprintf("task-%d", task.ID), fmt.Sprintf("run-%d", run.ID), "try-1")
	if err := os.MkdirAll(stalePath, 0o755); err != nil {
		t.Fatalf("MkdirAll(stalePath) error = %v", err)
	}

	git := &fakeGit{}
	manager := Manager{
		Store:        store,
		Git:          git,
		WorktreeRoot: worktreeRoot,
	}

	assignment, err := manager.Prepare(ctx, Request{
		Mutating:      true,
		ProjectID:     project.ID,
		ProjectKey:    project.Key,
		TaskID:        task.ID,
		RunID:         run.ID,
		RepoRoot:      project.GitRoot,
		DefaultBranch: project.DefaultBranch,
		Try:           1,
	})
	if err != nil {
		t.Fatalf("Prepare(existing path) error = %v", err)
	}

	wantBranch := fmt.Sprintf("odin/%s/task-%d/run-%d/try-2", project.Key, task.ID, run.ID)
	if assignment.BranchName != wantBranch {
		t.Fatalf("Prepare(existing path).BranchName = %q, want %q", assignment.BranchName, wantBranch)
	}
	wantPath := filepath.ToSlash(filepath.Join(worktreeRoot, project.Key, fmt.Sprintf("task-%d", task.ID), fmt.Sprintf("run-%d", run.ID), "try-2"))
	if assignment.WorktreePath != wantPath {
		t.Fatalf("Prepare(existing path).WorktreePath = %q, want %q", assignment.WorktreePath, wantPath)
	}
	if git.addWorktreeCalls != 1 {
		t.Fatalf("git add worktree calls = %d, want 1", git.addWorktreeCalls)
	}
}

type fakeGit struct {
	createBranchCalls int
	addWorktreeCalls  int
	branchExistsErr   error
	createBranchErr   error
	addWorktreeErr    error
}

func (git *fakeGit) BranchExists(context.Context, string, string) (bool, error) {
	if git.branchExistsErr != nil {
		return false, git.branchExistsErr
	}
	return false, nil
}

func (git *fakeGit) CreateBranch(context.Context, string, string, string) error {
	git.createBranchCalls++
	return git.createBranchErr
}

func (git *fakeGit) AddWorktree(context.Context, string, string, string) error {
	git.addWorktreeCalls++
	return git.addWorktreeErr
}

func (git *fakeGit) RemoveWorktree(context.Context, string, string) error {
	return nil
}

func (git *fakeGit) WorktreeDirty(context.Context, string) (bool, error) {
	return false, nil
}

func openLeaseManagerStore(t *testing.T) (*sqlite.Store, sqlite.Project, sqlite.Task, sqlite.Run) {
	t.Helper()

	store := openManagerTestStore(t)
	project, task, run := createProjectTaskRun(t, context.Background(), store)
	return store, project, task, run
}

func openManagerTestStore(t *testing.T) *sqlite.Store {
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

func createProjectTaskRun(t *testing.T, ctx context.Context, store *sqlite.Store) (sqlite.Project, sqlite.Task, sqlite.Run) {
	t.Helper()

	project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "cfipros",
		Name:          "CFI Pros",
		Scope:         "project",
		GitRoot:       "/home/orchestrator/projects/cfipros",
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}

	task, run := createTaskRun(t, ctx, store, project.ID, 42)
	return project, task, run
}

func createTaskRun(t *testing.T, ctx context.Context, store *sqlite.Store, projectID int64, idBase int64) (sqlite.Task, sqlite.Run) {
	t.Helper()

	task, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   projectID,
		Key:         fmt.Sprintf("task-key-%d", idBase),
		Title:       "Task title",
		Status:      "running",
		Scope:       "project",
		RequestedBy: "operator",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	run, err := store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:   task.ID,
		Executor: "codex",
		Attempt:  1,
		Status:   "running",
	})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	return task, run
}

type leaseLogRecord struct {
	Level     logs.Level     `json:"level"`
	Component string         `json:"component"`
	Message   string         `json:"message"`
	ProjectID *int64         `json:"project_id,omitempty"`
	TaskID    *int64         `json:"task_id,omitempty"`
	RunID     *int64         `json:"run_id,omitempty"`
	Fields    map[string]any `json:"fields,omitempty"`
}

func decodeSingleLeaseLogRecord(t *testing.T, data []byte) leaseLogRecord {
	t.Helper()

	records := decodeLeaseLogRecords(t, data)
	if len(records) != 1 {
		t.Fatalf("log record count = %d, want 1: %s", len(records), string(data))
	}
	return records[0]
}

func decodeLeaseLogRecords(t *testing.T, data []byte) []leaseLogRecord {
	t.Helper()

	lines := bytes.Split(bytes.TrimSpace(data), []byte("\n"))
	if len(lines) == 1 && len(lines[0]) == 0 {
		return nil
	}
	records := make([]leaseLogRecord, 0, len(lines))
	for _, line := range lines {
		var record leaseLogRecord
		if err := json.Unmarshal(line, &record); err != nil {
			t.Fatalf("decode log record %q: %v", string(line), err)
		}
		records = append(records, record)
	}
	return records
}
