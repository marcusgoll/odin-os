package jobs

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"odin-os/internal/cli/scope"
	"odin-os/internal/core/projects"
	"odin-os/internal/executors/contract"
	executorrouter "odin-os/internal/executors/router"
	"odin-os/internal/runtime/projections"
	runsvc "odin-os/internal/runtime/runs"
	"odin-os/internal/store/sqlite"
	"odin-os/internal/tools/budgets"
	"odin-os/internal/vcs/leases"
)

type Service struct {
	Store          *sqlite.Store
	Registry       projects.Registry
	Executors      map[string]contract.Executor
	ExecutorConfig executorrouter.Config
	Transitions    projects.Service
	Runs           runsvc.Service
	Leases         leases.Manager
	Now            func() time.Time
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
	return service.runSchedulerCycle(ctx)
}

func (service Service) RunNext(ctx context.Context) error {
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

	_, err = service.runQueuedTask(ctx, task)
	return err
}

func (service Service) runQueuedTask(ctx context.Context, task sqlite.Task) (bool, error) {
	if service.Store == nil {
		return false, fmt.Errorf("job store is required")
	}

	project, err := service.Store.GetProject(ctx, task.ProjectID)
	if err != nil {
		return false, err
	}
	manifest, ok := service.Registry.Lookup(project.Key)
	if !ok {
		return false, service.failTaskWithoutRun(ctx, task.ID, fmt.Errorf("unknown manifest for project %q", project.Key))
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
		return false, err
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
		return false, service.failTaskWithoutRun(ctx, task.ID, err)
	}

	runsService := service.runsService()

	run, err := runsService.Start(ctx, task, decision.ExecutorKey)
	if err != nil {
		return false, err
	}

	finishFailure := func(cause error) error {
		if err := runsService.Fail(ctx, run.ID, cause); err != nil {
			return err
		}
		return cause
	}

	assignment := leases.Assignment{
		Mode:         "read_only",
		RepoRoot:     project.GitRoot,
		WorktreePath: project.GitRoot,
	}

	if _, err := service.Transitions.AuthorizeAction(ctx, projects.ActionInput{
		ProjectID:   project.ID,
		Actor:       projects.TransitionControllerOdinOS,
		ActionClass: projects.ActionClassIsolatedMutation,
		ActionKey:   "run_task",
	}); err != nil {
		return false, finishFailure(err)
	}
	if requirement := projects.ApprovalRequiredForAction(manifest, projects.ActionClassIsolatedMutation); requirement.Required {
		if err := service.requestApproval(ctx, task, run, requirement.Reason); err != nil {
			return false, finishFailure(err)
		}
		return false, nil
	}

	leaseManager := service.Leases
	if leaseManager.Store == nil {
		leaseManager.Store = service.Store
	}
	assignment, err = leaseManager.Prepare(ctx, leases.Request{
		Mutating:      true,
		ProjectID:     project.ID,
		ProjectKey:    project.Key,
		TaskID:        task.ID,
		RunID:         run.ID,
		RepoRoot:      project.GitRoot,
		DefaultBranch: project.DefaultBranch,
		Try:           run.Attempt,
	})
	if err != nil {
		return false, finishFailure(err)
	}
	if err := validateAssignment(manifest, project, assignment); err != nil {
		return false, finishFailure(err)
	}
	defer releaseAssignment(ctx, service.Store, assignment)

	spec.Metadata["branch_name"] = assignment.BranchName
	spec.Metadata["repo_root"] = assignment.RepoRoot
	spec.Metadata["run_id"] = fmt.Sprintf("%d", run.ID)
	spec.Metadata["run_attempt"] = fmt.Sprintf("%d", run.Attempt)
	spec.Metadata["worktree_path"] = assignment.WorktreePath

	executor := executors[decision.ExecutorKey]
	result, err := executor.RunTask(ctx, spec)
	if err != nil {
		return false, finishFailure(err)
	}
	if err := runsService.Complete(ctx, run.ID, result); err != nil {
		return false, err
	}
	return false, nil
}

func (service Service) runSchedulerCycle(ctx context.Context) error {
	if service.Store == nil {
		return fmt.Errorf("job store is required")
	}

	now := service.now()
	if err := service.demoteStalledRuns(ctx, now.Add(-service.stalledRunTimeout())); err != nil {
		return err
	}

	views, err := projections.ListTaskStatusViews(ctx, service.Store.DB())
	if err != nil {
		return err
	}
	activeRuns, err := projections.ListActiveRunViews(ctx, service.Store.DB())
	if err != nil {
		return err
	}

	activeByProject := make(map[string]int, len(activeRuns))
	for _, view := range activeRuns {
		activeByProject[view.ProjectKey]++
	}

	projectQueues := make(map[string][]int64)
	projectOrder := make([]string, 0)
	for _, view := range views {
		if view.Status != "queued" {
			continue
		}
		if _, seen := projectQueues[view.ProjectKey]; !seen {
			projectOrder = append(projectOrder, view.ProjectKey)
		}
		projectQueues[view.ProjectKey] = append(projectQueues[view.ProjectKey], view.TaskID)
	}

	var cycleErrors []error
	for _, projectKey := range projectOrder {
		taskIDs := projectQueues[projectKey]
		if len(taskIDs) == 0 {
			continue
		}

		manifest, ok := service.Registry.Lookup(projectKey)
		if !ok {
			task, err := service.Store.GetTask(ctx, taskIDs[0])
			if err != nil {
				cycleErrors = append(cycleErrors, fmt.Errorf("project %s task %d lookup: %w", projectKey, taskIDs[0], err))
				continue
			}
			occupied, err := service.runQueuedTask(ctx, task)
			if err != nil {
				cycleErrors = append(cycleErrors, fmt.Errorf("project %s task %d: %w", projectKey, task.ID, err))
				continue
			}
			if occupied {
				activeByProject[projectKey]++
			}
			continue
		}

		budget := schedulerBudget(manifest)
		startedThisCycle := 0
		for _, taskID := range taskIDs {
			if !budget.CanStart(activeByProject[projectKey], startedThisCycle) {
				break
			}

			task, err := service.Store.GetTask(ctx, taskID)
			if err != nil {
				cycleErrors = append(cycleErrors, fmt.Errorf("project %s task %d lookup: %w", projectKey, taskID, err))
				continue
			}
			occupied, err := service.runQueuedTask(ctx, task)
			if err != nil {
				cycleErrors = append(cycleErrors, fmt.Errorf("project %s task %d: %w", projectKey, task.ID, err))
				continue
			}
			startedThisCycle++
			if occupied {
				activeByProject[projectKey]++
			}
		}
	}

	return errors.Join(cycleErrors...)
}

func (service Service) demoteStalledRuns(ctx context.Context, cutoff time.Time) error {
	views, err := projections.ListStalledRunViews(ctx, service.Store.DB(), cutoff)
	if err != nil {
		return err
	}

	for _, view := range views {
		if err := service.resolveStalledRun(ctx, view); err != nil {
			return err
		}
	}

	return nil
}

func (service Service) resolveStalledRun(ctx context.Context, view projections.StalledRunView) error {
	manifest, ok := service.Registry.Lookup(view.ProjectKey)
	scheduler := defaultScheduler()
	if ok {
		scheduler = manifest.Scheduler.WithDefaults()
	}

	reason := "interrupted by stalled run recovery"
	if scheduler.StalledRunRetryLimit > 0 && view.Attempt >= scheduler.StalledRunRetryLimit {
		reason = "stalled run retry budget exhausted"
		if err := service.Store.ResolveStalledRun(ctx, sqlite.ResolveStalledRunParams{
			RunID:          view.RunID,
			TaskID:         view.TaskID,
			TaskStatus:     "dead_letter",
			Summary:        reason,
			TerminalReason: reason,
			ArtifactsJSON:  "[]",
		}); err != nil {
			return err
		}
		return nil
	}

	if err := service.Store.ResolveStalledRun(ctx, sqlite.ResolveStalledRunParams{
		RunID:          view.RunID,
		TaskID:         view.TaskID,
		TaskStatus:     "queued",
		Summary:        "",
		TerminalReason: "",
		ArtifactsJSON:  "[]",
	}); err != nil {
		return err
	}

	return nil
}

func schedulerBudget(manifest projects.Manifest) budgets.SchedulerBudget {
	settings := manifest.Scheduler.WithDefaults()
	return budgets.SchedulerBudget{
		MaxConcurrentRuns: settings.MaxConcurrentRuns,
		MaxStartsPerCycle: settings.MaxStartsPerCycle,
	}
}

func defaultScheduler() projects.Scheduler {
	return projects.Scheduler{}.WithDefaults()
}

func (service Service) stalledRunTimeout() time.Duration {
	return 30 * time.Minute
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
	row := service.Store.DB().QueryRowContext(ctx, `
		SELECT id
		FROM tasks
		WHERE status = 'queued'
		ORDER BY id ASC
		LIMIT 1
	`)

	var taskID int64
	if err := row.Scan(&taskID); err != nil {
		return sqlite.Task{}, err
	}
	return service.Store.GetTask(ctx, taskID)
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

func (service Service) runsService() runsvc.Service {
	runsService := service.Runs
	if runsService.Store == nil {
		runsService.Store = service.Store
	}
	if runsService.DB == nil && service.Store != nil {
		runsService.DB = service.Store.DB()
	}
	return runsService
}

func (service Service) failTaskWithoutRun(ctx context.Context, taskID int64, cause error) error {
	message := strings.TrimSpace(cause.Error())
	if message == "" {
		message = "failed"
	}
	_, _ = service.Store.UpdateTaskStatus(ctx, sqlite.UpdateTaskStatusParams{
		TaskID:         taskID,
		Status:         "failed",
		Summary:        message,
		TerminalReason: message,
		ArtifactsJSON:  "[]",
	})
	return cause
}

func normalizeRouteName(targetKey string) string {
	targetKey = strings.TrimSpace(targetKey)
	targetKey = strings.TrimPrefix(targetKey, "router/")
	if targetKey == "" {
		return "default"
	}
	return targetKey
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

func releaseAssignment(ctx context.Context, store *sqlite.Store, assignment leases.Assignment) {
	if assignment.LeaseID == nil {
		return
	}
	_, _ = store.ReleaseWorktreeLease(ctx, sqlite.ReleaseWorktreeLeaseParams{
		LeaseID: *assignment.LeaseID,
		State:   "released",
	})
}

func (service Service) requestApproval(ctx context.Context, task sqlite.Task, run sqlite.Run, reason string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "approval required"
	}

	_, _, _, err := service.Store.AwaitApproval(ctx, sqlite.AwaitApprovalParams{
		TaskID:         task.ID,
		RunID:          run.ID,
		RequestedBy:    string(projects.TransitionControllerOdinOS),
		Summary:        reason,
		TerminalReason: reason,
		ArtifactsJSON:  "[]",
	})
	return err
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
