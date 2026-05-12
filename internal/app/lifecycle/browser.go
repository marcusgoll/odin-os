package lifecycle

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"odin-os/internal/app/bootstrap"
	commands "odin-os/internal/cli/commands"
	browserexecutor "odin-os/internal/executors/browser"
	"odin-os/internal/runtime/browserprofileartifacts"
	"odin-os/internal/runtime/browserprofilekeys"
	"odin-os/internal/runtime/browserprofilematerialize"
	"odin-os/internal/runtime/recovery"
	runsvc "odin-os/internal/runtime/runs"
	"odin-os/internal/store/sqlite"
)

type browserRunView struct {
	Status                 string                               `json:"status"`
	GoalID                 int64                                `json:"goal_id,omitempty"`
	TaskID                 int64                                `json:"task_id,omitempty"`
	RunID                  int64                                `json:"run_id,omitempty"`
	BrowserSessionID       int64                                `json:"browser_session_id,omitempty"`
	EvidenceID             int64                                `json:"evidence_id,omitempty"`
	EvidenceType           string                               `json:"evidence_type"`
	ApprovalRequired       bool                                 `json:"approval_required,omitempty"`
	ApprovalID             int64                                `json:"approval_id,omitempty"`
	RiskClass              string                               `json:"risk_class,omitempty"`
	AdapterStatus          string                               `json:"adapter_status,omitempty"`
	AdapterKind            string                               `json:"adapter_kind,omitempty"`
	StartURLs              []string                             `json:"start_urls"`
	AllowedDomains         []string                             `json:"allowed_domains"`
	MaxPages               int                                  `json:"max_pages"`
	MaxDurationSeconds     int                                  `json:"max_duration_seconds"`
	VisitedURLs            []string                             `json:"visited_urls,omitempty"`
	PageResults            []browserexecutor.PageResult         `json:"page_results,omitempty"`
	ExtractedTextSummary   string                               `json:"extracted_text_summary,omitempty"`
	Screenshots            []string                             `json:"screenshots,omitempty"`
	ScreenshotMetadata     []browserexecutor.ScreenshotMetadata `json:"screenshot_metadata,omitempty"`
	SelectedLinks          []browserexecutor.SelectedLink       `json:"selected_links,omitempty"`
	DownloadedFiles        []browserexecutor.DownloadedFile     `json:"downloaded_files,omitempty"`
	FormStateSummary       []browserexecutor.FormStateSummary   `json:"form_state_summary,omitempty"`
	BrowserNotes           []string                             `json:"browser_notes,omitempty"`
	Confidence             string                               `json:"confidence,omitempty"`
	Limitations            []string                             `json:"limitations,omitempty"`
	RecoveryRecommendation string                               `json:"recovery_recommendation,omitempty"`
	ActionLog              []string                             `json:"action_log,omitempty"`
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

type browserSessionProfileEnvelope struct {
	Profile browserSessionProfileView `json:"profile"`
}

type browserSessionProfileRetentionEnvelope struct {
	Retention browserSessionProfileRetentionView `json:"retention"`
}

type browserSessionProfileArtifactEnvelope struct {
	Artifact browserSessionProfileArtifactView `json:"artifact"`
}

type browserSessionProfileArtifactListView struct {
	Artifacts []browserSessionProfileArtifactView `json:"artifacts"`
}

type browserSessionProfileMaterializationEnvelope struct {
	Materialization browserSessionProfileMaterializationView `json:"materialization"`
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

type browserSessionProfileRetentionView struct {
	DryRun    bool                                            `json:"dry_run"`
	Apply     bool                                            `json:"apply"`
	Eligible  int                                             `json:"eligible"`
	Cleaned   int                                             `json:"cleaned"`
	Failed    int                                             `json:"failed"`
	Skipped   int                                             `json:"skipped"`
	Artifacts []browserprofileartifacts.RetentionArtifactItem `json:"artifacts"`
}

type browserSessionProfileArtifactView struct {
	ID               int64  `json:"id"`
	SessionID        int64  `json:"session_id"`
	Status           string `json:"status"`
	ProfilePath      string `json:"profile_path"`
	ArtifactPath     string `json:"artifact_path"`
	EncryptionKeyRef string `json:"encryption_key_ref"`
	CreatedAt        string `json:"created_at"`
	UpdatedAt        string `json:"updated_at"`
	ExpiresAt        string `json:"expires_at,omitempty"`
	RevokedAt        string `json:"revoked_at,omitempty"`
	CleanedAt        string `json:"cleaned_at,omitempty"`
	ErrorCode        string `json:"error_code,omitempty"`
	ErrorMessage     string `json:"error_message,omitempty"`
}

type browserSessionProfileMaterializationView struct {
	ArtifactID           int64  `json:"artifact_id"`
	SessionID            int64  `json:"session_id"`
	ArtifactPath         string `json:"artifact_path"`
	MaterializationPath  string `json:"materialization_path"`
	MaterializedFilePath string `json:"materialized_file_path,omitempty"`
	ReadOnly             bool   `json:"read_only,omitempty"`
	Removed              bool   `json:"removed,omitempty"`
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

	objective := strings.TrimSpace(command.Objective)
	if command.TaskID > 0 {
		task, err := app.Store.GetTask(ctx, command.TaskID)
		if err != nil {
			return err
		}
		if objective == "" {
			objective = task.Title
		}
		run, err := (runsvc.Service{Store: app.Store}).Start(ctx, task, browserexecutor.EvidenceType)
		if err != nil {
			return err
		}
		result, runErr := browserexecutor.Service{Store: app.Store}.Run(ctx, browserexecutor.ReadOnlyTask{
			GoalID:             command.GoalID,
			TaskID:             command.TaskID,
			RunID:              run.ID,
			BrowserSessionID:   command.SessionID,
			WorkerMode:         command.WorkerMode,
			Objective:          objective,
			AllowedDomains:     command.AllowedDomains,
			StartURLs:          command.URLs,
			MaxPages:           command.MaxPages,
			MaxDurationSeconds: command.MaxDurationSeconds,
			EvidenceRequired:   command.EvidenceRequired,
			Actions:            command.Actions,
		})
		if runErr != nil {
			if finishErr := finishBrowserRunFailed(ctx, app.Store, run.ID, command.TaskID, result, runErr); finishErr != nil {
				return finishErr
			}
			return runErr
		}
		if result.Status == "approval_required" {
			if err := finishBrowserRunApprovalRequired(ctx, app.Store, run.ID, result); err != nil {
				return err
			}
			return writeBrowserRunView(stdout, command.JSON, result)
		}
		if err := finishBrowserRunFromWork(ctx, app.Store, run.ID, command.TaskID, result); err != nil {
			return err
		}
		return writeBrowserRunView(stdout, command.JSON, result)
	}

	goal, err := app.Store.GetGoal(ctx, command.GoalID)
	if err != nil {
		return err
	}
	if objective == "" {
		objective = goal.Title
	}
	result, err := browserexecutor.Service{Store: app.Store}.Run(ctx, browserexecutor.ReadOnlyTask{
		GoalID:             command.GoalID,
		BrowserSessionID:   command.SessionID,
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
	return writeBrowserRunView(stdout, command.JSON, result)
}

func writeBrowserRunView(stdout io.Writer, jsonOutput bool, result browserexecutor.Result) error {
	view := browserRunView{
		Status:                 result.Status,
		GoalID:                 result.GoalID,
		TaskID:                 result.TaskID,
		RunID:                  result.RunID,
		BrowserSessionID:       result.BrowserSessionID,
		EvidenceID:             result.EvidenceID,
		EvidenceType:           result.EvidenceType,
		ApprovalRequired:       result.ApprovalRequired,
		ApprovalID:             result.ApprovalID,
		RiskClass:              result.RiskClass,
		AdapterStatus:          result.AdapterStatus,
		AdapterKind:            result.AdapterKind,
		StartURLs:              result.StartURLs,
		AllowedDomains:         result.AllowedDomains,
		MaxPages:               result.MaxPages,
		MaxDurationSeconds:     result.MaxDurationSeconds,
		VisitedURLs:            result.VisitedURLs,
		PageResults:            result.PageResults,
		ExtractedTextSummary:   result.ExtractedTextSummary,
		Screenshots:            result.Screenshots,
		ScreenshotMetadata:     result.ScreenshotMetadata,
		SelectedLinks:          result.SelectedLinks,
		DownloadedFiles:        result.DownloadedFiles,
		FormStateSummary:       result.FormStateSummary,
		BrowserNotes:           result.BrowserNotes,
		Confidence:             result.Confidence,
		Limitations:            result.Limitations,
		RecoveryRecommendation: result.RecoveryRecommendation,
		ActionLog:              result.ActionLog,
	}
	if jsonOutput {
		return commands.WriteJSON(stdout, view)
	}
	_, err := fmt.Fprintf(stdout, "browser goal=%d task=%d run=%d status=%s evidence=%d type=%s\n", view.GoalID, view.TaskID, view.RunID, view.Status, view.EvidenceID, view.EvidenceType)
	return err
}

func finishBrowserRunFromWork(ctx context.Context, store *sqlite.Store, runID int64, taskID int64, result browserexecutor.Result) error {
	artifactsJSON := browserResultArtifactsJSON(result)
	runStatus := "completed"
	taskStatus := "blocked"
	blockedReason := "browser_evidence_review"
	lastError := ""
	if result.AdapterStatus == "failed" || result.AdapterStatus == "timeout" {
		runStatus = "failed"
		taskStatus = "failed"
		blockedReason = ""
		lastError = "browser evidence capture failed: " + firstNonBlank(result.ErrorMessage, result.ExtractedTextSummary, "unknown browser error")
	}
	if _, err := store.FinishRun(ctx, sqlite.FinishRunParams{
		RunID:          runID,
		Status:         runStatus,
		Summary:        firstNonBlank(result.ExtractedTextSummary, result.AdapterStatus, result.Status),
		TerminalReason: runStatus,
		ArtifactsJSON:  artifactsJSON,
	}); err != nil {
		return err
	}
	task, err := store.GetTask(ctx, taskID)
	if err != nil {
		return err
	}
	updated, err := store.UpdateTaskQueueState(ctx, sqlite.UpdateTaskQueueStateParams{
		TaskID:         task.ID,
		Status:         taskStatus,
		NextEligibleAt: task.NextEligibleAt,
		Priority:       task.Priority,
		LastError:      lastError,
		RetryCount:     task.RetryCount,
		MaxAttempts:    task.MaxAttempts,
		BlockedReason:  blockedReason,
	})
	if err != nil {
		return err
	}
	if taskStatus == "failed" {
		guidance := recovery.RetryGuidanceForTask(recovery.RetryGuidanceInput{
			RetryCount:  updated.RetryCount,
			MaxAttempts: updated.MaxAttempts,
			WorkKind:    "browser_evidence",
			RequestedBy: updated.RequestedBy,
		})
		return store.RecordTaskRecoveryRecommendation(ctx, sqlite.RecordTaskRecoveryRecommendationParams{
			Task:                   updated,
			Decision:               guidance.Decision,
			RetryEligible:          guidance.RetryEligible,
			RecoveryRecommendation: guidance.RecoveryRecommendation,
			Source:                 guidance.Source,
		})
	}
	return nil
}

func finishBrowserRunApprovalRequired(ctx context.Context, store *sqlite.Store, runID int64, result browserexecutor.Result) error {
	_, err := store.FinishRun(ctx, sqlite.FinishRunParams{
		RunID:          runID,
		Status:         "interrupted",
		Summary:        "browser request requires approval before external mutation",
		TerminalReason: "approval_required",
		ArtifactsJSON:  browserResultArtifactsJSON(result),
	})
	return err
}

func finishBrowserRunFailed(ctx context.Context, store *sqlite.Store, runID int64, taskID int64, result browserexecutor.Result, runErr error) error {
	if result.ErrorMessage == "" {
		result.ErrorMessage = runErr.Error()
	}
	result.AdapterStatus = firstNonBlank(result.AdapterStatus, "failed")
	return finishBrowserRunFromWork(ctx, store, runID, taskID, result)
}

func browserResultArtifactsJSON(result browserexecutor.Result) string {
	if result.WorkArtifact == nil {
		return "[]"
	}
	raw, err := json.Marshal([]browserexecutor.EvidenceArtifact{*result.WorkArtifact})
	if err != nil {
		return "[]"
	}
	return string(raw)
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
	case "profile":
		return runBrowserSessionProfile(ctx, app, command, stdout)
	default:
		return fmt.Errorf(commands.BrowserUsage)
	}
}

func runBrowserSessionProfile(ctx context.Context, app bootstrap.App, command commands.BrowserCommand, stdout io.Writer) error {
	switch command.ProfileAction {
	case "retention":
		return runBrowserSessionProfileRetention(ctx, app, command, stdout)
	case "artifact":
		return runBrowserSessionProfileArtifact(ctx, app, command, stdout)
	default:
		return fmt.Errorf(commands.BrowserUsage)
	}
}

func runBrowserSessionProfileRetention(ctx context.Context, app bootstrap.App, command commands.BrowserCommand, stdout io.Writer) error {
	if command.RetentionAction != "cleanup" {
		return fmt.Errorf(commands.BrowserUsage)
	}
	result, err := browserprofileartifacts.Retain(ctx, browserprofileartifacts.RetentionParams{
		Store:     app.Store,
		ODINRoot:  app.RuntimeRoot,
		Now:       time.Now().UTC(),
		SessionID: command.SessionID,
		Apply:     command.Apply,
	})
	if err != nil {
		return err
	}
	view := browserSessionProfileRetentionView{
		DryRun:    result.DryRun,
		Apply:     command.Apply,
		Eligible:  result.Eligible,
		Cleaned:   result.Cleaned,
		Failed:    result.Failed,
		Skipped:   result.Skipped,
		Artifacts: result.Artifacts,
	}
	if command.JSON {
		return commands.WriteJSON(stdout, browserSessionProfileRetentionEnvelope{Retention: view})
	}
	_, err = fmt.Fprintf(stdout, "browser_session_profile_retention dry_run=%t apply=%t eligible=%d cleaned=%d failed=%d skipped=%d\n", view.DryRun, view.Apply, view.Eligible, view.Cleaned, view.Failed, view.Skipped)
	return err
}

func runBrowserSessionProfileArtifact(ctx context.Context, app bootstrap.App, command commands.BrowserCommand, stdout io.Writer) error {
	switch command.ArtifactAction {
	case "create-fixture":
		session, err := app.Store.GetBrowserSession(ctx, command.SessionID)
		if err != nil {
			return err
		}
		artifactPath, err := browserSessionFixtureArtifactPath(command.ArtifactName)
		if err != nil {
			return err
		}
		plaintext, err := readBrowserSessionFixturePlaintext(app.RuntimeRoot, command.PlaintextFile)
		if err != nil {
			return err
		}
		artifact, err := browserprofileartifacts.Write(ctx, browserprofileartifacts.Params{
			Store:        app.Store,
			ODINRoot:     app.RuntimeRoot,
			SessionID:    session.ID,
			ProfilePath:  session.ProfilePath,
			Plaintext:    plaintext,
			ArtifactPath: artifactPath,
			KeyProvider:  browserprofilekeys.LoadFromEnv,
		})
		if err != nil {
			return err
		}
		view := newBrowserSessionProfileArtifactView(artifact)
		if command.JSON {
			return commands.WriteJSON(stdout, browserSessionProfileArtifactEnvelope{Artifact: view})
		}
		_, err = fmt.Fprintf(stdout, "browser_session_profile_artifact=%d session=%d status=%s path=%s key_ref=%s\n", view.ID, view.SessionID, view.Status, view.ArtifactPath, view.EncryptionKeyRef)
		return err
	case "list":
		artifacts, err := app.Store.ListBrowserEncryptedProfileArtifacts(ctx, sqlite.ListBrowserEncryptedProfileArtifactsParams{SessionID: command.SessionID})
		if err != nil {
			return err
		}
		views := make([]browserSessionProfileArtifactView, 0, len(artifacts))
		for _, artifact := range artifacts {
			views = append(views, newBrowserSessionProfileArtifactView(artifact))
		}
		if command.JSON {
			return commands.WriteJSON(stdout, browserSessionProfileArtifactListView{Artifacts: views})
		}
		if len(views) == 0 {
			_, err := fmt.Fprintln(stdout, "no browser session profile artifacts")
			return err
		}
		for _, view := range views {
			if _, err := fmt.Fprintf(stdout, "browser_session_profile_artifact=%d session=%d status=%s path=%s key_ref=%s\n", view.ID, view.SessionID, view.Status, view.ArtifactPath, view.EncryptionKeyRef); err != nil {
				return err
			}
		}
		return nil
	case "show":
		artifact, err := app.Store.GetBrowserEncryptedProfileArtifact(ctx, command.ID)
		if err != nil {
			return err
		}
		view := newBrowserSessionProfileArtifactView(artifact)
		if command.JSON {
			return commands.WriteJSON(stdout, browserSessionProfileArtifactEnvelope{Artifact: view})
		}
		_, err = fmt.Fprintf(stdout, "browser_session_profile_artifact=%d session=%d status=%s path=%s key_ref=%s\n", view.ID, view.SessionID, view.Status, view.ArtifactPath, view.EncryptionKeyRef)
		return err
	case "revoke":
		artifact, err := app.Store.MarkBrowserEncryptedProfileArtifactRevoked(ctx, sqlite.MarkBrowserEncryptedProfileArtifactRevokedParams{
			ID:     command.ID,
			Actor:  "operator",
			Reason: "operator revoked encrypted profile artifact metadata",
		})
		if err != nil {
			return err
		}
		view := newBrowserSessionProfileArtifactView(artifact)
		if command.JSON {
			return commands.WriteJSON(stdout, browserSessionProfileArtifactEnvelope{Artifact: view})
		}
		_, err = fmt.Fprintf(stdout, "browser_session_profile_artifact=%d session=%d status=%s path=%s key_ref=%s\n", view.ID, view.SessionID, view.Status, view.ArtifactPath, view.EncryptionKeyRef)
		return err
	case "materialize":
		artifact, err := app.Store.GetBrowserEncryptedProfileArtifact(ctx, command.ID)
		if err != nil {
			return err
		}
		result, err := browserprofilematerialize.Materialize(ctx, browserprofilematerialize.Params{
			Store:       app.Store,
			ODINRoot:    app.RuntimeRoot,
			Artifact:    artifact,
			TargetDir:   command.TargetDir,
			KeyProvider: browserprofilekeys.LoadFromEnv,
			Actor:       "operator",
			Reason:      "operator materialized encrypted profile artifact read-only",
		})
		if err != nil {
			return err
		}
		view := newBrowserSessionProfileMaterializationView(result)
		if command.JSON {
			return commands.WriteJSON(stdout, browserSessionProfileMaterializationEnvelope{Materialization: view})
		}
		_, err = fmt.Fprintf(stdout, "browser_session_profile_materialization artifact=%d session=%d path=%s file=%s read_only=%t\n", view.ArtifactID, view.SessionID, view.MaterializationPath, view.MaterializedFilePath, view.ReadOnly)
		return err
	case "cleanup-materialization":
		artifact, err := app.Store.GetBrowserEncryptedProfileArtifact(ctx, command.ID)
		if err != nil {
			return err
		}
		result, err := browserprofilematerialize.Cleanup(ctx, browserprofilematerialize.CleanupParams{
			Store:     app.Store,
			ODINRoot:  app.RuntimeRoot,
			Artifact:  artifact,
			TargetDir: command.TargetDir,
			Actor:     "operator",
			Reason:    "operator cleaned encrypted profile materialization",
		})
		if err != nil {
			return err
		}
		view := newBrowserSessionProfileMaterializationCleanupView(result)
		if command.JSON {
			return commands.WriteJSON(stdout, browserSessionProfileMaterializationEnvelope{Materialization: view})
		}
		_, err = fmt.Fprintf(stdout, "browser_session_profile_materialization artifact=%d session=%d path=%s removed=%t\n", view.ArtifactID, view.SessionID, view.MaterializationPath, view.Removed)
		return err
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

func newBrowserSessionProfileArtifactView(artifact sqlite.BrowserEncryptedProfileArtifact) browserSessionProfileArtifactView {
	return browserSessionProfileArtifactView{
		ID:               artifact.ID,
		SessionID:        artifact.SessionID,
		Status:           string(artifact.Status),
		ProfilePath:      artifact.ProfilePath,
		ArtifactPath:     artifact.EncryptedArtifactPath,
		EncryptionKeyRef: artifact.EncryptionKeyRef,
		CreatedAt:        formatBrowserSessionTime(artifact.CreatedAt),
		UpdatedAt:        formatBrowserSessionTime(artifact.UpdatedAt),
		ExpiresAt:        formatBrowserSessionOptionalTime(artifact.ExpiresAt),
		RevokedAt:        formatBrowserSessionOptionalTime(artifact.RevokedAt),
		CleanedAt:        formatBrowserSessionOptionalTime(artifact.CleanedAt),
		ErrorCode:        browserSessionStringPtrValue(artifact.ErrorCode),
		ErrorMessage:     browserSessionStringPtrValue(artifact.ErrorMessage),
	}
}

func newBrowserSessionProfileMaterializationView(result browserprofilematerialize.Result) browserSessionProfileMaterializationView {
	return browserSessionProfileMaterializationView{
		ArtifactID:           result.ArtifactID,
		SessionID:            result.SessionID,
		ArtifactPath:         result.ArtifactPath,
		MaterializationPath:  result.MaterializationPath,
		MaterializedFilePath: result.MaterializedFilePath,
		ReadOnly:             result.ReadOnly,
	}
}

func newBrowserSessionProfileMaterializationCleanupView(result browserprofilematerialize.CleanupResult) browserSessionProfileMaterializationView {
	return browserSessionProfileMaterializationView{
		ArtifactID:          result.ArtifactID,
		SessionID:           result.SessionID,
		ArtifactPath:        result.ArtifactPath,
		MaterializationPath: result.MaterializationPath,
		Removed:             result.Removed,
	}
}

func browserSessionFixtureArtifactPath(name string) (string, error) {
	name = strings.TrimSpace(name)
	if strings.HasSuffix(name, ".enc") {
		name = strings.TrimSuffix(name, ".enc")
	}
	if name == "" || name == "." || name == ".." {
		return "", fmt.Errorf("browser profile fixture artifact name is required")
	}
	if strings.ContainsAny(name, `/\`) {
		return "", fmt.Errorf("browser profile fixture artifact name must be one safe component")
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_' || r == '.':
		default:
			return "", fmt.Errorf("browser profile fixture artifact name contains unsafe component %q", name)
		}
	}
	if err := rejectBrowserSessionFixtureMetadata(name); err != nil {
		return "", err
	}
	return filepath.ToSlash(filepath.Join("browser-sessions", "encrypted-profiles", name+".enc")), nil
}

func readBrowserSessionFixturePlaintext(runtimeRoot string, plaintextFile string) ([]byte, error) {
	plaintextFile = strings.TrimSpace(plaintextFile)
	if plaintextFile == "" {
		return nil, fmt.Errorf("--plaintext-file is required")
	}
	absPath, err := filepath.Abs(plaintextFile)
	if err != nil {
		return nil, fmt.Errorf("browser profile fixture plaintext path: %w", err)
	}
	if err := rejectBrowserSessionFixtureRuntimePath(runtimeRoot, absPath); err != nil {
		return nil, err
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return nil, fmt.Errorf("read plaintext fixture: %w", err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("read plaintext fixture: path is a directory")
	}
	plaintext, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("read plaintext fixture: %w", err)
	}
	return plaintext, nil
}

func rejectBrowserSessionFixtureRuntimePath(runtimeRoot string, absPath string) error {
	rootAbs, err := filepath.Abs(runtimeRoot)
	if err != nil {
		return fmt.Errorf("ODIN_ROOT absolute path: %w", err)
	}
	browserRoot := filepath.Join(filepath.Clean(rootAbs), "browser-sessions")
	rel, err := filepath.Rel(browserRoot, absPath)
	if err != nil {
		return fmt.Errorf("read plaintext fixture path: %w", err)
	}
	if rel == "." || (!strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".." && !filepath.IsAbs(rel)) {
		return fmt.Errorf("read plaintext fixture: file must not come from ODIN_ROOT/browser-sessions")
	}
	return nil
}

func rejectBrowserSessionFixtureMetadata(values ...string) error {
	for _, value := range values {
		lower := strings.ToLower(value)
		for _, forbidden := range []string{"password", "passkey", "totp", "backup_code", "cookie", "credential", "profile_bytes"} {
			if strings.Contains(lower, forbidden) {
				return fmt.Errorf("browser profile fixture artifact forbidden metadata marker %q", forbidden)
			}
		}
	}
	return nil
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
