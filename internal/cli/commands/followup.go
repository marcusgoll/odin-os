package commands

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

type FollowUpCommand struct {
	Name       string
	Initiative string
	Title      string
	Cadence    string
	ID         int64
	Until      time.Time
	JSON       bool
}

type FollowUpView struct {
	ID                 int64      `json:"id"`
	InitiativeKey      string     `json:"initiative_key,omitempty"`
	InitiativeID       *int64     `json:"initiative_id,omitempty"`
	CompanionID        *int64     `json:"companion_id,omitempty"`
	TargetProjectID    int64      `json:"target_project_id"`
	TargetProjectKey   string     `json:"target_project_key,omitempty"`
	Title              string     `json:"title"`
	Status             string     `json:"status"`
	Cadence            string     `json:"cadence"`
	NextDueAt          time.Time  `json:"next_due_at"`
	LastMaterializedAt *time.Time `json:"last_materialized_at,omitempty"`
	LastCompletedAt    *time.Time `json:"last_completed_at,omitempty"`
}

type FollowUpListView struct {
	Obligations []FollowUpView `json:"obligations"`
}

func ParseFollowUp(args []string) (FollowUpCommand, error) {
	if len(args) == 0 {
		return FollowUpCommand{}, fmt.Errorf("usage: odin followup <add|list|complete|snooze> [options]")
	}

	command := FollowUpCommand{Name: strings.ToLower(args[0])}
	switch command.Name {
	case "add", "list", "complete", "snooze":
	default:
		return FollowUpCommand{}, fmt.Errorf("unsupported followup subcommand: %s", args[0])
	}

	if command.Name == "complete" || command.Name == "snooze" {
		if len(args) < 2 {
			return FollowUpCommand{}, fmt.Errorf("followup %s requires an obligation ID", command.Name)
		}
		id, err := strconv.ParseInt(args[1], 10, 64)
		if err != nil || id <= 0 {
			return FollowUpCommand{}, fmt.Errorf("followup %s requires a numeric obligation ID", command.Name)
		}
		command.ID = id
		args = append([]string{args[0]}, args[2:]...)
	} else {
		args = append([]string{args[0]}, args[1:]...)
	}

	for index := 1; index < len(args); index++ {
		switch args[index] {
		case "--initiative":
			if index+1 >= len(args) {
				return FollowUpCommand{}, fmt.Errorf("--initiative requires a value")
			}
			index++
			command.Initiative = strings.TrimSpace(args[index])
		case "--title":
			if index+1 >= len(args) {
				return FollowUpCommand{}, fmt.Errorf("--title requires a value")
			}
			index++
			command.Title = strings.TrimSpace(args[index])
		case "--cadence":
			if index+1 >= len(args) {
				return FollowUpCommand{}, fmt.Errorf("--cadence requires a value")
			}
			index++
			command.Cadence = strings.ToLower(strings.TrimSpace(args[index]))
		case "--until":
			if index+1 >= len(args) {
				return FollowUpCommand{}, fmt.Errorf("--until requires a value")
			}
			index++
			until, err := time.Parse(time.RFC3339, strings.TrimSpace(args[index]))
			if err != nil {
				return FollowUpCommand{}, fmt.Errorf("--until requires an RFC3339 timestamp")
			}
			command.Until = until.UTC()
		case "--json":
			if command.JSON {
				return FollowUpCommand{}, fmt.Errorf("duplicate --json flag")
			}
			command.JSON = true
		default:
			if command.Name == "complete" || command.Name == "snooze" {
				return FollowUpCommand{}, fmt.Errorf("unknown followup argument: %s", args[index])
			}
			return FollowUpCommand{}, fmt.Errorf("unknown followup argument: %s", args[index])
		}
	}

	switch command.Name {
	case "add":
		if command.Initiative == "" {
			return FollowUpCommand{}, fmt.Errorf("--initiative is required")
		}
		if command.Title == "" {
			return FollowUpCommand{}, fmt.Errorf("--title is required")
		}
		if command.Cadence == "" {
			return FollowUpCommand{}, fmt.Errorf("--cadence is required")
		}
		if command.JSON {
			return FollowUpCommand{}, fmt.Errorf("--json is only valid for followup list")
		}
	case "list":
		if command.Initiative != "" || command.Title != "" || command.Cadence != "" {
			return FollowUpCommand{}, fmt.Errorf("followup list does not accept add options")
		}
	case "complete":
		if command.Initiative != "" || command.Title != "" || command.Cadence != "" || !command.Until.IsZero() {
			return FollowUpCommand{}, fmt.Errorf("followup complete does not accept extra flags")
		}
		if command.JSON {
			return FollowUpCommand{}, fmt.Errorf("--json is only valid for followup list")
		}
	case "snooze":
		if command.Initiative != "" || command.Title != "" || command.Cadence != "" {
			return FollowUpCommand{}, fmt.Errorf("followup snooze does not accept add options")
		}
		if command.Until.IsZero() {
			return FollowUpCommand{}, fmt.Errorf("--until is required")
		}
		if command.JSON {
			return FollowUpCommand{}, fmt.Errorf("--json is only valid for followup list")
		}
	}

	return command, nil
}
