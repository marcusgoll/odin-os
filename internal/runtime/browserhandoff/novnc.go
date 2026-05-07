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

	defaultNoVNCBindAddr = "127.0.0.1:0"
)

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
	BrowserCommand      string
	DisplayCommand      string
	WebsockifyCommand   string
	AllowedCommandPaths []string
	BindAddr            string
	PrivateBaseURL      string
	TimeoutSeconds      int
}

type NoVNCRunner struct {
	LoadConfig func() (NoVNCLaunchConfig, error)
}

type NoVNCPlan struct {
	Commands       []NoVNCPlannedCommand `json:"commands"`
	BindAddr       string                `json:"bind_addr"`
	PrivateBaseURL string                `json:"private_base_url"`
	ViewerURL      string                `json:"viewer_url"`
	TimeoutSeconds int                   `json:"timeout_seconds"`
}

type NoVNCPlannedCommand struct {
	Role string   `json:"role"`
	Path string   `json:"path"`
	Args []string `json:"args,omitempty"`
}

func (runner NoVNCRunner) Start(_ context.Context, request StartRequest) (StartResponse, error) {
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
	if _, err := ValidateNoVNCLaunchConfig(config, request.TimeoutSeconds); err != nil {
		return StartResponse{}, err
	}
	return StartResponse{
		Status:         StatusNotImplemented,
		SessionID:      request.SessionID,
		LoginRequestID: request.LoginRequestID,
		HandoffID:      strings.TrimSpace(request.HandoffID),
		ErrorCode:      "not_implemented",
		ErrorMessage:   "browser handoff NoVNC runner process launch is not implemented",
	}, nil
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
	browserCommand, err := validateNoVNCCommand("browser command", config.BrowserCommand, config.BrowserAllowedCommands)
	if err != nil {
		return NoVNCPlan{}, err
	}
	displayCommand, err := validateNoVNCCommand("display command", config.DisplayCommand, config.DisplayAllowedCommands)
	if err != nil {
		return NoVNCPlan{}, err
	}
	novncCommand, err := validateNoVNCCommand("novnc command", config.NoVNCCommand, config.NoVNCAllowedCommands)
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
			{Role: "display", Path: displayCommand},
			{Role: "browser", Path: browserCommand},
			{Role: "novnc", Path: novncCommand},
		},
		BindAddr:       bindAddr,
		PrivateBaseURL: privateBaseURL,
		ViewerURL:      buildNoVNCViewerURL(privateBaseURL, request.HandoffID),
		TimeoutSeconds: timeoutSeconds,
	}, nil
}

func LoadNoVNCLaunchConfigFromEnv() (NoVNCLaunchConfig, error) {
	timeoutSeconds, err := noVNCTimeoutSecondsFromEnv()
	if err != nil {
		return NoVNCLaunchConfig{}, err
	}
	return NoVNCLaunchConfig{
		BrowserCommand:      strings.TrimSpace(os.Getenv(NoVNCBrowserCommandEnvVar)),
		DisplayCommand:      strings.TrimSpace(os.Getenv(NoVNCDisplayCommandEnvVar)),
		WebsockifyCommand:   strings.TrimSpace(os.Getenv(NoVNCWebsockifyCommandEnvVar)),
		AllowedCommandPaths: splitNoVNCList(os.Getenv(NoVNCAllowedCommandsEnvVar)),
		BindAddr:            strings.TrimSpace(os.Getenv(NoVNCBindAddrEnvVar)),
		PrivateBaseURL:      strings.TrimSpace(os.Getenv(NoVNCPrivateBaseURLEnvVar)),
		TimeoutSeconds:      timeoutSeconds,
	}, nil
}

func ValidateNoVNCLaunchConfig(config NoVNCLaunchConfig, requestTimeoutSeconds int) (NoVNCLaunchConfig, error) {
	browserCommand, err := validateNoVNCCommand("browser command", config.BrowserCommand, config.AllowedCommandPaths)
	if err != nil {
		return NoVNCLaunchConfig{}, err
	}
	displayCommand, err := validateNoVNCCommand("display command", config.DisplayCommand, config.AllowedCommandPaths)
	if err != nil {
		return NoVNCLaunchConfig{}, err
	}
	websockifyCommand, err := validateNoVNCCommand("websockify command", config.WebsockifyCommand, config.AllowedCommandPaths)
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
		BrowserCommand:      browserCommand,
		DisplayCommand:      displayCommand,
		WebsockifyCommand:   websockifyCommand,
		AllowedCommandPaths: cleanNoVNCAllowedCommands(config.AllowedCommandPaths),
		BindAddr:            bindAddr,
		PrivateBaseURL:      privateBaseURL,
		TimeoutSeconds:      timeoutSeconds,
	}, nil
}

func validateNoVNCCommand(label string, command string, allowedCommands []string) (string, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return "", fmt.Errorf("%s is required", label)
	}
	if !filepath.IsAbs(command) {
		return "", fmt.Errorf("%s must be an absolute path", label)
	}
	cleanCommand := filepath.Clean(command)
	for _, allowed := range allowedCommands {
		allowed = strings.TrimSpace(allowed)
		if allowed == "" {
			continue
		}
		if filepath.Clean(allowed) == cleanCommand {
			return cleanCommand, nil
		}
	}
	return "", fmt.Errorf("%s %q is not in allowlist", label, cleanCommand)
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
