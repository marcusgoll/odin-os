package codexexec

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"odin-os/internal/executors/contract"
	"odin-os/internal/runner"
	"odin-os/internal/security"
)

func TestRunCurrentlyDoesNotConstructCodexExecCommand(t *testing.T) {
	t.Parallel()

	result, err := NewAdapter().Run(context.Background(), runner.Request{
		WorkItemID: "work-123",
		Role:       "builder",
		Worktree:   "/tmp/odin/worktrees/work-123",
		Prompt:     "implement the issue",
	})
	if !errors.Is(err, runner.ErrNotImplemented) {
		t.Fatalf("Run() error = %v, want %v", err, runner.ErrNotImplemented)
	}
	if result != (runner.Result{}) {
		t.Fatalf("Run() result = %#v, want zero value", result)
	}
}

func TestRunWithExecutorRoutesThroughTypedExecutorContract(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{
		result: contract.ExecutionResult{
			Status: "completed",
			Output: "codex_headless completed build task work-123 in project scope: implement the issue",
		},
	}
	result, err := NewAdapterWithExecutor(executor).Run(context.Background(), runner.Request{
		WorkItemID: "work-123",
		Role:       "builder",
		Worktree:   "/tmp/odin/worktrees/work-123",
		Prompt:     "implement the issue",
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Summary == "" {
		t.Fatal("Run().Summary is empty, want executor output")
	}
	if want := "codex_headless completed build task work-123 in project scope: implement the issue"; result.Summary != want {
		t.Fatalf("Run().Summary = %q, want %q", result.Summary, want)
	}
}

func TestRunMapsRequestToExecutorTaskWithoutExecutingPromptText(t *testing.T) {
	t.Parallel()

	dangerPath := filepath.Join(t.TempDir(), "should-not-exist")
	prompt := "implement issue; touch " + dangerPath
	executor := &recordingExecutor{
		result: contract.ExecutionResult{
			Status: "completed",
			Output: "completed through typed executor",
		},
	}

	result, err := NewAdapterWithExecutor(executor).Run(context.Background(), runner.Request{
		WorkItemID: "work-456",
		Role:       "reviewer",
		Worktree:   "/tmp/odin/worktrees/work-456",
		Prompt:     prompt,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if result.Summary != "completed through typed executor" {
		t.Fatalf("Run().Summary = %q", result.Summary)
	}
	if executor.spec.ID != "work-456" {
		t.Fatalf("TaskSpec.ID = %q, want work-456", executor.spec.ID)
	}
	if executor.spec.Kind != contract.TaskKindReview {
		t.Fatalf("TaskSpec.Kind = %q, want %q", executor.spec.Kind, contract.TaskKindReview)
	}
	if executor.spec.Scope != "project" {
		t.Fatalf("TaskSpec.Scope = %q, want project", executor.spec.Scope)
	}
	if executor.spec.Prompt != prompt {
		t.Fatalf("TaskSpec.Prompt = %q, want prompt text preserved", executor.spec.Prompt)
	}
	if executor.spec.Metadata["worktree"] != "/tmp/odin/worktrees/work-456" {
		t.Fatalf("TaskSpec.Metadata[worktree] = %q", executor.spec.Metadata["worktree"])
	}
}

func TestBuildCommandUsesExplicitCodexExecArgs(t *testing.T) {
	t.Parallel()

	command, err := BuildCommand(Config{
		Command:     "codex",
		SandboxMode: "workspace-write",
		Timeout:     2 * time.Minute,
	}, runner.Request{
		WorkItemID: "work-789",
		Role:       "builder",
		Worktree:   "/tmp/odin/worktrees/work-789",
		Prompt:     "implement issue; touch /tmp/should-not-run",
	})
	if err != nil {
		t.Fatalf("BuildCommand() error = %v", err)
	}

	wantArgs := []string{
		"exec",
		"-C", "/tmp/odin/worktrees/work-789",
		"--sandbox", "workspace-write",
		"implement issue; touch /tmp/should-not-run",
	}
	if command.Path != "codex" {
		t.Fatalf("Command.Path = %q, want codex", command.Path)
	}
	if !reflect.DeepEqual(command.Args, wantArgs) {
		t.Fatalf("Command.Args = %#v, want %#v", command.Args, wantArgs)
	}
	if command.Dir != "/tmp/odin/worktrees/work-789" {
		t.Fatalf("Command.Dir = %q, want worktree", command.Dir)
	}
	if command.Timeout != 2*time.Minute {
		t.Fatalf("Command.Timeout = %v, want 2m", command.Timeout)
	}
}

func TestRunDryRunReturnsRedactedCommandWithoutExecuting(t *testing.T) {
	t.Parallel()

	executor := &recordingCommandExecutor{}
	result, err := NewAgentRunnerWithExecutor(Config{
		Command:      "codex",
		SandboxMode:  "workspace-write",
		DryRun:       true,
		SecretValues: []string{"ghp_secret"},
	}, executor).Run(context.Background(), runner.Request{
		WorkItemID: "work-789",
		Role:       "builder",
		Worktree:   "/tmp/odin/worktrees/work-789",
		Prompt:     "use GITHUB_TOKEN=ghp_secret",
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if executor.calls != 0 {
		t.Fatalf("command executor calls = %d, want 0 in dry-run", executor.calls)
	}
	if result.Summary == "" {
		t.Fatal("Run().Summary is empty")
	}
	if contains(result.Summary, "ghp_secret") {
		t.Fatalf("Run().Summary leaked secret: %q", result.Summary)
	}
	if !contains(result.Summary, "codex exec -C /tmp/odin/worktrees/work-789 --sandbox workspace-write") {
		t.Fatalf("Run().Summary = %q, want redacted command", result.Summary)
	}
}

func TestRunRejectsDangerFullAccess(t *testing.T) {
	t.Parallel()

	_, err := NewAgentRunner(Config{
		Command:     "codex",
		SandboxMode: "danger-full-access",
	}).Run(context.Background(), runner.Request{
		WorkItemID: "work-789",
		Role:       "builder",
		Worktree:   "/tmp/odin/worktrees/work-789",
		Prompt:     "implement the issue",
	})
	if !errors.Is(err, security.ErrDangerFullAccess) {
		t.Fatalf("Run() error = %v, want %v", err, security.ErrDangerFullAccess)
	}
}

func TestRunPassesExplicitTimeoutToCommandExecutor(t *testing.T) {
	t.Parallel()

	executor := &recordingCommandExecutor{output: "codex completed"}
	_, err := NewAgentRunnerWithExecutor(Config{
		Command:     "codex",
		SandboxMode: "workspace-write",
		Timeout:     10 * time.Minute,
	}, executor).Run(context.Background(), runner.Request{
		WorkItemID: "work-789",
		Role:       "builder",
		Worktree:   "/tmp/odin/worktrees/work-789",
		Prompt:     "implement the issue",
		Timeout:    45 * time.Second,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if executor.command.Timeout != 45*time.Second {
		t.Fatalf("command timeout = %v, want request timeout", executor.command.Timeout)
	}
}

func TestRunRedactsSecretsFromCommandOutput(t *testing.T) {
	t.Parallel()

	result, err := NewAgentRunnerWithExecutor(Config{
		Command:      "codex",
		SandboxMode:  "workspace-write",
		SecretValues: []string{"ghp_secret", "super-secret-value"},
	}, &recordingCommandExecutor{
		output: "done with ghp_secret and API_TOKEN=super-secret-value",
	}).Run(context.Background(), runner.Request{
		WorkItemID: "work-789",
		Role:       "builder",
		Worktree:   "/tmp/odin/worktrees/work-789",
		Prompt:     "implement the issue",
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if contains(result.Summary, "ghp_secret") || contains(result.Summary, "super-secret-value") {
		t.Fatalf("Run().Summary leaked secret: %q", result.Summary)
	}
}

func TestRunRedactsSecretsFromCommandErrors(t *testing.T) {
	t.Parallel()

	_, err := NewAgentRunnerWithExecutor(Config{
		Command:      "codex",
		SandboxMode:  "workspace-write",
		SecretValues: []string{"ghp_secret"},
	}, &recordingCommandExecutor{
		err: errors.New("codex failed with GITHUB_TOKEN=ghp_secret"),
	}).Run(context.Background(), runner.Request{
		WorkItemID: "work-789",
		Role:       "builder",
		Worktree:   "/tmp/odin/worktrees/work-789",
		Prompt:     "implement the issue",
	})
	if err == nil {
		t.Fatal("Run() error = nil, want redacted error")
	}
	if contains(err.Error(), "ghp_secret") {
		t.Fatalf("Run() error leaked secret: %v", err)
	}
}

type recordingCommandExecutor struct {
	calls   int
	command Command
	output  string
	err     error
}

func (executor *recordingCommandExecutor) Run(_ context.Context, command Command) (string, error) {
	executor.calls++
	executor.command = command
	return executor.output, executor.err
}

type recordingExecutor struct {
	spec   contract.TaskSpec
	result contract.ExecutionResult
}

func (executor *recordingExecutor) Key() string {
	return "recording"
}

func (executor *recordingExecutor) Class() contract.ExecutorClass {
	return contract.ExecutorClassPlanBackedCLI
}

func (executor *recordingExecutor) Health(context.Context) (contract.HealthReport, error) {
	return contract.HealthReport{Status: contract.HealthStatusHealthy}, nil
}

func (executor *recordingExecutor) Capabilities(context.Context) (contract.Capabilities, error) {
	return contract.Capabilities{}, nil
}

func (executor *recordingExecutor) RunTask(_ context.Context, spec contract.TaskSpec) (contract.ExecutionResult, error) {
	executor.spec = spec
	return executor.result, nil
}

func (executor *recordingExecutor) ResumeTask(context.Context, contract.TaskHandle, contract.ResumePacket) (contract.ExecutionResult, error) {
	return contract.ExecutionResult{}, contract.ErrNotImplemented
}

func (executor *recordingExecutor) CancelTask(context.Context, contract.TaskHandle) error {
	return contract.ErrNotImplemented
}

func (executor *recordingExecutor) EstimateCost(context.Context, contract.TaskSpec) (contract.CostEstimate, error) {
	return contract.CostEstimate{}, contract.ErrNotImplemented
}

func contains(value string, substring string) bool {
	for idx := range value {
		if len(value[idx:]) < len(substring) {
			return false
		}
		if value[idx:idx+len(substring)] == substring {
			return true
		}
	}
	return substring == ""
}
