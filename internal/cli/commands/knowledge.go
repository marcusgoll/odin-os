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

const KnowledgeUsage = "knowledge search query=<text> [project=<key>] [limit=<n>] [--json] | knowledge context-pack task=<id|key> [project=<key>] [limit=<n>] [--propose] [--json] | knowledge context-pack show <id> [--json] | knowledge context-packs [status=<status>] [--json]"

func RunKnowledge(ctx context.Context, store *sqlite.Store, args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: odin %s", KnowledgeUsage)
	}
	if args[0] == "--help" || args[0] == "help" {
		_, err := fmt.Fprintf(stdout, "usage: odin %s\n\nRead-only retrieval over Odin-owned runtime state. These commands do not write memory, tasks, jobs, runs, or approvals.\n", KnowledgeUsage)
		return err
	}
	jsonOutput, propose, args, err := consumeKnowledgeFlags(args)
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
		if len(args) >= 2 && strings.EqualFold(args[1], "show") {
			if propose {
				return fmt.Errorf("--propose is not valid with knowledge context-pack show")
			}
			if len(args) != 3 {
				return fmt.Errorf("usage: odin %s", KnowledgeUsage)
			}
			packetID, err := strconv.ParseInt(strings.TrimSpace(args[2]), 10, 64)
			if err != nil || packetID <= 0 {
				return fmt.Errorf("context pack id must be a positive integer")
			}
			proposal, err := service.GetContextPackProposal(ctx, packetID)
			if err != nil {
				return err
			}
			if jsonOutput {
				return WriteJSON(stdout, newKnowledgeContextPackProposalView(proposal))
			}
			_, err = fmt.Fprintf(stdout, "context_pack id=%d status=%s task=%s persistence=%s\n", proposal.Packet.ID, proposal.Packet.Status, proposal.ContextPack.ObjectKey, proposal.Persistence)
			return err
		}
		options, err := parseOptionTokens(args[1:])
		if err != nil {
			return err
		}
		limit, err := parseKnowledgeLimit(options["limit"])
		if err != nil {
			return err
		}
		params := runtimeknowledge.ContextPackParams{
			TaskRef:    firstKnowledgeValue(options["task"], options["task_key"], options["id"]),
			ProjectKey: options["project"],
			Limit:      limit,
		}
		if propose {
			proposal, err := service.ProposeContextPack(ctx, params)
			if err != nil {
				return err
			}
			if jsonOutput {
				return WriteJSON(stdout, newKnowledgeContextPackProposalView(proposal))
			}
			_, err = fmt.Fprintf(stdout, "context_pack_proposal id=%d status=%s task=%s persistence=%s\n", proposal.Packet.ID, proposal.Packet.Status, proposal.ContextPack.ObjectKey, proposal.Persistence)
			return err
		}
		result, err := service.BuildContextPack(ctx, params)
		if err != nil {
			return err
		}
		if jsonOutput {
			return WriteJSON(stdout, newKnowledgeContextPackView(result))
		}
		_, err = fmt.Fprintf(stdout, "context_pack object=task key=%s events=%d runs=%d read_only=true persistence=none\n", result.ObjectKey, len(result.Events), len(result.Runs))
		return err
	case "context-packs":
		if propose {
			return fmt.Errorf("--propose is only valid with knowledge context-pack task=<id|key>")
		}
		options, err := parseOptionTokens(args[1:])
		if err != nil {
			return err
		}
		proposals, err := service.ListContextPackProposals(ctx, options["status"])
		if err != nil {
			return err
		}
		if jsonOutput {
			return WriteJSON(stdout, knowledgeContextPackProposalListView{Items: newKnowledgeContextPackProposalViews(proposals)})
		}
		if len(proposals) == 0 {
			_, err := fmt.Fprintln(stdout, "no context packs")
			return err
		}
		for _, proposal := range proposals {
			if _, err := fmt.Fprintf(stdout, "context_pack id=%d status=%s task=%s actions=%s\n", proposal.Packet.ID, proposal.Packet.Status, proposal.ContextPack.ObjectKey, strings.Join(proposal.AllowedActions, ",")); err != nil {
				return err
			}
		}
		return nil
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

type knowledgeContextPackProposalListView struct {
	Items []knowledgeContextPackProposalView `json:"items"`
}

type knowledgeContextPackProposalView struct {
	Proposed       bool                               `json:"proposed"`
	ReadOnly       bool                               `json:"read_only"`
	Persistence    string                             `json:"persistence"`
	ReviewDecision string                             `json:"review_decision,omitempty"`
	Proposal       knowledgeContextPacketView         `json:"proposal"`
	ContextPack    knowledgeContextPackView           `json:"context_pack"`
	Review         runtimeknowledge.ContextPackReview `json:"review"`
	AllowedActions []string                           `json:"allowed_actions"`
}

type knowledgeContextPacketView struct {
	ID          int64  `json:"id"`
	TaskID      *int64 `json:"task_id,omitempty"`
	RunID       *int64 `json:"run_id,omitempty"`
	PacketKind  string `json:"packet_kind"`
	PacketScope string `json:"packet_scope"`
	Trigger     string `json:"trigger"`
	Status      string `json:"status"`
	Summary     string `json:"summary"`
	CreatedAt   string `json:"created_at"`
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

func newKnowledgeContextPackProposalViews(proposals []runtimeknowledge.ContextPackProposal) []knowledgeContextPackProposalView {
	views := make([]knowledgeContextPackProposalView, 0, len(proposals))
	for _, proposal := range proposals {
		views = append(views, newKnowledgeContextPackProposalView(proposal))
	}
	return views
}

func NewKnowledgeContextPackProposalView(proposal runtimeknowledge.ContextPackProposal) any {
	return newKnowledgeContextPackProposalView(proposal)
}

func newKnowledgeContextPackProposalView(proposal runtimeknowledge.ContextPackProposal) knowledgeContextPackProposalView {
	return knowledgeContextPackProposalView{
		Proposed:       proposal.Proposed,
		ReadOnly:       false,
		Persistence:    proposal.Persistence,
		ReviewDecision: proposal.Review.Decision,
		Proposal: knowledgeContextPacketView{
			ID:          proposal.Packet.ID,
			TaskID:      proposal.Packet.TaskID,
			RunID:       proposal.Packet.RunID,
			PacketKind:  proposal.Packet.PacketKind,
			PacketScope: proposal.Packet.PacketScope,
			Trigger:     proposal.Packet.Trigger,
			Status:      proposal.Packet.Status,
			Summary:     proposal.Packet.Summary,
			CreatedAt:   proposal.Packet.CreatedAt.UTC().Format(time.RFC3339),
		},
		ContextPack:    newKnowledgeContextPackView(proposal.ContextPack),
		Review:         proposal.Review,
		AllowedActions: append([]string(nil), proposal.AllowedActions...),
	}
}

func consumeKnowledgeFlags(args []string) (bool, bool, []string, error) {
	filtered := make([]string, 0, len(args))
	var jsonOutput bool
	var propose bool
	for _, arg := range args {
		if arg == "--json" {
			jsonOutput = true
			continue
		}
		if strings.HasPrefix(arg, "--json=") {
			return false, false, nil, fmt.Errorf("invalid option: %s", arg)
		}
		if arg == "--propose" {
			propose = true
			continue
		}
		if strings.HasPrefix(arg, "--propose=") {
			return false, false, nil, fmt.Errorf("invalid option: %s", arg)
		}
		filtered = append(filtered, arg)
	}
	if len(filtered) == 0 {
		return jsonOutput, propose, filtered, fmt.Errorf("usage: odin %s", KnowledgeUsage)
	}
	return jsonOutput, propose, filtered, nil
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
