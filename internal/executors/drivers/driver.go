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

var inheritedWorkerEnvAllowlist = map[string]struct{}{
	"LANG":                              {},
	"LC_ALL":                            {},
	"LC_CTYPE":                          {},
	"PATH":                              {},
	"TEMP":                              {},
	"TMP":                               {},
	"TMPDIR":                            {},
	"TZ":                                {},
	"ODIN_ROOT":                         {},
	"ODIN_DIR":                          {},
	"ODIN_PROJECT":                      {},
	"ODIN_PROJECT_ID":                   {},
	"ODIN_AGENT_NAME":                   {},
	"ODIN_CODEX_BIN":                    {},
	"ODIN_CODEX_SANDBOX_MODE":           {},
	"ODIN_CODEX_DRIVER_TRACE":           {},
	"ODIN_CODEX_DRIVER_HEALTH_RESPONSE": {},
	"ODIN_CODEX_DRIVER_RUN_RESPONSE":    {},
	"ODIN_DRIVER_REQUEST_PATH":          {},
}

var explicitWorkerEnvAllowlist = map[string]struct{}{
	"ODIN_CODEX_DRIVER_ACTION": {},
}

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
	cmd.Env = AllowlistedEnvironment()
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

func AllowlistedEnvironment(extra ...string) []string {
	env := make([]string, 0, len(inheritedWorkerEnvAllowlist)+len(extra))
	for _, entry := range os.Environ() {
		key, _, ok := strings.Cut(entry, "=")
		if !ok || !isAllowedInheritedWorkerEnvKey(key) {
			continue
		}
		env = append(env, entry)
	}
	for _, entry := range extra {
		key, _, ok := strings.Cut(entry, "=")
		if !ok || !isAllowedExplicitWorkerEnvKey(key) {
			continue
		}
		env = append(env, entry)
	}
	if !hasEnvKey(env, "PATH") {
		env = append(env, "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin")
	}
	return env
}

func isAllowedInheritedWorkerEnvKey(key string) bool {
	normalized := strings.ToUpper(strings.TrimSpace(key))
	if normalized == "" || looksSensitiveEnvKey(normalized) {
		return false
	}
	_, ok := inheritedWorkerEnvAllowlist[normalized]
	return ok
}

func isAllowedExplicitWorkerEnvKey(key string) bool {
	normalized := strings.ToUpper(strings.TrimSpace(key))
	if normalized == "" || looksSensitiveEnvKey(normalized) {
		return false
	}
	if _, ok := inheritedWorkerEnvAllowlist[normalized]; ok {
		return true
	}
	_, ok := explicitWorkerEnvAllowlist[normalized]
	return ok
}

func looksSensitiveEnvKey(key string) bool {
	for _, marker := range []string{"TOKEN", "SECRET", "PASSWORD", "PASSWD", "API_KEY", "ACCESS_KEY", "PRIVATE_KEY", "CREDENTIAL"} {
		if strings.Contains(key, marker) {
			return true
		}
	}
	return false
}

func hasEnvKey(env []string, key string) bool {
	for _, entry := range env {
		entryKey, _, ok := strings.Cut(entry, "=")
		if ok && entryKey == key {
			return true
		}
	}
	return false
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
