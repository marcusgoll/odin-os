package delegations

import (
	"context"
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
	Inputs        map[string]string
}

type RunResult struct {
	ParentTask          sqlite.Task
	ParentRun           *sqlite.Run
	ChildDelegations    []sqlite.Delegation
	LearningProposalIDs []int64
}

type childExecutionResult struct {
	index      int
	spec       ChildSpec
	delegation sqlite.Delegation
	proposalID int64
	err        error
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

	childSpecs, err := childSpecsForAgent(input.AgentKey, input.Inputs)
	if err != nil {
		return sqlite.Task{}, nil, RunResult{}, err
	}

	requestedBy := cleanInput(input.RequestedBy)
	if requestedBy == "" {
		requestedBy = "operator"
	}

	parentTask, err := service.Jobs.CreateTask(ctx, jobsvc.CreateTaskParams{
		Resolved:    input.ResolvedScope,
		Title:       parentTaskTitle(input.AgentKey, input.Inputs),
		RequestedBy: requestedBy,
	})
	if err != nil {
		return sqlite.Task{}, nil, RunResult{}, err
	}

	parentRun, err := service.startParentRun(ctx, parentTask, input)
	if err != nil {
		return parentTask, nil, RunResult{
			ParentTask: parentTask,
		}, err
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
			return parentTask, &parentRun, result, firstErr
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
		"agent_key":    input.AgentKey,
		"portal_track": cleanInput(input.Inputs["portal_track"]),
		"surface":      cleanInput(input.Inputs["surface"]),
		"goal":         cleanInput(input.Inputs["goal"]),
		"skill_key":    spec.SkillKey,
		"role":         spec.Role,
	})
	if err != nil {
		return sqlite.Delegation{}, err
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
		return sqlite.Delegation{}, err
	}

	childTask, err := service.Jobs.CreateTask(ctx, jobsvc.CreateTaskParams{
		Resolved:    input.ResolvedScope,
		Title:       childTaskTitle(spec, input.Inputs),
		RequestedBy: "agent:" + input.AgentKey,
	})
	if err != nil {
		return sqlite.Delegation{}, err
	}

	delegation, err = service.Store.AttachDelegationChildTask(ctx, sqlite.AttachDelegationChildTaskParams{
		DelegationID: delegation.ID,
		ChildTaskID:  childTask.ID,
	})
	if err != nil {
		return sqlite.Delegation{}, err
	}

	if err := service.recordChildCheckpoint(ctx, childTask, input, delegation, spec); err != nil {
		return sqlite.Delegation{}, err
	}

	delegation, err = service.Store.UpdateDelegationStatus(ctx, sqlite.UpdateDelegationStatusParams{
		DelegationID: delegation.ID,
		Status:       "running",
	})
	if err != nil {
		return sqlite.Delegation{}, err
	}

	requestMetadata := map[string]string{
		"agent_key":      input.AgentKey,
		"delegation_id":  strconv.FormatInt(delegation.ID, 10),
		"portal_track":   cleanInput(input.Inputs["portal_track"]),
		"delegation_key": spec.DelegationKey,
		"child_role":     spec.Role,
	}
	if spec.SkillKey != "" {
		requestMetadata["requested_skill"] = spec.SkillKey
		requestMetadata["effective_skill"] = spec.SkillKey
		requestMetadata["skill_source"] = "agent_template"
	}

	promptOverride, err := service.childPrompt(spec, input.Inputs)
	if err != nil {
		return sqlite.Delegation{}, err
	}

	outcome, execErr := service.Jobs.ExecuteTaskWithRequest(ctx, childTask.ID, jobsvc.ExecutionRequest{
		PromptOverride: promptOverride,
		Metadata:       requestMetadata,
	})

	childStatus := "completed"
	if execErr != nil {
		childStatus = "failed"
	}
	if outcome.Run != nil {
		delegation, err = service.Store.AttachDelegationChildTask(ctx, sqlite.AttachDelegationChildTaskParams{
			DelegationID: delegation.ID,
			ChildTaskID:  childTask.ID,
			ChildRunID:   &outcome.Run.ID,
		})
		if err != nil {
			return sqlite.Delegation{}, err
		}
	}
	delegation, err = service.Store.UpdateDelegationStatus(ctx, sqlite.UpdateDelegationStatusParams{
		DelegationID: delegation.ID,
		Status:       childStatus,
	})
	if err != nil {
		return sqlite.Delegation{}, err
	}

	if artifactErr := service.recordDelegationArtifacts(ctx, delegation, childTask, outcome, requestMetadata); artifactErr != nil {
		return sqlite.Delegation{}, artifactErr
	}
	if execErr != nil {
		return delegation, execErr
	}
	return delegation, nil
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
	for _, key := range []string{"agent_key", "delegation_id", "portal_track", "delegation_key", "child_role", "requested_skill", "effective_skill", "skill_source"} {
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
		return sqlite.Task{}, sqlite.Run{}, err
	}
	if _, err := service.Store.UpdateTaskStatus(ctx, sqlite.UpdateTaskStatusParams{
		TaskID: parentTask.ID,
		Status: taskStatus,
	}); err != nil {
		return sqlite.Task{}, sqlite.Run{}, err
	}

	updatedTask, err := service.Store.GetTask(ctx, parentTask.ID)
	if err != nil {
		return sqlite.Task{}, sqlite.Run{}, err
	}
	updatedRun, err := service.Store.GetRun(ctx, parentRun.ID)
	if err != nil {
		return sqlite.Task{}, sqlite.Run{}, err
	}
	if err := service.recordParentEvidence(ctx, updatedTask, updatedRun, input, result, summary); err != nil {
		return sqlite.Task{}, sqlite.Run{}, err
	}
	return updatedTask, updatedRun, nil
}

func (service Service) recordParentEvidence(ctx context.Context, parentTask sqlite.Task, parentRun sqlite.Run, input RunInput, result RunResult, summary string) error {
	project, err := service.Store.GetProject(ctx, parentTask.ProjectID)
	if err != nil {
		return err
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
		return err
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
	return err
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
