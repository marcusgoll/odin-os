package browserhandoff

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

type ProcessStatus string

const (
	ProcessStatusStarted   ProcessStatus = "started"
	ProcessStatusExited    ProcessStatus = "exited"
	ProcessStatusFailed    ProcessStatus = "failed"
	ProcessStatusTimeout   ProcessStatus = "timeout"
	ProcessStatusCancelled ProcessStatus = "cancelled"
)

type StartProcessRequest struct {
	Role             string   `json:"role"`
	CommandPath      string   `json:"command_path"`
	Args             []string `json:"args,omitempty"`
	Env              []string `json:"env,omitempty"`
	WorkingDirectory string   `json:"working_directory,omitempty"`
	TimeoutSeconds   int      `json:"timeout_seconds"`
	AllowedCommands  []string `json:"allowed_commands,omitempty"`
}

type ProcessHandle struct {
	PID         int64         `json:"pid"`
	Role        string        `json:"role"`
	CommandPath string        `json:"command_path"`
	StartedAt   time.Time     `json:"started_at"`
	Status      ProcessStatus `json:"status"`
}

type ProcessResult struct {
	PID          int64         `json:"pid"`
	Role         string        `json:"role"`
	CommandPath  string        `json:"command_path"`
	StartedAt    time.Time     `json:"started_at"`
	ExitedAt     *time.Time    `json:"exited_at,omitempty"`
	Status       ProcessStatus `json:"status"`
	Stdout       string        `json:"stdout,omitempty"`
	Stderr       string        `json:"stderr,omitempty"`
	ErrorMessage string        `json:"error_message,omitempty"`
}

type ProcessCommandRunner interface {
	Start(context.Context, StartProcessRequest) (int64, error)
	Wait(context.Context, ProcessHandle) (ProcessResult, error)
	Cancel(context.Context, ProcessHandle) error
}

type ProcessSupervisor interface {
	StartProcess(context.Context, StartProcessRequest) (ProcessHandle, error)
	WaitProcess(context.Context, ProcessHandle) (ProcessResult, error)
	CancelProcess(context.Context, ProcessHandle, string) (ProcessResult, error)
}

type BoundedProcessSupervisor struct {
	Runner ProcessCommandRunner
	Now    func() time.Time
}

func (supervisor BoundedProcessSupervisor) StartProcess(ctx context.Context, request StartProcessRequest) (ProcessHandle, error) {
	request, err := validateStartProcessRequest(request)
	if err != nil {
		return ProcessHandle{}, err
	}
	if supervisor.Runner == nil {
		return ProcessHandle{}, fmt.Errorf("process command runner is required")
	}
	pid, err := supervisor.Runner.Start(ctx, request)
	if err != nil {
		return ProcessHandle{}, err
	}
	if pid <= 0 {
		return ProcessHandle{}, fmt.Errorf("process runner returned invalid pid")
	}
	return ProcessHandle{
		PID:         pid,
		Role:        request.Role,
		CommandPath: request.CommandPath,
		StartedAt:   supervisor.now(),
		Status:      ProcessStatusStarted,
	}, nil
}

func (supervisor BoundedProcessSupervisor) WaitProcess(ctx context.Context, handle ProcessHandle) (ProcessResult, error) {
	if err := validateProcessHandle(handle); err != nil {
		return ProcessResult{}, err
	}
	if supervisor.Runner == nil {
		return ProcessResult{}, fmt.Errorf("process command runner is required")
	}
	result, err := supervisor.Runner.Wait(ctx, handle)
	if err != nil {
		return ProcessResult{}, err
	}
	return supervisor.normalizeProcessResult(handle, result), nil
}

func (supervisor BoundedProcessSupervisor) CancelProcess(ctx context.Context, handle ProcessHandle, reason string) (ProcessResult, error) {
	if err := validateProcessHandle(handle); err != nil {
		return ProcessResult{}, err
	}
	if supervisor.Runner == nil {
		return ProcessResult{}, fmt.Errorf("process command runner is required")
	}
	if err := supervisor.Runner.Cancel(ctx, handle); err != nil {
		return ProcessResult{}, err
	}
	now := supervisor.now()
	return ProcessResult{
		PID:          handle.PID,
		Role:         handle.Role,
		CommandPath:  handle.CommandPath,
		StartedAt:    handle.StartedAt,
		ExitedAt:     &now,
		Status:       ProcessStatusCancelled,
		ErrorMessage: strings.TrimSpace(reason),
	}, nil
}

func (supervisor BoundedProcessSupervisor) normalizeProcessResult(handle ProcessHandle, result ProcessResult) ProcessResult {
	if result.PID <= 0 {
		result.PID = handle.PID
	}
	if strings.TrimSpace(result.Role) == "" {
		result.Role = handle.Role
	}
	if strings.TrimSpace(result.CommandPath) == "" {
		result.CommandPath = handle.CommandPath
	}
	if result.StartedAt.IsZero() {
		result.StartedAt = handle.StartedAt
	}
	if result.Status == "" {
		result.Status = ProcessStatusExited
	}
	if result.Status != ProcessStatusStarted && result.ExitedAt == nil {
		now := supervisor.now()
		result.ExitedAt = &now
	}
	return result
}

func (supervisor BoundedProcessSupervisor) now() time.Time {
	if supervisor.Now != nil {
		return supervisor.Now()
	}
	return time.Now().UTC()
}

func validateStartProcessRequest(request StartProcessRequest) (StartProcessRequest, error) {
	role := strings.TrimSpace(request.Role)
	if role == "" {
		return StartProcessRequest{}, fmt.Errorf("process role is required")
	}
	command, err := validateProcessCommandPath(request.CommandPath, request.AllowedCommands)
	if err != nil {
		return StartProcessRequest{}, err
	}
	if request.TimeoutSeconds <= 0 {
		return StartProcessRequest{}, fmt.Errorf("timeout_seconds must be positive")
	}
	if strings.TrimSpace(request.WorkingDirectory) != "" && !filepath.IsAbs(request.WorkingDirectory) {
		return StartProcessRequest{}, fmt.Errorf("working directory must be absolute")
	}
	request.Role = role
	request.CommandPath = command
	return request, nil
}

func validateProcessCommandPath(command string, allowedCommands []string) (string, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return "", fmt.Errorf("process command is required")
	}
	if !filepath.IsAbs(command) {
		return "", fmt.Errorf("process command must be an absolute path")
	}
	cleanCommand := filepath.Clean(command)
	for _, allowed := range allowedCommands {
		allowed = strings.TrimSpace(allowed)
		if allowed == "" {
			continue
		}
		if filepath.Clean(allowed) == cleanCommand {
			return cleanCommand, nil
		}
	}
	return "", fmt.Errorf("process command %q is not in allowlist", cleanCommand)
}

func validateProcessHandle(handle ProcessHandle) error {
	if handle.PID <= 0 {
		return fmt.Errorf("process pid must be positive")
	}
	if strings.TrimSpace(handle.Role) == "" {
		return fmt.Errorf("process role is required")
	}
	if strings.TrimSpace(handle.CommandPath) == "" {
		return fmt.Errorf("process command is required")
	}
	if handle.StartedAt.IsZero() {
		return fmt.Errorf("process started_at is required")
	}
	return nil
}
