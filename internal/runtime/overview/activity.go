package overview

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	runtimeevents "odin-os/internal/runtime/events"
	"odin-os/internal/store/sqlite"
)

func BuildActivityEventSummaries(ctx context.Context, store *sqlite.Store, records []runtimeevents.Record, includePayload bool) ([]ActivityEventSummary, error) {
	taskCache := make(map[int64]sqlite.Task)
	taskMissing := make(map[int64]struct{})
	projectCache := make(map[int64]string)
	projectMissing := make(map[int64]struct{})

	summaries := make([]ActivityEventSummary, 0, len(records))
	for _, record := range records {
		payload := decodeEventPayloadMap(record.Payload)
		taskID := cloneInt64Ptr(record.TaskID)
		if taskID == nil {
			if id, ok := payloadInt64(payload, "task_id"); ok {
				taskID = &id
			}
		}
		runID := cloneInt64Ptr(record.RunID)
		if runID == nil {
			if id, ok := payloadInt64(payload, "run_id"); ok {
				runID = &id
			}
		}

		var workItemKey string
		projectID := cloneInt64Ptr(record.ProjectID)
		if taskID != nil {
			task, ok, err := cachedActivityTask(ctx, store, taskCache, taskMissing, *taskID)
			if err != nil {
				return nil, err
			}
			if ok {
				workItemKey = task.Key
				if projectID == nil {
					id := task.ProjectID
					projectID = &id
				}
			}
		}

		projectKey := ""
		if projectID != nil {
			key, ok, err := cachedActivityProjectKey(ctx, store, projectCache, projectMissing, *projectID)
			if err != nil {
				return nil, err
			}
			if ok {
				projectKey = key
			}
		}

		item := ActivityEventSummary{
			EventID:     record.ID,
			StreamType:  string(record.StreamType),
			StreamID:    record.StreamID,
			EventType:   string(record.Type),
			Scope:       record.Scope,
			ProjectID:   projectID,
			ProjectKey:  projectKey,
			TaskID:      taskID,
			WorkItemKey: workItemKey,
			RunID:       runID,
			ApprovalID:  activityApprovalID(record),
			OccurredAt:  record.OccurredAt.UTC().Format(time.RFC3339),
			Summary:     summarizeActivityEvent(record, payload, workItemKey, projectKey),
		}
		if includePayload && len(record.Payload) > 0 {
			item.Payload = append(json.RawMessage(nil), record.Payload...)
		}
		summaries = append(summaries, item)
	}
	return summaries, nil
}

func cachedActivityTask(ctx context.Context, store *sqlite.Store, cache map[int64]sqlite.Task, missing map[int64]struct{}, taskID int64) (sqlite.Task, bool, error) {
	if task, ok := cache[taskID]; ok {
		return task, true, nil
	}
	if _, ok := missing[taskID]; ok {
		return sqlite.Task{}, false, nil
	}
	task, err := store.GetTask(ctx, taskID)
	if err != nil {
		if err == sql.ErrNoRows {
			missing[taskID] = struct{}{}
			return sqlite.Task{}, false, nil
		}
		return sqlite.Task{}, false, err
	}
	cache[taskID] = task
	return task, true, nil
}

func cachedActivityProjectKey(ctx context.Context, store *sqlite.Store, cache map[int64]string, missing map[int64]struct{}, projectID int64) (string, bool, error) {
	if key, ok := cache[projectID]; ok {
		return key, true, nil
	}
	if _, ok := missing[projectID]; ok {
		return "", false, nil
	}
	project, err := store.GetProject(ctx, projectID)
	if err != nil {
		if err == sql.ErrNoRows {
			missing[projectID] = struct{}{}
			return "", false, nil
		}
		return "", false, err
	}
	cache[projectID] = project.Key
	return project.Key, true, nil
}

func activityApprovalID(record runtimeevents.Record) *int64 {
	if record.StreamType != runtimeevents.StreamApproval || record.StreamID <= 0 {
		return nil
	}
	id := record.StreamID
	return &id
}

func summarizeActivityEvent(record runtimeevents.Record, payload map[string]any, workItemKey, projectKey string) string {
	switch record.Type {
	case runtimeevents.EventTaskCreated:
		parts := []string{"created work item"}
		if workItemKey != "" {
			parts = append(parts, workItemKey)
		}
		appendPayloadPart(&parts, payload, "status", "status")
		appendPayloadPart(&parts, payload, "requested_by", "requested_by")
		appendPayloadPart(&parts, payload, "execution_intent", "intent")
		return strings.Join(parts, " ")
	case runtimeevents.EventTaskDispatchRequested:
		parts := []string{"dispatch requested"}
		appendPayloadPart(&parts, payload, "executor", "executor")
		appendPayloadPart(&parts, payload, "attempt", "attempt")
		appendPayloadPart(&parts, payload, "status", "status")
		return strings.Join(parts, " ")
	case runtimeevents.EventTaskStatusChanged:
		return transitionSummary("work item status", payload, "previous_status", "status")
	case runtimeevents.EventTaskQueueStateChanged:
		parts := []string{transitionSummary("queue state", payload, "previous_status", "status")}
		appendPayloadPart(&parts, payload, "blocked_reason", "blocked_reason")
		appendPayloadPart(&parts, payload, "retry_count", "retry_count")
		return strings.Join(parts, " ")
	case runtimeevents.EventRunStarted:
		parts := []string{"run started"}
		appendPayloadPart(&parts, payload, "executor", "executor")
		appendPayloadPart(&parts, payload, "attempt", "attempt")
		appendPayloadPart(&parts, payload, "status", "status")
		return strings.Join(parts, " ")
	case runtimeevents.EventRunStatusChanged:
		return transitionSummary("run status", payload, "previous_status", "status")
	case runtimeevents.EventRunFinished:
		parts := []string{"run finished"}
		appendPayloadPart(&parts, payload, "status", "status")
		appendPayloadPart(&parts, payload, "terminal_reason", "terminal_reason")
		appendPayloadPart(&parts, payload, "summary", "summary")
		return strings.Join(parts, " ")
	case runtimeevents.EventApprovalRequested:
		parts := []string{"approval requested"}
		appendPayloadPart(&parts, payload, "status", "status")
		appendPayloadPart(&parts, payload, "requested_by", "requested_by")
		return strings.Join(parts, " ")
	case runtimeevents.EventApprovalResolved:
		parts := []string{"approval resolved"}
		appendPayloadPart(&parts, payload, "status", "status")
		appendPayloadPart(&parts, payload, "decision_by", "decision_by")
		appendPayloadPart(&parts, payload, "reason", "reason")
		return strings.Join(parts, " ")
	case runtimeevents.EventContextPacketCreated:
		parts := []string{"context packet created"}
		appendPayloadPart(&parts, payload, "packet_kind", "kind")
		appendPayloadPart(&parts, payload, "trigger", "trigger")
		appendPayloadPart(&parts, payload, "status", "status")
		appendPayloadPart(&parts, payload, "summary", "summary")
		return strings.Join(parts, " ")
	case runtimeevents.EventIntakeItemCreated, runtimeevents.EventIntakeProcessed, runtimeevents.EventIntakeReviewAccepted:
		parts := []string{strings.TrimPrefix(string(record.Type), "intake.")}
		appendPayloadPart(&parts, payload, "status", "status")
		appendPayloadPart(&parts, payload, "dedupe_key", "dedup_key")
		appendPayloadPart(&parts, payload, "requested_by", "requested_by")
		return strings.Join(parts, " ")
	case runtimeevents.EventAutomationTriggerCreated, runtimeevents.EventAutomationTriggerEvaluated, runtimeevents.EventAutomationTriggerMaterialized:
		parts := []string{strings.TrimPrefix(string(record.Type), "automation_trigger.")}
		appendPayloadPart(&parts, payload, "status", "status")
		appendPayloadPart(&parts, payload, "title", "title")
		return strings.Join(parts, " ")
	default:
		parts := []string{fmt.Sprintf("event %s", record.Type)}
		if workItemKey != "" {
			parts = append(parts, "work_item="+workItemKey)
		}
		if projectKey != "" {
			parts = append(parts, "project="+projectKey)
		}
		return strings.Join(parts, " ")
	}
}

func transitionSummary(label string, payload map[string]any, previousKey, statusKey string) string {
	previous := payloadString(payload, previousKey)
	status := payloadString(payload, statusKey)
	if previous != "" && status != "" {
		return fmt.Sprintf("%s %s -> %s", label, previous, status)
	}
	if status != "" {
		return fmt.Sprintf("%s status=%s", label, status)
	}
	return label
}

func appendPayloadPart(parts *[]string, payload map[string]any, key, label string) {
	value := payloadString(payload, key)
	if value == "" {
		return
	}
	*parts = append(*parts, fmt.Sprintf("%s=%s", label, value))
}

func decodeEventPayloadMap(raw json.RawMessage) map[string]any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return map[string]any{}
	}
	return payload
}

func payloadString(payload map[string]any, key string) string {
	value, ok := payload[key]
	if !ok || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case float64:
		return formatPayloadFloat(typed)
	case bool:
		if typed {
			return "true"
		}
		return "false"
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}

func payloadInt64(payload map[string]any, key string) (int64, bool) {
	value, ok := payload[key]
	if !ok || value == nil {
		return 0, false
	}
	switch typed := value.(type) {
	case float64:
		return int64(typed), typed > 0
	case int64:
		return typed, typed > 0
	case int:
		return int64(typed), typed > 0
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return 0, false
		}
		var parsed int64
		if _, err := fmt.Sscan(trimmed, &parsed); err != nil || parsed <= 0 {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}

func formatPayloadFloat(value float64) string {
	if value == float64(int64(value)) {
		return fmt.Sprintf("%d", int64(value))
	}
	return fmt.Sprintf("%g", value)
}

func cloneInt64Ptr(value *int64) *int64 {
	if value == nil {
		return nil
	}
	clone := *value
	return &clone
}
