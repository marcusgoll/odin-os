package media

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"odin-os/internal/app/backup"
	approvals "odin-os/internal/core/approvals"
	coremedia "odin-os/internal/core/media"
	"odin-os/internal/runtime/checkpoints"
	healthsvc "odin-os/internal/runtime/health"
	"odin-os/internal/store/sqlite"
)

const defaultBackupFreshnessTTL = 24 * time.Hour

type MaintenanceService struct {
	Store       *sqlite.Store
	Config      *coremedia.Config
	RuntimeRoot string
	Now         func() time.Time
}

type PreflightRequest struct {
	TaskID *int64
	Action string
}

type PreflightResult struct {
	RequiresApproval bool
	ApprovalID       *int64
	EvidencePacketID *int64
	BlockedReason    string
}

type PostflightRequest struct {
	TaskID               *int64
	Action               string
	Checks               []healthsvc.Check
	KnownCriticalSignals []string
}

type PostflightResult struct {
	RollbackRecommended    bool
	RecommendationPacketID *int64
}

func (service MaintenanceService) Preflight(ctx context.Context, request PreflightRequest) (PreflightResult, error) {
	if service.Store == nil {
		return PreflightResult{}, fmt.Errorf("media maintenance store is required")
	}
	if service.Config == nil {
		return PreflightResult{}, fmt.Errorf("media maintenance config is required")
	}

	now := time.Now().UTC()
	if service.Now != nil {
		now = service.Now().UTC()
	}

	decision := approvals.Service{}.Evaluate(*service.Config, request.Action)
	result := PreflightResult{RequiresApproval: decision.RequiresApproval}

	if !decision.Allowed {
		result.BlockedReason = "action is forbidden by media policy"
		packetID, err := service.createMaintenancePacket(ctx, request.TaskID, "media maintenance blocked", result.BlockedReason, []string{"choose an approval-safe action"}, []string{"forbidden action"})
		if err != nil {
			return PreflightResult{}, err
		}
		result.EvidencePacketID = packetID
		return result, nil
	}

	if !decision.RequiresApproval {
		return result, nil
	}

	status, err := backup.Service{RuntimeRoot: service.RuntimeRoot}.VerificationStatus(defaultBackupFreshnessTTL, now)
	if err != nil {
		return PreflightResult{}, err
	}
	if !status.Present || !status.Fresh {
		result.BlockedReason = "backup freshness is stale or missing"
		packetID, err := service.createMaintenancePacket(ctx, request.TaskID, "media maintenance blocked", result.BlockedReason, []string{"run odin backup", "run odin verify-backup", "retry preflight"}, []string{"approval required"})
		if err != nil {
			return PreflightResult{}, err
		}
		result.EvidencePacketID = packetID
		return result, nil
	}

	if request.TaskID != nil {
		approvalID, err := service.requestApprovalIfNeeded(ctx, *request.TaskID)
		if err != nil {
			return PreflightResult{}, err
		}
		result.ApprovalID = approvalID
	}

	packetID, err := service.createMaintenancePacket(ctx, request.TaskID, "media maintenance awaiting approval", "awaiting operator approval", []string{"review preflight evidence", "approve or reject the requested action"}, []string{"approval required", "backup verified"})
	if err != nil {
		return PreflightResult{}, err
	}
	result.EvidencePacketID = packetID
	return result, nil
}

func (service MaintenanceService) Postflight(ctx context.Context, request PostflightRequest) (PostflightResult, error) {
	if service.Store == nil {
		return PostflightResult{}, fmt.Errorf("media maintenance store is required")
	}

	knownCritical := make(map[string]bool, len(request.KnownCriticalSignals))
	for _, signal := range request.KnownCriticalSignals {
		knownCritical[strings.TrimSpace(signal)] = true
	}

	var criticalChecks []healthsvc.Check
	for _, check := range request.Checks {
		if strings.HasPrefix(check.Name, "media.") && check.Status == healthsvc.StatusFailed && !knownCritical[check.Name] {
			criticalChecks = append(criticalChecks, check)
		}
	}
	if len(criticalChecks) == 0 {
		return PostflightResult{}, nil
	}

	packetID, err := service.createMaintenancePacket(ctx, request.TaskID, "rollback recommended after media postflight failure", fmt.Sprintf("critical media failure detected after %s", request.Action), []string{"review rollback plan", "resolve the critical media fault before retrying"}, []string{"approval required", "rollback recommended"})
	if err != nil {
		return PostflightResult{}, err
	}
	return PostflightResult{
		RollbackRecommended:    true,
		RecommendationPacketID: packetID,
	}, nil
}

func (service MaintenanceService) requestApprovalIfNeeded(ctx context.Context, taskID int64) (*int64, error) {
	row := service.Store.DB().QueryRowContext(ctx, `
		SELECT id
		FROM approvals
		WHERE task_id = ? AND status = 'pending'
		ORDER BY id ASC
		LIMIT 1
	`, taskID)

	var approvalID int64
	if err := row.Scan(&approvalID); err == nil {
		return &approvalID, nil
	} else if err != sql.ErrNoRows {
		return nil, err
	}

	approval, err := service.Store.RequestApproval(ctx, sqlite.RequestApprovalParams{
		TaskID:      taskID,
		Status:      "pending",
		RequestedBy: "media-supervisor",
	})
	if err != nil {
		return nil, err
	}
	return &approval.ID, nil
}

func (service MaintenanceService) createMaintenancePacket(ctx context.Context, taskID *int64, summary string, blockingReason string, nextSteps []string, constraints []string) (*int64, error) {
	if taskID == nil {
		return nil, nil
	}

	task, err := service.Store.GetTask(ctx, *taskID)
	if err != nil {
		return nil, err
	}

	payload, err := json.Marshal(checkpoints.TaskWakePacket{
		TaskID:         task.ID,
		TaskKey:        task.Key,
		Scope:          task.Scope,
		Objective:      "Review media maintenance change",
		Status:         "waiting",
		Trigger:        checkpoints.TriggerApprovalWait,
		BlockingReason: blockingReason,
		NextSteps:      nextSteps,
		Constraints:    constraints,
		Evidence: []checkpoints.Evidence{
			{Kind: "media_action", Summary: summary},
		},
	})
	if err != nil {
		return nil, err
	}

	packet, err := service.Store.CreateContextPacket(ctx, sqlite.CreateContextPacketParams{
		TaskID:        taskID,
		PacketKind:    "maintenance",
		PacketScope:   string(checkpoints.PacketScopeTaskWake),
		Trigger:       string(checkpoints.TriggerApprovalWait),
		CheckpointKey: fmt.Sprintf("media-maintenance-%d", task.ID),
		Status:        string(checkpoints.PacketStatusActive),
		Summary:       summary,
		PayloadJSON:   string(payload),
	})
	if err != nil {
		return nil, err
	}
	return &packet.ID, nil
}
