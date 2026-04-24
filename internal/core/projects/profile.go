package projects

import (
	"os"
	"path/filepath"
	"strings"
)

type ProjectProfile struct {
	SpecFlowCompatible bool     `json:"spec_flow_compatible"`
	Evidence           []string `json:"evidence,omitempty"`
}

func DetectProjectProfile(gitRoot string) ProjectProfile {
	root := strings.TrimSpace(gitRoot)
	if root == "" {
		return ProjectProfile{}
	}

	signals := []string{}
	for _, candidate := range []struct {
		path string
		name string
	}{
		{".spec-flow", ".spec-flow/"},
		{filepath.Join(".claude", "commands"), ".claude/commands/"},
		{"CLAUDE.md", "CLAUDE.md"},
		{"specs", "specs/"},
		{"epics", "epics/"},
	} {
		if _, err := os.Stat(filepath.Join(root, candidate.path)); err == nil {
			signals = append(signals, candidate.name)
		}
	}

	return ProjectProfile{
		SpecFlowCompatible: len(signals) >= 3 && containsProfileSignal(signals, ".spec-flow/"),
		Evidence:           signals,
	}
}

func containsProfileSignal(signals []string, want string) bool {
	for _, signal := range signals {
		if signal == want {
			return true
		}
	}
	return false
}
