package runs

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"odin-os/internal/cli/scope"
	"odin-os/internal/runtime/projections"
	"odin-os/internal/store/sqlite"
)

type Service struct {
	DB    *sql.DB
	Store *sqlite.Store
}

type DelegationEvidence struct {
	Relation   string
	Delegation sqlite.Delegation
	Artifacts  []sqlite.DelegationArtifact
}

type Detail struct {
	Run             sqlite.Run
	Task            sqlite.Task
	Project         sqlite.Project
	Transcripts     []sqlite.ConversationTranscript
	MemorySummaries []sqlite.MemorySummary
	Delegations     []DelegationEvidence
}

func (service Service) List(ctx context.Context, resolved scope.Resolution) ([]projections.RunSummaryView, error) {
	rows, err := service.DB.QueryContext(ctx, `
		SELECT
			r.id,
			r.task_id,
			t.key,
			r.executor,
			r.status,
			r.attempt,
			r.started_at,
			r.finished_at,
			p.key,
			t.scope
		FROM runs r
		JOIN tasks t ON t.id = r.task_id
		JOIN projects p ON p.id = t.project_id
		ORDER BY r.id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var views []projections.RunSummaryView
	for rows.Next() {
		var view projections.RunSummaryView
		var projectKey string
		var taskScope string
		var finishedAt sql.NullString
		if err := rows.Scan(
			&view.RunID,
			&view.TaskID,
			&view.TaskKey,
			&view.Executor,
			&view.Status,
			&view.Attempt,
			&view.StartedAt,
			&finishedAt,
			&projectKey,
			&taskScope,
		); err != nil {
			return nil, err
		}
		if finishedAt.Valid {
			view.FinishedAt = &finishedAt.String
		}
		if matchesRunScope(projectKey, taskScope, resolved) {
			views = append(views, view)
		}
	}

	return views, rows.Err()
}

func (service Service) Detail(ctx context.Context, resolved scope.Resolution, runID int64) (Detail, error) {
	if service.Store == nil {
		return Detail{}, fmt.Errorf("runs service store is not configured")
	}

	run, err := service.Store.GetRun(ctx, runID)
	if err != nil {
		return Detail{}, err
	}
	task, err := service.Store.GetTask(ctx, run.TaskID)
	if err != nil {
		return Detail{}, err
	}
	project, err := service.Store.GetProject(ctx, task.ProjectID)
	if err != nil {
		return Detail{}, err
	}
	if !matchesRunScope(project.Key, task.Scope, resolved) {
		return Detail{}, sql.ErrNoRows
	}

	transcripts, err := service.Store.ListConversationTranscripts(ctx, sqlite.ListConversationTranscriptsParams{
		RunID: &run.ID,
	})
	if err != nil {
		return Detail{}, err
	}
	summaries, err := service.Store.ListMemorySummaries(ctx, sqlite.ListMemorySummariesParams{
		RunID: &run.ID,
	})
	if err != nil {
		return Detail{}, err
	}
	delegationEvidence, err := service.loadDelegationEvidence(ctx, run.ID)
	if err != nil {
		return Detail{}, err
	}

	return Detail{
		Run:             run,
		Task:            task,
		Project:         project,
		Transcripts:     transcripts,
		MemorySummaries: summaries,
		Delegations:     delegationEvidence,
	}, nil
}

func (service Service) Cancel(ctx context.Context, resolved scope.Resolution, runID int64) (Detail, error) {
	if service.Store == nil {
		return Detail{}, fmt.Errorf("runs service store is not configured")
	}

	detail, err := service.Detail(ctx, resolved, runID)
	if err != nil {
		return Detail{}, err
	}
	if detail.Run.Status != "running" {
		return detail, nil
	}

	summary := "cancelled by operator"
	if lease, leaseErr := service.Store.GetActiveWorktreeLeaseByTaskRun(ctx, detail.Task.ID, detail.Run.ID); leaseErr == nil {
		killed, killErr := terminateProcessesForWorktree(lease.WorktreePath)
		switch {
		case killErr != nil:
			summary = fmt.Sprintf("%s (process termination error: %v)", summary, killErr)
		case killed > 0:
			summary = fmt.Sprintf("%s (%d process(es) terminated)", summary, killed)
		}
		if _, releaseErr := service.Store.ReleaseWorktreeLease(ctx, sqlite.ReleaseWorktreeLeaseParams{
			LeaseID: lease.ID,
			State:   "released",
		}); releaseErr != nil {
			return Detail{}, releaseErr
		}
	} else if leaseErr != sql.ErrNoRows {
		return Detail{}, leaseErr
	}

	if _, err := service.Store.FinishRun(ctx, sqlite.FinishRunParams{
		RunID:   detail.Run.ID,
		Status:  "cancelled",
		Summary: summary,
	}); err != nil {
		return Detail{}, err
	}
	if _, err := service.Store.UpdateTaskStatus(ctx, sqlite.UpdateTaskStatusParams{
		TaskID: detail.Task.ID,
		Status: "cancelled",
	}); err != nil {
		return Detail{}, err
	}

	return service.Detail(ctx, resolved, runID)
}

func terminateProcessesForWorktree(worktreePath string) (int, error) {
	worktreePath = filepath.Clean(worktreePath)
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0, err
	}

	selfPID := os.Getpid()
	seen := map[int]struct{}{}
	var matched []int
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid == selfPID {
			continue
		}
		if processMatchesWorktree(pid, worktreePath) {
			if _, ok := seen[pid]; ok {
				continue
			}
			seen[pid] = struct{}{}
			matched = append(matched, pid)
		}
	}

	for _, pid := range matched {
		_ = syscall.Kill(pid, syscall.SIGTERM)
	}

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		allExited := true
		for _, pid := range matched {
			if processAlive(pid) {
				allExited = false
				break
			}
		}
		if allExited {
			return len(matched), nil
		}
		time.Sleep(25 * time.Millisecond)
	}

	for _, pid := range matched {
		if processAlive(pid) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	}

	return len(matched), nil
}

func processMatchesWorktree(pid int, worktreePath string) bool {
	cwd, err := os.Readlink(filepath.Join("/proc", strconv.Itoa(pid), "cwd"))
	if err == nil {
		cleaned := filepath.Clean(cwd)
		if cleaned == worktreePath || strings.HasPrefix(cleaned, worktreePath+string(os.PathSeparator)) {
			return true
		}
	}

	cmdline, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "cmdline"))
	if err == nil && strings.Contains(string(cmdline), worktreePath) {
		return true
	}

	return false
}

func processAlive(pid int) bool {
	err := syscall.Kill(pid, syscall.Signal(0))
	return err == nil
}

func matchesRunScope(projectKey, taskScope string, resolved scope.Resolution) bool {
	switch resolved.Kind {
	case scope.ScopeGlobal:
		return true
	case scope.ScopeNewProject:
		return taskScope == string(scope.ScopeNewProject)
	case scope.ScopeProject, scope.ScopeOdinCore:
		return projectKey == resolved.ProjectKey
	default:
		return false
	}
}

func (service Service) loadDelegationEvidence(ctx context.Context, runID int64) ([]DelegationEvidence, error) {
	parentDelegations, err := service.Store.ListDelegations(ctx, sqlite.ListDelegationsParams{
		ParentRunID: &runID,
	})
	if err != nil {
		return nil, err
	}
	childDelegations, err := service.Store.ListDelegations(ctx, sqlite.ListDelegationsParams{
		ChildRunID: &runID,
	})
	if err != nil {
		return nil, err
	}

	evidenceByID := make(map[int64]DelegationEvidence, len(parentDelegations)+len(childDelegations))
	order := make([]int64, 0, len(parentDelegations)+len(childDelegations))
	appendEvidence := func(relation string, delegations []sqlite.Delegation) error {
		for _, delegation := range delegations {
			entry, ok := evidenceByID[delegation.ID]
			if !ok {
				artifacts, err := service.Store.ListDelegationArtifacts(ctx, sqlite.ListDelegationArtifactsParams{
					DelegationID: delegation.ID,
				})
				if err != nil {
					return err
				}
				entry = DelegationEvidence{
					Relation:   relation,
					Delegation: delegation,
					Artifacts:  artifacts,
				}
				order = append(order, delegation.ID)
			} else if entry.Relation != relation {
				entry.Relation = "parent_child"
			}
			evidenceByID[delegation.ID] = entry
		}
		return nil
	}
	if err := appendEvidence("parent", parentDelegations); err != nil {
		return nil, err
	}
	if err := appendEvidence("child", childDelegations); err != nil {
		return nil, err
	}

	evidence := make([]DelegationEvidence, 0, len(order))
	for _, delegationID := range order {
		evidence = append(evidence, evidenceByID[delegationID])
	}
	return evidence, nil
}
