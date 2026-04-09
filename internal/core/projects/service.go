package projects

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"odin-os/internal/store/sqlite"
)

var ErrTransitionDenied = errors.New("project transition denied")

type Service struct {
	Store *sqlite.Store
}

type TransitionStateInput struct {
	ProjectID      int64
	Actor          TransitionController
	TargetState    TransitionState
	LimitedActions []string
	ChangedBy      string
	Notes          string
}

type ReportInput struct {
	ProjectID   int64
	Actor       TransitionController
	Summary     string
	DetailsJSON string
}

type ActionInput struct {
	ProjectID   int64
	Actor       TransitionController
	ActionClass ActionClass
	ActionKey   string
}

func (service Service) SetTransitionState(ctx context.Context, input TransitionStateInput) (sqlite.ProjectTransition, error) {
	if service.Store == nil {
		return sqlite.ProjectTransition{}, fmt.Errorf("transition store is required")
	}

	current, found, err := service.loadTransition(ctx, input.ProjectID)
	if err != nil {
		return sqlite.ProjectTransition{}, err
	}

	if !found {
		decision := initialTransitionDecision(input.Actor, input.TargetState)
		if !decision.Allowed {
			_ = service.Store.RecordProjectTransitionDenied(ctx, input.ProjectID, string(ActionClassTransitionControl), decision.Reason)
			return sqlite.ProjectTransition{}, fmt.Errorf("%w: %s", ErrTransitionDenied, decision.Reason)
		}
	} else {
		decision := ValidateTransitionChange(current, TransitionChangeRequest{
			Actor:       input.Actor,
			TargetState: input.TargetState,
		})
		if !decision.Allowed {
			_ = service.Store.RecordProjectTransitionDenied(ctx, input.ProjectID, string(ActionClassTransitionControl), decision.Reason)
			return sqlite.ProjectTransition{}, fmt.Errorf("%w: %s", ErrTransitionDenied, decision.Reason)
		}
	}

	limitedActionsJSON, err := normalizeLimitedActions(input.TargetState, input.LimitedActions)
	if err != nil {
		_ = service.Store.RecordProjectTransitionDenied(ctx, input.ProjectID, string(ActionClassTransitionControl), err.Error())
		return sqlite.ProjectTransition{}, fmt.Errorf("%w: %s", ErrTransitionDenied, err.Error())
	}

	return service.Store.SetProjectTransition(ctx, sqlite.SetProjectTransitionParams{
		ProjectID:          input.ProjectID,
		State:              string(input.TargetState),
		Controller:         string(controllerForState(input.TargetState)),
		LimitedActionsJSON: limitedActionsJSON,
		Notes:              input.Notes,
		ChangedBy:          input.ChangedBy,
	})
}

func (service Service) RecordShadowObservation(ctx context.Context, input ReportInput) (sqlite.ProjectTransitionReport, error) {
	return service.recordReport(ctx, input, TransitionStateShadow, "shadow_observation")
}

func (service Service) RecordCompareReport(ctx context.Context, input ReportInput) (sqlite.ProjectTransitionReport, error) {
	return service.recordReport(ctx, input, TransitionStateCompare, "compare_report")
}

func (service Service) AuthorizeAction(ctx context.Context, input ActionInput) (TransitionDecision, error) {
	if service.Store == nil {
		return TransitionDecision{}, fmt.Errorf("transition store is required")
	}

	current, _, err := service.loadTransition(ctx, input.ProjectID)
	if err != nil {
		return TransitionDecision{}, err
	}

	decision := AuthorizeTransitionAction(TransitionAuthRequest{
		Transition:  current,
		Actor:       input.Actor,
		ActionClass: input.ActionClass,
		ActionKey:   input.ActionKey,
	})
	if !decision.Allowed {
		_ = service.Store.RecordProjectTransitionDenied(ctx, input.ProjectID, string(input.ActionClass), decision.Reason)
		return decision, fmt.Errorf("%w: %s", ErrTransitionDenied, decision.Reason)
	}

	return decision, nil
}

func (service Service) recordReport(ctx context.Context, input ReportInput, requiredState TransitionState, reportType string) (sqlite.ProjectTransitionReport, error) {
	if service.Store == nil {
		return sqlite.ProjectTransitionReport{}, fmt.Errorf("transition store is required")
	}

	current, _, err := service.loadTransition(ctx, input.ProjectID)
	if err != nil {
		return sqlite.ProjectTransitionReport{}, err
	}
	if current.State != requiredState {
		reason := fmt.Sprintf("%s reports require state %q", reportType, requiredState)
		_ = service.Store.RecordProjectTransitionDenied(ctx, input.ProjectID, string(ActionClassReadOnly), reason)
		return sqlite.ProjectTransitionReport{}, fmt.Errorf("%w: %s", ErrTransitionDenied, reason)
	}

	return service.Store.RecordProjectTransitionReport(ctx, sqlite.RecordProjectTransitionReportParams{
		ProjectID:   input.ProjectID,
		ReportType:  reportType,
		Summary:     input.Summary,
		DetailsJSON: input.DetailsJSON,
	})
}

func (service Service) loadTransition(ctx context.Context, projectID int64) (RuntimeTransition, bool, error) {
	record, err := service.Store.GetProjectTransition(ctx, projectID)
	if err == nil {
		return RuntimeTransition{
			State:          TransitionState(record.State),
			Controller:     TransitionController(record.Controller),
			LimitedActions: decodeLimitedActions(record.LimitedActionsJSON),
		}, true, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return RuntimeTransition{
			State:      TransitionStateInventory,
			Controller: TransitionControllerLegacyOdin,
		}, false, nil
	}
	return RuntimeTransition{}, false, err
}

func initialTransitionDecision(actor TransitionController, target TransitionState) TransitionDecision {
	if target == "" {
		return TransitionDecision{Allowed: false, Reason: "target transition state is required"}
	}
	if actor != TransitionControllerOdinOS {
		return TransitionDecision{
			Allowed: false,
			Reason:  fmt.Sprintf("controller %q cannot initialize transition state", actor),
		}
	}
	return TransitionDecision{Allowed: true}
}

func controllerForState(state TransitionState) TransitionController {
	switch state {
	case TransitionStateLimitedAction, TransitionStateCutover, TransitionStateDecommissioned:
		return TransitionControllerOdinOS
	default:
		return TransitionControllerLegacyOdin
	}
}

func normalizeLimitedActions(state TransitionState, limitedActions []string) (string, error) {
	if state != TransitionStateLimitedAction {
		return "", nil
	}
	if len(limitedActions) == 0 {
		return "", fmt.Errorf("limited_action requires at least one allowlisted action")
	}
	encoded, err := json.Marshal(limitedActions)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func decodeLimitedActions(encoded string) []string {
	if encoded == "" {
		return nil
	}
	var decoded []string
	if err := json.Unmarshal([]byte(encoded), &decoded); err != nil {
		return nil
	}
	return decoded
}
