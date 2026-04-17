package execution

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"odin-os/internal/core/projects"
	"odin-os/internal/core/workitems"
	"odin-os/internal/executors/contract"
	executorrouter "odin-os/internal/executors/router"
	"odin-os/internal/store/sqlite"
	"odin-os/internal/vcs/leases"
)

type Service struct {
	Store          *sqlite.Store
	Registry       projects.Registry
	Executors      map[string]contract.Executor
	ExecutorConfig executorrouter.Config
	Governance     projects.Service
	WorkItems      workitems.Service
	Leases         leases.Preparer
}

func (service Service) ExecuteNextQueued(ctx context.Context) error {
	if service.Store == nil {
		return fmt.Errorf("execution store is required")
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

	finishFailure := func(cause error) error {
		_, _ = service.Store.FinishRun(ctx, sqlite.FinishRunParams{
			RunID:   run.ID,
			Status:  "failed",
			Summary: cause.Error(),
		})
		_, _ = service.workItemService().Fail(ctx, task.ID)
		return cause
	}
	interruptRun := func(cause error) error {
		_, _ = service.Store.FinishRun(ctx, sqlite.FinishRunParams{
			RunID:   run.ID,
			Status:  "interrupted",
			Summary: cause.Error(),
		})
		return cause
	}

	if _, err := service.workItemService().Start(ctx, task.ID); err != nil {
		current, loadErr := service.Store.GetTask(ctx, task.ID)
		if loadErr == nil && current.Status != "queued" {
			return interruptRun(err)
		}
		return finishFailure(err)
	}

	if err := service.governanceService().AuthorizeExecutionMutation(ctx, projects.ExecutionAuthorizationInput{
		ProjectID:   project.ID,
		Manifest:    manifest,
		Actor:       projects.TransitionControllerOdinOS,
		ActionClass: projects.ActionClassIsolatedMutation,
		ActionKey:   "run_task",
	}); err != nil {
		return finishFailure(err)
	}

	if service.Leases == nil {
		return finishFailure(fmt.Errorf("lease preparer is required"))
	}
	assignment, err := service.Leases.Prepare(ctx, leases.Request{
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
	if err := projects.ValidateMutationAssignment(manifest, project.GitRoot, project.DefaultBranch, projects.MutationAssignment{
		BranchName:   assignment.BranchName,
		WorktreePath: assignment.WorktreePath,
	}); err != nil {
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

	if _, err := service.workItemService().Finalize(ctx, task.ID, result.Status); err != nil {
		return err
	}

	return nil
}

func (service Service) governanceService() projects.Service {
	if service.Governance.Store == nil {
		service.Governance.Store = service.Store
	}
	return service.Governance
}

func (service Service) workItemService() workitems.Service {
	if service.WorkItems.Store == nil {
		service.WorkItems.Store = service.Store
	}
	return service.WorkItems
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

func releaseAssignment(ctx context.Context, store *sqlite.Store, assignment leases.Assignment) {
	if assignment.LeaseID == nil {
		return
	}
	_, _ = store.ReleaseWorktreeLease(ctx, sqlite.ReleaseWorktreeLeaseParams{
		LeaseID: *assignment.LeaseID,
		State:   "released",
	})
}
