package recovery

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	runtimeevents "odin-os/internal/runtime/events"
	"odin-os/internal/store/sqlite"
)

var (
	ErrUnknownPlaybook           = errors.New("unknown recovery playbook")
	ErrPlaybookScopeNotAllowed   = errors.New("playbook is not allowed in the current scope")
	ErrInvalidActionResultStatus = errors.New("invalid recovery action result status")
)

type Outcome struct {
	Status     string
	Suppressed bool
	Escalated  bool
	Attempt    int
	Incident   sqlite.Incident
	Recovery   sqlite.Recovery
}

type Executor struct {
	Store     *sqlite.Store
	Playbooks map[string]Playbook
	Now       func() time.Time
}

func (executor Executor) Execute(ctx context.Context, decision Decision) (Outcome, error) {
	if executor.Store == nil {
		return Outcome{}, fmt.Errorf("recovery executor store is not configured")
	}
	decision = normalizeDecision(decision)

	switch decision.Mode {
	case DecisionModeIgnore:
		return Outcome{}, nil
	case DecisionModeIncidentOnly, DecisionModeApprovalRequired:
		incident, err := executor.findOrCreateIncident(ctx, decision.Observation, decision)
		if err != nil {
			return Outcome{}, err
		}
		return Outcome{Status: string(OutcomeStatusIncidentOnly), Incident: incident}, nil
	case DecisionModeEscalate:
		incident, err := executor.findOrCreateIncident(ctx, decision.Observation, decision)
		if err != nil {
			return Outcome{}, err
		}
		incident, err = executor.Store.UpdateIncidentStatus(ctx, sqlite.UpdateIncidentStatusParams{
			IncidentID:  incident.ID,
			Status:      string(OutcomeStatusEscalated),
			Reason:      decision.reasonOrSummary(),
			DetailsJSON: incidentDetailsJSON(decision.Observation, decision, string(OutcomeStatusEscalated), decision.reasonOrSummary()),
		})
		if err != nil {
			return Outcome{}, err
		}
		return Outcome{Status: string(OutcomeStatusEscalated), Escalated: true, Incident: incident}, nil
	}

	playbook, ok := executor.Playbooks[decision.Playbook]
	if !ok {
		return Outcome{}, ErrUnknownPlaybook
	}
	playbook = normalizePlaybook(playbook)
	if !playbook.allowsScope(decision.Observation.Scope) {
		return Outcome{}, ErrPlaybookScopeNotAllowed
	}

	now := time.Now().UTC()
	if executor.Now != nil {
		now = executor.Now().UTC()
	}

	incident, err := executor.findOrCreateIncident(ctx, decision.Observation, decision)
	if err != nil {
		return Outcome{}, err
	}
	if incident.Status == "escalated" {
		return Outcome{Status: string(OutcomeStatusEscalated), Escalated: true, Incident: incident}, nil
	}

	recoveries, err := executor.listRecoveriesByIncident(ctx, incident.ID)
	if err != nil {
		return Outcome{}, err
	}
	if len(recoveries) > 0 {
		latest := recoveries[len(recoveries)-1]
		if playbook.Cooldown > 0 && latest.StartedAt.After(now.Add(-playbook.Cooldown)) {
			return Outcome{
				Status:     string(OutcomeStatusSuppressed),
				Suppressed: true,
				Attempt:    len(recoveries),
				Incident:   incident,
				Recovery:   latest,
			}, nil
		}
	}

	attempt := len(recoveries) + 1
	recoveryDetails, err := marshalJSON(map[string]any{
		"fault_key":     decision.Observation.FaultKey,
		"subject_key":   decision.Observation.SubjectKey,
		"decision_mode": decision.Mode,
		"playbook":      playbook.Name,
		"action_name":   playbook.ActionName,
		"attempt":       attempt,
		"next_action":   decision.nextActionOrDefault("monitor recovery result"),
	})
	if err != nil {
		return Outcome{}, err
	}

	recoveryRecord, err := executor.Store.StartRecovery(ctx, sqlite.StartRecoveryParams{
		IncidentID:  &incident.ID,
		RunID:       incident.RunID,
		Status:      "running",
		Strategy:    playbook.Name,
		DetailsJSON: recoveryDetails,
	})
	if err != nil {
		return Outcome{}, err
	}

	actionResult := ActionResult{Status: "completed"}
	actionErr := error(nil)
	if playbook.Action != nil {
		actionResult, actionErr = playbook.Action(ctx, ActionContext{
			Observation: decision.Observation,
			Incident:    incident,
			Recovery:    recoveryRecord,
			Attempt:     attempt,
			Store:       executor.Store,
			Now:         now,
		})
	}
	if actionErr != nil {
		actionResult.Status = "failed"
		actionResult.Description = actionErr.Error()
	}
	if actionResult.Status == "" {
		actionResult.Status = string(ActionResultStatusCompleted)
	}

	if !isValidActionResultStatus(actionResult.Status) {
		rawStatus := actionResult.Status
		contractViolation := &runtimeevents.RecoveryActionContractViolation{
			Key:       runtimeevents.RecoveryActionContractViolationInvalidActionResultStatus,
			RawStatus: rawStatus,
		}
		description := actionResult.Description
		if description == "" {
			description = fmt.Sprintf("invalid recovery action result status: %s", rawStatus)
		}
		invalidDetails := actionResult.DetailsJSON
		if invalidDetails == "" {
			invalidDetails = invalidActionResultDetailsJSON(rawStatus, description)
		}
		if err := executor.Store.RecordRecoveryAction(ctx, sqlite.RecordRecoveryActionParams{
			RecoveryID:        recoveryRecord.ID,
			Playbook:          playbook.Name,
			FaultKey:          string(decision.Observation.FaultKey),
			ActionName:        playbook.ActionName,
			Attempt:           attempt,
			Result:            string(ActionResultStatusFailed),
			Description:       description,
			ContractViolation: contractViolation,
		}); err != nil {
			return Outcome{}, err
		}
		recoveryRecord, err = executor.Store.CompleteRecovery(ctx, sqlite.CompleteRecoveryParams{
			RecoveryID:  recoveryRecord.ID,
			Status:      string(OutcomeStatusFailed),
			DetailsJSON: invalidDetails,
		})
		if err != nil {
			return Outcome{}, err
		}
		incident, err = executor.Store.UpdateIncidentStatus(ctx, sqlite.UpdateIncidentStatusParams{
			IncidentID:  incident.ID,
			Status:      string(OutcomeStatusEscalated),
			Reason:      description,
			DetailsJSON: incidentDetailsJSON(decision.Observation, decision, string(OutcomeStatusEscalated), description),
		})
		if err != nil {
			return Outcome{}, err
		}
		return Outcome{
			Status:    string(OutcomeStatusEscalated),
			Escalated: true,
			Attempt:   attempt,
			Incident:  incident,
			Recovery:  recoveryRecord,
		}, fmt.Errorf("%w: %s", ErrInvalidActionResultStatus, rawStatus)
	}

	if err := executor.Store.RecordRecoveryAction(ctx, sqlite.RecordRecoveryActionParams{
		RecoveryID:  recoveryRecord.ID,
		Playbook:    playbook.Name,
		FaultKey:    string(decision.Observation.FaultKey),
		ActionName:  playbook.ActionName,
		Attempt:     attempt,
		Result:      actionResult.Status,
		Description: actionResult.Description,
	}); err != nil {
		return Outcome{}, err
	}

	switch actionResult.Status {
	case string(ActionResultStatusCompleted):
		recoveryRecord, err = executor.Store.CompleteRecovery(ctx, sqlite.CompleteRecoveryParams{
			RecoveryID:  recoveryRecord.ID,
			Status:      string(OutcomeStatusCompleted),
			DetailsJSON: actionResult.DetailsJSON,
		})
		if err != nil {
			return Outcome{}, err
		}
		incident, err = executor.Store.UpdateIncidentStatus(ctx, sqlite.UpdateIncidentStatusParams{
			IncidentID:  incident.ID,
			Status:      "resolved",
			Reason:      actionResult.Description,
			DetailsJSON: incidentDetailsJSON(decision.Observation, decision, "resolved", actionResult.Description),
		})
		if err != nil {
			return Outcome{}, err
		}
		return Outcome{
			Status:   string(OutcomeStatusCompleted),
			Attempt:  attempt,
			Incident: incident,
			Recovery: recoveryRecord,
		}, nil
	case string(ActionResultStatusEscalated):
		return executor.finishEscalatedRecovery(ctx, decision, incident, recoveryRecord, attempt, actionResult.Description, actionResult.DetailsJSON)
	default:
		if attempt >= playbook.MaxRetries {
			return executor.finishEscalatedRecovery(ctx, decision, incident, recoveryRecord, attempt, actionResult.Description, actionResult.DetailsJSON)
		}
		recoveryRecord, err = executor.Store.CompleteRecovery(ctx, sqlite.CompleteRecoveryParams{
			RecoveryID:  recoveryRecord.ID,
			Status:      string(OutcomeStatusFailed),
			DetailsJSON: actionResult.DetailsJSON,
		})
		if err != nil {
			return Outcome{}, err
		}
		return Outcome{
			Status:   string(OutcomeStatusFailed),
			Attempt:  attempt,
			Incident: incident,
			Recovery: recoveryRecord,
		}, nil
	}
}

func (executor Executor) finishEscalatedRecovery(ctx context.Context, decision Decision, incident sqlite.Incident, recoveryRecord sqlite.Recovery, attempt int, description string, detailsJSON string) (Outcome, error) {
	var err error
	recoveryRecord, err = executor.Store.CompleteRecovery(ctx, sqlite.CompleteRecoveryParams{
		RecoveryID:  recoveryRecord.ID,
		Status:      string(OutcomeStatusEscalated),
		DetailsJSON: detailsJSON,
	})
	if err != nil {
		return Outcome{}, err
	}
	incident, err = executor.Store.UpdateIncidentStatus(ctx, sqlite.UpdateIncidentStatusParams{
		IncidentID:  incident.ID,
		Status:      string(OutcomeStatusEscalated),
		Reason:      description,
		DetailsJSON: incidentDetailsJSON(decision.Observation, decision, string(OutcomeStatusEscalated), description),
	})
	if err != nil {
		return Outcome{}, err
	}
	return Outcome{
		Status:    string(OutcomeStatusEscalated),
		Escalated: true,
		Attempt:   attempt,
		Incident:  incident,
		Recovery:  recoveryRecord,
	}, nil
}

func (executor Executor) findOrCreateIncident(ctx context.Context, observation Observation, decision Decision) (sqlite.Incident, error) {
	incident, err := executor.findActiveIncident(ctx, observation)
	switch {
	case err == nil:
		return incident, nil
	case !errors.Is(err, sql.ErrNoRows):
		return sqlite.Incident{}, err
	}

	return executor.Store.OpenIncident(ctx, sqlite.OpenIncidentParams{
		RunID:       observation.RunID,
		Severity:    observation.SeverityOrDefault(),
		Status:      "open",
		Summary:     observation.Summary,
		DetailsJSON: incidentDetailsJSON(observation, decision, "open", decision.reasonOrSummary()),
	})
}

func (executor Executor) findActiveIncident(ctx context.Context, observation Observation) (sqlite.Incident, error) {
	rows, err := executor.Store.DB().QueryContext(ctx, `
		SELECT id, run_id, severity, status, summary, details_json, opened_at, updated_at
		FROM incidents
		WHERE status IN ('open', 'escalated')
		ORDER BY id DESC
	`)
	if err != nil {
		return sqlite.Incident{}, err
	}
	defer rows.Close()

	for rows.Next() {
		incident, err := scanIncidentRow(rows)
		if err != nil {
			return sqlite.Incident{}, err
		}
		if incident.RunID == nil && observation.RunID != nil {
			continue
		}
		if incident.RunID != nil && observation.RunID == nil {
			continue
		}
		if incident.RunID != nil && observation.RunID != nil && *incident.RunID != *observation.RunID {
			continue
		}
		faultKey, subjectKey := decodeIncidentDetails(incident.DetailsJSON)
		if faultKey == string(observation.FaultKey) && subjectKey == observation.SubjectKey {
			return incident, nil
		}
	}
	if err := rows.Err(); err != nil {
		return sqlite.Incident{}, err
	}
	return sqlite.Incident{}, sql.ErrNoRows
}

func (executor Executor) listRecoveriesByIncident(ctx context.Context, incidentID int64) ([]sqlite.Recovery, error) {
	rows, err := executor.Store.DB().QueryContext(ctx, `
		SELECT id, incident_id, run_id, status, strategy, details_json, started_at, finished_at, updated_at
		FROM recoveries
		WHERE incident_id = ?
		ORDER BY started_at ASC, id ASC
	`, incidentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var recoveries []sqlite.Recovery
	for rows.Next() {
		recoveryRecord, err := scanRecoveryRow(rows)
		if err != nil {
			return nil, err
		}
		recoveries = append(recoveries, recoveryRecord)
	}
	return recoveries, rows.Err()
}

func decodeIncidentDetails(detailsJSON string) (faultKey string, subjectKey string) {
	var payload struct {
		FaultKey   string `json:"fault_key"`
		SubjectKey string `json:"subject_key"`
	}
	if err := json.Unmarshal([]byte(detailsJSON), &payload); err != nil {
		return "", ""
	}
	return payload.FaultKey, payload.SubjectKey
}

func incidentDetailsJSON(observation Observation, decision Decision, status string, reason string) string {
	payload, err := marshalJSON(map[string]any{
		"fault_key":     observation.FaultKey,
		"subject_key":   observation.SubjectKey,
		"decision_mode": decision.Mode,
		"status":        status,
		"reason":        reason,
		"next_action":   decision.NextAction,
	})
	if err != nil {
		return "{}"
	}
	return payload
}

func invalidActionResultDetailsJSON(rawStatus string, description string) string {
	payload, err := marshalJSON(map[string]any{
		"contract_violation": map[string]any{
			"key":        runtimeevents.RecoveryActionContractViolationInvalidActionResultStatus,
			"raw_status": rawStatus,
		},
		"description": description,
	})
	if err != nil {
		return "{}"
	}
	return payload
}

func normalizeDecision(decision Decision) Decision {
	if decision.Mode == "" {
		if decision.Playbook != "" {
			decision.Mode = DecisionModePlaybook
		} else {
			decision.Mode = DecisionModeIgnore
		}
	}
	return decision
}

func isValidActionResultStatus(status string) bool {
	switch status {
	case string(ActionResultStatusCompleted), string(ActionResultStatusFailed), string(ActionResultStatusEscalated):
		return true
	default:
		return false
	}
}

func (decision Decision) reasonOrSummary() string {
	if decision.Reason != "" {
		return decision.Reason
	}
	if decision.Observation.Summary != "" {
		return decision.Observation.Summary
	}
	return string(decision.Observation.FaultKey)
}

func (decision Decision) nextActionOrDefault(fallback string) string {
	if decision.NextAction != "" {
		return decision.NextAction
	}
	return fallback
}

func marshalJSON(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func scanIncidentRow(row interface{ Scan(...any) error }) (sqlite.Incident, error) {
	var incident sqlite.Incident
	var runID sql.NullInt64
	var openedAt string
	var updatedAt string
	if err := row.Scan(
		&incident.ID,
		&runID,
		&incident.Severity,
		&incident.Status,
		&incident.Summary,
		&incident.DetailsJSON,
		&openedAt,
		&updatedAt,
	); err != nil {
		return sqlite.Incident{}, err
	}

	incident.RunID = nullableInt64Ptr(runID)
	var err error
	incident.OpenedAt, err = time.Parse(time.RFC3339Nano, openedAt)
	if err != nil {
		return sqlite.Incident{}, err
	}
	incident.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return sqlite.Incident{}, err
	}
	return incident, nil
}

func scanRecoveryRow(row interface{ Scan(...any) error }) (sqlite.Recovery, error) {
	var recoveryRecord sqlite.Recovery
	var incidentID sql.NullInt64
	var runID sql.NullInt64
	var startedAt string
	var finishedAt sql.NullString
	var updatedAt string
	if err := row.Scan(
		&recoveryRecord.ID,
		&incidentID,
		&runID,
		&recoveryRecord.Status,
		&recoveryRecord.Strategy,
		&recoveryRecord.DetailsJSON,
		&startedAt,
		&finishedAt,
		&updatedAt,
	); err != nil {
		return sqlite.Recovery{}, err
	}

	var err error
	recoveryRecord.IncidentID = nullableInt64Ptr(incidentID)
	recoveryRecord.RunID = nullableInt64Ptr(runID)
	recoveryRecord.StartedAt, err = time.Parse(time.RFC3339Nano, startedAt)
	if err != nil {
		return sqlite.Recovery{}, err
	}
	recoveryRecord.FinishedAt, err = parseNullableTime(finishedAt)
	if err != nil {
		return sqlite.Recovery{}, err
	}
	recoveryRecord.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return sqlite.Recovery{}, err
	}
	return recoveryRecord, nil
}

func nullableInt64Ptr(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	result := value.Int64
	return &result
}

func parseNullableTime(value sql.NullString) (*time.Time, error) {
	if !value.Valid || value.String == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value.String)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func (observation Observation) SeverityOrDefault() string {
	if observation.Severity == "" {
		return "warning"
	}
	return observation.Severity
}
