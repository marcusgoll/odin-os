package commands

import "strings"

type Command struct {
	Name string
	Args []string
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

func RouteAskIntent(line string) Intent {
	normalized := strings.ToLower(strings.TrimSpace(line))
	if normalized == "" {
		return IntentUnknown
	}
	tokens := askTokens(normalized)

	switch {
	case normalized == "help" || hasPhrase(normalized, "show help", "list commands", "show commands"):
		return IntentHelp
	case hasToken(tokens, "mode") && looksLikeStateQuestion(normalized):
		return IntentMode
	case hasToken(tokens, "scope") && looksLikeStateQuestion(normalized):
		return IntentScope
	case (hasToken(tokens, "project") || hasToken(tokens, "self")) && looksLikeStateQuestion(normalized):
		return IntentProject
	case looksLikeListing(normalized) && hasToken(tokens, "job", "jobs", "task", "tasks"):
		return IntentJobs
	case looksLikeListing(normalized) && hasToken(tokens, "run", "runs"):
		return IntentRuns
	case (looksLikeListing(normalized) && hasToken(tokens, "approval", "approvals")) || (hasToken(tokens, "approval", "approvals") && hasToken(tokens, "waiting", "pending")):
		return IntentApprovals
	case looksLikeListing(normalized) && hasToken(tokens, "log", "logs"):
		return IntentLogs
	case normalized == "doctor" || strings.HasPrefix(normalized, "doctor ") || (hasToken(tokens, "health") && looksLikeStateQuestion(normalized)):
		return IntentDoctor
	default:
		return IntentUnknown
	}
}

func askTokens(normalized string) []string {
	return strings.FieldsFunc(normalized, func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9')
	})
}

func hasToken(tokens []string, want ...string) bool {
	for _, token := range tokens {
		for _, candidate := range want {
			if token == candidate {
				return true
			}
		}
	}
	return false
}

func looksLikeStateQuestion(normalized string) bool {
	return hasPhrase(normalized,
		"what ",
		"which ",
		"show ",
		"current ",
		"am i in",
		"selected",
	)
}

func looksLikeListing(normalized string) bool {
	return hasPhrase(normalized,
		"show ",
		"list ",
		"display ",
		"view ",
		"what ",
		"which ",
		"current ",
		"pending ",
		"any ",
	)
}

func hasPhrase(normalized string, prefixes ...string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(normalized, prefix) || strings.Contains(normalized, " "+prefix) {
			return true
		}
	}
	return false
}
