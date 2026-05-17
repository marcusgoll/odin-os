package browserhandoff

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

const DefaultExecCaptureLimitBytes = 4096

type ExecCommandRunner struct {
	CaptureLimitBytes int

	mu        sync.Mutex
	processes map[int64]*execProcess
}

type execProcess struct {
	cmd     *exec.Cmd
	ctx     context.Context
	cancel  context.CancelFunc
	stdout  *boundedProcessBuffer
	stderr  *boundedProcessBuffer
	request StartProcessRequest
}

func NewExecCommandRunner() *ExecCommandRunner {
	return &ExecCommandRunner{}
}

func (runner *ExecCommandRunner) Start(ctx context.Context, request StartProcessRequest) (int64, error) {
	request, err := validateStartProcessRequest(request)
	if err != nil {
		return 0, err
	}
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(request.TimeoutSeconds)*time.Second)
	cmd := exec.CommandContext(runCtx, request.CommandPath, request.Args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return killProcessGroup(cmd)
	}
	if strings.TrimSpace(request.WorkingDirectory) != "" {
		cmd.Dir = request.WorkingDirectory
	}
	if len(request.Env) > 0 {
		cmd.Env = append(os.Environ(), request.Env...)
	}
	stdout := &boundedProcessBuffer{limit: runner.captureLimit()}
	stderr := &boundedProcessBuffer{limit: runner.captureLimit()}
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	if err := cmd.Start(); err != nil {
		cancel()
		return 0, err
	}
	pid := int64(cmd.Process.Pid)
	runner.mu.Lock()
	runner.ensureProcessesLocked()
	runner.processes[pid] = &execProcess{
		cmd:     cmd,
		ctx:     runCtx,
		cancel:  cancel,
		stdout:  stdout,
		stderr:  stderr,
		request: request,
	}
	runner.mu.Unlock()
	return pid, nil
}

func (runner *ExecCommandRunner) Wait(_ context.Context, handle ProcessHandle) (ProcessResult, error) {
	if err := validateProcessHandle(handle); err != nil {
		return ProcessResult{}, err
	}
	process, err := runner.processForHandle(handle)
	if err != nil {
		return ProcessResult{}, err
	}
	waitErr := process.cmd.Wait()
	process.cancel()
	runner.deleteProcess(handle.PID)

	status := ProcessStatusExited
	errorMessage := ""
	switch {
	case errors.Is(process.ctx.Err(), context.DeadlineExceeded):
		status = ProcessStatusTimeout
		errorMessage = fmt.Sprintf("process timed out after %d seconds", process.request.TimeoutSeconds)
	case waitErr != nil:
		status = ProcessStatusFailed
		errorMessage = strings.TrimSpace(waitErr.Error())
	}
	return ProcessResult{
		PID:          handle.PID,
		Role:         handle.Role,
		CommandPath:  handle.CommandPath,
		StartedAt:    handle.StartedAt,
		Status:       status,
		Stdout:       process.stdout.String(),
		Stderr:       process.stderr.String(),
		ErrorMessage: errorMessage,
	}, nil
}

func (runner *ExecCommandRunner) Probe(_ context.Context, handle ProcessHandle) (ProcessResult, bool, error) {
	if err := validateProcessHandle(handle); err != nil {
		return ProcessResult{}, false, err
	}
	process, err := runner.processForHandle(handle)
	if err != nil {
		return ProcessResult{}, false, err
	}
	var waitStatus syscall.WaitStatus
	waitedPID, err := syscall.Wait4(int(handle.PID), &waitStatus, syscall.WNOHANG, nil)
	if err != nil {
		return ProcessResult{}, false, err
	}
	if waitedPID == 0 {
		return ProcessResult{}, false, nil
	}
	process.cancel()
	runner.deleteProcess(handle.PID)
	status := ProcessStatusExited
	errorMessage := ""
	if waitStatus.Signaled() {
		status = ProcessStatusFailed
		errorMessage = fmt.Sprintf("process exited after signal %s", waitStatus.Signal())
	} else if waitStatus.Exited() && waitStatus.ExitStatus() != 0 {
		status = ProcessStatusFailed
		errorMessage = fmt.Sprintf("process exited with status %d", waitStatus.ExitStatus())
	}
	return ProcessResult{
		PID:          handle.PID,
		Role:         handle.Role,
		CommandPath:  handle.CommandPath,
		StartedAt:    handle.StartedAt,
		Status:       status,
		Stdout:       process.stdout.String(),
		Stderr:       process.stderr.String(),
		ErrorMessage: errorMessage,
	}, true, nil
}

func (runner *ExecCommandRunner) Cancel(_ context.Context, handle ProcessHandle) error {
	if err := validateProcessHandle(handle); err != nil {
		return err
	}
	process, err := runner.processForHandle(handle)
	if err != nil {
		return err
	}
	process.cancel()
	killErr := killProcessGroup(process.cmd)
	_ = process.cmd.Wait()
	runner.deleteProcess(handle.PID)
	if killErr != nil {
		return killErr
	}
	return nil
}

func (runner *ExecCommandRunner) captureLimit() int {
	if runner != nil && runner.CaptureLimitBytes > 0 {
		return runner.CaptureLimitBytes
	}
	return DefaultExecCaptureLimitBytes
}

func (runner *ExecCommandRunner) processForHandle(handle ProcessHandle) (*execProcess, error) {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	if runner.processes == nil {
		return nil, fmt.Errorf("process %d is not tracked", handle.PID)
	}
	process := runner.processes[handle.PID]
	if process == nil {
		return nil, fmt.Errorf("process %d is not tracked", handle.PID)
	}
	return process, nil
}

func (runner *ExecCommandRunner) deleteProcess(pid int64) {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	delete(runner.processes, pid)
}

func (runner *ExecCommandRunner) ensureProcessesLocked() {
	if runner.processes == nil {
		runner.processes = make(map[int64]*execProcess)
	}
}

func killProcessGroup(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		if killErr := cmd.Process.Kill(); killErr != nil && !errors.Is(killErr, os.ErrProcessDone) {
			return err
		}
	}
	return nil
}

type boundedProcessBuffer struct {
	mu    sync.Mutex
	limit int
	data  []byte
}

func (buffer *boundedProcessBuffer) Write(payload []byte) (int, error) {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	if buffer.limit <= 0 {
		return len(payload), nil
	}
	remaining := buffer.limit - len(buffer.data)
	if remaining > 0 {
		if remaining > len(payload) {
			remaining = len(payload)
		}
		buffer.data = append(buffer.data, payload[:remaining]...)
	}
	return len(payload), nil
}

func (buffer *boundedProcessBuffer) String() string {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return string(buffer.data)
}
