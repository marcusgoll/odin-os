package shell

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

type MediaSignal struct {
	Name    string            `json:"name"`
	Status  string            `json:"status"`
	Summary string            `json:"summary"`
	Details map[string]string `json:"details,omitempty"`
}

type MediaProbeOutput struct {
	Signals []MediaSignal `json:"signals"`
}

type CommandRunner interface {
	Run(context.Context, string) ([]byte, error)
}

type MediaProbe struct {
	Runner CommandRunner
}

func (probe MediaProbe) Run(ctx context.Context, command string) (MediaProbeOutput, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return MediaProbeOutput{}, nil
	}

	runner := probe.Runner
	if runner == nil {
		runner = execRunner{}
	}

	output, err := runner.Run(ctx, command)
	if err != nil {
		return MediaProbeOutput{}, err
	}

	var parsed MediaProbeOutput
	if err := json.Unmarshal(output, &parsed); err != nil {
		return MediaProbeOutput{}, err
	}
	return parsed, nil
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, command string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "bash", "-lc", command)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("media probe command failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return output, nil
}
