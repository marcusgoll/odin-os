package commands

import (
	"fmt"
	"strings"

	"odin-os/internal/core/initiatives"
)

type InitiativeCommand struct {
	Name  string
	Kind  string
	Key   string
	Title string
	JSON  bool
}

type InitiativeView struct {
	ID      int64  `json:"id"`
	Key     string `json:"key"`
	Title   string `json:"title"`
	Kind    string `json:"kind"`
	Status  string `json:"status"`
	Summary string `json:"summary,omitempty"`
}

type InitiativeListView struct {
	Initiatives []InitiativeView `json:"initiatives"`
}

func ParseInitiative(args []string) (InitiativeCommand, error) {
	if len(args) == 0 {
		return InitiativeCommand{}, fmt.Errorf("usage: odin initiative <create|list> [--kind <kind>] [--key <key>] [--title <title>] [--json]")
	}

	command := InitiativeCommand{Name: strings.ToLower(args[0])}
	switch command.Name {
	case "create", "list":
	default:
		return InitiativeCommand{}, fmt.Errorf("unsupported initiative subcommand: %s", args[0])
	}

	for index := 1; index < len(args); index++ {
		switch args[index] {
		case "--kind":
			if index+1 >= len(args) {
				return InitiativeCommand{}, fmt.Errorf("--kind requires a value")
			}
			index++
			command.Kind = strings.ToLower(strings.TrimSpace(args[index]))
		case "--key":
			if index+1 >= len(args) {
				return InitiativeCommand{}, fmt.Errorf("--key requires a value")
			}
			index++
			command.Key = strings.TrimSpace(args[index])
		case "--title":
			if index+1 >= len(args) {
				return InitiativeCommand{}, fmt.Errorf("--title requires a value")
			}
			index++
			command.Title = strings.TrimSpace(args[index])
		case "--json":
			if command.JSON {
				return InitiativeCommand{}, fmt.Errorf("duplicate --json flag")
			}
			command.JSON = true
		default:
			return InitiativeCommand{}, fmt.Errorf("unknown initiative argument: %s", args[index])
		}
	}

	switch command.Name {
	case "create":
		if command.Kind == "" {
			return InitiativeCommand{}, fmt.Errorf("--kind is required")
		}
		if !isSupportedNonProjectInitiativeKind(command.Kind) {
			return InitiativeCommand{}, fmt.Errorf("unsupported initiative kind: %s", command.Kind)
		}
		if command.Key == "" {
			return InitiativeCommand{}, fmt.Errorf("--key is required")
		}
		if command.Title == "" {
			return InitiativeCommand{}, fmt.Errorf("--title is required")
		}
	case "list":
		if command.Kind != "" {
			return InitiativeCommand{}, fmt.Errorf("--kind is only valid for initiative create")
		}
		if command.Key != "" || command.Title != "" {
			return InitiativeCommand{}, fmt.Errorf("initiative list does not accept --key or --title")
		}
	}

	return command, nil
}

func isSupportedNonProjectInitiativeKind(kind string) bool {
	switch initiatives.Kind(strings.ToLower(kind)) {
	case initiatives.KindGoal, initiatives.KindCase, initiatives.KindRoutine, initiatives.KindCampaign, initiatives.KindPersonalAdmin:
		return true
	default:
		return false
	}
}
