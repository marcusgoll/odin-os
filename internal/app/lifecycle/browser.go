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
	default:
		return fmt.Errorf(commands.BrowserUsage)
	}
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

func cloneBrowserSessionStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	ptr := new(string)
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
