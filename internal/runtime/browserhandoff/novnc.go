package browserhandoff

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	NoVNCBrowserCommandEnvVar    = "ODIN_NOVNC_BROWSER_COMMAND"
	NoVNCDisplayCommandEnvVar    = "ODIN_NOVNC_DISPLAY_COMMAND"
	NoVNCWebsockifyCommandEnvVar = "ODIN_NOVNC_WEBSOCKIFY_COMMAND"
	NoVNCAllowedCommandsEnvVar   = "ODIN_NOVNC_ALLOWED_COMMANDS"
	NoVNCBindAddrEnvVar          = "ODIN_NOVNC_BIND_ADDR"
	NoVNCPrivateBaseURLEnvVar    = "ODIN_NOVNC_PRIVATE_BASE_URL"
	NoVNCTimeoutSecondsEnvVar    = "ODIN_NOVNC_TIMEOUT_SECONDS"
	NoVNCRealBrowserEnvVar       = "ODIN_NOVNC_REAL_BROWSER"
	NoVNCRealDisplayEnvVar       = "ODIN_NOVNC_REAL_DISPLAY"
	NoVNCRealWebsockifyEnvVar    = "ODIN_NOVNC_REAL_WEBSOCKIFY"
	BrowserProfileDirEnvVar      = "ODIN_BROWSER_PROFILE_DIR"
	BrowserStartURLEnvVar        = "ODIN_BROWSER_START_URL"

	defaultNoVNCBindAddr = "127.0.0.1:0"

	NoVNCDisplayCommandRole         = "display"
	NoVNCBrowserCommandRole         = "browser"
	NoVNCWebsockifyCommandRole      = "novnc/websockify"
	NoVNCCommandValidationValid     = "valid"
	NoVNCCommandValidationInvalid   = "invalid"
	NoVNCCommandErrorMissing        = "missing_command"
	NoVNCCommandErrorRelative       = "relative_command"
	NoVNCCommandErrorNotAllowlisted = "command_not_allowlisted"
	NoVNCCommandErrorNotFound       = "command_not_found"
	NoVNCCommandErrorNotExecutable  = "command_not_executable"
)

var noVNCFixtureSafeCommandNames = map[string]struct{}{
	"false": {},
	"sleep": {},
	"true":  {},
	"yes":   {},
}

type NoVNCRunnerConfig struct {
	BrowserCommand         string
	BrowserAllowedCommands []string
	DisplayCommand         string
	DisplayAllowedCommands []string
	NoVNCCommand           string
	NoVNCAllowedCommands   []string
	BindAddr               string
	PrivateBaseURL         string
	TimeoutSeconds         int
}

type NoVNCLaunchConfig struct {
	BrowserCommand        string
	DisplayCommand        string
	WebsockifyCommand     string
	AllowedCommandPaths   []string
	BindAddr              string
	PrivateBaseURL        string
	TimeoutSeconds        int
	RealBrowserEnabled    bool
	RealDisplayEnabled    bool
	RealWebsockifyEnabled bool
}

type NoVNCRunner struct {
	LoadConfig func() (NoVNCLaunchConfig, error)
	Supervisor ProcessSupervisor
}

type NoVNCPlan struct {
	Commands       []NoVNCPlannedCommand `json:"commands"`
	BindAddr       string                `json:"bind_addr"`
	PrivateBaseURL string                `json:"private_base_url"`
	ViewerURL      string                `json:"viewer_url"`
	TimeoutSeconds int                   `json:"timeout_seconds"`
}

type NoVNCPlannedCommand struct {
	Role             string   `json:"role"`
	Path             string   `json:"path"`
	Args             []string `json:"args,omitempty"`
	DetectedPath     string   `json:"detected_path,omitempty"`
	CommandRole      string   `json:"command_role,omitempty"`
	ValidationStatus string   `json:"validation_status,omitempty"`
	ErrorCode        string   `json:"error_code,omitempty"`
	ErrorMessage     string   `json:"error_message,omitempty"`
}

type NoVNCCommandDetection struct {
	DetectedPath     string
	CommandRole      string
	ValidationStatus string
	ErrorCode        string
	ErrorMessage     string
}

func (runner NoVNCRunner) Start(ctx context.Context, request StartRequest) (StartResponse, error) {
	if err := ValidateStartRequest(request); err != nil {
		return StartResponse{}, err
	}
	loadConfig := runner.LoadConfig
	if loadConfig == nil {
		loadConfig = LoadNoVNCLaunchConfigFromEnv
	}
	config, err := loadConfig()
	if err != nil {
		return StartResponse{}, err
	}
	config, err = ValidateNoVNCLaunchConfig(config, request.TimeoutSeconds)
	if err != nil {
		return StartResponse{}, err
	}
	if err := validateNoVNCFixtureSafeLaunchConfig(config); err != nil {
		return StartResponse{}, err
	}
	supervisor := runner.Supervisor
	if supervisor == nil {
		supervisor = BoundedProcessSupervisor{Runner: NewExecCommandRunner()}
	}

	commands := []NoVNCPlannedCommand{
		{Role: "display", Path: config.DisplayCommand},
		{Role: "browser", Path: config.BrowserCommand},
		{Role: NoVNCWebsockifyCommandRole, Path: config.WebsockifyCommand},
	}
	handles := make([]ProcessHandle, 0, len(commands))
	for _, command := range commands {
		handle, err := supervisor.StartProcess(ctx, StartProcessRequest{
			Role:            command.Role,
			CommandPath:     command.Path,
			Env:             noVNCProcessEnv(command.Role, request),
			TimeoutSeconds:  config.TimeoutSeconds,
			AllowedCommands: config.AllowedCommandPaths,
		})
		if err != nil {
			results := cancelNoVNCProcesses(ctx, supervisor, handles, "novnc start failed")
			response := newNoVNCStartResponse(request, config, handles, results)
			response.Status = StatusFailed
			response.ErrorCode = "novnc_start_failed"
			response.ErrorMessage = err.Error()
			return response, nil
		}
		handles = append(handles, handle)
	}

	response := newNoVNCStartResponse(request, config, handles, nil)
	if noVNCFullRealLaunchEnabled(config) {
		response.Status = StatusStarted
		response.ChildProcesses = noVNCStartedProcessResults(handles)
		return response, nil
	}
	results := make([]ProcessResult, 0, len(handles))
	for index, handle := range handles {
		result, err := supervisor.WaitProcess(ctx, handle)
		if err != nil {
			results = append(results, cancelNoVNCProcesses(ctx, supervisor, handles[index+1:], "novnc wait failed")...)
			response.ChildProcesses = results
			response.Status = StatusFailed
			response.ErrorCode = "novnc_wait_failed"
			response.ErrorMessage = err.Error()
			return response, nil
		}
		results = append(results, result)
		switch result.Status {
		case ProcessStatusExited:
			continue
		case ProcessStatusTimeout:
			results = append(results, cancelNoVNCProcesses(ctx, supervisor, handles[index+1:], "novnc timeout cleanup")...)
			response.ChildProcesses = results
			response.Status = StatusExpired
			response.ErrorCode = "novnc_timeout"
			response.ErrorMessage = noVNCProcessErrorMessage(result, "browser handoff NoVNC fixture process timed out")
			return response, nil
		case ProcessStatusFailed:
			results = append(results, cancelNoVNCProcesses(ctx, supervisor, handles[index+1:], "novnc failure cleanup")...)
			response.ChildProcesses = results
			response.Status = StatusFailed
			response.ErrorCode = "novnc_process_failed"
			response.ErrorMessage = noVNCProcessErrorMessage(result, "browser handoff NoVNC fixture process failed")
			return response, nil
		case ProcessStatusCancelled:
			results = append(results, cancelNoVNCProcesses(ctx, supervisor, handles[index+1:], "novnc cancellation cleanup")...)
			response.ChildProcesses = results
			response.Status = StatusFailed
			response.ErrorCode = "novnc_process_cancelled"
			response.ErrorMessage = noVNCProcessErrorMessage(result, "browser handoff NoVNC fixture process cancelled")
			return response, nil
		default:
			results = append(results, cancelNoVNCProcesses(ctx, supervisor, handles[index+1:], "novnc unsupported status cleanup")...)
			response.ChildProcesses = results
			response.Status = StatusFailed
			response.ErrorCode = "novnc_unsupported_process_status"
			response.ErrorMessage = fmt.Sprintf("unsupported NoVNC process status %q", result.Status)
			return response, nil
		}
	}
	response.ChildProcesses = results
	response.Status = StatusCompleted
	return response, nil
}

func noVNCProcessEnv(role string, request StartRequest) []string {
	if role != NoVNCBrowserCommandRole {
		return nil
	}
	profileDir := strings.TrimSpace(request.BrowserProfileDir)
	if profileDir == "" {
		return nil
	}
	return []string{
		BrowserProfileDirEnvVar + "=" + filepath.Clean(profileDir),
		BrowserStartURLEnvVar + "=" + noVNCStartURL(request.AllowedDomain),
	}
}

func noVNCStartURL(domain string) string {
	domain = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(domain)), ".")
	if domain == "" {
		return "about:blank"
	}
	return "https://" + domain + "/"
}

func newNoVNCStartResponse(request StartRequest, config NoVNCLaunchConfig, handles []ProcessHandle, results []ProcessResult) StartResponse {
	runnerID := buildNoVNCRunnerID(config, handles)
	processID := int64(0)
	if len(handles) > 0 {
		processID = handles[len(handles)-1].PID
	}
	response := StartResponse{
		Status:         StatusNotImplemented,
		RunnerID:       runnerID,
		ProcessID:      processID,
		SessionID:      request.SessionID,
		LoginRequestID: request.LoginRequestID,
		HandoffID:      strings.TrimSpace(request.HandoffID),
		ViewerURL:      buildNoVNCSessionViewerURL(config.PrivateBaseURL, runnerID),
		BindAddr:       config.BindAddr,
		PrivateBaseURL: config.PrivateBaseURL,
		ChildProcesses: results,
	}
	return response
}

func noVNCFullRealLaunchEnabled(config NoVNCLaunchConfig) bool {
	return config.RealDisplayEnabled && config.RealBrowserEnabled && config.RealWebsockifyEnabled
}

func noVNCStartedProcessResults(handles []ProcessHandle) []ProcessResult {
	results := make([]ProcessResult, 0, len(handles))
	for _, handle := range handles {
		results = append(results, ProcessResult{
			PID:         handle.PID,
			Role:        handle.Role,
			CommandPath: handle.CommandPath,
			StartedAt:   handle.StartedAt,
			Status:      ProcessStatusStarted,
		})
	}
	return results
}

func validateNoVNCFixtureSafeLaunchConfig(config NoVNCLaunchConfig) error {
	commands := []struct {
		label               string
		path                string
		allowRealBrowser    bool
		allowRealDisplay    bool
		allowRealWebsockify bool
	}{
		{label: "display command", path: config.DisplayCommand, allowRealDisplay: config.RealDisplayEnabled},
		{label: "browser command", path: config.BrowserCommand, allowRealBrowser: config.RealBrowserEnabled},
		{label: "websockify command", path: config.WebsockifyCommand, allowRealWebsockify: config.RealWebsockifyEnabled},
	}
	for _, command := range commands {
		name := filepath.Base(command.path)
		if _, ok := noVNCFixtureSafeCommandNames[name]; !ok {
			if command.allowRealBrowser {
				continue
			}
			if command.allowRealDisplay {
				continue
			}
			if command.allowRealWebsockify {
				continue
			}
			if command.label == "browser command" {
				return fmt.Errorf("real browser command %q requires %s=true", command.path, NoVNCRealBrowserEnvVar)
			}
			if command.label == "display command" {
				return fmt.Errorf("real display command %q requires %s=true", command.path, NoVNCRealDisplayEnvVar)
			}
			if command.label == "websockify command" {
				return fmt.Errorf("real websockify command %q requires %s=true", command.path, NoVNCRealWebsockifyEnvVar)
			}
			return fmt.Errorf("%s %q is not fixture-safe", command.label, command.path)
		}
	}
	return nil
}

func buildNoVNCRunnerID(config NoVNCLaunchConfig, handles []ProcessHandle) string {
	if len(handles) == 0 {
		return ""
	}
	parts := make([]string, 0, len(handles)+1)
	if noVNCFullRealLaunchEnabled(config) {
		parts = append(parts, "novnc-real")
	} else {
		parts = append(parts, "novnc")
	}
	for _, handle := range handles {
		parts = append(parts, strconv.FormatInt(handle.PID, 10))
	}
	return strings.Join(parts, "-")
}

func cancelNoVNCProcesses(ctx context.Context, supervisor ProcessSupervisor, handles []ProcessHandle, reason string) []ProcessResult {
	results := make([]ProcessResult, 0, len(handles))
	for index := len(handles) - 1; index >= 0; index-- {
		result, err := supervisor.CancelProcess(ctx, handles[index], reason)
		if err != nil {
			result = ProcessResult{
				PID:          handles[index].PID,
				Role:         handles[index].Role,
				CommandPath:  handles[index].CommandPath,
				StartedAt:    handles[index].StartedAt,
				Status:       ProcessStatusFailed,
				ErrorMessage: err.Error(),
			}
		}
		results = append(results, result)
	}
	return results
}

func noVNCProcessErrorMessage(result ProcessResult, fallback string) string {
	for _, candidate := range []string{result.ErrorMessage, result.Stderr, result.Stdout} {
		if trimmed := strings.TrimSpace(candidate); trimmed != "" {
			return trimmed
		}
	}
	return fallback
}

func (runner NoVNCRunner) Cancel(_ context.Context, request CancelRequest) (StatusResponse, error) {
	runnerID := strings.TrimSpace(request.RunnerID)
	if runnerID == "" {
		return StatusResponse{}, fmt.Errorf("runner_id is required")
	}
	return StatusResponse{
		Status:       StatusNotImplemented,
		RunnerID:     runnerID,
		ErrorCode:    "not_implemented",
		ErrorMessage: "browser handoff NoVNC runner cancellation is not implemented",
	}, nil
}

func (runner NoVNCRunner) LaunchCount() int {
	return 0
}

func PlanNoVNCStart(request StartRequest, config NoVNCRunnerConfig) (NoVNCPlan, error) {
	if err := ValidateStartRequest(request); err != nil {
		return NoVNCPlan{}, err
	}
	displayDetection, err := DetectNoVNCCommand("display command", NoVNCDisplayCommandRole, config.DisplayCommand, config.DisplayAllowedCommands)
	if err != nil {
		return NoVNCPlan{}, err
	}
	browserDetection, err := DetectNoVNCCommand("browser command", NoVNCBrowserCommandRole, config.BrowserCommand, config.BrowserAllowedCommands)
	if err != nil {
		return NoVNCPlan{}, err
	}
	novncDetection, err := DetectNoVNCWebsockifyCommand(config.NoVNCCommand, config.NoVNCAllowedCommands)
	if err != nil {
		return NoVNCPlan{}, err
	}
	bindAddr, err := validateNoVNCBindAddr(config.BindAddr)
	if err != nil {
		return NoVNCPlan{}, err
	}
	privateBaseURL, err := validateNoVNCPrivateBaseURL(config.PrivateBaseURL)
	if err != nil {
		return NoVNCPlan{}, err
	}
	timeoutSeconds, err := validateNoVNCTimeout(config.TimeoutSeconds, request.TimeoutSeconds)
	if err != nil {
		return NoVNCPlan{}, err
	}

	return NoVNCPlan{
		Commands: []NoVNCPlannedCommand{
			newNoVNCPlannedCommand("display", displayDetection),
			newNoVNCPlannedCommand("browser", browserDetection),
			newNoVNCPlannedCommand("novnc", novncDetection),
		},
		BindAddr:       bindAddr,
		PrivateBaseURL: privateBaseURL,
		ViewerURL:      buildNoVNCViewerURL(privateBaseURL, request.HandoffID),
		TimeoutSeconds: timeoutSeconds,
	}, nil
}

func DetectNoVNCWebsockifyCommand(command string, allowedCommands []string) (NoVNCCommandDetection, error) {
	return DetectNoVNCCommand("novnc command", NoVNCWebsockifyCommandRole, command, allowedCommands)
}

func DetectNoVNCCommand(label string, commandRole string, command string, allowedCommands []string) (NoVNCCommandDetection, error) {
	label = strings.TrimSpace(label)
	if label == "" {
		label = "command"
	}
	commandRole = strings.TrimSpace(commandRole)
	if commandRole == "" {
		commandRole = label
	}
	detection := NoVNCCommandDetection{
		CommandRole:      commandRole,
		ValidationStatus: NoVNCCommandValidationInvalid,
	}
	command = strings.TrimSpace(command)
	if command == "" {
		return noVNCCommandDetectionError(detection, NoVNCCommandErrorMissing, fmt.Sprintf("%s is required", label))
	}
	if !filepath.IsAbs(command) {
		return noVNCCommandDetectionError(detection, NoVNCCommandErrorRelative, fmt.Sprintf("%s must be an absolute path", label))
	}
	cleanCommand := filepath.Clean(command)
	detection.DetectedPath = cleanCommand
	if !isNoVNCCommandAllowlisted(cleanCommand, allowedCommands) {
		return noVNCCommandDetectionError(detection, NoVNCCommandErrorNotAllowlisted, fmt.Sprintf("%s %q is not in allowlist", label, cleanCommand))
	}
	info, err := os.Stat(cleanCommand)
	if err != nil {
		if os.IsNotExist(err) {
			return noVNCCommandDetectionError(detection, NoVNCCommandErrorNotFound, fmt.Sprintf("%s %q was not found", label, cleanCommand))
		}
		return noVNCCommandDetectionError(detection, NoVNCCommandErrorNotFound, fmt.Sprintf("%s %q could not be inspected: %v", label, cleanCommand, err))
	}
	if info.IsDir() || info.Mode().Perm()&0o111 == 0 {
		return noVNCCommandDetectionError(detection, NoVNCCommandErrorNotExecutable, fmt.Sprintf("%s %q is not executable", label, cleanCommand))
	}
	detection.ValidationStatus = NoVNCCommandValidationValid
	return detection, nil
}

func newNoVNCPlannedCommand(role string, detection NoVNCCommandDetection) NoVNCPlannedCommand {
	return NoVNCPlannedCommand{
		Role:             role,
		Path:             detection.DetectedPath,
		DetectedPath:     detection.DetectedPath,
		CommandRole:      detection.CommandRole,
		ValidationStatus: detection.ValidationStatus,
		ErrorCode:        detection.ErrorCode,
		ErrorMessage:     detection.ErrorMessage,
	}
}

func noVNCCommandDetectionError(detection NoVNCCommandDetection, code string, message string) (NoVNCCommandDetection, error) {
	detection.ValidationStatus = NoVNCCommandValidationInvalid
	detection.ErrorCode = code
	detection.ErrorMessage = message
	return detection, fmt.Errorf("%s", message)
}

func LoadNoVNCLaunchConfigFromEnv() (NoVNCLaunchConfig, error) {
	timeoutSeconds, err := noVNCTimeoutSecondsFromEnv()
	if err != nil {
		return NoVNCLaunchConfig{}, err
	}
	realBrowserEnabled, err := noVNCRealBrowserFromEnv()
	if err != nil {
		return NoVNCLaunchConfig{}, err
	}
	realDisplayEnabled, err := noVNCRealDisplayFromEnv()
	if err != nil {
		return NoVNCLaunchConfig{}, err
	}
	realWebsockifyEnabled, err := noVNCRealWebsockifyFromEnv()
	if err != nil {
		return NoVNCLaunchConfig{}, err
	}
	return NoVNCLaunchConfig{
		BrowserCommand:        strings.TrimSpace(os.Getenv(NoVNCBrowserCommandEnvVar)),
		DisplayCommand:        strings.TrimSpace(os.Getenv(NoVNCDisplayCommandEnvVar)),
		WebsockifyCommand:     strings.TrimSpace(os.Getenv(NoVNCWebsockifyCommandEnvVar)),
		AllowedCommandPaths:   splitNoVNCList(os.Getenv(NoVNCAllowedCommandsEnvVar)),
		BindAddr:              strings.TrimSpace(os.Getenv(NoVNCBindAddrEnvVar)),
		PrivateBaseURL:        strings.TrimSpace(os.Getenv(NoVNCPrivateBaseURLEnvVar)),
		TimeoutSeconds:        timeoutSeconds,
		RealBrowserEnabled:    realBrowserEnabled,
		RealDisplayEnabled:    realDisplayEnabled,
		RealWebsockifyEnabled: realWebsockifyEnabled,
	}, nil
}

func ValidateNoVNCLaunchConfig(config NoVNCLaunchConfig, requestTimeoutSeconds int) (NoVNCLaunchConfig, error) {
	browserCommand, err := validateNoVNCCommand("browser command", NoVNCBrowserCommandRole, config.BrowserCommand, config.AllowedCommandPaths)
	if err != nil {
		return NoVNCLaunchConfig{}, err
	}
	displayCommand, err := validateNoVNCCommand("display command", NoVNCDisplayCommandRole, config.DisplayCommand, config.AllowedCommandPaths)
	if err != nil {
		return NoVNCLaunchConfig{}, err
	}
	websockifyCommand, err := validateNoVNCCommand("websockify command", NoVNCWebsockifyCommandRole, config.WebsockifyCommand, config.AllowedCommandPaths)
	if err != nil {
		return NoVNCLaunchConfig{}, err
	}
	bindAddr := strings.TrimSpace(config.BindAddr)
	if bindAddr == "" {
		bindAddr = defaultNoVNCBindAddr
	}
	bindAddr, err = validateNoVNCBindAddr(bindAddr)
	if err != nil {
		return NoVNCLaunchConfig{}, err
	}
	privateBaseURL, err := validateNoVNCPrivateBaseURL(config.PrivateBaseURL)
	if err != nil {
		return NoVNCLaunchConfig{}, err
	}
	timeoutSeconds, err := validateNoVNCTimeout(config.TimeoutSeconds, requestTimeoutSeconds)
	if err != nil {
		return NoVNCLaunchConfig{}, err
	}
	return NoVNCLaunchConfig{
		BrowserCommand:        browserCommand,
		DisplayCommand:        displayCommand,
		WebsockifyCommand:     websockifyCommand,
		AllowedCommandPaths:   cleanNoVNCAllowedCommands(config.AllowedCommandPaths),
		BindAddr:              bindAddr,
		PrivateBaseURL:        privateBaseURL,
		TimeoutSeconds:        timeoutSeconds,
		RealBrowserEnabled:    config.RealBrowserEnabled,
		RealDisplayEnabled:    config.RealDisplayEnabled,
		RealWebsockifyEnabled: config.RealWebsockifyEnabled,
	}, nil
}

func validateNoVNCCommand(label string, commandRole string, command string, allowedCommands []string) (string, error) {
	detection, err := DetectNoVNCCommand(label, commandRole, command, allowedCommands)
	if err != nil {
		return "", err
	}
	return detection.DetectedPath, nil
}

func isNoVNCCommandAllowlisted(command string, allowedCommands []string) bool {
	for _, allowed := range allowedCommands {
		allowed = strings.TrimSpace(allowed)
		if allowed == "" {
			continue
		}
		if filepath.Clean(allowed) == command {
			return true
		}
	}
	return false
}

func validateNoVNCBindAddr(bindAddr string) (string, error) {
	bindAddr = strings.TrimSpace(bindAddr)
	if bindAddr == "" {
		return "", fmt.Errorf("bind_addr is required")
	}
	host, port, err := net.SplitHostPort(bindAddr)
	if err != nil {
		return "", fmt.Errorf("bind_addr must include host and port")
	}
	if strings.TrimSpace(port) == "" {
		return "", fmt.Errorf("bind_addr must include port")
	}
	if strings.EqualFold(host, "localhost") {
		return net.JoinHostPort("localhost", port), nil
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	if ip == nil {
		return "", fmt.Errorf("bind_addr host must be localhost or a private IP")
	}
	if ip.IsLoopback() || ip.IsPrivate() || isTailnetIP(ip) {
		return net.JoinHostPort(ip.String(), port), nil
	}
	return "", fmt.Errorf("bind_addr must not use a public interface")
}

func validateNoVNCPrivateBaseURL(rawURL string) (string, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return "", fmt.Errorf("private_base_url is required")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("private_base_url must be an absolute URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("private_base_url must use http or https")
	}
	return strings.TrimRight(parsed.String(), "/"), nil
}

func validateNoVNCTimeout(timeoutSeconds int, requestTimeoutSeconds int) (int, error) {
	if timeoutSeconds <= 0 {
		return 0, fmt.Errorf("timeout_seconds is required")
	}
	if requestTimeoutSeconds <= 0 {
		return 0, fmt.Errorf("request timeout_seconds must be positive")
	}
	if timeoutSeconds > requestTimeoutSeconds {
		return 0, fmt.Errorf("timeout_seconds must not exceed request timeout_seconds")
	}
	return timeoutSeconds, nil
}

func buildNoVNCViewerURL(privateBaseURL string, handoffID string) string {
	return strings.TrimRight(privateBaseURL, "/") + "/session/dry-run-" + url.PathEscape(strings.TrimSpace(handoffID))
}

func buildNoVNCSessionViewerURL(privateBaseURL string, runnerID string) string {
	runnerID = strings.TrimSpace(runnerID)
	if runnerID == "" {
		return ""
	}
	return strings.TrimRight(privateBaseURL, "/") + "/session/" + url.PathEscape(runnerID)
}

func noVNCTimeoutSecondsFromEnv() (int, error) {
	raw := strings.TrimSpace(os.Getenv(NoVNCTimeoutSecondsEnvVar))
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", NoVNCTimeoutSecondsEnvVar)
	}
	return value, nil
}

func noVNCRealBrowserFromEnv() (bool, error) {
	return noVNCBoolFromEnv(NoVNCRealBrowserEnvVar)
}

func noVNCRealDisplayFromEnv() (bool, error) {
	return noVNCBoolFromEnv(NoVNCRealDisplayEnvVar)
}

func noVNCRealWebsockifyFromEnv() (bool, error) {
	return noVNCBoolFromEnv(NoVNCRealWebsockifyEnvVar)
}

func noVNCBoolFromEnv(name string) (bool, error) {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	switch raw {
	case "":
		return false, nil
	case "1", "true", "yes", "y", "on":
		return true, nil
	case "0", "false", "no", "n", "off":
		return false, nil
	default:
		return false, fmt.Errorf("%s must be a boolean", name)
	}
}

func splitNoVNCList(raw string) []string {
	parts := strings.Split(raw, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			values = append(values, part)
		}
	}
	return values
}

func cleanNoVNCAllowedCommands(commands []string) []string {
	values := make([]string, 0, len(commands))
	for _, command := range commands {
		command = strings.TrimSpace(command)
		if command != "" {
			values = append(values, filepath.Clean(command))
		}
	}
	return values
}

func isTailnetIP(ip net.IP) bool {
	ip4 := ip.To4()
	if ip4 == nil {
		return false
	}
	return ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127
}
