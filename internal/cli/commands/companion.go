package commands

import (
	"fmt"
	"strings"

	corecompanions "odin-os/internal/core/companions"
)

type CompanionCommand struct {
	Name  string
	Kind  string
	Key   string
	Title string
	JSON  bool
}

type CompanionView struct {
	ID     int64  `json:"id"`
	Key    string `json:"key"`
	Title  string `json:"title"`
	Kind   string `json:"kind"`
	Status string `json:"status"`
}

type CompanionListView struct {
	Companions []CompanionView `json:"companions"`
}

func ParseCompanion(args []string) (CompanionCommand, error) {
	if len(args) == 0 {
		return CompanionCommand{}, fmt.Errorf("usage: odin companion <create|list> [--kind <kind>] [--key <key>] [--title <title>] [--json]")
	}

	command := CompanionCommand{Name: strings.ToLower(args[0])}
	switch command.Name {
	case "create", "list":
	default:
		return CompanionCommand{}, fmt.Errorf("unsupported companion subcommand: %s", args[0])
	}

	for index := 1; index < len(args); index++ {
		switch args[index] {
		case "--kind":
			if index+1 >= len(args) {
				return CompanionCommand{}, fmt.Errorf("--kind requires a value")
			}
			index++
			command.Kind = strings.ToLower(strings.TrimSpace(args[index]))
		case "--key":
			if index+1 >= len(args) {
				return CompanionCommand{}, fmt.Errorf("--key requires a value")
			}
			index++
			command.Key = strings.TrimSpace(args[index])
		case "--title":
			if index+1 >= len(args) {
				return CompanionCommand{}, fmt.Errorf("--title requires a value")
			}
			index++
			command.Title = strings.TrimSpace(args[index])
		case "--json":
			if command.JSON {
				return CompanionCommand{}, fmt.Errorf("duplicate --json flag")
			}
			command.JSON = true
		default:
			return CompanionCommand{}, fmt.Errorf("unknown companion argument: %s", args[index])
		}
	}

	switch command.Name {
	case "create":
		if command.Kind == "" {
			return CompanionCommand{}, fmt.Errorf("--kind is required")
		}
		if !isSupportedCompanionKind(command.Kind) {
			return CompanionCommand{}, fmt.Errorf("unsupported companion kind: %s", command.Kind)
		}
		if command.Key == "" {
			return CompanionCommand{}, fmt.Errorf("--key is required")
		}
		if command.Title == "" {
			return CompanionCommand{}, fmt.Errorf("--title is required")
		}
	case "list":
		if command.Kind != "" {
			return CompanionCommand{}, fmt.Errorf("--kind is only valid for companion create")
		}
		if command.Key != "" || command.Title != "" {
			return CompanionCommand{}, fmt.Errorf("companion list does not accept --key or --title")
		}
	}

	return command, nil
}

func isSupportedCompanionKind(kind string) bool {
	switch corecompanions.Kind(strings.ToLower(kind)) {
	case corecompanions.KindAssistant, corecompanions.KindAdvisor, corecompanions.KindOperator, corecompanions.KindSpecialist:
		return true
	default:
		return false
	}
}
