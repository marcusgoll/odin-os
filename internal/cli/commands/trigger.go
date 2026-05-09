package commands

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"odin-os/internal/runtime/triggers"
	"odin-os/internal/store/sqlite"
)

const TriggerUsage = "trigger [list|show <key>|upsert <key>|fire <key>|evaluate|ingest github-issue] [key=value ...] [--json]"

func RunTrigger(ctx context.Context, service triggers.Service, args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: odin %s", TriggerUsage)
	}
	if args[0] == "--help" || args[0] == "help" {
		_, err := fmt.Fprintf(stdout, "usage: odin %s\n\nScheduled triggers:\n  odin trigger upsert <key> initiative=<project> kind=schedule status=enabled next=<RFC3339> [cadence=<duration>] [cron=<expr>] [quiet=<HH:MM-HH:MM>] [batch=<key> batch_window=<duration>] [title=<text>] [summary=<text>] [intent=<read_only|mutation|governance|destructive>] [--json]\n  odin trigger evaluate now=<RFC3339> [--json]\n\nManual trigger fire:\n  odin trigger fire <key> [reason=<reason>] [--json]\n\nEvent triggers:\n  odin trigger upsert <key> initiative=<project> kind=event event=<event_type> [match_status=<status>] [match_previous_status=<status>] [match_task_id=<id>] [match_scope=<scope>] [match_provider=<provider>] [match_repo=<owner/repo>] [intent=<read_only|mutation|governance|destructive>] [--json]\n  odin trigger evaluate source=events [--json]\n\nExternal event ingest:\n  odin trigger ingest github-issue project=<project> repo=<owner/repo> number=<n> action=<opened> title=<text> [body=<text>] [url=<url>] [labels=a,b] [--json]\n", TriggerUsage)
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
	case "show":
		if len(args) < 2 {
			return fmt.Errorf("usage: odin %s", TriggerUsage)
		}
		options, err := parseOptionTokens(args[2:])
		if err != nil {
			return err
		}
		return runTriggerShow(ctx, service, options["workspace"], args[1], stdout, jsonOutput)
	case "upsert":
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
	case "fire":
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
	_, err = fmt.Fprintf(stdout, "trigger=%s workspace=%s initiative=%s kind=%s status=%s readiness=%s rule_summary=%q last_materialization=%s last_work_item=%s next_eligible=%s\n",
		item.Key,
		item.WorkspaceID,
		item.InitiativeKey,
		item.Kind,
		item.Status,
		triggerReadiness(item),
		item.RuleSummary,
		noneIfEmpty(item.LastMaterializationKey),
		noneIfEmpty(item.LastWorkItemKey),
		formatOptionalTime(item.NextEligibleAt),
	)
	return err
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
