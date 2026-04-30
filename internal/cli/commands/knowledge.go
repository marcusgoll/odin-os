package commands

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"odin-os/internal/memory/knowledge"
	"odin-os/internal/store/sqlite"
)

const knowledgeUsage = "usage: odin knowledge ingest|list|show|search|refresh|approve-use"

type KnowledgeCommand struct {
	Action        string
	Path          string
	Key           string
	Title         string
	Scope         string
	ScopeKey      string
	SourceKind    string
	SourceClass   string
	Restricted    bool
	RestrictedSet bool
	Lifecycle     string
	Query         string
	Limit         int
	UseType       string
	Reason        string
	DecidedBy     string
	Decision      string
	EvidenceJSON  string
}

func ParseKnowledge(args []string) (KnowledgeCommand, error) {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" {
		return KnowledgeCommand{Action: "help"}, nil
	}

	switch args[0] {
	case "ingest":
		return parseKnowledgeIngest(args[1:])
	case "list":
		return parseKnowledgeList(args[1:])
	case "show":
		return parseKnowledgeKeyCommand("show", args[1:])
	case "search":
		return parseKnowledgeSearch(args[1:])
	case "refresh":
		return parseKnowledgeKeyCommand("refresh", args[1:])
	case "approve-use":
		return parseKnowledgeApproveUse(args[1:])
	default:
		return KnowledgeCommand{}, fmt.Errorf("unknown knowledge command: %s", args[0])
	}
}

func RunKnowledge(ctx context.Context, store *sqlite.Store, repoRoot string, runtimeRoot string, args []string, stdout io.Writer) error {
	if store == nil {
		return fmt.Errorf("knowledge store is required")
	}

	command, err := ParseKnowledge(args)
	if err != nil {
		return err
	}
	if command.Action == "help" {
		_, err := fmt.Fprintln(stdout, knowledgeUsage)
		return err
	}

	service := knowledge.Service{
		Store:       store,
		RepoRoot:    repoRoot,
		RuntimeRoot: runtimeRoot,
	}

	switch command.Action {
	case "ingest":
		result, err := service.Ingest(ctx, knowledge.IngestParams{
			Path:        command.Path,
			Key:         command.Key,
			Title:       command.Title,
			Scope:       command.Scope,
			ScopeKey:    command.ScopeKey,
			Restricted:  command.Restricted,
			SourceKind:  command.SourceKind,
			SourceClass: knowledge.SourceClass(command.SourceClass),
		})
		if err != nil {
			return err
		}
		return renderKnowledgeIngest(stdout, result)
	case "list":
		var restricted *bool
		if command.RestrictedSet {
			restricted = &command.Restricted
		}
		results, err := service.List(ctx, knowledge.ListParams{
			Scope:      command.Scope,
			ScopeKey:   command.ScopeKey,
			Lifecycle:  knowledge.Lifecycle(command.Lifecycle),
			Restricted: restricted,
		})
		if err != nil {
			return err
		}
		return renderKnowledgeList(stdout, results)
	case "show":
		result, err := service.Show(ctx, command.Key)
		if err != nil {
			return err
		}
		return renderKnowledgeSource(stdout, result.Source)
	case "search":
		results, err := service.Search(ctx, knowledge.SearchParams{
			Query:    command.Query,
			Scope:    command.Scope,
			ScopeKey: command.ScopeKey,
			Limit:    command.Limit,
		})
		if err != nil {
			return err
		}
		return renderKnowledgeSearch(stdout, results)
	case "refresh":
		result, err := service.Refresh(ctx, command.Key)
		if err != nil {
			return err
		}
		return renderKnowledgeRefresh(stdout, result)
	case "approve-use":
		source, err := service.Show(ctx, command.Key)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("knowledge source %q not found", command.Key)
			}
			return err
		}
		approval, err := store.RecordRestrictedKnowledgeUseApproval(ctx, sqlite.RecordRestrictedKnowledgeUseApprovalParams{
			SourceID:     source.Source.ID,
			UseType:      command.UseType,
			Reason:       command.Reason,
			Decision:     command.Decision,
			EvidenceJSON: command.EvidenceJSON,
			DecidedBy:    command.DecidedBy,
		})
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(stdout, "approval_id=%d source=%s use_type=%s decision=%s\n", approval.ID, source.Source.Key, approval.UseType, approval.Decision)
		return err
	default:
		return fmt.Errorf("unknown knowledge action: %s", command.Action)
	}
}

func parseKnowledgeIngest(args []string) (KnowledgeCommand, error) {
	cmd := KnowledgeCommand{
		Action:     "ingest",
		Scope:      "global",
		ScopeKey:   "global",
		SourceKind: "manual",
	}
	if len(args) == 0 || strings.HasPrefix(args[0], "--") {
		return KnowledgeCommand{}, fmt.Errorf("knowledge ingest requires a source path")
	}
	cmd.Path = args[0]
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--key":
			value, next, err := requireKnowledgeFlagValue(args, i)
			if err != nil {
				return KnowledgeCommand{}, err
			}
			cmd.Key = value
			i = next
		case "--title":
			value, next, err := requireKnowledgeFlagValue(args, i)
			if err != nil {
				return KnowledgeCommand{}, err
			}
			cmd.Title = value
			i = next
		case "--scope":
			value, next, err := requireKnowledgeFlagValue(args, i)
			if err != nil {
				return KnowledgeCommand{}, err
			}
			cmd.Scope = value
			i = next
		case "--scope-key":
			value, next, err := requireKnowledgeFlagValue(args, i)
			if err != nil {
				return KnowledgeCommand{}, err
			}
			cmd.ScopeKey = value
			i = next
		case "--kind":
			value, next, err := requireKnowledgeFlagValue(args, i)
			if err != nil {
				return KnowledgeCommand{}, err
			}
			cmd.SourceKind = value
			i = next
		case "--restricted":
			cmd.Restricted = true
			cmd.RestrictedSet = true
		case "--source-class":
			value, next, err := requireKnowledgeFlagValue(args, i)
			if err != nil {
				return KnowledgeCommand{}, err
			}
			cmd.SourceClass = value
			i = next
		default:
			return KnowledgeCommand{}, fmt.Errorf("unknown knowledge ingest flag: %s", args[i])
		}
	}
	if strings.TrimSpace(cmd.Key) == "" {
		return KnowledgeCommand{}, fmt.Errorf("knowledge ingest requires --key")
	}
	if strings.TrimSpace(cmd.Title) == "" {
		return KnowledgeCommand{}, fmt.Errorf("knowledge ingest requires --title")
	}
	return cmd, nil
}

func parseKnowledgeList(args []string) (KnowledgeCommand, error) {
	cmd := KnowledgeCommand{
		Action:   "list",
		Scope:    "global",
		ScopeKey: "global",
	}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--scope":
			value, next, err := requireKnowledgeFlagValue(args, i)
			if err != nil {
				return KnowledgeCommand{}, err
			}
			cmd.Scope = value
			i = next
		case "--scope-key":
			value, next, err := requireKnowledgeFlagValue(args, i)
			if err != nil {
				return KnowledgeCommand{}, err
			}
			cmd.ScopeKey = value
			i = next
		case "--lifecycle":
			value, next, err := requireKnowledgeFlagValue(args, i)
			if err != nil {
				return KnowledgeCommand{}, err
			}
			cmd.Lifecycle = value
			i = next
		case "--restricted":
			value, next, err := requireKnowledgeFlagValue(args, i)
			if err != nil {
				return KnowledgeCommand{}, err
			}
			restricted, err := strconv.ParseBool(value)
			if err != nil {
				return KnowledgeCommand{}, fmt.Errorf("--restricted requires true or false")
			}
			cmd.Restricted = restricted
			cmd.RestrictedSet = true
			i = next
		default:
			return KnowledgeCommand{}, fmt.Errorf("unknown knowledge list flag: %s", args[i])
		}
	}
	return cmd, nil
}

func parseKnowledgeKeyCommand(action string, args []string) (KnowledgeCommand, error) {
	if len(args) != 1 || strings.TrimSpace(args[0]) == "" {
		return KnowledgeCommand{}, fmt.Errorf("knowledge %s requires a source key", action)
	}
	return KnowledgeCommand{Action: action, Key: args[0]}, nil
}

func parseKnowledgeSearch(args []string) (KnowledgeCommand, error) {
	cmd := KnowledgeCommand{
		Action:   "search",
		Scope:    "global",
		ScopeKey: "global",
	}
	if len(args) == 0 || strings.HasPrefix(args[0], "--") {
		return KnowledgeCommand{}, fmt.Errorf("knowledge search requires a query")
	}
	cmd.Query = args[0]
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--scope":
			value, next, err := requireKnowledgeFlagValue(args, i)
			if err != nil {
				return KnowledgeCommand{}, err
			}
			cmd.Scope = value
			i = next
		case "--scope-key":
			value, next, err := requireKnowledgeFlagValue(args, i)
			if err != nil {
				return KnowledgeCommand{}, err
			}
			cmd.ScopeKey = value
			i = next
		case "--limit":
			value, next, err := requireKnowledgeFlagValue(args, i)
			if err != nil {
				return KnowledgeCommand{}, err
			}
			limit, err := strconv.Atoi(value)
			if err != nil || limit <= 0 {
				return KnowledgeCommand{}, fmt.Errorf("--limit requires a positive integer")
			}
			cmd.Limit = limit
			i = next
		default:
			return KnowledgeCommand{}, fmt.Errorf("unknown knowledge search flag: %s", args[i])
		}
	}
	return cmd, nil
}

func parseKnowledgeApproveUse(args []string) (KnowledgeCommand, error) {
	cmd := KnowledgeCommand{
		Action:       "approve-use",
		Decision:     "approved",
		DecidedBy:    "operator",
		EvidenceJSON: "{}",
	}
	if len(args) == 0 || strings.HasPrefix(args[0], "--") {
		return KnowledgeCommand{}, fmt.Errorf("knowledge approve-use requires a source key")
	}
	cmd.Key = args[0]
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--use-type":
			value, next, err := requireKnowledgeFlagValue(args, i)
			if err != nil {
				return KnowledgeCommand{}, err
			}
			cmd.UseType = value
			i = next
		case "--reason":
			value, next, err := requireKnowledgeFlagValue(args, i)
			if err != nil {
				return KnowledgeCommand{}, err
			}
			cmd.Reason = value
			i = next
		case "--decided-by":
			value, next, err := requireKnowledgeFlagValue(args, i)
			if err != nil {
				return KnowledgeCommand{}, err
			}
			cmd.DecidedBy = value
			i = next
		case "--decision":
			value, next, err := requireKnowledgeFlagValue(args, i)
			if err != nil {
				return KnowledgeCommand{}, err
			}
			switch value {
			case "approved", "rejected":
				cmd.Decision = value
			default:
				return KnowledgeCommand{}, fmt.Errorf("--decision must be approved or rejected")
			}
			i = next
		case "--evidence-json":
			value, next, err := requireKnowledgeFlagValue(args, i)
			if err != nil {
				return KnowledgeCommand{}, err
			}
			cmd.EvidenceJSON = value
			i = next
		default:
			return KnowledgeCommand{}, fmt.Errorf("unknown knowledge approve-use flag: %s", args[i])
		}
	}
	if strings.TrimSpace(cmd.UseType) == "" {
		return KnowledgeCommand{}, fmt.Errorf("knowledge approve-use requires --use-type")
	}
	if strings.TrimSpace(cmd.Reason) == "" {
		return KnowledgeCommand{}, fmt.Errorf("knowledge approve-use requires --reason")
	}
	if !json.Valid([]byte(cmd.EvidenceJSON)) {
		return KnowledgeCommand{}, fmt.Errorf("--evidence-json must be valid JSON")
	}
	return cmd, nil
}

func requireKnowledgeFlagValue(args []string, index int) (string, int, error) {
	if index+1 >= len(args) || strings.HasPrefix(args[index+1], "--") {
		return "", index, fmt.Errorf("%s requires a value", args[index])
	}
	return args[index+1], index + 1, nil
}

func renderKnowledgeIngest(stdout io.Writer, result knowledge.IngestResult) error {
	failureCode := result.Extraction.FailureCode
	if failureCode == "" {
		failureCode = "none"
	}
	_, err := fmt.Fprintf(
		stdout,
		"source=%s lifecycle=%s restricted=%t artifact_sha256=%s extractor=%s:%s manifest=%s failure_code=%s\n",
		result.Source.Key,
		result.Source.Lifecycle,
		result.Source.Restricted,
		result.Artifact.SHA256,
		result.Extraction.ExtractorName,
		result.Extraction.ExtractorVersion,
		result.ManifestPath,
		failureCode,
	)
	return err
}

func renderKnowledgeRefresh(stdout io.Writer, result knowledge.RefreshResult) error {
	failureCode := result.Extraction.FailureCode
	if failureCode == "" {
		failureCode = "none"
	}
	_, err := fmt.Fprintf(
		stdout,
		"source=%s lifecycle=%s restricted=%t artifact_sha256=%s extractor=%s:%s manifest=%s failure_code=%s\n",
		result.Source.Key,
		result.Source.Lifecycle,
		result.Source.Restricted,
		result.Artifact.SHA256,
		result.Extraction.ExtractorName,
		result.Extraction.ExtractorVersion,
		result.Source.ManifestPath,
		failureCode,
	)
	return err
}

func renderKnowledgeList(stdout io.Writer, results []knowledge.SourceView) error {
	if len(results) == 0 {
		_, err := fmt.Fprintln(stdout, "no knowledge sources")
		return err
	}
	for _, result := range results {
		if err := renderKnowledgeSource(stdout, result.Source); err != nil {
			return err
		}
	}
	return nil
}

func renderKnowledgeSource(stdout io.Writer, source knowledge.Source) error {
	_, err := fmt.Fprintf(
		stdout,
		"source=%s title=%s lifecycle=%s restricted=%t class=%s manifest=%s\n",
		source.Key,
		oneLine(source.Title),
		source.Lifecycle,
		source.Restricted,
		source.SourceClass,
		source.ManifestPath,
	)
	return err
}

func renderKnowledgeSearch(stdout io.Writer, results []knowledge.SearchResult) error {
	if len(results) == 0 {
		_, err := fmt.Fprintln(stdout, "no knowledge results")
		return err
	}
	for _, result := range results {
		if _, err := fmt.Fprintf(
			stdout,
			"source=%s title=%s chunk_id=%d restricted=%t anchor=%s snippet=%s\n",
			result.SourceKey,
			oneLine(result.Title),
			result.ChunkID,
			result.Restricted,
			result.Anchor,
			oneLine(result.Snippet),
		); err != nil {
			return err
		}
	}
	return nil
}

func oneLine(value string) string {
	return strings.Join(strings.Fields(value), " ")
}
