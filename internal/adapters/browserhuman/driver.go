package browserhuman

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

const defaultDriverEnvVar = "ODIN_BROWSER_HUMAN_DRIVER"
const defaultToolKey = "huginn_browser_session"

type Request struct {
	ToolKey             string `json:"tool_key,omitempty"`
	AllowDefaultToolKey bool   `json:"-"`
	Input               any    `json:"input,omitempty"`
}

type Response struct {
	Status    string         `json:"status"`
	ToolKey   string         `json:"tool_key"`
	Summary   string         `json:"summary"`
	Artifacts map[string]any `json:"artifacts"`
	RawOutput string         `json:"-"`
}

type Driver struct {
	EnvVar         string
	DefaultToolKey string
}

func NewDriver() Driver {
	return Driver{
		EnvVar:         defaultDriverEnvVar,
		DefaultToolKey: defaultToolKey,
	}
}

func (driver Driver) Invoke(ctx context.Context, request Request) (Response, error) {
	command := strings.TrimSpace(os.Getenv(driver.envVar()))
	if command == "" {
		return Response{}, fmt.Errorf("driver command not configured")
	}

	if strings.TrimSpace(request.ToolKey) == "" {
		if request.AllowDefaultToolKey {
			request.ToolKey = driver.defaultToolKey()
		}
	}
	if strings.TrimSpace(request.ToolKey) == "" {
		return Response{}, fmt.Errorf("tool key not configured")
	}

	requestBytes, err := json.Marshal(request)
	if err != nil {
		return Response{}, err
	}

	// Parse the configured shell command, then exec it so cancellation targets
	// the actual driver process instead of a wrapper shell.
	// Unix-only: put the driver in its own process group so context
	// cancellation can tear down the whole tree, not just the leader.
	cmd := exec.CommandContext(ctx, "sh", "-c", `eval "set -- $1"; exec "$@"`, "sh", command)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}

	cmd.Stdin = bytes.NewReader(requestBytes)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return Response{}, fmt.Errorf("driver command failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	var response Response
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		return Response{}, fmt.Errorf("decode driver response: %w; stdout=%q", err, stdout.String())
	}
	response.RawOutput = stdout.String()
	if response.ToolKey != request.ToolKey {
		return Response{}, fmt.Errorf("driver response tool_key %q does not match request %q; stdout=%q", response.ToolKey, request.ToolKey, response.RawOutput)
	}
	if response.Artifacts == nil {
		return Response{}, fmt.Errorf("driver response artifacts are missing; stdout=%q", response.RawOutput)
	}
	if strings.TrimSpace(response.Status) == "" {
		return Response{}, fmt.Errorf("driver response status is empty; stdout=%q", response.RawOutput)
	}
	if !strings.EqualFold(strings.TrimSpace(response.Status), "completed") {
		return Response{}, fmt.Errorf("driver response status %q is not completed; stdout=%q", response.Status, response.RawOutput)
	}

	return response, nil
}

func (driver Driver) envVar() string {
	if strings.TrimSpace(driver.EnvVar) == "" {
		return defaultDriverEnvVar
	}
	return driver.EnvVar
}

func (driver Driver) defaultToolKey() string {
	if strings.TrimSpace(driver.DefaultToolKey) == "" {
		return defaultToolKey
	}
	return driver.DefaultToolKey
}

func (driver Driver) WithDefaults() Driver {
	defaults := NewDriver()
	if strings.TrimSpace(driver.EnvVar) == "" {
		driver.EnvVar = defaults.EnvVar
	}
	if strings.TrimSpace(driver.DefaultToolKey) == "" {
		driver.DefaultToolKey = defaults.DefaultToolKey
	}
	return driver
}
