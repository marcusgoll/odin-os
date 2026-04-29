package triggers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"odin-os/internal/core/projects"
	"odin-os/internal/store/sqlite"
)

type Service struct {
	Store    *sqlite.Store
	Registry projects.Registry
}

type UpsertParams struct {
	WorkspaceID    string
	Key            string
	InitiativeKey  string
	Kind           string
	Status         string
	RuleSummary    string
	RuleJSON       string
	WorkItemTitle  string
	NextEligibleAt *time.Time
	Cadence        string
	Cron           string
}

type DueEvaluationResult struct {
	Evaluated    int
	Materialized int
	Errored      int
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
		if cadence != "" {
			payload["cadence"] = cadence
		}
		if cron != "" {
			payload["cron"] = cron
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
		nextEligibleAt, err := nextScheduleEligibleAt(trigger, dueAt)
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

func (service Service) now() time.Time {
	if service.Store != nil && service.Store.Now != nil {
		return service.Store.Now().UTC()
	}
	return time.Now().UTC()
}

func scheduledDueReason(dueAt time.Time) string {
	return "due-" + dueAt.UTC().Format("20060102t150405z")
}

func nextScheduleEligibleAt(trigger sqlite.AutomationTrigger, dueAt time.Time) (*time.Time, error) {
	rule, err := parseTriggerScheduleRule(trigger)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(rule.Cron) != "" {
		next, err := nextCronEligibleAt(rule.Cron, dueAt)
		if err != nil {
			return nil, fmt.Errorf("automation trigger %s has invalid cron rule: %w", trigger.Key, err)
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
	return &next, nil
}

type scheduleRule struct {
	Cadence        string `json:"cadence"`
	CadenceSeconds int64  `json:"cadence_seconds"`
	Cron           string `json:"cron"`
}

func parseTriggerScheduleRule(trigger sqlite.AutomationTrigger) (scheduleRule, error) {
	var rule scheduleRule
	if err := json.Unmarshal([]byte(trigger.RuleJSON), &rule); err != nil {
		return scheduleRule{}, fmt.Errorf("automation trigger %s has invalid rule json: %w", trigger.Key, err)
	}
	return rule, nil
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
