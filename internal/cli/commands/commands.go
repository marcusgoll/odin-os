package commands

import "strings"

type Command struct {
	Name string
	Args []string
}

type RegistryCommand struct {
	CapabilityID      string
	CapabilityVersion string
}

type Intent string

const (
	IntentUnknown   Intent = "unknown"
	IntentHelp      Intent = "help"
	IntentMode      Intent = "mode"
	IntentScope     Intent = "scope"
	IntentProject   Intent = "project"
	IntentJobs      Intent = "jobs"
	IntentRuns      Intent = "runs"
	IntentApprovals Intent = "approvals"
	IntentLogs      Intent = "logs"
	IntentDoctor    Intent = "doctor"
)

var registryCommands = map[string]RegistryCommand{
	"status": {
		CapabilityID:      "project.status",
		CapabilityVersion: "1.0.0",
	},
	"stat": {
		CapabilityID:      "project.status",
		CapabilityVersion: "1.0.0",
	},
}

func Parse(line string) (Command, bool) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "/") {
		return Command{}, false
	}

	fields := strings.Fields(strings.TrimPrefix(line, "/"))
	if len(fields) == 0 {
		return Command{}, false
	}

	return Command{
		Name: strings.ToLower(fields[0]),
		Args: fields[1:],
	}, true
}

func ResolveRegistryCommand(command Command) (RegistryCommand, bool) {
	resolved, ok := registryCommands[command.Name]
	if !ok {
		return RegistryCommand{}, false
	}
	return resolved, true
}

func RouteAskIntent(line string) Intent {
	normalized := strings.ToLower(strings.TrimSpace(line))

	switch {
	case normalized == "help" || strings.Contains(normalized, "help"):
		return IntentHelp
	case strings.Contains(normalized, "mode"):
		return IntentMode
	case strings.Contains(normalized, "scope"):
		return IntentScope
	case strings.Contains(normalized, "project") || strings.Contains(normalized, "self"):
		return IntentProject
	case strings.Contains(normalized, "job") || strings.Contains(normalized, "task"):
		return IntentJobs
	case strings.Contains(normalized, "run"):
		return IntentRuns
	case strings.Contains(normalized, "approval"):
		return IntentApprovals
	case strings.Contains(normalized, "log"):
		return IntentLogs
	case strings.Contains(normalized, "doctor") || strings.Contains(normalized, "health"):
		return IntentDoctor
	default:
		return IntentUnknown
	}
}
