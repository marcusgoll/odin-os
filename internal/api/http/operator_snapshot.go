package httpapi

import (
	"context"
	"fmt"
	"strings"
	"time"

	"odin-os/internal/cli/overview"
	"odin-os/internal/cli/scope"
)

type operatorSnapshot struct {
	GeneratedAt    string                 `json:"generated_at"`
	ActionRequired []operatorSnapshotRow  `json:"action_required"`
	OdinHealth     operatorSnapshotHealth `json:"odin_health"`
	LiveExecution  []operatorSnapshotRow  `json:"live_execution"`
	Activity       []operatorSnapshotRow  `json:"activity"`
	Browser        []operatorSnapshotRow  `json:"browser"`
}

type operatorSnapshotRow struct {
	ID       string         `json:"id"`
	Label    string         `json:"label"`
	Summary  string         `json:"summary"`
	Severity string         `json:"severity"`
	Details  map[string]any `json:"details"`
	Command  string         `json:"command,omitempty"`
	DeepLink string         `json:"deep_link,omitempty"`
}

type operatorSnapshotHealth struct {
	Status  string         `json:"status"`
	Ready   bool           `json:"ready"`
	Summary string         `json:"summary"`
	Details map[string]any `json:"details"`
	Command string         `json:"command,omitempty"`
}

func buildOperatorSnapshot(ctx context.Context, deps Dependencies, now func() time.Time) (operatorSnapshot, error) {
	if deps.Store == nil || deps.ReadModels == nil {
		return operatorSnapshot{}, fmt.Errorf("runtime store and read models are required")
	}

	status, err := buildStatusPayload(ctx, deps, now)
	if err != nil {
		return operatorSnapshot{}, err
	}
	readiness := "not_ready"
	if status.Ready {
		readiness = "ready"
	}
	overviewView, err := overview.Service{
		Store:            deps.Store,
		Registry:         deps.Registry,
		RegistrySnapshot: deps.RegistrySnapshot,
		Now:              now,
		ReadinessStatus:  readiness,
		HealthStatus:     status.HealthStatus,
	}.Build(ctx, scope.Resolution{Kind: scope.ScopeGlobal})
	if err != nil {
		return operatorSnapshot{}, err
	}
	reviewItems, err := mobileReviewQueue(ctx, deps)
	if err != nil {
		return operatorSnapshot{}, err
	}

	snapshot := operatorSnapshot{
		GeneratedAt:    status.GeneratedAt,
		ActionRequired: operatorSnapshotActionRows(reviewItems, overviewView),
		OdinHealth:     operatorSnapshotHealthRow(status, overviewView),
		LiveExecution:  operatorSnapshotLiveRows(overviewView),
		Activity:       operatorSnapshotActivityRows(overviewView),
		Browser:        operatorSnapshotBrowserRows(reviewItems),
	}
	return snapshot, nil
}

func operatorSnapshotHealthRow(status dashboardStatus, view overview.View) operatorSnapshotHealth {
	summary := fmt.Sprintf("runtime %s, %d active run(s), %d action-required item(s)", status.Runtime.Status, status.Counts.ActiveRunAttempts, status.Counts.ActionRequiredItems)
	return operatorSnapshotHealth{
		Status:  status.HealthStatus,
		Ready:   status.Ready,
		Summary: summary,
		Command: "odin healthcheck",
		Details: map[string]any{
			"readiness":       view.Readiness,
			"runtime":         status.Runtime,
			"counts":          status.Counts,
			"worker_dispatch": status.WorkerDispatch,
			"tmux":            status.Tmux,
			"freshness":       view.Observability.Freshness,
		},
	}
}

func operatorSnapshotActionRows(items []mobileReviewItem, view overview.View) []operatorSnapshotRow {
	rows := make([]operatorSnapshotRow, 0, len(items)+len(view.Observability.BlockedWork)+len(view.Observability.RecoveryGuidance))
	for _, item := range items {
		command := "odin review list"
		if strings.HasPrefix(item.QueueID, "approval:") {
			command = fmt.Sprintf("odin approvals show %d", item.ObjectID)
		}
		rows = append(rows, operatorSnapshotRow{
			ID:       item.QueueID,
			Label:    operatorSnapshotLabel(item.SourceType, item.Title),
			Summary:  operatorSnapshotReviewSummary(item),
			Severity: operatorSnapshotReviewSeverity(item),
			Command:  command,
			DeepLink: item.DeepLink,
			Details: map[string]any{
				"source_type":           item.SourceType,
				"source":                item.Source,
				"object_id":             item.ObjectID,
				"object_key":            item.ObjectKey,
				"title":                 item.Title,
				"status":                item.Status,
				"reason":                item.Reason,
				"project_key":           item.ProjectKey,
				"allowed_actions":       item.AllowedActions,
				"browser_event":         item.BrowserEvent,
				"real_browser_evidence": item.RealBrowserEvidence,
				"notification":          item.Notification,
			},
		})
	}
	for _, blocked := range view.Observability.BlockedWork {
		rows = append(rows, operatorSnapshotRow{
			ID:       fmt.Sprintf("blocked-work:%d", blocked.TaskID),
			Label:    "Blocked work",
			Summary:  fmt.Sprintf("%s is blocked: %s", blocked.WorkItemKey, firstNonEmpty(blocked.Reason, "blocked")),
			Severity: "warning",
			Command:  fmt.Sprintf("odin logs trail --task %d", blocked.TaskID),
			DeepLink: fmt.Sprintf("/work-items/%d", blocked.TaskID),
			Details: map[string]any{
				"source_type":    "blocked_work",
				"task_id":        blocked.TaskID,
				"work_item_key":  blocked.WorkItemKey,
				"project_key":    blocked.ProjectKey,
				"workspace_key":  blocked.WorkspaceKey,
				"initiative_key": blocked.InitiativeKey,
				"companion_key":  blocked.CompanionKey,
				"work_kind":      blocked.WorkKind,
				"source":         blocked.Source,
				"reason":         blocked.Reason,
			},
		})
	}
	for _, guidance := range view.Observability.RecoveryGuidance {
		rows = append(rows, operatorSnapshotRow{
			ID:       fmt.Sprintf("recovery:%d", guidance.TaskID),
			Label:    "Recovery guidance",
			Summary:  fmt.Sprintf("%s recovery: %s", guidance.WorkItemKey, guidance.RecoveryRecommendation),
			Severity: operatorSnapshotRecoverySeverity(guidance),
			Command:  fmt.Sprintf("odin logs trail --task %d", guidance.TaskID),
			DeepLink: fmt.Sprintf("/work-items/%d", guidance.TaskID),
			Details: map[string]any{
				"source_type":             "recovery_guidance",
				"task_id":                 guidance.TaskID,
				"work_item_key":           guidance.WorkItemKey,
				"project_key":             guidance.ProjectKey,
				"initiative_key":          guidance.InitiativeKey,
				"companion_key":           guidance.CompanionKey,
				"work_kind":               guidance.WorkKind,
				"source":                  guidance.Source,
				"status":                  guidance.Status,
				"decision":                guidance.Decision,
				"retry_eligible":          guidance.RetryEligible,
				"retry_count":             guidance.RetryCount,
				"max_attempts":            guidance.MaxAttempts,
				"last_error":              guidance.LastError,
				"recovery_recommendation": guidance.RecoveryRecommendation,
			},
		})
	}
	return rows
}

func operatorSnapshotLiveRows(view overview.View) []operatorSnapshotRow {
	rows := make([]operatorSnapshotRow, 0, len(view.Observability.ActiveRuns))
	for _, run := range view.Observability.ActiveRuns {
		rows = append(rows, operatorSnapshotRow{
			ID:       fmt.Sprintf("run:%d", run.RunID),
			Label:    fmt.Sprintf("Run %d", run.RunID),
			Summary:  fmt.Sprintf("%s attempt %d is %s on %s", run.WorkItemKey, run.Attempt, run.Status, run.Executor),
			Severity: operatorSnapshotRunSeverity(run.Status),
			Command:  fmt.Sprintf("odin runs show %d", run.RunID),
			DeepLink: fmt.Sprintf("/runs/%d", run.RunID),
			Details: map[string]any{
				"run_id":         run.RunID,
				"task_id":        run.TaskID,
				"work_item_key":  run.WorkItemKey,
				"project_key":    run.ProjectKey,
				"initiative_key": run.InitiativeKey,
				"companion_key":  run.CompanionKey,
				"executor":       run.Executor,
				"status":         run.Status,
				"attempt":        run.Attempt,
				"started_at":     run.StartedAt,
			},
		})
	}
	return rows
}

func operatorSnapshotActivityRows(view overview.View) []operatorSnapshotRow {
	rows := make([]operatorSnapshotRow, 0, len(view.Observability.ActivityLog))
	for _, event := range view.Observability.ActivityLog {
		rows = append(rows, operatorSnapshotRow{
			ID:       fmt.Sprintf("event:%d", event.EventID),
			Label:    event.EventType,
			Summary:  event.Summary,
			Severity: "info",
			Command:  fmt.Sprintf("odin logs show %d", event.EventID),
			DeepLink: fmt.Sprintf("/logs/%d", event.EventID),
			Details: map[string]any{
				"event_id":      event.EventID,
				"event_type":    event.EventType,
				"stream_type":   event.StreamType,
				"stream_id":     event.StreamID,
				"scope":         event.Scope,
				"project_key":   event.ProjectKey,
				"work_item_key": event.WorkItemKey,
				"task_id":       event.TaskID,
				"run_id":        event.RunID,
				"approval_id":   event.ApprovalID,
				"occurred_at":   event.OccurredAt,
			},
		})
	}
	return rows
}

func operatorSnapshotBrowserRows(reviewItems []mobileReviewItem) []operatorSnapshotRow {
	rows := make([]operatorSnapshotRow, 0)
	for _, item := range reviewItems {
		if strings.TrimSpace(item.BrowserEvent) == "" {
			continue
		}
		rows = append(rows, operatorSnapshotRow{
			ID:       "browser:" + item.QueueID,
			Label:    operatorSnapshotLabel(item.BrowserEvent, item.Title),
			Summary:  operatorSnapshotReviewSummary(item),
			Severity: operatorSnapshotReviewSeverity(item),
			Command:  "odin review list",
			DeepLink: item.DeepLink,
			Details: map[string]any{
				"queue_id":              item.QueueID,
				"browser_event":         item.BrowserEvent,
				"source_type":           item.SourceType,
				"object_id":             item.ObjectID,
				"object_key":            item.ObjectKey,
				"status":                item.Status,
				"allowed_actions":       item.AllowedActions,
				"real_browser_evidence": item.RealBrowserEvidence,
				"notification":          item.Notification,
			},
		})
	}
	return rows
}

func operatorSnapshotReviewSummary(item mobileReviewItem) string {
	parts := []string{item.Status}
	if item.Reason != "" {
		parts = append(parts, item.Reason)
	}
	if item.ProjectKey != "" {
		parts = append(parts, "project="+item.ProjectKey)
	}
	return fmt.Sprintf("%s: %s", item.Title, strings.Join(parts, " "))
}

func operatorSnapshotReviewSeverity(item mobileReviewItem) string {
	switch item.SourceType {
	case "approval", "browser_attended_login":
		return "warning"
	case "failed_work", "browser_run_failed":
		return "critical"
	default:
		if strings.Contains(strings.ToLower(item.Status), "required") {
			return "warning"
		}
		return "info"
	}
}

func operatorSnapshotRunSeverity(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "failed":
		return "critical"
	case "blocked", "waiting":
		return "warning"
	default:
		return "info"
	}
}

func operatorSnapshotRecoverySeverity(guidance overview.RetryRecoveryGuidanceSummary) string {
	if guidance.RetryEligible {
		return "warning"
	}
	return "critical"
}

func operatorSnapshotLabel(kind string, fallback string) string {
	label := strings.ReplaceAll(strings.TrimSpace(kind), "_", " ")
	if label == "" {
		label = strings.TrimSpace(fallback)
	}
	if label == "" {
		return "Operator row"
	}
	return strings.ToUpper(label[:1]) + label[1:]
}
