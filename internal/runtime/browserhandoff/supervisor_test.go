package browserhandoff

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestBoundedProcessSupervisorFakeStartSucceeds(t *testing.T) {
	now := time.Date(2026, 5, 7, 7, 0, 0, 0, time.UTC)
	fake := &fakeProcessCommandRunner{pid: 4242}
	supervisor := BoundedProcessSupervisor{
		Runner: fake,
		Now:    func() time.Time { return now },
	}
	request := validStartProcessRequest(t, "display")

	handle, err := supervisor.StartProcess(context.Background(), request)
	if err != nil {
		t.Fatalf("StartProcess() error = %v", err)
	}
	if handle.PID != 4242 || handle.Role != "display" || handle.Status != ProcessStatusStarted || !handle.StartedAt.Equal(now) {
		t.Fatalf("handle = %+v, want started display handle with fake pid and timestamp", handle)
	}
	if len(fake.started) != 1 || fake.started[0].CommandPath != request.CommandPath || fake.started[0].Args[0] != "--foreground" || fake.started[0].Env[0] != "ODIN_TEST=1" || fake.started[0].WorkingDirectory != request.WorkingDirectory {
		t.Fatalf("fake started requests = %+v, want exact validated request forwarded", fake.started)
	}
}

func TestBoundedProcessSupervisorRejectsUnsafeCommand(t *testing.T) {
	supervisor := BoundedProcessSupervisor{Runner: &fakeProcessCommandRunner{}}
	tests := []struct {
		name    string
		mutate  func(*StartProcessRequest)
		wantErr string
	}{
		{name: "missing command", mutate: func(request *StartProcessRequest) { request.CommandPath = "" }, wantErr: "command"},
		{name: "relative command", mutate: func(request *StartProcessRequest) { request.CommandPath = "true" }, wantErr: "absolute"},
		{name: "not allowlisted", mutate: func(request *StartProcessRequest) { request.AllowedCommands = []string{"/usr/bin/false"} }, wantErr: "allowlist"},
		{name: "missing role", mutate: func(request *StartProcessRequest) { request.Role = "" }, wantErr: "role"},
		{name: "missing timeout", mutate: func(request *StartProcessRequest) { request.TimeoutSeconds = 0 }, wantErr: "timeout"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := validStartProcessRequest(t, "display")
			test.mutate(&request)
			if _, err := supervisor.StartProcess(context.Background(), request); err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("StartProcess() error = %v, want substring %q", err, test.wantErr)
			}
		})
	}
}

func TestBoundedProcessSupervisorWaitHandlesTimeout(t *testing.T) {
	now := time.Date(2026, 5, 7, 7, 10, 0, 0, time.UTC)
	fake := &fakeProcessCommandRunner{
		pid: 4343,
		waitResult: ProcessResult{
			Status:       ProcessStatusTimeout,
			ErrorMessage: "fake timed out",
		},
	}
	supervisor := BoundedProcessSupervisor{
		Runner: fake,
		Now:    func() time.Time { return now },
	}
	handle, err := supervisor.StartProcess(context.Background(), validStartProcessRequest(t, "display"))
	if err != nil {
		t.Fatalf("StartProcess() error = %v", err)
	}
	result, err := supervisor.WaitProcess(context.Background(), handle)
	if err != nil {
		t.Fatalf("WaitProcess() error = %v", err)
	}
	if result.Status != ProcessStatusTimeout || result.ExitedAt == nil || !result.ExitedAt.Equal(now) || result.ErrorMessage != "fake timed out" {
		t.Fatalf("result = %+v, want timeout with supervisor exit timestamp", result)
	}
}

func TestBoundedProcessSupervisorCancelHandlesRunningProcess(t *testing.T) {
	now := time.Date(2026, 5, 7, 7, 20, 0, 0, time.UTC)
	fake := &fakeProcessCommandRunner{pid: 4444}
	supervisor := BoundedProcessSupervisor{
		Runner: fake,
		Now:    func() time.Time { return now },
	}
	handle, err := supervisor.StartProcess(context.Background(), validStartProcessRequest(t, "display"))
	if err != nil {
		t.Fatalf("StartProcess() error = %v", err)
	}
	result, err := supervisor.CancelProcess(context.Background(), handle, "operator cancelled")
	if err != nil {
		t.Fatalf("CancelProcess() error = %v", err)
	}
	if result.Status != ProcessStatusCancelled || result.ExitedAt == nil || !result.ExitedAt.Equal(now) || result.ErrorMessage != "operator cancelled" {
		t.Fatalf("result = %+v, want cancelled result with reason and timestamp", result)
	}
	if len(fake.cancelled) != 1 || fake.cancelled[0].PID != handle.PID {
		t.Fatalf("fake cancelled = %+v, want cancelled handle", fake.cancelled)
	}
}

func TestBoundedProcessSupervisorDoesNotInvokeBrowserOrNoVNCCommands(t *testing.T) {
	fake := &fakeProcessCommandRunner{pid: 4545}
	supervisor := BoundedProcessSupervisor{Runner: fake}
	for _, role := range []string{"display", "browser", "novnc"} {
		if _, err := supervisor.StartProcess(context.Background(), validStartProcessRequest(t, role)); err != nil {
			t.Fatalf("StartProcess(%s) error = %v", role, err)
		}
	}
	for _, request := range fake.started {
		lower := strings.ToLower(request.CommandPath + " " + strings.Join(request.Args, " "))
		for _, forbidden := range []string{"browser", "chromium", "chrome", "novnc", "websockify", "tailscale"} {
			if strings.Contains(lower, forbidden) {
				t.Fatalf("fake request invoked forbidden command token %q: %+v", forbidden, request)
			}
		}
	}
}

func validStartProcessRequest(t *testing.T, role string) StartProcessRequest {
	t.Helper()
	commandPath := testExecutablePath(t, "true")
	return StartProcessRequest{
		Role:             role,
		CommandPath:      commandPath,
		Args:             []string{"--foreground"},
		Env:              []string{"ODIN_TEST=1"},
		WorkingDirectory: t.TempDir(),
		TimeoutSeconds:   5,
		AllowedCommands:  []string{commandPath},
	}
}

type fakeProcessCommandRunner struct {
	pid        int64
	started    []StartProcessRequest
	cancelled  []ProcessHandle
	waitResult ProcessResult
}

func (runner *fakeProcessCommandRunner) Start(_ context.Context, request StartProcessRequest) (int64, error) {
	runner.started = append(runner.started, request)
	if runner.pid > 0 {
		return runner.pid, nil
	}
	return 1, nil
}

func (runner *fakeProcessCommandRunner) Wait(_ context.Context, handle ProcessHandle) (ProcessResult, error) {
	if runner.waitResult.Status != "" {
		result := runner.waitResult
		result.PID = handle.PID
		result.Role = handle.Role
		result.CommandPath = handle.CommandPath
		result.StartedAt = handle.StartedAt
		return result, nil
	}
	return ProcessResult{
		PID:         handle.PID,
		Role:        handle.Role,
		CommandPath: handle.CommandPath,
		StartedAt:   handle.StartedAt,
		Status:      ProcessStatusExited,
	}, nil
}

func (runner *fakeProcessCommandRunner) Cancel(_ context.Context, handle ProcessHandle) error {
	runner.cancelled = append(runner.cancelled, handle)
	return nil
}
