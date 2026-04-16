package browserhuman

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

	runtimeRoot, err := invocationRuntimeRoot(request.ToolKey)
	if err != nil {
		return Response{}, fmt.Errorf("prepare invocation runtime: %w", err)
	}
	request.Input = scopeRequestInput(request.Input, runtimeRoot)

	requestBytes, err := json.Marshal(request)
	if err != nil {
		return Response{}, err
	}

	// Unix-only: assume the configured value is a shell command string, and
	// isolate it in its own process group so cancellation can kill the whole tree.
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
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

	cmd.Stdin = bytes.NewReader(requestBytes)
	cmd.Env = envWithOverride(os.Environ(), "ODIN_DIR", runtimeRoot)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return Response{}, fmt.Errorf("driver command failed: %w; stdout=%q; stderr=%q", err, stdout.String(), strings.TrimSpace(stderr.String()))
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

func invocationRuntimeRoot(toolKey string) (string, error) {
	toolSegment := sanitizePathSegment(toolKey)
	if toolSegment == "" {
		toolSegment = "browserhuman"
	}
	baseRoot := strings.TrimSpace(os.Getenv("ODIN_DIR"))
	if baseRoot == "" {
		return os.MkdirTemp("", "odin-browserhuman-"+toolSegment+"-")
	}
	invocationBase := filepath.Join(baseRoot, "browserhuman", toolSegment)
	if err := os.MkdirAll(invocationBase, 0o755); err != nil {
		return "", err
	}
	return os.MkdirTemp(invocationBase, "run-")
}

func sanitizePathSegment(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	var builder strings.Builder
	prevDash := false
	for _, r := range value {
		isAlphaNum := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if isAlphaNum {
			builder.WriteRune(r)
			prevDash = false
			continue
		}
		if !prevDash {
			builder.WriteByte('-')
			prevDash = true
		}
	}
	return strings.Trim(builder.String(), "-")
}

func scopeRequestInput(input any, runtimeRoot string) any {
	switch typed := input.(type) {
	case map[string]any:
		cloned := make(map[string]any, len(typed))
		for key, value := range typed {
			cloned[key] = value
		}
		scopePathFieldAny(cloned, runtimeRoot)
		return cloned
	case map[string]string:
		cloned := make(map[string]string, len(typed))
		for key, value := range typed {
			cloned[key] = value
		}
		scopePathFieldString(cloned, runtimeRoot)
		return cloned
	default:
		return input
	}
}

func scopePathFieldAny(values map[string]any, runtimeRoot string) {
	path, ok := values["path"].(string)
	if !ok {
		return
	}
	values["path"] = scopedArtifactPath(runtimeRoot, path)
}

func scopePathFieldString(values map[string]string, runtimeRoot string) {
	path, ok := values["path"]
	if !ok {
		return
	}
	values["path"] = scopedArtifactPath(runtimeRoot, path)
}

func scopedArtifactPath(runtimeRoot string, requested string) string {
	name := strings.TrimSpace(filepath.Base(requested))
	if name == "" || name == "." || name == string(filepath.Separator) {
		name = "artifact"
	}
	return filepath.Join(runtimeRoot, "artifacts", name)
}

func envWithOverride(base []string, key string, value string) []string {
	prefix := key + "="
	result := make([]string, 0, len(base)+1)
	replaced := false
	for _, entry := range base {
		if strings.HasPrefix(entry, prefix) {
			if !replaced {
				result = append(result, prefix+value)
				replaced = true
			}
			continue
		}
		result = append(result, entry)
	}
	if !replaced {
		result = append(result, prefix+value)
	}
	return result
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
