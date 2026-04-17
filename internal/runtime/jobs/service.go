package jobs

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"odin-os/internal/cli/scope"
	"odin-os/internal/core/projects"
	"odin-os/internal/executors/contract"
	executorrouter "odin-os/internal/executors/router"
	healthsvc "odin-os/internal/runtime/health"
	"odin-os/internal/runtime/projections"
	"odin-os/internal/store/sqlite"
	"odin-os/internal/vcs/leases"
)

type Service struct {
	Store          *sqlite.Store
	Registry       projects.Registry
	Executors      map[string]contract.Executor
	ExecutorConfig executorrouter.Config
	Transitions    projects.Service
	Leases         leases.Manager
	Now            func() time.Time
}

type admissionOutcome string

const (
	admissionDispatchable admissionOutcome = "dispatchable"
	admissionBlocked      admissionOutcome = "blocked"
	admissionFailed       admissionOutcome = "failed"
	admissionRetryLater   admissionOutcome = "retry_later"
)

type admissionDecision struct {
	Outcome        admissionOutcome
	BlockedReason  string
	LastError      string
	NextEligibleAt time.Time
}

func (service Service) List(ctx context.Context, resolved scope.Resolution) ([]projections.TaskStatusView, error) {
	views, err := projections.ListTaskStatusViews(ctx, service.Store.DB())
	if err != nil {
		return nil, err
	}

	filtered := make([]projections.TaskStatusView, 0, len(views))
	for _, view := range views {
		if matchesTaskScope(view.ProjectKey, view.Scope, resolved) {
			filtered = append(filtered, view)
		}
	}

	return filtered, nil
}

func (service Service) CreateTaskFromAct(ctx context.Context, resolved scope.Resolution, title string) (sqlite.Task, error) {
	if resolved.Kind == scope.ScopeGlobal {
		return sqlite.Task{}, fmt.Errorf("act mode requires a non-global scope")
	}

	projectManifest, taskScope, err := service.taskOwnerForScope(resolved)
	if err != nil {
		return sqlite.Task{}, err
	}

	project, err := service.ensureRuntimeProject(ctx, projectManifest)
	if err != nil {
		return sqlite.Task{}, err
	}

	now := time.Now().UTC()
	if service.Now != nil {
		now = service.Now().UTC()
	}

	return service.Store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         fmt.Sprintf("%s-%s", slugify(title), now.Format("20060102-150405")),
		Title:       title,
		Status:      "queued",
		Scope:       taskScope,
		RequestedBy: "operator",
	})
}

func (service Service) ExecuteNextQueued(ctx context.Context) error {
	if service.Store == nil {
		return fmt.Errorf("job store is required")
	}

	task, err := service.nextQueuedTask(ctx)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		return err
	}

	project, err := service.Store.GetProject(ctx, task.ProjectID)
	if err != nil {
		return err
	}
	manifest, ok := service.Registry.Lookup(project.Key)
	if !ok {
		return service.applyAdmissionDecision(ctx, task, admissionDecision{
			Outcome:   admissionFailed,
			LastError: fmt.Sprintf("unknown manifest for project %q", project.Key),
		})
	}

	executors := service.Executors
	if len(executors) == 0 {
		executors = executorrouter.DefaultCatalog()
	}
	if service.Transitions.Store == nil {
		service.Transitions = projects.Service{Store: service.Store}
	}

	config, err := service.executionConfig(ctx)
	if err != nil {
		return err
	}
	selector := executorrouter.Selector{
		Config:    config,
		Executors: executors,
	}
	spec := contract.TaskSpec{
		ID:     task.Key,
		Kind:   contract.TaskKindGeneral,
		Scope:  task.Scope,
		Prompt: task.Title,
		Requirements: contract.Requirements{
			AllowedClasses:    []contract.ExecutorClass{contract.ExecutorClassPlanBackedCLI},
			NeedsHeadlessPlan: true,
		},
		Metadata: map[string]string{
			"project_key": project.Key,
			"task_id":     fmt.Sprintf("%d", task.ID),
		},
	}

	decision, err := selector.Select(ctx, spec)
	if err != nil {
		return service.applyAdmissionDecision(ctx, task, admissionDecision{
			Outcome:   admissionFailed,
			LastError: err.Error(),
		})
	}

	admission, err := service.admitTask(ctx, task, project, manifest, decision.ExecutorKey)
	if err != nil {
		return err
	}
	if admission.Outcome != admissionDispatchable {
		return service.applyAdmissionDecision(ctx, task, admission)
	}

	attempt, err := service.nextRunAttempt(ctx, task.ID)
	if err != nil {
		return err
	}

	run, err := service.prepareRun(ctx, task, decision.ExecutorKey, attempt)
	if err != nil {
		return err
	}

	assignment, leaseAdmission, err := service.prepareLease(ctx, task, project, manifest, run, attempt)
	if err != nil {
		return err
	}
	if leaseAdmission.Outcome != admissionDispatchable {
		return service.finalizeOutcome(ctx, task, run, leaseAdmission, contract.ExecutionResult{}, nil)
	}
	defer releaseAssignment(ctx, service.Store, assignment)

	if _, run, err = service.Store.UpdateRunAndTaskStatus(ctx, sqlite.UpdateRunAndTaskStatusParams{
		RunID:      run.ID,
		RunStatus:  "running",
		TaskStatus: "running",
	}); err != nil {
		return err
	}

	spec.Metadata["branch_name"] = assignment.BranchName
	spec.Metadata["repo_root"] = assignment.RepoRoot
	spec.Metadata["worktree_path"] = assignment.WorktreePath

	executor := executors[decision.ExecutorKey]
	result, err := executor.RunTask(ctx, spec)
	return service.finalizeOutcome(ctx, task, run, admissionDecision{}, result, err)
}

func (service Service) now() time.Time {
	if service.Now != nil {
		return service.Now().UTC()
	}
	return time.Now().UTC()
}

func (service Service) ensureRuntimeProject(ctx context.Context, manifest projects.Manifest) (sqlite.Project, error) {
	project, err := service.Store.GetProjectByKey(ctx, manifest.Key)
	if err == nil {
		return project, nil
	}
	if err != sql.ErrNoRows {
		return sqlite.Project{}, err
	}

	scopeValue := "project"
	if manifest.SystemProject {
		scopeValue = "odin-core"
	}

	return service.Store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           manifest.Key,
		Name:          manifest.Name,
		Scope:         scopeValue,
		GitRoot:       manifest.GitRoot,
		DefaultBranch: manifest.DefaultBranch,
		GitHubRepo:    manifest.GitHub.Repo,
		ManifestPath:  manifest.SourcePath,
	})
}

func (service Service) taskOwnerForScope(resolved scope.Resolution) (projects.Manifest, string, error) {
	switch resolved.Kind {
	case scope.ScopeProject, scope.ScopeOdinCore:
		project, ok := service.Registry.Lookup(resolved.ProjectKey)
		if !ok {
			return projects.Manifest{}, "", fmt.Errorf("unknown project %q", resolved.ProjectKey)
		}
		return project, string(resolved.Kind), nil
	case scope.ScopeNewProject:
		project, ok := service.Registry.SystemProject()
		if !ok {
			return projects.Manifest{}, "", fmt.Errorf("new-project scope requires odin-core")
		}
		return project, string(scope.ScopeNewProject), nil
	default:
		return projects.Manifest{}, "", fmt.Errorf("unsupported scope %q", resolved.Kind)
	}
}

func matchesTaskScope(projectKey, taskScope string, resolved scope.Resolution) bool {
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

func (service Service) nextQueuedTask(ctx context.Context) (sqlite.Task, error) {
	tasks, err := service.Store.ListEligibleQueuedTasks(ctx, service.now())
	if err != nil {
		return sqlite.Task{}, err
	}
	if len(tasks) == 0 {
		return sqlite.Task{}, sql.ErrNoRows
	}
	return tasks[0], nil
}

func (service Service) nextRunAttempt(ctx context.Context, taskID int64) (int, error) {
	row := service.Store.DB().QueryRowContext(ctx, `
		SELECT COALESCE(MAX(attempt), 0) + 1
		FROM runs
		WHERE task_id = ?
	`, taskID)
	var attempt int
	if err := row.Scan(&attempt); err != nil {
		return 0, err
	}
	return attempt, nil
}

func (service Service) executionConfig(ctx context.Context) (executorrouter.Config, error) {
	config := service.ExecutorConfig
	promotions, err := service.Store.ListActiveLearningPromotions(ctx)
	if err != nil {
		return executorrouter.Config{}, err
	}
	if len(promotions) == 0 {
		return config, nil
	}

	refinements := make([]executorrouter.RoutingRefinement, 0, len(promotions))
	for _, promotion := range promotions {
		if promotion.ProposalType != "routing_rule_refinement" {
			continue
		}
		proposal, err := service.Store.GetLearningProposal(ctx, promotion.ProposalID)
		if err != nil {
			return executorrouter.Config{}, err
		}
		routeName := normalizeRouteName(proposal.TargetKey)
		refinement, err := executorrouter.ParseRoutingRefinementChange(proposal.ChangePayloadJSON, routeName, promotion.ID)
		if err != nil {
			return executorrouter.Config{}, err
		}
		refinements = append(refinements, refinement)
	}

	return executorrouter.ApplyRoutingRefinements(config, refinements)
}

func normalizeRouteName(targetKey string) string {
	targetKey = strings.TrimSpace(targetKey)
	targetKey = strings.TrimPrefix(targetKey, "router/")
	if targetKey == "" {
		return "default"
	}
	return targetKey
}

func (service Service) admitTask(ctx context.Context, task sqlite.Task, project sqlite.Project, manifest projects.Manifest, executorKey string) (admissionDecision, error) {
	if requiresExplicitApproval(manifest) {
		approval, err := service.latestTaskApproval(ctx, task.ID)
		if err != nil {
			return admissionDecision{}, err
		}
		switch approval.Status {
		case "approved":
		case "pending":
			return admissionDecision{Outcome: admissionBlocked, BlockedReason: "approval_required"}, nil
		case "":
			if _, err := service.Store.RequestApproval(ctx, sqlite.RequestApprovalParams{
				TaskID:      task.ID,
				Status:      "pending",
				RequestedBy: "system",
			}); err != nil {
				return admissionDecision{}, err
			}
			return admissionDecision{Outcome: admissionBlocked, BlockedReason: "approval_required"}, nil
		default:
			return admissionDecision{
				Outcome:   admissionFailed,
				LastError: fmt.Sprintf("approval for task %d is %s", task.ID, approval.Status),
			}, nil
		}
	}

	executorCheck, _, err := healthsvc.Service{
		DB:     service.Store.DB(),
		Config: healthsvc.DefaultConfig(),
		Now:    service.now,
	}.ExecutorStatus(ctx, executorKey)
	if err != nil {
		return admissionDecision{}, err
	}
	if executorCheck.Status != healthsvc.StatusHealthy {
		return admissionDecision{
			Outcome:       admissionBlocked,
			BlockedReason: "executor_unavailable",
			LastError:     executorCheck.Summary,
		}, nil
	}

	if _, err := service.Transitions.AuthorizeAction(ctx, projects.ActionInput{
		ProjectID:   project.ID,
		Actor:       projects.TransitionControllerOdinOS,
		ActionClass: projects.ActionClassIsolatedMutation,
		ActionKey:   "run_task",
	}); err != nil {
		return admissionDecision{
			Outcome:   admissionFailed,
			LastError: fmt.Sprintf("transition_denied: %v", err),
		}, nil
	}

	return admissionDecision{Outcome: admissionDispatchable}, nil
}

func (service Service) prepareRun(ctx context.Context, task sqlite.Task, executorKey string, attempt int) (sqlite.Run, error) {
	run, err := service.Store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:     task.ID,
		Executor:   executorKey,
		Attempt:    attempt,
		Status:     "preparing",
		TaskStatus: "preparing",
	})
	if err != nil {
		return sqlite.Run{}, err
	}
	return run, nil
}

func (service Service) prepareLease(ctx context.Context, task sqlite.Task, project sqlite.Project, manifest projects.Manifest, run sqlite.Run, attempt int) (leases.Assignment, admissionDecision, error) {
	assignment := leases.Assignment{
		Mode:         "read_only",
		RepoRoot:     project.GitRoot,
		WorktreePath: project.GitRoot,
	}

	leaseManager := service.Leases
	if leaseManager.Store == nil {
		leaseManager.Store = service.Store
	}
	assignment, err := leaseManager.Prepare(ctx, leases.Request{
		Mutating:      true,
		ProjectID:     project.ID,
		ProjectKey:    project.Key,
		TaskID:        task.ID,
		RunID:         run.ID,
		RepoRoot:      project.GitRoot,
		DefaultBranch: project.DefaultBranch,
		Try:           attempt,
	})
	if err != nil {
		if errors.Is(err, sqlite.ErrWorktreeLeaseConflict) {
			return leases.Assignment{}, admissionDecision{
				Outcome:        admissionRetryLater,
				LastError:      err.Error(),
				NextEligibleAt: service.now().Add(time.Second),
			}, nil
		}
		return leases.Assignment{}, admissionDecision{}, err
	}
	if err := validateAssignment(manifest, project, assignment); err != nil {
		if cleanupErr := releaseAssignment(ctx, service.Store, assignment); cleanupErr != nil {
			return leases.Assignment{}, admissionDecision{}, cleanupErr
		}
		return leases.Assignment{}, admissionDecision{
			Outcome:   admissionFailed,
			LastError: fmt.Sprintf("policy_denied: %v", err),
		}, nil
	}
	return assignment, admissionDecision{Outcome: admissionDispatchable}, nil
}

func (service Service) finalizeOutcome(ctx context.Context, task sqlite.Task, run sqlite.Run, admission admissionDecision, result contract.ExecutionResult, execErr error) error {
	if admission.Outcome != "" && admission.Outcome != admissionDispatchable {
		switch admission.Outcome {
		case admissionFailed:
			_, _, err := service.Store.FinishRunAndSetTaskStatus(ctx, sqlite.FinishRunAndSetTaskStatusParams{
				RunID:      run.ID,
				RunStatus:  "failed",
				Summary:    admission.LastError,
				TaskStatus: "failed",
			})
			return err
		case admissionRetryLater:
			_, _, err := service.Store.FailRunAndRetryTask(ctx, sqlite.FailRunAndRetryTaskParams{
				RunID:          run.ID,
				Summary:        admission.LastError,
				LastError:      admission.LastError,
				NextEligibleAt: admission.NextEligibleAt,
			})
			return err
		default:
			return fmt.Errorf("unsupported admission outcome %q", admission.Outcome)
		}
	}

	if execErr != nil {
		if isTransientFailure(execErr) {
			if task.RetryCount+1 >= task.MaxAttempts {
				_, _, err := service.Store.FinishRunAndSetTaskStatus(ctx, sqlite.FinishRunAndSetTaskStatusParams{
					RunID:      run.ID,
					RunStatus:  "failed",
					Summary:    execErr.Error(),
					TaskStatus: "failed",
				})
				if err != nil {
					return err
				}
				return execErr
			}
			nextRetryCount := task.RetryCount + 1
			nextEligibleAt := service.now().Add(retryDelay(nextRetryCount))
			_, _, err := service.Store.FailRunAndRetryTask(ctx, sqlite.FailRunAndRetryTaskParams{
				RunID:          run.ID,
				Summary:        execErr.Error(),
				LastError:      execErr.Error(),
				NextEligibleAt: nextEligibleAt,
			})
			return err
		}

		_, _, err := service.Store.FinishRunAndSetTaskStatus(ctx, sqlite.FinishRunAndSetTaskStatusParams{
			RunID:      run.ID,
			RunStatus:  "failed",
			Summary:    execErr.Error(),
			TaskStatus: "failed",
		})
		if err != nil {
			return err
		}
		return execErr
	}

	runStatus := result.Status
	if runStatus == "" {
		runStatus = "completed"
	}
	taskStatus := "completed"
	if runStatus != "completed" {
		taskStatus = "failed"
	}

	_, _, err := service.Store.FinishRunAndSetTaskStatus(ctx, sqlite.FinishRunAndSetTaskStatusParams{
		RunID:      run.ID,
		RunStatus:  runStatus,
		Summary:    result.Output,
		TaskStatus: taskStatus,
	})
	return err
}

func (service Service) applyAdmissionDecision(ctx context.Context, task sqlite.Task, decision admissionDecision) error {
	switch decision.Outcome {
	case admissionBlocked:
		_, err := service.Store.BlockTask(ctx, sqlite.BlockTaskParams{
			TaskID: task.ID,
			Reason: decision.BlockedReason,
		})
		return err
	case admissionFailed:
		_, err := service.Store.UpdateTaskQueueState(ctx, sqlite.UpdateTaskQueueStateParams{
			TaskID:         task.ID,
			Status:         "failed",
			NextEligibleAt: time.Time{},
			Priority:       task.Priority,
			LastError:      decision.LastError,
			RetryCount:     task.RetryCount,
			MaxAttempts:    task.MaxAttempts,
			BlockedReason:  "",
		})
		return err
	case admissionRetryLater:
		_, err := service.Store.UpdateTaskQueueState(ctx, sqlite.UpdateTaskQueueStateParams{
			TaskID:         task.ID,
			Status:         "queued",
			NextEligibleAt: decision.NextEligibleAt,
			Priority:       task.Priority,
			LastError:      decision.LastError,
			RetryCount:     task.RetryCount,
			MaxAttempts:    task.MaxAttempts,
			BlockedReason:  "",
		})
		return err
	case admissionDispatchable:
		return nil
	default:
		return fmt.Errorf("unsupported admission outcome %q", decision.Outcome)
	}
}

func (service Service) latestTaskApproval(ctx context.Context, taskID int64) (sqlite.Approval, error) {
	approval, err := service.Store.GetLatestTaskApproval(ctx, taskID)
	if err == nil {
		return approval, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return sqlite.Approval{}, nil
	}
	return sqlite.Approval{}, err
}

func requiresExplicitApproval(manifest projects.Manifest) bool {
	return manifest.SystemProject &&
		manifest.Policy.ApprovalGates.RequireForSystemProjectChanges != nil &&
		*manifest.Policy.ApprovalGates.RequireForSystemProjectChanges
}

func validateAssignment(manifest projects.Manifest, project sqlite.Project, assignment leases.Assignment) error {
	if manifest.Policy.BranchRules.RequireWorktree != nil && *manifest.Policy.BranchRules.RequireWorktree && assignment.WorktreePath == project.GitRoot {
		return fmt.Errorf("project %q requires an isolated worktree", manifest.Key)
	}
	if manifest.Policy.BranchRules.RequireTaskBranch != nil && *manifest.Policy.BranchRules.RequireTaskBranch && assignment.BranchName == "" {
		return fmt.Errorf("project %q requires a task-owned branch", manifest.Key)
	}
	if manifest.Policy.BranchRules.AllowDefaultBranchMutation != nil && !*manifest.Policy.BranchRules.AllowDefaultBranchMutation && assignment.BranchName == project.DefaultBranch {
		return fmt.Errorf("project %q cannot mutate the default branch directly", manifest.Key)
	}
	return nil
}

func releaseAssignment(ctx context.Context, store *sqlite.Store, assignment leases.Assignment) error {
	if assignment.LeaseID == nil {
		return nil
	}
	_, err := store.ReleaseWorktreeLease(ctx, sqlite.ReleaseWorktreeLeaseParams{
		LeaseID: *assignment.LeaseID,
		State:   "released",
	})
	return err
}

func retryDelay(retryCount int) time.Duration {
	if retryCount <= 1 {
		return time.Second
	}

	delay := time.Second
	for attempt := 1; attempt < retryCount; attempt++ {
		if delay >= time.Minute {
			return time.Minute
		}
		delay *= 2
	}
	if delay > time.Minute {
		return time.Minute
	}
	return delay
}

func isTransientFailure(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || os.IsTimeout(err) {
		return true
	}

	var netErr net.Error
	if errors.As(err, &netErr) && (netErr.Timeout() || netErr.Temporary()) {
		return true
	}

	return false
}

func slugify(input string) string {
	input = strings.ToLower(strings.TrimSpace(input))
	var builder strings.Builder
	lastDash := false

	for _, character := range input {
		switch {
		case character >= 'a' && character <= 'z':
			builder.WriteRune(character)
			lastDash = false
		case character >= '0' && character <= '9':
			builder.WriteRune(character)
			lastDash = false
		default:
			if !lastDash && builder.Len() > 0 {
				builder.WriteByte('-')
				lastDash = true
			}
		}
	}

	result := strings.Trim(builder.String(), "-")
	if result == "" {
		return "task"
	}
	return result
}
