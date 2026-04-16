package jobs

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"odin-os/internal/core/projects"
	corescope "odin-os/internal/core/scope"
	"odin-os/internal/core/workitems"
	"odin-os/internal/executors/contract"
	executorrouter "odin-os/internal/executors/router"
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
	WorkItems      workitems.Service
	Leases         leases.Manager
	Now            func() time.Time
}

func (service Service) List(ctx context.Context, resolved corescope.ControlScope) ([]projections.TaskStatusView, error) {
	views, err := projections.ListTaskStatusViews(ctx, service.Store.DB())
	if err != nil {
		return nil, err
	}

	filtered := make([]projections.TaskStatusView, 0, len(views))
	for _, view := range views {
		if resolved.MatchesTask(view.ProjectKey, view.Scope) {
			filtered = append(filtered, view)
		}
	}

	return filtered, nil
}

func (service Service) CreateTaskFromAct(ctx context.Context, resolved corescope.ControlScope, title string) (sqlite.Task, error) {
	if resolved.IsGlobal() {
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

	return service.workItemService().Queue(ctx, sqlite.CreateTaskParams{
		ProjectID:   project.ID,
		Key:         fmt.Sprintf("%s-%s", slugify(title), now.Format("20060102-150405")),
		Title:       title,
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
		_, _ = service.workItemService().Fail(ctx, task.ID)
		return fmt.Errorf("unknown manifest for project %q", project.Key)
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
		_, _ = service.workItemService().Fail(ctx, task.ID)
		return err
	}

	attempt, err := service.nextRunAttempt(ctx, task.ID)
	if err != nil {
		return err
	}

	run, err := service.Store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:   task.ID,
		Executor: decision.ExecutorKey,
		Attempt:  attempt,
		Status:   "running",
	})
	if err != nil {
		return err
	}

	if _, err := service.workItemService().Start(ctx, task.ID); err != nil {
		return err
	}

	finishFailure := func(cause error) error {
		_, _ = service.Store.FinishRun(ctx, sqlite.FinishRunParams{
			RunID:   run.ID,
			Status:  "failed",
			Summary: cause.Error(),
		})
		_, _ = service.workItemService().Fail(ctx, task.ID)
		return cause
	}

	assignment := leases.Assignment{
		Mode:         "read_only",
		RepoRoot:     project.GitRoot,
		WorktreePath: project.GitRoot,
	}

	if err := authorizeMutation(manifest); err != nil {
		return finishFailure(err)
	}
	if _, err := service.Transitions.AuthorizeAction(ctx, projects.ActionInput{
		ProjectID:   project.ID,
		Actor:       projects.TransitionControllerOdinOS,
		ActionClass: projects.ActionClassIsolatedMutation,
		ActionKey:   "run_task",
	}); err != nil {
		return finishFailure(err)
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
		Try:           attempt,
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
	spec.Metadata["worktree_path"] = assignment.WorktreePath

	executor := executors[decision.ExecutorKey]
	result, err := executor.RunTask(ctx, spec)
	if err != nil {
		return finishFailure(err)
	}

	runStatus := result.Status
	if runStatus == "" {
		runStatus = "completed"
	}

	if _, err := service.Store.FinishRun(ctx, sqlite.FinishRunParams{
		RunID:   run.ID,
		Status:  runStatus,
		Summary: result.Output,
	}); err != nil {
		return err
	}

	if err := finalizeTaskOutcome(ctx, service.workItemService(), task.ID, result); err != nil {
		return err
	}

	return nil
}

func (service Service) ensureRuntimeProject(ctx context.Context, manifest projects.Manifest) (sqlite.Project, error) {
	transitions := service.Transitions
	if transitions.Store == nil {
		transitions = projects.Service{Store: service.Store}
	}

	return transitions.RegisterManagedProject(ctx, manifest)
}

func (service Service) taskOwnerForScope(resolved corescope.ControlScope) (projects.Manifest, string, error) {
	switch resolved.SubjectType {
	case corescope.SubjectTypeNewProject:
		project, ok := service.Registry.SystemProject()
		if !ok {
			return projects.Manifest{}, "", fmt.Errorf("new-project scope requires odin-core")
		}
		return project, resolved.TaskScope(), nil
	case corescope.SubjectTypeInitiative:
		project, ok := service.Registry.Lookup(resolved.ProjectKey)
		if !ok {
			return projects.Manifest{}, "", fmt.Errorf("unknown project %q", resolved.ProjectKey)
		}
		return project, resolved.TaskScope(), nil
	default:
		return projects.Manifest{}, "", fmt.Errorf("unsupported scope %q", resolved.SubjectType)
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

func (service Service) workItemService() workitems.Service {
	if service.WorkItems.Store == nil {
		service.WorkItems.Store = service.Store
	}
	return service.WorkItems
}

type taskFinalizer interface {
	Finalize(context.Context, int64, string) (sqlite.Task, error)
}

func finalizeTaskOutcome(ctx context.Context, finalizer taskFinalizer, taskID int64, result contract.ExecutionResult) error {
	_, err := finalizer.Finalize(ctx, taskID, result.Status)
	return err
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

func authorizeMutation(manifest projects.Manifest) error {
	if manifest.SystemProject && manifest.Policy.ApprovalGates.RequireForSystemProjectChanges != nil && *manifest.Policy.ApprovalGates.RequireForSystemProjectChanges {
		return fmt.Errorf("system project %q requires explicit approval for mutations", manifest.Key)
	}
	return nil
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
