package commands

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	runtimeknowledge "odin-os/internal/runtime/knowledge"
	"odin-os/internal/store/sqlite"
)

const KnowledgeUsage = "knowledge search query=<text> [project=<key>] [limit=<n>] [--json] | knowledge context-pack task=<id|key> [project=<key>] [limit=<n>] [--json]"

func RunKnowledge(ctx context.Context, store *sqlite.Store, args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: odin %s", KnowledgeUsage)
	}
	if args[0] == "--help" || args[0] == "help" {
		_, err := fmt.Fprintf(stdout, "usage: odin %s\n\nRead-only retrieval over Odin-owned runtime state. These commands do not write memory, tasks, jobs, runs, or approvals.\n", KnowledgeUsage)
		return err
	}
	jsonOutput, args, err := consumeKnowledgeJSONFlag(args)
	if err != nil {
		return err
	}
	service := runtimeknowledge.Service{Store: store}
	switch strings.ToLower(args[0]) {
	case "search":
		options, err := parseOptionTokens(args[1:])
		if err != nil {
			return err
		}
		limit, err := parseKnowledgeLimit(options["limit"])
		if err != nil {
			return err
		}
		result, err := service.Search(ctx, runtimeknowledge.SearchParams{
			Query:      firstKnowledgeValue(options["query"], options["q"], options["contains"]),
			ProjectKey: options["project"],
			Limit:      limit,
		})
		if err != nil {
			return err
		}
		if jsonOutput {
			return WriteJSON(stdout, newKnowledgeSearchView(result))
		}
		_, err = fmt.Fprintf(stdout, "knowledge_search query=%q results=%d read_only=true persistence=none\n", result.Query, len(result.Results))
		return err
	case "context-pack":
		options, err := parseOptionTokens(args[1:])
		if err != nil {
			return err
		}
		limit, err := parseKnowledgeLimit(options["limit"])
		if err != nil {
			return err
		}
		result, err := service.BuildContextPack(ctx, runtimeknowledge.ContextPackParams{
			TaskRef:    firstKnowledgeValue(options["task"], options["task_key"], options["id"]),
			ProjectKey: options["project"],
			Limit:      limit,
		})
		if err != nil {
			return err
		}
		if jsonOutput {
			return WriteJSON(stdout, newKnowledgeContextPackView(result))
		}
		_, err = fmt.Fprintf(stdout, "context_pack object=task key=%s events=%d runs=%d read_only=true persistence=none\n", result.ObjectKey, len(result.Events), len(result.Runs))
		return err
	default:
		return fmt.Errorf("unknown knowledge command: %s", args[0])
	}
}

type knowledgeSearchView struct {
	Query       string                `json:"query"`
	ProjectKey  string                `json:"project_key,omitempty"`
	ReadOnly    bool                  `json:"read_only"`
	Persistence string                `json:"persistence"`
	Results     []knowledgeResultView `json:"results"`
}

type knowledgeResultView struct {
	Kind       string `json:"kind"`
	ID         int64  `json:"id"`
	Key        string `json:"key"`
	ProjectKey string `json:"project_key,omitempty"`
	Title      string `json:"title"`
	Status     string `json:"status,omitempty"`
	Summary    string `json:"summary,omitempty"`
	OccurredAt string `json:"occurred_at,omitempty"`
	Source     string `json:"source"`
}

type knowledgeContextPackView struct {
	ObjectType   string                         `json:"object_type"`
	ObjectID     int64                          `json:"object_id"`
	ObjectKey    string                         `json:"object_key"`
	ProjectKey   string                         `json:"project_key,omitempty"`
	ReadOnly     bool                           `json:"read_only"`
	Persistence  string                         `json:"persistence"`
	Task         runtimeknowledge.TaskContext   `json:"task"`
	Runs         []runtimeknowledge.RunContext  `json:"runs"`
	Events       []knowledgeEventView           `json:"events"`
	ContextItems []runtimeknowledge.ContextItem `json:"context_items"`
}

type knowledgeEventView struct {
	ID         int64  `json:"id"`
	Type       string `json:"type"`
	Scope      string `json:"scope"`
	Payload    any    `json:"payload"`
	OccurredAt string `json:"occurred_at"`
}

func newKnowledgeSearchView(result runtimeknowledge.SearchResponse) knowledgeSearchView {
	views := make([]knowledgeResultView, 0, len(result.Results))
	for _, item := range result.Results {
		occurredAt := ""
		if !item.OccurredAt.IsZero() {
			occurredAt = item.OccurredAt.UTC().Format(time.RFC3339)
		}
		views = append(views, knowledgeResultView{
			Kind:       item.Kind,
			ID:         item.ID,
			Key:        item.Key,
			ProjectKey: item.ProjectKey,
			Title:      item.Title,
			Status:     item.Status,
			Summary:    item.Summary,
			OccurredAt: occurredAt,
			Source:     item.Source,
		})
	}
	return knowledgeSearchView{
		Query:       result.Query,
		ProjectKey:  result.ProjectKey,
		ReadOnly:    result.ReadOnly,
		Persistence: result.Persistence,
		Results:     views,
	}
}

func newKnowledgeContextPackView(result runtimeknowledge.ContextPack) knowledgeContextPackView {
	events := make([]knowledgeEventView, 0, len(result.Events))
	for _, event := range result.Events {
		events = append(events, knowledgeEventView{
			ID:         event.ID,
			Type:       event.Type,
			Scope:      event.Scope,
			Payload:    event.Payload,
			OccurredAt: event.OccurredAt.UTC().Format(time.RFC3339),
		})
	}
	return knowledgeContextPackView{
		ObjectType:   result.ObjectType,
		ObjectID:     result.ObjectID,
		ObjectKey:    result.ObjectKey,
		ProjectKey:   result.ProjectKey,
		ReadOnly:     result.ReadOnly,
		Persistence:  result.Persistence,
		Task:         result.Task,
		Runs:         result.Runs,
		Events:       events,
		ContextItems: result.ContextItems,
	}
}

func consumeKnowledgeJSONFlag(args []string) (bool, []string, error) {
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
		return jsonOutput, filtered, fmt.Errorf("usage: odin %s", KnowledgeUsage)
	}
	return jsonOutput, filtered, nil
}

func parseKnowledgeLimit(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 20, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 {
		return 0, fmt.Errorf("knowledge limit must be a positive integer")
	}
	return limit, nil
}

func firstKnowledgeValue(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
