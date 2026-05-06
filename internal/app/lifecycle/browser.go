package lifecycle

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"odin-os/internal/app/bootstrap"
	commands "odin-os/internal/cli/commands"
	browserexecutor "odin-os/internal/executors/browser"
	"odin-os/internal/runtime/browserhandoff"
	"odin-os/internal/store/sqlite"
)

type browserRunView struct {
	Status               string                       `json:"status"`
	GoalID               int64                        `json:"goal_id"`
	EvidenceID           int64                        `json:"evidence_id"`
	EvidenceType         string                       `json:"evidence_type"`
	AdapterStatus        string                       `json:"adapter_status,omitempty"`
	AdapterKind          string                       `json:"adapter_kind,omitempty"`
	StartURLs            []string                     `json:"start_urls"`
	AllowedDomains       []string                     `json:"allowed_domains"`
	MaxPages             int                          `json:"max_pages"`
	MaxDurationSeconds   int                          `json:"max_duration_seconds"`
	VisitedURLs          []string                     `json:"visited_urls,omitempty"`
	PageResults          []browserexecutor.PageResult `json:"page_results,omitempty"`
	ExtractedTextSummary string                       `json:"extracted_text_summary,omitempty"`
	Screenshots          []string                     `json:"screenshots,omitempty"`
	ActionLog            []string                     `json:"action_log,omitempty"`
}

type browserSessionEnvelope struct {
	Session browserSessionView `json:"session"`
}

type browserSessionListView struct {
	Sessions []browserSessionView `json:"sessions"`
}

type browserSessionLoginRequestEnvelope struct {
	LoginRequest browserSessionLoginRequestView `json:"login_request"`
}

type browserSessionLoginRequestListView struct {
	LoginRequests []browserSessionLoginRequestView `json:"login_requests"`
}

type browserSessionHandoffEnvelope struct {
	Handoff browserSessionHandoffView `json:"handoff"`
}

type browserSessionRunnerEnvelope struct {
	Runner browserSessionRunnerView `json:"runner"`
}

type browserSessionRunnerListView struct {
	Runners []browserSessionRunnerView `json:"runners"`
}

type browserSessionProfileEnvelope struct {
	Profile browserSessionProfileView `json:"profile"`
}

type browserSessionView struct {
	ID                   int64  `json:"id"`
	Name                 string `json:"name"`
	Domain               string `json:"domain"`
	AccountHint          string `json:"account_hint,omitempty"`
	PermissionTier       string `json:"permission_tier"`
	Status               string `json:"status"`
	ProfileStoragePolicy string `json:"profile_storage_policy"`
	ProfilePath          string `json:"profile_path"`
	ProfilePathExists    bool   `json:"profile_path_exists"`
	CreatedAt            string `json:"created_at"`
	UpdatedAt            string `json:"updated_at"`
	LastVerifiedAt       string `json:"last_verified_at,omitempty"`
	ExpiresAt            string `json:"expires_at,omitempty"`
	RevokedAt            string `json:"revoked_at,omitempty"`
}

type browserSessionProfileView struct {
	SessionID         int64  `json:"session_id"`
	ProfilePath       string `json:"profile_path"`
	ProfilePathExists bool   `json:"profile_path_exists"`
	Created           bool   `json:"created"`
}

type browserSessionLoginRequestView struct {
	ID          int64   `json:"id"`
	SessionID   int64   `json:"session_id"`
	Status      string  `json:"status"`
	HandoffID   string  `json:"handoff_id"`
	HandoffURL  *string `json:"handoff_url"`
	ExpiresAt   string  `json:"expires_at"`
	CompletedAt string  `json:"completed_at,omitempty"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

type browserSessionHandoffView struct {
	HandoffID      string `json:"handoff_id"`
	LoginRequestID int64  `json:"login_request_id"`
	SessionID      int64  `json:"session_id"`
	SessionName    string `json:"session_name"`
	Domain         string `json:"domain"`
	AccountHint    string `json:"account_hint,omitempty"`
	ExpiresAt      string `json:"expires_at"`
	Status         string `json:"status"`
	AllowedActions string `json:"allowed_actions"`
}

type browserSessionRunnerView struct {
	ID             int64   `json:"id"`
	SessionID      int64   `json:"session_id"`
	LoginRequestID int64   `json:"login_request_id"`
	HandoffID      string  `json:"handoff_id"`
	Status         string  `json:"status"`
	ViewerURL      *string `json:"viewer_url"`
	RunnerID       *string `json:"runner_id"`
	ProcessID      *int64  `json:"process_id"`
	BindAddr       *string `json:"bind_addr"`
	PrivateBaseURL *string `json:"private_base_url"`
	PublicBaseURL  *string `json:"public_base_url"`
	ExpiresAt      string  `json:"expires_at"`
	StartedAt      string  `json:"started_at,omitempty"`
	CompletedAt    string  `json:"completed_at,omitempty"`
	CancelledAt    string  `json:"cancelled_at,omitempty"`
	CreatedAt      string  `json:"created_at"`
	UpdatedAt      string  `json:"updated_at"`
	ErrorCode      *string `json:"error_code"`
	ErrorMessage   *string `json:"error_message"`
}

func runBrowser(ctx context.Context, app bootstrap.App, args []string, stdout io.Writer) error {
	command, err := commands.ParseBrowser(args)
	if err != nil {
		return err
	}
	if command.Name == "help" {
		_, err := fmt.Fprintln(stdout, commands.BrowserUsage)
		return err
	}
	if command.Name == "session" {
		return runBrowserSession(ctx, app, command, stdout)
	}

	goal, err := app.Store.GetGoal(ctx, command.GoalID)
	if err != nil {
		return err
	}
	objective := strings.TrimSpace(command.Objective)
	if objective == "" {
		objective = goal.Title
	}
	result, err := browserexecutor.Service{Store: app.Store}.Run(ctx, browserexecutor.ReadOnlyTask{
		GoalID:             command.GoalID,
		WorkerMode:         command.WorkerMode,
		Objective:          objective,
		AllowedDomains:     command.AllowedDomains,
		StartURLs:          command.URLs,
		MaxPages:           command.MaxPages,
		MaxDurationSeconds: command.MaxDurationSeconds,
		EvidenceRequired:   command.EvidenceRequired,
		Actions:            command.Actions,
	})
	if err != nil {
		return err
	}
	view := browserRunView{
		Status:               result.Status,
		GoalID:               result.GoalID,
		EvidenceID:           result.EvidenceID,
		EvidenceType:         result.EvidenceType,
		AdapterStatus:        result.AdapterStatus,
		AdapterKind:          result.AdapterKind,
		StartURLs:            result.StartURLs,
		AllowedDomains:       result.AllowedDomains,
		MaxPages:             result.MaxPages,
		MaxDurationSeconds:   result.MaxDurationSeconds,
		VisitedURLs:          result.VisitedURLs,
		PageResults:          result.PageResults,
		ExtractedTextSummary: result.ExtractedTextSummary,
		Screenshots:          result.Screenshots,
		ActionLog:            result.ActionLog,
	}
	if command.JSON {
		return commands.WriteJSON(stdout, view)
	}
	_, err = fmt.Fprintf(stdout, "browser goal=%d status=%s evidence=%d type=%s\n", view.GoalID, view.Status, view.EvidenceID, view.EvidenceType)
	return err
}

func runBrowserSession(ctx context.Context, app bootstrap.App, command commands.BrowserCommand, stdout io.Writer) error {
	switch command.SessionAction {
	case "create":
		session, err := app.Store.CreateBrowserSession(ctx, sqlite.CreateBrowserSessionParams{
			Name:           command.SessionName,
			Domain:         command.SessionDomain,
			AccountHint:    command.AccountHint,
			PermissionTier: browserSessionPermissionTier(command.PermissionTier),
			ProfilePath:    command.ProfilePath,
		})
		if err != nil {
			return err
		}
		view := newBrowserSessionView(app.RuntimeRoot, session)
		if command.JSON {
			return commands.WriteJSON(stdout, browserSessionEnvelope{Session: view})
		}
		_, err = fmt.Fprintf(stdout, "browser_session=%d status=%s name=%q domain=%s\n", view.ID, view.Status, view.Name, view.Domain)
		return err
	case "list":
		sessions, err := app.Store.ListBrowserSessions(ctx, sqlite.ListBrowserSessionsParams{})
		if err != nil {
			return err
		}
		views := make([]browserSessionView, 0, len(sessions))
		for _, session := range sessions {
			views = append(views, newBrowserSessionView(app.RuntimeRoot, session))
		}
		if command.JSON {
			return commands.WriteJSON(stdout, browserSessionListView{Sessions: views})
		}
		if len(views) == 0 {
			_, err := fmt.Fprintln(stdout, "no browser sessions")
			return err
		}
		for _, view := range views {
			if _, err := fmt.Fprintf(stdout, "browser_session=%d status=%s name=%q domain=%s\n", view.ID, view.Status, view.Name, view.Domain); err != nil {
				return err
			}
		}
		return nil
	case "show":
		session, err := app.Store.GetBrowserSession(ctx, command.ID)
		if err != nil {
			return err
		}
		view := newBrowserSessionView(app.RuntimeRoot, session)
		if command.JSON {
			return commands.WriteJSON(stdout, browserSessionEnvelope{Session: view})
		}
		_, err = fmt.Fprintf(stdout, "browser_session=%d status=%s name=%q domain=%s\n", view.ID, view.Status, view.Name, view.Domain)
		return err
	case "status":
		session, err := app.Store.UpdateBrowserSessionStatus(ctx, sqlite.UpdateBrowserSessionStatusParams{
			SessionID: command.ID,
			Status:    sqlite.BrowserSessionStatus(command.Status),
			Actor:     "operator",
			Reason:    "operator updated browser session status",
		})
		if err != nil {
			return err
		}
		view := newBrowserSessionView(app.RuntimeRoot, session)
		if command.JSON {
			return commands.WriteJSON(stdout, browserSessionEnvelope{Session: view})
		}
		_, err = fmt.Fprintf(stdout, "browser_session=%d status=%s name=%q domain=%s\n", view.ID, view.Status, view.Name, view.Domain)
		return err
	case "revoke":
		session, err := app.Store.RevokeBrowserSession(ctx, sqlite.RevokeBrowserSessionParams{
			SessionID: command.ID,
			Actor:     "operator",
			Reason:    "operator revoked browser session metadata",
		})
		if err != nil {
			return err
		}
		view := newBrowserSessionView(app.RuntimeRoot, session)
		if command.JSON {
			return commands.WriteJSON(stdout, browserSessionEnvelope{Session: view})
		}
		_, err = fmt.Fprintf(stdout, "browser_session=%d status=%s name=%q domain=%s\n", view.ID, view.Status, view.Name, view.Domain)
		return err
	case "verify":
		session, _, err := app.Store.VerifyBrowserSession(ctx, sqlite.VerifyBrowserSessionParams{
			SessionID:      command.ID,
			LoginRequestID: command.LoginRequestID,
			Actor:          "operator",
			Reason:         "operator manually verified browser session metadata",
		})
		if err != nil {
			return err
		}
		view := newBrowserSessionView(app.RuntimeRoot, session)
		if command.JSON {
			return commands.WriteJSON(stdout, browserSessionEnvelope{Session: view})
		}
		_, err = fmt.Fprintf(stdout, "browser_session=%d status=%s name=%q domain=%s\n", view.ID, view.Status, view.Name, view.Domain)
		return err
	case "prepare-profile":
		session, err := app.Store.GetBrowserSession(ctx, command.ID)
		if err != nil {
			return err
		}
		view, err := prepareBrowserSessionProfile(ctx, app, session)
		if err != nil {
			return err
		}
		if command.JSON {
			return commands.WriteJSON(stdout, browserSessionProfileEnvelope{Profile: view})
		}
		_, err = fmt.Fprintf(stdout, "browser_session_profile=%d path=%s exists=%t created=%t\n", view.SessionID, view.ProfilePath, view.ProfilePathExists, view.Created)
		return err
	case "login-request":
		request, err := app.Store.CreateBrowserSessionLoginRequest(ctx, sqlite.CreateBrowserSessionLoginRequestParams{
			SessionID:      command.ID,
			HandoffBaseURL: command.HandoffBaseURL,
			ExpiresAt:      time.Now().UTC().Add(10 * time.Minute),
		})
		if err != nil {
			return err
		}
		view := newBrowserSessionLoginRequestView(request)
		if command.JSON {
			return commands.WriteJSON(stdout, browserSessionLoginRequestEnvelope{LoginRequest: view})
		}
		_, err = fmt.Fprintf(stdout, "browser_session_login_request=%d session=%d status=%s\n", view.ID, view.SessionID, view.Status)
		return err
	case "login-requests":
		requests, err := app.Store.ListBrowserSessionLoginRequests(ctx, sqlite.ListBrowserSessionLoginRequestsParams{SessionID: command.ID})
		if err != nil {
			return err
		}
		views := make([]browserSessionLoginRequestView, 0, len(requests))
		for _, request := range requests {
			views = append(views, newBrowserSessionLoginRequestView(request))
		}
		if command.JSON {
			return commands.WriteJSON(stdout, browserSessionLoginRequestListView{LoginRequests: views})
		}
		if len(views) == 0 {
			_, err := fmt.Fprintln(stdout, "no browser session login requests")
			return err
		}
		for _, view := range views {
			if _, err := fmt.Fprintf(stdout, "browser_session_login_request=%d session=%d status=%s\n", view.ID, view.SessionID, view.Status); err != nil {
				return err
			}
		}
		return nil
	case "handoff":
		handoff, err := app.Store.GetBrowserSessionLoginHandoff(ctx, command.HandoffID)
		if err != nil {
			return err
		}
		view := newBrowserSessionHandoffView(handoff)
		if command.JSON {
			return commands.WriteJSON(stdout, browserSessionHandoffEnvelope{Handoff: view})
		}
		_, err = fmt.Fprintf(stdout, "browser_session_handoff=%s login_request=%d session=%d status=%s allowed_actions=%s\n", view.HandoffID, view.LoginRequestID, view.SessionID, view.Status, view.AllowedActions)
		return err
	case "runner":
		return runBrowserSessionRunner(ctx, app, command, stdout)
	default:
		return fmt.Errorf(commands.BrowserUsage)
	}
}

func runBrowserSessionRunner(ctx context.Context, app bootstrap.App, command commands.BrowserCommand, stdout io.Writer) error {
	switch command.RunnerAction {
	case "create":
		request, err := app.Store.GetBrowserSessionLoginRequest(ctx, command.LoginRequestID)
		if err != nil {
			return err
		}
		runner, err := app.Store.CreateBrowserHandoffRunner(ctx, sqlite.CreateBrowserHandoffRunnerParams{
			SessionID:      request.SessionID,
			LoginRequestID: request.ID,
			HandoffID:      request.HandoffID,
			ExpiresAt:      request.ExpiresAt,
		})
		if err != nil {
			return err
		}
		view := newBrowserSessionRunnerView(runner)
		if command.JSON {
			return commands.WriteJSON(stdout, browserSessionRunnerEnvelope{Runner: view})
		}
		_, err = fmt.Fprintf(stdout, "browser_session_runner=%d login_request=%d session=%d status=%s\n", view.ID, view.LoginRequestID, view.SessionID, view.Status)
		return err
	case "list":
		runners, err := app.Store.ListBrowserHandoffRunners(ctx, sqlite.ListBrowserHandoffRunnersParams{LoginRequestID: command.LoginRequestID})
		if err != nil {
			return err
		}
		views := make([]browserSessionRunnerView, 0, len(runners))
		for _, runner := range runners {
			views = append(views, newBrowserSessionRunnerView(runner))
		}
		if command.JSON {
			return commands.WriteJSON(stdout, browserSessionRunnerListView{Runners: views})
		}
		if len(views) == 0 {
			_, err := fmt.Fprintln(stdout, "no browser session handoff runners")
			return err
		}
		for _, view := range views {
			if _, err := fmt.Fprintf(stdout, "browser_session_runner=%d login_request=%d session=%d status=%s\n", view.ID, view.LoginRequestID, view.SessionID, view.Status); err != nil {
				return err
			}
		}
		return nil
	case "show":
		runner, err := app.Store.GetBrowserHandoffRunner(ctx, command.ID)
		if err != nil {
			return err
		}
		view := newBrowserSessionRunnerView(runner)
		if command.JSON {
			return commands.WriteJSON(stdout, browserSessionRunnerEnvelope{Runner: view})
		}
		_, err = fmt.Fprintf(stdout, "browser_session_runner=%d login_request=%d session=%d status=%s\n", view.ID, view.LoginRequestID, view.SessionID, view.Status)
		return err
	case "start":
		runner, err := startBrowserSessionRunner(ctx, app, command.ID)
		if err != nil {
			return err
		}
		view := newBrowserSessionRunnerView(runner)
		if command.JSON {
			return commands.WriteJSON(stdout, browserSessionRunnerEnvelope{Runner: view})
		}
		_, err = fmt.Fprintf(stdout, "browser_session_runner=%d login_request=%d session=%d status=%s\n", view.ID, view.LoginRequestID, view.SessionID, view.Status)
		return err
	case "status":
		runner, err := app.Store.UpdateBrowserHandoffRunnerStatus(ctx, sqlite.UpdateBrowserHandoffRunnerStatusParams{
			ID:     command.ID,
			Status: sqlite.BrowserHandoffRunnerStatus(command.Status),
			Actor:  "operator",
			Reason: "operator updated browser handoff runner metadata",
		})
		if err != nil {
			return err
		}
		view := newBrowserSessionRunnerView(runner)
		if command.JSON {
			return commands.WriteJSON(stdout, browserSessionRunnerEnvelope{Runner: view})
		}
		_, err = fmt.Fprintf(stdout, "browser_session_runner=%d login_request=%d session=%d status=%s\n", view.ID, view.LoginRequestID, view.SessionID, view.Status)
		return err
	case "cancel":
		runner, err := app.Store.CancelBrowserHandoffRunner(ctx, sqlite.CancelBrowserHandoffRunnerParams{
			ID:     command.ID,
			Actor:  "operator",
			Reason: "operator cancelled browser handoff runner metadata",
		})
		if err != nil {
			return err
		}
		view := newBrowserSessionRunnerView(runner)
		if command.JSON {
			return commands.WriteJSON(stdout, browserSessionRunnerEnvelope{Runner: view})
		}
		_, err = fmt.Fprintf(stdout, "browser_session_runner=%d login_request=%d session=%d status=%s\n", view.ID, view.LoginRequestID, view.SessionID, view.Status)
		return err
	default:
		return fmt.Errorf(commands.BrowserUsage)
	}
}

func startBrowserSessionRunner(ctx context.Context, app bootstrap.App, runnerID int64) (sqlite.BrowserHandoffRunner, error) {
	runner, err := app.Store.GetBrowserHandoffRunner(ctx, runnerID)
	if err != nil {
		return sqlite.BrowserHandoffRunner{}, err
	}
	if runner.Status != sqlite.BrowserHandoffRunnerStatusRequested {
		return sqlite.BrowserHandoffRunner{}, fmt.Errorf("browser handoff runner status %q cannot start", runner.Status)
	}
	handoff, err := app.Store.GetBrowserSessionLoginHandoff(ctx, runner.HandoffID)
	if err != nil {
		return sqlite.BrowserHandoffRunner{}, err
	}
	if handoff.LoginRequest.ID != runner.LoginRequestID || handoff.Session.ID != runner.SessionID {
		return sqlite.BrowserHandoffRunner{}, fmt.Errorf("browser handoff runner link mismatch")
	}
	response, err := browserhandoff.StubRunner{}.Start(ctx, browserhandoff.StartRequest{
		SessionID:      handoff.Session.ID,
		LoginRequestID: handoff.LoginRequest.ID,
		HandoffID:      handoff.HandoffID,
		ProfilePath:    handoff.Session.ProfilePath,
		AllowedDomain:  handoff.Session.Domain,
		TimeoutSeconds: browserSessionRunnerTimeoutSeconds(handoff.LoginRequest.ExpiresAt),
		BindAddr:       browserSessionStringPtrValue(runner.BindAddr),
		PrivateBaseURL: browserSessionStringPtrValue(runner.PrivateBaseURL),
		PublicBaseURL:  browserSessionStringPtrValue(runner.PublicBaseURL),
	})
	if err != nil {
		return sqlite.BrowserHandoffRunner{}, err
	}
	switch response.Status {
	case browserhandoff.StatusNotImplemented:
		errorCode := response.ErrorCode
		if strings.TrimSpace(errorCode) == "" {
			errorCode = "not_implemented"
		}
		errorMessage := response.ErrorMessage
		if strings.TrimSpace(errorMessage) == "" {
			errorMessage = "browser handoff runner process boundary is not implemented"
		}
		return app.Store.UpdateBrowserHandoffRunnerStatus(ctx, sqlite.UpdateBrowserHandoffRunnerStatusParams{
			ID:           runner.ID,
			Status:       sqlite.BrowserHandoffRunnerStatusFailed,
			ErrorCode:    &errorCode,
			ErrorMessage: &errorMessage,
			Actor:        "operator",
			Reason:       "browser handoff StubRunner returned not_implemented",
		})
	default:
		return sqlite.BrowserHandoffRunner{}, fmt.Errorf("unsupported browser handoff runner start status %q", response.Status)
	}
}

func browserSessionRunnerTimeoutSeconds(expiresAt time.Time) int {
	remaining := time.Until(expiresAt)
	if remaining <= 0 {
		return 0
	}
	seconds := int(remaining.Seconds())
	if seconds <= 0 {
		return 1
	}
	return seconds
}

func prepareBrowserSessionProfile(ctx context.Context, app bootstrap.App, session sqlite.BrowserSession) (browserSessionProfileView, error) {
	if session.Status == sqlite.BrowserSessionStatusRevoked {
		return browserSessionProfileView{}, fmt.Errorf("revoked browser session cannot prepare profile")
	}
	profilePath, absPath, err := resolveBrowserSessionProfilePath(app.RuntimeRoot, session.ProfilePath)
	if err != nil {
		return browserSessionProfileView{}, err
	}
	created := false
	info, err := os.Stat(absPath)
	switch {
	case err == nil:
		if !info.IsDir() {
			return browserSessionProfileView{}, fmt.Errorf("browser session profile path exists and is not a directory")
		}
	case os.IsNotExist(err):
		if err := os.MkdirAll(absPath, 0o700); err != nil {
			return browserSessionProfileView{}, err
		}
		created = true
	default:
		return browserSessionProfileView{}, err
	}
	if err := os.Chmod(absPath, 0o700); err != nil {
		return browserSessionProfileView{}, err
	}
	if err := app.Store.RecordBrowserSessionProfilePrepared(ctx, sqlite.RecordBrowserSessionProfilePreparedParams{
		SessionID:   session.ID,
		ProfilePath: profilePath,
		Created:     created,
		Actor:       "operator",
	}); err != nil {
		return browserSessionProfileView{}, err
	}
	return browserSessionProfileView{
		SessionID:         session.ID,
		ProfilePath:       profilePath,
		ProfilePathExists: browserSessionProfilePathExists(app.RuntimeRoot, profilePath),
		Created:           created,
	}, nil
}

func resolveBrowserSessionProfilePath(runtimeRoot string, profilePath string) (string, string, error) {
	profilePath, err := sqlite.ValidateBrowserSessionProfilePath(profilePath)
	if err != nil {
		return "", "", err
	}
	absRuntimeRoot, err := filepath.Abs(runtimeRoot)
	if err != nil {
		return "", "", err
	}
	absPath := filepath.Join(absRuntimeRoot, filepath.FromSlash(profilePath))
	rel, err := filepath.Rel(absRuntimeRoot, absPath)
	if err != nil {
		return "", "", err
	}
	if rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." || filepath.IsAbs(rel) {
		return "", "", fmt.Errorf("browser session profile path must stay under ODIN_ROOT")
	}
	return profilePath, absPath, nil
}

func browserSessionPermissionTier(value string) sqlite.BrowserSessionPermissionTier {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "authenticated_read":
		return sqlite.BrowserSessionPermissionTierAuthenticatedReadOnly
	default:
		return sqlite.BrowserSessionPermissionTier(strings.ToLower(strings.TrimSpace(value)))
	}
}

func newBrowserSessionView(runtimeRoot string, session sqlite.BrowserSession) browserSessionView {
	return browserSessionView{
		ID:                   session.ID,
		Name:                 session.Name,
		Domain:               session.Domain,
		AccountHint:          session.AccountHint,
		PermissionTier:       string(session.PermissionTier),
		Status:               string(session.Status),
		ProfileStoragePolicy: string(session.ProfileStoragePolicy),
		ProfilePath:          session.ProfilePath,
		ProfilePathExists:    browserSessionProfilePathExists(runtimeRoot, session.ProfilePath),
		CreatedAt:            formatBrowserSessionTime(session.CreatedAt),
		UpdatedAt:            formatBrowserSessionTime(session.UpdatedAt),
		LastVerifiedAt:       formatBrowserSessionOptionalTime(session.LastVerifiedAt),
		ExpiresAt:            formatBrowserSessionOptionalTime(session.ExpiresAt),
		RevokedAt:            formatBrowserSessionOptionalTime(session.RevokedAt),
	}
}

func browserSessionProfilePathExists(runtimeRoot string, profilePath string) bool {
	profilePath = strings.TrimSpace(profilePath)
	if profilePath == "" || filepath.IsAbs(profilePath) {
		return false
	}
	_, err := os.Stat(filepath.Join(runtimeRoot, filepath.FromSlash(profilePath)))
	return err == nil
}

func newBrowserSessionLoginRequestView(request sqlite.BrowserSessionLoginRequest) browserSessionLoginRequestView {
	return browserSessionLoginRequestView{
		ID:          request.ID,
		SessionID:   request.SessionID,
		Status:      string(request.Status),
		HandoffID:   request.HandoffID,
		HandoffURL:  cloneBrowserSessionStringPtr(request.HandoffURL),
		ExpiresAt:   formatBrowserSessionTime(request.ExpiresAt),
		CompletedAt: formatBrowserSessionOptionalTime(request.CompletedAt),
		CreatedAt:   formatBrowserSessionTime(request.CreatedAt),
		UpdatedAt:   formatBrowserSessionTime(request.UpdatedAt),
	}
}

func newBrowserSessionHandoffView(handoff sqlite.BrowserSessionLoginHandoff) browserSessionHandoffView {
	return browserSessionHandoffView{
		HandoffID:      handoff.HandoffID,
		LoginRequestID: handoff.LoginRequest.ID,
		SessionID:      handoff.Session.ID,
		SessionName:    handoff.Session.Name,
		Domain:         handoff.Session.Domain,
		AccountHint:    handoff.Session.AccountHint,
		ExpiresAt:      formatBrowserSessionTime(handoff.LoginRequest.ExpiresAt),
		Status:         string(handoff.LoginRequest.Status),
		AllowedActions: "manual_login_only",
	}
}

func newBrowserSessionRunnerView(runner sqlite.BrowserHandoffRunner) browserSessionRunnerView {
	return browserSessionRunnerView{
		ID:             runner.ID,
		SessionID:      runner.SessionID,
		LoginRequestID: runner.LoginRequestID,
		HandoffID:      runner.HandoffID,
		Status:         string(runner.Status),
		ViewerURL:      cloneBrowserSessionStringPtr(runner.ViewerURL),
		RunnerID:       cloneBrowserSessionStringPtr(runner.RunnerID),
		ProcessID:      cloneBrowserSessionInt64Ptr(runner.ProcessID),
		BindAddr:       cloneBrowserSessionStringPtr(runner.BindAddr),
		PrivateBaseURL: cloneBrowserSessionStringPtr(runner.PrivateBaseURL),
		PublicBaseURL:  cloneBrowserSessionStringPtr(runner.PublicBaseURL),
		ExpiresAt:      formatBrowserSessionTime(runner.ExpiresAt),
		StartedAt:      formatBrowserSessionOptionalTime(runner.StartedAt),
		CompletedAt:    formatBrowserSessionOptionalTime(runner.CompletedAt),
		CancelledAt:    formatBrowserSessionOptionalTime(runner.CancelledAt),
		CreatedAt:      formatBrowserSessionTime(runner.CreatedAt),
		UpdatedAt:      formatBrowserSessionTime(runner.UpdatedAt),
		ErrorCode:      cloneBrowserSessionStringPtr(runner.ErrorCode),
		ErrorMessage:   cloneBrowserSessionStringPtr(runner.ErrorMessage),
	}
}

func cloneBrowserSessionStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	ptr := new(string)
	*ptr = *value
	return ptr
}

func browserSessionStringPtrValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func cloneBrowserSessionInt64Ptr(value *int64) *int64 {
	if value == nil {
		return nil
	}
	ptr := new(int64)
	*ptr = *value
	return ptr
}

func formatBrowserSessionOptionalTime(value *time.Time) string {
	if value == nil {
		return ""
	}
	return formatBrowserSessionTime(*value)
}

func formatBrowserSessionTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}
