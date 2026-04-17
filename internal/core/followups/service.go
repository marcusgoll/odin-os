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
	"odin-os/internal/store/sqlite"
)

type Service struct {
	Store *sqlite.Store
	Now   func() time.Time
}

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
		WorkspaceID:  params.WorkspaceID,
		InitiativeID: params.InitiativeID,
		CompanionID:  params.CompanionID,
		Title:        strings.TrimSpace(params.Title),
		Status:       string(StatusActive),
		CadenceJSON:  string(cadenceJSON),
		NextDueAt:    params.NextDueAt.UTC(),
		PolicyJSON:   policyJSON,
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
	if params.ProjectID <= 0 {
		return MaterializationResult{}, fmt.Errorf("project ID is required")
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
			ProjectID:    params.ProjectID,
			Key:          strings.TrimSpace(params.TaskKey),
			Title:        title,
			ActionKey:    strings.TrimSpace(params.ActionKey),
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

func IsNotFound(err error) bool {
	return errors.Is(err, sql.ErrNoRows)
}
