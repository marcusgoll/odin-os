package triggers

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"odin-os/internal/core/projects"
	runtimeevents "odin-os/internal/runtime/events"
	"odin-os/internal/store/sqlite"
)

type Service struct {
	Store    *sqlite.Store
	Registry projects.Registry
}

type UpsertParams struct {
	WorkspaceID         string
	Key                 string
	InitiativeKey       string
	Kind                string
	Status              string
	RuleSummary         string
	RuleJSON            string
	WorkItemTitle       string
	NextEligibleAt      *time.Time
	Cadence             string
	Cron                string
	QuietHours          string
	QuietTimezone       string
	EventType           string
	MatchStatus         string
	MatchPreviousStatus string
	MatchTaskID         string
	MatchScope          string
	MatchProvider       string
	MatchRepo           string
	ExecutionIntent     string
}

type GitHubIssueIngestParams struct {
	ProjectKey string
	Repo       string
	Number     int
	Action     string
	Title      string
	Body       string
	URL        string
	Labels     string
}

type GitHubIssueIngestResult struct {
	Issue            sqlite.ExternalIssue
	Source           string
	EventType        string
	ExternalEventKey string
	ProjectKey       string
	Action           string
}

type DueEvaluationResult struct {
	Evaluated    int
	Materialized int
	Deferred     int
	Errored      int
	Results      []sqlite.FireAutomationTriggerResult
	Deferrals    []DeferredEvaluationResult
}

type DeferredEvaluationResult struct {
	Trigger       sqlite.AutomationTrigger
	Reason        string
	DueAt         time.Time
	DeferredUntil time.Time
}

func (service Service) Upsert(ctx context.Context, params UpsertParams) (sqlite.AutomationTrigger, error) {
	if service.Store == nil {
		return sqlite.AutomationTrigger{}, fmt.Errorf("automation trigger store is required")
	}
	initiativeKey := strings.TrimSpace(params.InitiativeKey)
	if initiativeKey == "" {
		return sqlite.AutomationTrigger{}, fmt.Errorf("automation trigger initiative key is required")
	}
	project, err := service.ensureRuntimeProject(ctx, initiativeKey)
	if err != nil {
		return sqlite.AutomationTrigger{}, err
	}

	ruleJSON := strings.TrimSpace(params.RuleJSON)
	ruleSummary := strings.TrimSpace(params.RuleSummary)
	cadence := strings.TrimSpace(params.Cadence)
	cron := strings.TrimSpace(params.Cron)
	quietHours := strings.TrimSpace(params.QuietHours)
	quietTimezone := strings.TrimSpace(params.QuietTimezone)
	eventType := strings.TrimSpace(params.EventType)
	matchStatus := strings.TrimSpace(params.MatchStatus)
	matchPreviousStatus := strings.TrimSpace(params.MatchPreviousStatus)
	matchTaskID := strings.TrimSpace(params.MatchTaskID)
	matchScope := strings.TrimSpace(params.MatchScope)
	matchProvider := strings.TrimSpace(params.MatchProvider)
	matchRepo := strings.TrimSpace(params.MatchRepo)
	executionIntent := strings.ToLower(strings.TrimSpace(params.ExecutionIntent))
	kind := strings.ToLower(strings.TrimSpace(params.Kind))
	if executionIntent != "" && !validTriggerExecutionIntent(executionIntent) {
		return sqlite.AutomationTrigger{}, fmt.Errorf("automation trigger intent must be one of read_only, mutation, governance, destructive")
	}
	if cadence != "" {
		if _, _, err := parseScheduleCadence(cadence); err != nil {
			return sqlite.AutomationTrigger{}, err
		}
	}
	if cron != "" {
		if _, err := nextCronEligibleAt(cron, time.Now().UTC()); err != nil {
			return sqlite.AutomationTrigger{}, err
		}
	}
	if cadence != "" && cron != "" {
		return sqlite.AutomationTrigger{}, fmt.Errorf("automation trigger cadence cannot be combined with cron")
	}
	if quietHours != "" {
		if _, err := parseQuietHoursRule(quietHours, quietTimezone); err != nil {
			return sqlite.AutomationTrigger{}, err
		}
	}
	if kind == "event" {
		if eventType == "" {
			return sqlite.AutomationTrigger{}, fmt.Errorf("automation trigger event type is required for event triggers")
		}
		if matchTaskID != "" {
			if _, err := strconv.ParseInt(matchTaskID, 10, 64); err != nil {
				return sqlite.AutomationTrigger{}, fmt.Errorf("automation trigger match_task_id must be an integer: %w", err)
			}
		}
	}
	if ruleJSON != "" && (cadence != "" || cron != "") {
		return sqlite.AutomationTrigger{}, fmt.Errorf("automation trigger cadence or cron cannot be combined with rule_json")
	}
	if ruleJSON == "" {
		payload := map[string]string{
			"summary": ruleSummary,
		}
		if kind := strings.TrimSpace(params.Kind); kind != "" {
			payload["kind"] = strings.ToLower(kind)
		}
		if eventType != "" {
			payload["event_type"] = eventType
		}
		if matchStatus != "" {
			payload["match_status"] = matchStatus
		}
		if matchPreviousStatus != "" {
			payload["match_previous_status"] = matchPreviousStatus
		}
		if matchTaskID != "" {
			payload["match_task_id"] = matchTaskID
		}
		if matchScope != "" {
			payload["match_scope"] = matchScope
		}
		if matchProvider != "" {
			payload["match_provider"] = matchProvider
		}
		if matchRepo != "" {
			payload["match_repo"] = matchRepo
		}
		if executionIntent != "" {
			payload["execution_intent"] = executionIntent
		}
		if cadence != "" {
			payload["cadence"] = cadence
		}
		if cron != "" {
			payload["cron"] = cron
		}
		if quietHours != "" {
			payload["quiet_hours"] = quietHours
			payload["quiet_timezone"] = defaultTriggerString(quietTimezone, "UTC")
		}
		encoded, err := json.Marshal(payload)
		if err != nil {
			return sqlite.AutomationTrigger{}, err
		}
		ruleJSON = string(encoded)
	}
	nextEligibleAt := params.NextEligibleAt
	if nextEligibleAt == nil && strings.EqualFold(strings.TrimSpace(params.Kind), "schedule") {
		var rule scheduleRule
		if err := json.Unmarshal([]byte(ruleJSON), &rule); err == nil && strings.TrimSpace(rule.Cron) != "" {
			next, err := nextCronEligibleAt(rule.Cron, service.now())
			if err != nil {
				return sqlite.AutomationTrigger{}, err
			}
			nextEligibleAt = &next
		}
	}

	return service.Store.UpsertAutomationTrigger(ctx, sqlite.UpsertAutomationTriggerParams{
		WorkspaceID:    params.WorkspaceID,
		Key:            params.Key,
		ProjectID:      project.ID,
		InitiativeKey:  project.Key,
		Kind:           params.Kind,
		Status:         params.Status,
		RuleJSON:       ruleJSON,
		RuleSummary:    ruleSummary,
		WorkItemTitle:  params.WorkItemTitle,
		NextEligibleAt: nextEligibleAt,
	})
}

func validTriggerExecutionIntent(intent string) bool {
	switch intent {
	case "read_only", "mutation", "governance", "destructive":
		return true
	default:
		return false
	}
}

func (service Service) List(ctx context.Context, workspaceID string) ([]sqlite.AutomationTrigger, error) {
	if service.Store == nil {
		return nil, fmt.Errorf("automation trigger store is required")
	}
	return service.Store.ListAutomationTriggers(ctx, sqlite.ListAutomationTriggersParams{
		WorkspaceID: strings.TrimSpace(workspaceID),
	})
}

func (service Service) Show(ctx context.Context, workspaceID string, key string) (sqlite.AutomationTrigger, error) {
	if service.Store == nil {
		return sqlite.AutomationTrigger{}, fmt.Errorf("automation trigger store is required")
	}
	return service.Store.GetAutomationTriggerByWorkspaceKey(ctx, workspaceID, key)
}

func (service Service) IngestGitHubIssue(ctx context.Context, params GitHubIssueIngestParams) (GitHubIssueIngestResult, error) {
	if service.Store == nil {
		return GitHubIssueIngestResult{}, fmt.Errorf("automation trigger store is required")
	}
	repo := strings.TrimSpace(params.Repo)
	projectKey := strings.TrimSpace(params.ProjectKey)
	if projectKey == "" {
		projectKey = service.projectKeyForGitHubRepo(repo)
	}
	if projectKey == "" {
		return GitHubIssueIngestResult{}, fmt.Errorf("github issue event project is required")
	}
	project, err := service.ensureRuntimeProject(ctx, projectKey)
	if err != nil {
		return GitHubIssueIngestResult{}, err
	}
	if repo == "" {
		repo = strings.TrimSpace(project.GitHubRepo)
	}
	if repo == "" {
		return GitHubIssueIngestResult{}, fmt.Errorf("github issue event repo is required")
	}
	if params.Number <= 0 {
		return GitHubIssueIngestResult{}, fmt.Errorf("github issue event number must be positive")
	}
	action := normalizeGitHubIssueAction(params.Action)
	bodyHash := hashExternalIssueBody(params.Body)
	labelsJSON, err := encodeExternalIssueLabels(params.Labels)
	if err != nil {
		return GitHubIssueIngestResult{}, err
	}
	externalEventKey := fmt.Sprintf("github:issue:%s:%d:%s", repo, params.Number, action)
	issue, err := service.Store.UpsertExternalIssue(ctx, sqlite.UpsertExternalIssueParams{
		ProjectID:  project.ID,
		Provider:   "github",
		Repo:       repo,
		Number:     params.Number,
		Title:      strings.TrimSpace(params.Title),
		BodyHash:   bodyHash,
		URL:        strings.TrimSpace(params.URL),
		State:      action,
		LabelsJSON: labelsJSON,
		SyncStatus: "event_received",
	})
	if err != nil {
		return GitHubIssueIngestResult{}, err
	}
	if err := service.Store.RecordExternalGitHubIssueEvent(ctx, sqlite.RecordExternalGitHubIssueEventParams{
		ProjectID:        project.ID,
		ProjectKey:       project.Key,
		ExternalIssueID:  issue.ID,
		Provider:         issue.Provider,
		Repo:             issue.Repo,
		Number:           issue.Number,
		Action:           action,
		Title:            issue.Title,
		BodyHash:         issue.BodyHash,
		URL:              issue.URL,
		LabelsJSON:       issue.LabelsJSON,
		ExternalEventKey: externalEventKey,
	}); err != nil {
		return GitHubIssueIngestResult{}, err
	}
	return GitHubIssueIngestResult{
		Issue:            issue,
		Source:           "github_issue",
		EventType:        string(runtimeevents.EventExternalGitHubIssue),
		ExternalEventKey: externalEventKey,
		ProjectKey:       project.Key,
		Action:           action,
	}, nil
}

func (service Service) Fire(ctx context.Context, params sqlite.FireAutomationTriggerParams) (sqlite.FireAutomationTriggerResult, error) {
	if service.Store == nil {
		return sqlite.FireAutomationTriggerResult{}, fmt.Errorf("automation trigger store is required")
	}
	return service.Store.FireAutomationTrigger(ctx, params)
}

func (service Service) EvaluateDue(ctx context.Context, now time.Time) (DueEvaluationResult, error) {
	if service.Store == nil {
		return DueEvaluationResult{}, fmt.Errorf("automation trigger store is required")
	}

	due, err := service.Store.ListDueAutomationTriggers(ctx, now.UTC())
	if err != nil {
		return DueEvaluationResult{}, err
	}

	var result DueEvaluationResult
	for _, trigger := range due {
		if trigger.NextEligibleAt == nil {
			continue
		}
		dueAt := *trigger.NextEligibleAt
		result.Evaluated++
		rule, err := parseTriggerScheduleRule(trigger)
		if err != nil {
			if _, markErr := service.Store.MarkAutomationTriggerErrored(ctx, sqlite.MarkAutomationTriggerErroredParams{
				WorkspaceID: trigger.WorkspaceID,
				Key:         trigger.Key,
				Reason:      "rule-evaluation",
				Error:       err.Error(),
			}); markErr != nil {
				return result, markErr
			}
			result.Errored++
			continue
		}
		rule, err = service.applyWorkspaceQuietHours(ctx, trigger, rule)
		if err != nil {
			if _, markErr := service.Store.MarkAutomationTriggerErrored(ctx, sqlite.MarkAutomationTriggerErroredParams{
				WorkspaceID: trigger.WorkspaceID,
				Key:         trigger.Key,
				Reason:      "quiet-hours-policy",
				Error:       err.Error(),
			}); markErr != nil {
				return result, markErr
			}
			result.Errored++
			continue
		}
		if deferredUntil, ok, err := quietHoursDeferral(rule, now.UTC()); err != nil {
			if _, markErr := service.Store.MarkAutomationTriggerErrored(ctx, sqlite.MarkAutomationTriggerErroredParams{
				WorkspaceID: trigger.WorkspaceID,
				Key:         trigger.Key,
				Reason:      "quiet-hours-evaluation",
				Error:       err.Error(),
			}); markErr != nil {
				return result, markErr
			}
			result.Errored++
			continue
		} else if ok {
			deferred, err := service.Store.DeferAutomationTrigger(ctx, sqlite.DeferAutomationTriggerParams{
				WorkspaceID:   trigger.WorkspaceID,
				Key:           trigger.Key,
				Reason:        "quiet_hours",
				DueAt:         dueAt,
				DeferredUntil: deferredUntil,
			})
			if err != nil {
				return result, err
			}
			result.Deferred++
			result.Deferrals = append(result.Deferrals, DeferredEvaluationResult{
				Trigger:       deferred,
				Reason:        "quiet_hours",
				DueAt:         dueAt,
				DeferredUntil: deferredUntil,
			})
			continue
		}
		nextEligibleAt, err := nextScheduleEligibleAt(rule, trigger, dueAt, now.UTC())
		if err != nil {
			if _, markErr := service.Store.MarkAutomationTriggerErrored(ctx, sqlite.MarkAutomationTriggerErroredParams{
				WorkspaceID: trigger.WorkspaceID,
				Key:         trigger.Key,
				Reason:      "rule-evaluation",
				Error:       err.Error(),
			}); markErr != nil {
				return result, markErr
			}
			result.Errored++
			continue
		}
		fire, err := service.Store.FireAutomationTrigger(ctx, sqlite.FireAutomationTriggerParams{
			WorkspaceID:       trigger.WorkspaceID,
			Key:               trigger.Key,
			Source:            "schedule",
			Reason:            scheduledDueReason(dueAt),
			RequestedBy:       "automation_trigger_evaluator",
			SetNextEligibleAt: true,
			NextEligibleAt:    nextEligibleAt,
		})
		if err != nil {
			return result, err
		}
		if fire.CreatedWorkItem {
			result.Materialized++
		}
		result.Results = append(result.Results, fire)
	}
	return result, nil
}

func (service Service) applyWorkspaceQuietHours(ctx context.Context, trigger sqlite.AutomationTrigger, rule scheduleRule) (scheduleRule, error) {
	if strings.TrimSpace(rule.QuietHours) != "" {
		return rule, nil
	}
	workspace, err := service.Store.GetWorkspaceByKey(ctx, defaultTriggerString(trigger.WorkspaceID, "default"))
	if errors.Is(err, sql.ErrNoRows) {
		return rule, nil
	}
	if err != nil {
		return rule, err
	}
	profile, err := service.Store.GetWorkspaceProfile(ctx, workspace.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return rule, nil
	}
	if err != nil {
		return rule, err
	}
	var preferences struct {
		QuietHours string `json:"quiet_hours"`
	}
	if strings.TrimSpace(profile.PreferencesJSON) == "" {
		return rule, nil
	}
	if err := json.Unmarshal([]byte(profile.PreferencesJSON), &preferences); err != nil {
		return rule, fmt.Errorf("workspace profile preferences JSON is invalid: %w", err)
	}
	if quietHours := strings.TrimSpace(preferences.QuietHours); quietHours != "" {
		rule.QuietHours = quietHours
		rule.QuietTimezone = "UTC"
	}
	return rule, nil
}

func (service Service) EvaluateEvents(ctx context.Context) (DueEvaluationResult, error) {
	if service.Store == nil {
		return DueEvaluationResult{}, fmt.Errorf("automation trigger store is required")
	}

	triggers, err := service.Store.ListAutomationTriggers(ctx, sqlite.ListAutomationTriggersParams{WorkspaceID: "default", Status: "enabled"})
	if err != nil {
		return DueEvaluationResult{}, err
	}
	records, err := service.Store.ListEvents(ctx, sqlite.ListEventsParams{})
	if err != nil {
		return DueEvaluationResult{}, err
	}

	var result DueEvaluationResult
	for _, trigger := range triggers {
		if !strings.EqualFold(trigger.Kind, "event") {
			continue
		}
		rule, err := parseTriggerScheduleRule(trigger)
		if err != nil {
			if _, markErr := service.Store.MarkAutomationTriggerErrored(ctx, sqlite.MarkAutomationTriggerErroredParams{
				WorkspaceID: trigger.WorkspaceID,
				Key:         trigger.Key,
				Reason:      "event-rule-evaluation",
				Error:       err.Error(),
			}); markErr != nil {
				return result, markErr
			}
			result.Errored++
			continue
		}
		if strings.TrimSpace(rule.EventType) == "" {
			if _, markErr := service.Store.MarkAutomationTriggerErrored(ctx, sqlite.MarkAutomationTriggerErroredParams{
				WorkspaceID: trigger.WorkspaceID,
				Key:         trigger.Key,
				Reason:      "event-rule-evaluation",
				Error:       "event_type is required",
			}); markErr != nil {
				return result, markErr
			}
			result.Errored++
			continue
		}
		for _, record := range records {
			if !record.OccurredAt.After(trigger.CreatedAt) {
				continue
			}
			if !eventTriggerMatches(rule, record) {
				continue
			}
			result.Evaluated++
			eventID := record.ID
			fire, err := service.Store.FireAutomationTrigger(ctx, sqlite.FireAutomationTriggerParams{
				WorkspaceID:     trigger.WorkspaceID,
				Key:             trigger.Key,
				Source:          "event",
				Reason:          eventTriggerReason(record),
				RequestedBy:     "automation_trigger_event_evaluator",
				SourceEventID:   &eventID,
				SourceEventType: string(record.Type),
			})
			if err != nil {
				return result, err
			}
			if fire.CreatedWorkItem {
				result.Materialized++
			}
			result.Results = append(result.Results, fire)
		}
	}
	return result, nil
}

func (service Service) ensureRuntimeProject(ctx context.Context, key string) (sqlite.Project, error) {
	manifest, ok := service.Registry.Lookup(key)
	if !ok {
		return sqlite.Project{}, fmt.Errorf("unknown initiative %q", key)
	}

	project, err := service.Store.GetProjectByKey(ctx, manifest.Key)
	if err == nil {
		return project, nil
	}
	if err != sql.ErrNoRows {
		return sqlite.Project{}, err
	}

	scopeValue := "project"
	if manifest.SystemProject {
		scopeValue = "odin-core"
	}
	return service.Store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           manifest.Key,
		Name:          manifest.Name,
		Scope:         scopeValue,
		GitRoot:       manifest.GitRoot,
		DefaultBranch: manifest.DefaultBranch,
		GitHubRepo:    manifest.GitHub.Repo,
		ManifestPath:  manifest.SourcePath,
	})
}

func (service Service) projectKeyForGitHubRepo(repo string) string {
	repo = strings.TrimSpace(repo)
	if repo == "" {
		return ""
	}
	for _, manifest := range service.Registry.Projects() {
		if strings.EqualFold(strings.TrimSpace(manifest.GitHub.Repo), repo) {
			return manifest.Key
		}
	}
	return ""
}

func (service Service) now() time.Time {
	if service.Store != nil && service.Store.Now != nil {
		return service.Store.Now().UTC()
	}
	return time.Now().UTC()
}

func scheduledDueReason(dueAt time.Time) string {
	return "due-" + dueAt.UTC().Format("20060102t150405z")
}

func normalizeGitHubIssueAction(value string) string {
	action := strings.ToLower(strings.TrimSpace(value))
	if action == "" {
		return "opened"
	}
	return action
}

func hashExternalIssueBody(body string) string {
	sum := sha256.Sum256([]byte(body))
	return fmt.Sprintf("sha256:%x", sum)
}

func encodeExternalIssueLabels(value string) (string, error) {
	parts := strings.Split(value, ",")
	labels := make([]string, 0, len(parts))
	for _, part := range parts {
		label := strings.TrimSpace(part)
		if label != "" {
			labels = append(labels, label)
		}
	}
	if labels == nil {
		labels = []string{}
	}
	encoded, err := json.Marshal(labels)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func nextScheduleEligibleAt(rule scheduleRule, trigger sqlite.AutomationTrigger, dueAt time.Time, evaluatedAt time.Time) (*time.Time, error) {
	if strings.TrimSpace(rule.Cron) != "" {
		next, err := nextCronEligibleAt(rule.Cron, dueAt)
		if err != nil {
			return nil, fmt.Errorf("automation trigger %s has invalid cron rule: %w", trigger.Key, err)
		}
		for !next.After(evaluatedAt.UTC()) {
			next, err = nextCronEligibleAt(rule.Cron, next)
			if err != nil {
				return nil, fmt.Errorf("automation trigger %s has invalid cron rule: %w", trigger.Key, err)
			}
		}
		return &next, nil
	}
	cadence, recurring, err := parseScheduleCadence(rule.Cadence)
	if err != nil {
		return nil, err
	}
	if rule.CadenceSeconds > 0 {
		cadence = time.Duration(rule.CadenceSeconds) * time.Second
		recurring = true
	}
	if !recurring {
		return nil, nil
	}
	next := dueAt.UTC().Add(cadence)
	for !next.After(evaluatedAt.UTC()) {
		next = next.Add(cadence)
	}
	return &next, nil
}

type scheduleRule struct {
	Cadence             string `json:"cadence"`
	CadenceSeconds      int64  `json:"cadence_seconds"`
	Cron                string `json:"cron"`
	QuietHours          string `json:"quiet_hours"`
	QuietTimezone       string `json:"quiet_timezone"`
	EventType           string `json:"event_type"`
	MatchStatus         string `json:"match_status"`
	MatchPreviousStatus string `json:"match_previous_status"`
	MatchTaskID         string `json:"match_task_id"`
	MatchScope          string `json:"match_scope"`
	MatchProvider       string `json:"match_provider"`
	MatchRepo           string `json:"match_repo"`
}

func parseTriggerScheduleRule(trigger sqlite.AutomationTrigger) (scheduleRule, error) {
	var rule scheduleRule
	if err := json.Unmarshal([]byte(trigger.RuleJSON), &rule); err != nil {
		return scheduleRule{}, fmt.Errorf("automation trigger %s has invalid rule json: %w", trigger.Key, err)
	}
	return rule, nil
}

func eventTriggerMatches(rule scheduleRule, record runtimeevents.Record) bool {
	if strings.TrimSpace(rule.EventType) != string(record.Type) {
		return false
	}
	if strings.TrimSpace(rule.MatchScope) != "" && strings.TrimSpace(rule.MatchScope) != record.Scope {
		return false
	}
	if strings.TrimSpace(rule.MatchTaskID) != "" {
		taskID, err := strconv.ParseInt(strings.TrimSpace(rule.MatchTaskID), 10, 64)
		if err != nil || record.TaskID == nil || *record.TaskID != taskID {
			return false
		}
	}
	if strings.TrimSpace(rule.MatchStatus) == "" &&
		strings.TrimSpace(rule.MatchPreviousStatus) == "" &&
		strings.TrimSpace(rule.MatchProvider) == "" &&
		strings.TrimSpace(rule.MatchRepo) == "" {
		return true
	}
	var payload map[string]any
	if err := json.Unmarshal(record.Payload, &payload); err != nil {
		return false
	}
	if strings.TrimSpace(rule.MatchStatus) != "" && !strings.EqualFold(payloadString(payload, "status"), strings.TrimSpace(rule.MatchStatus)) {
		return false
	}
	if strings.TrimSpace(rule.MatchPreviousStatus) != "" && !strings.EqualFold(payloadString(payload, "previous_status"), strings.TrimSpace(rule.MatchPreviousStatus)) {
		return false
	}
	if strings.TrimSpace(rule.MatchProvider) != "" && !strings.EqualFold(payloadString(payload, "provider"), strings.TrimSpace(rule.MatchProvider)) {
		return false
	}
	if strings.TrimSpace(rule.MatchRepo) != "" && !strings.EqualFold(payloadString(payload, "repo"), strings.TrimSpace(rule.MatchRepo)) {
		return false
	}
	return true
}

func payloadString(payload map[string]any, key string) string {
	value, ok := payload[key]
	if !ok {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	default:
		return fmt.Sprint(typed)
	}
}

func eventTriggerReason(record runtimeevents.Record) string {
	var payload map[string]any
	if err := json.Unmarshal(record.Payload, &payload); err == nil {
		if key := strings.TrimSpace(payloadString(payload, "external_event_key")); key != "" {
			return "external-" + sanitizeExternalEventKey(key)
		}
	}
	return fmt.Sprintf("event-%d", record.ID)
}

func sanitizeExternalEventKey(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var builder strings.Builder
	lastDash := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			builder.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			builder.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(builder.String(), "-")
}

type quietHoursRule struct {
	Start    time.Duration
	End      time.Duration
	Timezone string
}

func quietHoursDeferral(rule scheduleRule, now time.Time) (time.Time, bool, error) {
	quietRule, err := parseQuietHoursRule(rule.QuietHours, rule.QuietTimezone)
	if err != nil {
		return time.Time{}, false, err
	}
	if quietRule == nil {
		return time.Time{}, false, nil
	}
	now = now.UTC()
	current := time.Duration(now.Hour())*time.Hour + time.Duration(now.Minute())*time.Minute + time.Duration(now.Second())*time.Second
	if !quietRule.contains(current) {
		return time.Time{}, false, nil
	}
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	deferredUntil := midnight.Add(quietRule.End)
	if quietRule.crossesMidnight() && current >= quietRule.Start {
		deferredUntil = deferredUntil.Add(24 * time.Hour)
	}
	if !quietRule.crossesMidnight() && !deferredUntil.After(now) {
		deferredUntil = deferredUntil.Add(24 * time.Hour)
	}
	return deferredUntil, true, nil
}

func (rule quietHoursRule) contains(current time.Duration) bool {
	if rule.crossesMidnight() {
		return current >= rule.Start || current < rule.End
	}
	return current >= rule.Start && current < rule.End
}

func (rule quietHoursRule) crossesMidnight() bool {
	return rule.Start > rule.End
}

func parseQuietHoursRule(value string, timezone string) (*quietHoursRule, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	timezone = defaultTriggerString(timezone, "UTC")
	if !strings.EqualFold(timezone, "UTC") && !strings.EqualFold(timezone, "Z") {
		return nil, fmt.Errorf("automation trigger quiet timezone %q is not supported yet; use UTC", timezone)
	}
	startValue, endValue, ok := strings.Cut(value, "-")
	if !ok {
		return nil, fmt.Errorf("automation trigger quiet hours %q must use HH:MM-HH:MM", value)
	}
	start, err := parseQuietClock(startValue)
	if err != nil {
		return nil, err
	}
	end, err := parseQuietClock(endValue)
	if err != nil {
		return nil, err
	}
	if start == end {
		return nil, fmt.Errorf("automation trigger quiet hours start and end must differ")
	}
	return &quietHoursRule{Start: start, End: end, Timezone: "UTC"}, nil
}

func parseQuietClock(value string) (time.Duration, error) {
	parsed, err := time.Parse("15:04", strings.TrimSpace(value))
	if err != nil {
		return 0, fmt.Errorf("invalid automation trigger quiet clock %q: use HH:MM", value)
	}
	return time.Duration(parsed.Hour())*time.Hour + time.Duration(parsed.Minute())*time.Minute, nil
}

func parseScheduleCadence(value string) (time.Duration, bool, error) {
	value = strings.TrimSpace(value)
	switch strings.ToLower(value) {
	case "", "manual", "none", "once", "one-shot", "one_shot":
		return 0, false, nil
	}
	cadence, err := time.ParseDuration(value)
	if err != nil || cadence <= 0 {
		if err == nil {
			err = fmt.Errorf("cadence must be greater than zero")
		}
		return 0, false, fmt.Errorf("invalid automation trigger cadence %q: %w", value, err)
	}
	return cadence, true, nil
}

func defaultTriggerString(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

type cronSchedule struct {
	minutes    map[int]bool
	hours      map[int]bool
	days       map[int]bool
	months     map[int]bool
	weekdays   map[int]bool
	dayAny     bool
	weekdayAny bool
}

func nextCronEligibleAt(expression string, after time.Time) (time.Time, error) {
	parts := strings.Split(expression, ";")
	var schedules []cronSchedule
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		schedule, err := parseCronSchedule(part)
		if err != nil {
			return time.Time{}, err
		}
		schedules = append(schedules, schedule)
	}
	if len(schedules) == 0 {
		return time.Time{}, fmt.Errorf("cron expression is empty")
	}

	start := after.UTC().Truncate(time.Minute).Add(time.Minute)
	deadline := start.AddDate(5, 0, 0)
	for candidate := start; candidate.Before(deadline); candidate = candidate.Add(time.Minute) {
		for _, schedule := range schedules {
			if schedule.matches(candidate) {
				return candidate, nil
			}
		}
	}
	return time.Time{}, fmt.Errorf("no matching cron window found within five years")
}

func parseCronSchedule(expression string) (cronSchedule, error) {
	fields := strings.Fields(expression)
	if len(fields) != 5 {
		return cronSchedule{}, fmt.Errorf("cron expression %q must have five fields", expression)
	}
	minutes, _, err := parseCronField(fields[0], 0, 59)
	if err != nil {
		return cronSchedule{}, fmt.Errorf("minute field: %w", err)
	}
	hours, _, err := parseCronField(fields[1], 0, 23)
	if err != nil {
		return cronSchedule{}, fmt.Errorf("hour field: %w", err)
	}
	days, dayAny, err := parseCronField(fields[2], 1, 31)
	if err != nil {
		return cronSchedule{}, fmt.Errorf("day-of-month field: %w", err)
	}
	months, _, err := parseCronField(fields[3], 1, 12)
	if err != nil {
		return cronSchedule{}, fmt.Errorf("month field: %w", err)
	}
	weekdays, weekdayAny, err := parseCronField(fields[4], 0, 7)
	if err != nil {
		return cronSchedule{}, fmt.Errorf("day-of-week field: %w", err)
	}
	if weekdays[7] {
		weekdays[0] = true
		delete(weekdays, 7)
	}
	return cronSchedule{
		minutes:    minutes,
		hours:      hours,
		days:       days,
		months:     months,
		weekdays:   weekdays,
		dayAny:     dayAny,
		weekdayAny: weekdayAny,
	}, nil
}

func parseCronField(field string, min int, max int) (map[int]bool, bool, error) {
	field = strings.TrimSpace(field)
	if field == "" {
		return nil, false, fmt.Errorf("field is empty")
	}
	values := map[int]bool{}
	any := false
	for _, rawPart := range strings.Split(field, ",") {
		part := strings.TrimSpace(rawPart)
		if part == "" {
			return nil, false, fmt.Errorf("empty list item in %q", field)
		}
		base := part
		step := 1
		if strings.Contains(part, "/") {
			pieces := strings.Split(part, "/")
			if len(pieces) != 2 || strings.TrimSpace(pieces[1]) == "" {
				return nil, false, fmt.Errorf("invalid step %q", part)
			}
			base = strings.TrimSpace(pieces[0])
			parsedStep, err := strconv.Atoi(strings.TrimSpace(pieces[1]))
			if err != nil || parsedStep <= 0 {
				return nil, false, fmt.Errorf("invalid step %q", part)
			}
			step = parsedStep
		}
		start, end, partAny, err := cronFieldRange(base, min, max)
		if err != nil {
			return nil, false, err
		}
		if partAny {
			any = true
		}
		for value := start; value <= end; value += step {
			values[value] = true
		}
	}
	return values, any, nil
}

func cronFieldRange(base string, min int, max int) (int, int, bool, error) {
	base = strings.TrimSpace(base)
	if base == "" || base == "*" {
		return min, max, true, nil
	}
	if strings.Contains(base, "-") {
		pieces := strings.Split(base, "-")
		if len(pieces) != 2 {
			return 0, 0, false, fmt.Errorf("invalid range %q", base)
		}
		start, err := parseCronNumber(strings.TrimSpace(pieces[0]), min, max)
		if err != nil {
			return 0, 0, false, err
		}
		end, err := parseCronNumber(strings.TrimSpace(pieces[1]), min, max)
		if err != nil {
			return 0, 0, false, err
		}
		if start > end {
			return 0, 0, false, fmt.Errorf("invalid descending range %q", base)
		}
		return start, end, false, nil
	}
	value, err := parseCronNumber(base, min, max)
	if err != nil {
		return 0, 0, false, err
	}
	return value, value, false, nil
}

func parseCronNumber(value string, min int, max int) (int, error) {
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("invalid value %q", value)
	}
	if parsed < min || parsed > max {
		return 0, fmt.Errorf("value %d outside range %d-%d", parsed, min, max)
	}
	return parsed, nil
}

func (schedule cronSchedule) matches(value time.Time) bool {
	if !schedule.minutes[value.Minute()] || !schedule.hours[value.Hour()] || !schedule.months[int(value.Month())] {
		return false
	}
	dayMatches := schedule.days[value.Day()]
	weekdayMatches := schedule.weekdays[int(value.Weekday())]
	switch {
	case schedule.dayAny && schedule.weekdayAny:
		return true
	case schedule.dayAny:
		return weekdayMatches
	case schedule.weekdayAny:
		return dayMatches
	default:
		return dayMatches || weekdayMatches
	}
}
