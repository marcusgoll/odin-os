package socialcopilot

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"odin-os/internal/core/projects"
	"odin-os/internal/store/sqlite"
)

const (
	defaultWorkflowKey = "marcus-social-growth-workflow"
	ownerProjectKey    = "odin-core"

	jobPacketKind  = "social_copilot_job_metadata"
	jobPacketScope = "workflow_job_metadata"
)

type Service struct {
	Store    *sqlite.Store
	Registry projects.Registry
	Now      func() time.Time
}

type EnsureJobParams struct {
	WorkflowKey string
	WatchScope  WatchScopeInput
	Cadence     time.Duration
}

type JobStatus struct {
	Task         sqlite.Task
	WatchScope   WatchScope
	TargetStates map[string]TargetState
	LastWakeAt   *time.Time
	NextWakeAt   *time.Time
	Due          bool
}

type WakeParams struct {
	WorkflowKey  string
	Trigger      string
	Reason       string
	Observations []Observation
}

type Observation struct {
	StableTargetKey       string
	Fingerprint           string
	RecommendedMemoryType string
	Summary               string
	Fields                map[string]string
	CooldownUntil         time.Time
}

type WakeResult struct {
	Run                sqlite.Run
	WatchScope         WatchScope
	TargetStates       map[string]TargetState
	AccountActions     string
	CreatedMemoryIDs   []int64
	CreatedMemoryCount int
	SuppressedTargets  []string
}

type TargetState struct {
	StableKey                  string `json:"stable_key"`
	LastCheckedAt              string `json:"last_checked_at,omitempty"`
	LastObservationFingerprint string `json:"last_observation_fingerprint,omitempty"`
	NextEligibleAt             string `json:"next_eligible_at,omitempty"`
	PendingMemoryID            int64  `json:"pending_memory_id,omitempty"`
}

type jobMetadata struct {
	WorkflowKey     string                 `json:"workflow_key"`
	WatchScope      WatchScope             `json:"watch_scope"`
	TargetStates    map[string]TargetState `json:"target_states,omitempty"`
	AccountActions  string                 `json:"account_actions"`
	CadenceSeconds  int64                  `json:"cadence_seconds,omitempty"`
	UpdatedAt       string                 `json:"updated_at,omitempty"`
	LastWakeAt      string                 `json:"last_wake_at,omitempty"`
	NextWakeAt      string                 `json:"next_wake_at,omitempty"`
	Trigger         string                 `json:"trigger,omitempty"`
	PreviousTargets []string               `json:"previous_targets,omitempty"`
}

type memoryDetails struct {
	Source              string            `json:"source"`
	SelectedWorkflowKey string            `json:"selected_workflow_key,omitempty"`
	Scope               string            `json:"scope"`
	ScopeKey            string            `json:"scope_key"`
	Fields              map[string]string `json:"fields,omitempty"`
}

func (service Service) EnsurePollingJob(ctx context.Context, params EnsureJobParams) (JobStatus, error) {
	if service.Store == nil {
		return JobStatus{}, fmt.Errorf("social copilot store is required")
	}

	workflowKey := normalizeWorkflowKey(params.WorkflowKey)
	watchScope, err := NormalizeWatchScope(params.WatchScope)
	if err != nil {
		return JobStatus{}, err
	}

	project, err := service.ensureOwnerProject(ctx)
	if err != nil {
		return JobStatus{}, err
	}

	taskKey := pollingJobTaskKey(workflowKey)
	task, err := service.Store.GetTaskByProjectAndKey(ctx, project.ID, taskKey)
	switch err {
	case nil:
	case sql.ErrNoRows:
		task, err = service.Store.CreateTask(ctx, sqlite.CreateTaskParams{
			ProjectID:   project.ID,
			Key:         taskKey,
			Title:       "Marcus Social Copilot polling loop",
			Status:      "scheduled",
			Scope:       "workflow",
			RequestedBy: "workflow:" + workflowKey,
		})
		if err != nil {
			return JobStatus{}, err
		}
	default:
		return JobStatus{}, err
	}

	status := JobStatus{
		Task:         task,
		WatchScope:   watchScope,
		TargetStates: emptyTargetStates(watchScope),
	}
	return status, nil
}

func (service Service) ReplaceWatchScope(ctx context.Context, workflowKey string, input WatchScopeInput) (JobStatus, error) {
	workflowKey = normalizeWorkflowKey(workflowKey)
	status, err := service.EnsurePollingJob(ctx, EnsureJobParams{
		WorkflowKey: workflowKey,
		WatchScope:  input,
	})
	if err != nil {
		return JobStatus{}, err
	}

	previous, _ := service.latestJobMetadata(ctx, status.Task.ID)
	status.TargetStates = reconcileTargetStates(status.WatchScope, previous.TargetStates)

	metadata := jobMetadata{
		WorkflowKey:     workflowKey,
		WatchScope:      status.WatchScope,
		TargetStates:    status.TargetStates,
		AccountActions:  "none",
		UpdatedAt:       service.now().Format(time.RFC3339),
		Trigger:         "watch_scope_replace",
		PreviousTargets: watchTargetStableKeys(previous.WatchScope),
	}
	payload, err := json.Marshal(metadata)
	if err != nil {
		return JobStatus{}, err
	}

	taskID := status.Task.ID
	if _, err := service.Store.CreateContextPacket(ctx, sqlite.CreateContextPacketParams{
		TaskID:        &taskID,
		PacketKind:    jobPacketKind,
		PacketScope:   jobPacketScope,
		Trigger:       "watch_scope_replace",
		CheckpointKey: checkpointKey(workflowKey),
		Status:        "active",
		Summary:       "Social Copilot watch scope replaced",
		PayloadJSON:   string(payload),
	}); err != nil {
		return JobStatus{}, err
	}

	return status, nil
}

func (service Service) Status(ctx context.Context, workflowKey string) (JobStatus, error) {
	if service.Store == nil {
		return JobStatus{}, fmt.Errorf("social copilot store is required")
	}

	workflowKey = normalizeWorkflowKey(workflowKey)
	project, err := service.ensureOwnerProject(ctx)
	if err != nil {
		return JobStatus{}, err
	}
	task, err := service.Store.GetTaskByProjectAndKey(ctx, project.ID, pollingJobTaskKey(workflowKey))
	if err != nil {
		return JobStatus{}, err
	}

	metadata, err := service.latestJobMetadata(ctx, task.ID)
	if err != nil && err != sql.ErrNoRows {
		return JobStatus{}, err
	}

	return JobStatus{
		Task:         task,
		WatchScope:   metadata.WatchScope,
		TargetStates: metadata.TargetStates,
	}, nil
}

func (service Service) Wake(ctx context.Context, params WakeParams) (WakeResult, error) {
	workflowKey := normalizeWorkflowKey(params.WorkflowKey)
	status, err := service.Status(ctx, workflowKey)
	if err != nil {
		return WakeResult{}, err
	}

	attempt, err := service.nextRunAttempt(ctx, status.Task.ID)
	if err != nil {
		return WakeResult{}, err
	}
	run, err := service.Store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:   status.Task.ID,
		Executor: "social_copilot",
		Attempt:  attempt,
		Status:   "running",
	})
	if err != nil {
		return WakeResult{}, err
	}

	trigger := strings.TrimSpace(params.Trigger)
	if trigger == "" {
		trigger = "manual"
	}
	now := service.now()
	targetStates := cloneTargetStates(status.TargetStates)
	if targetStates == nil {
		targetStates = map[string]TargetState{}
	}
	allowedTargets := watchTargetSet(status.WatchScope)
	var createdMemoryIDs []int64
	var suppressedTargets []string
	for _, observation := range params.Observations {
		created, suppressed, updatedState, err := service.processObservation(ctx, workflowKey, status.Task.ID, run.ID, now, targetStates, allowedTargets, observation)
		if err != nil {
			return WakeResult{}, err
		}
		if created != 0 {
			createdMemoryIDs = append(createdMemoryIDs, created)
		}
		if suppressed {
			suppressedTargets = append(suppressedTargets, updatedState.StableKey)
		}
		targetStates[updatedState.StableKey] = updatedState
	}

	metadata := jobMetadata{
		WorkflowKey:    workflowKey,
		WatchScope:     status.WatchScope,
		TargetStates:   targetStates,
		AccountActions: "none",
		UpdatedAt:      now.Format(time.RFC3339),
		LastWakeAt:     now.Format(time.RFC3339),
		Trigger:        trigger + "_wake",
	}
	if metadata.TargetStates == nil {
		metadata.TargetStates = map[string]TargetState{}
	}
	payload, err := json.Marshal(metadata)
	if err != nil {
		return WakeResult{}, err
	}

	taskID := status.Task.ID
	if _, err := service.Store.CreateContextPacket(ctx, sqlite.CreateContextPacketParams{
		TaskID:        &taskID,
		RunID:         &run.ID,
		PacketKind:    jobPacketKind,
		PacketScope:   jobPacketScope,
		Trigger:       trigger + "_wake",
		CheckpointKey: checkpointKey(workflowKey),
		Status:        "active",
		Summary:       "Social Copilot wake completed without account actions",
		PayloadJSON:   string(payload),
	}); err != nil {
		return WakeResult{}, err
	}

	finished, err := service.Store.FinishRun(ctx, sqlite.FinishRunParams{
		RunID:   run.ID,
		Status:  "completed",
		Summary: "Social Copilot wake completed; account_actions=none",
	})
	if err != nil {
		return WakeResult{}, err
	}

	return WakeResult{
		Run:                finished,
		WatchScope:         status.WatchScope,
		TargetStates:       targetStates,
		AccountActions:     "none",
		CreatedMemoryIDs:   createdMemoryIDs,
		CreatedMemoryCount: len(createdMemoryIDs),
		SuppressedTargets:  suppressedTargets,
	}, nil
}

func (service Service) processObservation(ctx context.Context, workflowKey string, taskID int64, runID int64, now time.Time, states map[string]TargetState, allowedTargets map[string]struct{}, observation Observation) (int64, bool, TargetState, error) {
	stableKey := strings.TrimSpace(observation.StableTargetKey)
	if stableKey == "" {
		return 0, false, TargetState{}, fmt.Errorf("observation stable target key is required")
	}
	if _, ok := allowedTargets[stableKey]; !ok {
		return 0, false, TargetState{}, fmt.Errorf("observation target %q is not in the workflow watch scope", stableKey)
	}

	memoryType, err := normalizeObservationMemoryType(observation.RecommendedMemoryType)
	if err != nil {
		return 0, false, TargetState{}, err
	}

	state := states[stableKey]
	state.StableKey = stableKey
	fingerprint := strings.TrimSpace(observation.Fingerprint)
	if fingerprint != "" && state.LastObservationFingerprint == fingerprint && isFutureTimestamp(state.NextEligibleAt, now) {
		state.LastCheckedAt = now.Format(time.RFC3339)
		return 0, true, state, nil
	}

	memoryID, created, err := service.ensurePendingObservationMemory(ctx, workflowKey, taskID, runID, state.PendingMemoryID, memoryType, stableKey, fingerprint, observation)
	if err != nil {
		return 0, false, TargetState{}, err
	}

	state.LastCheckedAt = now.Format(time.RFC3339)
	state.LastObservationFingerprint = fingerprint
	state.NextEligibleAt = nextEligibleAt(observation.CooldownUntil, now).Format(time.RFC3339)
	state.PendingMemoryID = memoryID
	if created {
		return memoryID, false, state, nil
	}
	return 0, false, state, nil
}

func (service Service) ensurePendingObservationMemory(ctx context.Context, workflowKey string, taskID int64, runID int64, hintedMemoryID int64, memoryType string, stableKey string, fingerprint string, observation Observation) (int64, bool, error) {
	summaries, err := service.Store.ListMemorySummaries(ctx, sqlite.ListMemorySummariesParams{
		Scope:      "workflow",
		ScopeKey:   workflowKey,
		MemoryType: memoryType,
	})
	if err != nil {
		return 0, false, err
	}

	if hintedMemoryID != 0 {
		for _, summary := range summaries {
			details := parseSocialMemoryDetails(summary.DetailsJSON)
			if summary.ID == hintedMemoryID && isPendingObservationMemory(summary, details, memoryType, stableKey, fingerprint) {
				return summary.ID, false, nil
			}
		}
	}

	for _, summary := range summaries {
		details := parseSocialMemoryDetails(summary.DetailsJSON)
		if isPendingObservationMemory(summary, details, memoryType, stableKey, fingerprint) {
			return summary.ID, false, nil
		}
	}

	detailsJSON, err := marshalObservationMemoryDetails(workflowKey, memoryType, stableKey, fingerprint, observation.Fields)
	if err != nil {
		return 0, false, err
	}
	taskIDCopy := taskID
	runIDCopy := runID
	recorded, err := service.Store.RecordMemorySummary(ctx, sqlite.RecordMemorySummaryParams{
		TaskID:      &taskIDCopy,
		RunID:       &runIDCopy,
		Scope:       "workflow",
		ScopeKey:    workflowKey,
		MemoryType:  memoryType,
		Summary:     strings.TrimSpace(observation.Summary),
		DetailsJSON: detailsJSON,
	})
	if err != nil {
		return 0, false, err
	}
	return recorded.ID, true, nil
}

func normalizeObservationMemoryType(memoryType string) (string, error) {
	switch strings.TrimSpace(memoryType) {
	case "social_draft", "social_research":
		return strings.TrimSpace(memoryType), nil
	default:
		return "", fmt.Errorf("observation recommended memory type %q is not allowed", strings.TrimSpace(memoryType))
	}
}

func watchTargetSet(scope WatchScope) map[string]struct{} {
	targets := make(map[string]struct{}, len(scope.Targets))
	for _, target := range scope.Targets {
		if strings.TrimSpace(target.StableKey) == "" {
			continue
		}
		targets[target.StableKey] = struct{}{}
	}
	return targets
}

func cloneTargetStates(states map[string]TargetState) map[string]TargetState {
	if states == nil {
		return nil
	}
	clone := make(map[string]TargetState, len(states))
	for key, state := range states {
		clone[key] = state
	}
	return clone
}

func nextEligibleAt(cooldownUntil time.Time, now time.Time) time.Time {
	if cooldownUntil.IsZero() {
		return now.Add(30 * time.Minute).UTC()
	}
	return cooldownUntil.UTC()
}

func isFutureTimestamp(raw string, now time.Time) bool {
	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	return parsed.After(now)
}

func marshalObservationMemoryDetails(workflowKey string, memoryType string, stableKey string, fingerprint string, fields map[string]string) (string, error) {
	nextFields := make(map[string]string, len(fields)+3)
	for key, value := range fields {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		nextFields[key] = value
	}
	switch memoryType {
	case "social_draft":
		nextFields["approval"] = "pending"
	case "social_research":
		nextFields["status"] = "pending"
	}
	nextFields["watched_target_key"] = stableKey
	if strings.TrimSpace(fingerprint) != "" {
		nextFields["observation_fingerprint"] = strings.TrimSpace(fingerprint)
	}

	payload := memoryDetails{
		Source:              "social_copilot",
		SelectedWorkflowKey: workflowKey,
		Scope:               "workflow",
		ScopeKey:            workflowKey,
		Fields:              nextFields,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func parseSocialMemoryDetails(detailsJSON string) memoryDetails {
	var payload memoryDetails
	if strings.TrimSpace(detailsJSON) == "" {
		return payload
	}
	if err := json.Unmarshal([]byte(detailsJSON), &payload); err == nil {
		if len(payload.Fields) != 0 || payload.Source != "" || payload.Scope != "" || payload.ScopeKey != "" || payload.SelectedWorkflowKey != "" {
			return payload
		}
	}

	var raw map[string]string
	if err := json.Unmarshal([]byte(detailsJSON), &raw); err != nil {
		return payload
	}
	fields := make(map[string]string)
	for key, value := range raw {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		switch key {
		case "", "source", "scope", "scope_key", "selected_workflow_key", "selected_skill_key":
			continue
		}
		if value != "" {
			fields[key] = value
		}
	}
	payload.Fields = fields
	return payload
}

func isPendingObservationMemory(summary sqlite.MemorySummary, details memoryDetails, memoryType string, stableKey string, fingerprint string) bool {
	if summary.MemoryType != memoryType || summary.Scope != "workflow" {
		return false
	}
	if details.Fields["watched_target_key"] != stableKey {
		return false
	}
	if strings.TrimSpace(fingerprint) != "" && details.Fields["observation_fingerprint"] != strings.TrimSpace(fingerprint) {
		return false
	}
	switch memoryType {
	case "social_draft":
		return details.Fields["approval"] == "pending"
	case "social_research":
		return details.Fields["status"] == "pending" || details.Fields["approval"] == "pending"
	default:
		return false
	}
}

func (service Service) ensureOwnerProject(ctx context.Context) (sqlite.Project, error) {
	project, err := service.Store.GetProjectByKey(ctx, ownerProjectKey)
	if err == nil {
		return project, nil
	}
	if err != sql.ErrNoRows {
		return sqlite.Project{}, err
	}

	manifest, ok := service.Registry.SystemProject()
	if !ok {
		return sqlite.Project{}, fmt.Errorf("odin-core project is required for social copilot job")
	}
	return service.Store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           manifest.Key,
		Name:          manifest.Name,
		Scope:         "odin-core",
		GitRoot:       manifest.GitRoot,
		DefaultBranch: manifest.DefaultBranch,
		GitHubRepo:    manifest.GitHub.Repo,
		ManifestPath:  manifest.SourcePath,
	})
}

func (service Service) nextRunAttempt(ctx context.Context, taskID int64) (int, error) {
	row := service.Store.DB().QueryRowContext(ctx, `
		SELECT COALESCE(MAX(attempt), 0) + 1
		FROM runs
		WHERE task_id = ?
	`, taskID)
	var attempt int
	if err := row.Scan(&attempt); err != nil {
		return 0, err
	}
	return attempt, nil
}

func (service Service) latestJobMetadata(ctx context.Context, taskID int64) (jobMetadata, error) {
	packets, err := service.Store.ListContextPackets(ctx, sqlite.ListContextPacketsParams{
		TaskID:      &taskID,
		PacketScope: jobPacketScope,
	})
	if err != nil {
		return jobMetadata{}, err
	}

	for index := len(packets) - 1; index >= 0; index-- {
		if strings.TrimSpace(packets[index].PayloadJSON) == "" {
			continue
		}
		var metadata jobMetadata
		if err := json.Unmarshal([]byte(packets[index].PayloadJSON), &metadata); err != nil {
			continue
		}
		if metadata.TargetStates == nil {
			metadata.TargetStates = map[string]TargetState{}
		}
		return metadata, nil
	}
	return jobMetadata{}, sql.ErrNoRows
}

func reconcileTargetStates(scope WatchScope, previous map[string]TargetState) map[string]TargetState {
	next := make(map[string]TargetState, len(scope.Targets))
	for _, target := range scope.Targets {
		state, ok := previous[target.StableKey]
		if !ok {
			state = TargetState{StableKey: target.StableKey}
		}
		state.StableKey = target.StableKey
		next[target.StableKey] = state
	}
	return next
}

func emptyTargetStates(scope WatchScope) map[string]TargetState {
	return reconcileTargetStates(scope, nil)
}

func watchTargetStableKeys(scope WatchScope) []string {
	if len(scope.Targets) == 0 {
		return nil
	}
	keys := make([]string, 0, len(scope.Targets))
	for _, target := range scope.Targets {
		keys = append(keys, target.StableKey)
	}
	return keys
}

func pollingJobTaskKey(workflowKey string) string {
	return "workflow-" + workflowKey + "-social-copilot-loop"
}

func checkpointKey(workflowKey string) string {
	return "social-copilot/" + workflowKey + "/social-copilot-loop"
}

func normalizeWorkflowKey(workflowKey string) string {
	workflowKey = strings.TrimSpace(workflowKey)
	if workflowKey == "" {
		return defaultWorkflowKey
	}
	return workflowKey
}

func (service Service) now() time.Time {
	if service.Now != nil {
		return service.Now().UTC()
	}
	return time.Now().UTC()
}
