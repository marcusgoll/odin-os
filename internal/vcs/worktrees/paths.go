package worktrees

import (
	"fmt"
	"path/filepath"
	"strings"
)

const defaultRoot = "~/.config/superpowers/worktrees/odin-os"

type Defaults struct {
	LocalDevelopmentRoot string
	ServerRuntimeRoot    string
}

type PathParams struct {
	Root       string
	ProjectKey string
	TaskID     int64
	RunID      int64
	Try        int
}

func DefaultRoot() string {
	return defaultRoot
}

func LongTermDefaults() Defaults {
	return Defaults{
		LocalDevelopmentRoot: "~/.local/share/superpowers/worktrees/odin-os",
		ServerRuntimeRoot:    "/var/odin/worktrees/odin-os",
	}
}

func ResolvePath(params PathParams) string {
	root := params.Root
	if root == "" {
		root = DefaultRoot()
	}

	projectKey := sanitizeProjectKey(params.ProjectKey)
	try := params.Try
	if try <= 0 {
		try = 1
	}

	return filepath.ToSlash(filepath.Join(
		root,
		projectKey,
		fmt.Sprintf("task-%d", params.TaskID),
		fmt.Sprintf("run-%d", params.RunID),
		fmt.Sprintf("try-%d", try),
	))
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
