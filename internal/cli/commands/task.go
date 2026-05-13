package commands

import (
	"fmt"
	"strings"
)

type TaskCommand struct {
	Name               string
	ProjectKey         string
	Title              string
	AcceptanceCriteria []string
	JSON               bool
}

type TaskCreateView struct {
	ID     int64  `json:"id"`
	Key    string `json:"key"`
	Status string `json:"status"`
	Scope  string `json:"scope"`
}

type TaskRunResultView struct {
	ID       int64  `json:"id"`
	Executor string `json:"executor"`
	Status   string `json:"status"`
	Summary  string `json:"summary,omitempty"`
}

type TaskRunView struct {
	Task TaskCreateView     `json:"task"`
	Run  *TaskRunResultView `json:"run,omitempty"`
}

func ParseTask(args []string) (TaskCommand, error) {
	if len(args) == 0 {
		return TaskCommand{}, fmt.Errorf("usage: odin task <create|run> --project <key> --title <title> [--acceptance <criterion>] [--json]")
	}

	command := TaskCommand{Name: strings.ToLower(args[0])}
	switch command.Name {
	case "create", "run":
	default:
		return TaskCommand{}, fmt.Errorf("unsupported task subcommand: %s", args[0])
	}

	for index := 1; index < len(args); index++ {
		switch args[index] {
		case "--project":
			if index+1 >= len(args) {
				return TaskCommand{}, fmt.Errorf("--project requires a value")
			}
			index++
			command.ProjectKey = strings.TrimSpace(args[index])
		case "--title":
			if index+1 >= len(args) {
				return TaskCommand{}, fmt.Errorf("--title requires a value")
			}
			index++
			command.Title = strings.TrimSpace(args[index])
		case "--acceptance", "--acceptance-criteria":
			if index+1 >= len(args) {
				return TaskCommand{}, fmt.Errorf("%s requires a value", args[index])
			}
			index++
			criterion := strings.TrimSpace(args[index])
			if criterion != "" {
				command.AcceptanceCriteria = append(command.AcceptanceCriteria, criterion)
			}
		case "--json":
			if command.JSON {
				return TaskCommand{}, fmt.Errorf("duplicate --json flag")
			}
			command.JSON = true
		default:
			return TaskCommand{}, fmt.Errorf("unknown task argument: %s", args[index])
		}
	}

	if command.ProjectKey == "" {
		return TaskCommand{}, fmt.Errorf("--project is required")
	}
	if command.Title == "" {
		return TaskCommand{}, fmt.Errorf("--title is required")
	}

	return command, nil
}
