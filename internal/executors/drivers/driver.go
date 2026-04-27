package drivers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

type Options struct {
	DriverPath string
	Label      string
	Timeout    time.Duration
	WorkDir    string
}

func Invoke(ctx context.Context, options Options, request any, response any) ([]byte, error) {
	label := strings.TrimSpace(options.Label)
	if label == "" {
		label = "executor"
	}
	driverPath := strings.TrimSpace(options.DriverPath)
	if driverPath == "" {
		return nil, fmt.Errorf("%s driver unavailable", label)
	}
	if err := validateDriverPath(driverPath); err != nil {
		return nil, fmt.Errorf("%s driver unavailable: %w", label, err)
	}

	payload, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}

	driverCtx, cancel, timeoutLabel := boundedContext(ctx, options.Timeout)
	defer cancel()

	cmd := exec.CommandContext(driverCtx, driverPath)
	if workDir := strings.TrimSpace(options.WorkDir); workDir != "" {
		cmd.Dir = workDir
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
			return err
		}
		return nil
	}
	cmd.Stdin = strings.NewReader(string(payload))
	output, err := cmd.Output()
	if err != nil {
		if errors.Is(driverCtx.Err(), context.DeadlineExceeded) {
			if timeoutLabel != "" {
				return nil, fmt.Errorf("%s driver timed out after %s: %w", label, timeoutLabel, driverCtx.Err())
			}
			return nil, fmt.Errorf("%s driver timed out: %w", label, driverCtx.Err())
		}
		if exitErr, ok := err.(*exec.ExitError); ok && len(exitErr.Stderr) > 0 {
			return nil, fmt.Errorf("%s driver failed: %w: %s", label, err, strings.TrimSpace(string(exitErr.Stderr)))
		}
		return nil, fmt.Errorf("%s driver failed: %w", label, err)
	}

	if err := json.Unmarshal(output, response); err != nil {
		return nil, fmt.Errorf("%s driver returned invalid JSON: %w", label, err)
	}
	return payload, nil
}

func validateDriverPath(driverPath string) error {
	info, err := os.Stat(driverPath)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("%s is a directory", driverPath)
	}
	if info.Mode()&0o111 == 0 {
		return fmt.Errorf("%s is not executable", driverPath)
	}
	return nil
}

func boundedContext(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc, string) {
	if _, ok := ctx.Deadline(); ok || timeout <= 0 {
		return ctx, func() {}, ""
	}
	boundedCtx, cancel := context.WithTimeout(ctx, timeout)
	return boundedCtx, cancel, timeout.String()
}
