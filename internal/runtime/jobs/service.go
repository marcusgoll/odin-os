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
	runtimeevents "odin-os/internal/runtime/events"
	healthsvc "odin-os/internal/runtime/health"
	"odin-os/internal/runtime/projections"
	"odin-os/internal/runtime/recovery"
	"odin-os/internal/store/sqlite"
	"odin-os/internal/vcs/leases"
)

type Service struct {
	Store               *sqlite.Store
	RuntimeRoot         string
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
	StaleRunTimeout     time.Duration
}

type CreateTaskParams struct {
	Resolved              scope.Resolution
	Title                 string
	AcceptanceCriteria    []string
	RequestedBy           string
	Key                   string
	CompanionID           int64
	ExecutionIntent       string
	ExecutionIntentSource string
}

type CreateTaskResult struct {
	Task    sqlite.Task
	Created bool
}

type ExecutionOutcome struct {
	Task sqlite.Task
	Run  *sqlite.Run
}

type DispatchOutcome struct {
	Task       sqlite.Task
	Run        *sqlite.Run
	Dispatched bool
	Reason     string
}

type RunExecutionOutcome struct {
	Task     sqlite.Task
	Run      *sqlite.Run
	Executed bool
	Reason   string
}

type RetryOutcome struct {
	Task                   sqlite.Task
	Retried                bool
	Reason                 string
	Decision               string
	RetryEligible          bool
	RecoveryRecommendation string
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
	defaultStaleRunTimeout                   = 30 * time.Minute
	operatorPausedReason                     = "operator_paused"
)

var (
	ErrOperatorPauseUnsupported  = errors.New("operator pause unsupported")
	ErrOperatorResumeUnsupported = errors.New("operator resume unsupported")
)

type admissionDecision struct {
	Outcome        admissionOutcome
	BlockedReason  string
	LastError      string
	FailureCode    recovery.FailureCode
	NextEligibleAt time.Time
}

type executionIntent struct {
	ActionClass projects.ActionClass
	ActionKey   string
	Mutating    bool
	Reason      string
	Value       string
	Source      string
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
	result, err := service.CreateTaskOnce(ctx, params)
	return result.Task, err
}

func (service Service) PauseIssue(ctx context.Context, issueID int64) (sqlite.Task, error) {
	if service.Store == nil {
		return sqlite.Task{}, fmt.Errorf("job store is required")
	}
	task, err := service.resolveDashboardIssueTask(ctx, issueID)
	if err != nil {
		return sqlite.Task{}, err
	}

	switch task.Status {
	case "queued":
		return service.Store.BlockTask(ctx, sqlite.BlockTaskParams{
			TaskID: task.ID,
			Reason: operatorPausedReason,
		})
	case "blocked":
		if task.BlockedReason == operatorPausedReason {
			return task, nil
		}
		return task, fmt.Errorf("%w: task %d is blocked by %s", ErrOperatorPauseUnsupported, task.ID, task.BlockedReason)
	case "running":
		return task, fmt.Errorf("%w: task %d is running and run interruption is not supported", ErrOperatorPauseUnsupported, task.ID)
	case "completed", "failed", "canceled":
		return task, fmt.Errorf("%w: task %d is terminal with status %s", ErrOperatorPauseUnsupported, task.ID, task.Status)
	default:
		return task, fmt.Errorf("%w: task %d has unsupported status %s", ErrOperatorPauseUnsupported, task.ID, task.Status)
	}
}

func (service Service) ResumeIssue(ctx context.Context, issueID int64) (sqlite.Task, error) {
	if service.Store == nil {
		return sqlite.Task{}, fmt.Errorf("job store is required")
	}
	task, err := service.resolveDashboardIssueTask(ctx, issueID)
	if err != nil {
		return sqlite.Task{}, err
	}
	if task.Status != "blocked" || task.BlockedReason != operatorPausedReason {
		return task, fmt.Errorf("%w: task %d is %s/%s", ErrOperatorResumeUnsupported, task.ID, task.Status, task.BlockedReason)
	}
	return service.Store.RequeueTaskAt(ctx, sqlite.RequeueTaskAtParams{
		TaskID:         task.ID,
		NextEligibleAt: task.NextEligibleAt,
	})
}

func (service Service) resolveDashboardIssueTask(ctx context.Context, issueID int64) (sqlite.Task, error) {
	if issueID <= 0 {
		return sqlite.Task{}, sql.ErrNoRows
	}
	task, err := service.taskForExternalIssue(ctx, issueID)
	if err == nil {
		return task, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return sqlite.Task{}, err
	}
	externalExists, err := service.externalIssueExists(ctx, issueID)
	if err != nil {
		return sqlite.Task{}, err
	}
	if externalExists {
		return sqlite.Task{}, sql.ErrNoRows
	}
	return service.Store.GetTask(ctx, issueID)
}

func (service Service) externalIssueExists(ctx context.Context, issueID int64) (bool, error) {
	var id int64
	err := service.Store.DB().QueryRowContext(ctx, `
		SELECT id
		FROM external_issues
		WHERE id = ?
	`, issueID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (service Service) taskForExternalIssue(ctx context.Context, issueID int64) (sqlite.Task, error) {
	records, err := service.Store.ListEvents(ctx, sqlite.ListEventsParams{})
	if err != nil {
		return sqlite.Task{}, err
	}
	externalEventIDs := make(map[int64]struct{})
	for _, record := range records {
		if record.StreamType == runtimeevents.StreamExternalEvent && record.StreamID == issueID && record.Type == runtimeevents.EventExternalGitHubIssue {
			externalEventIDs[record.ID] = struct{}{}
		}
	}
	if len(externalEventIDs) == 0 {
		return sqlite.Task{}, sql.ErrNoRows
	}

	var taskID int64
	for _, record := range records {
		if record.Type != runtimeevents.EventAutomationTriggerMaterialized {
			continue
		}
		var payload runtimeevents.AutomationTriggerMaterializedPayload
		if err := json.Unmarshal(record.Payload, &payload); err != nil {
			continue
		}
		if payload.SourceEventID == nil {
			continue
		}
		if _, ok := externalEventIDs[*payload.SourceEventID]; !ok {
			continue
		}
		if payload.TaskID != 0 {
			taskID = payload.TaskID
			continue
		}
		if record.TaskID != nil {
			taskID = *record.TaskID
		}
	}
	if taskID == 0 {
		return sqlite.Task{}, sql.ErrNoRows
	}
	return service.Store.GetTask(ctx, taskID)
}

func (service Service) CreateTaskOnce(ctx context.Context, params CreateTaskParams) (CreateTaskResult, error) {
	requestedBy := strings.TrimSpace(params.RequestedBy)
	if requestedBy == "" {
		requestedBy = "operator"
	}
	return service.createManagedTaskOnce(ctx, params.Resolved, params.Title, createManagedTaskInput{
		requestedBy:           requestedBy,
		taskCompanionID:       params.CompanionID,
		requestedSwarmTrigger: "",
		key:                   strings.TrimSpace(params.Key),
		acceptanceCriteria:    sqlite.NormalizeAcceptanceCriteria(params.AcceptanceCriteria),
		executionIntent:       params.ExecutionIntent,
		executionIntentSource: params.ExecutionIntentSource,
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
	key                   string
	acceptanceCriteria    []string
	executionIntent       string
	executionIntentSource string
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
	result, err := service.createManagedTaskOnce(ctx, resolved, title, input)
	return result.Task, err
}

func (service Service) createManagedTaskOnce(ctx context.Context, resolved scope.Resolution, title string, input createManagedTaskInput) (CreateTaskResult, error) {
	if resolved.Kind == scope.ScopeGlobal {
		return CreateTaskResult{}, fmt.Errorf("act mode requires a non-global scope")
	}

	projectManifest, taskScope, err := service.taskOwnerForScope(resolved)
	if err != nil {
		return CreateTaskResult{}, err
	}

	transitions := service.Transitions
	if transitions.Store == nil {
		transitions = projects.Service{Store: service.Store}
	}

	project, err := transitions.RegisterManagedProject(ctx, projectManifest)
	if err != nil {
		return CreateTaskResult{}, err
	}
	workspace, err := workspaces.Service{Store: service.Store}.BootstrapDefaultWorkspace(ctx)
	if err != nil {
		return CreateTaskResult{}, err
	}
	defaultCompanion, err := companions.Service{Store: service.Store}.GetCompanionByKey(ctx, workspace.ID, workspace.DefaultCompanionKey)
	if err != nil {
		return CreateTaskResult{}, err
	}

	taskCompanionID := input.taskCompanionID
	if taskCompanionID <= 0 {
		taskCompanionID = defaultCompanion.ID
	}

	ownerCompanionID, err := service.initiativeOwnerCompanionID(ctx, workspace.ID, project.Key, defaultCompanion.ID)
	if err != nil {
		return CreateTaskResult{}, err
	}
	initiative, err := transitions.RegisterManagedProjectInitiative(ctx, workspace.ID, project, ownerCompanionID)
	if err != nil {
		return CreateTaskResult{}, err
	}

	now := time.Now().UTC()
	if service.Now != nil {
		now = service.Now().UTC()
	}
	actionKey := strings.TrimSpace(input.actionKey)
	if actionKey == "" {
		actionKey = supportedSwarmTrigger(input.requestedSwarmTrigger)
	}

	key := strings.TrimSpace(input.key)
	if key == "" {
		key = fmt.Sprintf("%s-%s-%09d", slugify(title), now.Format("20060102-150405"), now.Nanosecond())
	} else if existing, err := service.Store.GetTaskByProjectAndKey(ctx, project.ID, key); err == nil {
		return CreateTaskResult{Task: existing, Created: false}, nil
	}
	executionIntent := normalizeExecutionIntentValue(input.executionIntent)
	if strings.TrimSpace(input.executionIntent) != "" && executionIntent == "" {
		return CreateTaskResult{}, fmt.Errorf("execution intent must be one of read_only, mutation, governance, destructive")
	}
	executionIntentSource := strings.TrimSpace(input.executionIntentSource)
	if executionIntent != "" && executionIntentSource == "" {
		executionIntentSource = "operator"
	}

	task, err := service.Store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:             project.ID,
		Key:                   key,
		Title:                 title,
		AcceptanceCriteria:    input.acceptanceCriteria,
		ActionKey:             actionKey,
		Status:                "queued",
		Scope:                 taskScope,
		RequestedBy:           input.requestedBy,
		WorkspaceID:           &workspace.ID,
		InitiativeID:          &initiative.ID,
		CompanionID:           &taskCompanionID,
		WorkKind:              taskScope,
		ExecutionIntent:       executionIntent,
		ExecutionIntentSource: executionIntentSource,
	})
	if err != nil && input.key != "" && isTaskKeyConflict(err) {
		existing, getErr := service.Store.GetTaskByProjectAndKey(ctx, project.ID, key)
		return CreateTaskResult{Task: existing, Created: false}, getErr
	}
	return CreateTaskResult{Task: task, Created: true}, err
}

func isTaskKeyConflict(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "UNIQUE constraint failed: tasks.project_id, tasks.key")
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
	service.addRuntimeRootMetadata(spec.Metadata)

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
	claimedTask, claimedRun, claimed, err := service.Store.ClaimRunExecution(ctx, sqlite.ClaimRunExecutionParams{
		TaskID: task.ID,
		RunID:  run.ID,
		Actor:  "serve.queue_executor",
	})
	if err != nil {
		return err
	}
	if claimed {
		task = claimedTask
		run = claimedRun
	}
	if !service.dispatchAllowed() {
		return service.interruptDispatch(ctx, run.ID)
	}

	spec.Metadata["branch_name"] = assignment.BranchName
	spec.Metadata["repo_root"] = assignment.RepoRoot
	spec.Metadata["worktree_path"] = assignment.WorktreePath
	service.addRuntimeRootMetadata(spec.Metadata)
	if _, err := service.applyLatestTaskIntakeMetadata(ctx, task.ID, spec.Metadata); err != nil {
		return err
	}
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

func (service Service) DispatchNextRunAttempt(ctx context.Context) (DispatchOutcome, error) {
	if service.Store == nil {
		return DispatchOutcome{}, fmt.Errorf("job store is required")
	}
	task, err := service.nextQueuedTask(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return DispatchOutcome{Reason: "no_queued_work"}, nil
		}
		return DispatchOutcome{}, err
	}
	return service.DispatchTaskRunAttempt(ctx, task.ID)
}

func (service Service) DispatchTaskRunAttempt(ctx context.Context, taskID int64) (DispatchOutcome, error) {
	if service.Store == nil {
		return DispatchOutcome{}, fmt.Errorf("job store is required")
	}

	task, err := service.Store.GetTask(ctx, taskID)
	if err != nil {
		return DispatchOutcome{}, err
	}
	if task.Status != "queued" {
		outcome := DispatchOutcome{
			Task:   task,
			Reason: "task_not_queued",
		}
		if task.CurrentRunID != nil {
			run, runErr := service.Store.GetRun(ctx, *task.CurrentRunID)
			if runErr != nil {
				return DispatchOutcome{}, runErr
			}
			outcome.Run = &run
		}
		return outcome, nil
	}

	project, err := service.Store.GetProject(ctx, task.ProjectID)
	if err != nil {
		return DispatchOutcome{}, err
	}
	manifest, ok := service.Registry.Lookup(project.Key)
	if !ok {
		return DispatchOutcome{}, fmt.Errorf("unknown manifest for project %q", project.Key)
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
		return DispatchOutcome{}, err
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
		return DispatchOutcome{}, err
	}

	admission, err := service.admitDirectTask(ctx, task, project, manifest)
	if err != nil {
		return DispatchOutcome{}, err
	}
	if admission.Outcome != admissionDispatchable {
		if err := service.applyAdmissionDecision(ctx, task, admission); err != nil {
			return DispatchOutcome{}, err
		}
		updated, loadErr := service.Store.GetTask(ctx, task.ID)
		if loadErr != nil {
			return DispatchOutcome{}, loadErr
		}
		reason := string(admission.Outcome)
		if admission.BlockedReason != "" {
			reason = admission.BlockedReason
		}
		if reason == "" && admission.LastError != "" {
			reason = admission.LastError
		}
		return DispatchOutcome{Task: updated, Reason: reason}, nil
	}

	attempt, err := service.nextRunAttempt(ctx, task.ID)
	if err != nil {
		return DispatchOutcome{}, err
	}
	if err := service.Store.RecordTaskDispatchRequested(ctx, task, decision.ExecutorKey, attempt); err != nil {
		return DispatchOutcome{}, err
	}
	run, err := service.prepareRun(ctx, task, decision.ExecutorKey, attempt)
	if err != nil {
		return DispatchOutcome{}, err
	}
	updatedTask, updatedRun, err := service.Store.UpdateRunAndTaskStatus(ctx, sqlite.UpdateRunAndTaskStatusParams{
		RunID:      run.ID,
		RunStatus:  "running",
		TaskStatus: "running",
	})
	if err != nil {
		return DispatchOutcome{}, err
	}
	return DispatchOutcome{
		Task:       updatedTask,
		Run:        &updatedRun,
		Dispatched: true,
		Reason:     "dispatched",
	}, nil
}

func (service Service) RetryFailedTask(ctx context.Context, taskID int64) (RetryOutcome, error) {
	return service.retryFailedTask(ctx, taskID, "work_retry", "")
}

func (service Service) RetryFailedTaskFromReview(ctx context.Context, taskID int64, queueID string) (RetryOutcome, error) {
	return service.retryFailedTask(ctx, taskID, "review_queue", queueID)
}

func (service Service) retryFailedTask(ctx context.Context, taskID int64, source string, queueID string) (RetryOutcome, error) {
	if service.Store == nil {
		return RetryOutcome{}, fmt.Errorf("job store is required")
	}
	task, err := service.Store.GetTask(ctx, taskID)
	if err != nil {
		return RetryOutcome{}, err
	}

	switch task.Status {
	case "failed":
		policy := evaluateRetryPolicy(task)
		if !policy.retryEligible {
			if err := service.Store.RecordTaskRetryDecision(ctx, sqlite.RecordTaskRetryDecisionParams{
				Task:                   task,
				Decision:               policy.decision,
				RetryEligible:          false,
				RecoveryRecommendation: policy.recoveryRecommendation,
				Source:                 source,
				QueueID:                queueID,
			}); err != nil {
				return RetryOutcome{}, err
			}
			return RetryOutcome{
				Task:                   task,
				Retried:                false,
				Reason:                 policy.decision,
				Decision:               policy.decision,
				RetryEligible:          false,
				RecoveryRecommendation: policy.recoveryRecommendation,
			}, nil
		}
		lastError := strings.TrimSpace(task.TerminalReason)
		if lastError == "" {
			lastError = strings.TrimSpace(task.Summary)
		}
		if lastError == "" {
			lastError = "operator requested retry after terminal failure"
		}
		updated, err := service.Store.IncrementTaskRetry(ctx, sqlite.IncrementTaskRetryParams{
			TaskID:                 task.ID,
			LastError:              lastError,
			NextEligibleAt:         service.now(),
			RecordDecision:         true,
			Decision:               policy.decision,
			RetryEligible:          true,
			RecoveryRecommendation: policy.recoveryRecommendation,
			RetrySource:            source,
			ReviewQueueID:          queueID,
		})
		if err != nil {
			return RetryOutcome{}, err
		}
		return RetryOutcome{
			Task:                   updated,
			Retried:                true,
			Reason:                 "retried",
			Decision:               policy.decision,
			RetryEligible:          true,
			RecoveryRecommendation: policy.recoveryRecommendation,
		}, nil
	case "queued":
		const recommendation = "Task is already queued; dispatch it instead of retrying again."
		if err := service.Store.RecordTaskRetryDecision(ctx, sqlite.RecordTaskRetryDecisionParams{Task: task, Decision: "retry_already_queued", RetryEligible: false, RecoveryRecommendation: recommendation, Source: source, QueueID: queueID}); err != nil {
			return RetryOutcome{}, err
		}
		return RetryOutcome{Task: task, Retried: false, Reason: "already_queued", Decision: "retry_already_queued", RetryEligible: false, RecoveryRecommendation: recommendation}, nil
	case "running", "preparing", "executing":
		const recommendation = "Task already has active execution; wait for the current run to finish before retrying."
		if err := service.Store.RecordTaskRetryDecision(ctx, sqlite.RecordTaskRetryDecisionParams{Task: task, Decision: "retry_blocked_active", RetryEligible: false, RecoveryRecommendation: recommendation, Source: source, QueueID: queueID}); err != nil {
			return RetryOutcome{}, err
		}
		return RetryOutcome{Task: task, Retried: false, Reason: "already_active", Decision: "retry_blocked_active", RetryEligible: false, RecoveryRecommendation: recommendation}, nil
	default:
		const recommendation = "Only terminal failed work can be retried through work retry."
		if err := service.Store.RecordTaskRetryDecision(ctx, sqlite.RecordTaskRetryDecisionParams{Task: task, Decision: "retry_blocked_non_retryable", RetryEligible: false, RecoveryRecommendation: recommendation, Source: source, QueueID: queueID}); err != nil {
			return RetryOutcome{}, err
		}
		return RetryOutcome{Task: task, Retried: false, Reason: "task_not_failed", Decision: "retry_blocked_non_retryable", RetryEligible: false, RecoveryRecommendation: recommendation}, nil
	}
}

type retryPolicyDecision struct {
	decision               string
	retryEligible          bool
	recoveryRecommendation string
}

func evaluateRetryPolicy(task sqlite.Task) retryPolicyDecision {
	guidance := recovery.RetryGuidanceForTask(recovery.RetryGuidanceInput{
		RetryCount:  task.RetryCount,
		MaxAttempts: task.MaxAttempts,
		WorkKind:    task.WorkKind,
		RequestedBy: task.RequestedBy,
	})
	return retryPolicyDecision{
		decision:               guidance.Decision,
		retryEligible:          guidance.RetryEligible,
		recoveryRecommendation: guidance.RecoveryRecommendation,
	}
}

func (service Service) ExecuteDispatchedRun(ctx context.Context, taskID int64) (RunExecutionOutcome, error) {
	return service.executeDispatchedRun(ctx, taskID, "operator")
}

func (service Service) ExecuteNextDispatchedRun(ctx context.Context) (RunExecutionOutcome, error) {
	if service.Store == nil {
		return RunExecutionOutcome{}, fmt.Errorf("job store is required")
	}
	executingRuns, err := service.Store.ListRunsByStatus(ctx, "executing")
	if err != nil {
		return RunExecutionOutcome{}, err
	}
	if len(executingRuns) > 0 {
		run := executingRuns[0]
		task, err := service.Store.GetTask(ctx, run.TaskID)
		if err != nil {
			return RunExecutionOutcome{}, err
		}
		if service.isStaleRun(run) && task.Status == "running" && task.CurrentRunID != nil && *task.CurrentRunID == run.ID {
			const summary = "stale executing run recovered by live service loop"
			if err := service.Store.ResolveStalledRun(ctx, sqlite.ResolveStalledRunParams{
				RunID:          run.ID,
				TaskID:         task.ID,
				TaskStatus:     "queued",
				Summary:        summary,
				TerminalReason: summary,
				ArtifactsJSON:  `{"reason":"stale_executing_run","recovered_by":"serve.task_loop"}`,
			}); err != nil {
				return RunExecutionOutcome{}, err
			}
			recoveredRun, err := service.Store.GetRun(ctx, run.ID)
			if err != nil {
				return RunExecutionOutcome{}, err
			}
			recoveredTask, err := service.Store.GetTask(ctx, task.ID)
			if err != nil {
				return RunExecutionOutcome{}, err
			}
			return RunExecutionOutcome{Task: recoveredTask, Run: &recoveredRun, Reason: "stale_executing_run_recovered"}, nil
		}
		return RunExecutionOutcome{Task: task, Run: &run, Reason: "run_already_executing"}, nil
	}
	runs, err := service.Store.ListRunsByStatus(ctx, "running")
	if err != nil {
		return RunExecutionOutcome{}, err
	}
	for _, run := range runs {
		task, err := service.Store.GetTask(ctx, run.TaskID)
		if err != nil {
			return RunExecutionOutcome{}, err
		}
		if task.Status != "running" || task.CurrentRunID == nil || *task.CurrentRunID != run.ID {
			continue
		}
		return service.executeDispatchedRun(ctx, task.ID, "serve.task_loop")
	}
	return RunExecutionOutcome{Reason: "no_running_dispatched_runs"}, nil
}

func (service Service) staleRunTimeout() time.Duration {
	if service.StaleRunTimeout > 0 {
		return service.StaleRunTimeout
	}
	return defaultStaleRunTimeout
}

func (service Service) isStaleRun(run sqlite.Run) bool {
	return run.StartedAt.Before(service.now().Add(-service.staleRunTimeout()))
}

func (service Service) executeDispatchedRun(ctx context.Context, taskID int64, actor string) (RunExecutionOutcome, error) {
	if service.Store == nil {
		return RunExecutionOutcome{}, fmt.Errorf("job store is required")
	}
	task, err := service.Store.GetTask(ctx, taskID)
	if err != nil {
		return RunExecutionOutcome{}, err
	}
	if task.Status != "running" || task.CurrentRunID == nil {
		return RunExecutionOutcome{
			Task:   task,
			Reason: "task_not_running",
		}, nil
	}

	run, err := service.Store.GetRun(ctx, *task.CurrentRunID)
	if err != nil {
		return RunExecutionOutcome{}, err
	}
	if run.Status != "running" {
		reason := "run_not_running"
		if run.Status == "executing" {
			reason = "run_already_executing"
		}
		return RunExecutionOutcome{
			Task:   task,
			Run:    &run,
			Reason: reason,
		}, nil
	}

	task, run, claimed, err := service.Store.ClaimRunExecution(ctx, sqlite.ClaimRunExecutionParams{
		TaskID: task.ID,
		RunID:  run.ID,
		Actor:  actor,
	})
	if err != nil {
		return RunExecutionOutcome{}, err
	}
	if !claimed {
		reason := "run_not_running"
		if run.Status == "executing" {
			reason = "run_already_executing"
		}
		if task.Status != "running" || task.CurrentRunID == nil {
			reason = "task_not_running"
		}
		return RunExecutionOutcome{
			Task:   task,
			Run:    &run,
			Reason: reason,
		}, nil
	}

	project, err := service.Store.GetProject(ctx, task.ProjectID)
	if err != nil {
		return RunExecutionOutcome{}, err
	}
	executors := service.Executors
	if len(executors) == 0 {
		executors = executorrouter.DefaultCatalog()
	}
	executor, ok := executors[run.Executor]
	if !ok {
		return RunExecutionOutcome{}, fmt.Errorf("executor %q is unavailable", run.Executor)
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
			"project_key":   project.Key,
			"task_id":       fmt.Sprintf("%d", task.ID),
			"run_id":        fmt.Sprintf("%d", run.ID),
			"repo_root":     project.GitRoot,
			"worktree_path": project.GitRoot,
			"branch_name":   project.DefaultBranch,
		},
	}
	service.addRuntimeRootMetadata(spec.Metadata)
	if _, err := service.applyLatestTaskIntakeMetadata(ctx, task.ID, spec.Metadata); err != nil {
		return RunExecutionOutcome{}, err
	}
	if service.PromptRenderer != nil {
		renderedPrompt, err := service.renderPrompt(ctx, spec, task)
		if err != nil {
			return RunExecutionOutcome{}, err
		}
		spec.Prompt = renderedPrompt
		spec.Metadata["prompt_size_bytes"] = fmt.Sprintf("%d", prompts.PromptSizeBytes(renderedPrompt))
	}

	result, execErr := runExecutorTask(ctx, run.Executor, executor, spec)
	executionMetadata := executionMetadataForResult(spec.Metadata, result.Metadata, leases.Assignment{}, run.Executor, result.Handle.ExternalID)
	finalizeErr := service.finalizeOutcome(ctx, task, run, admissionDecision{}, result, execErr)
	outcome, loadErr := service.loadExecutionOutcome(ctx, task.ID, &run.ID)
	if finalizeErr != nil {
		if loadErr == nil {
			return RunExecutionOutcome{
				Task:     outcome.Task,
				Run:      outcome.Run,
				Executed: true,
				Reason:   "execution_failed",
			}, finalizeErr
		}
		return RunExecutionOutcome{}, finalizeErr
	}
	if loadErr != nil {
		return RunExecutionOutcome{}, loadErr
	}
	if outcome.Run != nil && execErr == nil {
		if err := service.recordExecutionEvidenceArtifact(ctx, outcome.Run.ID, executionMetadata); err != nil {
			return RunExecutionOutcome{}, err
		}
	}
	if err := service.recordExecutionMemory(ctx, project, outcome.Task, outcome.Run, task.Title, executionMetadata); err != nil {
		return RunExecutionOutcome{}, err
	}

	reason := "completed"
	if outcome.Run != nil && outcome.Run.Status != "completed" {
		reason = outcome.Run.Status
	}
	return RunExecutionOutcome{
		Task:     outcome.Task,
		Run:      outcome.Run,
		Executed: true,
		Reason:   reason,
	}, nil
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
	service.addRuntimeRootMetadata(spec.Metadata)
	intakeSummary := ""
	if hasIntake {
		intakeSummary = applyTaskIntakeMetadata(intake, spec.Metadata)
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
	claimedTask, claimedRun, claimed, err := service.Store.ClaimRunExecution(ctx, sqlite.ClaimRunExecutionParams{
		TaskID: task.ID,
		RunID:  run.ID,
		Actor:  "operator",
	})
	if err != nil {
		return ExecutionOutcome{}, err
	}
	if claimed {
		task = claimedTask
		run = claimedRun
	}

	spec.Metadata["branch_name"] = assignment.BranchName
	spec.Metadata["repo_root"] = assignment.RepoRoot
	spec.Metadata["worktree_path"] = assignment.WorktreePath
	service.addRuntimeRootMetadata(spec.Metadata)
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

func (service Service) addRuntimeRootMetadata(metadata map[string]string) {
	if metadata == nil {
		return
	}
	if root := strings.TrimSpace(service.RuntimeRoot); root != "" {
		metadata["runtime_root"] = root
	}
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
	case "driver_kind", "operation", "external_id", "driver_cwd", "branch_observed", "marker_path", "marker_written", "artifact_path", "artifacts_json", "failure_code":
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

func (service Service) applyLatestTaskIntakeMetadata(ctx context.Context, taskID int64, metadata map[string]string) (string, error) {
	intake, hasIntake, err := service.latestTaskIntake(ctx, taskID)
	if err != nil || !hasIntake {
		return "", err
	}
	return applyTaskIntakeMetadata(intake, metadata), nil
}

func applyTaskIntakeMetadata(intake sqlite.TaskIntake, metadata map[string]string) string {
	if metadata == nil {
		return compactIntakeSummary(intake)
	}
	metadata["intake_source"] = intake.Source
	metadata["intake_type"] = intake.IntakeType
	metadata["intake_payload_json"] = intake.PayloadJSON
	return compactIntakeSummary(intake)
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
		Title:              trustedPromptTitle(task.Title, spec.Metadata),
		AcceptanceCriteria: promptAcceptanceCriteria(task, spec.Metadata),
		Metadata:           spec.Metadata,
		UntrustedData:      untrustedPromptData(task, spec.Metadata),
	})
}

func promptAcceptanceCriteria(task sqlite.Task, metadata map[string]string) []string {
	if criteria := sqlite.NormalizeAcceptanceCriteria(task.AcceptanceCriteria); len(criteria) > 0 {
		return criteria
	}
	if metadata == nil {
		return nil
	}
	return acceptanceCriteriaFromMetadata(metadata["acceptance_criteria"])
}

func trustedPromptTitle(title string, metadata map[string]string) string {
	if hasExternalIntakeMetadata(metadata) {
		return ""
	}
	return title
}

func untrustedPromptData(task sqlite.Task, metadata map[string]string) []prompts.UntrustedDataBlock {
	if !hasExternalIntakeMetadata(metadata) {
		return nil
	}
	source := strings.TrimSpace(metadata["intake_source"])
	kind := strings.TrimSpace(metadata["intake_type"])
	blocks := []prompts.UntrustedDataBlock{}
	if title := strings.TrimSpace(task.Title); title != "" {
		blocks = append(blocks, prompts.UntrustedDataBlock{
			Source:  source,
			Kind:    kind,
			Field:   "title",
			Content: title,
		})
	}
	if payload := strings.TrimSpace(metadata["intake_payload_json"]); payload != "" {
		blocks = append(blocks, prompts.UntrustedDataBlock{
			Source:  source,
			Kind:    kind,
			Field:   "payload_json",
			Content: payload,
		})
	}
	return blocks
}

func hasExternalIntakeMetadata(metadata map[string]string) bool {
	if metadata == nil {
		return false
	}
	return strings.TrimSpace(metadata["intake_source"]) != "" || strings.TrimSpace(metadata["intake_payload_json"]) != ""
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
	task, intent, err := service.resolveAndPersistTaskExecutionIntent(ctx, manifest, task)
	if err != nil {
		return admissionDecision{}, err
	}
	approvalDecision, required, err := service.evaluateTaskApproval(ctx, task, manifest, intent)
	if err != nil {
		return admissionDecision{}, err
	}
	if required && approvalDecision.Outcome != admissionDispatchable {
		return approvalDecision, nil
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
			FailureCode:   recovery.FailureCodeExecutorUnavailable,
		}, nil
	}

	if _, err := service.Transitions.AuthorizeAction(ctx, projects.ActionInput{
		ProjectID:   project.ID,
		Actor:       projects.TransitionControllerOdinOS,
		ActionClass: intent.ActionClass,
		ActionKey:   intent.ActionKey,
	}); err != nil {
		return admissionDecision{
			Outcome:     admissionFailed,
			LastError:   fmt.Sprintf("transition_denied: %v", err),
			FailureCode: recovery.FailureCodePolicyDenied,
		}, nil
	}

	return admissionDecision{Outcome: admissionDispatchable}, nil
}

func (service Service) admitDirectTask(ctx context.Context, task sqlite.Task, project sqlite.Project, manifest projects.Manifest) (admissionDecision, error) {
	task, intent, err := service.resolveAndPersistTaskExecutionIntent(ctx, manifest, task)
	if err != nil {
		return admissionDecision{}, err
	}
	approvalDecision, required, err := service.evaluateTaskApproval(ctx, task, manifest, intent)
	if err != nil {
		return admissionDecision{}, err
	}
	if required && approvalDecision.Outcome != admissionDispatchable {
		return approvalDecision, nil
	}

	if _, err := service.Transitions.AuthorizeAction(ctx, projects.ActionInput{
		ProjectID:   project.ID,
		Actor:       projects.TransitionControllerOdinOS,
		ActionClass: intent.ActionClass,
		ActionKey:   intent.ActionKey,
	}); err != nil {
		return admissionDecision{
			Outcome:     admissionFailed,
			LastError:   fmt.Sprintf("transition_denied: %v", err),
			FailureCode: recovery.FailureCodePolicyDenied,
		}, nil
	}

	if intent.Mutating && mutationRequiresIsolatedWorktree(manifest) {
		return admissionDecision{
			Outcome:       admissionBlocked,
			BlockedReason: "mutation_requires_isolated_worktree",
			LastError:     fmt.Sprintf("policy_denied: project %q requires an isolated task worktree before mutation", manifest.Key),
			FailureCode:   recovery.FailureCodeWorkspacePolicyDenied,
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
	intent := resolveTaskExecutionIntent(manifest, task)
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
		Mutating:      intent.Mutating,
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
				FailureCode:    recovery.FailureCodeWorkspaceLeaseConflict,
				NextEligibleAt: service.now().Add(time.Second),
			}, nil
		}
		return leases.Assignment{}, admissionDecision{}, err
	}
	if intent.Mutating {
		if err := validateAssignment(manifest, project, assignment); err != nil {
			return leases.Assignment{}, admissionDecision{
				Outcome:     admissionFailed,
				LastError:   fmt.Sprintf("policy_denied: %v", err),
				FailureCode: recovery.FailureCodeWorkspacePolicyDenied,
			}, nil
		}
	}
	if !intent.Mutating && assignment.Mode == "" {
		assignment.Mode = "read_only"
	}
	if !intent.Mutating && assignment.WorktreePath == "" {
		assignment.WorktreePath = project.GitRoot
	}
	if !intent.Mutating && assignment.RepoRoot == "" {
		assignment.RepoRoot = project.GitRoot
	}
	if intent.Mutating && assignment.WorktreePath == project.GitRoot {
		return leases.Assignment{}, admissionDecision{
			Outcome:     admissionFailed,
			LastError:   fmt.Sprintf("policy_denied: project %q requires an isolated task worktree before mutation", manifest.Key),
			FailureCode: recovery.FailureCodeWorkspacePolicyDenied,
		}, nil
	}
	return assignment, admissionDecision{Outcome: admissionDispatchable}, nil
}

func (service Service) finalizeOutcome(ctx context.Context, task sqlite.Task, run sqlite.Run, admission admissionDecision, result contract.ExecutionResult, execErr error) error {
	if admission.Outcome != "" && admission.Outcome != admissionDispatchable {
		switch admission.Outcome {
		case admissionFailed:
			artifactsJSON := service.failureAnalysisArtifact(task, "dispatch", admission.LastError, task.RetryCount, admission.FailureCode)
			updatedTask, updatedRun, err := service.Store.FinishRunAndSetTaskStatus(ctx, sqlite.FinishRunAndSetTaskStatusParams{
				RunID:         run.ID,
				RunStatus:     "failed",
				Summary:       admission.LastError,
				ArtifactsJSON: artifactsJSON,
				TaskStatus:    "failed",
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
			artifactsJSON := service.failureAnalysisArtifact(task, "dispatch", admission.LastError, task.RetryCount, admission.FailureCode)
			_, _, err := service.Store.FailRunAndRetryTask(ctx, sqlite.FailRunAndRetryTaskParams{
				RunID:          run.ID,
				Summary:        admission.LastError,
				ArtifactsJSON:  artifactsJSON,
				LastError:      admission.LastError,
				NextEligibleAt: admission.NextEligibleAt,
			})
			return err
		default:
			return fmt.Errorf("unsupported admission outcome %q", admission.Outcome)
		}
	}

	if execErr != nil {
		failureCode := failureCodeForExecutionError(execErr, result)
		if isTransientFailure(execErr) {
			artifactsJSON := service.failureAnalysisArtifact(task, "codex_run", execErr.Error(), task.RetryCount+1, failureCode)
			if task.RetryCount+1 >= task.MaxAttempts {
				failedTask, _, err := service.Store.FinishRunAndSetTaskStatus(ctx, sqlite.FinishRunAndSetTaskStatusParams{
					RunID:         run.ID,
					RunStatus:     "failed",
					Summary:       execErr.Error(),
					ArtifactsJSON: artifactsJSON,
					TaskStatus:    "failed",
				})
				if err != nil {
					return err
				}
				if err := service.recordTaskRecoveryRecommendation(ctx, failedTask); err != nil {
					return err
				}
				return execErr
			}
			nextRetryCount := task.RetryCount + 1
			nextEligibleAt := service.now().Add(retryDelay(nextRetryCount))
			_, _, err := service.Store.FailRunAndRetryTask(ctx, sqlite.FailRunAndRetryTaskParams{
				RunID:          run.ID,
				Summary:        execErr.Error(),
				ArtifactsJSON:  artifactsJSON,
				LastError:      execErr.Error(),
				NextEligibleAt: nextEligibleAt,
			})
			return err
		}

		artifactsJSON := service.failureAnalysisArtifact(task, "codex_run", execErr.Error(), task.RetryCount, failureCode)
		failedTask, _, err := service.Store.FinishRunAndSetTaskStatus(ctx, sqlite.FinishRunAndSetTaskStatusParams{
			RunID:         run.ID,
			RunStatus:     "failed",
			Summary:       execErr.Error(),
			ArtifactsJSON: artifactsJSON,
			TaskStatus:    "failed",
		})
		if err != nil {
			return err
		}
		if err := service.recordTaskRecoveryRecommendation(ctx, failedTask); err != nil {
			return err
		}
		return execErr
	}

	runStatus := result.Status
	if runStatus == "" {
		runStatus = "completed"
	}
	taskStatus := "completed"
	artifactsJSON := ""
	if runStatus != "completed" {
		taskStatus = "failed"
		artifactsJSON = service.failureAnalysisArtifact(task, "codex_run", result.Output, task.RetryCount, failureCodeFromResult(result))
	}

	updatedTask, _, err := service.Store.FinishRunAndSetTaskStatus(ctx, sqlite.FinishRunAndSetTaskStatusParams{
		RunID:         run.ID,
		RunStatus:     runStatus,
		Summary:       result.Output,
		ArtifactsJSON: artifactsJSON,
		TaskStatus:    taskStatus,
	})
	if err != nil {
		return err
	}
	if taskStatus == "failed" {
		return service.recordTaskRecoveryRecommendation(ctx, updatedTask)
	}
	return nil
}

func (service Service) recordTaskRecoveryRecommendation(ctx context.Context, task sqlite.Task) error {
	if service.Store == nil || !strings.EqualFold(strings.TrimSpace(task.Status), "failed") {
		return nil
	}
	guidance := recovery.RetryGuidanceForTask(recovery.RetryGuidanceInput{
		RetryCount:  task.RetryCount,
		MaxAttempts: task.MaxAttempts,
		WorkKind:    task.WorkKind,
		RequestedBy: task.RequestedBy,
	})
	return service.Store.RecordTaskRecoveryRecommendation(ctx, sqlite.RecordTaskRecoveryRecommendationParams{
		Task:                   task,
		Decision:               guidance.Decision,
		RetryEligible:          guidance.RetryEligible,
		RecoveryRecommendation: guidance.RecoveryRecommendation,
		Source:                 guidance.Source,
	})
}

func (service Service) failureAnalysisArtifact(task sqlite.Task, step string, summary string, retryCount int, code recovery.FailureCode) string {
	analysis := recovery.AnalyzeFailure(recovery.FailureInput{
		Code:                  code,
		Step:                  step,
		TicketTitle:           task.Title,
		ExistingBehaviorKnown: true,
		ErrorText:             summary,
		Summary:               summary,
		RetryCount:            retryCount,
		MaxAttempts:           task.MaxAttempts,
	})
	payload, err := recovery.MarshalFailureAnalysisArtifact(analysis)
	if err != nil {
		return ""
	}
	return payload
}

func failureCodeForExecutionError(err error, result contract.ExecutionResult) recovery.FailureCode {
	if code := failureCodeFromResult(result); code != "" {
		return code
	}
	if isTransientFailure(err) {
		return recovery.FailureCodeExecutorTimeout
	}
	return ""
}

func failureCodeFromResult(result contract.ExecutionResult) recovery.FailureCode {
	if code := recovery.FailureCode(strings.TrimSpace(result.FailureCode)); code != "" {
		return code
	}
	if result.Metadata == nil {
		return ""
	}
	return recovery.FailureCode(strings.TrimSpace(result.Metadata["failure_code"]))
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

func (service Service) resolveAndPersistTaskExecutionIntent(ctx context.Context, manifest projects.Manifest, task sqlite.Task) (sqlite.Task, executionIntent, error) {
	intent := resolveTaskExecutionIntent(manifest, task)
	if intent.Value == "" {
		return task, intent, nil
	}
	if strings.TrimSpace(task.ExecutionIntent) == intent.Value && strings.TrimSpace(task.ExecutionIntentSource) == intent.Source {
		return task, intent, nil
	}
	if intent.Source != "safety_classifier" {
		return task, intent, nil
	}
	updated, err := service.Store.UpdateTaskExecutionIntent(ctx, sqlite.UpdateTaskExecutionIntentParams{
		TaskID:                task.ID,
		ExecutionIntent:       intent.Value,
		ExecutionIntentSource: intent.Source,
	})
	if err != nil {
		return task, executionIntent{}, err
	}
	return updated, intent, nil
}

func (service Service) evaluateTaskApproval(ctx context.Context, task sqlite.Task, manifest projects.Manifest, intent executionIntent) (admissionDecision, bool, error) {
	requirement := projects.ApprovalRequiredForAction(manifest, intent.ActionClass)
	if !requirement.Required {
		return admissionDecision{Outcome: admissionDispatchable}, false, nil
	}

	approval, err := service.latestTaskApproval(ctx, task.ID)
	if err != nil {
		return admissionDecision{}, true, err
	}
	switch approval.Status {
	case "approved":
		return admissionDecision{Outcome: admissionDispatchable}, true, nil
	case "pending":
		return admissionDecision{
			Outcome:       admissionBlocked,
			BlockedReason: "approval_required",
			LastError:     requirement.Reason,
		}, true, nil
	case "":
		if _, err := service.Store.RequestApproval(ctx, sqlite.RequestApprovalParams{
			TaskID:      task.ID,
			Status:      "pending",
			RequestedBy: "system",
		}); err != nil {
			return admissionDecision{}, true, err
		}
		return admissionDecision{
			Outcome:       admissionBlocked,
			BlockedReason: "approval_required",
			LastError:     requirement.Reason,
		}, true, nil
	default:
		return admissionDecision{
			Outcome:     admissionFailed,
			LastError:   fmt.Sprintf("approval for task %d is %s", task.ID, approval.Status),
			FailureCode: recovery.FailureCodePolicyDenied,
		}, true, nil
	}
}

func classifyTaskExecutionIntent(title string) executionIntent {
	normalized := normalizeIntentTitle(title)
	intent := executionIntent{
		ActionClass: projects.ActionClassReadOnly,
		ActionKey:   "read_only_task",
		Reason:      "default_read_only",
		Value:       "read_only",
		Source:      "fallback_title",
	}
	if normalized == "" {
		return intent
	}
	if containsAny(normalized, []string{"read-only", "read only", "inspect", "status", "list "}) {
		intent.Reason = "explicit_read_only"
		return intent
	}
	if highRiskIntent := classifyHighRiskExecutionIntent(normalized); highRiskIntent.Value != "" {
		return highRiskIntent
	}

	if containsAny(normalized, []string{
		"delete", "remove", "rm ", " reset ", "git reset", "clean", "force push", "force-push",
		"drop ", "destroy", "truncate", "wipe", "purge", "destructive",
	}) {
		return executionIntent{
			ActionClass: projects.ActionClassDestructiveMutation,
			ActionKey:   "run_task",
			Mutating:    true,
			Reason:      "destructive_keyword",
			Value:       "destructive",
			Source:      "fallback_title",
		}
	}
	if containsAny(normalized, []string{
		"governance", "transition", "system project", "_system_", "system_trigger",
	}) {
		return executionIntent{
			ActionClass: projects.ActionClassGovernanceMutation,
			ActionKey:   "run_task",
			Mutating:    true,
			Reason:      "governance_keyword",
			Value:       "governance",
			Source:      "fallback_title",
		}
	}
	if containsAny(normalized, []string{
		"modify", "mutate", "mutation", "write", "edit", "change", "update", "create", "add file",
		"touch ", "apply patch", "commit", "implement ", "fix ", "repair", "refactor",
	}) {
		return executionIntent{
			ActionClass: projects.ActionClassIsolatedMutation,
			ActionKey:   "run_task",
			Mutating:    true,
			Reason:      "mutation_keyword",
			Value:       "mutation",
			Source:      "fallback_title",
		}
	}
	return intent
}

func resolveTaskExecutionIntent(manifest projects.Manifest, task sqlite.Task) executionIntent {
	if intent := executionIntentFromStored(task.ExecutionIntent, task.ExecutionIntentSource); intent.Value != "" {
		if intent.Value == "read_only" {
			if highRiskIntent := classifyHighRiskExecutionIntent(normalizeIntentTitle(task.Title)); highRiskIntent.Value != "" {
				highRiskIntent.Source = "safety_classifier"
				highRiskIntent.Reason = "high_risk_read_only_override"
				return applyManifestExecutionDefaults(manifest, highRiskIntent)
			}
		}
		return intent
	}
	return applyManifestExecutionDefaults(manifest, classifyTaskExecutionIntent(task.Title))
}

func normalizeIntentTitle(title string) string {
	normalized := strings.ToLower(strings.TrimSpace(title))
	normalized = strings.NewReplacer("_", " ", "-", " ").Replace(normalized)
	return strings.Join(strings.Fields(normalized), " ")
}

func classifyHighRiskExecutionIntent(normalized string) executionIntent {
	if normalized == "" {
		return executionIntent{}
	}
	if containsAny(normalized, []string{
		"delete data", "delete record", "delete records", "remove data", "remove record", "remove records",
		"destroy data", "wipe data", "purge data", "truncate",
	}) {
		return executionIntent{
			ActionClass: projects.ActionClassDestructiveMutation,
			ActionKey:   "run_task",
			Mutating:    true,
			Reason:      "high_risk_destructive_keyword",
			Value:       "destructive",
			Source:      "fallback_title",
		}
	}
	if containsAny(normalized, []string{
		"send email", "send message", "send sms", "send text",
		"create calendar event", "change calendar event", "update calendar event", "modify calendar event",
		"make purchase", "buy ", "purchase ",
		"deploy code", "deploy ", "release to production",
		"modify production", "change production", "production system", "prod system",
		"change permission", "change permissions", "grant permission", "revoke permission",
		"publish public", "publish post", "post to x", "publish to x", "publish social",
		"financial record", "financial records", "legal record", "legal records", "medical record", "medical records",
	}) {
		return executionIntent{
			ActionClass: projects.ActionClassGovernanceMutation,
			ActionKey:   "run_task",
			Mutating:    true,
			Reason:      "high_risk_real_world_keyword",
			Value:       "governance",
			Source:      "fallback_title",
		}
	}
	return executionIntent{}
}

func executionIntentFromStored(value string, source string) executionIntent {
	intentValue := normalizeExecutionIntentValue(value)
	if intentValue == "" {
		return executionIntent{}
	}
	intent := executionIntent{
		ActionKey: "run_task",
		Value:     intentValue,
		Source:    strings.TrimSpace(source),
		Reason:    "persisted_intent",
	}
	if intent.Source == "" {
		intent.Source = "persisted"
	}
	switch intentValue {
	case "read_only":
		intent.ActionClass = projects.ActionClassReadOnly
		intent.ActionKey = "read_only_task"
		intent.Mutating = false
	case "mutation":
		intent.ActionClass = projects.ActionClassIsolatedMutation
		intent.Mutating = true
	case "governance":
		intent.ActionClass = projects.ActionClassGovernanceMutation
		intent.Mutating = true
	case "destructive":
		intent.ActionClass = projects.ActionClassDestructiveMutation
		intent.Mutating = true
	}
	return intent
}

func normalizeExecutionIntentValue(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "read_only", "readonly", "read-only", "read only":
		return "read_only"
	case "mutation", "mutating":
		return "mutation"
	case "governance", "governance_mutation":
		return "governance"
	case "destructive", "destructive_mutation":
		return "destructive"
	default:
		return ""
	}
}

func applyManifestExecutionDefaults(manifest projects.Manifest, intent executionIntent) executionIntent {
	if manifest.SystemProject && intent.Reason == "default_read_only" {
		return executionIntent{
			ActionClass: projects.ActionClassIsolatedMutation,
			ActionKey:   "run_task",
			Mutating:    false,
			Reason:      "system_project_default_approval_gate",
			Value:       "mutation",
			Source:      "system_project_default",
		}
	}
	return intent
}

func containsAny(value string, candidates []string) bool {
	for _, candidate := range candidates {
		if strings.Contains(value, candidate) {
			return true
		}
	}
	return false
}

func mutationRequiresIsolatedWorktree(manifest projects.Manifest) bool {
	return manifest.Policy.BranchRules.RequireWorktree != nil && *manifest.Policy.BranchRules.RequireWorktree
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
