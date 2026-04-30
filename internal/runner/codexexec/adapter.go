package codexexec

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"odin-os/internal/executors/contract"
	"odin-os/internal/runner"
	"odin-os/internal/security"
)

const (
	defaultCommand     = "codex"
	defaultSandboxMode = "workspace-write"
	defaultTimeout     = 30 * time.Minute
	redactedValue      = "[REDACTED]"
)

type Config struct {
	Command      string
	SandboxMode  string
	DryRun       bool
	Timeout      time.Duration
	SecretValues []string
}

type Command struct {
	Path    string
	Args    []string
	Dir     string
	Timeout time.Duration
}

type CommandExecutor interface {
	Run(context.Context, Command) (string, error)
}

// Adapter keeps the old codexexec shim surface while routing normalized calls
// through the canonical executor contract when an executor is supplied.
type Adapter struct {
	executor        contract.Executor
	config          Config
	commandExecutor CommandExecutor
}

func NewAdapter() *Adapter {
	return &Adapter{}
}

func NewAdapterWithExecutor(executor contract.Executor) *Adapter {
	return &Adapter{executor: executor}
}

func NewAgentRunner(config Config) *Adapter {
	return NewAgentRunnerWithExecutor(config, osCommandExecutor{})
}

func NewAgentRunnerWithExecutor(config Config, executor CommandExecutor) *Adapter {
	return &Adapter{
		config:          config,
		commandExecutor: executor,
	}
}

func (adapter Adapter) Run(ctx context.Context, request runner.Request) (runner.Result, error) {
	if adapter.executor == nil && adapter.commandExecutor == nil {
		return runner.Result{}, runner.ErrNotImplemented
	}
	if adapter.commandExecutor != nil {
		return adapter.runCommand(ctx, request)
	}

	result, err := adapter.executor.RunTask(ctx, contract.TaskSpec{
		ID:     request.WorkItemID,
		Kind:   taskKindForRole(request.Role),
		Scope:  "project",
		Prompt: request.Prompt,
		Metadata: map[string]string{
			"role":     request.Role,
			"worktree": request.Worktree,
		},
	})
	if err != nil {
		return runner.Result{}, err
	}

	return runner.Result{Summary: result.Output}, nil
}

func (adapter Adapter) runCommand(ctx context.Context, request runner.Request) (runner.Result, error) {
	command, err := BuildCommand(adapter.config, request)
	if err != nil {
		return runner.Result{}, err
	}

	if adapter.config.DryRun || request.DryRun {
		return runner.Result{
			Summary: "dry-run: " + RedactCommand(command, adapter.config.SecretValues),
		}, nil
	}

	output, err := adapter.commandExecutor.Run(ctx, command)
	if err != nil {
		return runner.Result{}, errors.New(Redact(err.Error(), adapter.config.SecretValues))
	}
	return runner.Result{Summary: Redact(output, adapter.config.SecretValues)}, nil
}

func BuildCommand(config Config, request runner.Request) (Command, error) {
	command := config.Command
	if command == "" {
		command = defaultCommand
	}

	sandboxMode := request.SandboxMode
	if sandboxMode == "" {
		sandboxMode = config.SandboxMode
	}
	if sandboxMode == "" {
		sandboxMode = defaultSandboxMode
	}
	if sandboxMode == "danger-full-access" {
		return Command{}, security.ErrDangerFullAccess
	}

	timeout := request.Timeout
	if timeout == 0 {
		timeout = config.Timeout
	}
	if timeout == 0 {
		timeout = defaultTimeout
	}

	return Command{
		Path: command,
		Args: []string{
			"exec",
			"-C", request.Worktree,
			"--sandbox", sandboxMode,
			request.Prompt,
		},
		Dir:     request.Worktree,
		Timeout: timeout,
	}, nil
}

func RedactCommand(command Command, secrets []string) string {
	parts := append([]string{command.Path}, command.Args...)
	return Redact(strings.Join(parts, " "), secrets)
}

func Redact(value string, secrets []string) string {
	redacted := value
	for _, secret := range secrets {
		if secret == "" {
			continue
		}
		redacted = strings.ReplaceAll(redacted, secret, redactedValue)
	}
	for _, prefix := range []string{"GITHUB_TOKEN=", "API_TOKEN=", "ODIN_TRADEBOARD_API_TOKEN="} {
		redacted = redactAssignment(redacted, prefix)
	}
	return redacted
}

func redactAssignment(value string, prefix string) string {
	var builder strings.Builder
	cursor := 0
	for {
		index := strings.Index(value[cursor:], prefix)
		if index < 0 {
			builder.WriteString(value[cursor:])
			return builder.String()
		}

		index += cursor
		start := index + len(prefix)
		end := start
		for end < len(value) {
			switch value[end] {
			case ' ', '\t', '\n', '\r', '"', '\'':
				builder.WriteString(value[cursor:start])
				builder.WriteString(redactedValue)
				cursor = end
				goto continueScan
			default:
				end++
			}
		}

		builder.WriteString(value[cursor:start])
		builder.WriteString(redactedValue)
		cursor = end
	continueScan:
	}
}

type osCommandExecutor struct{}

func (osCommandExecutor) Run(ctx context.Context, command Command) (string, error) {
	runCtx := ctx
	cancel := func() {}
	if command.Timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, command.Timeout)
	}
	defer cancel()

	cmd := exec.CommandContext(runCtx, command.Path, command.Args...)
	cmd.Dir = command.Dir
	output, err := cmd.CombinedOutput()
	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		return string(output), fmt.Errorf("codex exec timed out after %s: %w", command.Timeout, runCtx.Err())
	}
	if err != nil {
		return string(output), err
	}
	return string(output), nil
}

func taskKindForRole(role string) contract.TaskKind {
	switch role {
	case "builder":
		return contract.TaskKindBuild
	case "reviewer":
		return contract.TaskKindReview
	case "qa":
		return contract.TaskKindQA
	default:
		return contract.TaskKindGeneral
	}
}
