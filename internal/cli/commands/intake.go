package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"unicode"
)

type IntakeCommand struct {
	Name        string
	Source      string
	Type        string
	ProjectKey  string
	Title       string
	ActionKey   string
	DedupKey    string
	RequestedBy string
	PayloadFile string
	JSON        bool
}

func ParseIntake(args []string) (IntakeCommand, error) {
	if len(args) == 0 {
		return IntakeCommand{}, fmt.Errorf("usage: odin intake enqueue --source <source> --project <key> --title <title> --type <type> [--action-key <key>] [--dedup-key <key>] [--requested-by <actor>] [--payload-file <path|-] [--json]")
	}

	command := IntakeCommand{Name: strings.ToLower(args[0])}
	seen := struct {
		source      bool
		intakeType  bool
		project     bool
		title       bool
		actionKey   bool
		dedupKey    bool
		requestedBy bool
		payloadFile bool
	}{}
	switch command.Name {
	case "enqueue":
	default:
		return IntakeCommand{}, fmt.Errorf("unsupported intake subcommand: %s", args[0])
	}

	for index := 1; index < len(args); index++ {
		switch args[index] {
		case "--source":
			if seen.source {
				return IntakeCommand{}, fmt.Errorf("duplicate --source flag")
			}
			value, nextIndex, err := requiredValue(args, index, "--source")
			if err != nil {
				return IntakeCommand{}, err
			}
			seen.source = true
			command.Source = value
			index = nextIndex
		case "--type":
			if seen.intakeType {
				return IntakeCommand{}, fmt.Errorf("duplicate --type flag")
			}
			value, nextIndex, err := requiredValue(args, index, "--type")
			if err != nil {
				return IntakeCommand{}, err
			}
			seen.intakeType = true
			command.Type = value
			index = nextIndex
		case "--project":
			if seen.project {
				return IntakeCommand{}, fmt.Errorf("duplicate --project flag")
			}
			value, nextIndex, err := requiredValue(args, index, "--project")
			if err != nil {
				return IntakeCommand{}, err
			}
			seen.project = true
			command.ProjectKey = value
			index = nextIndex
		case "--title":
			if seen.title {
				return IntakeCommand{}, fmt.Errorf("duplicate --title flag")
			}
			value, nextIndex, err := requiredValue(args, index, "--title")
			if err != nil {
				return IntakeCommand{}, err
			}
			seen.title = true
			command.Title = value
			index = nextIndex
		case "--action-key":
			if seen.actionKey {
				return IntakeCommand{}, fmt.Errorf("duplicate --action-key flag")
			}
			value, nextIndex, err := requiredValue(args, index, "--action-key")
			if err != nil {
				return IntakeCommand{}, err
			}
			seen.actionKey = true
			command.ActionKey = value
			index = nextIndex
		case "--dedup-key":
			if seen.dedupKey {
				return IntakeCommand{}, fmt.Errorf("duplicate --dedup-key flag")
			}
			value, nextIndex, err := requiredValue(args, index, "--dedup-key")
			if err != nil {
				return IntakeCommand{}, err
			}
			seen.dedupKey = true
			command.DedupKey = value
			index = nextIndex
		case "--requested-by":
			if seen.requestedBy {
				return IntakeCommand{}, fmt.Errorf("duplicate --requested-by flag")
			}
			value, nextIndex, err := requiredValue(args, index, "--requested-by")
			if err != nil {
				return IntakeCommand{}, err
			}
			seen.requestedBy = true
			command.RequestedBy = value
			index = nextIndex
		case "--payload-file":
			if seen.payloadFile {
				return IntakeCommand{}, fmt.Errorf("duplicate --payload-file flag")
			}
			value, nextIndex, err := requiredValue(args, index, "--payload-file")
			if err != nil {
				return IntakeCommand{}, err
			}
			seen.payloadFile = true
			command.PayloadFile = value
			index = nextIndex
		case "--json":
			if command.JSON {
				return IntakeCommand{}, fmt.Errorf("duplicate --json flag")
			}
			command.JSON = true
		default:
			return IntakeCommand{}, fmt.Errorf("unknown intake argument: %s", args[index])
		}
	}

	if command.Source == "" {
		return IntakeCommand{}, fmt.Errorf("--source is required")
	}
	if command.ProjectKey == "" {
		return IntakeCommand{}, fmt.Errorf("--project is required")
	}
	if command.Title == "" {
		return IntakeCommand{}, fmt.Errorf("--title is required")
	}
	if command.Type == "" {
		return IntakeCommand{}, fmt.Errorf("--type is required")
	}
	if err := validateMachineValue("--source", command.Source); err != nil {
		return IntakeCommand{}, err
	}
	if err := validateMachineValue("--project", command.ProjectKey); err != nil {
		return IntakeCommand{}, err
	}
	if err := validateMachineValue("--type", command.Type); err != nil {
		return IntakeCommand{}, err
	}
	if err := validateOptionalMachineValue("--action-key", command.ActionKey); err != nil {
		return IntakeCommand{}, err
	}
	if err := validateDedupKey(command.DedupKey); err != nil {
		return IntakeCommand{}, err
	}
	if command.RequestedBy == "" {
		command.RequestedBy = command.Source
	}
	if err := validateOptionalMachineValue("--requested-by", command.RequestedBy); err != nil {
		return IntakeCommand{}, err
	}
	if err := validatePayloadFile(command.PayloadFile); err != nil {
		return IntakeCommand{}, err
	}

	return command, nil
}

func requiredValue(args []string, index int, flag string) (string, int, error) {
	if index+1 >= len(args) {
		return "", index, fmt.Errorf("%s requires a value", flag)
	}
	value := args[index+1]
	if strings.TrimSpace(value) == "" {
		return "", index, fmt.Errorf("%s requires a value", flag)
	}
	return value, index + 1, nil
}

func validateMachineValue(flag string, value string) error {
	if strings.TrimSpace(value) != value {
		return fmt.Errorf("%s must not have leading or trailing whitespace", flag)
	}
	for _, r := range value {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return fmt.Errorf("%s must not contain whitespace or control characters", flag)
		}
	}
	return nil
}

func validateOptionalMachineValue(flag string, value string) error {
	if value == "" {
		return nil
	}
	return validateMachineValue(flag, value)
}

func validateDedupKey(value string) error {
	if value == "" {
		return nil
	}
	if value != strings.TrimSpace(value) {
		return fmt.Errorf("--dedup-key must not have leading or trailing whitespace")
	}
	for _, r := range value {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return fmt.Errorf("--dedup-key must not contain whitespace or control characters")
		}
	}
	return nil
}

func validatePayloadFile(path string) error {
	if path == "" || path == "-" {
		return nil
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read --payload-file: %w", err)
	}
	if !json.Valid(content) {
		return fmt.Errorf("--payload-file must contain valid JSON")
	}
	var payload any
	if err := json.Unmarshal(content, &payload); err != nil {
		return fmt.Errorf("--payload-file must contain valid JSON: %w", err)
	}
	if _, ok := payload.(map[string]any); !ok {
		return fmt.Errorf("--payload-file must contain a JSON object")
	}
	return nil
}
