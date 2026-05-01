package supervision

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"odin-os/internal/store/sqlite"
)

type Service struct {
	store  *sqlite.Store
	config Config
}

func NewService(store *sqlite.Store, config Config) Service {
	if config.ModeKey == "" {
		config.ModeKey = ModeKeyStage7SupervisedAgency
	}
	return Service{store: store, config: config}
}

func (service Service) Status(ctx context.Context) (Report, error) {
	if err := service.validateConfig(); err != nil {
		return Report{}, err
	}
	control, err := service.control(ctx)
	if err != nil {
		return Report{}, err
	}
	return Report{
		ModeKey:     service.config.ModeKey,
		Control:     controlState(control),
		SideEffects: notStartedSideEffects(),
	}, nil
}

func (service Service) Start(ctx context.Context, operator string) (Report, error) {
	if err := service.validateConfig(); err != nil {
		return Report{}, err
	}
	control, err := service.upsertControl(ctx, ControlStatusEnabled, false, operator)
	if err != nil {
		return Report{}, err
	}
	return Report{
		ModeKey:     service.config.ModeKey,
		Control:     controlState(control),
		SideEffects: notStartedSideEffects(),
	}, nil
}

func (service Service) Stop(ctx context.Context, operator string) (Report, error) {
	if err := service.validateConfig(); err != nil {
		return Report{}, err
	}
	control, err := service.upsertControl(ctx, ControlStatusStopped, true, operator)
	if err != nil {
		return Report{}, err
	}
	return Report{
		ModeKey:     service.config.ModeKey,
		Control:     controlState(control),
		SideEffects: notStartedSideEffects(),
	}, nil
}

func (service Service) Queue(ctx context.Context, project Project, issues []Issue) (Report, error) {
	if err := service.validateConfig(); err != nil {
		return Report{}, err
	}
	control, err := service.control(ctx)
	if err != nil {
		return Report{}, err
	}
	configHash, err := ConfigHash(service.config)
	if err != nil {
		return Report{}, err
	}

	activeClaims, err := service.activeClaims(ctx, project.ID, project.Repo)
	if err != nil {
		return Report{}, err
	}
	activeCount := len(activeClaims)
	activeClaimKeys := make(map[string]bool, len(activeClaims))
	for _, claim := range activeClaims {
		activeClaimKeys[claim.ClaimKey] = true
	}

	report := Report{
		ModeKey:     service.config.ModeKey,
		Control:     controlState(control),
		SideEffects: notStartedSideEffects(),
	}
	for _, issue := range issues {
		eligibility := EvaluateIssue(service.config, issue)
		decision := QueueDecision{
			ProjectKey:  project.Key,
			Repo:        project.Repo,
			IssueNumber: issue.Number,
			Eligible:    eligibility.Eligible,
		}
		switch {
		case control.KillSwitchActive || control.Status != ControlStatusEnabled:
			decision.Decision = DecisionRefused
			decision.RefusalReason = RefusalKillSwitchActive
		case !eligibility.Eligible:
			decision.Decision = DecisionRefused
			decision.RefusalReason = eligibility.RefusalReason
		default:
			claimKey := fmt.Sprintf("%s:%s:%d", service.config.ModeKey, project.Key, issue.Number)
			if activeCount >= service.config.MaxConcurrentTasks && !activeClaimKeys[claimKey] {
				decision.Decision = DecisionRefused
				decision.RefusalReason = RefusalConcurrencyLimit
				break
			}
			claim, err := service.store.UpsertSupervisionDispatchClaim(ctx, sqlite.UpsertSupervisionDispatchClaimParams{
				ProjectID:   project.ID,
				Repo:        project.Repo,
				IssueNumber: issue.Number,
				ClaimKey:    claimKey,
				Status:      ClaimStatusReserved,
				ConfigHash:  configHash,
				ClaimedBy:   supervisionServiceClaimedBy,
			})
			if err != nil {
				return Report{}, err
			}
			if !activeClaimKeys[claimKey] {
				activeCount++
				activeClaimKeys[claimKey] = true
			}
			decision.Decision = DecisionEligible
			decision.ClaimKey = claim.ClaimKey
			report.Claims = append(report.Claims, PlannedClaim{
				ProjectKey:  project.Key,
				Repo:        claim.Repo,
				IssueNumber: claim.IssueNumber,
				ClaimKey:    claim.ClaimKey,
				Status:      claim.Status,
				ClaimedAt:   claim.ClaimedAt,
			})
		}

		decision.DecidedAt, err = service.recordDecision(ctx, project, issue, decision, eligibility, configHash)
		if err != nil {
			return Report{}, err
		}
		report.Decisions = append(report.Decisions, decision)
	}
	return report, nil
}

func (service Service) Recover(ctx context.Context) (Report, error) {
	if err := service.validateConfig(); err != nil {
		return Report{}, err
	}
	control, err := service.control(ctx)
	if err != nil {
		return Report{}, err
	}
	configHash, err := ConfigHash(service.config)
	if err != nil {
		return Report{}, err
	}
	claims, err := service.activeClaims(ctx, 0, "")
	if err != nil {
		return Report{}, err
	}

	status := RecoveryStatusClean
	reason := RecoveryReasonNoStaleClaims
	for _, claim := range claims {
		if claim.ConfigHash != configHash {
			status = RecoveryStatusBlocked
			reason = RefusalRecoveryBlocked
			break
		}
	}

	details, err := json.Marshal(map[string]any{
		"active_claims": len(claims),
		"config_hash":   configHash,
	})
	if err != nil {
		return Report{}, err
	}
	if _, err := service.store.CreateSupervisionRecoveryObservation(ctx, sqlite.CreateSupervisionRecoveryObservationParams{
		ModeKey:         service.config.ModeKey,
		ObservationType: RecoveryObservationRestart,
		Status:          status,
		Reason:          reason,
		ConfigHash:      configHash,
		DetailsJSON:     string(details),
	}); err != nil {
		return Report{}, err
	}

	return Report{
		ModeKey: service.config.ModeKey,
		Control: controlState(control),
		Recovery: RecoveryReport{
			Status:       status,
			Reason:       reason,
			ActiveClaims: len(claims),
		},
		SideEffects: notStartedSideEffects(),
	}, nil
}

func (service Service) control(ctx context.Context) (sqlite.SupervisionControl, error) {
	control, err := service.store.GetSupervisionControl(ctx, service.config.ModeKey)
	if err == nil {
		if err := validatePersistedControl(control); err != nil {
			return sqlite.SupervisionControl{}, err
		}
		return control, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return sqlite.SupervisionControl{}, err
	}
	return service.upsertControl(ctx, ControlStatusStopped, true, "system")
}

func (service Service) validateConfig() error {
	return ValidateConfig(service.config)
}

func validatePersistedControl(control sqlite.SupervisionControl) error {
	if control.MaxConcurrentTasks != 1 {
		return fmt.Errorf("%w: persisted max_concurrent_tasks must be 1", ErrInvalidConfig)
	}
	if control.DryRun {
		return fmt.Errorf("%w: persisted dry_run must be false", ErrInvalidConfig)
	}
	if !control.RequireHumanApproval {
		return fmt.Errorf("%w: persisted require_human_approval must be true", ErrInvalidConfig)
	}
	return nil
}

func (service Service) upsertControl(ctx context.Context, status string, killSwitch bool, operator string) (sqlite.SupervisionControl, error) {
	configHash, err := ConfigHash(service.config)
	if err != nil {
		return sqlite.SupervisionControl{}, err
	}
	return service.store.UpsertSupervisionControl(ctx, sqlite.UpsertSupervisionControlParams{
		ModeKey:              service.config.ModeKey,
		Status:               status,
		KillSwitchActive:     killSwitch,
		ConfigHash:           configHash,
		MaxConcurrentTasks:   service.config.MaxConcurrentTasks,
		DryRun:               service.config.DryRun,
		RequireHumanApproval: service.config.RequireHumanApproval,
		UpdatedBy:            operator,
	})
}

func (service Service) activeClaims(ctx context.Context, projectID int64, repo string) ([]sqlite.SupervisionDispatchClaim, error) {
	var projectIDPtr *int64
	if projectID != 0 {
		projectIDPtr = &projectID
	}
	reserved, err := service.store.ListSupervisionDispatchClaims(ctx, sqlite.ListSupervisionDispatchClaimsParams{
		ProjectID: projectIDPtr,
		Repo:      repo,
		Status:    ClaimStatusReserved,
	})
	if err != nil {
		return nil, err
	}
	active, err := service.store.ListSupervisionDispatchClaims(ctx, sqlite.ListSupervisionDispatchClaimsParams{
		ProjectID: projectIDPtr,
		Repo:      repo,
		Status:    ClaimStatusActive,
	})
	if err != nil {
		return nil, err
	}
	return append(reserved, active...), nil
}

func (service Service) recordDecision(ctx context.Context, project Project, issue Issue, decision QueueDecision, eligibility Eligibility, configHash string) (time.Time, error) {
	payload, err := json.Marshal(map[string]any{
		"issue_title":   issue.Title,
		"labels":        eligibility.Labels,
		"changed_paths": eligibility.ChangedPaths,
		"claim_key":     decision.ClaimKey,
		"side_effects":  notStartedSideEffects(),
	})
	if err != nil {
		return time.Time{}, err
	}
	record, err := service.store.UpsertSupervisionQueueDecision(ctx, sqlite.UpsertSupervisionQueueDecisionParams{
		ProjectID:    project.ID,
		Repo:         project.Repo,
		IssueNumber:  issue.Number,
		Decision:     decision.Decision,
		Reason:       decisionReason(decision),
		ConfigHash:   configHash,
		DecisionJSON: string(payload),
	})
	if err != nil {
		return time.Time{}, err
	}
	return record.DecidedAt, nil
}

func decisionReason(decision QueueDecision) string {
	if decision.Decision == DecisionEligible {
		return DecisionEligible
	}
	return decision.RefusalReason
}

func controlState(control sqlite.SupervisionControl) ControlState {
	return ControlState{
		Status:               control.Status,
		KillSwitchActive:     control.KillSwitchActive,
		ConfigHash:           control.ConfigHash,
		MaxConcurrentTasks:   control.MaxConcurrentTasks,
		DryRun:               control.DryRun,
		RequireHumanApproval: control.RequireHumanApproval,
		UpdatedBy:            control.UpdatedBy,
	}
}
