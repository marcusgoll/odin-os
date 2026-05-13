package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"odin-os/internal/cli/overview"
	"odin-os/internal/cli/scope"
	"odin-os/internal/core/workspaces"
	approvalsvc "odin-os/internal/runtime/approvals"
	"odin-os/internal/runtime/projections"
	"odin-os/internal/store/sqlite"
)

const mobileMaxBodyBytes = 1 << 20
const mobileMaxImageBytes = 10 << 20
const mobileMaxAudioBytes = 25 << 20

func registerMobileRoutes(mux *http.ServeMux, deps Dependencies, now func() time.Time) {
	mux.HandleFunc("GET /mobile/status", mobileAuthorized(deps, func(writer http.ResponseWriter, request *http.Request) {
		payload, err := buildStatusPayload(request.Context(), deps, now)
		if err != nil {
			writeAPIError(writer, http.StatusServiceUnavailable, "mobile_status_unavailable", err.Error())
			return
		}
		writeMobileJSON(writer, http.StatusOK, payload)
	}))

	mux.HandleFunc("GET /mobile/overview", mobileAuthorized(deps, func(writer http.ResponseWriter, request *http.Request) {
		if deps.Store == nil {
			writeAPIError(writer, http.StatusServiceUnavailable, "mobile_store_unavailable", "runtime store unavailable")
			return
		}
		status, err := buildStatusPayload(request.Context(), deps, now)
		if err != nil {
			writeAPIError(writer, http.StatusServiceUnavailable, "mobile_overview_unavailable", err.Error())
			return
		}
		readiness := "not_ready"
		if status.Ready {
			readiness = "ready"
		}
		view, err := overview.Service{
			Store:            deps.Store,
			Registry:         deps.Registry,
			RegistrySnapshot: deps.RegistrySnapshot,
			Now:              now,
			ReadinessStatus:  readiness,
			HealthStatus:     status.HealthStatus,
		}.Build(request.Context(), scope.Resolution{Kind: scope.ScopeGlobal})
		if err != nil {
			writeAPIError(writer, http.StatusServiceUnavailable, "mobile_overview_unavailable", err.Error())
			return
		}
		writeMobileJSON(writer, http.StatusOK, view)
	}))

	mux.HandleFunc("GET /mobile/work-items", mobileAuthorized(deps, func(writer http.ResponseWriter, request *http.Request) {
		if deps.ReadModels == nil {
			writeAPIError(writer, http.StatusServiceUnavailable, "read_models_unavailable", "read models unavailable")
			return
		}
		views, err := projections.ListTaskStatusViews(request.Context(), deps.ReadModels)
		if err != nil {
			writeAPIError(writer, http.StatusServiceUnavailable, "mobile_work_items_unavailable", err.Error())
			return
		}
		writeMobileJSON(writer, http.StatusOK, map[string]any{"items": views})
	}))

	mux.HandleFunc("GET /mobile/runs", mobileAuthorized(deps, func(writer http.ResponseWriter, request *http.Request) {
		if deps.ReadModels == nil {
			writeAPIError(writer, http.StatusServiceUnavailable, "read_models_unavailable", "read models unavailable")
			return
		}
		views, err := projections.ListRunSummaryViews(request.Context(), deps.ReadModels)
		if err != nil {
			writeAPIError(writer, http.StatusServiceUnavailable, "mobile_runs_unavailable", err.Error())
			return
		}
		writeMobileJSON(writer, http.StatusOK, map[string]any{"items": views})
	}))

	mux.HandleFunc("GET /mobile/review-queue", mobileAuthorized(deps, func(writer http.ResponseWriter, request *http.Request) {
		items, err := mobileReviewQueue(request.Context(), deps)
		if err != nil {
			writeAPIError(writer, http.StatusServiceUnavailable, "mobile_review_queue_unavailable", err.Error())
			return
		}
		writeMobileJSON(writer, http.StatusOK, map[string]any{"items": items, "count": len(items)})
	}))

	mux.HandleFunc("GET /mobile/approvals", mobileAuthorized(deps, func(writer http.ResponseWriter, request *http.Request) {
		items, err := mobileApprovals(request.Context(), deps)
		if err != nil {
			writeAPIError(writer, http.StatusServiceUnavailable, "mobile_approvals_unavailable", err.Error())
			return
		}
		writeMobileJSON(writer, http.StatusOK, map[string]any{"items": items, "count": len(items)})
	}))

	mux.HandleFunc("POST /mobile/approvals/{approval_id}/decision", mobileAuthorized(deps, func(writer http.ResponseWriter, request *http.Request) {
		handleMobileApprovalDecision(writer, request, deps)
	}))

	mux.HandleFunc("POST /mobile/intake/raw", mobileAuthorized(deps, func(writer http.ResponseWriter, request *http.Request) {
		handleMobileRawIntake(writer, request, deps, now)
	}))

	mux.HandleFunc("POST /mobile/intake/attachments", mobileAuthorized(deps, func(writer http.ResponseWriter, request *http.Request) {
		handleMobileAttachmentIntake(writer, request, deps, now)
	}))

	mux.HandleFunc("GET /mobile/notifications/preferences", mobileAuthorized(deps, func(writer http.ResponseWriter, request *http.Request) {
		writeMobileJSON(writer, http.StatusOK, mobileNotificationPreferencesResponse{
			Status:        "not_configured",
			Enabled:       false,
			DeliveryModes: []string{"web_push"},
			Subscriptions: []mobileNotificationSubscriptionView{},
		})
	}))

	mux.HandleFunc("POST /mobile/notifications/subscriptions", mobileAuthorized(deps, func(writer http.ResponseWriter, request *http.Request) {
		handleMobileNotificationSubscription(writer, request, deps, now)
	}))

	mux.HandleFunc("GET /mobile/browser/status", mobileAuthorized(deps, func(writer http.ResponseWriter, request *http.Request) {
		status, err := mobileBrowserStatus(request.Context(), deps)
		if err != nil {
			writeAPIError(writer, http.StatusServiceUnavailable, "mobile_browser_status_unavailable", err.Error())
			return
		}
		writeMobileJSON(writer, http.StatusOK, status)
	}))
}

func mobileAuthorized(deps Dependencies, next func(http.ResponseWriter, *http.Request)) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		if statusCode, ok := authorizeAdmin(request, deps.AdminToken); !ok {
			writeAdminAuthorizationError(writer, statusCode)
			return
		}
		next(writer, request)
	}
}

func writeMobileJSON(writer http.ResponseWriter, statusCode int, payload any) {
	writer.Header().Set("Cache-Control", "no-store")
	writeJSON(writer, statusCode, payload)
}

type mobileReviewItem struct {
	QueueID        string   `json:"queue_id"`
	SourceType     string   `json:"source_type"`
	ObjectID       int64    `json:"object_id"`
	ObjectKey      string   `json:"object_key"`
	Title          string   `json:"title"`
	Status         string   `json:"status"`
	Reason         string   `json:"reason,omitempty"`
	ProjectKey     string   `json:"project_key,omitempty"`
	AllowedActions []string `json:"allowed_actions"`
}

type mobileApprovalView struct {
	ApprovalID      int64  `json:"approval_id"`
	TaskID          int64  `json:"task_id"`
	TaskKey         string `json:"task_key"`
	ProjectKey      string `json:"project_key"`
	Status          string `json:"status"`
	RequestedAt     string `json:"requested_at"`
	ResolverSupport string `json:"resolver_support,omitempty"`
}

type mobileIntakeResponse struct {
	IntakeItem mobileIntakeItemView `json:"intake_item"`
}

type mobileIntakeItemView struct {
	ID         int64  `json:"id"`
	Key        string `json:"key"`
	Status     string `json:"status"`
	Source     string `json:"source"`
	IntakeType string `json:"intake_type"`
	Subject    string `json:"subject"`
	Scope      string `json:"scope,omitempty"`
	ScopeKey   string `json:"scope_key,omitempty"`
	DedupKey   string `json:"dedup_key"`
	ReceivedAt string `json:"received_at"`
	CreatedAt  string `json:"created_at"`
}

type mobileApprovalDecisionRequest struct {
	Action     string `json:"action"`
	Reason     string `json:"reason"`
	DecisionBy string `json:"decision_by"`
}

type mobileApprovalDecisionResponse struct {
	ApprovalID      int64  `json:"approval_id"`
	TaskID          int64  `json:"task_id"`
	Status          string `json:"status"`
	Action          string `json:"action"`
	ResolverSupport string `json:"resolver_support"`
	ResolvedAt      string `json:"resolved_at,omitempty"`
}

type mobileRawIntakeRequest struct {
	Kind       string `json:"kind"`
	Title      string `json:"title"`
	Content    string `json:"content"`
	Text       string `json:"text"`
	Prompt     string `json:"prompt"`
	Idea       string `json:"idea"`
	ProjectKey string `json:"project_key"`
	DedupKey   string `json:"dedup_key"`
	Transcript string `json:"transcript"`
	SourceApp  string `json:"source_app"`
	ShareURL   string `json:"share_url"`
	ShareTitle string `json:"share_title"`
}

type mobileAttachmentIntakeRequest struct {
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	SizeBytes   int64  `json:"size_bytes"`
	Digest      string `json:"digest"`
	Description string `json:"description"`
	ProjectKey  string `json:"project_key"`
}

type mobileNotificationPreferencesResponse struct {
	Status        string                               `json:"status"`
	Enabled       bool                                 `json:"enabled"`
	DeliveryModes []string                             `json:"delivery_modes"`
	Subscriptions []mobileNotificationSubscriptionView `json:"subscriptions"`
}

type mobileNotificationSubscriptionView struct {
	Status string `json:"status"`
}

type mobileNotificationSubscriptionRequest struct {
	Endpoint  string `json:"endpoint"`
	UserAgent string `json:"user_agent"`
	Platform  string `json:"platform"`
}

type mobileNotificationSubscriptionResponse struct {
	Status     string               `json:"status"`
	IntakeItem mobileIntakeItemView `json:"intake_item"`
}

type mobileBrowserStatusResponse struct {
	SessionCount      int                         `json:"session_count"`
	LoginRequestCount int                         `json:"login_request_count"`
	RunnerCount       int                         `json:"runner_count"`
	Sessions          []mobileBrowserSessionView  `json:"sessions"`
	LoginRequests     []mobileBrowserLoginRequest `json:"login_requests"`
	Runners           []mobileBrowserRunnerView   `json:"runners"`
}

type mobileBrowserSessionView struct {
	ID             int64  `json:"id"`
	Name           string `json:"name"`
	Domain         string `json:"domain"`
	PermissionTier string `json:"permission_tier"`
	Status         string `json:"status"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
	LastVerifiedAt string `json:"last_verified_at,omitempty"`
	ExpiresAt      string `json:"expires_at,omitempty"`
}

type mobileBrowserLoginRequest struct {
	ID          int64  `json:"id"`
	SessionID   int64  `json:"session_id"`
	Status      string `json:"status"`
	ExpiresAt   string `json:"expires_at"`
	CompletedAt string `json:"completed_at,omitempty"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

type mobileBrowserRunnerView struct {
	ID             int64  `json:"id"`
	SessionID      int64  `json:"session_id"`
	LoginRequestID int64  `json:"login_request_id"`
	Status         string `json:"status"`
	ExpiresAt      string `json:"expires_at"`
	StartedAt      string `json:"started_at,omitempty"`
	ExitedAt       string `json:"exited_at,omitempty"`
	CompletedAt    string `json:"completed_at,omitempty"`
	CancelledAt    string `json:"cancelled_at,omitempty"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
	ErrorCode      string `json:"error_code,omitempty"`
	ErrorMessage   string `json:"error_message,omitempty"`
}

func mobileReviewQueue(ctx context.Context, deps Dependencies) ([]mobileReviewItem, error) {
	if deps.Store == nil || deps.ReadModels == nil {
		return nil, fmt.Errorf("runtime store and read models are required")
	}
	items := make([]mobileReviewItem, 0)
	intakeItems, err := deps.Store.ListIntakeItems(ctx, sqlite.ListIntakeItemsParams{WorkspaceID: workspaces.DefaultWorkspaceKey})
	if err != nil {
		return nil, err
	}
	for _, item := range intakeItems {
		if item.Status != "review_required" && item.Status != "approval_required" {
			continue
		}
		actions := []string{"accept", "reject", "clarify", "archive"}
		if item.Status == "approval_required" {
			actions = []string{"approve", "deny"}
		}
		items = append(items, mobileReviewItem{
			QueueID:        fmt.Sprintf("intake-review:%d", item.ID),
			SourceType:     "intake_review",
			ObjectID:       item.ID,
			ObjectKey:      fmt.Sprintf("intake-%d", item.ID),
			Title:          item.Subject,
			Status:         item.Status,
			ProjectKey:     item.ScopeKey,
			AllowedActions: actions,
		})
	}
	approvals, err := projections.ListPendingApprovalViews(ctx, deps.ReadModels)
	if err != nil {
		return nil, err
	}
	for _, approval := range approvals {
		items = append(items, mobileReviewItem{
			QueueID:        fmt.Sprintf("approval:%d", approval.ApprovalID),
			SourceType:     "approval",
			ObjectID:       approval.ApprovalID,
			ObjectKey:      fmt.Sprintf("approval-%d", approval.ApprovalID),
			Title:          approval.TaskKey,
			Status:         approval.Status,
			Reason:         "approval_required",
			ProjectKey:     approval.ProjectKey,
			AllowedActions: []string{"approve", "deny"},
		})
	}
	tasks, err := projections.ListTaskStatusViews(ctx, deps.ReadModels)
	if err != nil {
		return nil, err
	}
	for _, task := range tasks {
		if strings.EqualFold(task.Status, "failed") {
			items = append(items, mobileReviewItem{
				QueueID:        fmt.Sprintf("failed-work:%d", task.TaskID),
				SourceType:     "failed_work",
				ObjectID:       task.TaskID,
				ObjectKey:      task.TaskKey,
				Title:          task.Title,
				Status:         task.Status,
				Reason:         "retry_allowed",
				ProjectKey:     task.ProjectKey,
				AllowedActions: []string{"retry", "follow-up"},
			})
		}
	}
	return items, nil
}

func mobileApprovals(ctx context.Context, deps Dependencies) ([]mobileApprovalView, error) {
	if deps.Store == nil || deps.ReadModels == nil {
		return nil, fmt.Errorf("runtime store and read models are required")
	}
	approvals, err := projections.ListPendingApprovalViews(ctx, deps.ReadModels)
	if err != nil {
		return nil, err
	}
	service := approvalsvc.Service{Store: deps.Store}
	items := make([]mobileApprovalView, 0, len(approvals))
	for _, approval := range approvals {
		item := mobileApprovalView{
			ApprovalID:  approval.ApprovalID,
			TaskID:      approval.TaskID,
			TaskKey:     approval.TaskKey,
			ProjectKey:  approval.ProjectKey,
			Status:      approval.Status,
			RequestedAt: approval.RequestedAt,
		}
		if detail, err := service.Detail(ctx, approval.ApprovalID); err == nil {
			item.ResolverSupport = string(detail.ResolverSupport)
		}
		items = append(items, item)
	}
	return items, nil
}

func handleMobileApprovalDecision(writer http.ResponseWriter, request *http.Request, deps Dependencies) {
	if deps.Store == nil {
		writeAPIError(writer, http.StatusServiceUnavailable, "mobile_approval_store_unavailable", "approval store unavailable")
		return
	}
	approvalID, err := parsePositivePathID(request.PathValue("approval_id"), "approval id")
	if err != nil {
		writeAPIError(writer, http.StatusBadRequest, "invalid_approval_id", err.Error())
		return
	}
	var body mobileApprovalDecisionRequest
	if err := decodeMobileJSON(writer, request, &body); err != nil {
		writeAPIError(writer, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	body.Action = strings.ToLower(strings.TrimSpace(body.Action))
	if body.Action != "approve" && body.Action != "deny" {
		writeAPIError(writer, http.StatusBadRequest, "invalid_approval_action", "action must be approve or deny")
		return
	}
	body.Reason = strings.TrimSpace(body.Reason)
	if body.Reason == "" {
		writeAPIError(writer, http.StatusBadRequest, "approval_reason_required", "reason is required")
		return
	}
	decisionBy := strings.TrimSpace(body.DecisionBy)
	if decisionBy == "" {
		decisionBy = "mobile-api"
	}
	result, err := approvalsvc.Service{Store: deps.Store}.Resolve(request.Context(), approvalsvc.ResolveParams{
		ApprovalID: approvalID,
		Action:     body.Action,
		DecisionBy: decisionBy,
		Reason:     body.Reason,
	})
	if err != nil {
		if errors.Is(err, approvalsvc.ErrUnsupportedResolver) {
			writeAPIError(writer, http.StatusConflict, "approval_resolver_unsupported", err.Error())
			return
		}
		writeAPIError(writer, http.StatusConflict, "approval_decision_failed", err.Error())
		return
	}
	writeMobileJSON(writer, http.StatusOK, mobileApprovalDecisionResponse{
		ApprovalID:      result.Approval.ID,
		TaskID:          result.Approval.TaskID,
		Status:          result.Approval.Status,
		Action:          body.Action,
		ResolverSupport: string(result.ResolverSupport),
		ResolvedAt:      formatMobileOptionalTime(result.Approval.ResolvedAt),
	})
}

func handleMobileRawIntake(writer http.ResponseWriter, request *http.Request, deps Dependencies, now func() time.Time) {
	if strings.HasPrefix(strings.ToLower(request.Header.Get("Content-Type")), "multipart/form-data") {
		handleMobileMultipartRawIntake(writer, request, deps, now)
		return
	}
	var body mobileRawIntakeRequest
	if err := decodeMobileJSON(writer, request, &body); err != nil {
		writeAPIError(writer, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	kind := strings.ToLower(strings.TrimSpace(body.Kind))
	if kind == "" {
		kind = "text"
	}
	if !mobileCaptureKindAllowed(kind) {
		writeAPIError(writer, http.StatusBadRequest, "invalid_intake_kind", "kind must be text, note, prompt, idea, task, bug, project_note, photo, or voice_note")
		return
	}
	content := firstNonEmpty(body.Content, body.Text, body.Prompt, body.Idea)
	if content == "" {
		writeAPIError(writer, http.StatusBadRequest, "intake_content_required", "content is required")
		return
	}
	title := strings.TrimSpace(body.Title)
	if title == "" {
		title = mobileDefaultTitle(kind, content)
	}
	facts := map[string]any{
		"source":         "mobile_api",
		"requested_by":   "mobile_api",
		"payload_policy": "stored_in_source_facts_json",
		"kind":           kind,
		"title":          title,
		"body":           content,
		"content_sha256": mobileHash(content),
		"received_via":   "odin_mobile_api",
		"transcript":     strings.TrimSpace(body.Transcript),
		"source_app":     strings.TrimSpace(body.SourceApp),
		"share": map[string]string{
			"url":   strings.TrimSpace(body.ShareURL),
			"title": strings.TrimSpace(body.ShareTitle),
		},
	}
	item, err := createMobileIntakeItem(request.Context(), deps, mobileCreateIntakeInput{
		Kind:       kind,
		Title:      title,
		DedupeKey:  strings.TrimSpace(body.DedupKey),
		ProjectKey: strings.TrimSpace(body.ProjectKey),
		Facts:      facts,
		Now:        now,
	})
	if err != nil {
		writeAPIError(writer, http.StatusBadRequest, "mobile_intake_create_failed", err.Error())
		return
	}
	writeMobileJSON(writer, http.StatusAccepted, mobileIntakeResponse{IntakeItem: mobileIntakeView(item)})
}

func handleMobileMultipartRawIntake(writer http.ResponseWriter, request *http.Request, deps Dependencies, now func() time.Time) {
	request.Body = http.MaxBytesReader(writer, request.Body, mobileMaxAudioBytes+mobileMaxBodyBytes)
	if err := request.ParseMultipartForm(mobileMaxAudioBytes + mobileMaxBodyBytes); err != nil {
		writeAPIError(writer, http.StatusBadRequest, "invalid_multipart_request", err.Error())
		return
	}
	kind := strings.ToLower(strings.TrimSpace(request.FormValue("kind")))
	if kind == "" {
		kind = "photo"
	}
	if !mobileCaptureKindAllowed(kind) {
		writeAPIError(writer, http.StatusBadRequest, "invalid_intake_kind", "kind must be text, note, prompt, idea, task, bug, project_note, photo, or voice_note")
		return
	}
	file, header, err := request.FormFile("attachment")
	if err != nil {
		writeAPIError(writer, http.StatusBadRequest, "attachment_required", "attachment file is required")
		return
	}
	defer file.Close()

	contentType := strings.TrimSpace(header.Header.Get("Content-Type"))
	attachmentKind, maxBytes, ok := mobileAllowedAttachment(contentType)
	if !ok {
		writeAPIError(writer, http.StatusBadRequest, "attachment_type_not_allowed", "attachment content_type must be an allowed image or audio type; retry after choosing a supported file")
		return
	}
	data, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		writeAPIError(writer, http.StatusBadRequest, "attachment_read_failed", err.Error())
		return
	}
	if int64(len(data)) > maxBytes {
		writeAPIError(writer, http.StatusBadRequest, "attachment_too_large", fmt.Sprintf("attachment exceeds %d bytes; retry with a smaller file", maxBytes))
		return
	}
	title := strings.TrimSpace(request.FormValue("title"))
	if title == "" {
		title = header.Filename
	}
	content := strings.TrimSpace(request.FormValue("content"))
	if content == "" {
		content = strings.TrimSpace(request.FormValue("text"))
	}
	sha := mobileHashBytes(data)
	attachmentFacts := map[string]any{
		"kind":         attachmentKind,
		"filename":     header.Filename,
		"content_type": contentType,
		"size_bytes":   len(data),
		"sha256":       sha,
		"status":       "stored",
	}
	facts := map[string]any{
		"source":         "mobile_api",
		"requested_by":   "mobile_api",
		"payload_policy": "stored_in_source_facts_json",
		"kind":           kind,
		"title":          title,
		"body":           content,
		"content_sha256": mobileHash(content),
		"received_via":   "odin_mobile_api",
		"transcript":     strings.TrimSpace(request.FormValue("transcript")),
		"source_app":     strings.TrimSpace(request.FormValue("source_app")),
		"share": map[string]string{
			"url":   strings.TrimSpace(request.FormValue("share_url")),
			"title": strings.TrimSpace(request.FormValue("share_title")),
		},
		"attachments": []map[string]any{attachmentFacts},
	}
	item, err := createMobileIntakeItem(request.Context(), deps, mobileCreateIntakeInput{
		Kind:       kind,
		Title:      title,
		DedupeKey:  strings.TrimSpace(request.FormValue("dedup_key")),
		ProjectKey: strings.TrimSpace(request.FormValue("project_key")),
		Facts:      facts,
		Now:        now,
	})
	if err != nil {
		writeAPIError(writer, http.StatusBadRequest, "mobile_intake_create_failed", err.Error())
		return
	}
	if _, err := deps.Store.CreateIntakeAttachment(request.Context(), sqlite.CreateIntakeAttachmentParams{
		IntakeItemID: item.ID,
		Kind:         attachmentKind,
		Filename:     header.Filename,
		ContentType:  contentType,
		SizeBytes:    int64(len(data)),
		SHA256:       sha,
		Status:       "stored",
		Bytes:        data,
	}); err != nil {
		writeAPIError(writer, http.StatusBadRequest, "mobile_attachment_store_failed", err.Error())
		return
	}
	writeMobileJSON(writer, http.StatusAccepted, mobileIntakeResponse{IntakeItem: mobileIntakeView(item)})
}

func handleMobileAttachmentIntake(writer http.ResponseWriter, request *http.Request, deps Dependencies, now func() time.Time) {
	var body mobileAttachmentIntakeRequest
	if err := decodeMobileJSON(writer, request, &body); err != nil {
		writeAPIError(writer, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	filename := strings.TrimSpace(body.Filename)
	if filename == "" {
		writeAPIError(writer, http.StatusBadRequest, "attachment_filename_required", "filename is required")
		return
	}
	facts := map[string]any{
		"source":        "mobile_api",
		"kind":          "attachment_metadata",
		"filename":      filename,
		"content_type":  strings.TrimSpace(body.ContentType),
		"size_bytes":    body.SizeBytes,
		"digest":        strings.TrimSpace(body.Digest),
		"description":   strings.TrimSpace(body.Description),
		"metadata_only": true,
	}
	item, err := createMobileIntakeItem(request.Context(), deps, mobileCreateIntakeInput{
		Kind:       "attachment_metadata",
		Title:      filename,
		ProjectKey: strings.TrimSpace(body.ProjectKey),
		Facts:      facts,
		Now:        now,
	})
	if err != nil {
		writeAPIError(writer, http.StatusBadRequest, "mobile_attachment_create_failed", err.Error())
		return
	}
	writeMobileJSON(writer, http.StatusAccepted, mobileIntakeResponse{IntakeItem: mobileIntakeView(item)})
}

func handleMobileNotificationSubscription(writer http.ResponseWriter, request *http.Request, deps Dependencies, now func() time.Time) {
	var body mobileNotificationSubscriptionRequest
	if err := decodeMobileJSON(writer, request, &body); err != nil {
		writeAPIError(writer, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	endpoint := strings.TrimSpace(body.Endpoint)
	if endpoint == "" {
		writeAPIError(writer, http.StatusBadRequest, "notification_endpoint_required", "endpoint is required")
		return
	}
	endpointHost := ""
	if parsed, err := url.Parse(endpoint); err == nil {
		endpointHost = parsed.Host
	}
	facts := map[string]any{
		"source":          "mobile_api",
		"kind":            "notification_subscription",
		"endpoint_host":   endpointHost,
		"endpoint_sha256": mobileHash(endpoint),
		"user_agent":      strings.TrimSpace(body.UserAgent),
		"platform":        strings.TrimSpace(body.Platform),
		"secret_policy":   "subscription keys and endpoint URLs are not echoed in API responses",
	}
	item, err := createMobileIntakeItem(request.Context(), deps, mobileCreateIntakeInput{
		Kind:  "notification_subscription",
		Title: "Mobile notification subscription request",
		Facts: facts,
		Now:   now,
	})
	if err != nil {
		writeAPIError(writer, http.StatusBadRequest, "mobile_notification_subscription_failed", err.Error())
		return
	}
	writeMobileJSON(writer, http.StatusAccepted, mobileNotificationSubscriptionResponse{
		Status:     "accepted_as_intake_metadata",
		IntakeItem: mobileIntakeView(item),
	})
}

type mobileCreateIntakeInput struct {
	Kind       string
	Title      string
	DedupeKey  string
	ProjectKey string
	Facts      map[string]any
	Now        func() time.Time
}

func createMobileIntakeItem(ctx context.Context, deps Dependencies, input mobileCreateIntakeInput) (sqlite.IntakeItem, error) {
	if deps.Store == nil {
		return sqlite.IntakeItem{}, fmt.Errorf("runtime store unavailable")
	}
	if input.Now == nil {
		input.Now = func() time.Time { return time.Now().UTC() }
	}
	scopeKind := ""
	scopeKey := strings.TrimSpace(input.ProjectKey)
	if scopeKey != "" {
		if len(deps.Registry.Projects()) > 0 {
			if _, ok := deps.Registry.Lookup(scopeKey); !ok {
				return sqlite.IntakeItem{}, fmt.Errorf("unknown project %q", scopeKey)
			}
		}
		scopeKind = "project"
	}
	factsJSON, err := json.Marshal(input.Facts)
	if err != nil {
		return sqlite.IntakeItem{}, err
	}
	dedupeKey := strings.TrimSpace(input.DedupeKey)
	if dedupeKey == "" {
		dedupeKey = "mobile:" + mobileHash(string(factsJSON))
	}
	now := input.Now().UTC()
	return deps.Store.CreateIntakeItem(ctx, sqlite.CreateIntakeItemParams{
		WorkspaceID:         workspaces.DefaultWorkspaceKey,
		SourceFamily:        "mobile_api",
		ExternalObjectID:    dedupeKey,
		EventKind:           input.Kind,
		Subject:             input.Title,
		DedupeKey:           dedupeKey,
		DedupeRecipeVersion: "mobile-api-v1",
		SourceFactsJSON:     string(factsJSON),
		Status:              "received",
		Scope:               scopeKind,
		ScopeKey:            scopeKey,
		Summary:             input.Title,
		ReceivedAt:          now,
	})
}

func mobileBrowserStatus(ctx context.Context, deps Dependencies) (mobileBrowserStatusResponse, error) {
	if deps.Store == nil {
		return mobileBrowserStatusResponse{}, fmt.Errorf("runtime store unavailable")
	}
	sessions, err := deps.Store.ListBrowserSessions(ctx, sqlite.ListBrowserSessionsParams{})
	if err != nil {
		return mobileBrowserStatusResponse{}, err
	}
	response := mobileBrowserStatusResponse{
		Sessions:      make([]mobileBrowserSessionView, 0, len(sessions)),
		LoginRequests: []mobileBrowserLoginRequest{},
		Runners:       []mobileBrowserRunnerView{},
	}
	for _, session := range sessions {
		response.Sessions = append(response.Sessions, mobileBrowserSessionView{
			ID:             session.ID,
			Name:           session.Name,
			Domain:         session.Domain,
			PermissionTier: string(session.PermissionTier),
			Status:         string(session.Status),
			CreatedAt:      session.CreatedAt.UTC().Format(time.RFC3339),
			UpdatedAt:      session.UpdatedAt.UTC().Format(time.RFC3339),
			LastVerifiedAt: formatMobileOptionalTime(session.LastVerifiedAt),
			ExpiresAt:      formatMobileOptionalTime(session.ExpiresAt),
		})
		requests, err := deps.Store.ListBrowserSessionLoginRequests(ctx, sqlite.ListBrowserSessionLoginRequestsParams{SessionID: session.ID})
		if err != nil {
			return mobileBrowserStatusResponse{}, err
		}
		for _, loginRequest := range requests {
			response.LoginRequests = append(response.LoginRequests, mobileBrowserLoginRequest{
				ID:          loginRequest.ID,
				SessionID:   loginRequest.SessionID,
				Status:      string(loginRequest.Status),
				ExpiresAt:   loginRequest.ExpiresAt.UTC().Format(time.RFC3339),
				CompletedAt: formatMobileOptionalTime(loginRequest.CompletedAt),
				CreatedAt:   loginRequest.CreatedAt.UTC().Format(time.RFC3339),
				UpdatedAt:   loginRequest.UpdatedAt.UTC().Format(time.RFC3339),
			})
			runners, err := deps.Store.ListBrowserHandoffRunners(ctx, sqlite.ListBrowserHandoffRunnersParams{LoginRequestID: loginRequest.ID})
			if err != nil {
				return mobileBrowserStatusResponse{}, err
			}
			for _, runner := range runners {
				response.Runners = append(response.Runners, mobileBrowserRunnerView{
					ID:             runner.ID,
					SessionID:      runner.SessionID,
					LoginRequestID: runner.LoginRequestID,
					Status:         string(runner.Status),
					ExpiresAt:      runner.ExpiresAt.UTC().Format(time.RFC3339),
					StartedAt:      formatMobileOptionalTime(runner.StartedAt),
					ExitedAt:       formatMobileOptionalTime(runner.ExitedAt),
					CompletedAt:    formatMobileOptionalTime(runner.CompletedAt),
					CancelledAt:    formatMobileOptionalTime(runner.CancelledAt),
					CreatedAt:      runner.CreatedAt.UTC().Format(time.RFC3339),
					UpdatedAt:      runner.UpdatedAt.UTC().Format(time.RFC3339),
					ErrorCode:      mobileOptionalString(runner.ErrorCode),
					ErrorMessage:   mobileOptionalString(runner.ErrorMessage),
				})
			}
		}
	}
	response.SessionCount = len(response.Sessions)
	response.LoginRequestCount = len(response.LoginRequests)
	response.RunnerCount = len(response.Runners)
	return response, nil
}

func decodeMobileJSON(writer http.ResponseWriter, request *http.Request, target any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, mobileMaxBodyBytes))
	if err := decoder.Decode(target); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

func parsePositivePathID(raw string, label string) (int64, error) {
	value, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", label)
	}
	return value, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func mobileDefaultTitle(kind string, content string) string {
	content = strings.Join(strings.Fields(content), " ")
	if len(content) > 72 {
		content = content[:72]
	}
	if content == "" {
		return "Mobile " + kind
	}
	return content
}

func mobileCaptureKindAllowed(kind string) bool {
	switch kind {
	case "text", "note", "prompt", "idea", "task", "bug", "project_note", "photo", "image", "voice_note", "audio":
		return true
	default:
		return false
	}
}

func mobileAllowedAttachment(contentType string) (string, int64, bool) {
	switch strings.ToLower(strings.TrimSpace(contentType)) {
	case "image/jpeg", "image/png", "image/webp", "image/gif":
		return "image", mobileMaxImageBytes, true
	case "audio/webm", "audio/mpeg", "audio/mp4", "audio/wav", "audio/ogg", "audio/x-wav":
		return "audio", mobileMaxAudioBytes, true
	default:
		return "", 0, false
	}
}

func mobileHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func mobileHashBytes(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func mobileIntakeView(item sqlite.IntakeItem) mobileIntakeItemView {
	return mobileIntakeItemView{
		ID:         item.ID,
		Key:        fmt.Sprintf("intake-%d", item.ID),
		Status:     item.Status,
		Source:     item.SourceFamily,
		IntakeType: item.EventKind,
		Subject:    item.Subject,
		Scope:      item.Scope,
		ScopeKey:   item.ScopeKey,
		DedupKey:   item.DedupeKey,
		ReceivedAt: item.ReceivedAt.UTC().Format(time.RFC3339),
		CreatedAt:  item.CreatedAt.UTC().Format(time.RFC3339),
	}
}

func formatMobileOptionalTime(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func mobileOptionalString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
