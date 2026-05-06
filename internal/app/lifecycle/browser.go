package lifecycle

import (
	"context"
	"fmt"
	"io"
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

type browserSessionView struct {
	ID             int64  `json:"id"`
	Name           string `json:"name"`
	Domain         string `json:"domain"`
	AccountHint    string `json:"account_hint,omitempty"`
	PermissionTier string `json:"permission_tier"`
	Status         string `json:"status"`
	ProfilePath    string `json:"profile_path"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
	LastVerifiedAt string `json:"last_verified_at,omitempty"`
	ExpiresAt      string `json:"expires_at,omitempty"`
	RevokedAt      string `json:"revoked_at,omitempty"`
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
			ProfilePath:    browserSessionProfilePath(command),
		})
		if err != nil {
			return err
		}
		view := newBrowserSessionView(session)
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
			views = append(views, newBrowserSessionView(session))
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
		view := newBrowserSessionView(session)
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
		view := newBrowserSessionView(session)
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
		view := newBrowserSessionView(session)
		if command.JSON {
			return commands.WriteJSON(stdout, browserSessionEnvelope{Session: view})
		}
		_, err = fmt.Fprintf(stdout, "browser_session=%d status=%s name=%q domain=%s\n", view.ID, view.Status, view.Name, view.Domain)
		return err
	default:
		return fmt.Errorf(commands.BrowserUsage)
	}
}

func browserSessionPermissionTier(value string) sqlite.BrowserSessionPermissionTier {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "authenticated_read":
		return sqlite.BrowserSessionPermissionTierAuthenticatedReadOnly
	default:
		return sqlite.BrowserSessionPermissionTier(strings.ToLower(strings.TrimSpace(value)))
	}
}

func browserSessionProfilePath(command commands.BrowserCommand) string {
	if strings.TrimSpace(command.ProfilePath) != "" {
		return command.ProfilePath
	}
	return "browser-sessions/profiles/" + browserSessionPathSegment(command.SessionName)
}

func browserSessionPathSegment(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var builder strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		case r == '-' || r == '_' || r == '.':
			builder.WriteRune(r)
		default:
			builder.WriteRune('-')
		}
	}
	segment := strings.Trim(builder.String(), "-._")
	if segment == "" {
		return "session"
	}
	return segment
}

func newBrowserSessionView(session sqlite.BrowserSession) browserSessionView {
	return browserSessionView{
		ID:             session.ID,
		Name:           session.Name,
		Domain:         session.Domain,
		AccountHint:    session.AccountHint,
		PermissionTier: string(session.PermissionTier),
		Status:         string(session.Status),
		ProfilePath:    session.ProfilePath,
		CreatedAt:      formatBrowserSessionTime(session.CreatedAt),
		UpdatedAt:      formatBrowserSessionTime(session.UpdatedAt),
		LastVerifiedAt: formatBrowserSessionOptionalTime(session.LastVerifiedAt),
		ExpiresAt:      formatBrowserSessionOptionalTime(session.ExpiresAt),
		RevokedAt:      formatBrowserSessionOptionalTime(session.RevokedAt),
	}
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
