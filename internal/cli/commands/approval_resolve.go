package commands

import (
	"fmt"
	"strconv"
	"strings"
)

type ApprovalResolveCommand struct {
	Name       string
	ApprovalID int64
	Decision   string
	Reason     string
	By         string
	JSON       bool
}

func ParseApprovalResolve(args []string) (ApprovalResolveCommand, error) {
	if len(args) == 0 {
		return ApprovalResolveCommand{}, fmt.Errorf("usage: odin approvals resolve --id <id> --decision <approve|reject> --reason <reason> --by <actor> [--json]")
	}

	command := ApprovalResolveCommand{Name: strings.ToLower(args[0])}
	if command.Name != "resolve" {
		return ApprovalResolveCommand{}, fmt.Errorf("unsupported approvals subcommand: %s", args[0])
	}

	seen := struct {
		id       bool
		decision bool
		reason   bool
		by       bool
	}{}

	for index := 1; index < len(args); index++ {
		switch args[index] {
		case "--id":
			if seen.id {
				return ApprovalResolveCommand{}, fmt.Errorf("duplicate --id flag")
			}
			value, nextIndex, err := approvalResolveValue(args, index, "--id")
			if err != nil {
				return ApprovalResolveCommand{}, err
			}
			approvalID, err := strconv.ParseInt(value, 10, 64)
			if err != nil {
				return ApprovalResolveCommand{}, fmt.Errorf("--id must be an integer")
			}
			seen.id = true
			command.ApprovalID = approvalID
			index = nextIndex
		case "--decision":
			if seen.decision {
				return ApprovalResolveCommand{}, fmt.Errorf("duplicate --decision flag")
			}
			value, nextIndex, err := approvalResolveValue(args, index, "--decision")
			if err != nil {
				return ApprovalResolveCommand{}, err
			}
			seen.decision = true
			command.Decision = strings.ToLower(strings.TrimSpace(value))
			index = nextIndex
		case "--reason":
			if seen.reason {
				return ApprovalResolveCommand{}, fmt.Errorf("duplicate --reason flag")
			}
			value, nextIndex, err := approvalResolveReason(args, index)
			if err != nil {
				return ApprovalResolveCommand{}, err
			}
			seen.reason = true
			command.Reason = value
			index = nextIndex
		case "--by":
			if seen.by {
				return ApprovalResolveCommand{}, fmt.Errorf("duplicate --by flag")
			}
			value, nextIndex, err := approvalResolveValue(args, index, "--by")
			if err != nil {
				return ApprovalResolveCommand{}, err
			}
			seen.by = true
			command.By = value
			index = nextIndex
		case "--json":
			if command.JSON {
				return ApprovalResolveCommand{}, fmt.Errorf("duplicate --json flag")
			}
			command.JSON = true
		default:
			return ApprovalResolveCommand{}, fmt.Errorf("unknown approvals argument: %s", args[index])
		}
	}

	if command.ApprovalID == 0 {
		return ApprovalResolveCommand{}, fmt.Errorf("--id is required")
	}
	if command.Decision == "" {
		return ApprovalResolveCommand{}, fmt.Errorf("--decision is required")
	}
	if command.Reason == "" {
		return ApprovalResolveCommand{}, fmt.Errorf("--reason is required")
	}
	if command.By == "" {
		return ApprovalResolveCommand{}, fmt.Errorf("--by is required")
	}
	if command.Decision != "approve" && command.Decision != "approved" && command.Decision != "reject" && command.Decision != "rejected" && command.Decision != "deny" && command.Decision != "denied" {
		return ApprovalResolveCommand{}, fmt.Errorf("--decision must be approve or reject")
	}
	return command, nil
}

func approvalResolveValue(args []string, index int, flag string) (string, int, error) {
	if index+1 >= len(args) {
		return "", index, fmt.Errorf("%s requires a value", flag)
	}
	value := strings.TrimSpace(args[index+1])
	if value == "" {
		return "", index, fmt.Errorf("%s requires a value", flag)
	}
	return value, index + 1, nil
}

func approvalResolveReason(args []string, index int) (string, int, error) {
	if index+1 >= len(args) {
		return "", index, fmt.Errorf("--reason requires a value")
	}

	valueStart := index + 1
	valueEnd := valueStart
	for valueEnd < len(args) && !strings.HasPrefix(args[valueEnd], "--") {
		valueEnd++
	}
	if valueEnd == valueStart {
		return "", index, fmt.Errorf("--reason requires a value")
	}

	return strings.TrimSpace(strings.Join(args[valueStart:valueEnd], " ")), valueEnd - 1, nil
}
