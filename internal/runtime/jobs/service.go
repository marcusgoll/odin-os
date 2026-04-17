package jobs

import (
	"context"
	"fmt"
	"strings"
	"time"

	"odin-os/internal/core/companions"
	"odin-os/internal/core/execution"
	"odin-os/internal/core/projects"
	corescope "odin-os/internal/core/scope"
	"odin-os/internal/core/workitems"
	"odin-os/internal/core/workspaces"
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
	Execution      queueExecutor
	Now            func() time.Time
}

type queueExecutor interface {
	ExecuteNextQueued(context.Context) error
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
	if service.Transitions.Store == nil {
		service.Transitions = projects.Service{Store: service.Store}
	}

	workspace, err := service.actWorkspace(ctx, resolved)
	if err != nil {
		return sqlite.Task{}, err
	}
	companion, err := service.actCompanion(ctx, workspace, resolved)
	if err != nil {
		return sqlite.Task{}, err
	}
	initiative, err := service.Transitions.RegisterManagedProjectInitiative(ctx, workspace.ID, project, &companion.ID)
	if err != nil {
		return sqlite.Task{}, err
	}

	now := time.Now().UTC()
	if service.Now != nil {
		now = service.Now().UTC()
	}

	return service.workItemService().Queue(ctx, sqlite.CreateTaskParams{
		ProjectID:    project.ID,
		Key:          fmt.Sprintf("%s-%s", slugify(title), now.Format("20060102-150405")),
		Title:        title,
		Scope:        taskScope,
		RequestedBy:  "operator",
		WorkspaceID:  &workspace.ID,
		InitiativeID: &initiative.ID,
		CompanionID:  &companion.ID,
		WorkKind:     taskScope,
	})
}

func (service Service) ExecuteNextQueued(ctx context.Context) error {
	return service.queueExecutor().ExecuteNextQueued(ctx)
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

func (service Service) workItemService() workitems.Service {
	if service.WorkItems.Store == nil {
		service.WorkItems.Store = service.Store
	}
	return service.WorkItems
}

func (service Service) actWorkspace(ctx context.Context, resolved corescope.ControlScope) (workspaces.Workspace, error) {
	workspaceService := workspaces.Service{Store: service.Store}
	if resolved.WorkspaceKey == workspaces.DefaultWorkspaceKey {
		return workspaceService.BootstrapDefaultWorkspace(ctx)
	}
	workspace, err := workspaceService.GetWorkspaceByKey(ctx, resolved.WorkspaceKey)
	if err != nil {
		return workspaces.Workspace{}, err
	}
	return workspace, nil
}

func (service Service) actCompanion(ctx context.Context, workspace workspaces.Workspace, resolved corescope.ControlScope) (companions.Companion, error) {
	companionKey := resolved.CompanionKey
	if companionKey == "" {
		companionKey = workspace.DefaultCompanionKey
	}

	companionService := companions.Service{Store: service.Store}
	return companionService.GetCompanionByKey(ctx, workspace.ID, companionKey)
}

type taskFinalizer interface {
	Finalize(context.Context, int64, string) (sqlite.Task, error)
}

func finalizeTaskOutcome(ctx context.Context, finalizer taskFinalizer, taskID int64, result contract.ExecutionResult) error {
	_, err := finalizer.Finalize(ctx, taskID, result.Status)
	return err
}

func (service Service) queueExecutor() queueExecutor {
	if service.Execution != nil {
		return service.Execution
	}
	leaseManager := service.Leases
	if leaseManager.Store == nil {
		leaseManager.Store = service.Store
	}
	return execution.Service{
		Store:          service.Store,
		Registry:       service.Registry,
		Executors:      service.Executors,
		ExecutorConfig: service.ExecutorConfig,
		Governance:     service.Transitions,
		WorkItems:      service.WorkItems,
		Leases:         leaseManager,
	}
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
