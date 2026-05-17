package commands

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

const BrowserUsage = "usage: odin browser run (--goal-id <id>|--task-id <id>) --url <url> [--objective <text>] [--allowed-domain <domain>] [--session-id <id>] [--max-pages <n>] [--max-duration-seconds <n>] [--worker-mode <fetch|browser>] [--evidence-required] [--action <read|navigate|snapshot|extract>] [--json] | odin browser session create --name <name> --domain <domain> --permission-tier <tier> [--account-hint <hint>] [--profile-path <path>] [--json] | odin browser session list [--json] | odin browser session show --id <id> [--json] | odin browser session status --id <id> --status <status> [--json] | odin browser session revoke --id <id> [--json] | odin browser session login-request --id <id> [--handoff-base-url <url>] [--json] | odin browser session login-requests --id <id> [--json] | odin browser session handoff show --handoff-id <id> [--json] | odin browser session prove --id <id> --url <url> --expect-title <title> [--json] | odin browser session runner create --login-request-id <id> [--json] | odin browser session runner list --login-request-id <id> [--json] | odin browser session runner show --id <id> [--json] | odin browser session runner start --id <id> [--json] | odin browser session runner plan-novnc --id <id> --browser-command <path> --browser-allowed-command <path> --display-command <path> --display-allowed-command <path> --novnc-command <path> --novnc-allowed-command <path> --bind-addr <addr> --private-base-url <url> --timeout-seconds <n> [--json] | odin browser session runner status --id <id> --status <status> [--json] | odin browser session runner cancel --id <id> [--json] | odin browser session verify --id <id> [--login-request-id <id>] [--json] | odin browser session prepare-profile --id <id> [--json] | odin browser session profile retention cleanup [--session-id <id>] [--apply] [--json] | odin browser session profile artifact create-fixture --session-id <id> --name <safe-name> --plaintext-file <path> [--json] | odin browser session profile artifact create-directory --session-id <id> --name <safe-name> --source-dir <path> [--json] | odin browser session profile artifact list --session-id <id> [--json] | odin browser session profile artifact show --id <id> [--json] | odin browser session profile artifact revoke --id <id> [--json] | odin browser session profile artifact materialize --id <id> --target-dir <path> [--json] | odin browser session profile artifact materialize-directory --id <id> --target-dir <path> [--json] | odin browser session profile artifact cleanup-materialization --id <id> --target-dir <path> [--json]"

type BrowserCommand struct {
	Name                        string
	SessionAction               string
	HandoffAction               string
	RunnerAction                string
	ProfileAction               string
	RetentionAction             string
	ArtifactAction              string
	ID                          int64
	SessionID                   int64
	LoginRequestID              int64
	GoalID                      int64
	TaskID                      int64
	URL                         string
	URLs                        []string
	Objective                   string
	AllowedDomains              []string
	MaxPages                    int
	MaxDurationSeconds          int
	WorkerMode                  string
	EvidenceRequired            bool
	Actions                     []string
	SessionName                 string
	SessionDomain               string
	PermissionTier              string
	AccountHint                 string
	ArtifactName                string
	PlaintextFile               string
	SourceDir                   string
	TargetDir                   string
	ProfilePath                 string
	HandoffID                   string
	HandoffBaseURL              string
	Status                      string
	ExpectedTitle               string
	NoVNCBrowserCommand         string
	NoVNCBrowserAllowedCommands []string
	NoVNCDisplayCommand         string
	NoVNCDisplayAllowedCommands []string
	NoVNCCommand                string
	NoVNCAllowedCommands        []string
	NoVNCBindAddr               string
	NoVNCPrivateBaseURL         string
	NoVNCTimeoutSeconds         int
	Apply                       bool
	JSON                        bool
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
		case "--task-id":
			value, nextIndex, err := requiredValue(args, index, "--task-id")
			if err != nil {
				return BrowserCommand{}, err
			}
			taskID, err := strconv.ParseInt(value, 10, 64)
			if err != nil || taskID <= 0 {
				return BrowserCommand{}, fmt.Errorf("--task-id must be a positive integer")
			}
			command.TaskID = taskID
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
		case "--session-id":
			value, nextIndex, err := requiredValue(args, index, "--session-id")
			if err != nil {
				return BrowserCommand{}, err
			}
			id, err := strconv.ParseInt(value, 10, 64)
			if err != nil || id <= 0 {
				return BrowserCommand{}, fmt.Errorf("--session-id must be a positive integer")
			}
			command.SessionID = id
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
	if command.GoalID <= 0 && command.TaskID <= 0 {
		return BrowserCommand{}, fmt.Errorf("--goal-id or --task-id is required")
	}
	if command.GoalID > 0 && command.TaskID > 0 {
		return BrowserCommand{}, fmt.Errorf("--goal-id and --task-id are mutually exclusive")
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
			return BrowserCommand{}, fmt.Errorf("usage: odin browser session runner <create|list|show|start|plan-novnc|status|cancel> [flags]")
		}
		command.RunnerAction = strings.ToLower(strings.TrimSpace(args[1]))
		flagStart = 2
		if !isKnownBrowserHandoffRunnerAction(command.RunnerAction) {
			return BrowserCommand{}, fmt.Errorf("unsupported browser session runner subcommand: %s", args[1])
		}
	}
	if command.SessionAction == "profile" {
		if len(args) < 3 || strings.HasPrefix(args[1], "--") || strings.HasPrefix(args[2], "--") {
			return BrowserCommand{}, fmt.Errorf("usage: odin browser session profile <retention cleanup|artifact create-fixture> [flags]")
		}
		command.ProfileAction = strings.ToLower(strings.TrimSpace(args[1]))
		flagStart = 3
		switch command.ProfileAction {
		case "retention":
			command.RetentionAction = strings.ToLower(strings.TrimSpace(args[2]))
			if command.RetentionAction != "cleanup" {
				return BrowserCommand{}, fmt.Errorf("unsupported browser session profile subcommand: %s %s", args[1], args[2])
			}
		case "artifact":
			command.ArtifactAction = strings.ToLower(strings.TrimSpace(args[2]))
			if !isKnownBrowserProfileArtifactAction(command.ArtifactAction) {
				return BrowserCommand{}, fmt.Errorf("unsupported browser session profile subcommand: %s %s", args[1], args[2])
			}
		default:
			return BrowserCommand{}, fmt.Errorf("unsupported browser session profile subcommand: %s %s", args[1], args[2])
		}
	}
	switch command.SessionAction {
	case "create", "list", "show", "status", "revoke", "login-request", "login-requests", "verify", "prepare-profile", "prove", "handoff", "runner", "profile":
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
		case "--session-id":
			value, nextIndex, err := requiredValue(args, index, "--session-id")
			if err != nil {
				return BrowserCommand{}, err
			}
			id, err := strconv.ParseInt(value, 10, 64)
			if err != nil || id <= 0 {
				return BrowserCommand{}, fmt.Errorf("--session-id must be a positive integer")
			}
			command.SessionID = id
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
			if command.SessionAction == "profile" && command.ProfileAction == "artifact" {
				command.ArtifactName = value
			} else {
				command.SessionName = value
			}
			index = nextIndex
		case "--plaintext-file":
			value, nextIndex, err := requiredValue(args, index, "--plaintext-file")
			if err != nil {
				return BrowserCommand{}, err
			}
			command.PlaintextFile = value
			index = nextIndex
		case "--source-dir":
			value, nextIndex, err := requiredValue(args, index, "--source-dir")
			if err != nil {
				return BrowserCommand{}, err
			}
			command.SourceDir = value
			index = nextIndex
		case "--target-dir":
			value, nextIndex, err := requiredValue(args, index, "--target-dir")
			if err != nil {
				return BrowserCommand{}, err
			}
			command.TargetDir = value
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
		case "--url":
			value, nextIndex, err := requiredValue(args, index, "--url")
			if err != nil {
				return BrowserCommand{}, err
			}
			command.URL = value
			index = nextIndex
		case "--expect-title":
			value, nextIndex, err := requiredValue(args, index, "--expect-title")
			if err != nil {
				return BrowserCommand{}, err
			}
			command.ExpectedTitle = value
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
				return BrowserCommand{}, fmt.Errorf("--status must be created, login_requested, requires_attended_login, verified, or expired")
			}
			index = nextIndex
		case "--browser-command":
			value, nextIndex, err := requiredValue(args, index, "--browser-command")
			if err != nil {
				return BrowserCommand{}, err
			}
			command.NoVNCBrowserCommand = value
			index = nextIndex
		case "--browser-allowed-command":
			value, nextIndex, err := requiredValue(args, index, "--browser-allowed-command")
			if err != nil {
				return BrowserCommand{}, err
			}
			command.NoVNCBrowserAllowedCommands = append(command.NoVNCBrowserAllowedCommands, value)
			index = nextIndex
		case "--display-command":
			value, nextIndex, err := requiredValue(args, index, "--display-command")
			if err != nil {
				return BrowserCommand{}, err
			}
			command.NoVNCDisplayCommand = value
			index = nextIndex
		case "--display-allowed-command":
			value, nextIndex, err := requiredValue(args, index, "--display-allowed-command")
			if err != nil {
				return BrowserCommand{}, err
			}
			command.NoVNCDisplayAllowedCommands = append(command.NoVNCDisplayAllowedCommands, value)
			index = nextIndex
		case "--novnc-command":
			value, nextIndex, err := requiredValue(args, index, "--novnc-command")
			if err != nil {
				return BrowserCommand{}, err
			}
			command.NoVNCCommand = value
			index = nextIndex
		case "--novnc-allowed-command":
			value, nextIndex, err := requiredValue(args, index, "--novnc-allowed-command")
			if err != nil {
				return BrowserCommand{}, err
			}
			command.NoVNCAllowedCommands = append(command.NoVNCAllowedCommands, value)
			index = nextIndex
		case "--bind-addr":
			value, nextIndex, err := requiredValue(args, index, "--bind-addr")
			if err != nil {
				return BrowserCommand{}, err
			}
			command.NoVNCBindAddr = value
			index = nextIndex
		case "--private-base-url":
			value, nextIndex, err := requiredValue(args, index, "--private-base-url")
			if err != nil {
				return BrowserCommand{}, err
			}
			command.NoVNCPrivateBaseURL = value
			index = nextIndex
		case "--timeout-seconds":
			value, nextIndex, err := requiredValue(args, index, "--timeout-seconds")
			if err != nil {
				return BrowserCommand{}, err
			}
			timeoutSeconds, err := strconv.Atoi(value)
			if err != nil || timeoutSeconds <= 0 {
				return BrowserCommand{}, fmt.Errorf("--timeout-seconds must be a positive integer")
			}
			command.NoVNCTimeoutSeconds = timeoutSeconds
			index = nextIndex
		case "--apply":
			if command.Apply {
				return BrowserCommand{}, fmt.Errorf("duplicate --apply flag")
			}
			command.Apply = true
		case "--json":
			if command.JSON {
				return BrowserCommand{}, fmt.Errorf("duplicate --json flag")
			}
			command.JSON = true
		default:
			return BrowserCommand{}, fmt.Errorf("unknown browser session argument: %s", args[index])
		}
	}
	if command.SessionAction != "prove" && (strings.TrimSpace(command.URL) != "" || strings.TrimSpace(command.ExpectedTitle) != "") {
		return BrowserCommand{}, fmt.Errorf("browser session %s does not accept --url or --expect-title", command.SessionAction)
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
		if command.ID != 0 || command.SessionID != 0 || command.LoginRequestID != 0 || command.HandoffAction != "" || command.ProfileAction != "" || command.RetentionAction != "" || command.ArtifactAction != "" || command.ArtifactName != "" || command.PlaintextFile != "" || command.TargetDir != "" || command.HandoffID != "" || command.HandoffBaseURL != "" || command.Status != "" || command.Apply {
			return BrowserCommand{}, fmt.Errorf("browser session create only accepts create fields and --json")
		}
	case "list":
		if command.ID != 0 || command.SessionID != 0 || command.LoginRequestID != 0 || command.HandoffAction != "" || command.ProfileAction != "" || command.RetentionAction != "" || command.ArtifactAction != "" || command.SessionName != "" || command.SessionDomain != "" || command.PermissionTier != "" || command.AccountHint != "" || command.ArtifactName != "" || command.PlaintextFile != "" || command.SourceDir != "" || command.TargetDir != "" || command.ProfilePath != "" || command.HandoffID != "" || command.HandoffBaseURL != "" || command.Status != "" || command.Apply {
			return BrowserCommand{}, fmt.Errorf("browser session list only accepts --json")
		}
	case "show", "revoke", "login-requests", "prepare-profile":
		if command.ID <= 0 {
			return BrowserCommand{}, fmt.Errorf("--id is required")
		}
		if command.SessionID != 0 || command.LoginRequestID != 0 || command.HandoffAction != "" || command.ProfileAction != "" || command.RetentionAction != "" || command.ArtifactAction != "" || command.SessionName != "" || command.SessionDomain != "" || command.PermissionTier != "" || command.AccountHint != "" || command.ArtifactName != "" || command.PlaintextFile != "" || command.SourceDir != "" || command.TargetDir != "" || command.ProfilePath != "" || command.HandoffID != "" || command.HandoffBaseURL != "" || command.Status != "" || command.Apply {
			return BrowserCommand{}, fmt.Errorf("browser session %s only accepts --id and --json", command.SessionAction)
		}
	case "login-request":
		if command.ID <= 0 {
			return BrowserCommand{}, fmt.Errorf("--id is required")
		}
		if command.SessionID != 0 || command.LoginRequestID != 0 || command.HandoffAction != "" || command.ProfileAction != "" || command.RetentionAction != "" || command.ArtifactAction != "" || command.SessionName != "" || command.SessionDomain != "" || command.PermissionTier != "" || command.AccountHint != "" || command.ArtifactName != "" || command.PlaintextFile != "" || command.SourceDir != "" || command.TargetDir != "" || command.ProfilePath != "" || command.HandoffID != "" || command.Status != "" || command.Apply {
			return BrowserCommand{}, fmt.Errorf("browser session login-request only accepts --id, --handoff-base-url, and --json")
		}
	case "prove":
		if command.ID <= 0 {
			return BrowserCommand{}, fmt.Errorf("--id is required")
		}
		if strings.TrimSpace(command.URL) == "" {
			return BrowserCommand{}, fmt.Errorf("--url is required")
		}
		if strings.TrimSpace(command.ExpectedTitle) == "" {
			return BrowserCommand{}, fmt.Errorf("--expect-title is required")
		}
		if _, err := browserURLHost(command.URL); err != nil {
			return BrowserCommand{}, err
		}
		if command.SessionID != 0 || command.LoginRequestID != 0 || command.HandoffAction != "" || command.ProfileAction != "" || command.RetentionAction != "" || command.ArtifactAction != "" || command.SessionName != "" || command.SessionDomain != "" || command.PermissionTier != "" || command.AccountHint != "" || command.ArtifactName != "" || command.PlaintextFile != "" || command.SourceDir != "" || command.TargetDir != "" || command.ProfilePath != "" || command.HandoffID != "" || command.HandoffBaseURL != "" || command.Status != "" || command.Apply || command.hasNoVNCPlanConfig() {
			return BrowserCommand{}, fmt.Errorf("browser session prove only accepts --id, --url, --expect-title, and --json")
		}
	case "handoff":
		if command.HandoffAction != "show" {
			return BrowserCommand{}, fmt.Errorf("unsupported browser session handoff subcommand: %s", command.HandoffAction)
		}
		if strings.TrimSpace(command.HandoffID) == "" {
			return BrowserCommand{}, fmt.Errorf("--handoff-id is required")
		}
		if command.ID != 0 || command.SessionID != 0 || command.LoginRequestID != 0 || command.SessionName != "" || command.SessionDomain != "" || command.PermissionTier != "" || command.AccountHint != "" || command.ArtifactName != "" || command.PlaintextFile != "" || command.SourceDir != "" || command.TargetDir != "" || command.ProfilePath != "" || command.HandoffBaseURL != "" || command.Status != "" || command.Apply {
			return BrowserCommand{}, fmt.Errorf("browser session handoff show only accepts --handoff-id and --json")
		}
	case "runner":
		if command.SessionID != 0 || command.HandoffAction != "" || command.ProfileAction != "" || command.RetentionAction != "" || command.ArtifactAction != "" || command.SessionName != "" || command.SessionDomain != "" || command.PermissionTier != "" || command.AccountHint != "" || command.ArtifactName != "" || command.PlaintextFile != "" || command.SourceDir != "" || command.TargetDir != "" || command.ProfilePath != "" || command.HandoffID != "" || command.HandoffBaseURL != "" || command.Apply {
			return BrowserCommand{}, fmt.Errorf("browser session runner %s only accepts runner fields and --json", command.RunnerAction)
		}
		switch command.RunnerAction {
		case "create", "list":
			if command.LoginRequestID <= 0 {
				return BrowserCommand{}, fmt.Errorf("--login-request-id is required")
			}
			if command.ID != 0 || command.Status != "" || command.hasNoVNCPlanConfig() {
				return BrowserCommand{}, fmt.Errorf("browser session runner %s only accepts --login-request-id and --json", command.RunnerAction)
			}
		case "show", "start", "cancel":
			if command.ID <= 0 {
				return BrowserCommand{}, fmt.Errorf("--id is required")
			}
			if command.LoginRequestID != 0 || command.Status != "" || command.hasNoVNCPlanConfig() {
				return BrowserCommand{}, fmt.Errorf("browser session runner %s only accepts --id and --json", command.RunnerAction)
			}
		case "plan-novnc":
			if command.ID <= 0 {
				return BrowserCommand{}, fmt.Errorf("--id is required")
			}
			if command.LoginRequestID != 0 || command.Status != "" {
				return BrowserCommand{}, fmt.Errorf("browser session runner plan-novnc only accepts --id, NoVNC config fields, and --json")
			}
		case "status":
			if command.ID <= 0 {
				return BrowserCommand{}, fmt.Errorf("--id is required")
			}
			if command.Status == "" {
				return BrowserCommand{}, fmt.Errorf("--status is required")
			}
			if command.LoginRequestID != 0 || command.hasNoVNCPlanConfig() {
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
		if command.SessionID != 0 || command.LoginRequestID != 0 || command.HandoffAction != "" || command.ProfileAction != "" || command.RetentionAction != "" || command.ArtifactAction != "" || command.SessionName != "" || command.SessionDomain != "" || command.PermissionTier != "" || command.AccountHint != "" || command.ArtifactName != "" || command.PlaintextFile != "" || command.SourceDir != "" || command.TargetDir != "" || command.ProfilePath != "" || command.HandoffID != "" || command.HandoffBaseURL != "" || command.Apply {
			return BrowserCommand{}, fmt.Errorf("browser session status only accepts --id, --status, and --json")
		}
	case "verify":
		if command.ID <= 0 {
			return BrowserCommand{}, fmt.Errorf("--id is required")
		}
		if command.SessionID != 0 || command.HandoffAction != "" || command.ProfileAction != "" || command.RetentionAction != "" || command.ArtifactAction != "" || command.SessionName != "" || command.SessionDomain != "" || command.PermissionTier != "" || command.AccountHint != "" || command.ArtifactName != "" || command.PlaintextFile != "" || command.SourceDir != "" || command.TargetDir != "" || command.ProfilePath != "" || command.HandoffID != "" || command.HandoffBaseURL != "" || command.Status != "" || command.Apply {
			return BrowserCommand{}, fmt.Errorf("browser session verify only accepts --id, --login-request-id, and --json")
		}
	case "profile":
		switch command.ProfileAction {
		case "retention":
			if command.RetentionAction != "cleanup" {
				return BrowserCommand{}, fmt.Errorf("unsupported browser session profile subcommand: %s %s", command.ProfileAction, command.RetentionAction)
			}
			if command.ID != 0 || command.LoginRequestID != 0 || command.HandoffAction != "" || command.RunnerAction != "" || command.ArtifactAction != "" || command.SessionName != "" || command.SessionDomain != "" || command.PermissionTier != "" || command.AccountHint != "" || command.ArtifactName != "" || command.PlaintextFile != "" || command.SourceDir != "" || command.TargetDir != "" || command.ProfilePath != "" || command.HandoffID != "" || command.HandoffBaseURL != "" || command.Status != "" || command.hasNoVNCPlanConfig() {
				return BrowserCommand{}, fmt.Errorf("browser session profile retention cleanup only accepts --session-id, --apply, and --json")
			}
		case "artifact":
			if !isKnownBrowserProfileArtifactAction(command.ArtifactAction) {
				return BrowserCommand{}, fmt.Errorf("unsupported browser session profile subcommand: %s %s", command.ProfileAction, command.ArtifactAction)
			}
			switch command.ArtifactAction {
			case "create-fixture":
				if command.SessionID <= 0 {
					return BrowserCommand{}, fmt.Errorf("--session-id is required")
				}
				if strings.TrimSpace(command.ArtifactName) == "" {
					return BrowserCommand{}, fmt.Errorf("--name is required")
				}
				if strings.TrimSpace(command.PlaintextFile) == "" {
					return BrowserCommand{}, fmt.Errorf("--plaintext-file is required")
				}
				if command.ID != 0 || command.LoginRequestID != 0 || command.HandoffAction != "" || command.RunnerAction != "" || command.RetentionAction != "" || command.SessionName != "" || command.SessionDomain != "" || command.PermissionTier != "" || command.AccountHint != "" || command.SourceDir != "" || command.TargetDir != "" || command.ProfilePath != "" || command.HandoffID != "" || command.HandoffBaseURL != "" || command.Status != "" || command.Apply || command.hasNoVNCPlanConfig() {
					return BrowserCommand{}, fmt.Errorf("browser session profile artifact create-fixture only accepts --session-id, --name, --plaintext-file, and --json")
				}
			case "create-directory":
				if command.SessionID <= 0 {
					return BrowserCommand{}, fmt.Errorf("--session-id is required")
				}
				if strings.TrimSpace(command.ArtifactName) == "" {
					return BrowserCommand{}, fmt.Errorf("--name is required")
				}
				if strings.TrimSpace(command.SourceDir) == "" {
					return BrowserCommand{}, fmt.Errorf("--source-dir is required")
				}
				if command.ID != 0 || command.LoginRequestID != 0 || command.HandoffAction != "" || command.RunnerAction != "" || command.RetentionAction != "" || command.SessionName != "" || command.SessionDomain != "" || command.PermissionTier != "" || command.AccountHint != "" || command.PlaintextFile != "" || command.TargetDir != "" || command.ProfilePath != "" || command.HandoffID != "" || command.HandoffBaseURL != "" || command.Status != "" || command.Apply || command.hasNoVNCPlanConfig() {
					return BrowserCommand{}, fmt.Errorf("browser session profile artifact create-directory only accepts --session-id, --name, --source-dir, and --json")
				}
			case "list":
				if command.SessionID <= 0 {
					return BrowserCommand{}, fmt.Errorf("--session-id is required")
				}
				if command.ID != 0 || command.LoginRequestID != 0 || command.HandoffAction != "" || command.RunnerAction != "" || command.RetentionAction != "" || command.SessionName != "" || command.SessionDomain != "" || command.PermissionTier != "" || command.AccountHint != "" || command.ArtifactName != "" || command.PlaintextFile != "" || command.SourceDir != "" || command.TargetDir != "" || command.ProfilePath != "" || command.HandoffID != "" || command.HandoffBaseURL != "" || command.Status != "" || command.Apply || command.hasNoVNCPlanConfig() {
					return BrowserCommand{}, fmt.Errorf("browser session profile artifact list only accepts --session-id and --json")
				}
			case "show", "revoke":
				if command.ID <= 0 {
					return BrowserCommand{}, fmt.Errorf("--id is required")
				}
				if command.SessionID != 0 || command.LoginRequestID != 0 || command.HandoffAction != "" || command.RunnerAction != "" || command.RetentionAction != "" || command.SessionName != "" || command.SessionDomain != "" || command.PermissionTier != "" || command.AccountHint != "" || command.ArtifactName != "" || command.PlaintextFile != "" || command.SourceDir != "" || command.TargetDir != "" || command.ProfilePath != "" || command.HandoffID != "" || command.HandoffBaseURL != "" || command.Status != "" || command.Apply || command.hasNoVNCPlanConfig() {
					return BrowserCommand{}, fmt.Errorf("browser session profile artifact %s only accepts --id and --json", command.ArtifactAction)
				}
			case "materialize", "materialize-directory", "cleanup-materialization":
				if command.ID <= 0 {
					return BrowserCommand{}, fmt.Errorf("--id is required")
				}
				if strings.TrimSpace(command.TargetDir) == "" {
					return BrowserCommand{}, fmt.Errorf("--target-dir is required")
				}
				if command.SessionID != 0 || command.LoginRequestID != 0 || command.HandoffAction != "" || command.RunnerAction != "" || command.RetentionAction != "" || command.SessionName != "" || command.SessionDomain != "" || command.PermissionTier != "" || command.AccountHint != "" || command.ArtifactName != "" || command.PlaintextFile != "" || command.SourceDir != "" || command.ProfilePath != "" || command.HandoffID != "" || command.HandoffBaseURL != "" || command.Status != "" || command.Apply || command.hasNoVNCPlanConfig() {
					return BrowserCommand{}, fmt.Errorf("browser session profile artifact %s only accepts --id, --target-dir, and --json", command.ArtifactAction)
				}
			}
		default:
			return BrowserCommand{}, fmt.Errorf("unsupported browser session profile subcommand: %s", command.ProfileAction)
		}
	}
	return command, nil
}

func isKnownBrowserHandoffRunnerAction(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "create", "list", "show", "start", "plan-novnc", "status", "cancel":
		return true
	default:
		return false
	}
}

func isKnownBrowserProfileArtifactAction(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "create-fixture", "create-directory", "list", "show", "revoke", "materialize", "materialize-directory", "cleanup-materialization":
		return true
	default:
		return false
	}
}

func (command BrowserCommand) hasNoVNCPlanConfig() bool {
	return command.NoVNCBrowserCommand != "" ||
		len(command.NoVNCBrowserAllowedCommands) > 0 ||
		command.NoVNCDisplayCommand != "" ||
		len(command.NoVNCDisplayAllowedCommands) > 0 ||
		command.NoVNCCommand != "" ||
		len(command.NoVNCAllowedCommands) > 0 ||
		command.NoVNCBindAddr != "" ||
		command.NoVNCPrivateBaseURL != "" ||
		command.NoVNCTimeoutSeconds != 0
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
	case "created", "login_requested", "requires_attended_login", "verified", "expired":
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
