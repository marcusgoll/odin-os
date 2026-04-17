package jobs

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"odin-os/internal/cli/scope"
	coreinitiatives "odin-os/internal/core/initiatives"
	"odin-os/internal/core/projects"
	coreworkspaces "odin-os/internal/core/workspaces"
	"odin-os/internal/executors/contract"
	executorrouter "odin-os/internal/executors/router"
	"odin-os/internal/runtime/projections"
	runsvc "odin-os/internal/runtime/runs"
	"odin-os/internal/store/sqlite"
	"odin-os/internal/tools/budgets"
	"odin-os/internal/vcs/leases"
)

const defaultWorkspaceKey = "marcus"

type Service struct {
	Store          *sqlite.Store
	Registry       projects.Registry
	Executors      map[string]contract.Executor
	ExecutorConfig executorrouter.Config
	RuntimeRoot    string
	Transitions    projects.Service
	Runs           runsvc.Service
	Leases         leases.Manager
	Now            func() time.Time
}

type ExecutionOutcome struct {
	Task sqlite.Task
	Run  *sqlite.Run
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

	actionKey, title, err := parseActInput(title)
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
		ActionKey:   actionKey,
		Status:      "queued",
		Scope:       taskScope,
		RequestedBy: "operator",
	})
}

func (service Service) CreateTaskFromProjectKey(ctx context.Context, projectKey string, title string) (sqlite.Task, error) {
	manifest, ok := service.Registry.Lookup(projectKey)
	if !ok {
		return sqlite.Task{}, fmt.Errorf("unknown project %q", projectKey)
	}

	resolved := scope.Resolve(scope.ResolveInput{
		ExplicitTarget: &scope.Target{
			ProjectKey:    manifest.Key,
			SystemProject: manifest.SystemProject,
		},
	})
	return service.CreateTaskFromAct(ctx, resolved, title)
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

func (service Service) ExecuteTask(ctx context.Context, taskID int64) (ExecutionOutcome, error) {
	if service.Store == nil {
		return ExecutionOutcome{}, fmt.Errorf("job store is required")
	}

	task, err := service.Store.GetTask(ctx, taskID)
	if err != nil {
		return ExecutionOutcome{}, err
	}

	return service.runQueuedTask(ctx, task)
}

func (service Service) runQueuedTask(ctx context.Context, task sqlite.Task) (ExecutionOutcome, error) {
	if service.Store == nil {
		return ExecutionOutcome{}, fmt.Errorf("job store is required")
	}

	project, err := service.Store.GetProject(ctx, task.ProjectID)
	if err != nil {
		return ExecutionOutcome{}, err
	}
	manifest, ok := service.Registry.Lookup(project.Key)
	if !ok {
		return service.failTaskWithoutRun(ctx, task.ID, fmt.Errorf("unknown manifest for project %q", project.Key))
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
		return ExecutionOutcome{}, err
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
	actionKey := runtimeActionKey(task.ActionKey)

	if err := service.ensureHarnessDriverConfigured(ctx, config, executors, spec); err != nil {
		return service.failTaskWithoutRun(ctx, task.ID, err)
	}
	if strings.TrimSpace(service.RuntimeRoot) != "" {
		spec.Metadata["runtime_root"] = service.RuntimeRoot
	}

	decision, err := selector.Select(ctx, spec)
	if err != nil {
		return service.failTaskWithoutRun(ctx, task.ID, err)
	}

	runsService := service.runsService()

	run, err := runsService.Start(ctx, task, decision.ExecutorKey)
	if err != nil {
		return ExecutionOutcome{}, err
	}

	finishFailure := func(cause error) (ExecutionOutcome, error) {
		if err := runsService.Fail(ctx, run.ID, cause); err != nil {
			return ExecutionOutcome{}, err
		}
		outcome, loadErr := service.executionOutcome(ctx, task.ID, run.ID)
		if loadErr == nil {
			if persistErr := service.recordExecutionMemory(ctx, project, outcome.Task, outcome.Run, task.Title); persistErr != nil {
				return outcome, persistErr
			}
			return outcome, cause
		}
		failedRun := run
		failedRun.Status = "failed"
		failedRun.Summary = cause.Error()
		return ExecutionOutcome{
			Task: sqlite.Task{
				ID:        task.ID,
				ProjectID: task.ProjectID,
				Key:       task.Key,
				Title:     task.Title,
				Status:    "failed",
				Scope:     task.Scope,
			},
			Run: &failedRun,
		}, cause
	}

	assignment := leases.Assignment{
		Mode:         "read_only",
		RepoRoot:     project.GitRoot,
		WorktreePath: project.GitRoot,
	}

	if task.ActionKey != "" && !projects.SupportsLimitedAction(manifest, task.ActionKey) {
		return finishFailure(fmt.Errorf("action key %q is not supported by project policy", task.ActionKey))
	}
	if task.ActionKey != "" {
		return finishFailure(fmt.Errorf("action key %q is not enabled on this line", task.ActionKey))
	}
	if _, err := service.Transitions.AuthorizeAction(ctx, projects.ActionInput{
		ProjectID:   project.ID,
		Actor:       projects.TransitionControllerOdinOS,
		ActionClass: projects.ActionClassIsolatedMutation,
		ActionKey:   actionKey,
	}); err != nil {
		return finishFailure(err)
	}
	if requirement := projects.ApprovalRequiredForAction(manifest, projects.ActionClassIsolatedMutation); requirement.Required {
		if err := service.requestApproval(ctx, task, run, requirement.Reason); err != nil {
			return finishFailure(err)
		}
		return service.executionOutcome(ctx, task.ID, run.ID)
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
		return finishFailure(err)
	}
	if err := validateAssignment(manifest, project, assignment); err != nil {
		return finishFailure(err)
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
		return finishFailure(err)
	}
	if err := runsService.Complete(ctx, run.ID, result); err != nil {
		return ExecutionOutcome{}, err
	}

	outcome, err := service.executionOutcome(ctx, task.ID, run.ID)
	if err != nil {
		return ExecutionOutcome{}, err
	}
	if err := service.recordExecutionMemory(ctx, project, outcome.Task, outcome.Run, task.Title); err != nil {
		return ExecutionOutcome{}, err
	}
	return outcome, nil
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
			_, err = service.runQueuedTask(ctx, task)
			if err != nil {
				cycleErrors = append(cycleErrors, fmt.Errorf("project %s task %d: %w", projectKey, task.ID, err))
			}
			updatedActiveByProject, refreshErr := service.activeRunsByProject(ctx)
			if refreshErr != nil {
				cycleErrors = append(cycleErrors, fmt.Errorf("project %s active refresh: %w", projectKey, refreshErr))
				return errors.Join(cycleErrors...)
			}
			activeByProject = updatedActiveByProject
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
			_, err = service.runQueuedTask(ctx, task)
			if err != nil {
				cycleErrors = append(cycleErrors, fmt.Errorf("project %s task %d: %w", projectKey, task.ID, err))
			}
			startedThisCycle++
			updatedActiveByProject, refreshErr := service.activeRunsByProject(ctx)
			if refreshErr != nil {
				cycleErrors = append(cycleErrors, fmt.Errorf("project %s active refresh: %w", projectKey, refreshErr))
				return errors.Join(cycleErrors...)
			}
			activeByProject = updatedActiveByProject
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
			if errors.Is(err, sql.ErrNoRows) {
				return nil
			}
			return err
		}
		return nil
	}

	if err := service.Store.ResolveStalledRun(ctx, sqlite.ResolveStalledRunParams{
		RunID:          view.RunID,
		TaskID:         view.TaskID,
		TaskStatus:     "queued",
		Summary:        "",
		TerminalReason: reason,
		ArtifactsJSON:  "[]",
	}); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}
	return nil
}

func parseActInput(input string) (string, string, error) {
	input = strings.TrimSpace(input)
	if !strings.HasPrefix(strings.ToLower(input), "action:") {
		return "", input, nil
	}

	parts := strings.Fields(input)
	if len(parts) == 0 {
		return "", "", fmt.Errorf("act input is required")
	}

	key := strings.TrimSpace(parts[0][len("action:"):])
	if key == "" {
		return "", "", fmt.Errorf("explicit action key is required after action:")
	}
	if len(parts) == 1 {
		return "", "", fmt.Errorf("act input with action:%s requires a task title", key)
	}
	return key, strings.Join(parts[1:], " "), nil
}

func runtimeActionKey(actionKey string) string {
	if actionKey == "" {
		return "run_task"
	}
	return actionKey
}

func (service Service) activeRunsByProject(ctx context.Context) (map[string]int, error) {
	activeRuns, err := projections.ListActiveRunViews(ctx, service.Store.DB())
	if err != nil {
		return nil, err
	}

	activeByProject := make(map[string]int, len(activeRuns))
	for _, view := range activeRuns {
		activeByProject[view.ProjectKey]++
	}
	return activeByProject, nil
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

func (service Service) EnsureRuntimeProject(ctx context.Context, manifest projects.Manifest) (sqlite.Project, error) {
	return service.ensureRuntimeProject(ctx, manifest)
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

func (service Service) executionOutcome(ctx context.Context, taskID int64, runID int64) (ExecutionOutcome, error) {
	task, err := service.Store.GetTask(ctx, taskID)
	if err != nil {
		return ExecutionOutcome{}, err
	}
	run, err := service.Store.GetRun(ctx, runID)
	if err != nil {
		return ExecutionOutcome{}, err
	}
	return ExecutionOutcome{
		Task: task,
		Run:  &run,
	}, nil
}

func (service Service) recordExecutionMemory(ctx context.Context, project sqlite.Project, task sqlite.Task, run *sqlite.Run, prompt string) error {
	if run == nil {
		return nil
	}
	workspaceID, initiativeID, err := service.memoryOwnersForProject(ctx, project.ID)
	if err != nil {
		return err
	}
	responseText := strings.TrimSpace(run.Summary)
	if responseText == "" {
		responseText = fmt.Sprintf("Task %s finished with status %s.", task.Key, run.Status)
	}

	toolSummaryBytes, err := json.Marshal(map[string]string{
		"executor":    run.Executor,
		"run_status":  run.Status,
		"task_status": task.Status,
	})
	if err != nil {
		return err
	}
	transcript, err := service.Store.RecordConversationTranscript(ctx, sqlite.RecordConversationTranscriptParams{
		ProjectID:    &project.ID,
		WorkspaceID:  workspaceID,
		InitiativeID: initiativeID,
		TaskID:       &task.ID,
		RunID:        &run.ID,
		Scope:        task.Scope,
		ScopeKey:     project.Key,
		Mode:         "act",
		Prompt:       strings.TrimSpace(prompt),
		Response:     responseText,
		ToolSummary:  string(toolSummaryBytes),
		Executor:     run.Executor,
	})
	if err != nil {
		return err
	}

	detailsBytes, err := json.Marshal(map[string]string{
		"task_key":    task.Key,
		"task_status": task.Status,
		"run_status":  run.Status,
		"executor":    run.Executor,
		"prompt":      strings.TrimSpace(prompt),
	})
	if err != nil {
		return err
	}

	summaryText := strings.TrimSpace(run.Summary)
	if summaryText == "" {
		summaryText = fmt.Sprintf("Task %s finished with status %s.", task.Key, run.Status)
	} else {
		summaryText = fmt.Sprintf("Task %s %s via %s: %s", task.Key, run.Status, run.Executor, summaryText)
	}

	_, err = service.Store.RecordMemorySummary(ctx, sqlite.RecordMemorySummaryParams{
		ProjectID:          &project.ID,
		WorkspaceID:        workspaceID,
		InitiativeID:       initiativeID,
		SourceTranscriptID: &transcript.ID,
		TaskID:             &task.ID,
		RunID:              &run.ID,
		Scope:              task.Scope,
		ScopeKey:           project.Key,
		VisibilityScope:    "initiative",
		RetentionClass:     "episodic",
		MemoryType:         "episode",
		Summary:            summaryText,
		DetailsJSON:        string(detailsBytes),
	})
	return err
}

func (service Service) memoryOwnersForProject(ctx context.Context, projectID int64) (*int64, *int64, error) {
	if service.Store == nil {
		return nil, nil, nil
	}

	workspace, err := coreworkspaces.Service{Store: service.Store}.GetByKey(ctx, defaultWorkspaceKey)
	switch {
	case err == nil:
	case err == sql.ErrNoRows:
		return nil, nil, nil
	default:
		return nil, nil, err
	}

	workspaceID := workspace.ID
	initiatives, err := coreinitiatives.Service{Store: service.Store}.ListByWorkspace(ctx, workspace.ID)
	if err != nil {
		return nil, nil, err
	}
	for _, initiative := range initiatives {
		if initiative.LinkedProjectID != nil && *initiative.LinkedProjectID == projectID {
			initiativeID := initiative.ID
			return &workspaceID, &initiativeID, nil
		}
	}
	return &workspaceID, nil, nil
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

func (service Service) ensureHarnessDriverConfigured(ctx context.Context, config executorrouter.Config, executors map[string]contract.Executor, spec contract.TaskSpec) error {
	if !spec.Requirements.NeedsHeadlessPlan {
		return nil
	}

	route, ok := matchRouteConfig(config, spec)
	if !ok {
		return nil
	}

	order := append([]string{}, route.Preferred...)
	order = append(order, route.Fallback...)

	headlessCandidates := 0
	for _, key := range order {
		executorConfig, ok := config.ExecutorByKey(key)
		if !ok || !executorConfig.Enabled || executorConfig.Class != contract.ExecutorClassPlanBackedCLI {
			continue
		}
		executor, ok := executors[key]
		if !ok {
			continue
		}
		capabilities, err := executor.Capabilities(ctx)
		if err != nil || !capabilities.Matches(spec) {
			continue
		}
		headlessCandidates++

		health, err := executor.Health(ctx)
		if err != nil {
			continue
		}
		if health.Status == contract.HealthStatusHealthy || health.Status == contract.HealthStatusDegraded {
			return nil
		}
	}

	if headlessCandidates == 0 {
		return nil
	}

	return fmt.Errorf("no harness driver configured for route %q", route.Name)
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

func (service Service) failTaskWithoutRun(ctx context.Context, taskID int64, cause error) (ExecutionOutcome, error) {
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
	task, err := service.Store.GetTask(ctx, taskID)
	if err == nil {
		return ExecutionOutcome{Task: task}, cause
	}
	return ExecutionOutcome{}, cause
}

func normalizeRouteName(targetKey string) string {
	targetKey = strings.TrimSpace(targetKey)
	targetKey = strings.TrimPrefix(targetKey, "router/")
	if targetKey == "" {
		return "default"
	}
	return targetKey
}

func matchRouteConfig(config executorrouter.Config, spec contract.TaskSpec) (executorrouter.RouteConfig, bool) {
	for _, route := range config.Routes {
		if len(route.Match.TaskKinds) > 0 && !taskKindsContain(route.Match.TaskKinds, spec.Kind) {
			continue
		}
		if len(route.Match.Scopes) > 0 && !stringsContain(route.Match.Scopes, spec.Scope) {
			continue
		}
		return route, true
	}
	return executorrouter.RouteConfig{}, false
}

func taskKindsContain(values []contract.TaskKind, value contract.TaskKind) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func stringsContain(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
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
