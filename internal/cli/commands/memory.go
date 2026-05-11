package commands

import (
	"context"
	"fmt"
	"io"
	"strings"

	"odin-os/internal/runtime/memoryproposal"
	"odin-os/internal/store/sqlite"
)

const OdinMemoryUsage = "memory propose scope=<scope> type=<type> source_type=<type> source_id=<id>|source_key=<key> sensitivity=<level> --summary <text> [project=<key>] [--json] | memory list [scope=<scope>] [project=<key>] [type=<type>] [status=<status>] [--json] | memory show <id|memory-proposal:<id>> [--json] | memory resolve <id|memory-proposal:<id>> <accept|reject|archive> because <reason...> [--json]"

type MemoryListView struct {
	Items []memoryproposal.Proposal `json:"items"`
}

func RunMemory(ctx context.Context, store *sqlite.Store, args []string, stdout io.Writer) error {
	jsonOutput, args, err := consumeMemoryJSONFlag(args)
	if err != nil {
		return err
	}
	if len(args) == 0 || strings.EqualFold(args[0], "help") || strings.EqualFold(args[0], "--help") {
		_, err := fmt.Fprintf(stdout, "usage: odin %s\n\nProposal-gated durable memory. Pending proposals are not active recall material until accepted.\n", OdinMemoryUsage)
		return err
	}
	service := memoryproposal.Service{Store: store}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "propose":
		options, summary, err := parseMemoryProposeArgs(args[1:])
		if err != nil {
			return err
		}
		proposal, err := service.Propose(ctx, memoryproposal.ProposeParams{
			Scope:       options["scope"],
			ProjectKey:  options["project"],
			MemoryType:  firstMemoryValue(options["type"], options["memory_type"]),
			Summary:     summary,
			SourceType:  options["source_type"],
			SourceID:    options["source_id"],
			SourceKey:   options["source_key"],
			SourceURL:   options["source_url"],
			Sensitivity: options["sensitivity"],
			CreatedBy:   "operator",
		})
		if err != nil {
			return err
		}
		if jsonOutput {
			return WriteJSON(stdout, proposal)
		}
		_, err = fmt.Fprintf(stdout, "memory=%d queue=%s status=%s active=%t\n", proposal.ID, proposal.QueueID, proposal.Status, proposal.Active)
		return err
	case "list":
		options, err := parseOptionTokens(args[1:])
		if err != nil {
			return err
		}
		proposals, err := service.List(ctx, memoryproposal.ListParams{
			Scope:      options["scope"],
			ProjectKey: options["project"],
			MemoryType: firstMemoryValue(options["type"], options["memory_type"]),
			Status:     options["status"],
		})
		if err != nil {
			return err
		}
		if jsonOutput {
			return WriteJSON(stdout, MemoryListView{Items: proposals})
		}
		if len(proposals) == 0 {
			_, err := fmt.Fprintln(stdout, "no memory")
			return err
		}
		for _, proposal := range proposals {
			if _, err := fmt.Fprintf(stdout, "memory=%d queue=%s status=%s active=%t summary=%s\n", proposal.ID, proposal.QueueID, proposal.Status, proposal.Active, proposal.Summary); err != nil {
				return err
			}
		}
		return nil
	case "show":
		if len(args) != 2 {
			return fmt.Errorf("usage: odin %s", OdinMemoryUsage)
		}
		id, err := memoryproposal.ParseRef(args[1])
		if err != nil {
			return err
		}
		proposal, err := service.Get(ctx, id)
		if err != nil {
			return err
		}
		if jsonOutput {
			return WriteJSON(stdout, proposal)
		}
		_, err = fmt.Fprintf(stdout, "memory=%d queue=%s status=%s active=%t summary=%s\n", proposal.ID, proposal.QueueID, proposal.Status, proposal.Active, proposal.Summary)
		return err
	case "resolve":
		ref, action, reason, err := parseMemoryResolveArgs(args[1:])
		if err != nil {
			return err
		}
		id, err := memoryproposal.ParseRef(ref)
		if err != nil {
			return err
		}
		proposal, repeated, err := service.Resolve(ctx, memoryproposal.ResolveParams{
			ID:         id,
			Decision:   action,
			ReviewedBy: "operator",
			Reason:     reason,
		})
		if err != nil {
			return err
		}
		view := struct {
			Decision string                  `json:"decision"`
			Status   string                  `json:"status"`
			Repeated bool                    `json:"repeated"`
			Memory   memoryproposal.Proposal `json:"memory"`
		}{
			Decision: action,
			Status:   proposal.Status,
			Repeated: repeated,
			Memory:   proposal,
		}
		if jsonOutput {
			return WriteJSON(stdout, view)
		}
		_, err = fmt.Fprintf(stdout, "memory=%d decision=%s status=%s repeated=%t\n", proposal.ID, action, proposal.Status, repeated)
		return err
	default:
		return fmt.Errorf("unknown memory command: %s", args[0])
	}
}

func consumeMemoryJSONFlag(args []string) (bool, []string, error) {
	filtered := make([]string, 0, len(args))
	jsonOutput := false
	for _, arg := range args {
		if arg == "--json" {
			if jsonOutput {
				return false, nil, fmt.Errorf("duplicate --json flag")
			}
			jsonOutput = true
			continue
		}
		if strings.HasPrefix(arg, "--json=") {
			return false, nil, fmt.Errorf("invalid option: %s", arg)
		}
		filtered = append(filtered, arg)
	}
	return jsonOutput, filtered, nil
}

func parseMemoryProposeArgs(args []string) (map[string]string, string, error) {
	summary := ""
	filtered := make([]string, 0, len(args))
	for index := 0; index < len(args); index++ {
		if args[index] != "--summary" {
			filtered = append(filtered, args[index])
			continue
		}
		if summary != "" || index+1 >= len(args) {
			return nil, "", fmt.Errorf("usage: odin %s", OdinMemoryUsage)
		}
		summary = args[index+1]
		index++
	}
	options, err := parseOptionTokens(filtered)
	return options, summary, err
}

func parseMemoryResolveArgs(args []string) (string, string, string, error) {
	if len(args) < 4 {
		return "", "", "", fmt.Errorf("usage: odin %s", OdinMemoryUsage)
	}
	ref := args[0]
	action := strings.ToLower(strings.TrimSpace(args[1]))
	if !strings.EqualFold(args[2], "because") {
		return "", "", "", fmt.Errorf("memory resolve requires because <reason...>")
	}
	reason := strings.TrimSpace(strings.Join(args[3:], " "))
	if reason == "" {
		return "", "", "", fmt.Errorf("memory resolve requires because <reason...>")
	}
	return ref, action, reason, nil
}

func firstMemoryValue(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
