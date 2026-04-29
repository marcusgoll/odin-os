package commands

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"odin-os/internal/runtime/triggers"
	"odin-os/internal/store/sqlite"
)

const TriggerUsage = "trigger [list|show <key>|upsert <key>|fire <key>|evaluate] [key=value ...]"

func RunTrigger(ctx context.Context, service triggers.Service, args []string, stdout io.Writer) error {
	if len(args) == 0 || args[0] == "list" {
		options, err := parseOptionTokens(args[1:])
		if err != nil {
			return err
		}
		return runTriggerList(ctx, service, options["workspace"], stdout)
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
		return runTriggerShow(ctx, service, options["workspace"], args[1], stdout)
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
			WorkspaceID:    options["workspace"],
			Key:            args[1],
			InitiativeKey:  options["initiative"],
			Kind:           options["kind"],
			Status:         options["status"],
			RuleSummary:    triggerFirstNonEmpty(options["rule"], options["summary"]),
			RuleJSON:       options["rule_json"],
			WorkItemTitle:  strings.ReplaceAll(options["title"], "_", " "),
			NextEligibleAt: nextEligibleAt,
			Cadence:        options["cadence"],
			Cron:           strings.ReplaceAll(options["cron"], "_", " "),
		})
		if err != nil {
			return err
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
		evaluateAt, err := parseTriggerEvaluateAt(options["now"])
		if err != nil {
			return err
		}
		result, err := service.EvaluateDue(ctx, evaluateAt)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(stdout, "automation_trigger_evaluation evaluated=%d materialized=%d errored=%d\n",
			result.Evaluated,
			result.Materialized,
			result.Errored,
		)
		return err
	default:
		return fmt.Errorf("unknown trigger command: %s", args[0])
	}
}

func runTriggerList(ctx context.Context, service triggers.Service, workspaceID string, stdout io.Writer) error {
	items, err := service.List(ctx, workspaceID)
	if err != nil {
		return err
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

func runTriggerShow(ctx context.Context, service triggers.Service, workspaceID string, key string, stdout io.Writer) error {
	item, err := service.Show(ctx, workspaceID, key)
	if err != nil {
		return err
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

func triggerReadiness(item sqlite.AutomationTrigger) string {
	if item.Status != "enabled" {
		return item.Status
	}
	if item.NextEligibleAt != nil && item.NextEligibleAt.After(time.Now().UTC()) {
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
