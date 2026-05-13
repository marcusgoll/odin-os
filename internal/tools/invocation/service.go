package invocation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"odin-os/internal/adapters/browserhuman"
	caldriver "odin-os/internal/adapters/calendar"
	webdriver "odin-os/internal/adapters/web"
)

type Request struct {
	Args map[string]string
}

type Result struct {
	Source          string            `json:"source"`
	Summary         string            `json:"summary"`
	KeyFacts        map[string]string `json:"key_facts"`
	FollowOnOptions []string          `json:"follow_on_options"`
	RawRef          string            `json:"raw_ref"`
	RawOutput       string            `json:"raw_output"`
}

type BrowserResult struct {
	ToolKey   string
	Summary   string
	Artifacts map[string]any
	RawOutput string
}

type Invoker interface {
	Invoke(context.Context, string, Request) (Result, error)
}

type Service struct {
	RuntimeRoot             string
	DriverPath              string
	Driver                  browserhuman.Driver
	RobinhoodTransferDriver webdriver.RobinhoodTransferDriver
	OpenDesignDriver        webdriver.OpenDesignDriver
}

func (service Service) Invoke(ctx context.Context, key string, request Request) (Result, error) {
	if key != "project_status" {
		return Result{}, fmt.Errorf("unsupported tool %q", key)
	}
	if strings.TrimSpace(service.RuntimeRoot) == "" {
		return Result{}, fmt.Errorf("runtime root is required")
	}

	payload, err := json.Marshal(driverRequest{
		Tool:        key,
		RuntimeRoot: service.RuntimeRoot,
		Args:        cloneStringMap(request.Args),
	})
	if err != nil {
		return Result{}, err
	}

	driverPath := service.driverPath()
	if err := validateDriverPath(driverPath); err != nil {
		return Result{}, fmt.Errorf("project status driver unavailable: %w", err)
	}

	cmd := exec.CommandContext(ctx, driverPath)
	cmd.Env = append(os.Environ(), "ODIN_PROJECT_STATUS_DRIVER_ACTION=invoke")
	cmd.Stdin = bytes.NewReader(payload)

	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			stderr := strings.TrimSpace(string(exitErr.Stderr))
			if stderr == "" {
				stderr = strings.TrimSpace(string(output))
			}
			return Result{}, fmt.Errorf("project status driver failed: %s", stderr)
		}
		return Result{}, err
	}

	var result Result
	if err := json.Unmarshal(output, &result); err != nil {
		return Result{}, fmt.Errorf("decode project status driver output: %w", err)
	}
	if result.Source == "" {
		result.Source = "driver"
	}
	if result.KeyFacts == nil {
		result.KeyFacts = map[string]string{}
	}
	if result.RawOutput == "" {
		result.RawOutput = strings.TrimSpace(string(output))
	}
	return result, nil
}

func (service Service) GoogleCalendarOffDates(ctx context.Context, request caldriver.Request) (BrowserResult, error) {
	response, err := caldriver.NewDriver().Invoke(ctx, request)
	if err != nil {
		return BrowserResult{}, err
	}
	return browserResultFromResponse(response.ToolKey, response.Summary, response.Artifacts, response)
}

func (service Service) HuginnPBSSession(ctx context.Context, request webdriver.Request) (BrowserResult, error) {
	response, err := webdriver.NewDriver().Invoke(ctx, request)
	if err != nil {
		return BrowserResult{}, err
	}
	return browserResultFromResponse(response.ToolKey, response.Summary, response.Artifacts, response)
}

func (service Service) HuginnVisualAudit(ctx context.Context, request webdriver.VisualRequest) (BrowserResult, error) {
	response, err := webdriver.NewVisualDriver().Invoke(ctx, request)
	if err != nil {
		return BrowserResult{}, err
	}
	return browserResultFromResponse(response.ToolKey, response.Summary, response.Artifacts, response)
}

func (service Service) HuginnXPostVisibleEvidence(ctx context.Context, request webdriver.XPostRequest) (BrowserResult, error) {
	response, err := webdriver.NewXPostDriver().Invoke(ctx, request)
	if err != nil {
		return BrowserResult{}, err
	}
	return browserResultFromResponse(response.ToolKey, response.Summary, response.Artifacts, response)
}

func (service Service) HuginnXPostPublish(ctx context.Context, request webdriver.XPublishRequest) (BrowserResult, error) {
	response, err := webdriver.NewXPublishDriver().Invoke(ctx, request)
	if err != nil {
		return BrowserResult{}, err
	}
	return browserResultFromResponse(response.ToolKey, response.Summary, response.Artifacts, response)
}

func (service Service) BrowserHuman(ctx context.Context, request browserhuman.Request) (BrowserResult, error) {
	driver := service.Driver.WithDefaults()

	response, err := driver.Invoke(ctx, request)
	if err != nil {
		return BrowserResult{}, err
	}
	return toBrowserResult(response.ToolKey, response.Summary, response.Artifacts, response.RawOutput), nil
}

func (service Service) RobinhoodTransfer(ctx context.Context, request webdriver.RobinhoodTransferRequest) (BrowserResult, error) {
	driver := service.RobinhoodTransferDriver
	if driver.InvokeFunc == nil && strings.TrimSpace(driver.Driver.EnvVar) == "" && strings.TrimSpace(driver.Driver.DefaultToolKey) == "" {
		driver = webdriver.NewRobinhoodTransferDriver()
	}

	response, err := driver.Invoke(ctx, request)
	if err != nil {
		return BrowserResult{}, err
	}
	return toBrowserResult(response.ToolKey, response.Summary, response.Artifacts, response.RawOutput), nil
}

func (service Service) OpenDesign(ctx context.Context, request webdriver.OpenDesignRequest) (BrowserResult, error) {
	driver := service.OpenDesignDriver
	if driver.InvokeFunc == nil && strings.TrimSpace(driver.Driver.EnvVar) == "" && strings.TrimSpace(driver.Driver.DefaultToolKey) == "" {
		driver = webdriver.NewOpenDesignDriver()
	}

	response, err := driver.Invoke(ctx, request)
	if err != nil {
		return BrowserResult{}, err
	}
	return toBrowserResult(response.ToolKey, response.Summary, response.Artifacts, response.RawOutput), nil
}

func browserResultFromResponse(toolKey string, summary string, artifacts map[string]any, response any) (BrowserResult, error) {
	rawOutput, err := json.Marshal(response)
	if err != nil {
		return BrowserResult{}, err
	}
	return toBrowserResult(toolKey, summary, artifacts, string(rawOutput)), nil
}

func toBrowserResult(toolKey string, summary string, artifacts map[string]any, rawOutput string) BrowserResult {
	return BrowserResult{
		ToolKey:   toolKey,
		Summary:   summary,
		Artifacts: cloneArtifacts(artifacts),
		RawOutput: rawOutput,
	}
}

type driverRequest struct {
	Tool        string            `json:"tool"`
	RuntimeRoot string            `json:"runtime_root"`
	Args        map[string]string `json:"args"`
}

func (service Service) driverPath() string {
	if driverPath := strings.TrimSpace(service.DriverPath); driverPath != "" {
		return filepath.Clean(driverPath)
	}
	if driverPath := strings.TrimSpace(os.Getenv("ODIN_PROJECT_STATUS_DRIVER")); driverPath != "" {
		return filepath.Clean(driverPath)
	}
	if cwd, err := os.Getwd(); err == nil {
		if driverPath, ok := findDriverUpward(cwd); ok {
			return driverPath
		}
	}
	if executable, err := os.Executable(); err == nil {
		if resolved, err := filepath.EvalSymlinks(executable); err == nil {
			executable = resolved
		}
		if driverPath, ok := findDriverUpward(filepath.Dir(executable)); ok {
			return driverPath
		}
	}
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return filepath.Join("scripts", "drivers", "project-status.sh")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", "scripts", "drivers", "project-status.sh"))
}

func findDriverUpward(start string) (string, bool) {
	if start == "" {
		return "", false
	}

	dir := filepath.Clean(start)
	for {
		driverPath := filepath.Join(dir, "scripts", "drivers", "project-status.sh")
		if info, err := os.Stat(driverPath); err == nil && !info.IsDir() {
			return driverPath, true
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
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

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}

	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func cloneArtifacts(values map[string]any) map[string]any {
	if values == nil {
		return nil
	}
	cloned := make(map[string]any, len(values))
	for key, value := range values {
		cloned[key] = cloneArtifactValue(value)
	}
	return cloned
}

func cloneArtifactValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneArtifacts(typed)
	case []any:
		cloned := make([]any, len(typed))
		for i, item := range typed {
			cloned[i] = cloneArtifactValue(item)
		}
		return cloned
	default:
		return typed
	}
}
