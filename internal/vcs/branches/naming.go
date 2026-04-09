package branches

import (
	"fmt"
	"strings"
)

type NameParams struct {
	ProjectKey string
	TaskID     int64
	RunID      int64
	Try        int
}

func Name(params NameParams) string {
	try := params.Try
	if try <= 0 {
		try = 1
	}

	return fmt.Sprintf(
		"odin/%s/task-%d/run-%d/try-%d",
		sanitizeProjectKey(params.ProjectKey),
		params.TaskID,
		params.RunID,
		try,
	)
}

func NextTry(branch string) string {
	parts := strings.Split(branch, "/")
	if len(parts) == 0 {
		return branch
	}

	last := parts[len(parts)-1]
	if !strings.HasPrefix(last, "try-") {
		return branch
	}

	var try int
	if _, err := fmt.Sscanf(last, "try-%d", &try); err != nil {
		return branch
	}

	parts[len(parts)-1] = fmt.Sprintf("try-%d", try+1)
	return strings.Join(parts, "/")
}

func sanitizeProjectKey(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var builder strings.Builder
	lastWasDash := false

	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			builder.WriteRune(r)
			lastWasDash = false
			continue
		}
		if !lastWasDash && builder.Len() > 0 {
			builder.WriteRune('-')
			lastWasDash = true
		}
	}

	sanitized := strings.Trim(builder.String(), "-")
	if sanitized == "" {
		return "project"
	}
	return sanitized
}
