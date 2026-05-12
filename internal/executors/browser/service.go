package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strings"

	"odin-os/internal/adapters/huginnbrowser"
	"odin-os/internal/store/sqlite"
)

const (
	EvidenceType                 = "browser_readonly"
	EvidenceTypeApprovalRequired = "browser_approval_required"
	EvidenceTypeFailed           = "browser_failed"
	WorkArtifactType             = "huginn_browser_plugin_evidence"
	MaxPagesLimit                = 20
	MaxDurationSecondsLimit      = 300
	defaultEvidenceCreatedBy     = "browser_executor"
	RiskReadOnly                 = "read_only"
	RiskExternalMutation         = "external_mutation"
)

type ReadOnlyTask struct {
	GoalID             int64                       `json:"goal_id"`
	TaskID             int64                       `json:"task_id,omitempty"`
	RunID              int64                       `json:"run_id,omitempty"`
	BrowserSessionID   int64                       `json:"browser_session_id,omitempty"`
	WorkerMode         string                      `json:"worker_mode,omitempty"`
	Objective          string                      `json:"objective"`
	AllowedDomains     []string                    `json:"allowed_domains"`
	StartURLs          []string                    `json:"start_urls"`
	MaxPages           int                         `json:"max_pages"`
	MaxDurationSeconds int                         `json:"max_duration_seconds"`
	EvidenceRequired   bool                        `json:"evidence_required"`
	SiteProfiles       []huginnbrowser.SiteProfile `json:"site_profiles,omitempty"`
	Actions            []string                    `json:"actions,omitempty"`
}

type PageResult = huginnbrowser.PageResult
type ScreenshotMetadata = huginnbrowser.ScreenshotMetadata
type SelectedLink = huginnbrowser.SelectedLink
type DownloadedFile = huginnbrowser.DownloadedFile
type FormStateSummary = huginnbrowser.FormStateSummary

type Result struct {
	Status                 string                             `json:"status"`
	GoalID                 int64                              `json:"goal_id"`
	TaskID                 int64                              `json:"task_id,omitempty"`
	RunID                  int64                              `json:"run_id,omitempty"`
	BrowserSessionID       int64                              `json:"browser_session_id,omitempty"`
	BrowserSession         *huginnbrowser.SessionRef          `json:"browser_session,omitempty"`
	EvidenceID             int64                              `json:"evidence_id"`
	EvidenceType           string                             `json:"evidence_type"`
	RiskClass              string                             `json:"risk_class,omitempty"`
	ApprovalRequired       bool                               `json:"approval_required,omitempty"`
	ApprovalID             int64                              `json:"approval_id,omitempty"`
	AdapterStatus          string                             `json:"adapter_status,omitempty"`
	AdapterKind            string                             `json:"adapter_kind,omitempty"`
	StartURLs              []string                           `json:"start_urls"`
	AllowedDomains         []string                           `json:"allowed_domains"`
	MaxPages               int                                `json:"max_pages"`
	MaxDurationSeconds     int                                `json:"max_duration_seconds"`
	SiteProfiles           []huginnbrowser.SiteProfile        `json:"site_profiles,omitempty"`
	VisitedURLs            []string                           `json:"visited_urls,omitempty"`
	PageResults            []huginnbrowser.PageResult         `json:"page_results,omitempty"`
	ExtractedTextSummary   string                             `json:"extracted_text_summary,omitempty"`
	Screenshots            []string                           `json:"screenshots,omitempty"`
	ScreenshotMetadata     []huginnbrowser.ScreenshotMetadata `json:"screenshot_metadata,omitempty"`
	SelectedLinks          []huginnbrowser.SelectedLink       `json:"selected_links,omitempty"`
	DownloadedFiles        []huginnbrowser.DownloadedFile     `json:"downloaded_files,omitempty"`
	FormStateSummary       []huginnbrowser.FormStateSummary   `json:"form_state_summary,omitempty"`
	BrowserNotes           []string                           `json:"browser_notes,omitempty"`
	Confidence             string                             `json:"confidence,omitempty"`
	Limitations            []string                           `json:"limitations,omitempty"`
	ActionLog              []string                           `json:"action_log,omitempty"`
	WorkArtifact           *EvidenceArtifact                  `json:"work_artifact,omitempty"`
	RunArtifact            *sqlite.RunArtifact                `json:"run_artifact,omitempty"`
	RecoveryRecommendation string                             `json:"recovery_recommendation,omitempty"`
	ErrorCode              string                             `json:"error_code,omitempty"`
	ErrorMessage           string                             `json:"error_message,omitempty"`
	Evidence               sqlite.GoalEvidence                `json:"-"`
}

type EvidenceArtifact struct {
	Type                   string                             `json:"type"`
	EvidenceType           string                             `json:"evidence_type"`
	Status                 string                             `json:"status"`
	GoalID                 int64                              `json:"goal_id,omitempty"`
	TaskID                 int64                              `json:"task_id,omitempty"`
	RunID                  int64                              `json:"run_id,omitempty"`
	EvidenceID             int64                              `json:"evidence_id,omitempty"`
	RunArtifactID          int64                              `json:"run_artifact_id,omitempty"`
	ApprovalID             int64                              `json:"approval_id,omitempty"`
	ApprovalRequired       bool                               `json:"approval_required,omitempty"`
	RiskClass              string                             `json:"risk_class,omitempty"`
	AdapterStatus          string                             `json:"adapter_status,omitempty"`
	AdapterKind            string                             `json:"adapter_kind,omitempty"`
	Summary                string                             `json:"summary"`
	URI                    string                             `json:"uri,omitempty"`
	StartURLs              []string                           `json:"start_urls,omitempty"`
	AllowedDomains         []string                           `json:"allowed_domains,omitempty"`
	PageResults            []huginnbrowser.PageResult         `json:"page_results,omitempty"`
	Screenshots            []string                           `json:"screenshots,omitempty"`
	ScreenshotMetadata     []huginnbrowser.ScreenshotMetadata `json:"screenshot_metadata,omitempty"`
	SelectedLinks          []huginnbrowser.SelectedLink       `json:"selected_links,omitempty"`
	DownloadedFiles        []huginnbrowser.DownloadedFile     `json:"downloaded_files,omitempty"`
	FormStateSummary       []huginnbrowser.FormStateSummary   `json:"form_state_summary,omitempty"`
	ExtractedTextSummary   string                             `json:"extracted_text_summary,omitempty"`
	BrowserNotes           []string                           `json:"browser_notes,omitempty"`
	Confidence             string                             `json:"confidence,omitempty"`
	Limitations            []string                           `json:"limitations,omitempty"`
	RecoveryRecommendation string                             `json:"recovery_recommendation,omitempty"`
}

type ReadOnlyRunner interface {
	Run(context.Context, ReadOnlyTask) (Result, error)
}

type Service struct {
	Store   *sqlite.Store
	Adapter huginnbrowser.Adapter
}

func (service Service) Run(ctx context.Context, task ReadOnlyTask) (Result, error) {
	if service.Store == nil {
		return Result{}, fmt.Errorf("browser executor requires store")
	}
	riskClass := ClassifyRisk(task.Actions)
	if riskClass != RiskReadOnly {
		if task.TaskID <= 0 {
			return Result{}, ValidateReadOnlyTask(task)
		}
		if err := ValidateBrowserTaskEnvelope(task); err != nil {
			return Result{}, err
		}
		return service.requestApproval(ctx, task, riskClass)
	}
	if err := ValidateReadOnlyTask(task); err != nil {
		return Result{}, err
	}
	adapter := service.Adapter
	if adapter == nil {
		adapter = huginnbrowser.SelectAdapterFromEnv()
	}
	browserSession, err := service.attachBrowserSession(ctx, task)
	if err != nil {
		return Result{}, err
	}
	adapterResponse, err := adapter.Run(ctx, huginnbrowser.Request{
		GoalID:             task.GoalID,
		Mode:               task.WorkerMode,
		Objective:          task.Objective,
		StartURLs:          append([]string{}, task.StartURLs...),
		AllowedDomains:     append([]string{}, task.AllowedDomains...),
		MaxPages:           task.MaxPages,
		MaxDurationSeconds: task.MaxDurationSeconds,
		EvidenceRequired:   task.EvidenceRequired,
		SiteProfiles:       append([]huginnbrowser.SiteProfile{}, task.SiteProfiles...),
		BrowserSession:     browserSession,
	})
	if err != nil {
		result := Result{
			Status:           "failed",
			GoalID:           task.GoalID,
			TaskID:           task.TaskID,
			BrowserSessionID: task.BrowserSessionID,
			BrowserSession:   browserSession,
			RiskClass:        riskClass,
			ErrorCode:        "adapter_failed",
			ErrorMessage:     err.Error(),
		}
		if evidence, evidenceErr := service.recordFailedEvidence(ctx, task, result); evidenceErr == nil {
			result.EvidenceID = evidence.ID
			result.EvidenceType = evidence.EvidenceType
			result.Evidence = evidence
		}
		return result, fmt.Errorf("browser adapter failed: %w", err)
	}
	result := Result{
		Status:               "recorded",
		GoalID:               task.GoalID,
		TaskID:               task.TaskID,
		RunID:                task.RunID,
		BrowserSessionID:     task.BrowserSessionID,
		BrowserSession:       browserSession,
		EvidenceType:         EvidenceType,
		RiskClass:            riskClass,
		AdapterStatus:        adapterResponse.Status,
		AdapterKind:          adapterResponse.AdapterKind,
		StartURLs:            append([]string{}, task.StartURLs...),
		AllowedDomains:       append([]string{}, task.AllowedDomains...),
		MaxPages:             task.MaxPages,
		MaxDurationSeconds:   task.MaxDurationSeconds,
		SiteProfiles:         append([]huginnbrowser.SiteProfile{}, task.SiteProfiles...),
		VisitedURLs:          append([]string{}, adapterResponse.VisitedURLs...),
		PageResults:          append([]huginnbrowser.PageResult{}, adapterResponse.PageResults...),
		ExtractedTextSummary: adapterResponse.ExtractedTextSummary,
		Screenshots:          append([]string{}, adapterResponse.Screenshots...),
		ScreenshotMetadata:   append([]huginnbrowser.ScreenshotMetadata{}, adapterResponse.ScreenshotMetadata...),
		SelectedLinks:        append([]huginnbrowser.SelectedLink{}, adapterResponse.SelectedLinks...),
		DownloadedFiles:      append([]huginnbrowser.DownloadedFile{}, adapterResponse.DownloadedFiles...),
		FormStateSummary:     append([]huginnbrowser.FormStateSummary{}, adapterResponse.FormStateSummary...),
		BrowserNotes:         append([]string{}, adapterResponse.BrowserNotes...),
		Confidence:           adapterResponse.Confidence,
		Limitations:          append([]string{}, adapterResponse.Limitations...),
		ActionLog:            append([]string{}, adapterResponse.ActionLog...),
		ErrorCode:            adapterResponse.ErrorCode,
		ErrorMessage:         adapterResponse.ErrorMessage,
	}
	if err := service.recordEvidenceArtifacts(ctx, task, adapterResponse, &result); err != nil {
		return Result{}, err
	}
	return result, nil
}

func (service Service) attachBrowserSession(ctx context.Context, task ReadOnlyTask) (*huginnbrowser.SessionRef, error) {
	if task.BrowserSessionID <= 0 {
		return nil, nil
	}
	session, err := service.Store.GetBrowserSession(ctx, task.BrowserSessionID)
	if err != nil {
		return nil, err
	}
	if session.Status != sqlite.BrowserSessionStatusVerified {
		return nil, fmt.Errorf("browser session %d must be verified before attachment", session.ID)
	}
	allowedDomains, err := normalizeAllowedDomains(task.AllowedDomains)
	if err != nil {
		return nil, err
	}
	if !domainAllowed(session.Domain, allowedDomains) {
		return nil, fmt.Errorf("browser session domain %q is not allowed for this request", session.Domain)
	}
	for _, rawURL := range task.StartURLs {
		host, err := readOnlyURLHost(rawURL)
		if err != nil {
			return nil, err
		}
		if !domainAllowed(host, []string{session.Domain}) {
			return nil, fmt.Errorf("browser session domain %q does not match start url %q", session.Domain, rawURL)
		}
	}
	if err := service.Store.RecordBrowserProfileAttached(ctx, sqlite.RecordBrowserProfileAttachedParams{
		SessionID: session.ID,
		GoalID:    task.GoalID,
		TaskID:    task.TaskID,
		Actor:     defaultEvidenceCreatedBy,
		Reason:    "read_only_browser_request",
	}); err != nil {
		return nil, err
	}
	return &huginnbrowser.SessionRef{
		ID:                   session.ID,
		Domain:               session.Domain,
		PermissionTier:       string(session.PermissionTier),
		Status:               string(session.Status),
		ProfileStoragePolicy: string(session.ProfileStoragePolicy),
		ProfilePath:          session.ProfilePath,
	}, nil
}

func (service Service) recordEvidenceArtifacts(ctx context.Context, task ReadOnlyTask, response huginnbrowser.Response, result *Result) error {
	artifact := evidenceArtifactFromResponse(task, response)
	payload, err := json.Marshal(map[string]any{
		"executor": "browser_readonly",
		"status":   "adapter_response_recorded",
		"task":     task,
		"adapter":  response,
		"artifact": artifact,
	})
	if err != nil {
		return err
	}
	if task.GoalID > 0 {
		evidence, err := service.Store.AddGoalEvidence(ctx, sqlite.AddGoalEvidenceParams{
			GoalID:       task.GoalID,
			EvidenceType: EvidenceType,
			Summary:      artifact.Summary,
			URI:          artifact.URI,
			PayloadJSON:  string(payload),
			CreatedBy:    defaultEvidenceCreatedBy,
		})
		if err != nil {
			return err
		}
		result.EvidenceID = evidence.ID
		result.EvidenceType = evidence.EvidenceType
		result.Evidence = evidence
		artifact.EvidenceID = evidence.ID
	}
	if task.RunID > 0 {
		details, err := json.Marshal(artifact)
		if err != nil {
			return err
		}
		runArtifact, err := service.Store.RecordRunArtifact(ctx, sqlite.RecordRunArtifactParams{
			RunID:        task.RunID,
			ArtifactType: WorkArtifactType,
			Summary:      artifact.Summary,
			DetailsJSON:  string(details),
		})
		if err != nil {
			return err
		}
		result.RunArtifact = &runArtifact
		artifact.RunArtifactID = runArtifact.ID
	}
	if task.TaskID > 0 {
		if err := service.appendTaskEvidenceArtifact(ctx, task.TaskID, artifact); err != nil {
			return err
		}
		result.WorkArtifact = &artifact
	}
	return nil
}

func evidenceArtifactFromResponse(task ReadOnlyTask, response huginnbrowser.Response) EvidenceArtifact {
	return EvidenceArtifact{
		Type:                 WorkArtifactType,
		EvidenceType:         EvidenceType,
		Status:               "review_required",
		GoalID:               task.GoalID,
		TaskID:               task.TaskID,
		RunID:                task.RunID,
		AdapterStatus:        response.Status,
		AdapterKind:          response.AdapterKind,
		Summary:              defaultEvidenceSummary(response),
		URI:                  defaultEvidenceURI(task, response),
		StartURLs:            append([]string{}, task.StartURLs...),
		AllowedDomains:       append([]string{}, task.AllowedDomains...),
		PageResults:          append([]huginnbrowser.PageResult{}, response.PageResults...),
		Screenshots:          append([]string{}, response.Screenshots...),
		ScreenshotMetadata:   append([]huginnbrowser.ScreenshotMetadata{}, response.ScreenshotMetadata...),
		SelectedLinks:        append([]huginnbrowser.SelectedLink{}, response.SelectedLinks...),
		DownloadedFiles:      append([]huginnbrowser.DownloadedFile{}, response.DownloadedFiles...),
		FormStateSummary:     append([]huginnbrowser.FormStateSummary{}, response.FormStateSummary...),
		ExtractedTextSummary: response.ExtractedTextSummary,
		BrowserNotes:         append([]string{}, response.BrowserNotes...),
		Confidence:           response.Confidence,
		Limitations:          append([]string{}, response.Limitations...),
	}
}

func (service Service) appendTaskEvidenceArtifact(ctx context.Context, taskID int64, artifact EvidenceArtifact) error {
	task, err := service.Store.GetTask(ctx, taskID)
	if err != nil {
		return err
	}
	artifacts := ParseTaskEvidenceArtifacts(task.ArtifactsJSON)
	artifacts = append(artifacts, artifact)
	raw, err := json.Marshal(artifacts)
	if err != nil {
		return err
	}
	_, err = service.Store.UpdateTaskStatus(ctx, sqlite.UpdateTaskStatusParams{
		TaskID:         task.ID,
		Status:         task.Status,
		Summary:        task.Summary,
		TerminalReason: task.TerminalReason,
		ArtifactsJSON:  string(raw),
	})
	return err
}

func ParseTaskEvidenceArtifacts(raw string) []EvidenceArtifact {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "null" {
		return nil
	}
	var artifacts []EvidenceArtifact
	if err := json.Unmarshal([]byte(raw), &artifacts); err == nil {
		return filterEvidenceArtifacts(artifacts)
	}
	var generic []map[string]any
	if err := json.Unmarshal([]byte(raw), &generic); err != nil {
		return nil
	}
	artifacts = make([]EvidenceArtifact, 0, len(generic))
	for _, item := range generic {
		if fmt.Sprint(item["type"]) != WorkArtifactType {
			continue
		}
		encoded, err := json.Marshal(item)
		if err != nil {
			continue
		}
		var artifact EvidenceArtifact
		if err := json.Unmarshal(encoded, &artifact); err == nil {
			artifacts = append(artifacts, artifact)
		}
	}
	return artifacts
}

func filterEvidenceArtifacts(artifacts []EvidenceArtifact) []EvidenceArtifact {
	filtered := make([]EvidenceArtifact, 0, len(artifacts))
	for _, artifact := range artifacts {
		if artifact.Type == WorkArtifactType || artifact.EvidenceType == EvidenceType || artifact.EvidenceType == EvidenceTypeApprovalRequired {
			filtered = append(filtered, artifact)
		}
	}
	return filtered
}

func (service Service) requestApproval(ctx context.Context, task ReadOnlyTask, riskClass string) (Result, error) {
	if task.TaskID <= 0 {
		return Result{}, fmt.Errorf("task_id must be positive for approval-required browser requests")
	}
	if _, err := service.Store.BlockTask(ctx, sqlite.BlockTaskParams{TaskID: task.TaskID, Reason: "approval_required"}); err != nil {
		return Result{}, err
	}
	var runID *int64
	if task.RunID > 0 {
		runID = &task.RunID
	}
	approval, err := service.Store.RequestApproval(ctx, sqlite.RequestApprovalParams{
		TaskID:      task.TaskID,
		RunID:       runID,
		Status:      "pending",
		RequestedBy: defaultEvidenceCreatedBy,
	})
	if err != nil {
		return Result{}, err
	}
	artifact := EvidenceArtifact{
		Type:             WorkArtifactType,
		EvidenceType:     EvidenceTypeApprovalRequired,
		Status:           "approval_required",
		GoalID:           task.GoalID,
		TaskID:           task.TaskID,
		RunID:            task.RunID,
		ApprovalID:       approval.ID,
		ApprovalRequired: true,
		RiskClass:        riskClass,
		Summary:          "browser request requires approval before external mutation",
		URI:              defaultEvidenceURI(task, huginnbrowser.Response{}),
		StartURLs:        append([]string{}, task.StartURLs...),
		AllowedDomains:   append([]string{}, task.AllowedDomains...),
		BrowserNotes:     []string{"adapter was not executed because the request includes an external mutation action"},
		Confidence:       "high",
		Limitations:      []string{"no browsing evidence collected before approval"},
	}
	payload, err := json.Marshal(map[string]any{
		"executor":          "huginn_browser_plugin",
		"status":            "approval_required",
		"task":              task,
		"risk_class":        riskClass,
		"approval_required": true,
		"approval_id":       approval.ID,
		"executed":          false,
		"artifact":          artifact,
	})
	if err != nil {
		return Result{}, err
	}
	var evidence sqlite.GoalEvidence
	if task.GoalID > 0 {
		evidence, err = service.Store.AddGoalEvidence(ctx, sqlite.AddGoalEvidenceParams{
			GoalID:       task.GoalID,
			EvidenceType: EvidenceTypeApprovalRequired,
			Summary:      artifact.Summary,
			URI:          artifact.URI,
			PayloadJSON:  string(payload),
			CreatedBy:    defaultEvidenceCreatedBy,
		})
		if err != nil {
			return Result{}, err
		}
		artifact.EvidenceID = evidence.ID
	}
	if err := service.appendTaskEvidenceArtifact(ctx, task.TaskID, artifact); err != nil {
		return Result{}, err
	}
	return Result{
		Status:             "approval_required",
		GoalID:             task.GoalID,
		TaskID:             task.TaskID,
		RunID:              task.RunID,
		EvidenceID:         evidence.ID,
		EvidenceType:       EvidenceTypeApprovalRequired,
		RiskClass:          riskClass,
		ApprovalRequired:   true,
		ApprovalID:         approval.ID,
		WorkArtifact:       &artifact,
		StartURLs:          append([]string{}, task.StartURLs...),
		AllowedDomains:     append([]string{}, task.AllowedDomains...),
		MaxPages:           task.MaxPages,
		MaxDurationSeconds: task.MaxDurationSeconds,
		SiteProfiles:       append([]huginnbrowser.SiteProfile{}, task.SiteProfiles...),
		ActionLog:          []string{"approval_required", "adapter_not_executed"},
		BrowserNotes:       append([]string{}, artifact.BrowserNotes...),
		Confidence:         artifact.Confidence,
		Limitations:        append([]string{}, artifact.Limitations...),
		Evidence:           evidence,
	}, nil
}

func (service Service) recordFailedEvidence(ctx context.Context, task ReadOnlyTask, result Result) (sqlite.GoalEvidence, error) {
	payload, err := json.Marshal(map[string]any{
		"executor":   "huginn_browser_plugin",
		"status":     "failed",
		"task":       task,
		"risk_class": result.RiskClass,
		"error_code": result.ErrorCode,
		"error":      result.ErrorMessage,
	})
	if err != nil {
		return sqlite.GoalEvidence{}, err
	}
	return service.Store.AddGoalEvidence(ctx, sqlite.AddGoalEvidenceParams{
		GoalID:       task.GoalID,
		EvidenceType: EvidenceTypeFailed,
		Summary:      "browser request failed before evidence collection completed",
		URI:          defaultEvidenceURI(task, huginnbrowser.Response{}),
		PayloadJSON:  string(payload),
		CreatedBy:    defaultEvidenceCreatedBy,
	})
}

func defaultEvidenceSummary(response huginnbrowser.Response) string {
	if strings.TrimSpace(response.ExtractedTextSummary) != "" {
		return response.ExtractedTextSummary
	}
	return "read-only browser task produced stub/local evidence"
}

func defaultEvidenceURI(task ReadOnlyTask, response huginnbrowser.Response) string {
	for _, uri := range response.VisitedURLs {
		if strings.TrimSpace(uri) != "" {
			return strings.TrimSpace(uri)
		}
	}
	return task.StartURLs[0]
}

func ValidateReadOnlyTask(task ReadOnlyTask) error {
	if err := ValidateBrowserTaskEnvelope(task); err != nil {
		return err
	}
	for _, action := range task.Actions {
		if !isReadOnlyAction(action) {
			return fmt.Errorf("mutation action %q is not allowed for read-only browser tasks", action)
		}
	}
	return nil
}

func ValidateBrowserTaskEnvelope(task ReadOnlyTask) error {
	if task.GoalID <= 0 && task.TaskID <= 0 {
		return fmt.Errorf("goal_id or task_id must be positive")
	}
	if strings.TrimSpace(task.Objective) == "" {
		return fmt.Errorf("objective is required")
	}
	if len(task.AllowedDomains) == 0 {
		return fmt.Errorf("allowed_domains is required")
	}
	if len(task.StartURLs) == 0 {
		return fmt.Errorf("start_urls is required")
	}
	if task.MaxPages <= 0 || task.MaxPages > MaxPagesLimit {
		return fmt.Errorf("max_pages must be between 1 and %d", MaxPagesLimit)
	}
	if task.MaxDurationSeconds <= 0 || task.MaxDurationSeconds > MaxDurationSecondsLimit {
		return fmt.Errorf("max_duration_seconds must be between 1 and %d", MaxDurationSecondsLimit)
	}
	allowedDomains, err := normalizeAllowedDomains(task.AllowedDomains)
	if err != nil {
		return err
	}
	for _, profile := range task.SiteProfiles {
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
	for _, rawURL := range task.StartURLs {
		host, err := readOnlyURLHost(rawURL)
		if err != nil {
			return err
		}
		if !domainAllowed(host, allowedDomains) {
			return fmt.Errorf("disallowed domain %q for read-only browser task", host)
		}
	}
	return nil
}

func ClassifyRisk(actions []string) string {
	for _, action := range actions {
		if !isReadOnlyAction(action) {
			return RiskExternalMutation
		}
	}
	return RiskReadOnly
}

func normalizeAllowedDomains(domains []string) ([]string, error) {
	normalized := make([]string, 0, len(domains))
	for _, domain := range domains {
		candidate := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(domain)), ".")
		if candidate == "" {
			return nil, fmt.Errorf("allowed domain is required")
		}
		if strings.Contains(candidate, "/") || strings.Contains(candidate, ":") {
			return nil, fmt.Errorf("allowed domain %q must be a hostname", domain)
		}
		normalized = append(normalized, candidate)
	}
	return normalized, nil
}

func readOnlyURLHost(rawURL string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed == nil || parsed.Host == "" {
		return "", fmt.Errorf("start url %q must be an absolute URL", rawURL)
	}
	if parsed.User != nil {
		return "", fmt.Errorf("start url %q must not include credentials", rawURL)
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return "", fmt.Errorf("start url %q must use http or https", rawURL)
	}
	host := parsed.Hostname()
	if host == "" {
		host = parsed.Host
	}
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if ip := net.ParseIP(host); ip != nil {
		return "", fmt.Errorf("start url %q must use a hostname, not an IP address", rawURL)
	}
	return host, nil
}

func domainAllowed(host string, allowedDomains []string) bool {
	for _, domain := range allowedDomains {
		if host == domain || strings.HasSuffix(host, "."+domain) {
			return true
		}
	}
	return false
}

func isReadOnlyAction(action string) bool {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "", "read", "navigate", "snapshot", "extract":
		return true
	default:
		return false
	}
}
