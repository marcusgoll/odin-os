package browserhandoff

import (
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"strings"
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

func isTailnetIP(ip net.IP) bool {
	ip4 := ip.To4()
	if ip4 == nil {
		return false
	}
	return ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127
}
