package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	runtimeevents "odin-os/internal/runtime/events"
	"odin-os/internal/runtime/triggers"
	"odin-os/internal/store/sqlite"
)

const TriggerUsage = "trigger [list|show <key>|create <key>|upsert <key>|seed marcus-brand-os|test <key>|audit <key>|materialize <key>|fire <key>|evaluate|ingest github-issue] [key=value ...] [--json]"

func RunTrigger(ctx context.Context, service triggers.Service, args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: odin %s", TriggerUsage)
	}
	if args[0] == "--help" || args[0] == "help" {
		_, err := fmt.Fprintf(stdout, "usage: odin %s\n\nScheduled triggers:\n  odin trigger create <key> initiative=<project> kind=schedule status=enabled next=<RFC3339> [cadence=<duration>] [cron=<expr>] [quiet=<HH:MM-HH:MM>] [batch=<key> batch_window=<duration>] [title=<text>] [summary=<text>] [intent=<read_only|mutation|governance|destructive>] [--json]\n  odin trigger upsert <key> initiative=<project> kind=schedule status=enabled next=<RFC3339> [cadence=<duration>] [cron=<expr>] [quiet=<HH:MM-HH:MM>] [batch=<key> batch_window=<duration>] [title=<text>] [summary=<text>] [intent=<read_only|mutation|governance|destructive>] [--json]\n  odin trigger seed marcus-brand-os [initiative=marcusgoll] [workspace=default] [status=enabled] [start=<YYYY-MM-DD>] [timezone=America/New_York] [--json]\n  odin trigger test <key> now=<RFC3339> [--json]\n  odin trigger evaluate now=<RFC3339> [--json]\n\nManual trigger fire:\n  odin trigger materialize <key> [reason=<reason>] [--json]\n  odin trigger fire <key> [reason=<reason>] [--json]\n\nAudit:\n  odin trigger audit <key> [--json]\n\nEvent triggers:\n  odin trigger create <key> initiative=<project> kind=event event=external.github.issue [match_status=<status>] [match_previous_status=<status>] [match_task_id=<id>] [match_scope=<scope>] [match_provider=<provider>] [match_repo=<owner/repo>] [intent=<read_only|mutation|governance|destructive>] [--json]\n  odin trigger test <key> source=events [--json]\n  odin trigger evaluate source=events [--json]\n\nExternal event ingest:\n  odin trigger ingest github-issue project=<project> repo=<owner/repo> number=<n> action=<opened> title=<text> [body=<text>] [url=<url>] [labels=a,b] [--json]\n", TriggerUsage)
		return err
	}
	jsonOutput, args, err := consumeTriggerJSONFlag(args)
	if err != nil {
		return err
	}
	if args[0] == "list" {
		options, err := parseOptionTokens(args[1:])
		if err != nil {
			return err
		}
		return runTriggerList(ctx, service, options["workspace"], stdout, jsonOutput)
	}

	switch strings.ToLower(args[0]) {
	case "seed":
		if len(args) < 2 || !strings.EqualFold(args[1], "marcus-brand-os") {
			return fmt.Errorf("usage: odin %s", TriggerUsage)
		}
		options, err := parseOptionTokens(args[2:])
		if err != nil {
			return err
		}
		return runTriggerSeedMarcusBrandOS(ctx, service, options, stdout, jsonOutput)
	case "show":
		if len(args) < 2 {
			return fmt.Errorf("usage: odin %s", TriggerUsage)
		}
		options, err := parseOptionTokens(args[2:])
		if err != nil {
			return err
		}
		return runTriggerShow(ctx, service, options["workspace"], args[1], stdout, jsonOutput)
	case "create", "upsert":
		if len(args) < 2 {
			return fmt.Errorf("usage: odin %s", TriggerUsage)
		}
		options, err := parseOptionTokens(args[2:])
		if err != nil {
			return err
		}
		nextEligibleAt, err := parseTriggerNextEligibleAt(options["next"])
		if err != nil {
			return err
		}
		trigger, err := service.Upsert(ctx, triggers.UpsertParams{
			WorkspaceID:         options["workspace"],
			Key:                 args[1],
			InitiativeKey:       options["initiative"],
			Kind:                options["kind"],
			Status:              options["status"],
			RuleSummary:         triggerFirstNonEmpty(options["rule"], options["summary"]),
			RuleJSON:            options["rule_json"],
			WorkItemTitle:       strings.ReplaceAll(options["title"], "_", " "),
			NextEligibleAt:      nextEligibleAt,
			Cadence:             options["cadence"],
			Cron:                strings.ReplaceAll(options["cron"], "_", " "),
			QuietHours:          triggerFirstNonEmpty(options["quiet"], options["quiet_hours"]),
			QuietTimezone:       triggerFirstNonEmpty(options["quiet_tz"], options["quiet_timezone"], options["timezone"]),
			BatchKey:            triggerFirstNonEmpty(options["batch"], options["batch_key"]),
			BatchWindow:         options["batch_window"],
			EventType:           triggerFirstNonEmpty(options["event"], options["event_type"]),
			MatchStatus:         options["match_status"],
			MatchPreviousStatus: options["match_previous_status"],
			MatchTaskID:         options["match_task_id"],
			MatchScope:          options["match_scope"],
			MatchProvider:       options["match_provider"],
			MatchRepo:           options["match_repo"],
			ExecutionIntent:     triggerFirstNonEmpty(options["intent"], options["execution_intent"]),
		})
		if err != nil {
			return err
		}
		if jsonOutput {
			return WriteJSON(stdout, triggerEnvelope{Trigger: newAutomationTriggerView(trigger)})
		}
		_, err = fmt.Fprintf(stdout, "trigger=%s status=%s workspace=%s initiative=%s kind=%s\n",
			trigger.Key,
			trigger.Status,
			trigger.WorkspaceID,
			trigger.InitiativeKey,
			trigger.Kind,
		)
		return err
	case "test":
		if len(args) < 2 {
			return fmt.Errorf("usage: odin %s", TriggerUsage)
		}
		options, err := parseOptionTokens(args[2:])
		if err != nil {
			return err
		}
		now, err := parseTriggerEvaluateAt(options["now"])
		if err != nil {
			return err
		}
		result, err := service.PreviewTrigger(ctx, triggers.PreviewTriggerParams{
			WorkspaceID: options["workspace"],
			Key:         args[1],
			Now:         now,
			Source:      triggerFirstNonEmpty(options["source"], options["mode"]),
		})
		if err != nil {
			return err
		}
		if err := service.RecordTestAudit(ctx, result); err != nil {
			return err
		}
		if jsonOutput {
			return WriteJSON(stdout, newTriggerPreviewView(result))
		}
		_, err = fmt.Fprintf(stdout, "trigger test now=%s evaluated=%d would_run=%d would_defer=%d would_batch=%d approval_required=%d mutates=false\n",
			result.Now.UTC().Format(time.RFC3339),
			result.Evaluated,
			result.WouldRun,
			result.WouldDefer,
			result.WouldBatch,
			result.ApprovalRequired,
		)
		if err != nil {
			return err
		}
		for _, decision := range result.Decisions {
			if _, err := fmt.Fprintf(stdout, "trigger=%s decision=%s reason=%s quiet_hour_effect=%s batch=%s approval_required=%t recovery_state=%s\n",
				decision.Trigger.Key,
				decision.Decision,
				decision.Reason,
				defaultTriggerOutput(decision.QuietHourEffect, "none"),
				formatBatchState(decision.BatchKey, decision.BatchWindow),
				decision.ApprovalRequired,
				decision.RecoveryState,
			); err != nil {
				return err
			}
		}
		return nil
	case "audit":
		if len(args) < 2 {
			return fmt.Errorf("usage: odin %s", TriggerUsage)
		}
		options, err := parseOptionTokens(args[2:])
		if err != nil {
			return err
		}
		events, err := service.AuditEvents(ctx, options["workspace"], args[1])
		if err != nil {
			return err
		}
		if jsonOutput {
			return WriteJSON(stdout, newTriggerAuditView(args[1], events))
		}
		if _, err := fmt.Fprintf(stdout, "trigger=%s audit_events=%d\n", args[1], len(events)); err != nil {
			return err
		}
		for _, event := range events {
			if _, err := fmt.Fprintf(stdout, "event_id=%d event_type=%s occurred_at=%s\n",
				event.ID,
				event.EventType,
				event.OccurredAt.UTC().Format(time.RFC3339),
			); err != nil {
				return err
			}
		}
		return nil
	case "fire", "materialize":
		if len(args) < 2 {
			return fmt.Errorf("usage: odin %s", TriggerUsage)
		}
		options, err := parseOptionTokens(args[2:])
		if err != nil {
			return err
		}
		result, err := service.Fire(ctx, sqlite.FireAutomationTriggerParams{
			WorkspaceID: options["workspace"],
			Key:         args[1],
			Reason:      options["reason"],
			RequestedBy: "operator",
		})
		if err != nil {
			return err
		}
		if jsonOutput {
			return WriteJSON(stdout, newTriggerFireView(result))
		}
		_, err = fmt.Fprintf(stdout, "trigger=%s status=%s materialization_key=%s work_item=%s created=%t\n",
			result.Trigger.Key,
			result.Trigger.Status,
			result.Materialization.MaterializationKey,
			result.WorkItem.Key,
			result.CreatedWorkItem,
		)
		return err
	case "evaluate":
		options, err := parseOptionTokens(args[1:])
		if err != nil {
			return err
		}
		var result triggers.DueEvaluationResult
		var evaluateErr error
		if triggerEvaluateUsesEvents(options) {
			result, evaluateErr = service.EvaluateEvents(ctx)
		} else {
			var evaluateAt time.Time
			evaluateAt, evaluateErr = parseTriggerEvaluateAt(options["now"])
			if evaluateErr != nil {
				return evaluateErr
			}
			result, evaluateErr = service.EvaluateDue(ctx, evaluateAt)
		}
		if evaluateErr != nil {
			return evaluateErr
		}
		if jsonOutput {
			return WriteJSON(stdout, newTriggerEvaluateView(result))
		}
		_, err = fmt.Fprintf(stdout, "automation_trigger_evaluation evaluated=%d materialized=%d errored=%d\n",
			result.Evaluated,
			result.Materialized,
			result.Errored,
		)
		return err
	case "ingest":
		if len(args) < 2 || !strings.EqualFold(args[1], "github-issue") {
			return fmt.Errorf("usage: odin %s", TriggerUsage)
		}
		options, err := parseOptionTokens(args[2:])
		if err != nil {
			return err
		}
		number, err := strconv.Atoi(options["number"])
		if err != nil {
			return fmt.Errorf("github issue event number must be an integer: %w", err)
		}
		result, err := service.IngestGitHubIssue(ctx, triggers.GitHubIssueIngestParams{
			ProjectKey: triggerFirstNonEmpty(options["project"], options["initiative"]),
			Repo:       options["repo"],
			Number:     number,
			Action:     options["action"],
			Title:      strings.ReplaceAll(options["title"], "_", " "),
			Body:       strings.ReplaceAll(options["body"], "_", " "),
			URL:        options["url"],
			Labels:     options["labels"],
		})
		if err != nil {
			return err
		}
		if jsonOutput {
			return WriteJSON(stdout, newTriggerGitHubIssueIngestView(result))
		}
		_, err = fmt.Fprintf(stdout, "external_event source=%s event_type=%s key=%s repo=%s number=%d action=%s\n",
			result.Source,
			result.EventType,
			result.ExternalEventKey,
			result.Issue.Repo,
			result.Issue.Number,
			result.Action,
		)
		return err
	default:
		return fmt.Errorf("unknown trigger command: %s", args[0])
	}
}

func runTriggerList(ctx context.Context, service triggers.Service, workspaceID string, stdout io.Writer, jsonOutput bool) error {
	items, err := service.List(ctx, workspaceID)
	if err != nil {
		return err
	}
	if jsonOutput {
		views := make([]automationTriggerView, 0, len(items))
		for _, item := range items {
			views = append(views, newAutomationTriggerView(item))
		}
		return WriteJSON(stdout, triggerListView{Triggers: views})
	}
	if _, err := fmt.Fprintf(stdout, "automation_triggers total=%d\n", len(items)); err != nil {
		return err
	}
	for _, item := range items {
		if _, err := fmt.Fprintf(stdout, "trigger=%s workspace=%s initiative=%s kind=%s status=%s readiness=%s last_materialization=%s last_work_item=%s next_eligible=%s\n",
			item.Key,
			item.WorkspaceID,
			item.InitiativeKey,
			item.Kind,
			item.Status,
			triggerReadiness(item),
			noneIfEmpty(item.LastMaterializationKey),
			noneIfEmpty(item.LastWorkItemKey),
			formatOptionalTime(item.NextEligibleAt),
		); err != nil {
			return err
		}
	}
	return nil
}

func runTriggerShow(ctx context.Context, service triggers.Service, workspaceID string, key string, stdout io.Writer, jsonOutput bool) error {
	item, err := service.Show(ctx, workspaceID, key)
	if err != nil {
		return err
	}
	if jsonOutput {
		return WriteJSON(stdout, triggerEnvelope{Trigger: newAutomationTriggerView(item)})
	}
	auditEvents, err := service.AuditEvents(ctx, workspaceID, key)
	if err != nil {
		return err
	}
	details := triggerOperatorDetails(item, time.Now().UTC())
	_, err = fmt.Fprintf(stdout, "trigger=%s workspace=%s initiative=%s type=%s kind=%s status=%s readiness=%s schedule=%s next_run=%s last_run=%s quiet_hours=%s quiet_hour_effect=%s batch=%s approval_required=%t recovery_state=%s rule_summary=%q last_materialization=%s last_work_item=%s audit_events=%d\n",
		item.Key,
		item.WorkspaceID,
		item.InitiativeKey,
		item.Kind,
		item.Kind,
		item.Status,
		triggerReadiness(item),
		details.Schedule,
		formatOptionalTime(item.NextEligibleAt),
		formatOptionalTime(item.LastMaterializedAt),
		defaultTriggerOutput(details.QuietHours, "none"),
		details.QuietHourEffect,
		formatBatchState(details.BatchKey, details.BatchWindow),
		details.ApprovalRequired,
		details.RecoveryState,
		item.RuleSummary,
		noneIfEmpty(item.LastMaterializationKey),
		noneIfEmpty(item.LastWorkItemKey),
		len(auditEvents),
	)
	return err
}

type marcusBrandOSRoutine struct {
	Key        string
	SkillKey   string
	Title      string
	Summary    string
	LocalTime  string
	Cadence    string
	QuietHours string
	Request    string
}

var marcusBrandOSRoutines = []marcusBrandOSRoutine{
	{
		Key:        "marcus-brand-morning-editorial-scan",
		SkillKey:   "marcus-editorial-strategist",
		Title:      "Marcus brand morning editorial scan",
		Summary:    "Review current brand inputs and choose the strongest aviation-first priorities for the day.",
		LocalTime:  "08:30",
		Cadence:    "24h",
		QuietHours: "00:00-12:00",
		Request:    "Run the morning editorial scan for the Marcus Personal Brand Operating System. Review current ideas, recent outcomes, resource gaps, and audience needs. Return the top priorities for today, any no-go topics, and which lane should draft next. Do not publish or send anything.",
	},
	{
		Key:        "marcus-brand-engagement-opportunity-check",
		SkillKey:   "marcus-engagement-research-assistant",
		Title:      "Marcus brand engagement opportunity check",
		Summary:    "Check for approval-safe engagement and reply opportunities without taking account actions.",
		LocalTime:  "10:30",
		Cadence:    "4h",
		QuietHours: "00:00-12:00",
		Request:    "Review operator-provided social observations, pending social research, and explicit watch inputs for useful engagement opportunities. Suggest replies or no-reply decisions only. Do not like, repost, follow, DM, reply, publish, or bypass approval.",
	},
	{
		Key:        "marcus-brand-midday-writing-pass",
		SkillKey:   "marcus-writing-partner",
		Title:      "Marcus brand midday writing pass",
		Summary:    "Draft or revise one approval-ready Marcus Teaching Voice asset from the current top priority.",
		LocalTime:  "12:30",
		Cadence:    "24h",
		QuietHours: "00:00-12:00",
		Request:    "Draft or revise one high-signal Marcus Teaching Voice asset from the current editorial priority. Prefer aviation training, CFI decision-making, pilot progression, or practical teaching value. Mark public outputs as approval-required and do not publish.",
	},
	{
		Key:        "marcus-brand-resource-production-pass",
		SkillKey:   "marcus-resource-producer",
		Title:      "Marcus brand resource production pass",
		Summary:    "Turn recurring content ideas into useful resource or lead-magnet candidates.",
		LocalTime:  "15:00",
		Cadence:    "24h",
		QuietHours: "00:00-12:00",
		Request:    "Review the strongest recurring audience problems and propose one useful resource, checklist, guide, or tool candidate. Include intended audience, conversion path, source material needed, and next production step. Do not publish or send.",
	},
	{
		Key:        "marcus-brand-newsletter-editorial-pass",
		SkillKey:   "marcus-newsletter-editor",
		Title:      "Marcus brand newsletter editorial pass",
		Summary:    "Prepare or revise newsletter material from approved brand assets and recent learnings.",
		LocalTime:  "16:00",
		Cadence:    "168h",
		QuietHours: "00:00-12:00",
		Request:    "Prepare the next newsletter angle from approved or reviewable Marcus brand material. Include subject direction, core lesson, source assets, missing facts, and approval notes. Do not send email or publish.",
	},
	{
		Key:        "marcus-brand-evening-growth-review",
		SkillKey:   "marcus-growth-analyst",
		Title:      "Marcus brand evening growth review",
		Summary:    "Review brand outcomes and feed evidence-backed adjustments into tomorrow's plan.",
		LocalTime:  "17:30",
		Cadence:    "24h",
		QuietHours: "00:00-12:00",
		Request:    "Review today's brand work, pending approvals, social outcomes, evidence, and resource progress. Return what to keep, avoid, test next, and carry into tomorrow. Use evidence where available and label unknowns.",
	},
}

type triggerSeedView struct {
	Seed       string                  `json:"seed"`
	Workspace  string                  `json:"workspace"`
	Initiative string                  `json:"initiative"`
	Status     string                  `json:"status"`
	Timezone   string                  `json:"timezone"`
	StartDate  string                  `json:"start_date"`
	Triggers   []automationTriggerView `json:"triggers"`
}

func runTriggerSeedMarcusBrandOS(ctx context.Context, service triggers.Service, options map[string]string, stdout io.Writer, jsonOutput bool) error {
	workspaceID := triggerFirstNonEmpty(options["workspace"], "default")
	initiativeKey := triggerFirstNonEmpty(options["initiative"], options["project"], "marcusgoll")
	status := triggerFirstNonEmpty(options["status"], "enabled")
	timezone := triggerFirstNonEmpty(options["timezone"], options["quiet_tz"], "America/New_York")
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return fmt.Errorf("invalid marcus-brand-os timezone %q: %w", timezone, err)
	}
	explicitStart := strings.TrimSpace(options["start"]) != ""
	startDate, err := parseMarcusBrandOSStartDate(options["start"], location)
	if err != nil {
		return err
	}

	views := make([]automationTriggerView, 0, len(marcusBrandOSRoutines))
	for _, routine := range marcusBrandOSRoutines {
		nextEligibleAt, err := nextMarcusBrandOSRoutineAt(startDate, routine.LocalTime, routine.Cadence, location, explicitStart)
		if err != nil {
			return err
		}
		ruleJSON, err := marcusBrandOSRuleJSON(routine, initiativeKey)
		if err != nil {
			return err
		}
		trigger, err := service.Upsert(ctx, triggers.UpsertParams{
			WorkspaceID:    workspaceID,
			Key:            routine.Key,
			InitiativeKey:  initiativeKey,
			Kind:           "schedule",
			Status:         status,
			RuleSummary:    routine.Summary,
			RuleJSON:       ruleJSON,
			WorkItemTitle:  routine.Title,
			NextEligibleAt: &nextEligibleAt,
		})
		if err != nil {
			return err
		}
		views = append(views, newAutomationTriggerView(trigger))
	}

	if jsonOutput {
		return WriteJSON(stdout, triggerSeedView{
			Seed:       "marcus-brand-os",
			Workspace:  workspaceID,
			Initiative: initiativeKey,
			Status:     status,
			Timezone:   timezone,
			StartDate:  startDate.Format("2006-01-02"),
			Triggers:   views,
		})
	}

	if _, err := fmt.Fprintf(stdout, "seed=marcus-brand-os triggers=%d workspace=%s initiative=%s status=%s timezone=%s start=%s\n",
		len(views),
		workspaceID,
		initiativeKey,
		status,
		timezone,
		startDate.Format("2006-01-02"),
	); err != nil {
		return err
	}
	for _, view := range views {
		next := "none"
		if view.NextEligibleAt != nil {
			next = *view.NextEligibleAt
		}
		if _, err := fmt.Fprintf(stdout, "trigger=%s skill_invocation=true next_run=%s cadence=%s approval_required=false\n",
			view.Key,
			next,
			triggerRoutineCadence(view.Key),
		); err != nil {
			return err
		}
	}
	return nil
}

func marcusBrandOSRuleJSON(routine marcusBrandOSRoutine, projectKey string) (string, error) {
	payload := map[string]any{
		"summary":          routine.Summary,
		"kind":             "schedule",
		"cadence":          routine.Cadence,
		"quiet_hours":      routine.QuietHours,
		"quiet_timezone":   "UTC",
		"execution_intent": "read_only",
		"skill_invocation": map[string]any{
			"skill_key":               routine.SkillKey,
			"scope":                   "project",
			"project_key":             projectKey,
			"execution_intent":        "read_only",
			"execution_intent_source": "trigger",
			"review_state":            "review_required",
			"input_json": map[string]any{
				"request":           routine.Request,
				"workflow_key":      "marcus-personal-brand-operating-system",
				"source":            "marcus-brand-os-trigger",
				"approval_boundary": "internal drafting and analysis only; public actions require Marcus approval",
			},
		},
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func parseMarcusBrandOSStartDate(value string, location *time.Location) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		now := time.Now().In(location)
		return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, location), nil
	}
	parsed, err := time.ParseInLocation("2006-01-02", value, location)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid marcus-brand-os start value %q: use YYYY-MM-DD", value)
	}
	return parsed, nil
}

func nextMarcusBrandOSRoutineAt(startDate time.Time, localClock string, cadence string, location *time.Location, explicitStart bool) (time.Time, error) {
	parts := strings.Split(localClock, ":")
	if len(parts) != 2 {
		return time.Time{}, fmt.Errorf("invalid marcus-brand-os local time %q", localClock)
	}
	hour, err := strconv.Atoi(parts[0])
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid marcus-brand-os hour %q: %w", localClock, err)
	}
	minute, err := strconv.Atoi(parts[1])
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid marcus-brand-os minute %q: %w", localClock, err)
	}
	next := time.Date(startDate.Year(), startDate.Month(), startDate.Day(), hour, minute, 0, 0, location)
	duration, err := time.ParseDuration(cadence)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid marcus-brand-os cadence %q: %w", cadence, err)
	}
	if !explicitStart {
		now := time.Now().In(location)
		for !next.After(now) {
			next = next.Add(duration)
		}
	}
	return next.UTC(), nil
}

func triggerRoutineCadence(key string) string {
	for _, routine := range marcusBrandOSRoutines {
		if routine.Key == key {
			return routine.Cadence
		}
	}
	return "unknown"
}

type triggerEnvelope struct {
	Trigger automationTriggerView `json:"trigger"`
}

type triggerListView struct {
	Triggers []automationTriggerView `json:"triggers"`
}

type triggerEvaluateView struct {
	Evaluated    int                   `json:"evaluated"`
	Materialized int                   `json:"materialized"`
	Deferred     int                   `json:"deferred"`
	Errored      int                   `json:"errored"`
	Results      []triggerFireView     `json:"results"`
	Deferrals    []triggerDeferralView `json:"deferrals"`
}

type triggerFireView struct {
	Trigger         automationTriggerView      `json:"trigger"`
	Materialization triggerMaterializationView `json:"materialization"`
	WorkItem        triggerWorkItemView        `json:"work_item"`
	CreatedWorkItem bool                       `json:"created_work_item"`
}

type triggerGitHubIssueIngestView struct {
	Source           string `json:"source"`
	EventType        string `json:"event_type"`
	ExternalEventKey string `json:"external_event_key"`
	ProjectKey       string `json:"project_key"`
	Provider         string `json:"provider"`
	Repo             string `json:"repo"`
	Number           int    `json:"number"`
	Action           string `json:"action"`
	Title            string `json:"title"`
	URL              string `json:"url,omitempty"`
	ExternalIssueID  int64  `json:"external_issue_id"`
	SyncStatus       string `json:"sync_status"`
}

type triggerPreviewView struct {
	Now              string                       `json:"now"`
	DryRun           bool                         `json:"dry_run"`
	Mutates          bool                         `json:"mutates"`
	Evaluated        int                          `json:"evaluated"`
	WouldRun         int                          `json:"would_run"`
	WouldDefer       int                          `json:"would_defer"`
	WouldBatch       int                          `json:"would_batch"`
	ApprovalRequired int                          `json:"approval_required"`
	Errored          int                          `json:"errored"`
	Decisions        []triggerPreviewDecisionView `json:"decisions"`
}

type triggerPreviewDecisionView struct {
	Key                string                                   `json:"key"`
	Decision           string                                   `json:"decision"`
	Reason             string                                   `json:"reason"`
	MaterializationKey string                                   `json:"materialization_key,omitempty"`
	EventEnvelope      *runtimeevents.AutomationTriggerEnvelope `json:"event_envelope,omitempty"`
	TriggerType        string                                   `json:"trigger_type"`
	Schedule           string                                   `json:"schedule"`
	EventType          string                                   `json:"event_type,omitempty"`
	DueAt              *string                                  `json:"due_at,omitempty"`
	NextRun            *string                                  `json:"next_run,omitempty"`
	LastRun            *string                                  `json:"last_run,omitempty"`
	QuietHours         string                                   `json:"quiet_hours"`
	QuietHourEffect    string                                   `json:"quiet_hour_effect"`
	BatchKey           string                                   `json:"batch_key,omitempty"`
	BatchWindow        string                                   `json:"batch_window,omitempty"`
	BatchGroup         string                                   `json:"batch_group,omitempty"`
	CandidateEvents    int                                      `json:"candidate_events,omitempty"`
	MatchedEvents      []triggerPreviewEventMatchView           `json:"matched_events,omitempty"`
	ApprovalRequired   bool                                     `json:"approval_required"`
	RecoveryState      string                                   `json:"recovery_state"`
	Mutates            bool                                     `json:"mutates"`
	Error              string                                   `json:"error,omitempty"`
}

type triggerPreviewEventMatchView struct {
	ID         int64  `json:"id"`
	EventType  string `json:"event_type"`
	OccurredAt string `json:"occurred_at"`
	Reason     string `json:"reason"`
}

type triggerAuditView struct {
	TriggerKey  string                  `json:"trigger_key"`
	AuditEvents []triggerAuditEventView `json:"audit_events"`
}

type triggerAuditEventView struct {
	ID         int64           `json:"id"`
	EventType  string          `json:"event_type"`
	OccurredAt string          `json:"occurred_at"`
	Payload    json.RawMessage `json:"payload"`
}

type triggerDetailsView struct {
	Schedule         string
	QuietHours       string
	QuietHourEffect  string
	BatchKey         string
	BatchWindow      string
	ApprovalRequired bool
	RecoveryState    string
}

type automationTriggerView struct {
	ID                     int64   `json:"id"`
	Key                    string  `json:"key"`
	WorkspaceID            string  `json:"workspace_id"`
	InitiativeKey          string  `json:"initiative_key"`
	Kind                   string  `json:"kind"`
	Status                 string  `json:"status"`
	Readiness              string  `json:"readiness"`
	TimingStatus           string  `json:"timing_status"`
	RuleSummary            string  `json:"rule_summary"`
	RuleJSON               string  `json:"rule_json"`
	WorkItemTitle          string  `json:"work_item_title"`
	NextEligibleAt         *string `json:"next_eligible_at"`
	LastEvaluatedAt        *string `json:"last_evaluated_at"`
	LastMaterializedAt     *string `json:"last_materialized_at"`
	LastMaterializationKey string  `json:"last_materialization_key"`
	LastWorkItemID         *int64  `json:"last_work_item_id"`
	LastWorkItemKey        string  `json:"last_work_item_key"`
	CreatedAt              string  `json:"created_at"`
	UpdatedAt              string  `json:"updated_at"`
}

type triggerDeferralView struct {
	Key           string `json:"key"`
	WorkspaceID   string `json:"workspace_id"`
	Reason        string `json:"reason"`
	DueAt         string `json:"due_at"`
	DeferredUntil string `json:"deferred_until"`
}

type triggerMaterializationView struct {
	ID                 int64  `json:"id"`
	TriggerID          int64  `json:"trigger_id"`
	MaterializationKey string `json:"materialization_key"`
	TaskID             int64  `json:"task_id"`
	Reason             string `json:"reason"`
	RequestedBy        string `json:"requested_by"`
	CreatedAt          string `json:"created_at"`
	UpdatedAt          string `json:"updated_at"`
}

type triggerWorkItemView struct {
	ID                    int64  `json:"id"`
	Key                   string `json:"key"`
	Title                 string `json:"title"`
	Status                string `json:"status"`
	Scope                 string `json:"scope"`
	RequestedBy           string `json:"requested_by"`
	WorkKind              string `json:"work_kind"`
	ExecutionIntent       string `json:"execution_intent,omitempty"`
	ExecutionIntentSource string `json:"execution_intent_source,omitempty"`
}

func newTriggerEvaluateView(result triggers.DueEvaluationResult) triggerEvaluateView {
	views := make([]triggerFireView, 0, len(result.Results))
	for _, item := range result.Results {
		views = append(views, newTriggerFireView(item))
	}
	deferrals := make([]triggerDeferralView, 0, len(result.Deferrals))
	for _, item := range result.Deferrals {
		deferrals = append(deferrals, newTriggerDeferralView(item))
	}
	return triggerEvaluateView{
		Evaluated:    result.Evaluated,
		Materialized: result.Materialized,
		Deferred:     result.Deferred,
		Errored:      result.Errored,
		Results:      views,
		Deferrals:    deferrals,
	}
}

func newTriggerDeferralView(result triggers.DeferredEvaluationResult) triggerDeferralView {
	return triggerDeferralView{
		Key:           result.Trigger.Key,
		WorkspaceID:   result.Trigger.WorkspaceID,
		Reason:        result.Reason,
		DueAt:         result.DueAt.UTC().Format(time.RFC3339),
		DeferredUntil: result.DeferredUntil.UTC().Format(time.RFC3339),
	}
}

func newTriggerFireView(result sqlite.FireAutomationTriggerResult) triggerFireView {
	return triggerFireView{
		Trigger:         newAutomationTriggerView(result.Trigger),
		Materialization: newTriggerMaterializationView(result.Materialization),
		WorkItem:        newTriggerWorkItemView(result.WorkItem),
		CreatedWorkItem: result.CreatedWorkItem,
	}
}

func newTriggerGitHubIssueIngestView(result triggers.GitHubIssueIngestResult) triggerGitHubIssueIngestView {
	return triggerGitHubIssueIngestView{
		Source:           result.Source,
		EventType:        result.EventType,
		ExternalEventKey: result.ExternalEventKey,
		ProjectKey:       result.ProjectKey,
		Provider:         result.Issue.Provider,
		Repo:             result.Issue.Repo,
		Number:           result.Issue.Number,
		Action:           result.Action,
		Title:            result.Issue.Title,
		URL:              result.Issue.URL,
		ExternalIssueID:  result.Issue.ID,
		SyncStatus:       result.Issue.SyncStatus,
	}
}

func newTriggerPreviewView(result triggers.PreviewResult) triggerPreviewView {
	decisions := make([]triggerPreviewDecisionView, 0, len(result.Decisions))
	for _, decision := range result.Decisions {
		decisions = append(decisions, newTriggerPreviewDecisionView(decision))
	}
	return triggerPreviewView{
		Now:              result.Now.UTC().Format(time.RFC3339),
		DryRun:           true,
		Mutates:          false,
		Evaluated:        result.Evaluated,
		WouldRun:         result.WouldRun,
		WouldDefer:       result.WouldDefer,
		WouldBatch:       result.WouldBatch,
		ApprovalRequired: result.ApprovalRequired,
		Errored:          result.Errored,
		Decisions:        decisions,
	}
}

func newTriggerPreviewDecisionView(decision triggers.PreviewDecision) triggerPreviewDecisionView {
	details := triggerOperatorDetails(decision.Trigger, time.Now().UTC())
	quietEffect := defaultTriggerOutput(decision.QuietHourEffect, details.QuietHourEffect)
	matches := make([]triggerPreviewEventMatchView, 0, len(decision.MatchedEvents))
	for _, match := range decision.MatchedEvents {
		matches = append(matches, triggerPreviewEventMatchView{
			ID:         match.ID,
			EventType:  match.EventType,
			OccurredAt: match.OccurredAt.UTC().Format(time.RFC3339),
			Reason:     match.Reason,
		})
	}
	return triggerPreviewDecisionView{
		Key:                decision.Trigger.Key,
		Decision:           decision.Decision,
		Reason:             decision.Reason,
		MaterializationKey: decision.MaterializationKey,
		EventEnvelope:      decision.EventEnvelope,
		TriggerType:        decision.Trigger.Kind,
		Schedule:           details.Schedule,
		EventType:          decision.EventType,
		DueAt:              formatOptionalTimePointer(decision.DueAt),
		NextRun:            formatOptionalTimePointer(decision.NextEligibleAt),
		LastRun:            formatOptionalTimePointer(decision.Trigger.LastMaterializedAt),
		QuietHours:         defaultTriggerOutput(decision.QuietHours, details.QuietHours),
		QuietHourEffect:    quietEffect,
		BatchKey:           decision.BatchKey,
		BatchWindow:        decision.BatchWindow,
		BatchGroup:         decision.BatchGroup,
		CandidateEvents:    decision.CandidateEvents,
		MatchedEvents:      matches,
		ApprovalRequired:   decision.ApprovalRequired,
		RecoveryState:      decision.RecoveryState,
		Mutates:            false,
		Error:              decision.Error,
	}
}

func newTriggerAuditView(key string, events []triggers.AuditEvent) triggerAuditView {
	views := make([]triggerAuditEventView, 0, len(events))
	for _, event := range events {
		views = append(views, triggerAuditEventView{
			ID:         event.ID,
			EventType:  event.EventType,
			OccurredAt: event.OccurredAt.UTC().Format(time.RFC3339),
			Payload:    event.Payload,
		})
	}
	return triggerAuditView{TriggerKey: key, AuditEvents: views}
}

func newAutomationTriggerView(item sqlite.AutomationTrigger) automationTriggerView {
	return automationTriggerView{
		ID:                     item.ID,
		Key:                    item.Key,
		WorkspaceID:            item.WorkspaceID,
		InitiativeKey:          item.InitiativeKey,
		Kind:                   item.Kind,
		Status:                 item.Status,
		Readiness:              triggerReadiness(item),
		TimingStatus:           triggerTimingStatus(item, time.Now().UTC()),
		RuleSummary:            item.RuleSummary,
		RuleJSON:               item.RuleJSON,
		WorkItemTitle:          item.WorkItemTitle,
		NextEligibleAt:         formatOptionalTimePointer(item.NextEligibleAt),
		LastEvaluatedAt:        formatOptionalTimePointer(item.LastEvaluatedAt),
		LastMaterializedAt:     formatOptionalTimePointer(item.LastMaterializedAt),
		LastMaterializationKey: item.LastMaterializationKey,
		LastWorkItemID:         item.LastWorkItemID,
		LastWorkItemKey:        item.LastWorkItemKey,
		CreatedAt:              item.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:              item.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func newTriggerMaterializationView(item sqlite.AutomationTriggerMaterialization) triggerMaterializationView {
	return triggerMaterializationView{
		ID:                 item.ID,
		TriggerID:          item.TriggerID,
		MaterializationKey: item.MaterializationKey,
		TaskID:             item.TaskID,
		Reason:             item.Reason,
		RequestedBy:        item.RequestedBy,
		CreatedAt:          item.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:          item.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func newTriggerWorkItemView(item sqlite.Task) triggerWorkItemView {
	return triggerWorkItemView{
		ID:                    item.ID,
		Key:                   item.Key,
		Title:                 item.Title,
		Status:                item.Status,
		Scope:                 item.Scope,
		RequestedBy:           item.RequestedBy,
		WorkKind:              item.WorkKind,
		ExecutionIntent:       item.ExecutionIntent,
		ExecutionIntentSource: item.ExecutionIntentSource,
	}
}

func consumeTriggerJSONFlag(args []string) (bool, []string, error) {
	filtered := make([]string, 0, len(args))
	var jsonOutput bool
	for _, arg := range args {
		if arg == "--json" {
			jsonOutput = true
			continue
		}
		if strings.HasPrefix(arg, "--json=") {
			return false, nil, fmt.Errorf("invalid option: %s", arg)
		}
		filtered = append(filtered, arg)
	}
	if len(filtered) == 0 {
		return jsonOutput, filtered, fmt.Errorf("usage: odin %s", TriggerUsage)
	}
	return jsonOutput, filtered, nil
}

func parseOptionTokens(args []string) (map[string]string, error) {
	options := map[string]string{}
	for _, arg := range args {
		key, value, ok := strings.Cut(arg, "=")
		if !ok || strings.TrimSpace(key) == "" {
			return nil, fmt.Errorf("invalid option: %s", arg)
		}
		options[strings.ToLower(strings.TrimSpace(key))] = strings.TrimSpace(value)
	}
	return options, nil
}

func triggerFirstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func triggerEvaluateUsesEvents(options map[string]string) bool {
	for _, key := range []string{"source", "mode"} {
		value := strings.ToLower(strings.TrimSpace(options[key]))
		if value == "event" || value == "events" || value == "internal_events" {
			return true
		}
	}
	return strings.EqualFold(strings.TrimSpace(options["events"]), "true")
}

func triggerOperatorDetails(item sqlite.AutomationTrigger, now time.Time) triggerDetailsView {
	var rule struct {
		Summary         string `json:"summary"`
		Cadence         string `json:"cadence"`
		Cron            string `json:"cron"`
		QuietHours      string `json:"quiet_hours"`
		QuietTimezone   string `json:"quiet_timezone"`
		BatchKey        string `json:"batch_key"`
		BatchWindow     string `json:"batch_window"`
		EventType       string `json:"event_type"`
		ExecutionIntent string `json:"execution_intent"`
	}
	_ = json.Unmarshal([]byte(item.RuleJSON), &rule)
	details := triggerDetailsView{
		Schedule:        "manual",
		QuietHourEffect: "none",
		RecoveryState:   "not_started",
	}
	switch {
	case strings.TrimSpace(rule.Cron) != "":
		details.Schedule = "cron:" + strings.TrimSpace(rule.Cron)
	case strings.TrimSpace(rule.Cadence) != "":
		details.Schedule = "cadence:" + strings.TrimSpace(rule.Cadence)
	case strings.EqualFold(item.Kind, "event") && strings.TrimSpace(rule.EventType) != "":
		details.Schedule = "event:" + strings.TrimSpace(rule.EventType)
	}
	details.QuietHours = strings.TrimSpace(rule.QuietHours)
	if details.QuietHours != "" {
		details.QuietHourEffect = "pending"
	}
	if triggerTimingStatus(item, now) == "deferred" {
		details.QuietHourEffect = "deferred"
	}
	details.BatchKey = strings.TrimSpace(rule.BatchKey)
	details.BatchWindow = strings.TrimSpace(rule.BatchWindow)
	details.ApprovalRequired = triggerIntentNeedsApproval(rule.ExecutionIntent)
	return details
}

func triggerIntentNeedsApproval(intent string) bool {
	switch strings.ToLower(strings.TrimSpace(intent)) {
	case "governance", "destructive":
		return true
	default:
		return false
	}
}

func formatBatchState(key string, window string) string {
	key = strings.TrimSpace(key)
	window = strings.TrimSpace(window)
	if key == "" {
		return "none"
	}
	if window == "" {
		return key
	}
	return key + " window=" + window
}

func defaultTriggerOutput(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func triggerReadiness(item sqlite.AutomationTrigger) string {
	status := triggerTimingStatus(item, time.Now().UTC())
	if status == "deferred" {
		return "waiting"
	}
	return status
}

func triggerTimingStatus(item sqlite.AutomationTrigger, now time.Time) string {
	if item.Status != "enabled" {
		return item.Status
	}
	if item.NextEligibleAt != nil && item.NextEligibleAt.After(now.UTC()) {
		if item.LastEvaluatedAt != nil && (item.LastMaterializedAt == nil || item.LastEvaluatedAt.After(*item.LastMaterializedAt)) {
			return "deferred"
		}
		return "waiting"
	}
	return "ready"
}

func formatOptionalTime(value *time.Time) string {
	if value == nil {
		return "none"
	}
	return value.UTC().Format(time.RFC3339)
}

func formatOptionalTimePointer(value *time.Time) *string {
	if value == nil {
		return nil
	}
	formatted := value.UTC().Format(time.RFC3339)
	return &formatted
}

func parseTriggerNextEligibleAt(value string) (*time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" || strings.EqualFold(value, "none") {
		return nil, nil
	}
	if strings.EqualFold(value, "now") {
		now := time.Now().UTC()
		return &now, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return nil, fmt.Errorf("invalid trigger next value %q: use now, none, or RFC3339", value)
	}
	parsed = parsed.UTC()
	return &parsed, nil
}

func parseTriggerEvaluateAt(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" || strings.EqualFold(value, "now") {
		return time.Now().UTC(), nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid trigger evaluate now value %q: use now or RFC3339", value)
	}
	return parsed.UTC(), nil
}

func noneIfEmpty(value string) string {
	if strings.TrimSpace(value) == "" {
		return "none"
	}
	return value
}
