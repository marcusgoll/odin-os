package lifecycle

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"odin-os/internal/app/bootstrap"
	commands "odin-os/internal/cli/commands"
	cliscope "odin-os/internal/cli/scope"
	"odin-os/internal/runtime/jobs"
	"odin-os/internal/store/sqlite"
)

type xBioRequestView struct {
	Status            string                  `json:"status"`
	Workflow          string                  `json:"workflow"`
	TaskID            int64                   `json:"task_id"`
	TaskStatus        string                  `json:"task_status"`
	ApprovalID        int64                   `json:"approval_id"`
	MutationRequestID int64                   `json:"mutation_request_id"`
	SessionID         int64                   `json:"session_id"`
	ActionKind        string                  `json:"action_kind"`
	StartURL          string                  `json:"start_url"`
	PayloadHash       string                  `json:"payload_hash"`
	NextCommands      xBioRequestNextCommands `json:"next_commands"`
}

type xBioRequestNextCommands struct {
	Approve string `json:"approve"`
	Apply   string `json:"apply"`
}

type xPublishRequestView struct {
	Status      string `json:"status"`
	Workflow    string `json:"workflow"`
	NextSurface string `json:"next_surface"`
	Compliance  string `json:"compliance"`
	Execution   string `json:"execution"`
}

func runX(ctx context.Context, app bootstrap.App, args []string, stdout io.Writer) error {
	command, err := commands.ParseX(args)
	if err != nil {
		return err
	}
	if command.Workflow == "help" {
		_, err := fmt.Fprintln(stdout, commands.XUsage)
		return err
	}
	switch command.Workflow {
	case "bio":
		if command.Action == "request" {
			view, err := requestXBioChange(ctx, app, command)
			if err != nil {
				return err
			}
			return commands.WriteJSON(stdout, view)
		}
		view, err := applyXBioFromApproval(ctx, app, command)
		if err != nil {
			return err
		}
		return commands.WriteJSON(stdout, browserSessionXBioApplyEnvelope{Result: view})
	case "post", "reply":
		view := xPublishRequestView{
			Status:      "requires_social_outcome",
			Workflow:    command.Workflow,
			NextSurface: "/memory publish <approved-social-outcome-id> via=huginn_x",
			Compliance:  "X post and reply execution is mapped to approved social_outcome publishing; repost, quote, and share remain approval-gated follow-up designs; likes are read-only recommendations only.",
			Execution:   "not_started",
		}
		return commands.WriteJSON(stdout, view)
	default:
		return fmt.Errorf("unsupported x workflow: %s", command.Workflow)
	}
}

func requestXBioChange(ctx context.Context, app bootstrap.App, command commands.XCommand) (xBioRequestView, error) {
	session, err := app.Store.GetBrowserSession(ctx, command.SessionID)
	if err != nil {
		return xBioRequestView{}, err
	}
	if session.Status != sqlite.BrowserSessionStatusVerified {
		return xBioRequestView{}, fmt.Errorf("browser session %d must be verified before X bio request; status=%s", session.ID, session.Status)
	}
	if !isXBrowserSessionDomain(session.Domain) {
		return xBioRequestView{}, fmt.Errorf("browser session %d domain %q is not an X domain", session.ID, session.Domain)
	}
	targetURL := strings.TrimSpace(command.URL)
	if targetURL == "" {
		targetURL = "https://x.com/settings/profile"
	}
	hostAllowed, hostEvidence := browserSessionProofURLMatchesDomain(targetURL, session.Domain)
	if !hostAllowed {
		return xBioRequestView{}, fmt.Errorf("X bio URL does not match browser session domain: %s", hostEvidence)
	}
	manifest, ok := app.Registry.Lookup(command.ProjectKey)
	if !ok {
		return xBioRequestView{}, fmt.Errorf("unknown project: %s", command.ProjectKey)
	}
	resolved := cliscope.Resolve(cliscope.ResolveInput{
		ExplicitTarget: &cliscope.Target{
			ProjectKey:    manifest.Key,
			SystemProject: manifest.SystemProject,
		},
	})
	task, err := newJobService(app).Service.CreateTask(ctx, jobs.CreateTaskParams{
		Resolved:              resolved,
		Title:                 "Apply approved X profile bio change",
		RequestedBy:           "operator",
		WorkKind:              "x_profile_bio",
		ExecutionIntent:       "mutation",
		ExecutionIntentSource: "odin_x_bio_request",
		AcceptanceCriteria: []string{
			"Saved X browser session is verified and reused without a login prompt.",
			"Operator approval is resolved through Odin before external mutation.",
			"Public X profile page contains the approved bio before task completion.",
			"Odin records browser_x_bio evidence with bio_verified=true.",
		},
	})
	if err != nil {
		return xBioRequestView{}, err
	}
	payloadJSON, payloadHash, err := xBioMutationPayload(targetURL, session.ID, command.Bio)
	if err != nil {
		return xBioRequestView{}, err
	}
	allowedDomainsJSON, err := json.Marshal([]string{session.Domain})
	if err != nil {
		return xBioRequestView{}, err
	}
	sessionID := session.ID
	blocked, approval, mutationRequest, err := app.Store.BlockTaskAndRequestBrowserMutationApproval(ctx, sqlite.BlockTaskAndRequestBrowserMutationApprovalParams{
		TaskID:             task.ID,
		RequestedBy:        "operator",
		ActionKind:         "x_profile_bio",
		AllowedDomainsJSON: string(allowedDomainsJSON),
		StartURL:           targetURL,
		BrowserSessionID:   &sessionID,
		PayloadJSON:        payloadJSON,
		PayloadHash:        payloadHash,
	})
	if err != nil {
		return xBioRequestView{}, err
	}
	return xBioRequestView{
		Status:            "approval_required",
		Workflow:          "x_bio",
		TaskID:            blocked.ID,
		TaskStatus:        blocked.Status,
		ApprovalID:        approval.ID,
		MutationRequestID: mutationRequest.ID,
		SessionID:         session.ID,
		ActionKind:        mutationRequest.ActionKind,
		StartURL:          mutationRequest.StartURL,
		PayloadHash:       mutationRequest.PayloadHash,
		NextCommands: xBioRequestNextCommands{
			Approve: fmt.Sprintf("odin approvals resolve %d approve operator approved X bio mutation", approval.ID),
			Apply:   fmt.Sprintf("odin x bio apply --approval-id %d --json", approval.ID),
		},
	}, nil
}

func applyXBioFromApproval(ctx context.Context, app bootstrap.App, command commands.XCommand) (browserSessionXBioApplyView, error) {
	approval, err := app.Store.GetApproval(ctx, command.ApprovalID)
	if err != nil {
		return browserSessionXBioApplyView{}, err
	}
	if approval.Status != "approved" {
		return browserSessionXBioApplyView{}, fmt.Errorf("approval %d must be approved before X bio apply; status=%s", approval.ID, approval.Status)
	}
	mutationRequest, err := app.Store.GetBrowserMutationRequestByApproval(ctx, approval.ID)
	if err != nil {
		return browserSessionXBioApplyView{}, err
	}
	if mutationRequest.ActionKind != "x_profile_bio" {
		return browserSessionXBioApplyView{}, fmt.Errorf("approval %d is for %s, not x_profile_bio", approval.ID, mutationRequest.ActionKind)
	}
	sessionID := int64(0)
	if mutationRequest.BrowserSessionID != nil {
		sessionID = *mutationRequest.BrowserSessionID
	}
	if sessionID <= 0 {
		return browserSessionXBioApplyView{}, fmt.Errorf("x_profile_bio approval %d is missing browser_session_id", approval.ID)
	}
	bio, err := xBioFromMutationPayload(mutationRequest.PayloadJSON)
	if err != nil {
		return browserSessionXBioApplyView{}, err
	}
	return applyXBioChange(ctx, app, commands.BrowserCommand{
		ID:         sessionID,
		TaskID:     approval.TaskID,
		ApprovalID: approval.ID,
		Bio:        bio,
		URL:        mutationRequest.StartURL,
		JSON:       command.JSON,
	})
}

func xBioMutationPayload(startURL string, sessionID int64, bio string) (string, string, error) {
	payload := map[string]any{
		"schema_version":     1,
		"action_kind":        "x_profile_bio",
		"allowed_domains":    []string{"x.com"},
		"start_url":          strings.TrimSpace(startURL),
		"browser_session_id": sessionID,
		"requested_by":       "operator",
		"redaction_policy":   "secrets_and_sensitive_values",
		"approved_fields": map[string]any{
			"bio": strings.TrimSpace(bio),
		},
		"verification": map[string]any{
			"required_evidence": []string{"profile_url", "observed_title", "bio_verified"},
		},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", "", err
	}
	hash := sha256.Sum256(raw)
	return string(raw), hex.EncodeToString(hash[:]), nil
}

func xBioFromMutationPayload(raw string) (string, error) {
	var payload struct {
		ApprovedFields map[string]string `json:"approved_fields"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return "", fmt.Errorf("decode x bio payload: %w", err)
	}
	bio := strings.TrimSpace(payload.ApprovedFields["bio"])
	if bio == "" {
		return "", fmt.Errorf("x bio payload is missing approved_fields.bio")
	}
	return bio, nil
}
