package delegations

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"odin-os/internal/cli/scope"
	"odin-os/internal/learning/proposals"
	"odin-os/internal/prompting"
	"odin-os/internal/registry"
	"odin-os/internal/runtime/checkpoints"
	runtimeevents "odin-os/internal/runtime/events"
	jobsvc "odin-os/internal/runtime/jobs"
	"odin-os/internal/store/sqlite"
)

type Service struct {
	Store            *sqlite.Store
	Jobs             jobsvc.Service
	Checkpoints      checkpoints.Service
	RegistrySnapshot registry.Snapshot
}

type RunInput struct {
	ResolvedScope scope.Resolution
	AgentKey      string
	RequestedBy   string
	CompanionID   int64
	Intent        string
	Inputs        map[string]string
}

type RunResult struct {
	ParentTask          sqlite.Task
	ParentRun           *sqlite.Run
	ChildDelegations    []sqlite.Delegation
	LearningProposalIDs []int64
	Reused              bool
	Reason              string
}

type RetryResult struct {
	Delegation sqlite.Delegation
	ParentTask *sqlite.Task
	ParentRun  *sqlite.Run
	ChildTask  *sqlite.Task
	ChildRun   *sqlite.Run
	Retried    bool
	Reason     string
}

type childExecutionResult struct {
	index      int
	spec       ChildSpec
	delegation sqlite.Delegation
	proposalID int64
	err        error
}

func (service Service) RetryDelegation(ctx context.Context, delegationID int64) (RetryResult, error) {
	if service.Store == nil {
		return RetryResult{}, fmt.Errorf("delegation store is required")
	}
	if service.Jobs.Store == nil {
		return RetryResult{}, fmt.Errorf("jobs service is required")
	}

	delegation, err := service.Store.GetDelegation(ctx, delegationID)
	if err != nil {
		return RetryResult{}, err
	}
	result, err := service.retryResult(ctx, delegation, false, "loaded")
	if err != nil {
		return RetryResult{}, err
	}
	if delegation.Status == "completed" {
		if err := service.Store.RecordDelegationRetryEvent(ctx, sqlite.RecordDelegationRetryEventParams{
			DelegationID: delegation.ID,
			EventType:    runtimeevents.EventDelegationRetrySkipped,
			Reason:       "already_completed",
		}); err != nil {
			return RetryResult{}, err
		}
		result.Retried = false
		result.Reason = "already_completed"
		return result, nil
	}
	if !isRetryableDelegationStatus(delegation.Status) {
		if err := service.Store.RecordDelegationRetryEvent(ctx, sqlite.RecordDelegationRetryEventParams{
			DelegationID: delegation.ID,
			EventType:    runtimeevents.EventDelegationRetrySkipped,
			Reason:       "not_retryable:" + delegation.Status,
		}); err != nil {
			return RetryResult{}, err
		}
		result.Retried = false
		result.Reason = "not_retryable:" + delegation.Status
		return result, nil
	}
	if delegation.ChildTaskID == nil {
		if err := service.Store.RecordDelegationRetryEvent(ctx, sqlite.RecordDelegationRetryEventParams{
			DelegationID: delegation.ID,
			EventType:    runtimeevents.EventDelegationRetrySkipped,
			Reason:       "missing_child_task",
		}); err != nil {
			return RetryResult{}, err
		}
		result.Retried = false
		result.Reason = "missing_child_task"
		return result, nil
	}
	childTask, err := service.Store.GetTask(ctx, *delegation.ChildTaskID)
	if err != nil {
		return RetryResult{}, err
	}
	if service.delegationRetryBlockedByApproval(ctx, childTask) {
		if err := service.Store.RecordDelegationRetryEvent(ctx, sqlite.RecordDelegationRetryEventParams{
			DelegationID: delegation.ID,
			EventType:    runtimeevents.EventDelegationRetrySkipped,
			Reason:       "approval_required",
		}); err != nil {
			return RetryResult{}, err
		}
		result, err := service.retryResult(ctx, delegation, false, "approval_required")
		if err != nil {
			return RetryResult{}, err
		}
		return result, nil
	}
	if err := service.Store.RecordDelegationRetryEvent(ctx, sqlite.RecordDelegationRetryEventParams{
		DelegationID: delegation.ID,
		EventType:    runtimeevents.EventDelegationRetryRequested,
		Reason:       "operator_retry",
	}); err != nil {
		return RetryResult{}, err
	}

	inputs := retryInputsFromDelegation(delegation)
	agentKey := cleanInput(inputs["agent_key"])
	spec := childSpecFromDelegation(delegation, inputs)

	delegation, err = service.Store.UpdateDelegationStatus(ctx, sqlite.UpdateDelegationStatusParams{
		DelegationID: delegation.ID,
		Status:       "running",
	})
	if err != nil {
		return RetryResult{}, fmt.Errorf("mark delegation running: %w", err)
	}

	requestMetadata := retryRequestMetadata(delegation, agentKey, spec, inputs)
	promptOverride, err := service.childPrompt(spec, inputs)
	if err != nil {
		return RetryResult{}, fmt.Errorf("build child prompt: %w", err)
	}
	outcome, execErr := service.Jobs.ExecuteTaskWithRequest(ctx, childTask.ID, jobsvc.ExecutionRequest{
		PromptOverride: promptOverride,
		Metadata:       requestMetadata,
	})

	childStatus, statusErr := delegationStatusFromOutcome(outcome, execErr)
	if statusErr != nil && execErr == nil {
		execErr = statusErr
	}
	if outcome.Run != nil {
		delegation, err = service.Store.AttachDelegationChildTask(ctx, sqlite.AttachDelegationChildTaskParams{
			DelegationID: delegation.ID,
			ChildTaskID:  childTask.ID,
			ChildRunID:   &outcome.Run.ID,
		})
		if err != nil {
			return RetryResult{}, fmt.Errorf("attach child run: %w", err)
		}
	}
	delegation, err = service.Store.UpdateDelegationStatus(ctx, sqlite.UpdateDelegationStatusParams{
		DelegationID: delegation.ID,
		Status:       childStatus,
	})
	if err != nil {
		return RetryResult{}, fmt.Errorf("mark delegation %s: %w", childStatus, err)
	}
	if artifactErr := service.recordDelegationArtifacts(ctx, delegation, outcome.Task, outcome, requestMetadata); artifactErr != nil {
		return RetryResult{}, fmt.Errorf("record delegation artifacts: %w", artifactErr)
	}
	if err := service.reconcileParentAfterRetry(ctx, delegation); err != nil {
		return RetryResult{}, err
	}

	result, err = service.retryResult(ctx, delegation, true, "retried")
	if err != nil {
		return RetryResult{}, err
	}
	if execErr != nil {
		return result, fmt.Errorf("execute child task: %w", execErr)
	}
	return result, nil
}

func (service Service) RunAgent(ctx context.Context, input RunInput) (sqlite.Task, *sqlite.Run, RunResult, error) {
	if service.Store == nil {
		return sqlite.Task{}, nil, RunResult{}, fmt.Errorf("delegation store is required")
	}
	if service.Jobs.Store == nil {
		return sqlite.Task{}, nil, RunResult{}, fmt.Errorf("jobs service is required")
	}
	if strings.TrimSpace(input.AgentKey) == "" {
		return sqlite.Task{}, nil, RunResult{}, fmt.Errorf("agent key is required")
	}

	childSpecs, err := service.childSpecsForAgent(input.AgentKey, input.Inputs)
	if err != nil {
		return sqlite.Task{}, nil, RunResult{}, err
	}

	requestedBy := cleanInput(input.RequestedBy)
	if requestedBy == "" {
		requestedBy = "operator"
	}

	parentTaskResult, err := service.Jobs.CreateTaskOnce(ctx, jobsvc.CreateTaskParams{
		Resolved:              input.ResolvedScope,
		Title:                 parentTaskTitle(input.AgentKey, input.Inputs),
		RequestedBy:           requestedBy,
		Key:                   delegationParentTaskKey(input),
		CompanionID:           input.CompanionID,
		ExecutionIntent:       delegationRunIntent(input.Intent),
		ExecutionIntentSource: "companion_delegate",
	})
	if err != nil {
		return sqlite.Task{}, nil, RunResult{}, fmt.Errorf("create parent task: %w", err)
	}
	parentTask := parentTaskResult.Task
	if !parentTaskResult.Created {
		result, err := service.reusedRunResult(ctx, parentTask, childSpecs)
		if err != nil {
			return parentTask, nil, result, err
		}
		return parentTask, result.ParentRun, result, nil
	}

	parentRun, err := service.startParentRun(ctx, parentTask, input)
	if err != nil {
		return parentTask, nil, RunResult{
			ParentTask: parentTask,
		}, fmt.Errorf("start parent run: %w", err)
	}
	result := RunResult{
		ParentTask:       parentTask,
		ParentRun:        &parentRun,
		ChildDelegations: make([]sqlite.Delegation, len(childSpecs)),
	}

	for _, wave := range childSpecWaves(childSpecs) {
		waveResults := service.runChildWave(ctx, parentTask, &parentRun, input, childSpecs, wave)
		var firstErr error
		for _, childResult := range waveResults {
			if childResult.delegation.ID != 0 {
				result.ChildDelegations[childResult.index] = childResult.delegation
			}
			if childResult.proposalID != 0 {
				result.LearningProposalIDs = append(result.LearningProposalIDs, childResult.proposalID)
			}
			if firstErr == nil && childResult.err != nil {
				firstErr = childResult.err
			}
		}
		if firstErr != nil {
			result.ChildDelegations = compactDelegations(result.ChildDelegations)
			parentTask, parentRun, finishErr := service.completeParentRun(ctx, parentTask, parentRun, input, result, "failed", firstErr.Error())
			if finishErr != nil {
				return parentTask, &parentRun, result, finishErr
			}
			result.ParentTask = parentTask
			result.ParentRun = &parentRun
			return parentTask, &parentRun, result, fmt.Errorf("child wave failed: %w", firstErr)
		}
	}
	result.ChildDelegations = compactDelegations(result.ChildDelegations)

	parentTask, parentRun, err = service.completeParentRun(
		ctx,
		parentTask,
		parentRun,
		input,
		result,
		"completed",
		fmt.Sprintf("%s coordinated %d child delegations for %s %s", input.AgentKey, len(result.ChildDelegations), cleanInput(input.Inputs["portal_track"]), cleanInput(input.Inputs["surface"])),
	)
	if err != nil {
		return parentTask, &parentRun, result, err
	}
	result.ParentTask = parentTask
	result.ParentRun = &parentRun
	return parentTask, &parentRun, result, nil
}

func (service Service) reusedRunResult(ctx context.Context, parentTask sqlite.Task, childSpecs []ChildSpec) (RunResult, error) {
	delegations, err := service.Store.ListDelegations(ctx, sqlite.ListDelegationsParams{ParentTaskID: &parentTask.ID})
	if err != nil {
		return RunResult{}, err
	}
	if len(delegations) == 0 {
		return RunResult{}, fmt.Errorf("existing delegation parent task %s has no child delegations", parentTask.Key)
	}
	delegations = orderDelegationsBySpecs(delegations, childSpecs)
	reason := existingDelegationReason(delegations)
	for _, delegation := range delegations {
		if err := service.Store.RecordDelegationReuseEvent(ctx, sqlite.RecordDelegationReuseEventParams{
			DelegationID: delegation.ID,
			Reason:       reason,
		}); err != nil {
			return RunResult{}, err
		}
	}

	var parentRun *sqlite.Run
	for _, delegation := range delegations {
		if delegation.ParentRunID == nil {
			continue
		}
		run, err := service.Store.GetRun(ctx, *delegation.ParentRunID)
		if err != nil {
			return RunResult{}, err
		}
		parentRun = &run
		break
	}

	return RunResult{
		ParentTask:       parentTask,
		ParentRun:        parentRun,
		ChildDelegations: delegations,
		Reused:           true,
		Reason:           reason,
	}, nil
}

func (service Service) runChildWave(ctx context.Context, parentTask sqlite.Task, parentRun *sqlite.Run, input RunInput, childSpecs []ChildSpec, indexes []int) []childExecutionResult {
	results := make([]childExecutionResult, 0, len(indexes))
	resultCh := make(chan childExecutionResult, len(indexes))

	var wg sync.WaitGroup
	for _, index := range indexes {
		spec := childSpecs[index]
		wg.Add(1)
		go func(index int, spec ChildSpec) {
			defer wg.Done()

			delegation, err := service.runChildDelegation(ctx, parentTask, parentRun, input, spec)
			result := childExecutionResult{
				index:      index,
				spec:       spec,
				delegation: delegation,
				err:        err,
			}
			if err == nil && spec.Role == "learning_capture" {
				proposalID, proposalErr := service.recordLearningProposal(ctx, parentTask.ProjectID, input, delegation)
				if proposalErr != nil {
					result.err = proposalErr
				} else {
					result.proposalID = proposalID
					if _, artifactErr := service.Store.CreateDelegationArtifact(ctx, sqlite.CreateDelegationArtifactParams{
						DelegationID: delegation.ID,
						ArtifactType: "learning_proposal",
						Summary:      fmt.Sprintf("learning proposal %d submitted", proposalID),
						DetailsJSON:  fmt.Sprintf(`{"proposal_id":%d,"status":"submitted"}`, proposalID),
					}); artifactErr != nil {
						result.err = artifactErr
					}
				}
			}

			resultCh <- result
		}(index, spec)
	}

	wg.Wait()
	close(resultCh)
	for result := range resultCh {
		results = append(results, result)
	}
	return results
}

func (service Service) runChildDelegation(ctx context.Context, parentTask sqlite.Task, parentRun *sqlite.Run, input RunInput, spec ChildSpec) (sqlite.Delegation, error) {
	if parentRun == nil {
		return sqlite.Delegation{}, fmt.Errorf("parent run is required for child delegation")
	}

	detailsJSON, err := json.Marshal(map[string]string{
		"agent_key":               input.AgentKey,
		"portal_track":            cleanInput(input.Inputs["portal_track"]),
		"surface":                 cleanInput(input.Inputs["surface"]),
		"goal":                    cleanInput(input.Inputs["goal"]),
		"skill_key":               spec.SkillKey,
		"role":                    spec.Role,
		"execution_intent":        delegationExecutionIntent(spec.MutationMode),
		"execution_intent_source": "companion_delegate",
	})
	if err != nil {
		return sqlite.Delegation{}, fmt.Errorf("create delegation: %w", err)
	}

	delegation, err := service.Store.CreateDelegation(ctx, sqlite.CreateDelegationParams{
		ParentTaskID:    parentTask.ID,
		ParentRunID:     &parentRun.ID,
		ProjectID:       parentTask.ProjectID,
		Scope:           parentTask.Scope,
		DelegationKey:   spec.DelegationKey,
		Role:            spec.Role,
		ActionClass:     spec.ActionClass,
		ActionKey:       spec.ActionKey,
		MutationMode:    spec.MutationMode,
		ConvergenceMode: spec.ConvergenceMode,
		ArtifactTarget:  spec.ArtifactTarget,
		Executor:        spec.Executor,
		DetailsJSON:     string(detailsJSON),
	})
	if err != nil {
		return sqlite.Delegation{}, fmt.Errorf("create child task: %w", err)
	}

	childTask, err := service.Jobs.CreateTask(ctx, jobsvc.CreateTaskParams{
		Resolved:              input.ResolvedScope,
		Title:                 childTaskTitle(spec, input.Inputs),
		RequestedBy:           "agent:" + input.AgentKey,
		CompanionID:           input.CompanionID,
		ExecutionIntent:       delegationExecutionIntent(spec.MutationMode),
		ExecutionIntentSource: "companion_delegate",
	})
	if err != nil {
		return sqlite.Delegation{}, err
	}

	delegation, err = service.Store.AttachDelegationChildTask(ctx, sqlite.AttachDelegationChildTaskParams{
		DelegationID: delegation.ID,
		ChildTaskID:  childTask.ID,
	})
	if err != nil {
		return sqlite.Delegation{}, fmt.Errorf("attach child task: %w", err)
	}

	if err := service.recordChildCheckpoint(ctx, childTask, input, delegation, spec); err != nil {
		return sqlite.Delegation{}, fmt.Errorf("record child checkpoint: %w", err)
	}

	delegation, err = service.Store.UpdateDelegationStatus(ctx, sqlite.UpdateDelegationStatusParams{
		DelegationID: delegation.ID,
		Status:       "running",
	})
	if err != nil {
		return sqlite.Delegation{}, fmt.Errorf("mark delegation running: %w", err)
	}

	requestMetadata := map[string]string{
		"agent_key":               input.AgentKey,
		"delegation_id":           strconv.FormatInt(delegation.ID, 10),
		"portal_track":            cleanInput(input.Inputs["portal_track"]),
		"delegation_key":          spec.DelegationKey,
		"child_role":              spec.Role,
		"execution_intent":        delegationExecutionIntent(spec.MutationMode),
		"execution_intent_source": "companion_delegate",
	}
	if spec.SkillKey != "" {
		requestMetadata["requested_skill"] = spec.SkillKey
		requestMetadata["effective_skill"] = spec.SkillKey
		requestMetadata["skill_source"] = "agent_template"
	}

	promptOverride, err := service.childPrompt(spec, input.Inputs)
	if err != nil {
		return sqlite.Delegation{}, fmt.Errorf("build child prompt: %w", err)
	}

	outcome, execErr := service.Jobs.ExecuteTaskWithRequest(ctx, childTask.ID, jobsvc.ExecutionRequest{
		PromptOverride: promptOverride,
		Metadata:       requestMetadata,
	})

	childStatus, statusErr := delegationStatusFromOutcome(outcome, execErr)
	if statusErr != nil && execErr == nil {
		execErr = statusErr
	}
	if outcome.Run != nil {
		delegation, err = service.Store.AttachDelegationChildTask(ctx, sqlite.AttachDelegationChildTaskParams{
			DelegationID: delegation.ID,
			ChildTaskID:  childTask.ID,
			ChildRunID:   &outcome.Run.ID,
		})
		if err != nil {
			return sqlite.Delegation{}, fmt.Errorf("attach child run: %w", err)
		}
	}
	delegation, err = service.Store.UpdateDelegationStatus(ctx, sqlite.UpdateDelegationStatusParams{
		DelegationID: delegation.ID,
		Status:       childStatus,
	})
	if err != nil {
		return sqlite.Delegation{}, fmt.Errorf("mark delegation %s: %w", childStatus, err)
	}

	if artifactErr := service.recordDelegationArtifacts(ctx, delegation, childTask, outcome, requestMetadata); artifactErr != nil {
		return sqlite.Delegation{}, fmt.Errorf("record delegation artifacts: %w", artifactErr)
	}
	if execErr != nil {
		return delegation, fmt.Errorf("execute child task: %w", execErr)
	}
	return delegation, nil
}

func (service Service) retryResult(ctx context.Context, delegation sqlite.Delegation, retried bool, reason string) (RetryResult, error) {
	current, err := service.Store.GetDelegation(ctx, delegation.ID)
	if err != nil {
		return RetryResult{}, err
	}
	result := RetryResult{
		Delegation: current,
		Retried:    retried,
		Reason:     reason,
	}
	if parentTask, err := service.Store.GetTask(ctx, current.ParentTaskID); err == nil {
		result.ParentTask = &parentTask
	} else {
		return RetryResult{}, err
	}
	if current.ParentRunID != nil {
		parentRun, err := service.Store.GetRun(ctx, *current.ParentRunID)
		if err != nil {
			return RetryResult{}, err
		}
		result.ParentRun = &parentRun
	}
	if current.ChildTaskID != nil {
		childTask, err := service.Store.GetTask(ctx, *current.ChildTaskID)
		if err != nil {
			return RetryResult{}, err
		}
		result.ChildTask = &childTask
	}
	if current.ChildRunID != nil {
		childRun, err := service.Store.GetRun(ctx, *current.ChildRunID)
		if err != nil {
			return RetryResult{}, err
		}
		result.ChildRun = &childRun
	}
	return result, nil
}

func (service Service) reconcileParentAfterRetry(ctx context.Context, delegation sqlite.Delegation) error {
	siblings, err := service.Store.ListDelegations(ctx, sqlite.ListDelegationsParams{ParentTaskID: &delegation.ParentTaskID})
	if err != nil {
		return err
	}
	if len(siblings) == 0 {
		return nil
	}
	for _, sibling := range siblings {
		if sibling.Status != "completed" {
			return nil
		}
	}
	parentTask, err := service.Store.GetTask(ctx, delegation.ParentTaskID)
	if err != nil {
		return err
	}
	if parentTask.Status == "completed" {
		return nil
	}
	if delegation.ParentRunID == nil {
		_, err := service.Store.UpdateTaskStatus(ctx, sqlite.UpdateTaskStatusParams{
			TaskID:  parentTask.ID,
			Status:  "completed",
			Summary: "delegation retry recovered all child delegations",
		})
		return err
	}
	_, _, err = service.Store.FinishRunAndSetTaskStatus(ctx, sqlite.FinishRunAndSetTaskStatusParams{
		RunID:      *delegation.ParentRunID,
		RunStatus:  "completed",
		Summary:    "delegation retry recovered all child delegations",
		TaskStatus: "completed",
	})
	return err
}

func delegationStatusFromOutcome(outcome jobsvc.ExecutionOutcome, execErr error) (string, error) {
	if execErr != nil {
		return "failed", execErr
	}
	if outcome.Run != nil && isFailedDelegationRunStatus(outcome.Run.Status) {
		return outcome.Run.Status, fmt.Errorf("child run finished with status %s", outcome.Run.Status)
	}
	if outcome.Run == nil && isFailedDelegationTaskStatus(outcome.Task.Status) {
		return outcome.Task.Status, fmt.Errorf("child task finished with status %s", outcome.Task.Status)
	}
	return "completed", nil
}

func isFailedDelegationRunStatus(status string) bool {
	switch strings.TrimSpace(strings.ToLower(status)) {
	case "failed", "dead_letter", "timeout", "cancelled":
		return true
	default:
		return false
	}
}

func isRetryableDelegationStatus(status string) bool {
	switch strings.TrimSpace(strings.ToLower(status)) {
	case "failed", "dead_letter", "timeout", "cancelled", "blocked", "approval_required":
		return true
	default:
		return false
	}
}

func isFailedDelegationTaskStatus(status string) bool {
	switch strings.TrimSpace(strings.ToLower(status)) {
	case "failed", "dead_letter", "timeout", "cancelled", "blocked", "approval_required":
		return true
	default:
		return false
	}
}

func (service Service) delegationRetryBlockedByApproval(ctx context.Context, childTask sqlite.Task) bool {
	if strings.TrimSpace(childTask.BlockedReason) == "approval_required" {
		return true
	}
	approval, err := service.Store.GetLatestTaskApproval(ctx, childTask.ID)
	if err != nil {
		return false
	}
	return strings.TrimSpace(approval.Status) == "pending"
}

func delegationParentTaskKey(input RunInput) string {
	agent := cleanInput(input.AgentKey)
	portalTrack := cleanInput(input.Inputs["portal_track"])
	surface := cleanInput(input.Inputs["surface"])
	goal := cleanInput(input.Inputs["goal"])
	requestedBy := cleanInput(input.RequestedBy)
	digestInput := strings.Join([]string{
		requestedBy,
		agent,
		portalTrack,
		surface,
		goal,
		delegationRunIntent(input.Intent),
	}, "\x00")
	sum := sha256.Sum256([]byte(digestInput))
	digest := hex.EncodeToString(sum[:])[:16]
	return strings.Join(compactNonEmpty([]string{
		"delegate",
		keySegment(agent),
		keySegment(portalTrack),
		keySegment(surface),
		digest,
	}), "-")
}

func keySegment(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var builder strings.Builder
	lastDash := false
	for _, char := range value {
		switch {
		case char >= 'a' && char <= 'z':
			builder.WriteRune(char)
			lastDash = false
		case char >= '0' && char <= '9':
			builder.WriteRune(char)
			lastDash = false
		default:
			if !lastDash && builder.Len() > 0 {
				builder.WriteByte('-')
				lastDash = true
			}
		}
		if builder.Len() >= 36 {
			break
		}
	}
	result := strings.Trim(builder.String(), "-")
	if result == "" {
		return "request"
	}
	return result
}

func compactNonEmpty(values []string) []string {
	compacted := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.Trim(value, "-")
		if value != "" {
			compacted = append(compacted, value)
		}
	}
	return compacted
}

func orderDelegationsBySpecs(delegations []sqlite.Delegation, specs []ChildSpec) []sqlite.Delegation {
	byKey := make(map[string][]sqlite.Delegation, len(delegations))
	for _, delegation := range delegations {
		byKey[delegation.DelegationKey] = append(byKey[delegation.DelegationKey], delegation)
	}
	ordered := make([]sqlite.Delegation, 0, len(delegations))
	seen := make(map[int64]bool, len(delegations))
	for _, spec := range specs {
		items := byKey[spec.DelegationKey]
		for _, delegation := range items {
			if seen[delegation.ID] {
				continue
			}
			ordered = append(ordered, delegation)
			seen[delegation.ID] = true
			break
		}
	}
	for _, delegation := range delegations {
		if !seen[delegation.ID] {
			ordered = append(ordered, delegation)
		}
	}
	return ordered
}

func existingDelegationReason(delegations []sqlite.Delegation) string {
	for _, delegation := range delegations {
		if isRetryableDelegationStatus(delegation.Status) {
			return "existing_failed_use_retry"
		}
	}
	return "existing_delegation_tree"
}

func retryInputsFromDelegation(delegation sqlite.Delegation) map[string]string {
	inputs := map[string]string{}
	var details map[string]string
	if err := json.Unmarshal([]byte(delegation.DetailsJSON), &details); err == nil {
		for key, value := range details {
			inputs[key] = value
		}
	}
	if inputs["role"] == "" {
		inputs["role"] = delegation.Role
	}
	return inputs
}

func delegationRunIntent(value string) string {
	intent := strings.ToLower(strings.TrimSpace(value))
	switch intent {
	case "mutation", "governance", "destructive":
		return intent
	default:
		return "read_only"
	}
}

func childSpecFromDelegation(delegation sqlite.Delegation, inputs map[string]string) ChildSpec {
	return ChildSpec{
		DelegationKey:   delegation.DelegationKey,
		Role:            delegation.Role,
		ActionClass:     delegation.ActionClass,
		ActionKey:       delegation.ActionKey,
		MutationMode:    delegation.MutationMode,
		ConvergenceMode: delegation.ConvergenceMode,
		ArtifactTarget:  delegation.ArtifactTarget,
		Executor:        delegation.Executor,
		SkillKey:        cleanInput(inputs["skill_key"]),
	}
}

func retryRequestMetadata(delegation sqlite.Delegation, agentKey string, spec ChildSpec, inputs map[string]string) map[string]string {
	metadata := map[string]string{
		"agent_key":      agentKey,
		"delegation_id":  strconv.FormatInt(delegation.ID, 10),
		"portal_track":   cleanInput(inputs["portal_track"]),
		"delegation_key": delegation.DelegationKey,
		"child_role":     spec.Role,
		"retry":          "true",
	}
	if spec.SkillKey != "" {
		metadata["requested_skill"] = spec.SkillKey
		metadata["effective_skill"] = spec.SkillKey
		metadata["skill_source"] = "agent_template"
	}
	return metadata
}

func (service Service) recordChildCheckpoint(ctx context.Context, childTask sqlite.Task, input RunInput, delegation sqlite.Delegation, spec ChildSpec) error {
	checkpointService := service.Checkpoints
	if checkpointService.Store == nil {
		checkpointService.Store = service.Store
	}

	selectedCapabilities := []string{"agent:" + input.AgentKey}
	if spec.SkillKey != "" {
		selectedCapabilities = append(selectedCapabilities, "skill:"+spec.SkillKey)
	}

	_, err := checkpointService.Compact(ctx, checkpoints.CompactParams{
		TaskID:               childTask.ID,
		Trigger:              checkpoints.TriggerHandoff,
		CheckpointKey:        fmt.Sprintf("delegation-%d", delegation.ID),
		Objective:            childTask.Title,
		TaskStatus:           childTask.Status,
		LastCompletedStep:    "delegation created",
		NextSteps:            []string{"execute child run"},
		SelectedCapabilities: selectedCapabilities,
		Evidence: []checkpoints.Evidence{
			{
				Kind:    "delegation",
				Summary: fmt.Sprintf("delegation %d created for %s", delegation.ID, spec.Role),
				Ref:     strconv.FormatInt(delegation.ID, 10),
			},
		},
		ManifestSummary: fmt.Sprintf("agent=%s portal_track=%s surface=%s", input.AgentKey, cleanInput(input.Inputs["portal_track"]), cleanInput(input.Inputs["surface"])),
		PolicySummary:   "delegated child execution inherits project policy and task authority",
		OpenTaskSummary: "swarm child execution",
	})
	return err
}

func (service Service) recordDelegationArtifacts(ctx context.Context, delegation sqlite.Delegation, childTask sqlite.Task, outcome jobsvc.ExecutionOutcome, metadata map[string]string) error {
	runSummary := ""
	runDetails := map[string]any{
		"task_id":     childTask.ID,
		"task_key":    childTask.Key,
		"task_status": outcome.Task.Status,
	}
	for _, key := range []string{"agent_key", "delegation_id", "portal_track", "delegation_key", "child_role", "requested_skill", "effective_skill", "skill_source", "execution_intent", "execution_intent_source"} {
		if value := cleanInput(metadata[key]); value != "" {
			runDetails[key] = value
		}
	}
	if outcome.Run != nil {
		runSummary = strings.TrimSpace(outcome.Run.Summary)
		runDetails["run_id"] = outcome.Run.ID
		runDetails["run_status"] = outcome.Run.Status
		runDetails["executor"] = outcome.Run.Executor
	}
	if runSummary == "" {
		runSummary = fmt.Sprintf("child task %s finished with status %s", childTask.Key, outcome.Task.Status)
	}
	runDetailsJSON, err := json.Marshal(runDetails)
	if err != nil {
		return err
	}
	if _, err := service.Store.CreateDelegationArtifact(ctx, sqlite.CreateDelegationArtifactParams{
		DelegationID: delegation.ID,
		ArtifactType: "run_summary",
		Summary:      runSummary,
		DetailsJSON:  string(runDetailsJSON),
	}); err != nil {
		return err
	}

	if outcome.Run == nil {
		return nil
	}
	summaries, err := service.Store.ListMemorySummaries(ctx, sqlite.ListMemorySummariesParams{
		ProjectID:  &childTask.ProjectID,
		TaskID:     &childTask.ID,
		RunID:      &outcome.Run.ID,
		Scope:      childTask.Scope,
		MemoryType: "episode",
	})
	if err != nil {
		return err
	}
	if len(summaries) == 0 {
		return nil
	}
	latest := summaries[len(summaries)-1]
	detailsJSON, err := json.Marshal(map[string]any{
		"memory_summary_id": latest.ID,
		"memory_type":       latest.MemoryType,
		"run_id":            outcome.Run.ID,
	})
	if err != nil {
		return err
	}
	_, err = service.Store.CreateDelegationArtifact(ctx, sqlite.CreateDelegationArtifactParams{
		DelegationID: delegation.ID,
		ArtifactType: "memory_summary",
		Summary:      latest.Summary,
		DetailsJSON:  string(detailsJSON),
	})
	return err
}

func (service Service) recordLearningProposal(ctx context.Context, projectID int64, input RunInput, delegation sqlite.Delegation) (int64, error) {
	proposalService := proposals.Service{Store: service.Store}
	portalTrack := cleanInput(input.Inputs["portal_track"])
	surface := cleanInput(input.Inputs["surface"])
	targetKey := fmt.Sprintf("%s:%s", portalTrack, surface)
	changePayload, err := json.Marshal(map[string]string{
		"agent_key":       input.AgentKey,
		"delegation_key":  delegation.DelegationKey,
		"portal_track":    portalTrack,
		"surface":         surface,
		"recommended_use": "persist the strongest validated portal delivery learning",
	})
	if err != nil {
		return 0, err
	}
	proposal, err := proposalService.Create(ctx, proposals.CreateInput{
		ProjectID:         &projectID,
		ProposalType:      "portal_delivery_learning",
		Scope:             "project",
		TargetKey:         targetKey,
		Summary:           fmt.Sprintf("%s %s delivery learning", portalTrack, surface),
		Hypothesis:        fmt.Sprintf("Capturing the %s portal delivery pattern will improve future %s work.", portalTrack, surface),
		ChangePayloadJSON: string(changePayload),
		CreatedBy:         "agent:" + input.AgentKey,
	})
	if err != nil {
		return 0, err
	}
	if _, err := proposalService.Submit(ctx, proposal.ID); err != nil {
		return 0, err
	}
	return proposal.ID, nil
}

func (service Service) startParentRun(ctx context.Context, parentTask sqlite.Task, input RunInput) (sqlite.Run, error) {
	run, err := service.Store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:   parentTask.ID,
		Executor: input.AgentKey,
		Attempt:  1,
		Status:   "running",
	})
	if err != nil {
		return sqlite.Run{}, err
	}
	if _, err := service.Store.UpdateTaskStatus(ctx, sqlite.UpdateTaskStatusParams{
		TaskID: parentTask.ID,
		Status: "running",
	}); err != nil {
		return sqlite.Run{}, err
	}
	return run, nil
}

func (service Service) completeParentRun(ctx context.Context, parentTask sqlite.Task, parentRun sqlite.Run, input RunInput, result RunResult, status string, summary string) (sqlite.Task, sqlite.Run, error) {
	taskStatus := "completed"
	switch status {
	case "failed":
		taskStatus = "failed"
	case "cancelled":
		taskStatus = "cancelled"
	}

	if _, _, err := service.Store.FinishRunIfRunning(ctx, sqlite.FinishRunParams{
		RunID:   parentRun.ID,
		Status:  status,
		Summary: summary,
	}); err != nil {
		return sqlite.Task{}, sqlite.Run{}, fmt.Errorf("finish parent run: %w", err)
	}
	if _, err := service.Store.UpdateTaskStatus(ctx, sqlite.UpdateTaskStatusParams{
		TaskID: parentTask.ID,
		Status: taskStatus,
	}); err != nil {
		return sqlite.Task{}, sqlite.Run{}, fmt.Errorf("update parent task status: %w", err)
	}

	updatedTask, err := service.Store.GetTask(ctx, parentTask.ID)
	if err != nil {
		return sqlite.Task{}, sqlite.Run{}, fmt.Errorf("reload parent task: %w", err)
	}
	updatedRun, err := service.Store.GetRun(ctx, parentRun.ID)
	if err != nil {
		return sqlite.Task{}, sqlite.Run{}, fmt.Errorf("reload parent run: %w", err)
	}
	if err := service.recordParentEvidence(ctx, updatedTask, updatedRun, input, result, summary); err != nil {
		return sqlite.Task{}, sqlite.Run{}, fmt.Errorf("record parent evidence: %w", err)
	}
	return updatedTask, updatedRun, nil
}

func (service Service) recordParentEvidence(ctx context.Context, parentTask sqlite.Task, parentRun sqlite.Run, input RunInput, result RunResult, summary string) error {
	project, err := service.Store.GetProject(ctx, parentTask.ProjectID)
	if err != nil {
		return fmt.Errorf("load parent project: %w", err)
	}

	toolSummaryBytes, err := json.Marshal(map[string]string{
		"agent_key":               input.AgentKey,
		"portal_track":            cleanInput(input.Inputs["portal_track"]),
		"surface":                 cleanInput(input.Inputs["surface"]),
		"run_status":              parentRun.Status,
		"task_status":             parentTask.Status,
		"child_delegation_count":  strconv.Itoa(len(result.ChildDelegations)),
		"learning_proposal_count": strconv.Itoa(len(result.LearningProposalIDs)),
	})
	if err != nil {
		return err
	}
	transcript, err := service.Store.RecordConversationTranscript(ctx, sqlite.RecordConversationTranscriptParams{
		ProjectID:   &project.ID,
		TaskID:      &parentTask.ID,
		RunID:       &parentRun.ID,
		Scope:       parentTask.Scope,
		ScopeKey:    project.Key,
		Mode:        "act",
		Prompt:      parentPrompt(input.AgentKey, input.Inputs),
		Response:    strings.TrimSpace(summary),
		ToolSummary: string(toolSummaryBytes),
		Executor:    parentRun.Executor,
	})
	if err != nil {
		return fmt.Errorf("record parent transcript: %w", err)
	}

	detailsBytes, err := json.Marshal(map[string]any{
		"task_key":              parentTask.Key,
		"task_status":           parentTask.Status,
		"run_status":            parentRun.Status,
		"agent_key":             input.AgentKey,
		"portal_track":          cleanInput(input.Inputs["portal_track"]),
		"surface":               cleanInput(input.Inputs["surface"]),
		"child_delegation_ids":  delegationIDs(result.ChildDelegations),
		"learning_proposal_ids": append([]int64(nil), result.LearningProposalIDs...),
	})
	if err != nil {
		return err
	}
	_, err = service.Store.RecordMemorySummary(ctx, sqlite.RecordMemorySummaryParams{
		ProjectID:          &project.ID,
		SourceTranscriptID: &transcript.ID,
		TaskID:             &parentTask.ID,
		RunID:              &parentRun.ID,
		Scope:              parentTask.Scope,
		ScopeKey:           project.Key,
		MemoryType:         "episode",
		Summary:            summary,
		DetailsJSON:        string(detailsBytes),
	})
	if err != nil {
		return fmt.Errorf("record parent memory summary: %w", err)
	}
	return nil
}

func parentTaskTitle(agentKey string, inputs map[string]string) string {
	portalTrack := cleanInput(inputs["portal_track"])
	surface := cleanInput(inputs["surface"])
	goal := cleanInput(inputs["goal"])
	if goal == "" {
		goal = fmt.Sprintf("deliver %s %s", portalTrack, surface)
	}
	return fmt.Sprintf("%s %s", agentKey, goal)
}

func parentPrompt(agentKey string, inputs map[string]string) string {
	return fmt.Sprintf(
		"Run %s for portal_track=%s surface=%s goal=%s. Coordinate child work for IA audit, design direction, implementation handoff, visual verification, and learning capture.",
		agentKey,
		cleanInput(inputs["portal_track"]),
		cleanInput(inputs["surface"]),
		cleanInput(inputs["goal"]),
	)
}

func childTaskTitle(spec ChildSpec, inputs map[string]string) string {
	return fmt.Sprintf("%s %s %s", cleanInput(inputs["portal_track"]), cleanInput(inputs["surface"]), strings.ReplaceAll(spec.Role, "_", " "))
}

func (service Service) childPrompt(spec ChildSpec, inputs map[string]string) (string, error) {
	prompt := fmt.Sprintf(
		"Portal delivery child role=%s portal_track=%s surface=%s goal=%s. Produce a concise, implementation-ready result.",
		spec.Role,
		cleanInput(inputs["portal_track"]),
		cleanInput(inputs["surface"]),
		cleanInput(inputs["goal"]),
	)
	if spec.SkillKey == "" {
		return prompt, nil
	}

	item, ok := service.RegistrySnapshot.ByKey[spec.SkillKey]
	if !ok || item.Kind != registry.KindSkill {
		return "", fmt.Errorf("delegation skill %q is not available", spec.SkillKey)
	}
	return prompting.ComposeSkillPrompt(prompt, item), nil
}

func cleanInput(value string) string {
	return strings.TrimSpace(value)
}

func delegationExecutionIntent(mutationMode string) string {
	switch strings.ToLower(strings.TrimSpace(mutationMode)) {
	case "mutation", "mutating", "write":
		return "mutation"
	case "governance":
		return "governance"
	case "destructive":
		return "destructive"
	default:
		return "read_only"
	}
}

func delegationIDs(delegations []sqlite.Delegation) []int64 {
	ids := make([]int64, 0, len(delegations))
	for _, delegation := range delegations {
		ids = append(ids, delegation.ID)
	}
	return ids
}

func childSpecWaves(childSpecs []ChildSpec) [][]int {
	if len(childSpecs) == 0 {
		return nil
	}

	waves := make([][]int, 0, len(childSpecs))
	currentWave := childSpecs[0].Wave
	current := make([]int, 0, len(childSpecs))
	for index, spec := range childSpecs {
		if len(current) == 0 {
			currentWave = spec.Wave
		}
		if spec.Wave != currentWave {
			waves = append(waves, current)
			current = make([]int, 0, len(childSpecs)-index)
			currentWave = spec.Wave
		}
		current = append(current, index)
	}
	if len(current) > 0 {
		waves = append(waves, current)
	}
	return waves
}

func compactDelegations(items []sqlite.Delegation) []sqlite.Delegation {
	compacted := make([]sqlite.Delegation, 0, len(items))
	for _, item := range items {
		if item.ID == 0 {
			continue
		}
		compacted = append(compacted, item)
	}
	return compacted
}
