package commands

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

const BrowserUsage = "usage: odin browser run --goal-id <id> --url <url> [--objective <text>] [--allowed-domain <domain>] [--max-pages <n>] [--max-duration-seconds <n>] [--worker-mode <fetch|browser>] [--evidence-required] [--action <read|navigate|snapshot|extract>] [--json]"

type BrowserCommand struct {
	Name               string
	GoalID             int64
	URL                string
	URLs               []string
	Objective          string
	AllowedDomains     []string
	MaxPages           int
	MaxDurationSeconds int
	WorkerMode         string
	EvidenceRequired   bool
	Actions            []string
	JSON               bool
}

func ParseBrowser(args []string) (BrowserCommand, error) {
	if len(args) == 0 {
		return BrowserCommand{}, fmt.Errorf(BrowserUsage)
	}
	if args[0] == "help" || args[0] == "--help" {
		return BrowserCommand{Name: "help"}, nil
	}
	command := BrowserCommand{
		Name:               strings.ToLower(strings.TrimSpace(args[0])),
		MaxPages:           1,
		MaxDurationSeconds: 30,
		Actions:            []string{"read"},
	}
	if command.Name != "run" {
		return BrowserCommand{}, fmt.Errorf("unsupported browser subcommand: %s", args[0])
	}
	for index := 1; index < len(args); index++ {
		switch args[index] {
		case "--goal-id":
			value, nextIndex, err := requiredValue(args, index, "--goal-id")
			if err != nil {
				return BrowserCommand{}, err
			}
			goalID, err := strconv.ParseInt(value, 10, 64)
			if err != nil || goalID <= 0 {
				return BrowserCommand{}, fmt.Errorf("--goal-id must be a positive integer")
			}
			command.GoalID = goalID
			index = nextIndex
		case "--url":
			value, nextIndex, err := requiredValue(args, index, "--url")
			if err != nil {
				return BrowserCommand{}, err
			}
			command.URLs = append(command.URLs, value)
			if command.URL == "" {
				command.URL = value
			}
			index = nextIndex
		case "--objective":
			value, nextIndex, err := requiredValue(args, index, "--objective")
			if err != nil {
				return BrowserCommand{}, err
			}
			command.Objective = value
			index = nextIndex
		case "--allowed-domain":
			value, nextIndex, err := requiredValue(args, index, "--allowed-domain")
			if err != nil {
				return BrowserCommand{}, err
			}
			command.AllowedDomains = append(command.AllowedDomains, value)
			index = nextIndex
		case "--max-pages":
			value, nextIndex, err := requiredValue(args, index, "--max-pages")
			if err != nil {
				return BrowserCommand{}, err
			}
			maxPages, err := strconv.Atoi(value)
			if err != nil || maxPages <= 0 {
				return BrowserCommand{}, fmt.Errorf("--max-pages must be a positive integer")
			}
			command.MaxPages = maxPages
			index = nextIndex
		case "--max-duration-seconds":
			value, nextIndex, err := requiredValue(args, index, "--max-duration-seconds")
			if err != nil {
				return BrowserCommand{}, err
			}
			maxDuration, err := strconv.Atoi(value)
			if err != nil || maxDuration <= 0 {
				return BrowserCommand{}, fmt.Errorf("--max-duration-seconds must be a positive integer")
			}
			command.MaxDurationSeconds = maxDuration
			index = nextIndex
		case "--evidence-required":
			command.EvidenceRequired = true
		case "--worker-mode":
			value, nextIndex, err := requiredValue(args, index, "--worker-mode")
			if err != nil {
				return BrowserCommand{}, err
			}
			workerMode := strings.ToLower(strings.TrimSpace(value))
			if workerMode != "fetch" && workerMode != "browser" {
				return BrowserCommand{}, fmt.Errorf("--worker-mode must be fetch or browser")
			}
			command.WorkerMode = workerMode
			index = nextIndex
		case "--action":
			value, nextIndex, err := requiredValue(args, index, "--action")
			if err != nil {
				return BrowserCommand{}, err
			}
			command.Actions = []string{value}
			index = nextIndex
		case "--json":
			if command.JSON {
				return BrowserCommand{}, fmt.Errorf("duplicate --json flag")
			}
			command.JSON = true
		default:
			return BrowserCommand{}, fmt.Errorf("unknown browser argument: %s", args[index])
		}
	}
	if command.GoalID <= 0 {
		return BrowserCommand{}, fmt.Errorf("--goal-id is required")
	}
	if len(command.URLs) == 0 {
		return BrowserCommand{}, fmt.Errorf("--url is required")
	}
	if len(command.AllowedDomains) == 0 {
		host, err := browserURLHost(command.URL)
		if err != nil {
			return BrowserCommand{}, err
		}
		command.AllowedDomains = []string{host}
	}
	return command, nil
}

func browserURLHost(rawURL string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed == nil || parsed.Hostname() == "" {
		return "", fmt.Errorf("--url must be an absolute URL")
	}
	return strings.ToLower(parsed.Hostname()), nil
}
