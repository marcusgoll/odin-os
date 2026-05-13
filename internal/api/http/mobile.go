package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"odin-os/internal/core/workspaces"
	approvalsvc "odin-os/internal/runtime/approvals"
	"odin-os/internal/runtime/projections"
	"odin-os/internal/store/sqlite"
)

type mobileSummary struct {
	GeneratedAt string                 `json:"generated_at"`
	Readiness   mobileReadiness        `json:"readiness"`
	Runtime     runtimeStatus          `json:"runtime"`
	Counts      mobileCounts           `json:"counts"`
	Links       map[string]string      `json:"links"`
	Offline     mobileOfflinePolicy    `json:"offline"`
	Warnings    []mobileRuntimeWarning `json:"warnings,omitempty"`
}

type mobileReadiness struct {
	Ready        bool   `json:"ready"`
	HealthStatus string `json:"health_status"`
}

type mobileCounts struct {
	Approvals          int `json:"approvals"`
	ReviewQueue        int `json:"review_queue"`
	WorkItems          int `json:"work_items"`
	OpenWorkItems      int `json:"open_work_items"`
	RunAttempts        int `json:"run_attempts"`
	ActiveRunAttempts  int `json:"active_run_attempts"`
	AutomationTriggers int `json:"automation_triggers"`
	IntakeItems        int `json:"intake_items"`
	TaskIntakeEvidence int `json:"task_intake_evidence"`
}

type mobileOfflinePolicy struct {
	Mode            string `json:"mode"`
	ActionsQueued   bool   `json:"actions_queued"`
	PolicyStatement string `json:"policy_statement"`
}

type mobileRuntimeWarning struct {
	Source  string `json:"source"`
	Message string `json:"message"`
}

type mobileApprovalResponse struct {
	GeneratedAt string               `json:"generated_at"`
	Count       int                  `json:"count"`
	Approvals   []mobileApprovalView `json:"approvals"`
	Items       []mobileApprovalView `json:"items"`
}

type mobileApprovalDetailResponse struct {
	GeneratedAt string             `json:"generated_at"`
	Approval    mobileApprovalView `json:"approval"`
}

type mobileApprovalDecisionResponse struct {
	Approval mobileApprovalView `json:"approval"`
	Result   string             `json:"result"`
	Summary  string             `json:"summary"`
}

type mobileApprovalView struct {
	ID                  int64    `json:"id"`
	Title               string   `json:"title"`
	Status              string   `json:"status"`
	RiskLevel           string   `json:"risk_level"`
	SourceObject        string   `json:"source_object"`
	RequestedAction     string   `json:"requested_action"`
	RequiredReason      string   `json:"required_reason"`
	EvidenceContext     []string `json:"evidence_context"`
	Consequences        []string `json:"consequences"`
	ExpiresAt           string   `json:"expires_at"`
	PolicySnapshotHash  string   `json:"policy_snapshot_hash"`
	RuntimeSnapshotHash string   `json:"runtime_snapshot_hash"`
	AuditTrailPreview   []string `json:"audit_trail_preview"`
	Actions             []string `json:"actions"`
	ConfirmationPrompt  string   `json:"confirmation_prompt,omitempty"`
	TaskID              int64    `json:"task_id"`
	TaskKey             string   `json:"task_key"`
	ProjectKey          string   `json:"project_key,omitempty"`
	ResolverSupport     string   `json:"resolver_support"`
	DecisionBy          string   `json:"decision_by,omitempty"`
	DecisionReason      string   `json:"decision_reason,omitempty"`
}

type mobileApprovalDecisionRequest struct {
	Action                      string `json:"action"`
	Reason                      string `json:"reason"`
	Actor                       string `json:"actor"`
	ConfirmationText            string `json:"confirmation_text"`
	ExpectedPolicySnapshotHash  string `json:"expected_policy_snapshot_hash"`
	ExpectedRuntimeSnapshotHash string `json:"expected_runtime_snapshot_hash"`
}

type mobileReviewResponse struct {
	GeneratedAt string             `json:"generated_at"`
	Count       int                `json:"count"`
	Items       []mobileReviewItem `json:"items"`
}

type mobileReviewItem struct {
	QueueID        string   `json:"queue_id"`
	Source         string   `json:"source"`
	ObjectID       int64    `json:"object_id"`
	TaskID         int64    `json:"task_id,omitempty"`
	ProjectKey     string   `json:"project_key,omitempty"`
	WorkItemKey    string   `json:"work_item_key,omitempty"`
	Title          string   `json:"title"`
	Status         string   `json:"status"`
	RequestedAt    string   `json:"requested_at,omitempty"`
	Reason         string   `json:"reason,omitempty"`
	AllowedActions []string `json:"allowed_actions"`
}

type mobileWorkResponse struct {
	GeneratedAt string           `json:"generated_at"`
	WorkItems   []mobileWorkItem `json:"work_items"`
	Runs        []mobileRunItem  `json:"runs"`
}

type mobileWorkItem struct {
	TaskID                int64  `json:"task_id"`
	ProjectKey            string `json:"project_key"`
	WorkItemKey           string `json:"work_item_key"`
	Title                 string `json:"title"`
	Status                string `json:"status"`
	Scope                 string `json:"scope"`
	WorkKind              string `json:"work_kind"`
	ExecutionIntent       string `json:"execution_intent"`
	ExecutionIntentSource string `json:"execution_intent_source"`
	CurrentRunID          *int64 `json:"current_run_id,omitempty"`
	CurrentRunStatus      string `json:"current_run_status,omitempty"`
	BlockedReason         string `json:"blocked_reason,omitempty"`
	RetryCount            int    `json:"retry_count"`
	MaxAttempts           int    `json:"max_attempts"`
	LastError             string `json:"last_error,omitempty"`
	NextEligibleAt        string `json:"next_eligible_at,omitempty"`
}

type mobileRunItem struct {
	RunID                 int64   `json:"run_id"`
	TaskID                int64   `json:"task_id"`
	TaskKey               string  `json:"task_key"`
	ProjectKey            string  `json:"project_key"`
	Executor              string  `json:"executor"`
	Status                string  `json:"status"`
	Attempt               int     `json:"attempt"`
	StartedAt             string  `json:"started_at"`
	FinishedAt            *string `json:"finished_at,omitempty"`
	ExecutionIntent       string  `json:"execution_intent"`
	ExecutionIntentSource string  `json:"execution_intent_source"`
}

type mobileInboxResponse struct {
	GeneratedAt string                               `json:"generated_at"`
	RawItems    []mobileIntakeItem                   `json:"raw_items"`
	LinkedItems []projections.TaskIntakeEvidenceView `json:"linked_items"`
	Capture     mobileCapturePolicy                  `json:"capture"`
}

type mobileIntakeItem struct {
	ID                int64  `json:"id"`
	WorkspaceID       string `json:"workspace_id"`
	SourceFamily      string `json:"source_family"`
	ExternalObjectID  string `json:"external_object_id,omitempty"`
	EventKind         string `json:"event_kind"`
	Subject           string `json:"subject"`
	Status            string `json:"status"`
	Scope             string `json:"scope"`
	ScopeKey          string `json:"scope_key"`
	Summary           string `json:"summary,omitempty"`
	SuppressionReason string `json:"suppression_reason,omitempty"`
	RoutingNotes      string `json:"routing_notes,omitempty"`
	ReceivedAt        string `json:"received_at"`
	CreatedAt         string `json:"created_at"`
	CanonicalIntakeID *int64 `json:"canonical_intake_item_id,omitempty"`
	GoalID            *int64 `json:"goal_id,omitempty"`
}

type mobileCapturePolicy struct {
	Enabled         bool   `json:"enabled"`
	Endpoint        string `json:"endpoint,omitempty"`
	PolicyStatement string `json:"policy_statement"`
}

type mobileSettingsResponse struct {
	GeneratedAt   string              `json:"generated_at"`
	APIBase       string              `json:"api_base"`
	AppBase       string              `json:"app_base"`
	AdminActions  mobileAdminPolicy   `json:"admin_actions"`
	Offline       mobileOfflinePolicy `json:"offline"`
	RuntimeSource string              `json:"runtime_source"`
	Endpoints     []string            `json:"endpoints"`
}

type mobileAdminPolicy struct {
	Enabled         bool   `json:"enabled"`
	PolicyStatement string `json:"policy_statement"`
}

func registerMobileHandlers(mux *http.ServeMux, deps Dependencies, now func() time.Time) {
	mux.HandleFunc("GET /mobile/summary", func(writer http.ResponseWriter, request *http.Request) {
		payload, err := buildMobileSummary(request.Context(), deps, now)
		if err != nil {
			writeAPIError(writer, http.StatusServiceUnavailable, "mobile_summary_unavailable", err.Error())
			return
		}
		writeJSON(writer, http.StatusOK, payload)
	})
	mux.HandleFunc("GET /mobile/approvals", func(writer http.ResponseWriter, request *http.Request) {
		approvals, err := mobileApprovalViews(request.Context(), deps)
		if err != nil {
			writeAPIError(writer, http.StatusServiceUnavailable, "mobile_approvals_unavailable", err.Error())
			return
		}
		writeJSON(writer, http.StatusOK, mobileApprovalResponse{GeneratedAt: now().UTC().Format(time.RFC3339), Count: len(approvals), Approvals: approvals, Items: approvals})
	})
	mux.HandleFunc("GET /mobile/approvals/{approval_id}", func(writer http.ResponseWriter, request *http.Request) {
		view, ok := handleMobileApprovalDetail(writer, request, deps)
		if !ok {
			return
		}
		writeJSON(writer, http.StatusOK, mobileApprovalDetailResponse{GeneratedAt: now().UTC().Format(time.RFC3339), Approval: view})
	})
	mux.HandleFunc("POST /mobile/approvals/{approval_id}/decision", func(writer http.ResponseWriter, request *http.Request) {
		handleMobileApprovalDecision(writer, request, deps)
	})
	mux.HandleFunc("GET /mobile/review", func(writer http.ResponseWriter, request *http.Request) {
		items, err := mobileReviewItems(request.Context(), deps)
		if err != nil {
			writeAPIError(writer, http.StatusServiceUnavailable, "mobile_review_unavailable", err.Error())
			return
		}
		writeJSON(writer, http.StatusOK, mobileReviewResponse{GeneratedAt: now().UTC().Format(time.RFC3339), Count: len(items), Items: items})
	})
	mux.HandleFunc("GET /mobile/work", func(writer http.ResponseWriter, request *http.Request) {
		work, runs, err := mobileWorkAndRuns(request.Context(), deps)
		if err != nil {
			writeAPIError(writer, http.StatusServiceUnavailable, "mobile_work_unavailable", err.Error())
			return
		}
		writeJSON(writer, http.StatusOK, mobileWorkResponse{GeneratedAt: now().UTC().Format(time.RFC3339), WorkItems: work, Runs: runs})
	})
	mux.HandleFunc("GET /mobile/inbox", func(writer http.ResponseWriter, request *http.Request) {
		raw, linked, err := mobileInboxItems(request.Context(), deps)
		if err != nil {
			writeAPIError(writer, http.StatusServiceUnavailable, "mobile_inbox_unavailable", err.Error())
			return
		}
		writeJSON(writer, http.StatusOK, mobileInboxResponse{
			GeneratedAt: now().UTC().Format(time.RFC3339),
			RawItems:    raw,
			LinkedItems: linked,
			Capture: mobileCapturePolicy{
				Enabled:         false,
				PolicyStatement: "PWA capture is read-only in this build; no offline or background action queue is designed.",
			},
		})
	})
	mux.HandleFunc("GET /mobile/settings", func(writer http.ResponseWriter, request *http.Request) {
		writeJSON(writer, http.StatusOK, mobileSettingsResponse{
			GeneratedAt:   now().UTC().Format(time.RFC3339),
			APIBase:       "/mobile",
			AppBase:       "/app/",
			RuntimeSource: "odin serve internal/api/http",
			AdminActions: mobileAdminPolicy{
				Enabled:         strings.TrimSpace(deps.AdminToken) != "",
				PolicyStatement: "Mutations require explicit Odin API authorization; this PWA shell does not batch or queue actions.",
			},
			Offline: mobileOfflinePolicy{
				Mode:            "shell-only",
				ActionsQueued:   false,
				PolicyStatement: "Only static app shell files are cached for offline use; runtime data must be fetched from Odin API.",
			},
			Endpoints: []string{
				"/mobile/summary",
				"/mobile/approvals",
				"/mobile/review",
				"/mobile/work",
				"/mobile/inbox",
				"/mobile/settings",
			},
		})
	})
}

func buildMobileSummary(ctx context.Context, deps Dependencies, now func() time.Time) (mobileSummary, error) {
	status, err := buildStatusPayload(ctx, deps, now)
	if err != nil {
		return mobileSummary{}, err
	}
	counts, warnings, err := mobileRuntimeCounts(ctx, deps)
	if err != nil {
		return mobileSummary{}, err
	}
	return mobileSummary{
		GeneratedAt: status.GeneratedAt,
		Readiness: mobileReadiness{
			Ready:        status.Ready,
			HealthStatus: status.HealthStatus,
		},
		Runtime:  status.Runtime,
		Counts:   counts,
		Warnings: warnings,
		Links: map[string]string{
			"approvals": "/mobile/approvals",
			"review":    "/mobile/review",
			"work":      "/mobile/work",
			"inbox":     "/mobile/inbox",
			"settings":  "/mobile/settings",
		},
		Offline: mobileOfflinePolicy{
			Mode:            "shell-only",
			ActionsQueued:   false,
			PolicyStatement: "Only static app shell files are cached for offline use; runtime data must be fetched from Odin API.",
		},
	}, nil
}

func mobileRuntimeCounts(ctx context.Context, deps Dependencies) (mobileCounts, []mobileRuntimeWarning, error) {
	var counts mobileCounts
	var warnings []mobileRuntimeWarning
	if deps.ReadModels == nil {
		return counts, append(warnings, mobileRuntimeWarning{Source: "read_models", Message: "read models unavailable"}), nil
	}

	workItems, err := projections.ListTaskStatusViews(ctx, deps.ReadModels)
	if err != nil {
		return counts, warnings, err
	}
	runs, err := projections.ListRunSummaryViews(ctx, deps.ReadModels)
	if err != nil {
		return counts, warnings, err
	}
	activeRuns, err := projections.ListActiveRunViews(ctx, deps.ReadModels)
	if err != nil {
		return counts, warnings, err
	}
	approvals, err := mobilePendingApprovals(ctx, deps)
	if err != nil {
		return counts, warnings, err
	}
	reviewItems, err := mobileReviewItems(ctx, deps)
	if err != nil {
		return counts, warnings, err
	}
	linkedIntake, err := projections.ListTaskIntakeEvidenceViews(ctx, deps.ReadModels, workspaces.DefaultWorkspaceKey)
	if err != nil {
		return counts, warnings, err
	}
	counts = mobileCounts{
		Approvals:          len(approvals),
		ReviewQueue:        len(reviewItems),
		WorkItems:          len(workItems),
		OpenWorkItems:      countOpenMobileWorkItems(workItems),
		RunAttempts:        len(runs),
		ActiveRunAttempts:  len(activeRuns),
		TaskIntakeEvidence: len(linkedIntake),
	}
	if deps.Store == nil {
		warnings = append(warnings, mobileRuntimeWarning{Source: "store", Message: "store unavailable for intake item and trigger counts"})
		return counts, warnings, nil
	}
	triggers, err := deps.Store.ListAutomationTriggers(ctx, sqlite.ListAutomationTriggersParams{WorkspaceID: workspaces.DefaultWorkspaceKey})
	if err != nil {
		return counts, warnings, err
	}
	rawIntake, err := deps.Store.ListIntakeItems(ctx, sqlite.ListIntakeItemsParams{WorkspaceID: workspaces.DefaultWorkspaceKey})
	if err != nil {
		return counts, warnings, err
	}
	counts.AutomationTriggers = len(triggers)
	counts.IntakeItems = len(rawIntake)
	return counts, warnings, nil
}

func mobilePendingApprovals(ctx context.Context, deps Dependencies) ([]projections.PendingApprovalView, error) {
	if deps.ReadModels == nil {
		return []projections.PendingApprovalView{}, nil
	}
	return projections.ListPendingApprovalViews(ctx, deps.ReadModels)
}

func mobileApprovalViews(ctx context.Context, deps Dependencies) ([]mobileApprovalView, error) {
	if deps.Store == nil {
		return []mobileApprovalView{}, nil
	}
	pending, err := mobilePendingApprovals(ctx, deps)
	if err != nil {
		return nil, err
	}
	service := approvalsvc.Service{Store: deps.Store}
	views := make([]mobileApprovalView, 0, len(pending))
	for _, item := range pending {
		detail, err := service.Detail(ctx, item.ApprovalID)
		if err != nil {
			return nil, err
		}
		view, err := buildMobileApprovalView(ctx, deps.Store, detail)
		if err != nil {
			return nil, err
		}
		view.ProjectKey = item.ProjectKey
		views = append(views, view)
	}
	return views, nil
}

func handleMobileApprovalDetail(writer http.ResponseWriter, request *http.Request, deps Dependencies) (mobileApprovalView, bool) {
	if deps.Store == nil {
		writeAPIError(writer, http.StatusServiceUnavailable, "mobile_approval_unavailable", "approval store unavailable")
		return mobileApprovalView{}, false
	}
	approvalID, err := strconv.ParseInt(request.PathValue("approval_id"), 10, 64)
	if err != nil || approvalID <= 0 {
		writeAPIError(writer, http.StatusBadRequest, "invalid_approval_id", "approval id must be a positive integer")
		return mobileApprovalView{}, false
	}
	detail, err := (approvalsvc.Service{Store: deps.Store}).Detail(request.Context(), approvalID)
	if err != nil {
		writeAPIError(writer, http.StatusNotFound, "approval_not_found", err.Error())
		return mobileApprovalView{}, false
	}
	view, err := buildMobileApprovalView(request.Context(), deps.Store, detail)
	if err != nil {
		writeAPIError(writer, http.StatusServiceUnavailable, "mobile_approval_unavailable", err.Error())
		return mobileApprovalView{}, false
	}
	return view, true
}

func handleMobileApprovalDecision(writer http.ResponseWriter, request *http.Request, deps Dependencies) {
	if statusCode, ok := authorizeAdmin(request, deps.AdminToken); !ok {
		writeAdminAuthorizationError(writer, statusCode)
		return
	}
	view, ok := handleMobileApprovalDetail(writer, request, deps)
	if !ok {
		return
	}
	var payload mobileApprovalDecisionRequest
	if err := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 1<<20)).Decode(&payload); err != nil {
		writeAPIError(writer, http.StatusBadRequest, "invalid_mobile_approval_decision", err.Error())
		return
	}
	payload.Action = strings.ToLower(strings.TrimSpace(payload.Action))
	payload.Reason = strings.TrimSpace(payload.Reason)
	payload.Actor = strings.TrimSpace(payload.Actor)
	if payload.Actor == "" {
		payload.Actor = "operator"
	}
	if payload.Reason == "" {
		writeAPIError(writer, http.StatusBadRequest, "approval_reason_required", "decision reason is required")
		return
	}
	if payload.ExpectedPolicySnapshotHash != "" && payload.ExpectedPolicySnapshotHash != view.PolicySnapshotHash {
		writeAPIError(writer, http.StatusConflict, "stale_approval", "policy snapshot changed since approval was displayed")
		return
	}
	if payload.ExpectedRuntimeSnapshotHash != "" && payload.ExpectedRuntimeSnapshotHash != view.RuntimeSnapshotHash {
		writeAPIError(writer, http.StatusConflict, "stale_approval", "runtime snapshot changed since approval was displayed")
		return
	}
	if payload.Action == "approve" && mobileApprovalRequiresConfirmation(view) {
		want := fmt.Sprintf("APPROVE %d", view.ID)
		if strings.TrimSpace(payload.ConfirmationText) != want {
			writeAPIError(writer, http.StatusConflict, "explicit_confirmation_required", "approval requires explicit confirmation text: "+want)
			return
		}
	}
	result, err := (approvalsvc.Service{Store: deps.Store}).Resolve(request.Context(), approvalsvc.ResolveParams{
		ApprovalID: view.ID,
		Action:     payload.Action,
		DecisionBy: payload.Actor,
		Reason:     payload.Reason,
	})
	if err != nil {
		switch {
		case errors.Is(err, approvalsvc.ErrStaleApproval):
			writeAPIError(writer, http.StatusConflict, "stale_approval", err.Error())
		case errors.Is(err, approvalsvc.ErrUnsupportedResolver):
			writeAPIError(writer, http.StatusConflict, "unsupported_approval_resolver", err.Error())
		default:
			writeAPIError(writer, http.StatusBadRequest, "approval_decision_failed", err.Error())
		}
		return
	}
	updated, err := (approvalsvc.Service{Store: deps.Store}).Detail(request.Context(), result.Approval.ID)
	if err != nil {
		writeAPIError(writer, http.StatusServiceUnavailable, "approval_detail_unavailable", err.Error())
		return
	}
	updatedView, err := buildMobileApprovalView(request.Context(), deps.Store, updated)
	if err != nil {
		writeAPIError(writer, http.StatusServiceUnavailable, "approval_detail_unavailable", err.Error())
		return
	}
	receipt, err := approvalsvc.FormatReceipt(result)
	if err != nil {
		writeAPIError(writer, http.StatusServiceUnavailable, "approval_receipt_unavailable", err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, mobileApprovalDecisionResponse{
		Approval: updatedView,
		Result:   strings.TrimPrefix(receipt.Line, fmt.Sprintf("approval=%d status=resolved result=", result.Approval.ID)),
		Summary:  strings.TrimPrefix(receipt.Summary, "summary="),
	})
}

func buildMobileApprovalView(ctx context.Context, store *sqlite.Store, detail approvalsvc.Detail) (mobileApprovalView, error) {
	projectKey := ""
	if detail.Task.ProjectID > 0 {
		project, err := store.GetProject(ctx, detail.Task.ProjectID)
		if err != nil {
			return mobileApprovalView{}, err
		}
		projectKey = project.Key
	}
	auditTrail, err := mobileApprovalAuditTrail(ctx, store, detail.Task.ID)
	if err != nil {
		return mobileApprovalView{}, err
	}
	risk := mobileApprovalRiskLevel(detail.Task)
	view := mobileApprovalView{
		ID:                  detail.Approval.ID,
		Title:               fallbackString(detail.Task.Title, detail.Task.Key),
		Status:              detail.Approval.Status,
		RiskLevel:           risk,
		SourceObject:        fallbackString(projectKey+"/"+detail.Task.Key, detail.Task.Key),
		RequestedAction:     fallbackString(detail.Task.ActionKey, detail.Task.WorkKind, detail.Task.ExecutionIntent, "runtime action"),
		RequiredReason:      fallbackString(detail.Task.BlockedReason, detail.Task.TerminalReason, "approval_required"),
		EvidenceContext:     mobileApprovalEvidence(detail.Task),
		Consequences:        mobileApprovalConsequences(risk, detail.Task),
		ExpiresAt:           "",
		PolicySnapshotHash:  detail.Approval.PolicySnapshotHash,
		RuntimeSnapshotHash: detail.Approval.RuntimeSnapshotHash,
		AuditTrailPreview:   auditTrail,
		Actions:             mobileApprovalActions(detail.Approval.Status),
		TaskID:              detail.Task.ID,
		TaskKey:             detail.Task.Key,
		ProjectKey:          projectKey,
		ResolverSupport:     string(detail.ResolverSupport),
		DecisionBy:          detail.Approval.DecisionBy,
		DecisionReason:      detail.Approval.Reason,
	}
	if mobileApprovalRequiresConfirmation(view) {
		view.ConfirmationPrompt = fmt.Sprintf("APPROVE %d", view.ID)
	}
	return view, nil
}

func mobileApprovalActions(status string) []string {
	if status == "pending" {
		return []string{"approve", "deny", "clarify"}
	}
	return []string{}
}

func mobileApprovalRequiresConfirmation(view mobileApprovalView) bool {
	return view.RiskLevel == "high" || view.RiskLevel == "critical"
}

func mobileApprovalRiskLevel(task sqlite.Task) string {
	text := strings.ToLower(strings.Join([]string{task.ActionKey, task.WorkKind, task.ExecutionIntent, task.Title, task.Summary}, " "))
	switch {
	case strings.Contains(text, "critical") || strings.Contains(text, "delete") || strings.Contains(text, "force") || strings.Contains(text, "production"):
		return "critical"
	case strings.Contains(text, "external_mutation") || strings.Contains(text, "transfer") || strings.Contains(text, "deploy") || strings.Contains(text, "mutate"):
		return "high"
	case strings.Contains(text, "write") || strings.Contains(text, "publish"):
		return "medium"
	default:
		return "low"
	}
}

func mobileApprovalEvidence(task sqlite.Task) []string {
	values := []string{
		"task=" + task.Key,
		"status=" + task.Status,
		"blocked_reason=" + fallbackString(task.BlockedReason, "approval_required"),
	}
	if task.ExecutionIntent != "" {
		values = append(values, "execution_intent="+task.ExecutionIntent)
	}
	if task.Summary != "" {
		values = append(values, "summary="+task.Summary)
	}
	if strings.TrimSpace(task.ArtifactsJSON) != "" && strings.TrimSpace(task.ArtifactsJSON) != "[]" {
		values = append(values, "artifacts="+task.ArtifactsJSON)
	}
	return values
}

func mobileApprovalConsequences(risk string, task sqlite.Task) []string {
	consequences := []string{
		"approve: continue the blocked Odin work item through its registered resolver",
		"deny: keep the work blocked and preserve the denial reason",
		"clarify: keep the work blocked and route the question back to the review/work queue",
	}
	if risk == "high" || risk == "critical" {
		consequences = append(consequences, "approval may allow external or irreversible side effects for "+task.Key)
	}
	return consequences
}

func mobileApprovalAuditTrail(ctx context.Context, store *sqlite.Store, taskID int64) ([]string, error) {
	events, err := store.ListEvents(ctx, sqlite.ListEventsParams{TaskID: &taskID})
	if err != nil {
		return nil, err
	}
	start := len(events) - 5
	if start < 0 {
		start = 0
	}
	trail := make([]string, 0, len(events)-start)
	for _, event := range events[start:] {
		trail = append(trail, fmt.Sprintf("%s %s", event.OccurredAt.UTC().Format(time.RFC3339), event.Type))
	}
	if len(trail) == 0 {
		trail = append(trail, "no audit events recorded yet")
	}
	return trail, nil
}

func mobileReviewItems(ctx context.Context, deps Dependencies) ([]mobileReviewItem, error) {
	items := make([]mobileReviewItem, 0)
	approvals, err := mobilePendingApprovals(ctx, deps)
	if err != nil {
		return nil, err
	}
	for _, approval := range approvals {
		items = append(items, mobileReviewItem{
			QueueID:        "approval:" + strconv.FormatInt(approval.ApprovalID, 10),
			Source:         "approval",
			ObjectID:       approval.ApprovalID,
			TaskID:         approval.TaskID,
			ProjectKey:     approval.ProjectKey,
			WorkItemKey:    approval.TaskKey,
			Title:          approval.TaskKey,
			Status:         approval.Status,
			RequestedAt:    approval.RequestedAt,
			Reason:         "pending approval request",
			AllowedActions: []string{"inspect"},
		})
	}

	if deps.ReadModels != nil {
		workItems, err := projections.ListTaskStatusViews(ctx, deps.ReadModels)
		if err != nil {
			return nil, err
		}
		for _, item := range workItems {
			if item.Status == "blocked" && item.BlockedReason == "clarification_requested" {
				items = append(items, mobileReviewItem{
					QueueID:        "work-clarification:" + strconv.FormatInt(item.TaskID, 10),
					Source:         "work_clarification",
					ObjectID:       item.TaskID,
					TaskID:         item.TaskID,
					ProjectKey:     item.ProjectKey,
					WorkItemKey:    item.TaskKey,
					Title:          item.Title,
					Status:         item.Status,
					Reason:         "clarification_requested",
					AllowedActions: []string{"inspect"},
				})
				continue
			}
			if item.Status != "failed" {
				continue
			}
			if item.MaxAttempts > 0 && item.RetryCount >= item.MaxAttempts {
				continue
			}
			items = append(items, mobileReviewItem{
				QueueID:        "failed-work:" + strconv.FormatInt(item.TaskID, 10),
				Source:         "failed_work",
				ObjectID:       item.TaskID,
				TaskID:         item.TaskID,
				ProjectKey:     item.ProjectKey,
				WorkItemKey:    item.TaskKey,
				Title:          item.Title,
				Status:         item.Status,
				Reason:         strings.TrimSpace(item.LastError),
				AllowedActions: []string{"inspect"},
			})
		}
	}

	if deps.Store == nil {
		return items, nil
	}
	rawIntake, err := deps.Store.ListIntakeItems(ctx, sqlite.ListIntakeItemsParams{WorkspaceID: workspaces.DefaultWorkspaceKey})
	if err != nil {
		return nil, err
	}
	for _, item := range rawIntake {
		if !mobileIntakeNeedsReview(item.Status) {
			continue
		}
		items = append(items, mobileReviewItem{
			QueueID:        "intake-review:" + strconv.FormatInt(item.ID, 10),
			Source:         "intake",
			ObjectID:       item.ID,
			ProjectKey:     item.ScopeKey,
			Title:          fallbackString(item.Subject, item.Summary, item.ExternalObjectID),
			Status:         item.Status,
			RequestedAt:    formatMobileTime(item.ReceivedAt),
			Reason:         item.RoutingNotes,
			AllowedActions: []string{"inspect"},
		})
	}
	return items, nil
}

func mobileWorkAndRuns(ctx context.Context, deps Dependencies) ([]mobileWorkItem, []mobileRunItem, error) {
	if deps.ReadModels == nil {
		return []mobileWorkItem{}, []mobileRunItem{}, nil
	}
	taskViews, err := projections.ListTaskStatusViews(ctx, deps.ReadModels)
	if err != nil {
		return nil, nil, err
	}
	workItems := make([]mobileWorkItem, 0, len(taskViews))
	for _, view := range taskViews {
		workItems = append(workItems, mobileWorkItem{
			TaskID:                view.TaskID,
			ProjectKey:            view.ProjectKey,
			WorkItemKey:           view.TaskKey,
			Title:                 view.Title,
			Status:                view.Status,
			Scope:                 view.Scope,
			WorkKind:              view.WorkKind,
			ExecutionIntent:       view.ExecutionIntent,
			ExecutionIntentSource: view.ExecutionIntentSource,
			CurrentRunID:          view.CurrentRunID,
			CurrentRunStatus:      view.CurrentRunStatus,
			BlockedReason:         view.BlockedReason,
			RetryCount:            view.RetryCount,
			MaxAttempts:           view.MaxAttempts,
			LastError:             view.LastError,
			NextEligibleAt:        view.NextEligibleAt,
		})
	}
	runViews, err := projections.ListRunSummaryViews(ctx, deps.ReadModels)
	if err != nil {
		return nil, nil, err
	}
	runs := make([]mobileRunItem, 0, len(runViews))
	for _, view := range runViews {
		runs = append(runs, mobileRunItem{
			RunID:                 view.RunID,
			TaskID:                view.TaskID,
			TaskKey:               view.TaskKey,
			ProjectKey:            view.ProjectKey,
			Executor:              view.Executor,
			Status:                view.Status,
			Attempt:               view.Attempt,
			StartedAt:             view.StartedAt,
			FinishedAt:            view.FinishedAt,
			ExecutionIntent:       view.ExecutionIntent,
			ExecutionIntentSource: view.ExecutionIntentSource,
		})
	}
	return workItems, runs, nil
}

func mobileInboxItems(ctx context.Context, deps Dependencies) ([]mobileIntakeItem, []projections.TaskIntakeEvidenceView, error) {
	linked := []projections.TaskIntakeEvidenceView{}
	if deps.ReadModels != nil {
		views, err := projections.ListTaskIntakeEvidenceViews(ctx, deps.ReadModels, workspaces.DefaultWorkspaceKey)
		if err != nil {
			return nil, nil, err
		}
		linked = views
	}
	if deps.Store == nil {
		return []mobileIntakeItem{}, linked, nil
	}
	items, err := deps.Store.ListIntakeItems(ctx, sqlite.ListIntakeItemsParams{WorkspaceID: workspaces.DefaultWorkspaceKey})
	if err != nil {
		return nil, nil, err
	}
	raw := make([]mobileIntakeItem, 0, len(items))
	for _, item := range items {
		raw = append(raw, mobileIntakeItem{
			ID:                item.ID,
			WorkspaceID:       item.WorkspaceID,
			SourceFamily:      item.SourceFamily,
			ExternalObjectID:  item.ExternalObjectID,
			EventKind:         item.EventKind,
			Subject:           item.Subject,
			Status:            item.Status,
			Scope:             item.Scope,
			ScopeKey:          item.ScopeKey,
			Summary:           item.Summary,
			SuppressionReason: item.SuppressionReason,
			RoutingNotes:      item.RoutingNotes,
			ReceivedAt:        formatMobileTime(item.ReceivedAt),
			CreatedAt:         formatMobileTime(item.CreatedAt),
			CanonicalIntakeID: item.CanonicalIntakeItemID,
			GoalID:            item.GoalID,
		})
	}
	return raw, linked, nil
}

func countOpenMobileWorkItems(items []projections.TaskStatusView) int {
	open := 0
	for _, item := range items {
		switch item.Status {
		case "done", "completed", "cancelled", "archived":
			continue
		default:
			open++
		}
	}
	return open
}

func mobileIntakeNeedsReview(status string) bool {
	switch strings.TrimSpace(status) {
	case "review_required", "needs_clarification", "approval_required":
		return true
	default:
		return false
	}
}

func formatMobileTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func fallbackString(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return "unlabeled runtime item"
}
