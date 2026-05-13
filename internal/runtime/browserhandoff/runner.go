package browserhandoff

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

const (
	StatusNotImplemented = "not_implemented"
	StatusStarted        = "started"
	StatusCompleted      = "completed"
	StatusFailed         = "failed"
	StatusExpired        = "expired"
	StatusCancelled      = "cancelled"
)

type StartRequest struct {
	SessionID      int64  `json:"session_id"`
	LoginRequestID int64  `json:"login_request_id"`
	HandoffID      string `json:"handoff_id"`
	ProfilePath    string `json:"profile_path"`
	AllowedDomain  string `json:"allowed_domain"`
	TimeoutSeconds int    `json:"timeout_seconds"`
	BindAddr       string `json:"bind_addr,omitempty"`
	PrivateBaseURL string `json:"private_base_url,omitempty"`
	PublicBaseURL  string `json:"public_base_url,omitempty"`
}

type StartResponse struct {
	Status         string          `json:"status"`
	RunnerID       string          `json:"runner_id,omitempty"`
	ProcessID      int64           `json:"process_id,omitempty"`
	SessionID      int64           `json:"session_id"`
	LoginRequestID int64           `json:"login_request_id"`
	HandoffID      string          `json:"handoff_id"`
	ViewerURL      string          `json:"viewer_url,omitempty"`
	BindAddr       string          `json:"bind_addr,omitempty"`
	PrivateBaseURL string          `json:"private_base_url,omitempty"`
	ExpiresAt      string          `json:"expires_at,omitempty"`
	ErrorCode      string          `json:"error_code,omitempty"`
	ErrorMessage   string          `json:"error_message,omitempty"`
	ChildProcesses []ProcessResult `json:"child_processes,omitempty"`
}

type CancelRequest struct {
	RunnerID string `json:"runner_id"`
	Reason   string `json:"reason,omitempty"`
}

type StatusResponse struct {
	Status       string `json:"status"`
	RunnerID     string `json:"runner_id,omitempty"`
	ProcessID    int64  `json:"process_id,omitempty"`
	ViewerURL    string `json:"viewer_url,omitempty"`
	ErrorCode    string `json:"error_code,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`
}

type Runner interface {
	Start(context.Context, StartRequest) (StartResponse, error)
	Cancel(context.Context, CancelRequest) (StatusResponse, error)
}

type StubRunner struct{}

func (runner StubRunner) Start(_ context.Context, request StartRequest) (StartResponse, error) {
	if err := ValidateStartRequest(request); err != nil {
		return StartResponse{}, err
	}
	return StartResponse{
		Status:         StatusNotImplemented,
		SessionID:      request.SessionID,
		LoginRequestID: request.LoginRequestID,
		HandoffID:      strings.TrimSpace(request.HandoffID),
		ErrorCode:      "not_implemented",
		ErrorMessage:   "browser handoff runner process boundary is not implemented",
	}, nil
}

func (runner StubRunner) Cancel(_ context.Context, request CancelRequest) (StatusResponse, error) {
	runnerID := strings.TrimSpace(request.RunnerID)
	if runnerID == "" {
		return StatusResponse{}, fmt.Errorf("runner_id is required")
	}
	return StatusResponse{
		Status:       StatusNotImplemented,
		RunnerID:     runnerID,
		ErrorCode:    "not_implemented",
		ErrorMessage: "browser handoff runner cancellation is not implemented",
	}, nil
}

func (runner StubRunner) LaunchCount() int {
	return 0
}

func ValidateStartRequest(request StartRequest) error {
	if request.SessionID <= 0 {
		return fmt.Errorf("session_id must be positive")
	}
	if request.LoginRequestID <= 0 {
		return fmt.Errorf("login_request_id must be positive")
	}
	if strings.TrimSpace(request.HandoffID) == "" {
		return fmt.Errorf("handoff_id is required")
	}
	if err := validateProfilePath(request.ProfilePath); err != nil {
		return err
	}
	if err := validateAllowedDomain(request.AllowedDomain); err != nil {
		return err
	}
	if request.TimeoutSeconds <= 0 {
		return fmt.Errorf("timeout_seconds must be positive")
	}
	if err := validateBindAddr(request.BindAddr); err != nil {
		return err
	}
	if strings.TrimSpace(request.PublicBaseURL) != "" {
		return fmt.Errorf("public_base_url is not supported by the stub boundary")
	}
	return nil
}

func validateProfilePath(profilePath string) error {
	profilePath = strings.TrimSpace(profilePath)
	if profilePath == "" {
		return fmt.Errorf("profile_path is required")
	}
	if filepath.IsAbs(profilePath) {
		return fmt.Errorf("profile_path must be relative to ODIN_ROOT")
	}
	clean := filepath.ToSlash(filepath.Clean(profilePath))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") {
		return fmt.Errorf("profile_path must stay under ODIN_ROOT")
	}
	if !strings.HasPrefix(clean, "browser-sessions/profiles/") {
		return fmt.Errorf("profile_path must stay under browser-sessions/profiles")
	}
	if strings.TrimSpace(strings.TrimPrefix(clean, "browser-sessions/profiles/")) == "" {
		return fmt.Errorf("profile_path must include a profile component")
	}
	return nil
}

func validateAllowedDomain(domain string) error {
	domain = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(domain)), ".")
	if domain == "" {
		return fmt.Errorf("allowed_domain is required")
	}
	if strings.Contains(domain, "/") || strings.Contains(domain, ":") || strings.Contains(domain, "@") {
		return fmt.Errorf("allowed_domain must be a hostname")
	}
	return nil
}

func validateBindAddr(bindAddr string) error {
	bindAddr = strings.TrimSpace(bindAddr)
	if bindAddr == "" {
		return nil
	}
	if strings.HasPrefix(bindAddr, "127.0.0.1:") || strings.HasPrefix(bindAddr, "localhost:") || strings.HasPrefix(bindAddr, "[::1]:") {
		return nil
	}
	return fmt.Errorf("bind_addr must use loopback in the stub boundary")
}
