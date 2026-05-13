package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"odin-os/internal/cli/overview"
	"odin-os/internal/cli/scope"
	"odin-os/internal/core/workspaces"
	approvalsvc "odin-os/internal/runtime/approvals"
	runtimeevents "odin-os/internal/runtime/events"
	"odin-os/internal/runtime/projections"
	"odin-os/internal/store/sqlite"
)

const mobileMaxBodyBytes = 1 << 20
const mobileMaxImageBytes = 10 << 20
const mobileMaxAudioBytes = 25 << 20
const mobileSessionCookieName = "odin_mobile_session"
const mobileIntakeRateLimit = 30

func registerMobileRoutes(mux *http.ServeMux, deps Dependencies, now func() time.Time) {
	intakeLimiter := newMobileRateLimiter(mobileIntakeRateLimit, time.Minute, now)
	mux.HandleFunc("POST /mobile/devices/register", func(writer http.ResponseWriter, request *http.Request) {
		if statusCode, ok := authorizeAdmin(request, deps.AdminToken); !ok {
			writeAdminAuthorizationError(writer, statusCode)
			return
		}
		handleMobileDeviceRegister(writer, request, deps, now)
	})
	mux.HandleFunc("POST /mobile/devices/{device_id}/revoke", mobileAuthorized(deps, func(writer http.ResponseWriter, request *http.Request) {
		handleMobileDeviceRevoke(writer, request, deps)
	}))

	mux.HandleFunc("GET /mobile/summary", mobileAuthorized(deps, func(writer http.ResponseWriter, request *http.Request) {
		summary, err := mobileSummary(request.Context(), deps, now)
		if err != nil {
			writeAPIError(writer, http.StatusServiceUnavailable, "mobile_summary_unavailable", err.Error())
			return
		}
		writeMobileJSON(writer, http.StatusOK, summary)
	}))

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

	mux.HandleFunc("GET /mobile/review-queue/detail", mobileAuthorized(deps, func(writer http.ResponseWriter, request *http.Request) {
		items, err := mobileReviewQueueDetail(request.Context(), deps, request.URL.Query().Get("queue_id"))
		if err != nil {
			writeAPIError(writer, http.StatusServiceUnavailable, "mobile_review_detail_unavailable", err.Error())
			return
		}
		writeMobileJSON(writer, http.StatusOK, map[string]any{"items": items, "count": len(items)})
	}))

	mux.HandleFunc("GET /mobile/review", mobileAuthorized(deps, func(writer http.ResponseWriter, request *http.Request) {
		items, err := mobileReviewQueue(request.Context(), deps)
		if err != nil {
			writeAPIError(writer, http.StatusServiceUnavailable, "mobile_review_unavailable", err.Error())
			return
		}
		writeMobileJSON(writer, http.StatusOK, map[string]any{"items": items, "count": len(items)})
	}))

	mux.HandleFunc("GET /mobile/work", mobileAuthorized(deps, func(writer http.ResponseWriter, request *http.Request) {
		if deps.ReadModels == nil {
			writeAPIError(writer, http.StatusServiceUnavailable, "read_models_unavailable", "read models unavailable")
			return
		}
		workItems, err := projections.ListTaskStatusViews(request.Context(), deps.ReadModels)
		if err != nil {
			writeAPIError(writer, http.StatusServiceUnavailable, "mobile_work_unavailable", err.Error())
			return
		}
		runs, err := projections.ListRunSummaryViews(request.Context(), deps.ReadModels)
		if err != nil {
			writeAPIError(writer, http.StatusServiceUnavailable, "mobile_runs_unavailable", err.Error())
			return
		}
		writeMobileJSON(writer, http.StatusOK, map[string]any{"work_items": workItems, "runs": runs})
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

	mux.HandleFunc("GET /mobile/notifications", mobileAuthorized(deps, func(writer http.ResponseWriter, request *http.Request) {
		items, err := mobileNotifications(request.Context(), deps)
		if err != nil {
			writeAPIError(writer, http.StatusServiceUnavailable, "mobile_notifications_unavailable", err.Error())
			return
		}
		writeMobileJSON(writer, http.StatusOK, map[string]any{"items": items, "count": len(items)})
	}))

	mux.HandleFunc("POST /mobile/intake/raw", mobileAuthorized(deps, func(writer http.ResponseWriter, request *http.Request) {
		if !intakeLimiter.allow(mobileRateLimitKey(request)) {
			writeAPIError(writer, http.StatusTooManyRequests, "mobile_intake_rate_limited", "mobile intake rate limit exceeded")
			return
		}
		handleMobileRawIntake(writer, request, deps, now)
	}))

	mux.HandleFunc("POST /mobile/intake/share", mobileAuthorized(deps, func(writer http.ResponseWriter, request *http.Request) {
		handleMobileShareIntake(writer, request, deps, now)
	}))

	mux.HandleFunc("POST /mobile/intake/attachments", mobileAuthorized(deps, func(writer http.ResponseWriter, request *http.Request) {
		if !intakeLimiter.allow(mobileRateLimitKey(request)) {
			writeAPIError(writer, http.StatusTooManyRequests, "mobile_intake_rate_limited", "mobile intake rate limit exceeded")
			return
		}
		handleMobileAttachmentIntake(writer, request, deps, now)
	}))

	mux.HandleFunc("GET /mobile/inbox", mobileAuthorized(deps, func(writer http.ResponseWriter, request *http.Request) {
		if deps.Store == nil {
			writeAPIError(writer, http.StatusServiceUnavailable, "mobile_inbox_store_unavailable", "runtime store unavailable")
			return
		}
		items, err := deps.Store.ListIntakeItems(request.Context(), sqlite.ListIntakeItemsParams{WorkspaceID: workspaces.DefaultWorkspaceKey})
		if err != nil {
			writeAPIError(writer, http.StatusServiceUnavailable, "mobile_inbox_unavailable", err.Error())
			return
		}
		views := make([]mobileIntakeItemView, 0, len(items))
		for _, item := range items {
			views = append(views, mobileIntakeView(item))
		}
		writeMobileJSON(writer, http.StatusOK, map[string]any{
			"raw_items":    views,
			"linked_items": []mobileIntakeItemView{},
			"capture": map[string]any{
				"enabled":          true,
				"endpoint":         "/mobile/intake/raw",
				"policy_statement": "Mobile capture stores raw intake evidence first; review and promotion stay in Odin.",
			},
		})
	}))

	mux.HandleFunc("GET /mobile/settings", mobileAuthorized(deps, func(writer http.ResponseWriter, request *http.Request) {
		writeMobileJSON(writer, http.StatusOK, map[string]any{
			"runtime_source": "odin-api",
			"admin_actions": map[string]any{
				"enabled":          true,
				"policy_statement": "Mutations require an Odin admin token and explicit API authorization.",
			},
			"offline": mobileOfflinePolicy(),
			"capture": map[string]any{
				"enabled":          true,
				"policy_statement": "Capture requires a live Odin connection; failed browser uploads are visibly retained for attended retry.",
			},
			"endpoints": []string{
				"/mobile/summary",
				"/mobile/approvals",
				"/mobile/review",
				"/mobile/work",
				"/mobile/inbox",
				"/mobile/settings",
				"/mobile/intake/raw",
			},
		})
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
	mux.HandleFunc("POST /mobile/notifications/subscriptions/{subscription_id}/revoke", mobileAuthorized(deps, func(writer http.ResponseWriter, request *http.Request) {
		handleMobileNotificationSubscriptionRevoke(writer, request, deps)
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
		auth, statusCode, code, message, ok := authorizeMobileRequest(request, deps)
		if !ok {
			if code == "" {
				writeAdminAuthorizationError(writer, statusCode)
				return
			}
			writeAPIError(writer, statusCode, code, message)
			return
		}
		next(writer, request.WithContext(context.WithValue(request.Context(), mobileAuthContextKey{}, auth)))
	}
}

type mobileAuthContextKey struct{}

type mobileAuth struct {
	Admin      bool
	DeviceID   string
	SessionID  int64
	CSRFSHA256 string
}

func authorizeMobileRequest(request *http.Request, deps Dependencies) (mobileAuth, int, string, string, bool) {
	if statusCode, ok := authorizeAdmin(request, deps.AdminToken); ok {
		return mobileAuth{Admin: true}, http.StatusOK, "", "", true
	} else if statusCode == http.StatusForbidden {
		return mobileAuth{}, statusCode, "", "", false
	}
	if deps.Store == nil {
		return mobileAuth{}, http.StatusServiceUnavailable, "mobile_session_store_unavailable", "mobile session store unavailable", false
	}
	cookie, err := request.Cookie(mobileSessionCookieName)
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		return mobileAuth{}, http.StatusUnauthorized, "admin_auth_required", "admin authentication is required", false
	}
	session, err := deps.Store.GetMobileSessionByTokenHash(request.Context(), sqlite.GetMobileSessionByTokenHashParams{TokenSHA256: mobileHash(cookie.Value)})
	if err != nil {
		return mobileAuth{}, http.StatusForbidden, "mobile_session_invalid", "mobile session is invalid or revoked", false
	}
	if request.Method != http.MethodGet && request.Method != http.MethodHead && request.Method != http.MethodOptions {
		csrf := strings.TrimSpace(request.Header.Get("X-Odin-CSRF"))
		if csrf == "" || subtle.ConstantTimeCompare([]byte(mobileHash(csrf)), []byte(session.Session.CSRFSHA256)) != 1 {
			return mobileAuth{}, http.StatusForbidden, "mobile_csrf_required", "mobile browser session csrf token is required", false
		}
	}
	return mobileAuth{
		DeviceID:   session.Device.DeviceID,
		SessionID:  session.Session.ID,
		CSRFSHA256: session.Session.CSRFSHA256,
	}, http.StatusOK, "", "", true
}

func currentMobileAuth(request *http.Request) mobileAuth {
	auth, _ := request.Context().Value(mobileAuthContextKey{}).(mobileAuth)
	return auth
}

func writeMobileJSON(writer http.ResponseWriter, statusCode int, payload any) {
	writer.Header().Set("Cache-Control", "no-store")
	writeJSON(writer, statusCode, payload)
}

type mobileReviewItem struct {
	QueueID        string                  `json:"queue_id"`
	SourceType     string                  `json:"source_type"`
	Source         string                  `json:"source"`
	ObjectID       int64                   `json:"object_id"`
	ObjectKey      string                  `json:"object_key"`
	Title          string                  `json:"title"`
	Status         string                  `json:"status"`
	Reason         string                  `json:"reason,omitempty"`
	ProjectKey     string                  `json:"project_key,omitempty"`
	AllowedActions []string                `json:"allowed_actions"`
	BrowserEvent   string                  `json:"browser_event,omitempty"`
	DeepLink       string                  `json:"deep_link,omitempty"`
	Notification   *mobileNotificationView `json:"notification,omitempty"`
}

type mobileApprovalView struct {
	ApprovalID      int64                   `json:"approval_id"`
	ID              int64                   `json:"id"`
	TaskID          int64                   `json:"task_id"`
	TaskKey         string                  `json:"task_key"`
	ProjectKey      string                  `json:"project_key"`
	Status          string                  `json:"status"`
	RequestedAt     string                  `json:"requested_at"`
	ResolverSupport string                  `json:"resolver_support,omitempty"`
	Title           string                  `json:"title,omitempty"`
	RequestedAction string                  `json:"requested_action,omitempty"`
	RequiredReason  string                  `json:"required_reason,omitempty"`
	RiskLevel       string                  `json:"risk_level,omitempty"`
	SourceObject    string                  `json:"source_object,omitempty"`
	Actions         []string                `json:"actions,omitempty"`
	BrowserEvent    string                  `json:"browser_event,omitempty"`
	DeepLink        string                  `json:"deep_link,omitempty"`
	Notification    *mobileNotificationView `json:"notification,omitempty"`
}

type mobileNotificationView struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	DeepLink  string `json:"deep_link"`
	ObjectKey string `json:"object_key,omitempty"`
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
	Decision   string `json:"decision"`
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

func mobileSummary(ctx context.Context, deps Dependencies, now func() time.Time) (map[string]any, error) {
	status, err := buildStatusPayload(ctx, deps, now)
	if err != nil {
		return nil, err
	}
	runAttempts := status.Counts.ActiveRunAttempts
	intakeItems := 0
	if deps.Store != nil {
		items, err := deps.Store.ListIntakeItems(ctx, sqlite.ListIntakeItemsParams{WorkspaceID: workspaces.DefaultWorkspaceKey})
		if err != nil {
			return nil, err
		}
		intakeItems = len(items)
	}
	if deps.ReadModels != nil {
		runs, err := projections.ListRunSummaryViews(ctx, deps.ReadModels)
		if err != nil {
			return nil, err
		}
		runAttempts = len(runs)
	}
	return map[string]any{
		"generated_at": status.GeneratedAt,
		"readiness": map[string]any{
			"ready":         status.Ready,
			"health_status": status.HealthStatus,
		},
		"runtime": status.Runtime,
		"counts": map[string]int{
			"approvals":           status.Counts.PendingApprovals,
			"review_queue":        status.Counts.ReviewQueueItems,
			"work_items":          status.Counts.WorkItems,
			"open_work_items":     status.Counts.OpenWorkItems,
			"run_attempts":        runAttempts,
			"active_run_attempts": status.Counts.ActiveRunAttempts,
			"automation_triggers": status.Counts.AutomationTriggers,
			"intake_items":        intakeItems,
		},
		"offline": mobileOfflinePolicy(),
	}, nil
}

func mobileOfflinePolicy() map[string]any {
	return map[string]any{
		"mode":              "shell-only",
		"actions_queued":    false,
		"policy_statement":  "Only static app shell files are cached for offline use; runtime data must be fetched from Odin API.",
		"cache_runtime_api": false,
	}
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
	Status         string               `json:"status"`
	SubscriptionID int64                `json:"subscription_id,omitempty"`
	IntakeItem     mobileIntakeItemView `json:"intake_item"`
}

type mobileDeviceRegisterRequest struct {
	DeviceName string `json:"device_name"`
}

type mobileDeviceRegisterResponse struct {
	DeviceID  string `json:"device_id"`
	SessionID int64  `json:"session_id"`
	CSRFToken string `json:"csrf_token"`
	ExpiresAt string `json:"expires_at"`
}

type mobileDeviceRevokeRequest struct {
	Reason string `json:"reason"`
}

type mobileDeviceRevokeResponse struct {
	DeviceID  string `json:"device_id"`
	Status    string `json:"status"`
	RevokedAt string `json:"revoked_at,omitempty"`
}

type mobileNotificationSubscriptionRevokeRequest struct {
	Reason string `json:"reason"`
}

type mobileNotificationSubscriptionRevokeResponse struct {
	SubscriptionID int64  `json:"subscription_id"`
	Status         string `json:"status"`
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

func handleMobileDeviceRegister(writer http.ResponseWriter, request *http.Request, deps Dependencies, now func() time.Time) {
	if deps.Store == nil {
		writeAPIError(writer, http.StatusServiceUnavailable, "mobile_session_store_unavailable", "mobile session store unavailable")
		return
	}
	var body mobileDeviceRegisterRequest
	if err := decodeMobileJSON(writer, request, &body); err != nil {
		writeAPIError(writer, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	deviceID, err := randomMobileToken(18)
	if err != nil {
		writeAPIError(writer, http.StatusInternalServerError, "mobile_session_token_failed", err.Error())
		return
	}
	sessionToken, err := randomMobileToken(32)
	if err != nil {
		writeAPIError(writer, http.StatusInternalServerError, "mobile_session_token_failed", err.Error())
		return
	}
	csrfToken, err := randomMobileToken(32)
	if err != nil {
		writeAPIError(writer, http.StatusInternalServerError, "mobile_session_token_failed", err.Error())
		return
	}
	expiresAt := now().UTC().Add(30 * 24 * time.Hour)
	session, err := deps.Store.CreateMobileDeviceSession(request.Context(), sqlite.CreateMobileDeviceSessionParams{
		DeviceID:    deviceID,
		DeviceName:  strings.TrimSpace(body.DeviceName),
		TokenSHA256: mobileHash(sessionToken),
		CSRFSHA256:  mobileHash(csrfToken),
		ExpiresAt:   expiresAt,
		Actor:       "mobile-api",
	})
	if err != nil {
		writeAPIError(writer, http.StatusBadRequest, "mobile_device_register_failed", err.Error())
		return
	}
	http.SetCookie(writer, &http.Cookie{
		Name:     mobileSessionCookieName,
		Value:    sessionToken,
		Path:     "/",
		Expires:  expiresAt,
		MaxAge:   int((30 * 24 * time.Hour).Seconds()),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	})
	writeMobileJSON(writer, http.StatusCreated, mobileDeviceRegisterResponse{
		DeviceID:  session.Device.DeviceID,
		SessionID: session.Session.ID,
		CSRFToken: csrfToken,
		ExpiresAt: expiresAt.Format(time.RFC3339),
	})
}

func handleMobileDeviceRevoke(writer http.ResponseWriter, request *http.Request, deps Dependencies) {
	if deps.Store == nil {
		writeAPIError(writer, http.StatusServiceUnavailable, "mobile_session_store_unavailable", "mobile session store unavailable")
		return
	}
	deviceID := strings.TrimSpace(request.PathValue("device_id"))
	auth := currentMobileAuth(request)
	if deviceID == "" {
		writeAPIError(writer, http.StatusBadRequest, "mobile_device_id_required", "device id is required")
		return
	}
	if !auth.Admin && auth.DeviceID != deviceID {
		writeAPIError(writer, http.StatusForbidden, "mobile_device_revoke_forbidden", "mobile session may only revoke its own device")
		return
	}
	var body mobileDeviceRevokeRequest
	if err := decodeMobileJSON(writer, request, &body); err != nil {
		writeAPIError(writer, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	device, err := deps.Store.RevokeMobileDevice(request.Context(), sqlite.RevokeMobileDeviceParams{
		DeviceID: deviceID,
		Actor:    "mobile-api",
		Reason:   strings.TrimSpace(body.Reason),
	})
	if err != nil {
		writeAPIError(writer, http.StatusBadRequest, "mobile_device_revoke_failed", err.Error())
		return
	}
	http.SetCookie(writer, &http.Cookie{
		Name:     mobileSessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	})
	writeMobileJSON(writer, http.StatusOK, mobileDeviceRevokeResponse{
		DeviceID:  device.DeviceID,
		Status:    string(device.Status),
		RevokedAt: formatMobileOptionalTime(device.RevokedAt),
	})
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
			Source:         "intake_review",
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
		item := mobileReviewItem{
			QueueID:        fmt.Sprintf("approval:%d", approval.ApprovalID),
			SourceType:     "approval",
			Source:         "approval",
			ObjectID:       approval.ApprovalID,
			ObjectKey:      fmt.Sprintf("approval-%d", approval.ApprovalID),
			Title:          approval.TaskKey,
			Status:         approval.Status,
			Reason:         "approval_required",
			ProjectKey:     approval.ProjectKey,
			AllowedActions: []string{"approve", "deny"},
		}
		if mobileIsBrowserApproval(ctx, deps.Store, approval) {
			item.BrowserEvent = "browser_mutation_approval_required"
			item.DeepLink = mobileApprovalDeepLink(approval.ApprovalID)
			item.Notification = mobileNotification(item.BrowserEvent, item.ObjectKey, item.Title, item.DeepLink)
		}
		items = append(items, item)
	}
	tasks, err := projections.ListTaskStatusViews(ctx, deps.ReadModels)
	if err != nil {
		return nil, err
	}
	taskByID := make(map[int64]projections.TaskStatusView, len(tasks))
	for _, task := range tasks {
		taskByID[task.TaskID] = task
	}
	runs, err := projections.ListRunSummaryViews(ctx, deps.ReadModels)
	if err != nil {
		return nil, err
	}
	browserFailedTasks := make(map[int64]struct{})
	for _, run := range runs {
		if run.Executor != "huginn_browser" {
			continue
		}
		if run.Status == "failed" {
			browserFailedTasks[run.TaskID] = struct{}{}
		}
		artifacts, err := deps.Store.ListRunArtifacts(ctx, sqlite.ListRunArtifactsParams{RunID: run.RunID, ArtifactType: "browser_evidence"})
		if err != nil {
			return nil, err
		}
		for _, artifact := range artifacts {
			task := taskByID[run.TaskID]
			items = append(items, mobileReviewItem{
				QueueID:        fmt.Sprintf("browser-evidence:%d", artifact.ID),
				SourceType:     "browser_evidence",
				Source:         "browser_evidence",
				ObjectID:       artifact.ID,
				ObjectKey:      fmt.Sprintf("browser-evidence-%d", artifact.ID),
				Title:          firstNonEmpty(artifact.Summary, "Browser evidence ready"),
				Status:         "ready",
				Reason:         "browser_evidence_ready",
				ProjectKey:     task.ProjectKey,
				AllowedActions: []string{"review"},
				BrowserEvent:   "browser_evidence_ready",
				DeepLink:       fmt.Sprintf("/app/?queue_id=browser-evidence:%d", artifact.ID),
			})
		}
	}
	for _, task := range tasks {
		if !strings.EqualFold(task.Status, "failed") {
			continue
		}
		if _, ok := browserFailedTasks[task.TaskID]; ok {
			item := mobileReviewItem{
				QueueID:        fmt.Sprintf("browser-run-failed:%d", task.TaskID),
				SourceType:     "browser_run_failed",
				Source:         "browser_run_failed",
				ObjectID:       task.TaskID,
				ObjectKey:      task.TaskKey,
				Title:          task.Title,
				Status:         task.Status,
				Reason:         "retry_allowed",
				ProjectKey:     task.ProjectKey,
				AllowedActions: []string{"retry", "follow-up"},
				BrowserEvent:   "browser_run_failed_retryable",
				DeepLink:       fmt.Sprintf("/app/?queue_id=browser-run-failed:%d", task.TaskID),
			}
			item.Notification = mobileNotification(item.BrowserEvent, item.ObjectKey, item.Title, item.DeepLink)
			items = append(items, item)
			continue
		}
		items = append(items, mobileReviewItem{
			QueueID:        fmt.Sprintf("failed-work:%d", task.TaskID),
			SourceType:     "failed_work",
			Source:         "failed_work",
			ObjectID:       task.TaskID,
			ObjectKey:      task.TaskKey,
			Title:          task.Title,
			Status:         task.Status,
			Reason:         "retry_allowed",
			ProjectKey:     task.ProjectKey,
			AllowedActions: []string{"retry", "follow-up"},
		})
	}
	loginItems, err := mobileBrowserLoginReviewItems(ctx, deps)
	if err != nil {
		return nil, err
	}
	items = append(items, loginItems...)
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
			ApprovalID:      approval.ApprovalID,
			ID:              approval.ApprovalID,
			TaskID:          approval.TaskID,
			TaskKey:         approval.TaskKey,
			ProjectKey:      approval.ProjectKey,
			Status:          approval.Status,
			RequestedAt:     approval.RequestedAt,
			Title:           approval.TaskKey,
			RequestedAction: "runtime action",
			RequiredReason:  "approval_required",
			RiskLevel:       "approval_required",
			SourceObject:    approval.ProjectKey + "/" + approval.TaskKey,
		}
		if approval.Status == "pending" {
			item.Actions = []string{"approve", "deny"}
		}
		if detail, err := service.Detail(ctx, approval.ApprovalID); err == nil {
			item.ResolverSupport = string(detail.ResolverSupport)
		}
		if mobileIsBrowserApproval(ctx, deps.Store, approval) {
			item.BrowserEvent = "browser_mutation_approval_required"
			item.DeepLink = mobileApprovalDeepLink(approval.ApprovalID)
			item.Notification = mobileNotification(item.BrowserEvent, fmt.Sprintf("approval-%d", approval.ApprovalID), approval.TaskKey, item.DeepLink)
		}
		items = append(items, item)
	}
	return items, nil
}

func mobileReviewQueueDetail(ctx context.Context, deps Dependencies, queueID string) ([]mobileReviewItem, error) {
	queueID = strings.TrimSpace(queueID)
	if queueID == "" {
		return []mobileReviewItem{}, nil
	}
	items, err := mobileReviewQueue(ctx, deps)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if item.QueueID == queueID {
			return []mobileReviewItem{item}, nil
		}
	}
	return []mobileReviewItem{}, nil
}

func mobileNotifications(ctx context.Context, deps Dependencies) ([]mobileNotificationView, error) {
	items, err := mobileReviewQueue(ctx, deps)
	if err != nil {
		return nil, err
	}
	notifications := make([]mobileNotificationView, 0)
	for _, item := range items {
		if item.Notification != nil {
			notifications = append(notifications, *item.Notification)
		}
	}
	return notifications, nil
}

func mobileApprovalDeepLink(approvalID int64) string {
	return fmt.Sprintf("/app/?approval_id=%d", approvalID)
}

func mobileNotification(kind string, objectKey string, title string, deepLink string) *mobileNotificationView {
	return &mobileNotificationView{
		ID:        fmt.Sprintf("%s:%s", kind, objectKey),
		Kind:      kind,
		Title:     title,
		Body:      "Odin browser work needs operator review.",
		DeepLink:  deepLink,
		ObjectKey: objectKey,
	}
}

func mobileIsBrowserApproval(ctx context.Context, store *sqlite.Store, approval projections.PendingApprovalView) bool {
	if strings.Contains(strings.ToLower(approval.TaskKey), "browser") || strings.Contains(strings.ToLower(approval.WorkKind), "browser") {
		return true
	}
	events, err := store.ListEvents(ctx, sqlite.ListEventsParams{TaskID: &approval.TaskID})
	if err != nil {
		return false
	}
	for _, event := range events {
		if event.Type != runtimeevents.EventApprovalRequested || event.StreamID != approval.ApprovalID {
			continue
		}
		var payload runtimeevents.ApprovalRequestedPayload
		if err := json.Unmarshal(event.Payload, &payload); err == nil && strings.Contains(strings.ToLower(payload.RequestedBy), "browser") {
			return true
		}
	}
	return false
}

func mobileBrowserLoginReviewItems(ctx context.Context, deps Dependencies) ([]mobileReviewItem, error) {
	if deps.Store == nil {
		return nil, fmt.Errorf("runtime store unavailable")
	}
	sessions, err := deps.Store.ListBrowserSessions(ctx, sqlite.ListBrowserSessionsParams{})
	if err != nil {
		return nil, err
	}
	items := make([]mobileReviewItem, 0)
	for _, session := range sessions {
		requests, err := deps.Store.ListBrowserSessionLoginRequests(ctx, sqlite.ListBrowserSessionLoginRequestsParams{SessionID: session.ID})
		if err != nil {
			return nil, err
		}
		for _, request := range requests {
			if request.Status != sqlite.BrowserSessionLoginRequestStatusRequested {
				continue
			}
			deepLink := fmt.Sprintf("/browser/session/handoff?handoff_id=%s", request.HandoffID)
			item := mobileReviewItem{
				QueueID:        fmt.Sprintf("browser-login:%d", request.ID),
				SourceType:     "browser_attended_login",
				ObjectID:       request.ID,
				ObjectKey:      fmt.Sprintf("browser-login-%d", request.ID),
				Title:          fmt.Sprintf("Attended login required: %s", firstNonEmpty(session.Name, session.Domain)),
				Status:         string(request.Status),
				Reason:         "manual_login_required",
				AllowedActions: []string{"open-handoff"},
				BrowserEvent:   "browser_attended_login_required",
				DeepLink:       deepLink,
			}
			item.Notification = mobileNotification(item.BrowserEvent, item.ObjectKey, item.Title, item.DeepLink)
			items = append(items, item)
		}
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
	body.Action = strings.ToLower(strings.TrimSpace(firstNonEmpty(body.Action, body.Decision)))
	if body.Action != "approve" && body.Action != "approved" && body.Action != "deny" && body.Action != "denied" {
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
		Action:     mobileNormalizeApprovalAction(body.Action),
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
	auth := currentMobileAuth(request)
	if !auth.Admin && auth.DeviceID != "" {
		_ = deps.Store.RecordMobileApprovalEvent(request.Context(), sqlite.RecordMobileApprovalEventParams{
			DeviceID:   auth.DeviceID,
			SessionID:  auth.SessionID,
			ApprovalID: result.Approval.ID,
			Action:     body.Action,
		})
	}
	writeMobileJSON(writer, http.StatusOK, mobileApprovalDecisionResponse{
		ApprovalID:      result.Approval.ID,
		TaskID:          result.Approval.TaskID,
		Status:          result.Approval.Status,
		Action:          mobileNormalizeApprovalAction(body.Action),
		ResolverSupport: string(result.ResolverSupport),
		ResolvedAt:      formatMobileOptionalTime(result.Approval.ResolvedAt),
	})
}

func mobileNormalizeApprovalAction(action string) string {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "approved":
		return "approve"
	case "denied":
		return "deny"
	default:
		return strings.ToLower(strings.TrimSpace(action))
	}
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
	recordMobileIntakeAudit(request.Context(), deps, request, item)
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
	recordMobileIntakeAudit(request.Context(), deps, request, item)
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
	recordMobileIntakeAudit(request.Context(), deps, request, item)
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
	auth := currentMobileAuth(request)
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
	subscriptionID := int64(0)
	if !auth.Admin && auth.DeviceID != "" {
		subscription, err := deps.Store.CreateMobilePushSubscription(request.Context(), sqlite.CreateMobilePushSubscriptionParams{
			DeviceID:       auth.DeviceID,
			EndpointSHA256: mobileHash(endpoint),
			EndpointHost:   endpointHost,
			UserAgent:      strings.TrimSpace(body.UserAgent),
			Platform:       strings.TrimSpace(body.Platform),
		})
		if err != nil {
			writeAPIError(writer, http.StatusBadRequest, "mobile_notification_subscription_failed", err.Error())
			return
		}
		subscriptionID = subscription.ID
	}
	recordMobileIntakeAudit(request.Context(), deps, request, item)
	writeMobileJSON(writer, http.StatusAccepted, mobileNotificationSubscriptionResponse{
		Status:         "accepted_as_intake_metadata",
		SubscriptionID: subscriptionID,
		IntakeItem:     mobileIntakeView(item),
	})
}

func handleMobileNotificationSubscriptionRevoke(writer http.ResponseWriter, request *http.Request, deps Dependencies) {
	if deps.Store == nil {
		writeAPIError(writer, http.StatusServiceUnavailable, "mobile_session_store_unavailable", "mobile session store unavailable")
		return
	}
	auth := currentMobileAuth(request)
	if auth.Admin || auth.DeviceID == "" {
		writeAPIError(writer, http.StatusForbidden, "mobile_session_required", "mobile session is required to revoke a push subscription")
		return
	}
	subscriptionID, err := parsePositivePathID(request.PathValue("subscription_id"), "subscription id")
	if err != nil {
		writeAPIError(writer, http.StatusBadRequest, "invalid_subscription_id", err.Error())
		return
	}
	var body mobileNotificationSubscriptionRevokeRequest
	if err := decodeMobileJSON(writer, request, &body); err != nil {
		writeAPIError(writer, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	subscription, err := deps.Store.RevokeMobilePushSubscription(request.Context(), sqlite.RevokeMobilePushSubscriptionParams{
		DeviceID:       auth.DeviceID,
		SubscriptionID: subscriptionID,
		Actor:          "mobile-api",
		Reason:         strings.TrimSpace(body.Reason),
	})
	if err != nil {
		writeAPIError(writer, http.StatusBadRequest, "mobile_push_subscription_revoke_failed", err.Error())
		return
	}
	writeMobileJSON(writer, http.StatusOK, mobileNotificationSubscriptionRevokeResponse{
		SubscriptionID: subscription.ID,
		Status:         string(subscription.Status),
	})
}

func recordMobileIntakeAudit(ctx context.Context, deps Dependencies, request *http.Request, item sqlite.IntakeItem) {
	auth := currentMobileAuth(request)
	if deps.Store == nil || auth.Admin || auth.DeviceID == "" {
		return
	}
	_ = deps.Store.RecordMobileIntakeEvent(ctx, sqlite.RecordMobileIntakeEventParams{
		DeviceID:     auth.DeviceID,
		SessionID:    auth.SessionID,
		IntakeItemID: item.ID,
		IntakeType:   item.EventKind,
	})
}

type mobileCreateIntakeInput struct {
	Kind                string
	Title               string
	DedupeKey           string
	ProjectKey          string
	SourceFamily        string
	DedupeRecipeVersion string
	Facts               map[string]any
	Now                 func() time.Time
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
	sourceFamily := strings.TrimSpace(input.SourceFamily)
	if sourceFamily == "" {
		sourceFamily = "mobile_api"
	}
	dedupeRecipeVersion := strings.TrimSpace(input.DedupeRecipeVersion)
	if dedupeRecipeVersion == "" {
		dedupeRecipeVersion = "mobile-api-v1"
	}
	now := input.Now().UTC()
	return deps.Store.CreateIntakeItem(ctx, sqlite.CreateIntakeItemParams{
		WorkspaceID:         workspaces.DefaultWorkspaceKey,
		SourceFamily:        sourceFamily,
		ExternalObjectID:    dedupeKey,
		EventKind:           input.Kind,
		Subject:             input.Title,
		DedupeKey:           dedupeKey,
		DedupeRecipeVersion: dedupeRecipeVersion,
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

func randomMobileToken(byteCount int) (string, error) {
	raw := make([]byte, byteCount)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

type mobileRateLimiter struct {
	limit  int
	window time.Duration
	now    func() time.Time
	mu     sync.Mutex
	hits   map[string][]time.Time
}

func newMobileRateLimiter(limit int, window time.Duration, now func() time.Time) *mobileRateLimiter {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &mobileRateLimiter{
		limit:  limit,
		window: window,
		now:    now,
		hits:   make(map[string][]time.Time),
	}
}

func (limiter *mobileRateLimiter) allow(key string) bool {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	current := limiter.now().UTC()
	cutoff := current.Add(-limiter.window)
	existing := limiter.hits[key]
	kept := existing[:0]
	for _, hit := range existing {
		if hit.After(cutoff) {
			kept = append(kept, hit)
		}
	}
	if len(kept) >= limiter.limit {
		limiter.hits[key] = kept
		return false
	}
	limiter.hits[key] = append(kept, current)
	return true
}

func mobileRateLimitKey(request *http.Request) string {
	auth := currentMobileAuth(request)
	if auth.DeviceID != "" {
		return "device:" + auth.DeviceID
	}
	return "admin:" + request.RemoteAddr
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
