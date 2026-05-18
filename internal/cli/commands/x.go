package commands

import (
	"fmt"
	"strconv"
	"strings"
)

const XUsage = "usage: odin x bio request --session-id <id> --bio <text> [--url <url>] [--project <key>] [--json] | odin x bio apply --approval-id <id> [--json] | odin x post request --text <text> [--project <key>] [--json] | odin x reply request --reply-to <url> --text <text> [--project <key>] [--json]"

type XCommand struct {
	Workflow   string
	Action     string
	ProjectKey string
	SessionID  int64
	ApprovalID int64
	URL        string
	Bio        string
	Text       string
	ReplyToURL string
	JSON       bool
}

func ParseX(args []string) (XCommand, error) {
	if len(args) == 0 {
		return XCommand{}, fmt.Errorf(XUsage)
	}
	if args[0] == "help" || args[0] == "--help" {
		return XCommand{Workflow: "help"}, nil
	}
	if len(args) < 2 {
		return XCommand{}, fmt.Errorf(XUsage)
	}
	command := XCommand{
		Workflow:   strings.ToLower(strings.TrimSpace(args[0])),
		Action:     strings.ToLower(strings.TrimSpace(args[1])),
		ProjectKey: "marcusgoll",
	}
	switch command.Workflow {
	case "bio":
		return parseXBio(args[2:], command)
	case "post":
		return parseXPost(args[2:], command)
	case "reply":
		return parseXReply(args[2:], command)
	default:
		return XCommand{}, fmt.Errorf("unsupported x workflow: %s", args[0])
	}
}

func parseXBio(args []string, command XCommand) (XCommand, error) {
	switch command.Action {
	case "request", "apply":
	default:
		return XCommand{}, fmt.Errorf("unsupported x bio action: %s", command.Action)
	}
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "--session-id":
			value, nextIndex, err := requiredValue(args, index, "--session-id")
			if err != nil {
				return XCommand{}, err
			}
			id, err := strconv.ParseInt(value, 10, 64)
			if err != nil || id <= 0 {
				return XCommand{}, fmt.Errorf("--session-id must be a positive integer")
			}
			command.SessionID = id
			index = nextIndex
		case "--approval-id":
			value, nextIndex, err := requiredValue(args, index, "--approval-id")
			if err != nil {
				return XCommand{}, err
			}
			id, err := strconv.ParseInt(value, 10, 64)
			if err != nil || id <= 0 {
				return XCommand{}, fmt.Errorf("--approval-id must be a positive integer")
			}
			command.ApprovalID = id
			index = nextIndex
		case "--bio":
			value, nextIndex, err := requiredValue(args, index, "--bio")
			if err != nil {
				return XCommand{}, err
			}
			command.Bio = strings.TrimSpace(value)
			index = nextIndex
		case "--url":
			value, nextIndex, err := requiredValue(args, index, "--url")
			if err != nil {
				return XCommand{}, err
			}
			command.URL = strings.TrimSpace(value)
			index = nextIndex
		case "--project":
			value, nextIndex, err := requiredValue(args, index, "--project")
			if err != nil {
				return XCommand{}, err
			}
			command.ProjectKey = strings.TrimSpace(value)
			index = nextIndex
		case "--json":
			if command.JSON {
				return XCommand{}, fmt.Errorf("duplicate --json flag")
			}
			command.JSON = true
		default:
			return XCommand{}, fmt.Errorf("unknown x bio argument: %s", args[index])
		}
	}
	if command.Action == "request" {
		if command.SessionID <= 0 {
			return XCommand{}, fmt.Errorf("--session-id is required")
		}
		if command.Bio == "" {
			return XCommand{}, fmt.Errorf("--bio is required")
		}
	}
	if command.Action == "apply" && command.ApprovalID <= 0 {
		return XCommand{}, fmt.Errorf("--approval-id is required")
	}
	return command, nil
}

func parseXPost(args []string, command XCommand) (XCommand, error) {
	if command.Action != "request" {
		return XCommand{}, fmt.Errorf("unsupported x post action: %s", command.Action)
	}
	return parseXTextRequest(args, command, false)
}

func parseXReply(args []string, command XCommand) (XCommand, error) {
	if command.Action != "request" {
		return XCommand{}, fmt.Errorf("unsupported x reply action: %s", command.Action)
	}
	return parseXTextRequest(args, command, true)
}

func parseXTextRequest(args []string, command XCommand, requireReplyTo bool) (XCommand, error) {
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "--text":
			value, nextIndex, err := requiredValue(args, index, "--text")
			if err != nil {
				return XCommand{}, err
			}
			command.Text = strings.TrimSpace(value)
			index = nextIndex
		case "--reply-to":
			value, nextIndex, err := requiredValue(args, index, "--reply-to")
			if err != nil {
				return XCommand{}, err
			}
			command.ReplyToURL = strings.TrimSpace(value)
			index = nextIndex
		case "--project":
			value, nextIndex, err := requiredValue(args, index, "--project")
			if err != nil {
				return XCommand{}, err
			}
			command.ProjectKey = strings.TrimSpace(value)
			index = nextIndex
		case "--json":
			if command.JSON {
				return XCommand{}, fmt.Errorf("duplicate --json flag")
			}
			command.JSON = true
		default:
			return XCommand{}, fmt.Errorf("unknown x %s argument: %s", command.Workflow, args[index])
		}
	}
	if command.Text == "" {
		return XCommand{}, fmt.Errorf("--text is required")
	}
	if requireReplyTo && command.ReplyToURL == "" {
		return XCommand{}, fmt.Errorf("--reply-to is required")
	}
	return command, nil
}
