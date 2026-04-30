package jobs

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"odin-os/internal/cli/scope"
	"odin-os/internal/core/companions"
	"odin-os/internal/core/projects"
	"odin-os/internal/core/workspaces"
	"odin-os/internal/executors/contract"
	executorrouter "odin-os/internal/executors/router"
	"odin-os/internal/prompts"
	"odin-os/internal/runtime/checkpoints"
	healthsvc "odin-os/internal/runtime/health"
	"odin-os/internal/runtime/projections"
	"odin-os/internal/store/sqlite"
	"odin-os/internal/vcs/leases"
)

type Service struct {
	Store               *sqlite.Store
	Registry            projects.Registry
	Executors           map[string]contract.Executor
	ExecutorConfig      executorrouter.Config
	PromptRenderer      prompts.Renderer
	PromptTemplateName  string
	Transitions         projects.Service
	Leases              leases.Manager
	CheckpointCompactor func(context.Context, checkpoints.CompactParams) (checkpoints.CompactionResult, error)
	ShutdownRequested   *atomic.Bool
	Now                 func() time.Time
}

type CreateTaskParams struct {
	Resolved    scope.Resolution
	Title       string
	RequestedBy string
}

type ExecutionOutcome struct {
	Task sqlite.Task
	Run  *sqlite.Run
}

type ExecutionRequest struct {
	PromptOverride string
	Metadata       map[string]string
}

type DelegationAdmissionInput struct {
	ParentTask            sqlite.Task
	ParentRunID           *int64
	Companion             sqlite.Companion
	RequestedTools        []string
	RequestedMemoryScopes []string
	PreferredExecutor     string
}

type DelegationMemoryView struct {
	Mode         string   `json:"mode"`
	Scopes       []string `json:"scopes"`
	WorkspaceID  *int64   `json:"workspace_id,omitempty"`
	InitiativeID *int64   `json:"initiative_id,omitempty"`
	CompanionID  *int64   `json:"companion_id,omitempty"`
	ParentRunID  *int64   `json:"parent_run_id,omitempty"`
}

type DelegationAdmissionProfile struct {
	Executor     string               `json:"executor"`
	AllowedTools []string             `json:"allowed_tools"`
	MemoryView   DelegationMemoryView `json:"memory_view"`
}

type admissionOutcome string

const (
	admissionDispatchable   admissionOutcome = "dispatchable"
	admissionBlocked        admissionOutcome = "blocked"
	admissionFailed         admissionOutcome = "failed"
	admissionRetryLater     admissionOutcome = "retry_later"
	checkpointRecoveryDelay                  = time.Second
)

type admissionDecision struct {
	Outcome        admissionOutcome
	BlockedReason  string
	LastError      string
	NextEligibleAt time.Time
}

func (service Service) NarrowDelegationAdmission(input DelegationAdmissionInput) (DelegationAdmissionProfile, error) {
	if input.ParentTask.ID <= 0 {
		return DelegationAdmissionProfile{}, fmt.Errorf("parent task is required")
	}

	allowedTools, err := intersectAllowedTools(input.Companion.ToolPolicyJSON, input.RequestedTools)
	if err != nil {
		return DelegationAdmissionProfile{}, err
	}

	memoryMode, err := delegationMemoryMode(input.Companion.MemoryPolicyJSON)
	if err != nil {
		return DelegationAdmissionProfile{}, err
	}

	scopes := intersectMemoryScopes(input.ParentTask, input.ParentRunID, input.RequestedMemoryScopes)
	memoryView := DelegationMemoryView{
		Mode:         memoryMode,
		Scopes:       scopes,
		WorkspaceID:  pointerIfScope(scopes, "workspace", input.ParentTask.WorkspaceID),
		InitiativeID: pointerIfScope(scopes, "initiative", input.ParentTask.InitiativeID),
		CompanionID:  pointerIfScope(scopes, "companion", input.ParentTask.CompanionID),
		ParentRunID:  pointerIfScope(scopes, "run", input.ParentRunID),
	}

	return DelegationAdmissionProfile{
		Executor:     service.defaultDelegationExecutor(input.PreferredExecutor),
		AllowedTools: allowedTools,
		MemoryView:   memoryView,
	}, nil
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
	return service.createManagedTask(ctx, resolved, title, createManagedTaskInput{
		requestedBy:           "operator",
		taskCompanionID:       0,
		requestedSwarmTrigger: "",
	})
}

func (service Service) CreateTask(ctx context.Context, params CreateTaskParams) (sqlite.Task, error) {
	requestedBy := strings.TrimSpace(params.RequestedBy)
	if requestedBy == "" {
		requestedBy = "operator"
	}
	return service.createManagedTask(ctx, params.Resolved, params.Title, createManagedTaskInput{
		requestedBy:           requestedBy,
		taskCompanionID:       0,
		requestedSwarmTrigger: "",
	})
}

func (service Service) CreateTaskFromCompanionRun(ctx context.Context, resolved scope.Resolution, companion sqlite.Companion, title string, requestedSwarmTrigger string) (sqlite.Task, error) {
	return service.createManagedTask(ctx, resolved, title, createManagedTaskInput{
		requestedBy:           "companion",
		taskCompanionID:       companion.ID,
		requestedSwarmTrigger: requestedSwarmTrigger,
	})
}

type createManagedTaskInput struct {
	requestedBy           string
	taskCompanionID       int64
	requestedSwarmTrigger string
	actionKey             string
}

func (service Service) CreateTaskFromActWithAction(ctx context.Context, resolved scope.Resolution, title string, actionKey string) (sqlite.Task, error) {
	return service.createManagedTask(ctx, resolved, title, createManagedTaskInput{
		requestedBy:           "operator",
		taskCompanionID:       0,
		requestedSwarmTrigger: "",
		actionKey:             strings.TrimSpace(actionKey),
	})
}

func (service Service) createManagedTask(ctx context.Context, resolved scope.Resolution, title string, input createManagedTaskInput) (sqlite.Task, error) {
	if resolved.Kind == scope.ScopeGlobal {
		return sqlite.Task{}, fmt.Errorf("act mode requires a non-global scope")
	}

	projectManifest, taskScope, err := service.taskOwnerForScope(resolved)
	if err != nil {
		return sqlite.Task{}, err
	}

	transitions := service.Transitions
	if transitions.Store == nil {
		transitions = projects.Service{Store: service.Store}
	}

	project, err := transitions.RegisterManagedProject(ctx, projectManifest)
	if err != nil {
		return sqlite.Task{}, err
	}
	workspace, err := workspaces.Service{Store: service.Store}.BootstrapDefaultWorkspace(ctx)
	if err != nil {
		return sqlite.Task{}, err
	}
	defaultCompanion, err := companions.Service{Store: service.Store}.GetCompanionByKey(ctx, workspace.ID, workspace.DefaultCompanionKey)
	if err != nil {
		return sqlite.Task{}, err
	}

	taskCompanionID := input.taskCompanionID
	if taskCompanionID <= 0 {
		taskCompanionID = defaultCompanion.ID
	}

	ownerCompanionID, err := service.initiativeOwnerCompanionID(ctx, workspace.ID, project.Key, defaultCompanion.ID)
	if err != nil {
		return sqlite.Task{}, err
	}
	initiative, err := transitions.RegisterManagedProjectInitiative(ctx, workspace.ID, project, ownerCompanionID)
	if err != nil {
		return sqlite.Task{}, err
	}

	now := time.Now().UTC()
	if service.Now != nil {
		now = service.Now().UTC()
	}
	actionKey := strings.TrimSpace(input.actionKey)
	if actionKey == "" {
		actionKey = supportedSwarmTrigger(input.requestedSwarmTrigger)
	}

	return service.Store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:    project.ID,
		Key:          fmt.Sprintf("%s-%s-%09d", slugify(title), now.Format("20060102-150405"), now.Nanosecond()),
		Title:        title,
		ActionKey:    actionKey,
		Status:       "queued",
		Scope:        taskScope,
		RequestedBy:  input.requestedBy,
		WorkspaceID:  &workspace.ID,
		InitiativeID: &initiative.ID,
		CompanionID:  &taskCompanionID,
		WorkKind:     taskScope,
	})
}

func supportedSwarmTrigger(trigger string) string {
	switch strings.ToLower(strings.TrimSpace(trigger)) {
	case "parallel_research", "build_plus_review", "multi_artifact", "monitor_triage":
		return strings.ToLower(strings.TrimSpace(trigger))
	default:
		return ""
	}
}

func (service Service) ExecuteNextQueued(ctx context.Context) error {
	if service.Store == nil {
		return fmt.Errorf("job store is required")
	}
	if !service.dispatchAllowed() {
		return nil
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
	if !service.dispatchAllowed() {
		return nil
	}

	run, err := service.prepareRun(ctx, task, decision.ExecutorKey, attempt)
	if err != nil {
		return err
	}
	if !service.dispatchAllowed() {
		return service.interruptDispatch(ctx, run.ID)
	}

	assignment, leaseAdmission, err := service.prepareLease(ctx, task, project, manifest, run, attempt)
	if err != nil {
		return err
	}
	if leaseAdmission.Outcome != admissionDispatchable {
		return service.finalizeOutcome(ctx, task, run, leaseAdmission, contract.ExecutionResult{}, nil)
	}
	if !service.dispatchAllowed() {
		return service.interruptDispatch(ctx, run.ID)
	}

	if _, run, err = service.Store.UpdateRunAndTaskStatus(ctx, sqlite.UpdateRunAndTaskStatusParams{
		RunID:      run.ID,
		RunStatus:  "running",
		TaskStatus: "running",
	}); err != nil {
		return err
	}
	if !service.dispatchAllowed() {
		return service.interruptDispatch(ctx, run.ID)
	}

	spec.Metadata["branch_name"] = assignment.BranchName
	spec.Metadata["repo_root"] = assignment.RepoRoot
	spec.Metadata["worktree_path"] = assignment.WorktreePath
	if service.PromptRenderer != nil {
		renderedPrompt, err := service.renderPrompt(ctx, spec, task)
		if err != nil {
			return service.finalizeOutcome(ctx, task, run, admissionDecision{
				Outcome:   admissionFailed,
				LastError: err.Error(),
			}, contract.ExecutionResult{}, nil)
		}
		spec.Prompt = renderedPrompt
		spec.Metadata["prompt_size_bytes"] = fmt.Sprintf("%d", prompts.PromptSizeBytes(renderedPrompt))
	}

	executor := executors[decision.ExecutorKey]
	result, execErr := runExecutorTask(ctx, decision.ExecutorKey, executor, spec)
	executionMetadata := executionMetadataForResult(spec.Metadata, result.Metadata, assignment, decision.ExecutorKey, result.Handle.ExternalID)
	if err := service.finalizeOutcome(ctx, task, run, admissionDecision{}, result, execErr); err != nil {
		return err
	}
	if execErr == nil {
		return service.recordExecutionEvidenceArtifact(ctx, run.ID, executionMetadata)
	}
	return nil
}

func (service Service) ExecuteTask(ctx context.Context, taskID int64) (ExecutionOutcome, error) {
	return service.ExecuteTaskWithRequest(ctx, taskID, ExecutionRequest{})
}

func (service Service) ExecuteTaskWithRequest(ctx context.Context, taskID int64, request ExecutionRequest) (ExecutionOutcome, error) {
	if service.Store == nil {
		return ExecutionOutcome{}, fmt.Errorf("job store is required")
	}

	task, err := service.Store.GetTask(ctx, taskID)
	if err != nil {
		return ExecutionOutcome{}, err
	}
	project, err := service.Store.GetProject(ctx, task.ProjectID)
	if err != nil {
		return ExecutionOutcome{}, err
	}
	manifest, ok := service.Registry.Lookup(project.Key)
	if !ok {
		return service.failTaskBeforeRun(ctx, task, fmt.Errorf("unknown manifest for project %q", project.Key))
	}

	intake, hasIntake, err := service.latestTaskIntake(ctx, task.ID)
	if err != nil {
		return ExecutionOutcome{}, err
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
	prompt := strings.TrimSpace(request.PromptOverride)
	if prompt == "" {
		prompt = task.Title
	}
	spec := contract.TaskSpec{
		ID:     task.Key,
		Kind:   contract.TaskKindGeneral,
		Scope:  task.Scope,
		Prompt: prompt,
		Requirements: contract.Requirements{
			AllowedClasses:    []contract.ExecutorClass{contract.ExecutorClassPlanBackedCLI},
			NeedsHeadlessPlan: true,
		},
		Metadata: map[string]string{
			"project_key": project.Key,
			"task_id":     fmt.Sprintf("%d", task.ID),
		},
	}
	intakeSummary := ""
	if hasIntake {
		spec.Metadata["intake_source"] = intake.Source
		spec.Metadata["intake_type"] = intake.IntakeType
		spec.Metadata["intake_payload_json"] = intake.PayloadJSON
		intakeSummary = compactIntakeSummary(intake)
	}
	for key, value := range request.Metadata {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		spec.Metadata[key] = value
	}

	decision, err := selector.Select(ctx, spec)
	if err != nil {
		return service.failTaskBeforeRun(ctx, task, err)
	}

	admission, err := service.admitDirectTask(ctx, task, project, manifest)
	if err != nil {
		return ExecutionOutcome{}, err
	}
	if admission.Outcome != admissionDispatchable {
		if err := service.applyAdmissionDecision(ctx, task, admission); err != nil {
			return ExecutionOutcome{}, err
		}
		return service.loadExecutionOutcome(ctx, task.ID, nil)
	}

	attempt, err := service.nextRunAttempt(ctx, task.ID)
	if err != nil {
		return ExecutionOutcome{}, err
	}
	run, err := service.prepareRun(ctx, task, decision.ExecutorKey, attempt)
	if err != nil {
		return ExecutionOutcome{}, err
	}

	assignment, leaseAdmission, err := service.prepareLease(ctx, task, project, manifest, run, attempt)
	if err != nil {
		return ExecutionOutcome{}, err
	}
	if leaseAdmission.Outcome != admissionDispatchable {
		finalizeErr := service.finalizeOutcome(ctx, task, run, leaseAdmission, contract.ExecutionResult{}, nil)
		outcome, loadErr := service.loadExecutionOutcome(ctx, task.ID, &run.ID)
		if finalizeErr != nil {
			if loadErr == nil {
				return outcome, finalizeErr
			}
			return ExecutionOutcome{}, finalizeErr
		}
		return outcome, loadErr
	}

	updatedTask, updatedRun, err := service.Store.UpdateRunAndTaskStatus(ctx, sqlite.UpdateRunAndTaskStatusParams{
		RunID:      run.ID,
		RunStatus:  "running",
		TaskStatus: "running",
	})
	if err != nil {
		return ExecutionOutcome{}, err
	}
	task = updatedTask
	run = updatedRun

	spec.Metadata["branch_name"] = assignment.BranchName
	spec.Metadata["repo_root"] = assignment.RepoRoot
	spec.Metadata["worktree_path"] = assignment.WorktreePath
	if service.PromptRenderer != nil && strings.TrimSpace(request.PromptOverride) == "" {
		renderedPrompt, err := service.renderPrompt(ctx, spec, task)
		if err != nil {
			return ExecutionOutcome{}, err
		}
		spec.Prompt = renderedPrompt
		spec.Metadata["prompt_size_bytes"] = fmt.Sprintf("%d", prompts.PromptSizeBytes(renderedPrompt))
	}

	if intakeSummary != "" {
		if err := service.compactExecutionContext(ctx, project, task, run, intakeSummary); err != nil {
			return ExecutionOutcome{}, err
		}
	}

	executor := executors[decision.ExecutorKey]
	result, execErr := runExecutorTask(ctx, decision.ExecutorKey, executor, spec)
	executionMetadata := executionMetadataForResult(spec.Metadata, result.Metadata, assignment, decision.ExecutorKey, result.Handle.ExternalID)
	finalizeErr := service.finalizeOutcome(ctx, task, run, admissionDecision{}, result, execErr)
	outcome, loadErr := service.loadExecutionOutcome(ctx, task.ID, &run.ID)
	if finalizeErr != nil {
		if loadErr == nil {
			return outcome, finalizeErr
		}
		return ExecutionOutcome{}, finalizeErr
	}
	if loadErr != nil {
		return ExecutionOutcome{}, loadErr
	}
	if outcome.Run != nil && execErr == nil {
		if err := service.recordExecutionEvidenceArtifact(ctx, outcome.Run.ID, executionMetadata); err != nil {
			return ExecutionOutcome{}, err
		}
	}
	if err := service.recordExecutionMemory(ctx, project, outcome.Task, outcome.Run, prompt, executionMetadata); err != nil {
		return ExecutionOutcome{}, err
	}
	return outcome, nil
}

func (service Service) now() time.Time {
	if service.Now != nil {
		return service.Now().UTC()
	}
	return time.Now().UTC()
}

func runExecutorTask(ctx context.Context, executorKey string, executor contract.Executor, spec contract.TaskSpec) (result contract.ExecutionResult, execErr error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			result = contract.ExecutionResult{}
			execErr = fmt.Errorf("worker panic in executor %q", executorKey)
		}
	}()
	return executor.RunTask(ctx, spec)
}

func (service Service) dispatchAllowed() bool {
	return service.ShutdownRequested == nil || !service.ShutdownRequested.Load()
}

func (service Service) interruptDispatch(ctx context.Context, runID int64) error {
	if service.Store == nil {
		return nil
	}
	task, run, err := service.Store.InterruptRunAndRequeueTask(ctx, sqlite.InterruptRunAndRequeueTaskParams{
		RunID:   runID,
		Summary: "dispatch canceled: shutdown requested",
	})
	if err != nil {
		return err
	}
	if err := service.compactInterruptedRun(ctx, task, run); err != nil {
		return service.requeueAfterCheckpointFailure(ctx, task, time.Time{}, "dispatch handoff packet", err)
	}
	return nil
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

func (service Service) initiativeOwnerCompanionID(ctx context.Context, workspaceID int64, initiativeKey string, defaultCompanionID int64) (*int64, error) {
	initiative, err := service.Store.GetInitiativeByKey(ctx, workspaceID, initiativeKey)
	switch {
	case err == nil:
		if initiative.OwnerCompanionID != nil {
			return initiative.OwnerCompanionID, nil
		}
	case err == sql.ErrNoRows:
	default:
		return nil, err
	}

	return &defaultCompanionID, nil
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

func (service Service) recordExecutionMemory(ctx context.Context, project sqlite.Project, task sqlite.Task, run *sqlite.Run, prompt string, metadata map[string]string) error {
	if run == nil {
		return nil
	}
	responseText := strings.TrimSpace(run.Summary)
	if responseText == "" {
		responseText = fmt.Sprintf("Task %s finished with status %s.", task.Key, run.Status)
	}

	toolSummary := map[string]string{
		"executor":    run.Executor,
		"run_status":  run.Status,
		"task_status": task.Status,
	}
	for key, value := range normalizeExecutionMetadata(metadata) {
		toolSummary[key] = value
	}

	toolSummaryBytes, err := json.Marshal(toolSummary)
	if err != nil {
		return err
	}
	transcript, err := service.Store.RecordConversationTranscript(ctx, sqlite.RecordConversationTranscriptParams{
		ProjectID:   &project.ID,
		TaskID:      &task.ID,
		RunID:       &run.ID,
		Scope:       task.Scope,
		ScopeKey:    project.Key,
		Mode:        "act",
		Prompt:      strings.TrimSpace(prompt),
		Response:    responseText,
		ToolSummary: string(toolSummaryBytes),
		Executor:    run.Executor,
	})
	if err != nil {
		return err
	}

	details := map[string]any{
		"task_key":    task.Key,
		"task_status": task.Status,
		"run_status":  run.Status,
		"executor":    run.Executor,
		"prompt":      strings.TrimSpace(prompt),
	}
	if executionMetadata := normalizeExecutionMetadata(metadata); len(executionMetadata) != 0 {
		details["execution_metadata"] = executionMetadata
	}
	detailsBytes, err := json.Marshal(details)
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
		SourceTranscriptID: &transcript.ID,
		TaskID:             &task.ID,
		RunID:              &run.ID,
		Scope:              task.Scope,
		ScopeKey:           project.Key,
		MemoryType:         "episode",
		Summary:            summaryText,
		DetailsJSON:        string(detailsBytes),
	})
	return err
}

func normalizeExecutionMetadata(metadata map[string]string) map[string]string {
	if len(metadata) == 0 {
		return nil
	}

	normalized := make(map[string]string, len(metadata))
	for key, value := range metadata {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		normalized[key] = value
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}

func executionMetadataForResult(requestMetadata map[string]string, resultMetadata map[string]string, assignment leases.Assignment, executorLane string, externalID string) map[string]string {
	merged := make(map[string]string)
	for key, value := range normalizeExecutionMetadata(requestMetadata) {
		merged[key] = value
	}
	for key, value := range normalizeExecutionMetadata(resultMetadata) {
		if executorResultMetadataAllowed(key) {
			merged[key] = value
		}
	}
	if strings.TrimSpace(externalID) != "" {
		merged["external_id"] = strings.TrimSpace(externalID)
	}
	if strings.TrimSpace(merged["operation"]) == "" {
		merged["operation"] = "run"
	}

	if strings.TrimSpace(executorLane) != "" {
		merged["executor_lane"] = strings.TrimSpace(executorLane)
	}
	if strings.TrimSpace(assignment.WorktreePath) != "" {
		merged["worktree_path"] = strings.TrimSpace(assignment.WorktreePath)
	}
	if strings.TrimSpace(assignment.BranchName) != "" {
		merged["branch_name"] = strings.TrimSpace(assignment.BranchName)
	}
	if strings.TrimSpace(assignment.RepoRoot) != "" {
		merged["repo_root"] = strings.TrimSpace(assignment.RepoRoot)
	}

	if len(merged) == 0 {
		return nil
	}
	return merged
}

func executorResultMetadataAllowed(key string) bool {
	switch strings.TrimSpace(key) {
	case "driver_kind", "operation", "external_id", "driver_cwd", "branch_observed", "marker_path", "marker_written", "artifact_path", "artifacts_json":
		return true
	default:
		return false
	}
}

func (service Service) recordExecutionEvidenceArtifact(ctx context.Context, runID int64, metadata map[string]string) error {
	normalized := normalizeExecutionMetadata(metadata)
	if len(normalized) == 0 {
		return nil
	}
	detailsBytes, err := json.Marshal(normalized)
	if err != nil {
		return err
	}
	_, err = service.Store.RecordRunArtifact(ctx, sqlite.RecordRunArtifactParams{
		RunID:        runID,
		ArtifactType: "executor_evidence",
		Summary:      "executor evidence",
		DetailsJSON:  string(detailsBytes),
	})
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

func (service Service) latestTaskIntake(ctx context.Context, taskID int64) (sqlite.TaskIntake, bool, error) {
	row := service.Store.DB().QueryRowContext(ctx, `
		SELECT id, task_id, source, intake_type, dedup_key, requested_by, payload_json, created_at
		FROM task_intakes
		WHERE task_id = ?
		ORDER BY id DESC
		LIMIT 1
	`, taskID)

	var intake sqlite.TaskIntake
	var createdAt string
	if err := row.Scan(
		&intake.ID,
		&intake.TaskID,
		&intake.Source,
		&intake.IntakeType,
		&intake.DedupKey,
		&intake.RequestedBy,
		&intake.PayloadJSON,
		&createdAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return sqlite.TaskIntake{}, false, nil
		}
		return sqlite.TaskIntake{}, false, err
	}

	parsedCreatedAt, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return sqlite.TaskIntake{}, false, err
	}
	intake.CreatedAt = parsedCreatedAt
	return intake, true, nil
}

func (service Service) loadExecutionOutcome(ctx context.Context, taskID int64, runID *int64) (ExecutionOutcome, error) {
	task, err := service.Store.GetTask(ctx, taskID)
	if err != nil {
		return ExecutionOutcome{}, err
	}
	if runID == nil {
		return ExecutionOutcome{Task: task}, nil
	}

	run, err := service.Store.GetRun(ctx, *runID)
	if err != nil {
		return ExecutionOutcome{}, err
	}
	return ExecutionOutcome{
		Task: task,
		Run:  &run,
	}, nil
}

func (service Service) failTaskBeforeRun(ctx context.Context, task sqlite.Task, cause error) (ExecutionOutcome, error) {
	message := strings.TrimSpace(cause.Error())
	if message == "" {
		message = "failed"
	}

	if _, err := service.Store.UpdateTaskStatus(ctx, sqlite.UpdateTaskStatusParams{
		TaskID:         task.ID,
		Status:         "failed",
		Summary:        message,
		TerminalReason: message,
		ArtifactsJSON:  "[]",
	}); err != nil {
		return ExecutionOutcome{}, err
	}

	outcome, err := service.loadExecutionOutcome(ctx, task.ID, nil)
	if err != nil {
		return ExecutionOutcome{}, err
	}
	return outcome, cause
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

func (service Service) defaultDelegationExecutor(preferred string) string {
	preferred = strings.TrimSpace(preferred)
	if preferred != "" {
		return preferred
	}
	if _, ok := service.Executors["codex_headless"]; ok {
		return "codex_headless"
	}
	if len(service.ExecutorConfig.Executors) > 0 {
		for _, executor := range service.ExecutorConfig.Executors {
			if strings.TrimSpace(executor.Key) != "" {
				return executor.Key
			}
		}
	}
	return "codex_headless"
}

func (service Service) renderPrompt(ctx context.Context, spec contract.TaskSpec, task sqlite.Task) (string, error) {
	templateName := strings.TrimSpace(service.PromptTemplateName)
	if templateName == "" {
		templateName = string(spec.Kind)
	}
	return service.PromptRenderer.Render(ctx, templateName, prompts.TemplateData{
		WorkItemID:         task.Key,
		Role:               templateName,
		Title:              task.Title,
		AcceptanceCriteria: acceptanceCriteriaFromMetadata(spec.Metadata["acceptance_criteria"]),
		Metadata:           spec.Metadata,
	})
}

func acceptanceCriteriaFromMetadata(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, "\n")
	criteria := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(strings.TrimPrefix(part, "-"))
		if part != "" {
			criteria = append(criteria, part)
		}
	}
	return criteria
}

func intersectAllowedTools(rawPolicy string, requested []string) ([]string, error) {
	type toolPolicy struct {
		Allow []string `json:"allow"`
	}

	policy := toolPolicy{}
	trimmed := strings.TrimSpace(rawPolicy)
	if trimmed != "" && trimmed != "{}" {
		if err := json.Unmarshal([]byte(trimmed), &policy); err != nil {
			return nil, fmt.Errorf("invalid companion tool policy JSON: %w", err)
		}
	}

	allowed := uniqueStrings(policy.Allow)
	if len(requested) == 0 {
		return allowed, nil
	}
	if len(allowed) == 0 {
		return uniqueStrings(requested), nil
	}

	allowedSet := make(map[string]struct{}, len(allowed))
	for _, tool := range allowed {
		allowedSet[tool] = struct{}{}
	}

	filtered := make([]string, 0, len(requested))
	seen := make(map[string]struct{}, len(requested))
	for _, tool := range requested {
		tool = strings.TrimSpace(tool)
		if tool == "" {
			continue
		}
		if _, ok := allowedSet[tool]; !ok {
			continue
		}
		if _, ok := seen[tool]; ok {
			continue
		}
		seen[tool] = struct{}{}
		filtered = append(filtered, tool)
	}
	return filtered, nil
}

func delegationMemoryMode(rawPolicy string) (string, error) {
	type memoryPolicy struct {
		Mode string `json:"mode"`
	}

	policy := memoryPolicy{Mode: "companion"}
	trimmed := strings.TrimSpace(rawPolicy)
	if trimmed == "" || trimmed == "{}" {
		return policy.Mode, nil
	}
	if err := json.Unmarshal([]byte(trimmed), &policy); err != nil {
		return "", fmt.Errorf("invalid companion memory policy JSON: %w", err)
	}
	policy.Mode = strings.TrimSpace(policy.Mode)
	if policy.Mode == "" {
		policy.Mode = "companion"
	}
	return policy.Mode, nil
}

func intersectMemoryScopes(parentTask sqlite.Task, parentRunID *int64, requested []string) []string {
	available := availableMemoryScopes(parentTask, parentRunID)
	if len(requested) == 0 {
		return available
	}

	availableSet := make(map[string]struct{}, len(available))
	for _, scope := range available {
		availableSet[scope] = struct{}{}
	}

	filtered := make([]string, 0, len(requested))
	seen := make(map[string]struct{}, len(requested))
	for _, scope := range requested {
		scope = strings.TrimSpace(scope)
		if scope == "" {
			continue
		}
		if _, ok := availableSet[scope]; !ok {
			continue
		}
		if _, ok := seen[scope]; ok {
			continue
		}
		seen[scope] = struct{}{}
		filtered = append(filtered, scope)
	}
	return filtered
}

func availableMemoryScopes(parentTask sqlite.Task, parentRunID *int64) []string {
	scopes := make([]string, 0, 4)
	if parentTask.WorkspaceID != nil {
		scopes = append(scopes, "workspace")
	}
	if parentTask.InitiativeID != nil {
		scopes = append(scopes, "initiative")
	}
	if parentTask.CompanionID != nil {
		scopes = append(scopes, "companion")
	}
	if parentRunID != nil {
		scopes = append(scopes, "run")
	}
	return scopes
}

func uniqueStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	filtered := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		filtered = append(filtered, value)
	}
	return filtered
}

func pointerIfScope(scopes []string, required string, value *int64) *int64 {
	if value == nil {
		return nil
	}
	for _, scope := range scopes {
		if scope != required {
			continue
		}
		copied := *value
		return &copied
	}
	return nil
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

func (service Service) admitDirectTask(ctx context.Context, task sqlite.Task, project sqlite.Project, manifest projects.Manifest) (admissionDecision, error) {
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
			updatedTask, updatedRun, err := service.Store.FinishRunAndSetTaskStatus(ctx, sqlite.FinishRunAndSetTaskStatusParams{
				RunID:      run.ID,
				RunStatus:  "failed",
				Summary:    admission.LastError,
				TaskStatus: "failed",
			})
			if err != nil {
				return err
			}
			if err := service.compactFailedDispatch(ctx, updatedTask, updatedRun, admission.LastError); err != nil {
				return service.requeueAfterCheckpointFailure(
					ctx,
					updatedTask,
					service.now().Add(checkpointRecoveryDelay),
					fmt.Sprintf("dispatch preparation failed: %s", admission.LastError),
					err,
				)
			}
			return nil
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
		blockedTask, err := service.Store.BlockTask(ctx, sqlite.BlockTaskParams{
			TaskID: task.ID,
			Reason: decision.BlockedReason,
		})
		if err != nil {
			return err
		}
		if err := service.compactBlockedTask(ctx, blockedTask, decision); err != nil {
			return service.requeueAfterCheckpointFailure(
				ctx,
				blockedTask,
				service.now().Add(checkpointRecoveryDelay),
				fmt.Sprintf("blocked task (%s)", decision.BlockedReason),
				err,
			)
		}
		return nil
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

func (service Service) compactBlockedTask(ctx context.Context, task sqlite.Task, decision admissionDecision) error {
	if service.Store == nil {
		return nil
	}

	project, err := service.Store.GetProject(ctx, task.ProjectID)
	if err != nil {
		return err
	}

	approvalSummary := "none"
	if decision.BlockedReason == "approval_required" {
		approval, err := service.latestTaskApproval(ctx, task.ID)
		if err != nil {
			return err
		}
		if approval.Status != "" {
			approvalSummary = fmt.Sprintf("approval %s", approval.Status)
		}
	}

	_, err = service.compactCheckpoint(ctx, checkpoints.CompactParams{
		TaskID:               task.ID,
		Trigger:              blockedCheckpointTrigger(decision.BlockedReason),
		CheckpointKey:        fmt.Sprintf("task-%d-%s-%d", task.ID, decision.BlockedReason, service.now().UnixNano()),
		Objective:            task.Title,
		TaskStatus:           task.Status,
		BlockingReason:       decision.BlockedReason,
		NextSteps:            blockedNextSteps(decision.BlockedReason),
		Constraints:          blockedConstraints(decision.BlockedReason),
		SelectedCapabilities: blockedCapabilities(decision.BlockedReason),
		Evidence:             blockedEvidence(decision),
		ManifestSummary:      service.manifestSummary(project),
		PolicySummary:        service.policySummary(project),
		OpenTaskSummary:      fmt.Sprintf("task %s is blocked", task.Key),
		ApprovalSummary:      approvalSummary,
	})
	return err
}

func (service Service) compactInterruptedRun(ctx context.Context, task sqlite.Task, run sqlite.Run) error {
	if service.Store == nil {
		return nil
	}

	project, err := service.Store.GetProject(ctx, task.ProjectID)
	if err != nil {
		return err
	}

	_, err = service.compactCheckpoint(ctx, checkpoints.CompactParams{
		TaskID:            task.ID,
		RunID:             &run.ID,
		Trigger:           checkpoints.TriggerHandoff,
		CheckpointKey:     fmt.Sprintf("run-%d-interrupted-%d", run.ID, service.now().UnixNano()),
		Objective:         task.Title,
		TaskStatus:        task.Status,
		BlockingReason:    "shutdown_requested",
		LastCompletedStep: "dispatch interrupted before completion",
		NextSteps: []string{
			"Review the latest handoff packet",
			"Resume the queued task when the daemon is ready",
		},
		Constraints:          []string{"previous run was interrupted by shutdown"},
		SelectedCapabilities: []string{"handoff_resume"},
		Evidence: []checkpoints.Evidence{{
			Kind:    "interrupt",
			Summary: run.Summary,
		}},
		ManifestSummary: service.manifestSummary(project),
		PolicySummary:   service.policySummary(project),
		OpenTaskSummary: fmt.Sprintf("task %s was requeued after interruption", task.Key),
		ApprovalSummary: "none",
	})
	return err
}

func (service Service) compactFailedDispatch(ctx context.Context, task sqlite.Task, run sqlite.Run, summary string) error {
	if service.Store == nil {
		return nil
	}

	project, err := service.Store.GetProject(ctx, task.ProjectID)
	if err != nil {
		return err
	}

	_, err = service.compactCheckpoint(ctx, checkpoints.CompactParams{
		TaskID:            task.ID,
		RunID:             &run.ID,
		Trigger:           checkpoints.TriggerHandoff,
		CheckpointKey:     fmt.Sprintf("run-%d-failed-dispatch-%d", run.ID, service.now().UnixNano()),
		Objective:         task.Title,
		TaskStatus:        task.Status,
		BlockingReason:    summary,
		LastCompletedStep: "dispatch failed during preparation",
		NextSteps: []string{
			"Review the failed dispatch context",
			"Fix the policy or configuration issue before retrying the task",
		},
		Constraints:          []string{"dispatch failed after partial setup"},
		SelectedCapabilities: []string{"handoff_resume"},
		Evidence: []checkpoints.Evidence{{
			Kind:    "dispatch_failure",
			Summary: summary,
		}},
		ManifestSummary: service.manifestSummary(project),
		PolicySummary:   service.policySummary(project),
		OpenTaskSummary: fmt.Sprintf("task %s failed during dispatch preparation", task.Key),
		ApprovalSummary: "none",
	})
	return err
}

func (service Service) compactExecutionContext(ctx context.Context, project sqlite.Project, task sqlite.Task, run sqlite.Run, intakeSummary string) error {
	intakeSummary = strings.TrimSpace(intakeSummary)
	if intakeSummary == "" {
		return nil
	}

	_, err := service.compactCheckpoint(ctx, checkpoints.CompactParams{
		TaskID:          task.ID,
		RunID:           &run.ID,
		Trigger:         checkpoints.TriggerHandoff,
		CheckpointKey:   fmt.Sprintf("task-execution-%d", run.ID),
		Objective:       task.Title,
		TaskStatus:      task.Status,
		IntakeSummary:   intakeSummary,
		ManifestSummary: service.manifestSummary(project),
		PolicySummary:   "task execution intake compaction",
		OpenTaskSummary: "task queued for execution",
		ApprovalSummary: "none",
		Evidence: []checkpoints.Evidence{{
			Kind:    "intake",
			Summary: intakeSummary,
		}},
	})
	return err
}

func (service Service) compactCheckpoint(ctx context.Context, params checkpoints.CompactParams) (checkpoints.CompactionResult, error) {
	if service.CheckpointCompactor != nil {
		return service.CheckpointCompactor(ctx, params)
	}
	return checkpoints.Service{Store: service.Store}.Compact(ctx, params)
}

func (service Service) requeueAfterCheckpointFailure(ctx context.Context, task sqlite.Task, nextEligibleAt time.Time, summary string, compactErr error) error {
	if service.Store == nil {
		return compactErr
	}

	lastError := fmt.Sprintf("%s unavailable: %v", summary, compactErr)
	_, err := service.Store.UpdateTaskQueueState(ctx, sqlite.UpdateTaskQueueStateParams{
		TaskID:         task.ID,
		Status:         "queued",
		NextEligibleAt: nextEligibleAt,
		Priority:       task.Priority,
		LastError:      lastError,
		RetryCount:     task.RetryCount,
		MaxAttempts:    task.MaxAttempts,
		BlockedReason:  "",
	})
	if err != nil {
		return errors.Join(compactErr, err)
	}
	return compactErr
}

func (service Service) manifestSummary(project sqlite.Project) string {
	if manifest, ok := service.Registry.Lookup(project.Key); ok {
		if manifest.SystemProject {
			return "managed system project"
		}
		return "managed project"
	}
	return fmt.Sprintf("%s project %s", project.Scope, project.Key)
}

func (service Service) policySummary(project sqlite.Project) string {
	if manifest, ok := service.Registry.Lookup(project.Key); ok {
		if manifest.SystemProject {
			return "system project approval and branch rules apply"
		}
		return "project branch rules apply"
	}
	return "runtime-managed execution"
}

func blockedCheckpointTrigger(reason string) checkpoints.Trigger {
	switch reason {
	case "approval_required":
		return checkpoints.TriggerApprovalWait
	default:
		return checkpoints.TriggerIdlePause
	}
}

func blockedNextSteps(reason string) []string {
	switch reason {
	case "approval_required":
		return []string{
			"Review the pending approval request",
			"Resume the queued task once approval is granted",
		}
	default:
		return []string{
			"Inspect executor health and restore availability",
			"Resume the blocked task once a healthy executor is available",
		}
	}
}

func blockedConstraints(reason string) []string {
	switch reason {
	case "approval_required":
		return []string{"task cannot continue without explicit approval"}
	default:
		return []string{"task is blocked until executor health recovers"}
	}
}

func blockedCapabilities(reason string) []string {
	switch reason {
	case "approval_required":
		return []string{"approval_resume"}
	default:
		return []string{"executor_recovery"}
	}
}

func blockedEvidence(decision admissionDecision) []checkpoints.Evidence {
	if decision.LastError == "" {
		return nil
	}
	return []checkpoints.Evidence{{
		Kind:    "health",
		Summary: decision.LastError,
	}}
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

func compactIntakeSummary(intake sqlite.TaskIntake) string {
	parts := []string{strings.TrimSpace(intake.Source), strings.TrimSpace(intake.IntakeType), "intake"}
	filtered := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			filtered = append(filtered, part)
		}
	}
	return strings.Join(filtered, " ")
}
