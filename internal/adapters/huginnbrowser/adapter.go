package huginnbrowser

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

const AdapterEnvVar = "ODIN_BROWSER_ADAPTER"
const LiveCommandEnvVar = "ODIN_HUGINN_BROWSER_COMMAND"
const LiveAllowedCommandsEnvVar = "ODIN_HUGINN_BROWSER_ALLOWED_COMMANDS"
const LiveTimeoutEnvVar = "ODIN_HUGINN_BROWSER_TIMEOUT_SECONDS"

type Request struct {
	GoalID             int64                    `json:"goal_id,omitempty"`
	Mode               string                   `json:"mode,omitempty"`
	Objective          string                   `json:"objective"`
	StartURLs          []string                 `json:"start_urls"`
	AllowedDomains     []string                 `json:"allowed_domains"`
	MaxPages           int                      `json:"max_pages"`
	MaxDurationSeconds int                      `json:"max_duration_seconds"`
	EvidenceRequired   bool                     `json:"evidence_required"`
	SiteProfiles       []SiteProfile            `json:"site_profiles,omitempty"`
	BrowserSession     *BrowserSessionReference `json:"browser_session,omitempty"`
}

type SiteProfile struct {
	Domain             string `json:"domain"`
	MaxPages           int    `json:"max_pages,omitempty"`
	MinDelayMS         int    `json:"min_delay_ms,omitempty"`
	MaxDurationSeconds int    `json:"max_duration_seconds,omitempty"`
	ModeAllowed        string `json:"mode_allowed,omitempty"`
}

type BrowserSessionReference struct {
	ID                   int64  `json:"id"`
	Domain               string `json:"domain"`
	Status               string `json:"status"`
	PermissionTier       string `json:"permission_tier"`
	ProfileStoragePolicy string `json:"profile_storage_policy"`
	ProfilePath          string `json:"profile_path"`
	LastVerifiedAt       string `json:"last_verified_at,omitempty"`
}

type Response struct {
	Status                    string                   `json:"status"`
	AdapterKind               string                   `json:"adapter_kind"`
	VisitedURLs               []string                 `json:"visited_urls"`
	PageResults               []PageResult             `json:"page_results"`
	ExtractedTextSummary      string                   `json:"extracted_text_summary"`
	Screenshots               []string                 `json:"screenshots"`
	ScreenshotMetadata        []ScreenshotMetadata     `json:"screenshot_metadata,omitempty"`
	SelectedLinks             []SelectedLink           `json:"selected_links,omitempty"`
	DownloadedFiles           []DownloadedFileMetadata `json:"downloaded_files,omitempty"`
	FormStateSummary          string                   `json:"form_state_summary,omitempty"`
	BrowserErrorRecoveryNotes []string                 `json:"browser_error_recovery_notes,omitempty"`
	Confidence                string                   `json:"confidence,omitempty"`
	Limitations               []string                 `json:"limitations,omitempty"`
	ActionLog                 []string                 `json:"action_log"`
	Stdout                    string                   `json:"stdout,omitempty"`
	Stderr                    string                   `json:"stderr,omitempty"`
	ExitCode                  int                      `json:"exit_code,omitempty"`
	ErrorCode                 string                   `json:"error_code,omitempty"`
	ErrorMessage              string                   `json:"error_message,omitempty"`
}

type PageResult struct {
	URL          string `json:"url"`
	Status       string `json:"status"`
	Mode         string `json:"mode"`
	Title        string `json:"title,omitempty"`
	Summary      string `json:"summary,omitempty"`
	ErrorCode    string `json:"error_code,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`
}

type ScreenshotMetadata struct {
	Path       string `json:"path"`
	URL        string `json:"url,omitempty"`
	Title      string `json:"title,omitempty"`
	CapturedAt string `json:"captured_at,omitempty"`
}

type SelectedLink struct {
	Text   string `json:"text,omitempty"`
	URL    string `json:"url"`
	Reason string `json:"reason,omitempty"`
}

type DownloadedFileMetadata struct {
	Name        string `json:"name,omitempty"`
	Path        string `json:"path,omitempty"`
	ContentType string `json:"content_type,omitempty"`
	SizeBytes   int64  `json:"size_bytes,omitempty"`
	SHA256      string `json:"sha256,omitempty"`
}

type Adapter interface {
	Run(context.Context, Request) (Response, error)
}

type StubAdapter struct{}

func (StubAdapter) Run(_ context.Context, request Request) (Response, error) {
	if err := validateRequest(request); err != nil {
		return Response{}, err
	}
	visited := append([]string{}, request.StartURLs...)
	firstURL := visited[0]
	pageResults := make([]PageResult, 0, len(visited))
	for _, uri := range visited {
		pageResults = append(pageResults, PageResult{
			URL:     uri,
			Status:  "visited",
			Mode:    defaultString(request.Mode, "fetch"),
			Title:   "Stub Browser Evidence",
			Summary: "Stub/local read-only browser evidence for " + uri,
		})
	}
	return Response{
		Status:               "completed",
		AdapterKind:          "stub_local",
		VisitedURLs:          visited,
		PageResults:          pageResults,
		ExtractedTextSummary: "Stub/local read-only browser evidence for " + firstURL,
		Screenshots:          []string{"stub://huginnbrowser/screenshot/1"},
		ScreenshotMetadata: []ScreenshotMetadata{{
			Path:  "stub://huginnbrowser/screenshot/1",
			URL:   firstURL,
			Title: "Stub Browser Evidence",
		}},
		SelectedLinks: []SelectedLink{{
			Text:   "Stub evidence link",
			URL:    firstURL + "#evidence",
			Reason: "deterministic_stub",
		}},
		DownloadedFiles: []DownloadedFileMetadata{{
			Name:        "browser-evidence.txt",
			Path:        "stub://huginnbrowser/downloads/browser-evidence.txt",
			ContentType: "text/plain",
			SizeBytes:   0,
		}},
		FormStateSummary:          "No forms inspected or submitted.",
		BrowserErrorRecoveryNotes: []string{"No browser recovery required for stub evidence."},
		Confidence:                "deterministic_stub",
		Limitations:               []string{"Stub adapter does not launch a live browser."},
		ActionLog: []string{
			"stub_local_adapter_selected",
			"validated_read_only_request",
			"no_live_browser_launched",
			"no_external_mutation_performed",
		},
	}, nil
}

type LiveAdapter struct {
	Command         string
	AllowedCommands []string
	Timeout         time.Duration
}

func (adapter LiveAdapter) Run(ctx context.Context, request Request) (Response, error) {
	if err := validateRequest(request); err != nil {
		return Response{}, err
	}
	command := strings.TrimSpace(adapter.Command)
	if command == "" {
		command = strings.TrimSpace(os.Getenv(LiveCommandEnvVar))
	}
	if command == "" {
		return liveFailure("failed", "command_not_configured", "Huginn live browser command is not configured", request, "", "", 0), nil
	}
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return liveFailure("failed", "command_not_configured", "Huginn live browser command is not configured", request, "", "", 0), nil
	}
	allowedCommands := adapter.AllowedCommands
	if len(allowedCommands) == 0 {
		allowedCommands = allowedCommandsFromEnv()
	}
	if len(trimAllowedCommands(allowedCommands)) == 0 {
		return liveFailure("failed", "command_allowlist_empty", "Huginn live browser command allowlist is empty", request, "", "", 0), nil
	}
	if !commandAllowed(command, parts[0], allowedCommands) {
		return liveFailure("failed", "command_not_allowed", "Huginn live browser command is not allowlisted", request, "", "", 0), nil
	}
	timeout := adapter.Timeout
	if timeout <= 0 {
		timeout = timeoutFromEnv()
	}
	if timeout <= 0 {
		timeout = time.Duration(request.MaxDurationSeconds) * time.Second
	}
	if timeout <= 0 {
		return liveFailure("failed", "timeout_not_configured", "Huginn live browser timeout is not configured", request, "", "", 0), nil
	}

	payload, err := json.Marshal(request)
	if err != nil {
		return Response{}, err
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, parts[0], parts[1:]...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil && err != syscall.ESRCH {
			return err
		}
		return nil
	}
	cmd.Stdin = bytes.NewReader(payload)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	stdoutText := stdout.String()
	stderrText := stderr.String()
	if runCtx.Err() == context.DeadlineExceeded {
		return liveFailure("timeout", "command_timeout", "Huginn live browser command timed out", request, stdoutText, stderrText, exitCode(err)), nil
	}
	if err != nil {
		return liveFailure("failed", "command_failed", err.Error(), request, stdoutText, stderrText, exitCode(err)), nil
	}
	if strings.TrimSpace(stdoutText) == "" {
		return Response{
			Status:               "not_implemented",
			AdapterKind:          "huginn_live",
			VisitedURLs:          append([]string{}, request.StartURLs...),
			ExtractedTextSummary: "Huginn live browser command returned no evidence JSON.",
			Stdout:               stdoutText,
			Stderr:               stderrText,
			ActionLog:            liveActionLog("live_command_executed", "not_implemented", "no_live_browser_launched", "no_external_call_executed"),
		}, nil
	}
	if code, message := validateLiveResponseContract([]byte(stdoutText)); code != "" {
		return liveFailure("failed", code, message, request, stdoutText, stderrText, 0), nil
	}
	var response Response
	if err := json.Unmarshal([]byte(stdoutText), &response); err != nil {
		return liveFailure("failed", "invalid_response_json", err.Error(), request, stdoutText, stderrText, 0), nil
	}
	response = normalizeLiveResponse(response, request, stdoutText, stderrText)
	return response, nil
}

func SelectAdapterFromEnv() Adapter {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(AdapterEnvVar))) {
	case "live", "huginn", "huginn_live":
		return LiveAdapter{
			Command:         strings.TrimSpace(os.Getenv(LiveCommandEnvVar)),
			AllowedCommands: allowedCommandsFromEnv(),
			Timeout:         timeoutFromEnv(),
		}
	default:
		return StubAdapter{}
	}
}

func liveFailure(status string, code string, message string, request Request, stdout string, stderr string, exitCodeValue int) Response {
	return Response{
		Status:               status,
		AdapterKind:          "huginn_live",
		VisitedURLs:          append([]string{}, request.StartURLs...),
		PageResults:          failurePageResults(request, status, code, message),
		ExtractedTextSummary: "Huginn live browser adapter did not produce browsing evidence.",
		BrowserErrorRecoveryNotes: []string{
			"Inspect command configuration, timeout, and browser adapter stderr before retrying.",
		},
		Confidence:   "failed_capture",
		Limitations:  []string{"Live browser adapter did not complete evidence capture."},
		Stdout:       stdout,
		Stderr:       stderr,
		ExitCode:     exitCodeValue,
		ErrorCode:    code,
		ErrorMessage: message,
		ActionLog:    liveActionLog("live_command_attempted", code),
	}
}

func normalizeLiveResponse(response Response, request Request, stdout string, stderr string) Response {
	if len(response.VisitedURLs) == 0 {
		response.VisitedURLs = append([]string{}, request.StartURLs...)
	}
	if len(response.PageResults) == 0 {
		response.PageResults = visitedPageResults(request, response.VisitedURLs)
	}
	if strings.TrimSpace(response.ExtractedTextSummary) == "" {
		response.ExtractedTextSummary = "Huginn live browser command returned no text summary."
	}
	response.Stdout = stdout
	response.Stderr = stderr
	response.ActionLog = append(liveActionLog("live_command_executed"), response.ActionLog...)
	return response
}

func allowedCommandsFromEnv() []string {
	value := strings.TrimSpace(os.Getenv(LiveAllowedCommandsEnvVar))
	if value == "" {
		return nil
	}
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == '\n'
	})
	allowed := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			allowed = append(allowed, trimmed)
		}
	}
	return allowed
}

func trimAllowedCommands(commands []string) []string {
	trimmed := make([]string, 0, len(commands))
	for _, command := range commands {
		if candidate := strings.TrimSpace(command); candidate != "" {
			trimmed = append(trimmed, candidate)
		}
	}
	return trimmed
}

func commandAllowed(command string, executable string, allowedCommands []string) bool {
	command = strings.TrimSpace(command)
	executable = strings.TrimSpace(executable)
	for _, allowed := range trimAllowedCommands(allowedCommands) {
		if allowed == command || allowed == executable {
			return true
		}
	}
	return false
}

func validateLiveResponseContract(raw []byte) (string, string) {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return "invalid_response_json", err.Error()
	}
	for _, field := range mutationResponseFields {
		if _, ok := envelope[field]; ok {
			return "response_contract_invalid", fmt.Sprintf("live browser response includes mutation field %q", field)
		}
	}
	if code, message := requireStringField(envelope, "status"); code != "" {
		return code, message
	}
	if code, message := requireStringField(envelope, "adapter_kind"); code != "" {
		return code, message
	}
	if rawVisitedURLs, ok := envelope["visited_urls"]; ok {
		var visitedURLs []string
		if err := json.Unmarshal(rawVisitedURLs, &visitedURLs); err != nil {
			return "response_contract_invalid", "live browser response visited_urls must be a list"
		}
	}
	if rawPageResults, ok := envelope["page_results"]; ok {
		var pageResults []PageResult
		if err := json.Unmarshal(rawPageResults, &pageResults); err != nil {
			return "response_contract_invalid", "live browser response page_results must be a list"
		}
	}
	if rawScreenshots, ok := envelope["screenshot_metadata"]; ok {
		var screenshots []ScreenshotMetadata
		if err := json.Unmarshal(rawScreenshots, &screenshots); err != nil {
			return "response_contract_invalid", "live browser response screenshot_metadata must be a list"
		}
	}
	if rawLinks, ok := envelope["selected_links"]; ok {
		var links []SelectedLink
		if err := json.Unmarshal(rawLinks, &links); err != nil {
			return "response_contract_invalid", "live browser response selected_links must be a list"
		}
	}
	if rawDownloads, ok := envelope["downloaded_files"]; ok {
		var downloads []DownloadedFileMetadata
		if err := json.Unmarshal(rawDownloads, &downloads); err != nil {
			return "response_contract_invalid", "live browser response downloaded_files must be a list"
		}
	}
	return "", ""
}

func requireStringField(envelope map[string]json.RawMessage, field string) (string, string) {
	rawValue, ok := envelope[field]
	if !ok {
		return "response_contract_invalid", fmt.Sprintf("live browser response missing required field %q", field)
	}
	var value string
	if err := json.Unmarshal(rawValue, &value); err != nil || strings.TrimSpace(value) == "" {
		return "response_contract_invalid", fmt.Sprintf("live browser response field %q must be a non-empty string", field)
	}
	return "", ""
}

var mutationResponseFields = []string{
	"clicked_buttons",
	"clicked_selectors",
	"deleted_items",
	"external_mutations",
	"form_submissions",
	"login_performed",
	"mutation_actions",
	"mutations",
	"posted_messages",
	"purchases",
	"session_tokens",
	"submitted_forms",
}

func liveActionLog(items ...string) []string {
	log := []string{
		"live_adapter_selected",
		"validated_read_only_request",
		"no_login_or_session_handling",
		"no_form_submission",
		"no_external_mutation_performed",
	}
	log = append(log, items...)
	return log
}

func timeoutFromEnv() time.Duration {
	value := strings.TrimSpace(os.Getenv(LiveTimeoutEnvVar))
	if value == "" {
		return 0
	}
	seconds, err := time.ParseDuration(value + "s")
	if err != nil {
		return 0
	}
	return seconds
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return exitErr.ExitCode()
	}
	return -1
}

func defaultString(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func failurePageResults(request Request, status string, code string, message string) []PageResult {
	resultStatus := "failed"
	if status == "timeout" {
		resultStatus = "failed"
	}
	results := make([]PageResult, 0, len(request.StartURLs))
	for _, uri := range request.StartURLs {
		results = append(results, PageResult{
			URL:          uri,
			Status:       resultStatus,
			Mode:         defaultString(request.Mode, "fetch"),
			ErrorCode:    code,
			ErrorMessage: message,
		})
	}
	return results
}

func visitedPageResults(request Request, visitedURLs []string) []PageResult {
	results := make([]PageResult, 0, len(visitedURLs))
	for _, uri := range visitedURLs {
		results = append(results, PageResult{
			URL:    uri,
			Status: "visited",
			Mode:   defaultString(request.Mode, "fetch"),
		})
	}
	return results
}

func validateRequest(request Request) error {
	if strings.TrimSpace(request.Objective) == "" {
		return fmt.Errorf("objective is required")
	}
	if len(request.StartURLs) == 0 {
		return fmt.Errorf("start_urls is required")
	}
	if len(request.AllowedDomains) == 0 {
		return fmt.Errorf("allowed_domains is required")
	}
	if request.MaxPages <= 0 {
		return fmt.Errorf("max_pages must be positive")
	}
	if request.MaxDurationSeconds <= 0 {
		return fmt.Errorf("max_duration_seconds must be positive")
	}
	for _, profile := range request.SiteProfiles {
		if strings.TrimSpace(profile.Domain) == "" {
			return fmt.Errorf("site profile domain is required")
		}
		if profile.MaxPages < 0 {
			return fmt.Errorf("site profile max_pages must not be negative")
		}
		if profile.MinDelayMS < 0 {
			return fmt.Errorf("site profile min_delay_ms must not be negative")
		}
		if profile.MaxDurationSeconds < 0 {
			return fmt.Errorf("site profile max_duration_seconds must not be negative")
		}
		switch strings.ToLower(strings.TrimSpace(profile.ModeAllowed)) {
		case "", "fetch", "browser", "both":
		default:
			return fmt.Errorf("site profile mode_allowed must be fetch, browser, or both")
		}
	}
	if request.BrowserSession != nil {
		if request.BrowserSession.ID <= 0 {
			return fmt.Errorf("browser session id must be positive")
		}
		if strings.TrimSpace(request.BrowserSession.Domain) == "" {
			return fmt.Errorf("browser session domain is required")
		}
		if strings.TrimSpace(request.BrowserSession.Status) == "" {
			return fmt.Errorf("browser session status is required")
		}
		if strings.TrimSpace(request.BrowserSession.ProfilePath) == "" {
			return fmt.Errorf("browser session profile path is required")
		}
	}
	return nil
}
