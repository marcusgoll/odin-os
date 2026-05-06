package commands

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

const BrowserUsage = "usage: odin browser run --goal-id <id> --url <url> [--objective <text>] [--allowed-domain <domain>] [--max-pages <n>] [--max-duration-seconds <n>] [--worker-mode <fetch|browser>] [--evidence-required] [--action <read|navigate|snapshot|extract>] [--json] | odin browser session create --name <name> --domain <domain> --permission-tier <tier> [--account-hint <hint>] [--profile-path <path>] [--json] | odin browser session list [--json] | odin browser session show --id <id> [--json] | odin browser session status --id <id> --status <status> [--json] | odin browser session revoke --id <id> [--json] | odin browser session login-request --id <id> [--handoff-base-url <url>] [--json] | odin browser session login-requests --id <id> [--json] | odin browser session handoff show --handoff-id <id> [--json] | odin browser session runner create --login-request-id <id> [--json] | odin browser session runner list --login-request-id <id> [--json] | odin browser session runner show --id <id> [--json] | odin browser session runner start --id <id> [--json] | odin browser session runner status --id <id> --status <status> [--json] | odin browser session runner cancel --id <id> [--json] | odin browser session verify --id <id> [--login-request-id <id>] [--json] | odin browser session prepare-profile --id <id> [--json]"

type BrowserCommand struct {
	Name               string
	SessionAction      string
	HandoffAction      string
	RunnerAction       string
	ID                 int64
	LoginRequestID     int64
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
	SessionName        string
	SessionDomain      string
	PermissionTier     string
	AccountHint        string
	ProfilePath        string
	HandoffID          string
	HandoffBaseURL     string
	Status             string
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
	if command.Name == "session" {
		return parseBrowserSession(args[1:], command)
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

func parseBrowserSession(args []string, command BrowserCommand) (BrowserCommand, error) {
	if len(args) == 0 {
		return BrowserCommand{}, fmt.Errorf(BrowserUsage)
	}
	command.SessionAction = strings.ToLower(strings.TrimSpace(args[0]))
	flagStart := 1
	if command.SessionAction == "handoff" {
		if len(args) < 2 || strings.HasPrefix(args[1], "--") {
			return BrowserCommand{}, fmt.Errorf("usage: odin browser session handoff show --handoff-id <id> [--json]")
		}
		command.HandoffAction = strings.ToLower(strings.TrimSpace(args[1]))
		flagStart = 2
		if command.HandoffAction != "show" {
			return BrowserCommand{}, fmt.Errorf("unsupported browser session handoff subcommand: %s", args[1])
		}
	}
	if command.SessionAction == "runner" {
		if len(args) < 2 || strings.HasPrefix(args[1], "--") {
			return BrowserCommand{}, fmt.Errorf("usage: odin browser session runner <create|list|show|start|status|cancel> [flags]")
		}
		command.RunnerAction = strings.ToLower(strings.TrimSpace(args[1]))
		flagStart = 2
		if !isKnownBrowserHandoffRunnerAction(command.RunnerAction) {
			return BrowserCommand{}, fmt.Errorf("unsupported browser session runner subcommand: %s", args[1])
		}
	}
	switch command.SessionAction {
	case "create", "list", "show", "status", "revoke", "login-request", "login-requests", "verify", "prepare-profile", "handoff", "runner":
	default:
		return BrowserCommand{}, fmt.Errorf("unsupported browser session subcommand: %s", args[0])
	}
	for index := flagStart; index < len(args); index++ {
		switch args[index] {
		case "--id":
			value, nextIndex, err := requiredValue(args, index, "--id")
			if err != nil {
				return BrowserCommand{}, err
			}
			id, err := strconv.ParseInt(value, 10, 64)
			if err != nil || id <= 0 {
				return BrowserCommand{}, fmt.Errorf("--id must be a positive integer")
			}
			command.ID = id
			index = nextIndex
		case "--login-request-id":
			value, nextIndex, err := requiredValue(args, index, "--login-request-id")
			if err != nil {
				return BrowserCommand{}, err
			}
			id, err := strconv.ParseInt(value, 10, 64)
			if err != nil || id <= 0 {
				return BrowserCommand{}, fmt.Errorf("--login-request-id must be a positive integer")
			}
			command.LoginRequestID = id
			index = nextIndex
		case "--name":
			value, nextIndex, err := requiredValue(args, index, "--name")
			if err != nil {
				return BrowserCommand{}, err
			}
			command.SessionName = value
			index = nextIndex
		case "--domain":
			value, nextIndex, err := requiredValue(args, index, "--domain")
			if err != nil {
				return BrowserCommand{}, err
			}
			command.SessionDomain = value
			index = nextIndex
		case "--permission-tier":
			value, nextIndex, err := requiredValue(args, index, "--permission-tier")
			if err != nil {
				return BrowserCommand{}, err
			}
			command.PermissionTier = strings.ToLower(strings.TrimSpace(value))
			if !isKnownBrowserSessionPermissionTier(command.PermissionTier) {
				return BrowserCommand{}, fmt.Errorf("--permission-tier must be public_readonly, authenticated_read, or authenticated_readonly")
			}
			index = nextIndex
		case "--account-hint":
			value, nextIndex, err := requiredValue(args, index, "--account-hint")
			if err != nil {
				return BrowserCommand{}, err
			}
			command.AccountHint = value
			index = nextIndex
		case "--profile-path":
			value, nextIndex, err := requiredValue(args, index, "--profile-path")
			if err != nil {
				return BrowserCommand{}, err
			}
			command.ProfilePath = value
			index = nextIndex
		case "--handoff-id":
			value, nextIndex, err := requiredValue(args, index, "--handoff-id")
			if err != nil {
				return BrowserCommand{}, err
			}
			if command.HandoffID != "" {
				return BrowserCommand{}, fmt.Errorf("duplicate --handoff-id flag")
			}
			command.HandoffID = strings.TrimSpace(value)
			index = nextIndex
		case "--handoff-base-url":
			value, nextIndex, err := requiredValue(args, index, "--handoff-base-url")
			if err != nil {
				return BrowserCommand{}, err
			}
			command.HandoffBaseURL = value
			index = nextIndex
		case "--status":
			value, nextIndex, err := requiredValue(args, index, "--status")
			if err != nil {
				return BrowserCommand{}, err
			}
			command.Status = strings.ToLower(strings.TrimSpace(value))
			if command.SessionAction == "runner" {
				if !isKnownBrowserHandoffRunnerStatus(command.Status) {
					return BrowserCommand{}, fmt.Errorf("--status must be started, completed, expired, cancelled, or failed")
				}
			} else if !isKnownBrowserSessionStatus(command.Status) {
				return BrowserCommand{}, fmt.Errorf("--status must be created, login_requested, verified, or expired")
			}
			index = nextIndex
		case "--json":
			if command.JSON {
				return BrowserCommand{}, fmt.Errorf("duplicate --json flag")
			}
			command.JSON = true
		default:
			return BrowserCommand{}, fmt.Errorf("unknown browser session argument: %s", args[index])
		}
	}
	switch command.SessionAction {
	case "create":
		if strings.TrimSpace(command.SessionName) == "" {
			return BrowserCommand{}, fmt.Errorf("--name is required")
		}
		if strings.TrimSpace(command.SessionDomain) == "" {
			return BrowserCommand{}, fmt.Errorf("--domain is required")
		}
		if strings.TrimSpace(command.PermissionTier) == "" {
			return BrowserCommand{}, fmt.Errorf("--permission-tier is required")
		}
		if command.ID != 0 || command.LoginRequestID != 0 || command.HandoffAction != "" || command.HandoffID != "" || command.HandoffBaseURL != "" || command.Status != "" {
			return BrowserCommand{}, fmt.Errorf("browser session create only accepts create fields and --json")
		}
	case "list":
		if command.ID != 0 || command.LoginRequestID != 0 || command.HandoffAction != "" || command.SessionName != "" || command.SessionDomain != "" || command.PermissionTier != "" || command.AccountHint != "" || command.ProfilePath != "" || command.HandoffID != "" || command.HandoffBaseURL != "" || command.Status != "" {
			return BrowserCommand{}, fmt.Errorf("browser session list only accepts --json")
		}
	case "show", "revoke", "login-requests", "prepare-profile":
		if command.ID <= 0 {
			return BrowserCommand{}, fmt.Errorf("--id is required")
		}
		if command.LoginRequestID != 0 || command.HandoffAction != "" || command.SessionName != "" || command.SessionDomain != "" || command.PermissionTier != "" || command.AccountHint != "" || command.ProfilePath != "" || command.HandoffID != "" || command.HandoffBaseURL != "" || command.Status != "" {
			return BrowserCommand{}, fmt.Errorf("browser session %s only accepts --id and --json", command.SessionAction)
		}
	case "login-request":
		if command.ID <= 0 {
			return BrowserCommand{}, fmt.Errorf("--id is required")
		}
		if command.LoginRequestID != 0 || command.HandoffAction != "" || command.SessionName != "" || command.SessionDomain != "" || command.PermissionTier != "" || command.AccountHint != "" || command.ProfilePath != "" || command.HandoffID != "" || command.Status != "" {
			return BrowserCommand{}, fmt.Errorf("browser session login-request only accepts --id, --handoff-base-url, and --json")
		}
	case "handoff":
		if command.HandoffAction != "show" {
			return BrowserCommand{}, fmt.Errorf("unsupported browser session handoff subcommand: %s", command.HandoffAction)
		}
		if strings.TrimSpace(command.HandoffID) == "" {
			return BrowserCommand{}, fmt.Errorf("--handoff-id is required")
		}
		if command.ID != 0 || command.LoginRequestID != 0 || command.SessionName != "" || command.SessionDomain != "" || command.PermissionTier != "" || command.AccountHint != "" || command.ProfilePath != "" || command.HandoffBaseURL != "" || command.Status != "" {
			return BrowserCommand{}, fmt.Errorf("browser session handoff show only accepts --handoff-id and --json")
		}
	case "runner":
		if command.HandoffAction != "" || command.SessionName != "" || command.SessionDomain != "" || command.PermissionTier != "" || command.AccountHint != "" || command.ProfilePath != "" || command.HandoffID != "" || command.HandoffBaseURL != "" {
			return BrowserCommand{}, fmt.Errorf("browser session runner %s only accepts runner fields and --json", command.RunnerAction)
		}
		switch command.RunnerAction {
		case "create", "list":
			if command.LoginRequestID <= 0 {
				return BrowserCommand{}, fmt.Errorf("--login-request-id is required")
			}
			if command.ID != 0 || command.Status != "" {
				return BrowserCommand{}, fmt.Errorf("browser session runner %s only accepts --login-request-id and --json", command.RunnerAction)
			}
		case "show", "start", "cancel":
			if command.ID <= 0 {
				return BrowserCommand{}, fmt.Errorf("--id is required")
			}
			if command.LoginRequestID != 0 || command.Status != "" {
				return BrowserCommand{}, fmt.Errorf("browser session runner %s only accepts --id and --json", command.RunnerAction)
			}
		case "status":
			if command.ID <= 0 {
				return BrowserCommand{}, fmt.Errorf("--id is required")
			}
			if command.Status == "" {
				return BrowserCommand{}, fmt.Errorf("--status is required")
			}
			if command.LoginRequestID != 0 {
				return BrowserCommand{}, fmt.Errorf("browser session runner status only accepts --id, --status, and --json")
			}
		default:
			return BrowserCommand{}, fmt.Errorf("unsupported browser session runner subcommand: %s", command.RunnerAction)
		}
	case "status":
		if command.ID <= 0 {
			return BrowserCommand{}, fmt.Errorf("--id is required")
		}
		if command.Status == "" {
			return BrowserCommand{}, fmt.Errorf("--status is required")
		}
		if command.LoginRequestID != 0 || command.HandoffAction != "" || command.SessionName != "" || command.SessionDomain != "" || command.PermissionTier != "" || command.AccountHint != "" || command.ProfilePath != "" || command.HandoffID != "" || command.HandoffBaseURL != "" {
			return BrowserCommand{}, fmt.Errorf("browser session status only accepts --id, --status, and --json")
		}
	case "verify":
		if command.ID <= 0 {
			return BrowserCommand{}, fmt.Errorf("--id is required")
		}
		if command.HandoffAction != "" || command.SessionName != "" || command.SessionDomain != "" || command.PermissionTier != "" || command.AccountHint != "" || command.ProfilePath != "" || command.HandoffID != "" || command.HandoffBaseURL != "" || command.Status != "" {
			return BrowserCommand{}, fmt.Errorf("browser session verify only accepts --id, --login-request-id, and --json")
		}
	}
	return command, nil
}

func isKnownBrowserHandoffRunnerAction(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "create", "list", "show", "start", "status", "cancel":
		return true
	default:
		return false
	}
}

func isKnownBrowserSessionPermissionTier(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "public_readonly", "authenticated_read", "authenticated_readonly":
		return true
	default:
		return false
	}
}

func isKnownBrowserSessionStatus(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "created", "login_requested", "verified", "expired":
		return true
	default:
		return false
	}
}

func isKnownBrowserHandoffRunnerStatus(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "started", "completed", "expired", "cancelled", "failed":
		return true
	default:
		return false
	}
}

func browserURLHost(rawURL string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed == nil || parsed.Hostname() == "" {
		return "", fmt.Errorf("--url must be an absolute URL")
	}
	return strings.ToLower(parsed.Hostname()), nil
}
