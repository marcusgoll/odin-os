package commands

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const GoalUsage = "usage: odin goal create --title <title> [--description <text>] [--created-by <actor>] [--source <source>] [--json] | odin goal list [--status <status>] [--limit <n>] [--json] | odin goal show --id <id> [--json] | odin goal update --id <id> [--title <title>] [--description <text>] [--actor <actor>] [--reason <reason>] [--json] | odin goal transition --id <id> --status <status> [--actor <actor>] [--reason <reason>] [--json] | odin goal tick [--json]"

type GoalCommand struct {
	Name           string
	ID             int64
	Title          string
	Description    string
	TitleSet       bool
	DescriptionSet bool
	CreatedBy      string
	Source         string
	Status         string
	Actor          string
	Reason         string
	Limit          int
	JSON           bool
}

type GoalView struct {
	ID           int64     `json:"id"`
	Title        string    `json:"title"`
	Description  string    `json:"description,omitempty"`
	Status       string    `json:"status"`
	CreatedBy    string    `json:"created_by,omitempty"`
	Source       string    `json:"source,omitempty"`
	CurrentRunID *int64    `json:"current_run_id,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type GoalEvidenceView struct {
	ID           int64           `json:"id"`
	GoalID       int64           `json:"goal_id"`
	GoalRunID    *int64          `json:"goal_run_id,omitempty"`
	EvidenceType string          `json:"evidence_type"`
	Summary      string          `json:"summary"`
	URI          string          `json:"uri,omitempty"`
	PayloadJSON  json.RawMessage `json:"payload_json"`
	CreatedBy    string          `json:"created_by,omitempty"`
	CreatedAt    time.Time       `json:"created_at"`
}

type GoalEnvelope struct {
	Goal     GoalView           `json:"goal"`
	Evidence []GoalEvidenceView `json:"evidence,omitempty"`
}

type GoalListView struct {
	Goals []GoalView `json:"goals"`
}

func ParseGoal(args []string) (GoalCommand, error) {
	if len(args) == 0 {
		return GoalCommand{}, fmt.Errorf(GoalUsage)
	}
	if args[0] == "help" || args[0] == "--help" {
		return GoalCommand{Name: "help"}, nil
	}
	command := GoalCommand{Name: strings.ToLower(args[0])}
	switch command.Name {
	case "create", "list", "show", "update", "tick":
	case "transition":
		if len(args) >= 2 && !strings.HasPrefix(args[1], "--") {
			id, err := strconv.ParseInt(args[1], 10, 64)
			if err != nil || id <= 0 {
				return GoalCommand{}, fmt.Errorf("goal id must be a positive integer")
			}
			command.ID = id
			args = append([]string{args[0]}, args[2:]...)
		}
	default:
		return GoalCommand{}, fmt.Errorf("unsupported goal subcommand: %s", args[0])
	}
	if command.Name != "transition" || command.ID == 0 {
		args = append([]string{args[0]}, args[1:]...)
	}

	for index := 1; index < len(args); index++ {
		switch args[index] {
		case "--id":
			value, nextIndex, err := requiredValue(args, index, "--id")
			if err != nil {
				return GoalCommand{}, err
			}
			id, err := strconv.ParseInt(value, 10, 64)
			if err != nil || id <= 0 {
				return GoalCommand{}, fmt.Errorf("goal id must be a positive integer")
			}
			if command.ID != 0 && command.ID != id {
				return GoalCommand{}, fmt.Errorf("duplicate conflicting goal id")
			}
			command.ID = id
			index = nextIndex
		case "--title":
			value, nextIndex, err := requiredValue(args, index, "--title")
			if err != nil {
				return GoalCommand{}, err
			}
			command.Title = value
			command.TitleSet = true
			index = nextIndex
		case "--description":
			value, nextIndex, err := requiredValue(args, index, "--description")
			if err != nil {
				return GoalCommand{}, err
			}
			command.Description = value
			command.DescriptionSet = true
			index = nextIndex
		case "--created-by":
			value, nextIndex, err := requiredValue(args, index, "--created-by")
			if err != nil {
				return GoalCommand{}, err
			}
			command.CreatedBy = value
			index = nextIndex
		case "--source":
			value, nextIndex, err := requiredValue(args, index, "--source")
			if err != nil {
				return GoalCommand{}, err
			}
			command.Source = value
			index = nextIndex
		case "--status":
			value, nextIndex, err := requiredValue(args, index, "--status")
			if err != nil {
				return GoalCommand{}, err
			}
			command.Status = strings.ToLower(strings.TrimSpace(value))
			index = nextIndex
		case "--actor":
			value, nextIndex, err := requiredValue(args, index, "--actor")
			if err != nil {
				return GoalCommand{}, err
			}
			command.Actor = value
			index = nextIndex
		case "--reason":
			value, nextIndex, err := requiredValue(args, index, "--reason")
			if err != nil {
				return GoalCommand{}, err
			}
			command.Reason = value
			index = nextIndex
		case "--limit":
			value, nextIndex, err := requiredValue(args, index, "--limit")
			if err != nil {
				return GoalCommand{}, err
			}
			limit, err := strconv.Atoi(value)
			if err != nil || limit <= 0 {
				return GoalCommand{}, fmt.Errorf("--limit must be a positive integer")
			}
			command.Limit = limit
			index = nextIndex
		case "--json":
			if command.JSON {
				return GoalCommand{}, fmt.Errorf("duplicate --json flag")
			}
			command.JSON = true
		default:
			return GoalCommand{}, fmt.Errorf("unknown goal argument: %s", args[index])
		}
	}

	switch command.Name {
	case "create":
		if strings.TrimSpace(command.Title) == "" {
			return GoalCommand{}, fmt.Errorf("--title is required")
		}
		if command.ID != 0 || command.Status != "" || command.Actor != "" || command.Reason != "" || command.Limit != 0 {
			return GoalCommand{}, fmt.Errorf("goal create does not accept transition options")
		}
	case "list":
		if command.ID != 0 || command.TitleSet || command.DescriptionSet || command.CreatedBy != "" || command.Source != "" || command.Actor != "" || command.Reason != "" {
			return GoalCommand{}, fmt.Errorf("goal list only accepts --status, --limit, and --json")
		}
	case "show":
		if command.ID <= 0 {
			return GoalCommand{}, fmt.Errorf("--id is required")
		}
		if command.TitleSet || command.DescriptionSet || command.CreatedBy != "" || command.Source != "" || command.Status != "" || command.Actor != "" || command.Reason != "" || command.Limit != 0 {
			return GoalCommand{}, fmt.Errorf("goal show only accepts --id and --json")
		}
	case "update":
		if command.ID <= 0 {
			return GoalCommand{}, fmt.Errorf("--id is required")
		}
		if !command.TitleSet && !command.DescriptionSet {
			return GoalCommand{}, fmt.Errorf("goal update requires --title or --description")
		}
		if command.CreatedBy != "" || command.Source != "" || command.Status != "" || command.Limit != 0 {
			return GoalCommand{}, fmt.Errorf("goal update only accepts --id, update fields, audit fields, and --json")
		}
	case "transition":
		if command.ID <= 0 {
			return GoalCommand{}, fmt.Errorf("--id is required")
		}
		if strings.TrimSpace(command.Status) == "" {
			return GoalCommand{}, fmt.Errorf("--status is required")
		}
		if command.TitleSet || command.DescriptionSet || command.CreatedBy != "" || command.Source != "" || command.Limit != 0 {
			return GoalCommand{}, fmt.Errorf("goal transition does not accept create options")
		}
	case "tick":
		if command.ID != 0 || command.TitleSet || command.DescriptionSet || command.CreatedBy != "" || command.Source != "" || command.Status != "" || command.Actor != "" || command.Reason != "" || command.Limit != 0 {
			return GoalCommand{}, fmt.Errorf("goal tick only accepts --json")
		}
	}

	return command, nil
}
