package checkpoints

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"odin-os/internal/store/sqlite"
)

type Service struct {
	Store *sqlite.Store
}

type SealWakePacketParams struct {
	PacketID          int64
	BlockingReason    string
	LastCompletedStep string
}

type CompactParams struct {
	TaskID                 int64
	RunID                  *int64
	Trigger                Trigger
	CheckpointKey          string
	Objective              string
	TaskStatus             string
	BlockingReason         string
	LastCompletedStep      string
	NextSteps              []string
	Constraints            []string
	SelectedCapabilities   []string
	Evidence               []Evidence
	ManifestSummary        string
	PolicySummary          string
	OpenTaskSummary        string
	ApprovalSummary        string
	Invocation             *InvocationContext
	ToolResults            []ToolResult
	ProjectFacts           map[string]string
	RunFacts               map[string]string
	SupersedesWakePacketID *int64
}

type CompactionResult struct {
	ProjectPacket sqlite.ContextPacket
	RunPacket     *sqlite.ContextPacket
	WakePacket    sqlite.ContextPacket
	Project       ProjectContext
	Run           *RunContext
	Wake          TaskWakePacket
}

func (service Service) Compact(ctx context.Context, params CompactParams) (CompactionResult, error) {
	if service.Store == nil {
		return CompactionResult{}, fmt.Errorf("checkpoint store is required")
	}

	task, err := service.Store.GetTask(ctx, params.TaskID)
	if err != nil {
		return CompactionResult{}, err
	}
	project, err := service.Store.GetProject(ctx, task.ProjectID)
	if err != nil {
		return CompactionResult{}, err
	}

	checkpointKey := params.CheckpointKey
	if checkpointKey == "" {
		checkpointKey = fmt.Sprintf("%s-%d", params.Trigger, task.ID)
	}

	projectContext := ProjectContext{
		ProjectID:       project.ID,
		ProjectKey:      project.Key,
		Scope:           project.Scope,
		ManifestSummary: params.ManifestSummary,
		PolicySummary:   params.PolicySummary,
		OpenTaskSummary: params.OpenTaskSummary,
		Facts:           cloneFacts(params.ProjectFacts),
	}

	projectPayload, err := marshalPayload(projectContext)
	if err != nil {
		return CompactionResult{}, err
	}

	projectPacket, err := service.Store.CreateContextPacket(ctx, sqlite.CreateContextPacketParams{
		TaskID:        &task.ID,
		RunID:         params.RunID,
		PacketKind:    "context",
		PacketScope:   string(PacketScopeProjectContext),
		Trigger:       string(params.Trigger),
		CheckpointKey: checkpointKey + ":project",
		Status:        string(PacketStatusActive),
		Summary:       compactProjectSummary(project, params.OpenTaskSummary),
		PayloadJSON:   projectPayload,
	})
	if err != nil {
		return CompactionResult{}, err
	}

	var runPacket *sqlite.ContextPacket
	var runContext *RunContext
	if params.RunID != nil {
		run, err := service.Store.GetRun(ctx, *params.RunID)
		if err != nil {
			return CompactionResult{}, err
		}

		assembledRun := RunContext{
			RunID:           run.ID,
			TaskID:          run.TaskID,
			Executor:        run.Executor,
			Attempt:         run.Attempt,
			Status:          run.Status,
			ApprovalSummary: params.ApprovalSummary,
			Invocation:      params.Invocation,
			ToolResults:     append([]ToolResult(nil), params.ToolResults...),
			Facts:           cloneFacts(params.RunFacts),
		}

		runPayload, err := marshalPayload(assembledRun)
		if err != nil {
			return CompactionResult{}, err
		}

		createdRunPacket, err := service.Store.CreateContextPacket(ctx, sqlite.CreateContextPacketParams{
			TaskID:        &task.ID,
			RunID:         params.RunID,
			PacketKind:    "context",
			PacketScope:   string(PacketScopeRunContext),
			Trigger:       string(params.Trigger),
			CheckpointKey: checkpointKey + ":run",
			Status:        string(PacketStatusActive),
			Summary:       compactRunSummary(run, params.ApprovalSummary),
			PayloadJSON:   runPayload,
		})
		if err != nil {
			return CompactionResult{}, err
		}

		runPacket = &createdRunPacket
		runContext = &assembledRun
	}

	supersedes := params.SupersedesWakePacketID
	if supersedes == nil {
		latest, err := service.Store.GetLatestTaskWakePacket(ctx, project.ID, task.ID)
		if err == nil {
			supersedes = &latest.ID
		} else if !errors.Is(err, sql.ErrNoRows) {
			return CompactionResult{}, err
		}
	}

	wake := TaskWakePacket{
		TaskID:                 task.ID,
		TaskKey:                task.Key,
		Scope:                  task.Scope,
		Objective:              stringOrFallback(params.Objective, task.Title),
		Status:                 stringOrFallback(params.TaskStatus, task.Status),
		Trigger:                params.Trigger,
		BlockingReason:         params.BlockingReason,
		LastCompletedStep:      params.LastCompletedStep,
		NextSteps:              append([]string(nil), params.NextSteps...),
		Constraints:            append([]string(nil), params.Constraints...),
		SelectedCapabilities:   append([]string(nil), params.SelectedCapabilities...),
		Evidence:               append([]Evidence(nil), params.Evidence...),
		ProjectContextPacketID: int64Ptr(projectPacket.ID),
	}
	if runPacket != nil {
		wake.RunContextPacketID = int64Ptr(runPacket.ID)
	}

	wakePayload, err := marshalPayload(wake)
	if err != nil {
		return CompactionResult{}, err
	}

	wakePacket, err := service.Store.CreateContextPacket(ctx, sqlite.CreateContextPacketParams{
		TaskID:             &task.ID,
		RunID:              params.RunID,
		PacketKind:         "wake",
		PacketScope:        string(PacketScopeTaskWake),
		Trigger:            string(params.Trigger),
		CheckpointKey:      checkpointKey + ":wake",
		SupersedesPacketID: supersedes,
		Status:             string(defaultWakeStatus(params.Trigger)),
		Summary:            compactWakeSummary(wake),
		PayloadJSON:        wakePayload,
	})
	if err != nil {
		return CompactionResult{}, err
	}

	return CompactionResult{
		ProjectPacket: projectPacket,
		RunPacket:     runPacket,
		WakePacket:    wakePacket,
		Project:       projectContext,
		Run:           runContext,
		Wake:          wake,
	}, nil
}

func cloneFacts(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(input))
	for key, value := range input {
		cloned[key] = value
	}
	return cloned
}

func (service Service) LoadResumeState(ctx context.Context, projectID int64, taskID int64) (ResumeState, error) {
	if service.Store == nil {
		return ResumeState{}, fmt.Errorf("checkpoint store is required")
	}

	packet, err := service.Store.GetLatestActiveTaskWakePacket(ctx, projectID, taskID)
	if err != nil {
		return ResumeState{}, err
	}

	wake, err := unmarshalPayload[TaskWakePacket](packet.PayloadJSON)
	if err != nil {
		return ResumeState{}, err
	}

	state := ResumeState{
		TaskID:          wake.TaskID,
		TaskKey:         wake.TaskKey,
		Scope:           wake.Scope,
		Objective:       wake.Objective,
		Status:          wake.Status,
		Trigger:         wake.Trigger,
		BlockingReason:  wake.BlockingReason,
		NextSteps:       append([]string(nil), wake.NextSteps...),
		Constraints:     append([]string(nil), wake.Constraints...),
		Capabilities:    append([]string(nil), wake.SelectedCapabilities...),
		WakePacketID:    packet.ID,
		ProjectPacketID: wake.ProjectContextPacketID,
		RunPacketID:     wake.RunContextPacketID,
	}

	if wake.ProjectContextPacketID != nil {
		projectPacket, err := service.Store.GetContextPacket(ctx, *wake.ProjectContextPacketID)
		if err != nil {
			return ResumeState{}, err
		}
		projectContext, err := unmarshalPayload[ProjectContext](projectPacket.PayloadJSON)
		if err != nil {
			return ResumeState{}, err
		}
		state.ProjectContext = &projectContext
	}

	if wake.RunContextPacketID != nil {
		runPacket, err := service.Store.GetContextPacket(ctx, *wake.RunContextPacketID)
		if err != nil {
			return ResumeState{}, err
		}
		runContext, err := unmarshalPayload[RunContext](runPacket.PayloadJSON)
		if err != nil {
			return ResumeState{}, err
		}
		state.RunContext = &runContext
	}

	return state, nil
}

func (service Service) SealWakePacket(ctx context.Context, params SealWakePacketParams) (sqlite.ContextPacket, error) {
	if service.Store == nil {
		return sqlite.ContextPacket{}, fmt.Errorf("checkpoint store is required")
	}
	packet, err := service.Store.GetContextPacket(ctx, params.PacketID)
	if err != nil {
		return sqlite.ContextPacket{}, err
	}
	wake, err := unmarshalPayload[TaskWakePacket](packet.PayloadJSON)
	if err != nil {
		return sqlite.ContextPacket{}, err
	}
	if params.BlockingReason != "" {
		wake.BlockingReason = params.BlockingReason
	}
	if params.LastCompletedStep != "" {
		wake.LastCompletedStep = params.LastCompletedStep
	}
	payloadJSON, err := marshalPayload(wake)
	if err != nil {
		return sqlite.ContextPacket{}, err
	}
	return service.Store.UpdateContextPacketStatus(ctx, sqlite.UpdateContextPacketStatusParams{
		PacketID:    params.PacketID,
		Status:      string(PacketStatusSealed),
		Summary:     compactWakeSummary(wake),
		PayloadJSON: payloadJSON,
	})
}

func marshalPayload(payload any) (string, error) {
	bytes, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

func unmarshalPayload[T any](payload string) (T, error) {
	var decoded T
	err := json.Unmarshal([]byte(payload), &decoded)
	return decoded, err
}

func defaultWakeStatus(trigger Trigger) PacketStatus {
	if trigger == TriggerCompletion {
		return PacketStatusSealed
	}
	return PacketStatusActive
}

func compactProjectSummary(project sqlite.Project, openTaskSummary string) string {
	if openTaskSummary == "" {
		return fmt.Sprintf("project %s context", project.Key)
	}
	return fmt.Sprintf("project %s context: %s", project.Key, openTaskSummary)
}

func compactRunSummary(run sqlite.Run, approvalSummary string) string {
	if approvalSummary == "" {
		return fmt.Sprintf("run %d on %s", run.ID, run.Executor)
	}
	return fmt.Sprintf("run %d on %s: %s", run.ID, run.Executor, approvalSummary)
}

func compactWakeSummary(wake TaskWakePacket) string {
	if wake.BlockingReason != "" {
		return fmt.Sprintf("%s: %s", wake.Objective, wake.BlockingReason)
	}
	return wake.Objective
}

func stringOrFallback(value string, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

func int64Ptr(value int64) *int64 {
	return &value
}
