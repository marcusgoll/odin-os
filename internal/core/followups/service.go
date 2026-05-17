package followups

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"odin-os/internal/core/workitems"
	memorycompanions "odin-os/internal/memory/companions"
	memoryprojects "odin-os/internal/memory/projects"
	runtimeevents "odin-os/internal/runtime/events"
	"odin-os/internal/store/sqlite"
)

type Service struct {
	Store *sqlite.Store
	Now   func() time.Time
}

const defaultTargetProjectKey = "odin-core"

func (service Service) Create(ctx context.Context, params CreateParams) (FollowUpObligation, error) {
	if service.Store == nil {
		return FollowUpObligation{}, fmt.Errorf("follow-up store is required")
	}
	if params.WorkspaceID <= 0 {
		return FollowUpObligation{}, fmt.Errorf("workspace ID is required")
	}
	if strings.TrimSpace(params.Title) == "" {
		return FollowUpObligation{}, fmt.Errorf("follow-up title is required")
	}
	if params.NextDueAt.IsZero() {
		return FollowUpObligation{}, fmt.Errorf("next due time is required")
	}
	if err := params.Cadence.Validate(); err != nil {
		return FollowUpObligation{}, err
	}
	if err := service.validateOwnership(ctx, params.WorkspaceID, params.InitiativeID, params.CompanionID); err != nil {
		return FollowUpObligation{}, err
	}
	targetProjectID, err := service.resolveTargetProjectID(ctx, params.InitiativeID, params.TargetProjectID)
	if err != nil {
		return FollowUpObligation{}, err
	}

	cadenceJSON, err := json.Marshal(params.Cadence)
	if err != nil {
		return FollowUpObligation{}, err
	}

	policyJSON := strings.TrimSpace(params.PolicyJSON)
	if policyJSON == "" {
		policyJSON = `{}`
	} else if !json.Valid([]byte(policyJSON)) {
		return FollowUpObligation{}, fmt.Errorf("policy JSON must be valid")
	}

	record, err := service.Store.CreateFollowUpObligation(ctx, sqlite.CreateFollowUpObligationParams{
		WorkspaceID:     params.WorkspaceID,
		InitiativeID:    params.InitiativeID,
		CompanionID:     params.CompanionID,
		TargetProjectID: targetProjectID,
		Title:           strings.TrimSpace(params.Title),
		Status:          string(StatusActive),
		CadenceJSON:     string(cadenceJSON),
		NextDueAt:       params.NextDueAt.UTC(),
		PolicyJSON:      policyJSON,
	})
	if err != nil {
		return FollowUpObligation{}, err
	}

	return decode(record)
}

func (service Service) Get(ctx context.Context, obligationID int64) (FollowUpObligation, error) {
	if service.Store == nil {
		return FollowUpObligation{}, fmt.Errorf("follow-up store is required")
	}

	record, err := service.Store.GetFollowUpObligation(ctx, obligationID)
	if err != nil {
		return FollowUpObligation{}, err
	}
	return decode(record)
}

func (service Service) Complete(ctx context.Context, workspaceID int64, obligationID int64) (FollowUpObligation, error) {
	if service.Store == nil {
		return FollowUpObligation{}, fmt.Errorf("follow-up store is required")
	}
	if workspaceID <= 0 {
		return FollowUpObligation{}, fmt.Errorf("workspace ID is required")
	}

	obligation, err := service.Get(ctx, obligationID)
	if err != nil {
		return FollowUpObligation{}, err
	}
	if obligation.WorkspaceID != workspaceID {
		return FollowUpObligation{}, fmt.Errorf("follow-up obligation %d does not belong to workspace %d", obligation.ID, workspaceID)
	}
	if isTerminalFollowUpStatus(obligation.Status) {
		return FollowUpObligation{}, fmt.Errorf("follow-up obligation %d is already %s", obligation.ID, obligation.Status)
	}

	now := service.now()
	params := sqlite.UpdateFollowUpObligationParams{
		ObligationID:    obligation.ID,
		LastCompletedAt: &now,
	}
	if obligation.Cadence.Mode == CadenceModeRecurring {
		nextDue, err := obligation.Cadence.NextDueAfter(obligation.NextDueAt)
		if err != nil {
			return FollowUpObligation{}, err
		}
		params.Status = string(StatusActive)
		params.NextDueAt = &nextDue
	} else {
		params.Status = string(StatusCompleted)
	}

	record, err := service.Store.UpdateFollowUpObligation(ctx, params)
	if err != nil {
		return FollowUpObligation{}, err
	}
	updated, err := decode(record)
	if err != nil {
		return FollowUpObligation{}, err
	}
	// Completion is the primary mutation; lifecycle memory is best-effort.
	_ = service.recordCompletionMemory(ctx, obligation, now)
	return updated, nil
}

func (service Service) Snooze(ctx context.Context, workspaceID int64, obligationID int64, until time.Time) (FollowUpObligation, error) {
	if service.Store == nil {
		return FollowUpObligation{}, fmt.Errorf("follow-up store is required")
	}
	if workspaceID <= 0 {
		return FollowUpObligation{}, fmt.Errorf("workspace ID is required")
	}
	if until.IsZero() {
		return FollowUpObligation{}, fmt.Errorf("snooze until time is required")
	}

	obligation, err := service.Get(ctx, obligationID)
	if err != nil {
		return FollowUpObligation{}, err
	}
	if obligation.WorkspaceID != workspaceID {
		return FollowUpObligation{}, fmt.Errorf("follow-up obligation %d does not belong to workspace %d", obligation.ID, workspaceID)
	}
	if isTerminalFollowUpStatus(obligation.Status) {
		return FollowUpObligation{}, fmt.Errorf("follow-up obligation %d is already %s", obligation.ID, obligation.Status)
	}

	now := service.now()
	if !until.UTC().After(now) {
		return FollowUpObligation{}, fmt.Errorf("snooze until time must be in the future")
	}

	record, err := service.Store.UpdateFollowUpObligation(ctx, sqlite.UpdateFollowUpObligationParams{
		ObligationID: obligation.ID,
		Status:       string(StatusActive),
		NextDueAt:    timePtr(until.UTC()),
	})
	if err != nil {
		return FollowUpObligation{}, err
	}
	return decode(record)
}

func (service Service) Pause(ctx context.Context, workspaceID int64, obligationID int64) (FollowUpObligation, error) {
	return service.pause(ctx, workspaceID, obligationID, "")
}

func (service Service) PauseForInitiativeStatus(ctx context.Context, workspaceID int64, obligationID int64, initiativeStatus string) (FollowUpObligation, error) {
	return service.pause(ctx, workspaceID, obligationID, initiativeStatus)
}

func (service Service) pause(ctx context.Context, workspaceID int64, obligationID int64, initiativeStatus string) (FollowUpObligation, error) {
	if service.Store == nil {
		return FollowUpObligation{}, fmt.Errorf("follow-up store is required")
	}
	if workspaceID <= 0 {
		return FollowUpObligation{}, fmt.Errorf("workspace ID is required")
	}

	obligation, err := service.Get(ctx, obligationID)
	if err != nil {
		return FollowUpObligation{}, err
	}
	if obligation.WorkspaceID != workspaceID {
		return FollowUpObligation{}, fmt.Errorf("follow-up obligation %d does not belong to workspace %d", obligation.ID, workspaceID)
	}
	if obligation.Status == StatusPaused || isTerminalFollowUpStatus(obligation.Status) {
		return obligation, nil
	}

	record, err := service.Store.UpdateFollowUpObligation(ctx, sqlite.UpdateFollowUpObligationParams{
		ObligationID: obligation.ID,
		Status:       string(StatusPaused),
	})
	if err != nil {
		return FollowUpObligation{}, err
	}
	updated, err := decode(record)
	if err != nil {
		return FollowUpObligation{}, err
	}
	if err := service.recordRuntimeEvent(ctx, runtimeevents.EventFollowUpPaused, updated, 0, runtimeevents.FollowUpPausedPayload{
		ObligationID:     updated.ID,
		Status:           string(StatusPaused),
		InitiativeStatus: strings.TrimSpace(initiativeStatus),
	}); err != nil {
		return FollowUpObligation{}, err
	}
	return updated, nil
}

func (service Service) ListByWorkspace(ctx context.Context, workspaceID int64) ([]FollowUpObligation, error) {
	if service.Store == nil {
		return nil, fmt.Errorf("follow-up store is required")
	}

	records, err := service.Store.ListFollowUpObligations(ctx, sqlite.ListFollowUpObligationsParams{
		WorkspaceID: workspaceID,
	})
	if err != nil {
		return nil, err
	}

	obligations := make([]FollowUpObligation, 0, len(records))
	for _, record := range records {
		obligation, err := decode(record)
		if err != nil {
			return nil, err
		}
		obligations = append(obligations, obligation)
	}
	return obligations, nil
}

func (service Service) Materialize(ctx context.Context, params MaterializeParams) (MaterializationResult, error) {
	if service.Store == nil {
		return MaterializationResult{}, fmt.Errorf("follow-up store is required")
	}
	if strings.TrimSpace(params.TaskKey) == "" {
		return MaterializationResult{}, fmt.Errorf("task key is required")
	}

	obligation, err := service.Get(ctx, params.ObligationID)
	if err != nil {
		return MaterializationResult{}, err
	}
	if obligation.DueStatus(service.now()) != StatusDue {
		return MaterializationResult{}, fmt.Errorf("follow-up obligation %d is not due", obligation.ID)
	}
	if obligation.TargetProjectID <= 0 {
		return MaterializationResult{}, fmt.Errorf("follow-up obligation %d has no target project", obligation.ID)
	}

	title := strings.TrimSpace(params.Title)
	if title == "" {
		title = obligation.Title
	}
	scope := strings.TrimSpace(params.Scope)
	if scope == "" {
		scope = "project"
	}
	requestedBy := strings.TrimSpace(params.RequestedBy)
	if requestedBy == "" {
		requestedBy = "operator"
	}

	task, reused, err := workitems.Service{Store: service.Store}.QueueFollowUp(ctx, workitems.QueueFollowUpParams{
		CreateTask: sqlite.CreateTaskParams{
			ProjectID:    obligation.TargetProjectID,
			Key:          strings.TrimSpace(params.TaskKey),
			Title:        title,
			ActionKey:    strings.TrimSpace(params.ActionKey),
			Status:       strings.TrimSpace(params.TaskStatus),
			Scope:        scope,
			RequestedBy:  requestedBy,
			WorkspaceID:  int64Ptr(obligation.WorkspaceID),
			InitiativeID: obligation.InitiativeID,
			CompanionID:  obligation.CompanionID,
			WorkKind:     "follow_up",
		},
		FollowUpObligationID: obligation.ID,
		OccurrenceKey:        obligation.OccurrenceKey(),
	})
	if err != nil {
		return MaterializationResult{}, err
	}
	if !reused {
		record, err := service.Store.RecordFollowUpMaterialization(ctx, sqlite.RecordFollowUpMaterializationParams{
			ObligationID:       obligation.ID,
			LastMaterializedAt: service.now().UTC(),
		})
		if err != nil {
			return MaterializationResult{}, err
		}
		obligation, err = decode(record)
		if err != nil {
			return MaterializationResult{}, err
		}

		if err := service.recordRuntimeEvent(ctx, runtimeevents.EventFollowUpMaterialized, obligation, task.ID, runtimeevents.FollowUpMaterializedPayload{
			ObligationID:  obligation.ID,
			TaskID:        task.ID,
			OccurrenceKey: obligation.OccurrenceKey(),
			TaskStatus:    task.Status,
			Reused:        reused,
		}); err != nil {
			return MaterializationResult{}, err
		}
	}

	return MaterializationResult{
		Obligation:    obligation,
		TaskID:        task.ID,
		OccurrenceKey: obligation.OccurrenceKey(),
		Reused:        reused,
	}, nil
}

func (service Service) now() time.Time {
	if service.Now != nil {
		return service.Now()
	}
	return time.Now().UTC()
}

func (service Service) recordCompletionMemory(ctx context.Context, obligation FollowUpObligation, completedAt time.Time) error {
	if obligation.CompanionID != nil {
		companionMemory := memorycompanions.Service{Store: service.Store}
		if obligation.ScheduleState(completedAt, DefaultOverdueGrace) == ScheduleStateOverdue {
			if _, err := companionMemory.RememberFollowUpOverdue(ctx, obligation.WorkspaceID, *obligation.CompanionID, obligation.Title, obligation.NextDueAt); err != nil {
				return err
			}
		}
		_, err := companionMemory.RememberFollowUpCompletion(ctx, obligation.WorkspaceID, *obligation.CompanionID, obligation.Title, completedAt)
		return err
	}
	if obligation.InitiativeID != nil {
		projectMemory := memoryprojects.Service{Store: service.Store}
		if obligation.ScheduleState(completedAt, DefaultOverdueGrace) == ScheduleStateOverdue {
			if _, err := projectMemory.RememberFollowUpOverdue(ctx, obligation.WorkspaceID, *obligation.InitiativeID, obligation.Title, obligation.NextDueAt); err != nil {
				return err
			}
		}
		_, err := projectMemory.RememberFollowUpCompletion(ctx, obligation.WorkspaceID, *obligation.InitiativeID, obligation.Title, completedAt)
		return err
	}
	return nil
}

func (service Service) recordRuntimeEvent(ctx context.Context, eventType runtimeevents.Type, obligation FollowUpObligation, taskID int64, payload any) error {
	if service.Store == nil {
		return fmt.Errorf("follow-up store is required")
	}

	encoded, err := runtimeevents.EncodePayload(payload)
	if err != nil {
		return err
	}

	var taskArg any
	if taskID > 0 {
		taskArg = taskID
	}

	_, err = service.Store.DB().ExecContext(ctx, `
		INSERT INTO events (
			stream_type,
			stream_id,
			event_type,
			event_version,
			scope,
			project_id,
			task_id,
			run_id,
			payload_json,
			occurred_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		string(runtimeevents.StreamFollowUp),
		obligation.ID,
		string(eventType),
		1,
		"workspace",
		obligation.TargetProjectID,
		taskArg,
		nil,
		string(encoded),
		service.now().UTC().Format(time.RFC3339Nano),
	)
	return err
}

func (service Service) validateOwnership(ctx context.Context, workspaceID int64, initiativeID *int64, companionID *int64) error {
	if initiativeID != nil {
		initiative, err := service.Store.GetInitiativeByID(ctx, *initiativeID)
		if err != nil {
			return err
		}
		if initiative.WorkspaceID != workspaceID {
			return fmt.Errorf("initiative %d does not belong to workspace %d", *initiativeID, workspaceID)
		}
	}

	if companionID != nil {
		companion, err := service.Store.GetCompanionByID(ctx, *companionID)
		if err != nil {
			return err
		}
		if companion.WorkspaceID != workspaceID {
			return fmt.Errorf("companion %d does not belong to workspace %d", *companionID, workspaceID)
		}
	}

	return nil
}

func (service Service) resolveTargetProjectID(ctx context.Context, initiativeID *int64, explicitTargetID *int64) (int64, error) {
	if initiativeID != nil {
		initiative, err := service.Store.GetInitiativeByID(ctx, *initiativeID)
		if err != nil {
			return 0, err
		}
		if initiative.Kind == "managed_project" && initiative.LinkedProjectID != nil {
			return *initiative.LinkedProjectID, nil
		}
	}

	if explicitTargetID != nil {
		if _, err := service.Store.GetProject(ctx, *explicitTargetID); err != nil {
			return 0, err
		}
		return *explicitTargetID, nil
	}

	defaultProject, err := service.Store.GetProjectByKey(ctx, defaultTargetProjectKey)
	if err != nil {
		return 0, err
	}
	return defaultProject.ID, nil
}

func decode(record sqlite.FollowUpObligation) (FollowUpObligation, error) {
	var cadence Cadence
	if err := json.Unmarshal([]byte(record.CadenceJSON), &cadence); err != nil {
		return FollowUpObligation{}, err
	}
	if err := cadence.Validate(); err != nil {
		return FollowUpObligation{}, err
	}

	return FollowUpObligation{
		ID:                 record.ID,
		WorkspaceID:        record.WorkspaceID,
		InitiativeID:       record.InitiativeID,
		CompanionID:        record.CompanionID,
		TargetProjectID:    record.TargetProjectID,
		Title:              record.Title,
		Status:             Status(record.Status),
		Cadence:            cadence,
		NextDueAt:          record.NextDueAt,
		LastMaterializedAt: record.LastMaterializedAt,
		LastCompletedAt:    record.LastCompletedAt,
		PolicyJSON:         record.PolicyJSON,
		CreatedAt:          record.CreatedAt,
		UpdatedAt:          record.UpdatedAt,
	}, nil
}

func int64Ptr(value int64) *int64 {
	return &value
}

func timePtr(value time.Time) *time.Time {
	return &value
}

func isTerminalFollowUpStatus(status Status) bool {
	switch status {
	case StatusCompleted, StatusSkipped, StatusArchived:
		return true
	default:
		return false
	}
}

func IsNotFound(err error) bool {
	return errors.Is(err, sql.ErrNoRows)
}
