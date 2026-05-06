package browserhandoff

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	RunnerModeEnvVar                 = "ODIN_BROWSER_HANDOFF_RUNNER"
	FixtureCommandEnvVar             = "ODIN_BROWSER_HANDOFF_FIXTURE_COMMAND"
	FixtureArgsEnvVar                = "ODIN_BROWSER_HANDOFF_FIXTURE_ARGS"
	FixtureAllowedCommandsEnvVar     = "ODIN_BROWSER_HANDOFF_FIXTURE_ALLOWED_COMMANDS"
	FixtureTimeoutSecondsEnvVar      = "ODIN_BROWSER_HANDOFF_FIXTURE_TIMEOUT_SECONDS"
	RunnerModeFixture                = "fixture"
	defaultFixtureErrorMessageLength = 512
)

type FixtureRunner struct {
	Enabled         bool
	Command         string
	Args            []string
	AllowedCommands []string
	TimeoutSeconds  int
}

func RunnerFromEnv() (Runner, error) {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv(RunnerModeEnvVar)))
	if mode == "" || mode == "stub" {
		return StubRunner{}, nil
	}
	if mode != RunnerModeFixture {
		return nil, fmt.Errorf("unsupported browser handoff runner mode %q", mode)
	}
	timeoutSeconds, err := fixtureTimeoutSecondsFromEnv()
	if err != nil {
		return nil, err
	}
	return FixtureRunner{
		Enabled:         true,
		Command:         strings.TrimSpace(os.Getenv(FixtureCommandEnvVar)),
		Args:            strings.Fields(os.Getenv(FixtureArgsEnvVar)),
		AllowedCommands: splitFixtureList(os.Getenv(FixtureAllowedCommandsEnvVar)),
		TimeoutSeconds:  timeoutSeconds,
	}, nil
}

func (runner FixtureRunner) Start(ctx context.Context, request StartRequest) (StartResponse, error) {
	if err := ValidateStartRequest(request); err != nil {
		return StartResponse{}, err
	}
	if !runner.Enabled {
		return StartResponse{}, fmt.Errorf("fixture runner must be explicitly enabled")
	}
	command, err := validateFixtureCommand(runner.Command, runner.AllowedCommands)
	if err != nil {
		return StartResponse{}, err
	}
	timeoutSeconds := runner.TimeoutSeconds
	if timeoutSeconds <= 0 {
		timeoutSeconds = request.TimeoutSeconds
	}
	if timeoutSeconds <= 0 {
		return StartResponse{}, fmt.Errorf("fixture runner timeout_seconds must be positive")
	}

	runCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(runCtx, command, runner.Args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return fixtureStartResponse(request, StatusFailed, 0, "fixture_start_failed", err.Error()), nil
	}
	processID := int64(cmd.Process.Pid)
	err = cmd.Wait()
	switch {
	case errors.Is(runCtx.Err(), context.DeadlineExceeded):
		return fixtureStartResponse(request, StatusExpired, processID, "fixture_timeout", "fixture runner command timed out"), nil
	case err != nil:
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return fixtureStartResponse(request, StatusFailed, processID, "fixture_command_failed", truncateFixtureMessage(message)), nil
	default:
		return StartResponse{
			Status:         StatusStarted,
			RunnerID:       fmt.Sprintf("fixture-%d", processID),
			ProcessID:      processID,
			SessionID:      request.SessionID,
			LoginRequestID: request.LoginRequestID,
			HandoffID:      strings.TrimSpace(request.HandoffID),
		}, nil
	}
}

func (runner FixtureRunner) Cancel(_ context.Context, request CancelRequest) (StatusResponse, error) {
	runnerID := strings.TrimSpace(request.RunnerID)
	if runnerID == "" {
		return StatusResponse{}, fmt.Errorf("runner_id is required")
	}
	return StatusResponse{
		Status:       StatusCancelled,
		RunnerID:     runnerID,
		ErrorCode:    "fixture_cancelled",
		ErrorMessage: "fixture runner cancellation recorded",
	}, nil
}

func fixtureStartResponse(request StartRequest, status string, processID int64, errorCode string, errorMessage string) StartResponse {
	response := StartResponse{
		Status:         status,
		SessionID:      request.SessionID,
		LoginRequestID: request.LoginRequestID,
		HandoffID:      strings.TrimSpace(request.HandoffID),
		ProcessID:      processID,
		ErrorCode:      errorCode,
		ErrorMessage:   errorMessage,
	}
	if processID > 0 {
		response.RunnerID = fmt.Sprintf("fixture-%d", processID)
	}
	return response
}

func validateFixtureCommand(command string, allowedCommands []string) (string, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return "", fmt.Errorf("fixture command is required")
	}
	if !filepath.IsAbs(command) {
		return "", fmt.Errorf("fixture command must be an absolute path")
	}
	cleanCommand := filepath.Clean(command)
	for _, allowed := range allowedCommands {
		if filepath.Clean(strings.TrimSpace(allowed)) == cleanCommand {
			return cleanCommand, nil
		}
	}
	return "", fmt.Errorf("fixture command %q is not in allowlist", cleanCommand)
}

func splitFixtureList(raw string) []string {
	parts := strings.Split(raw, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			values = append(values, part)
		}
	}
	return values
}

func fixtureTimeoutSecondsFromEnv() (int, error) {
	raw := strings.TrimSpace(os.Getenv(FixtureTimeoutSecondsEnvVar))
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", FixtureTimeoutSecondsEnvVar)
	}
	return value, nil
}

func truncateFixtureMessage(message string) string {
	message = strings.TrimSpace(message)
	if len(message) <= defaultFixtureErrorMessageLength {
		return message
	}
	return message[:defaultFixtureErrorMessageLength]
}
