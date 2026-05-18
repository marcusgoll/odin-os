package commands

import (
	"fmt"
	"strings"
)

const FactoryUsage = "usage: odin factory start --project <key> --title <text> [--json] | odin factory status --task <id|key> [--json] | odin factory promote-intake <id|key> [--json] | odin factory merge-gate --task <id|key> [--json]"

type FactoryCommand struct {
	Action   string
	Project  string
	Title    string
	Task     string
	IntakeID string
	JSON     bool
}

func ParseFactory(args []string) (FactoryCommand, error) {
	if len(args) == 0 {
		return FactoryCommand{}, fmt.Errorf(FactoryUsage)
	}

	command := FactoryCommand{Action: strings.ToLower(strings.TrimSpace(args[0]))}
	switch command.Action {
	case "start":
		if err := parseFactoryFlags(args[1:], map[string]func(string) error{
			"project": func(value string) error {
				command.Project = strings.TrimSpace(value)
				return nil
			},
			"title": func(value string) error {
				command.Title = strings.TrimSpace(value)
				return nil
			},
		}, &command.JSON); err != nil {
			return FactoryCommand{}, err
		}
		if command.Project == "" {
			return FactoryCommand{}, fmt.Errorf("factory start requires --project")
		}
		if command.Title == "" {
			return FactoryCommand{}, fmt.Errorf("factory start requires --title")
		}
	case "status":
		if err := parseFactoryFlags(args[1:], map[string]func(string) error{
			"task": func(value string) error {
				command.Task = strings.TrimSpace(value)
				return nil
			},
		}, &command.JSON); err != nil {
			return FactoryCommand{}, err
		}
		if command.Task == "" {
			return FactoryCommand{}, fmt.Errorf("factory status requires --task")
		}
	case "promote-intake":
		remaining, err := parseFactoryPositional(args[1:], &command.JSON)
		if err != nil {
			return FactoryCommand{}, err
		}
		if len(remaining) != 1 {
			return FactoryCommand{}, fmt.Errorf("factory promote-intake requires an intake id or key")
		}
		command.IntakeID = strings.TrimSpace(remaining[0])
		if command.IntakeID == "" {
			return FactoryCommand{}, fmt.Errorf("factory promote-intake requires an intake id or key")
		}
	case "merge-gate":
		if err := parseFactoryFlags(args[1:], map[string]func(string) error{
			"task": func(value string) error {
				command.Task = strings.TrimSpace(value)
				return nil
			},
		}, &command.JSON); err != nil {
			return FactoryCommand{}, err
		}
		if command.Task == "" {
			return FactoryCommand{}, fmt.Errorf("factory merge-gate requires --task")
		}
	default:
		return FactoryCommand{}, fmt.Errorf("unknown factory command: %s", args[0])
	}

	return command, nil
}

func parseFactoryFlags(args []string, setters map[string]func(string) error, json *bool) error {
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if arg == "--json" {
			if *json {
				return fmt.Errorf("duplicate --json flag")
			}
			*json = true
			continue
		}
		if !strings.HasPrefix(arg, "--") {
			return fmt.Errorf("unknown factory argument: %s", arg)
		}
		key := strings.TrimPrefix(arg, "--")
		setter, ok := setters[key]
		if !ok {
			return fmt.Errorf("unknown factory argument: %s", arg)
		}
		if index+1 >= len(args) || strings.HasPrefix(args[index+1], "--") {
			return fmt.Errorf("%s requires a value", arg)
		}
		index++
		if err := setter(args[index]); err != nil {
			return err
		}
	}
	return nil
}

func parseFactoryPositional(args []string, json *bool) ([]string, error) {
	remaining := make([]string, 0, len(args))
	for _, arg := range args {
		if arg == "--json" {
			if *json {
				return nil, fmt.Errorf("duplicate --json flag")
			}
			*json = true
			continue
		}
		if strings.HasPrefix(arg, "--") {
			return nil, fmt.Errorf("unknown factory argument: %s", arg)
		}
		remaining = append(remaining, arg)
	}
	return remaining, nil
}
