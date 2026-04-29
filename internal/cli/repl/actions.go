package repl

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"strconv"

	"odin-os/internal/store/sqlite"
)

func (shell *Shell) handleActions(ctx context.Context, args []string, output io.Writer) error {
	switch len(args) {
	case 0:
		return shell.handleActionsList(ctx, output)
	case 1:
		actionID, err := parseActionID(args[0])
		if err != nil {
			_, writeErr := fmt.Fprintln(output, "usage: /actions [action_id] [evidence]")
			return writeErr
		}
		return shell.handleActionDetail(ctx, actionID, output)
	case 2:
		actionID, err := parseActionID(args[0])
		if err != nil || args[1] != "evidence" {
			_, writeErr := fmt.Fprintln(output, "usage: /actions [action_id] [evidence]")
			return writeErr
		}
		return shell.handleActionEvidence(ctx, actionID, output)
	default:
		_, err := fmt.Fprintln(output, "usage: /actions [action_id] [evidence]")
		return err
	}
}

func (shell *Shell) handleActionsList(ctx context.Context, output io.Writer) error {
	actions, err := shell.env.Store.ListActions(ctx, sqlite.ListActionsParams{})
	if err != nil {
		return err
	}
	if len(actions) == 0 {
		_, err := fmt.Fprintln(output, "no actions")
		return err
	}
	for _, action := range actions {
		if _, err := fmt.Fprintf(
			output,
			"action_id=%d workflow=%s type=%s lifecycle=%s payload_hash=%s\n",
			action.ID,
			action.WorkflowKey,
			action.ActionType,
			action.LifecycleState,
			action.CurrentPayloadHash,
		); err != nil {
			return err
		}
	}
	return nil
}

func (shell *Shell) handleActionDetail(ctx context.Context, actionID int64, output io.Writer) error {
	action, payload, err := shell.env.Store.GetAction(ctx, actionID)
	if err != nil {
		if err == sql.ErrNoRows {
			_, writeErr := fmt.Fprintf(output, "action_id=%d not found\n", actionID)
			return writeErr
		}
		return err
	}

	if _, err := fmt.Fprintf(
		output,
		"action_id=%d workflow=%s workflow_run_id=%d type=%s lifecycle=%s payload_hash=%s\n",
		action.ID,
		action.WorkflowKey,
		action.WorkflowRunID,
		action.ActionType,
		action.LifecycleState,
		action.CurrentPayloadHash,
	); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(
		output,
		"payload_schema=%s version=%d submit_path=%s readback_path=%s proof_requirement=%s\n",
		payload.PayloadSchema,
		payload.PayloadSchemaVersion,
		payload.SubmitPath,
		payload.ReadbackPath,
		payload.ProofRequirement,
	); err != nil {
		return err
	}

	approvals, err := shell.actionApprovals(ctx, action.ID)
	if err != nil {
		return err
	}
	if len(approvals) == 0 {
		_, err := fmt.Fprintln(output, "approval=none")
		return err
	}
	for _, approval := range approvals {
		if _, err := fmt.Fprintf(
			output,
			"approval_id=%d status=%s action_id=%d payload_hash=%s\n",
			approval.ID,
			approval.Status,
			action.ID,
			approval.PayloadHash,
		); err != nil {
			return err
		}
	}
	return nil
}

func (shell *Shell) handleActionEvidence(ctx context.Context, actionID int64, output io.Writer) error {
	events, err := shell.env.Store.ListActionEvidence(ctx, actionID)
	if err != nil {
		return err
	}
	if len(events) == 0 {
		_, err := fmt.Fprintln(output, "no action evidence")
		return err
	}
	for _, event := range events {
		if _, err := fmt.Fprintf(
			output,
			"evidence_id=%d type=%s version=%d payload_hash=%s approval_id=%s run_id=%s source=%s occurred_at=%s\n",
			event.ID,
			event.EventType,
			event.EventVersion,
			stringValue(event.PayloadHash),
			int64Value(event.ApprovalID),
			int64Value(event.RunID),
			event.Source,
			event.OccurredAt.Format(timeFormatRFC3339),
		); err != nil {
			return err
		}
	}
	return nil
}

type actionApproval struct {
	ID          int64
	Status      string
	PayloadHash string
}

func (shell *Shell) actionApprovals(ctx context.Context, actionID int64) ([]actionApproval, error) {
	rows, err := shell.env.Store.DB().QueryContext(ctx, `
		SELECT id, status, payload_hash
		FROM approvals
		WHERE action_id = ?
		ORDER BY id ASC
	`, actionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var approvals []actionApproval
	for rows.Next() {
		var approval actionApproval
		var payloadHash sql.NullString
		if err := rows.Scan(&approval.ID, &approval.Status, &payloadHash); err != nil {
			return nil, err
		}
		approval.PayloadHash = payloadHash.String
		approvals = append(approvals, approval)
	}
	return approvals, rows.Err()
}

func parseActionID(raw string) (int64, error) {
	actionID, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || actionID <= 0 {
		return 0, fmt.Errorf("invalid action_id")
	}
	return actionID, nil
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func int64Value(value *int64) string {
	if value == nil {
		return ""
	}
	return strconv.FormatInt(*value, 10)
}

const timeFormatRFC3339 = "2006-01-02T15:04:05Z07:00"
