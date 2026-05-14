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
	EvidenceType             = "browser_readonly"
	WorkEvidenceType         = "browser_evidence"
	WorkEvidenceExecutor     = "huginn_browser"
	MaxPagesLimit            = 20
	MaxDurationSecondsLimit  = 300
	defaultEvidenceCreatedBy = "browser_executor"
)

type RiskClass string

const (
	RiskClassReadOnly         RiskClass = "read_only"
	RiskClassExternalMutation RiskClass = "external_mutation"
)

type ReadOnlyTask struct {
	GoalID             int64                       `json:"goal_id"`
	WorkerMode         string                      `json:"worker_mode,omitempty"`
	Objective          string                      `json:"objective"`
	AllowedDomains     []string                    `json:"allowed_domains"`
	StartURLs          []string                    `json:"start_urls"`
	MaxPages           int                         `json:"max_pages"`
	MaxDurationSeconds int                         `json:"max_duration_seconds"`
	EvidenceRequired   bool                        `json:"evidence_required"`
	SiteProfiles       []huginnbrowser.SiteProfile `json:"site_profiles,omitempty"`
	BrowserSessionID   int64                       `json:"browser_session_id,omitempty"`
	BrowserSession     *BrowserSessionReference    `json:"browser_session,omitempty"`
	Actions            []string                    `json:"actions,omitempty"`
}

type PageResult = huginnbrowser.PageResult
type BrowserSessionReference = huginnbrowser.BrowserSessionReference
type ScreenshotMetadata = huginnbrowser.ScreenshotMetadata
type SelectedLink = huginnbrowser.SelectedLink
type DownloadedFileMetadata = huginnbrowser.DownloadedFileMetadata

type WorkEvidenceTask struct {
	TaskID             int64                       `json:"task_id"`
	WorkerMode         string                      `json:"worker_mode,omitempty"`
	Objective          string                      `json:"objective"`
	AllowedDomains     []string                    `json:"allowed_domains"`
	StartURLs          []string                    `json:"start_urls"`
	MaxPages           int                         `json:"max_pages"`
	MaxDurationSeconds int                         `json:"max_duration_seconds"`
	EvidenceRequired   bool                        `json:"evidence_required"`
	SiteProfiles       []huginnbrowser.SiteProfile `json:"site_profiles,omitempty"`
	BrowserSessionID   int64                       `json:"browser_session_id,omitempty"`
	BrowserSession     *BrowserSessionReference    `json:"browser_session,omitempty"`
	Actions            []string                    `json:"actions,omitempty"`
}

type PluginRequest struct {
	RequestID          string                      `json:"request_id,omitempty"`
	GoalID             int64                       `json:"goal_id,omitempty"`
	TaskID             int64                       `json:"task_id,omitempty"`
	WorkerMode         string                      `json:"worker_mode,omitempty"`
	Objective          string                      `json:"objective"`
	AllowedDomains     []string                    `json:"allowed_domains"`
	StartURLs          []string                    `json:"start_urls"`
	MaxPages           int                         `json:"max_pages"`
	MaxDurationSeconds int                         `json:"max_duration_seconds"`
	EvidenceRequired   bool                        `json:"evidence_required"`
	SiteProfiles       []huginnbrowser.SiteProfile `json:"site_profiles,omitempty"`
	BrowserSessionID   int64                       `json:"browser_session_id,omitempty"`
	BrowserSession     *BrowserSessionReference    `json:"browser_session,omitempty"`
	Actions            []string                    `json:"actions,omitempty"`
	RequestedBy        string                      `json:"requested_by,omitempty"`
}

type EvidenceArtifact struct {
	ID                     int64                                  `json:"id"`
	Type                   string                                 `json:"type"`
	EvidenceType           string                                 `json:"evidence_type,omitempty"`
	Status                 string                                 `json:"status,omitempty"`
	GoalID                 int64                                  `json:"goal_id,omitempty"`
	TaskID                 int64                                  `json:"task_id,omitempty"`
	RunID                  int64                                  `json:"run_id,omitempty"`
	EvidenceID             int64                                  `json:"evidence_id,omitempty"`
	RunArtifactID          int64                                  `json:"run_artifact_id,omitempty"`
	ApprovalID             int64                                  `json:"approval_id,omitempty"`
	ApprovalRequired       bool                                   `json:"approval_required,omitempty"`
	RiskClass              string                                 `json:"risk_class,omitempty"`
	AdapterStatus          string                                 `json:"adapter_status,omitempty"`
	AdapterKind            string                                 `json:"adapter_kind,omitempty"`
	URI                    string                                 `json:"uri,omitempty"`
	Summary                string                                 `json:"summary"`
	StartURLs              []string                               `json:"start_urls,omitempty"`
	AllowedDomains         []string                               `json:"allowed_domains,omitempty"`
	PageResults            []huginnbrowser.PageResult             `json:"page_results,omitempty"`
	Screenshots            []string                               `json:"screenshots,omitempty"`
	ScreenshotMetadata     []huginnbrowser.ScreenshotMetadata     `json:"screenshot_metadata,omitempty"`
	SelectedLinks          []huginnbrowser.SelectedLink           `json:"selected_links,omitempty"`
	DownloadedFiles        []huginnbrowser.DownloadedFileMetadata `json:"downloaded_files,omitempty"`
	DownloadedFileMetadata []huginnbrowser.DownloadedFileMetadata `json:"downloaded_file_metadata,omitempty"`
	FormStateSummary       string                                 `json:"form_state_summary,omitempty"`
	ExtractedTextSummary   string                                 `json:"extracted_text_summary,omitempty"`
	BrowserNotes           []string                               `json:"browser_notes,omitempty"`
	Confidence             string                                 `json:"confidence,omitempty"`
	Limitations            []string                               `json:"limitations,omitempty"`
	ActionLog              []string                               `json:"action_log,omitempty"`
	CreatedBy              string                                 `json:"created_by"`
}

type PluginResponse struct {
	Status           string            `json:"status"`
	RequestID        string            `json:"request_id,omitempty"`
	RiskClass        RiskClass         `json:"risk_class"`
	ApprovalRequired bool              `json:"approval_required"`
	ApprovalID       int64             `json:"approval_id,omitempty"`
	TaskID           int64             `json:"task_id,omitempty"`
	Evidence         *EvidenceArtifact `json:"evidence,omitempty"`
	Result           *Result           `json:"result,omitempty"`
	MutatingActions  []string          `json:"mutating_actions,omitempty"`
	ErrorCode        string            `json:"error_code,omitempty"`
	ErrorMessage     string            `json:"error_message,omitempty"`
	Approval         *sqlite.Approval  `json:"-"`
}

type Result struct {
	Status                    string                      `json:"status"`
	GoalID                    int64                       `json:"goal_id"`
	TaskID                    int64                       `json:"task_id,omitempty"`
	RunID                     int64                       `json:"run_id,omitempty"`
	RunArtifactID             int64                       `json:"run_artifact_id,omitempty"`
	EvidenceID                int64                       `json:"evidence_id"`
	EvidenceType              string                      `json:"evidence_type"`
	AdapterStatus             string                      `json:"adapter_status,omitempty"`
	AdapterKind               string                      `json:"adapter_kind,omitempty"`
	BrowserProofKind          string                      `json:"browser_proof_kind,omitempty"`
	RealBrowserEvidence       bool                        `json:"real_browser_evidence"`
	StartURLs                 []string                    `json:"start_urls"`
	AllowedDomains            []string                    `json:"allowed_domains"`
	MaxPages                  int                         `json:"max_pages"`
	MaxDurationSeconds        int                         `json:"max_duration_seconds"`
	SiteProfiles              []huginnbrowser.SiteProfile `json:"site_profiles,omitempty"`
	BrowserSession            *BrowserSessionReference    `json:"browser_session,omitempty"`
	VisitedURLs               []string                    `json:"visited_urls,omitempty"`
	PageResults               []huginnbrowser.PageResult  `json:"page_results,omitempty"`
	ExtractedTextSummary      string                      `json:"extracted_text_summary,omitempty"`
	Screenshots               []string                    `json:"screenshots,omitempty"`
	ScreenshotMetadata        []ScreenshotMetadata        `json:"screenshot_metadata,omitempty"`
	SelectedLinks             []SelectedLink              `json:"selected_links,omitempty"`
	DownloadedFiles           []DownloadedFileMetadata    `json:"downloaded_files,omitempty"`
	FormStateSummary          string                      `json:"form_state_summary,omitempty"`
	BrowserErrorRecoveryNotes []string                    `json:"browser_error_recovery_notes,omitempty"`
	Confidence                string                      `json:"confidence,omitempty"`
	Limitations               []string                    `json:"limitations,omitempty"`
	ActionLog                 []string                    `json:"action_log,omitempty"`
	ErrorCode                 string                      `json:"error_code,omitempty"`
	ErrorMessage              string                      `json:"error_message,omitempty"`
	Evidence                  sqlite.GoalEvidence         `json:"-"`
}

type ReadOnlyRunner interface {
	Run(context.Context, ReadOnlyTask) (Result, error)
}

type PluginRunner interface {
	RunPlugin(context.Context, PluginRequest) (PluginResponse, error)
}

type Service struct {
	Store   *sqlite.Store
	Adapter huginnbrowser.Adapter
}

func (service Service) RunPlugin(ctx context.Context, request PluginRequest) (PluginResponse, error) {
	riskClass := ClassifyRisk(request.Actions)
	if riskClass == RiskClassReadOnly {
		if request.TaskID > 0 && request.GoalID == 0 {
			result, err := service.RunWorkEvidence(ctx, request.workEvidenceTask())
			response := PluginResponse{
				Status:           result.Status,
				RequestID:        strings.TrimSpace(request.RequestID),
				RiskClass:        riskClass,
				ApprovalRequired: false,
				TaskID:           result.TaskID,
				Result:           &result,
				ErrorCode:        result.ErrorCode,
				ErrorMessage:     result.ErrorMessage,
			}
			if result.RunArtifactID > 0 {
				response.Evidence = &EvidenceArtifact{
					ID:        result.RunArtifactID,
					Type:      WorkEvidenceType,
					URI:       defaultResultURI(result),
					Summary:   result.ExtractedTextSummary,
					CreatedBy: defaultEvidenceCreatedBy,
				}
			}
			return response, err
		}
		result, err := service.Run(ctx, request.readOnlyTask())
		response := PluginResponse{
			Status:           result.Status,
			RequestID:        strings.TrimSpace(request.RequestID),
			RiskClass:        riskClass,
			ApprovalRequired: false,
			Result:           &result,
			ErrorCode:        result.ErrorCode,
			ErrorMessage:     result.ErrorMessage,
		}
		if result.EvidenceID > 0 {
			response.Evidence = &EvidenceArtifact{
				ID:        result.EvidenceID,
				Type:      result.EvidenceType,
				URI:       result.Evidence.URI,
				Summary:   result.Evidence.Summary,
				CreatedBy: result.Evidence.CreatedBy,
			}
		}
		return response, err
	}

	if service.Store == nil {
		return PluginResponse{}, fmt.Errorf("browser executor requires store")
	}
	if err := validateMutationPluginRequest(request); err != nil {
		return PluginResponse{
			Status:          "failed",
			RequestID:       strings.TrimSpace(request.RequestID),
			RiskClass:       riskClass,
			MutatingActions: mutatingActions(request.Actions),
			ErrorCode:       "invalid_request",
			ErrorMessage:    err.Error(),
		}, err
	}
	blockedTask, approval, err := service.Store.BlockTaskAndRequestApproval(ctx, sqlite.BlockTaskAndRequestApprovalParams{
		TaskID:      request.TaskID,
		RequestedBy: defaultString(strings.TrimSpace(request.RequestedBy), defaultEvidenceCreatedBy),
	})
	if err != nil {
		return PluginResponse{
			Status:          "failed",
			RequestID:       strings.TrimSpace(request.RequestID),
			RiskClass:       riskClass,
			MutatingActions: mutatingActions(request.Actions),
			ErrorCode:       "approval_request_failed",
			ErrorMessage:    err.Error(),
		}, err
	}
	return PluginResponse{
		Status:           "approval_required",
		RequestID:        strings.TrimSpace(request.RequestID),
		RiskClass:        riskClass,
		ApprovalRequired: true,
		ApprovalID:       approval.ID,
		TaskID:           blockedTask.ID,
		MutatingActions:  mutatingActions(request.Actions),
		ErrorCode:        "approval_required",
		ErrorMessage:     "external browser mutation requires Odin approval before execution",
		Approval:         &approval,
	}, nil
}

func (service Service) Run(ctx context.Context, task ReadOnlyTask) (Result, error) {
	if service.Store == nil {
		return Result{}, fmt.Errorf("browser executor requires store")
	}
	if err := ValidateReadOnlyTask(task); err != nil {
		return Result{}, err
	}
	resolvedTask := task
	sessionRef, err := service.resolveBrowserSession(ctx, task)
	if err != nil {
		return Result{}, err
	}
	resolvedTask.BrowserSession = sessionRef
	adapter := service.Adapter
	if adapter == nil {
		adapter = huginnbrowser.SelectAdapterFromEnv()
	}
	adapterResponse, err := adapter.Run(ctx, huginnbrowser.Request{
		GoalID:             resolvedTask.GoalID,
		Mode:               resolvedTask.WorkerMode,
		Objective:          resolvedTask.Objective,
		StartURLs:          append([]string{}, resolvedTask.StartURLs...),
		AllowedDomains:     append([]string{}, resolvedTask.AllowedDomains...),
		MaxPages:           resolvedTask.MaxPages,
		MaxDurationSeconds: resolvedTask.MaxDurationSeconds,
		EvidenceRequired:   resolvedTask.EvidenceRequired,
		SiteProfiles:       append([]huginnbrowser.SiteProfile{}, resolvedTask.SiteProfiles...),
		BrowserSession:     cloneBrowserSessionReference(resolvedTask.BrowserSession),
	})
	if err != nil {
		return Result{
			Status:       "failed",
			GoalID:       resolvedTask.GoalID,
			ErrorCode:    "adapter_failed",
			ErrorMessage: err.Error(),
		}, fmt.Errorf("browser adapter failed: %w", err)
	}
	browserProofKind, realBrowserEvidence := classifyBrowserProof(resolvedTask.WorkerMode, adapterResponse)
	payload, err := json.Marshal(map[string]any{
		"executor":              "browser_readonly",
		"status":                "adapter_response_recorded",
		"browser_proof_kind":    browserProofKind,
		"real_browser_evidence": realBrowserEvidence,
		"task":                  resolvedTask,
		"adapter":               adapterResponse,
	})
	if err != nil {
		return Result{}, err
	}
	evidence, err := service.Store.AddGoalEvidence(ctx, sqlite.AddGoalEvidenceParams{
		GoalID:       resolvedTask.GoalID,
		EvidenceType: EvidenceType,
		Summary:      defaultEvidenceSummary(adapterResponse),
		URI:          defaultEvidenceURI(resolvedTask, adapterResponse),
		PayloadJSON:  string(payload),
		CreatedBy:    defaultEvidenceCreatedBy,
	})
	if err != nil {
		return Result{}, err
	}
	return Result{
		Status:                    "recorded",
		GoalID:                    resolvedTask.GoalID,
		EvidenceID:                evidence.ID,
		EvidenceType:              evidence.EvidenceType,
		AdapterStatus:             adapterResponse.Status,
		AdapterKind:               adapterResponse.AdapterKind,
		BrowserProofKind:          browserProofKind,
		RealBrowserEvidence:       realBrowserEvidence,
		StartURLs:                 append([]string{}, resolvedTask.StartURLs...),
		AllowedDomains:            append([]string{}, resolvedTask.AllowedDomains...),
		MaxPages:                  resolvedTask.MaxPages,
		MaxDurationSeconds:        resolvedTask.MaxDurationSeconds,
		SiteProfiles:              append([]huginnbrowser.SiteProfile{}, resolvedTask.SiteProfiles...),
		BrowserSession:            cloneBrowserSessionReference(resolvedTask.BrowserSession),
		VisitedURLs:               append([]string{}, adapterResponse.VisitedURLs...),
		PageResults:               append([]huginnbrowser.PageResult{}, adapterResponse.PageResults...),
		ExtractedTextSummary:      adapterResponse.ExtractedTextSummary,
		Screenshots:               append([]string{}, adapterResponse.Screenshots...),
		ScreenshotMetadata:        append([]ScreenshotMetadata{}, adapterResponse.ScreenshotMetadata...),
		SelectedLinks:             append([]SelectedLink{}, adapterResponse.SelectedLinks...),
		DownloadedFiles:           append([]DownloadedFileMetadata{}, adapterResponse.DownloadedFiles...),
		FormStateSummary:          adapterResponse.FormStateSummary,
		BrowserErrorRecoveryNotes: append([]string{}, adapterResponse.BrowserErrorRecoveryNotes...),
		Confidence:                adapterResponse.Confidence,
		Limitations:               append([]string{}, adapterResponse.Limitations...),
		ActionLog:                 append([]string{}, adapterResponse.ActionLog...),
		Evidence:                  evidence,
	}, nil
}

func (service Service) RunWorkEvidence(ctx context.Context, task WorkEvidenceTask) (Result, error) {
	if service.Store == nil {
		return Result{}, fmt.Errorf("browser executor requires store")
	}
	if err := ValidateWorkEvidenceTask(task); err != nil {
		return Result{}, err
	}
	storedTask, err := service.Store.GetTask(ctx, task.TaskID)
	if err != nil {
		return Result{}, err
	}
	resolvedTask := task
	if strings.TrimSpace(resolvedTask.Objective) == "" {
		resolvedTask.Objective = storedTask.Title
	}
	readOnlyTask := resolvedTask.readOnlyTask()
	sessionRef, err := service.resolveBrowserSession(ctx, readOnlyTask)
	if err != nil {
		return Result{}, err
	}
	resolvedTask.BrowserSession = sessionRef
	readOnlyTask.BrowserSession = sessionRef
	attempt, err := service.nextWorkEvidenceAttempt(ctx, task.TaskID)
	if err != nil {
		return Result{}, err
	}
	run, err := service.Store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:   task.TaskID,
		Executor: WorkEvidenceExecutor,
		Attempt:  attempt,
		Status:   "running",
	})
	if err != nil {
		return Result{}, err
	}
	adapter := service.Adapter
	if adapter == nil {
		adapter = huginnbrowser.SelectAdapterFromEnv()
	}
	adapterResponse, adapterErr := adapter.Run(ctx, huginnbrowser.Request{
		Mode:               resolvedTask.WorkerMode,
		Objective:          resolvedTask.Objective,
		StartURLs:          append([]string{}, resolvedTask.StartURLs...),
		AllowedDomains:     append([]string{}, resolvedTask.AllowedDomains...),
		MaxPages:           resolvedTask.MaxPages,
		MaxDurationSeconds: resolvedTask.MaxDurationSeconds,
		EvidenceRequired:   resolvedTask.EvidenceRequired,
		SiteProfiles:       append([]huginnbrowser.SiteProfile{}, resolvedTask.SiteProfiles...),
		BrowserSession:     cloneBrowserSessionReference(resolvedTask.BrowserSession),
	})
	if adapterErr != nil {
		adapterResponse = huginnbrowser.Response{
			Status:                    "failed",
			AdapterKind:               "unknown",
			VisitedURLs:               append([]string{}, resolvedTask.StartURLs...),
			ExtractedTextSummary:      "Browser adapter failed before evidence capture completed.",
			ErrorCode:                 "adapter_failed",
			ErrorMessage:              adapterErr.Error(),
			BrowserErrorRecoveryNotes: []string{"Inspect browser adapter configuration and retry the capture."},
			Confidence:                "failed_capture",
			Limitations:               []string{"Adapter returned an execution error."},
		}
	}
	detailsJSON, err := browserWorkEvidenceDetailsJSON(resolvedTask, adapterResponse)
	if err != nil {
		return Result{}, err
	}
	artifact, err := service.Store.RecordRunArtifact(ctx, sqlite.RecordRunArtifactParams{
		RunID:        run.ID,
		ArtifactType: WorkEvidenceType,
		Summary:      defaultEvidenceSummary(adapterResponse),
		DetailsJSON:  detailsJSON,
	})
	if err != nil {
		return Result{}, err
	}
	result := Result{
		Status:                    "recorded",
		TaskID:                    resolvedTask.TaskID,
		RunID:                     run.ID,
		RunArtifactID:             artifact.ID,
		EvidenceType:              WorkEvidenceType,
		AdapterStatus:             adapterResponse.Status,
		AdapterKind:               adapterResponse.AdapterKind,
		BrowserProofKind:          classifyBrowserProofKind(resolvedTask.WorkerMode, adapterResponse),
		RealBrowserEvidence:       isRealBrowserEvidence(resolvedTask.WorkerMode, adapterResponse),
		StartURLs:                 append([]string{}, resolvedTask.StartURLs...),
		AllowedDomains:            append([]string{}, resolvedTask.AllowedDomains...),
		MaxPages:                  resolvedTask.MaxPages,
		MaxDurationSeconds:        resolvedTask.MaxDurationSeconds,
		SiteProfiles:              append([]huginnbrowser.SiteProfile{}, resolvedTask.SiteProfiles...),
		BrowserSession:            cloneBrowserSessionReference(resolvedTask.BrowserSession),
		VisitedURLs:               append([]string{}, adapterResponse.VisitedURLs...),
		PageResults:               append([]huginnbrowser.PageResult{}, adapterResponse.PageResults...),
		ExtractedTextSummary:      adapterResponse.ExtractedTextSummary,
		Screenshots:               append([]string{}, adapterResponse.Screenshots...),
		ScreenshotMetadata:        append([]ScreenshotMetadata{}, adapterResponse.ScreenshotMetadata...),
		SelectedLinks:             append([]SelectedLink{}, adapterResponse.SelectedLinks...),
		DownloadedFiles:           append([]DownloadedFileMetadata{}, adapterResponse.DownloadedFiles...),
		FormStateSummary:          adapterResponse.FormStateSummary,
		BrowserErrorRecoveryNotes: append([]string{}, adapterResponse.BrowserErrorRecoveryNotes...),
		Confidence:                adapterResponse.Confidence,
		Limitations:               append([]string{}, adapterResponse.Limitations...),
		ActionLog:                 append([]string{}, adapterResponse.ActionLog...),
		ErrorCode:                 adapterResponse.ErrorCode,
		ErrorMessage:              adapterResponse.ErrorMessage,
	}
	finishArtifacts := fmt.Sprintf(`[{"type":%q,"run_artifact_id":%d,"summary":%q}]`, WorkEvidenceType, artifact.ID, artifact.Summary)
	if browserEvidenceFailed(adapterResponse) {
		result.Status = "failed"
		if result.ErrorCode == "" {
			result.ErrorCode = "browser_capture_failed"
		}
		if result.ErrorMessage == "" {
			result.ErrorMessage = "browser evidence capture failed"
		}
		finishedTask, _, err := service.Store.FinishRunAndSetTaskStatus(ctx, sqlite.FinishRunAndSetTaskStatusParams{
			RunID:          run.ID,
			RunStatus:      "failed",
			TaskStatus:     "failed",
			Summary:        defaultEvidenceSummary(adapterResponse),
			TerminalReason: "browser_evidence_capture_failed",
			ArtifactsJSON:  finishArtifacts,
		})
		if err != nil {
			return Result{}, err
		}
		_ = service.Store.RecordTaskRecoveryRecommendation(ctx, sqlite.RecordTaskRecoveryRecommendationParams{
			Task:                   finishedTask,
			Decision:               "retry_browser_capture",
			RetryEligible:          true,
			RecoveryRecommendation: browserRecoveryRecommendation(adapterResponse),
			Source:                 "browser_evidence",
		})
		return result, nil
	}
	if _, err := service.Store.FinishRun(ctx, sqlite.FinishRunParams{
		RunID:          run.ID,
		Status:         "completed",
		Summary:        defaultEvidenceSummary(adapterResponse),
		TerminalReason: "browser_evidence_recorded",
		ArtifactsJSON:  finishArtifacts,
	}); err != nil {
		return Result{}, err
	}
	return result, nil
}

func (request PluginRequest) readOnlyTask() ReadOnlyTask {
	return ReadOnlyTask{
		GoalID:             request.GoalID,
		WorkerMode:         request.WorkerMode,
		Objective:          request.Objective,
		AllowedDomains:     append([]string{}, request.AllowedDomains...),
		StartURLs:          append([]string{}, request.StartURLs...),
		MaxPages:           request.MaxPages,
		MaxDurationSeconds: request.MaxDurationSeconds,
		EvidenceRequired:   request.EvidenceRequired,
		SiteProfiles:       append([]huginnbrowser.SiteProfile{}, request.SiteProfiles...),
		BrowserSessionID:   request.BrowserSessionID,
		BrowserSession:     cloneBrowserSessionReference(request.BrowserSession),
		Actions:            append([]string{}, request.Actions...),
	}
}

func (request PluginRequest) workEvidenceTask() WorkEvidenceTask {
	return WorkEvidenceTask{
		TaskID:             request.TaskID,
		WorkerMode:         request.WorkerMode,
		Objective:          request.Objective,
		AllowedDomains:     append([]string{}, request.AllowedDomains...),
		StartURLs:          append([]string{}, request.StartURLs...),
		MaxPages:           request.MaxPages,
		MaxDurationSeconds: request.MaxDurationSeconds,
		EvidenceRequired:   request.EvidenceRequired,
		SiteProfiles:       append([]huginnbrowser.SiteProfile{}, request.SiteProfiles...),
		BrowserSessionID:   request.BrowserSessionID,
		BrowserSession:     cloneBrowserSessionReference(request.BrowserSession),
		Actions:            append([]string{}, request.Actions...),
	}
}

func (task WorkEvidenceTask) readOnlyTask() ReadOnlyTask {
	return ReadOnlyTask{
		WorkerMode:         task.WorkerMode,
		Objective:          task.Objective,
		AllowedDomains:     append([]string{}, task.AllowedDomains...),
		StartURLs:          append([]string{}, task.StartURLs...),
		MaxPages:           task.MaxPages,
		MaxDurationSeconds: task.MaxDurationSeconds,
		EvidenceRequired:   task.EvidenceRequired,
		SiteProfiles:       append([]huginnbrowser.SiteProfile{}, task.SiteProfiles...),
		BrowserSessionID:   task.BrowserSessionID,
		BrowserSession:     cloneBrowserSessionReference(task.BrowserSession),
		Actions:            append([]string{}, task.Actions...),
	}
}

func validateMutationPluginRequest(request PluginRequest) error {
	if request.TaskID <= 0 {
		return fmt.Errorf("task_id is required for mutation-class browser requests")
	}
	task := request.readOnlyTask()
	task.GoalID = 1
	task.Actions = nil
	return ValidateReadOnlyTask(task)
}

func (service Service) resolveBrowserSession(ctx context.Context, task ReadOnlyTask) (*BrowserSessionReference, error) {
	if task.BrowserSessionID == 0 {
		return cloneBrowserSessionReference(task.BrowserSession), nil
	}
	session, err := service.Store.GetBrowserSession(ctx, task.BrowserSessionID)
	if err != nil {
		return nil, err
	}
	if session.Status != sqlite.BrowserSessionStatusVerified {
		return nil, fmt.Errorf("browser session %d must be verified before read-only browser use; status=%s", session.ID, session.Status)
	}
	if err := ensureBrowserSessionDomainMatchesTask(session, task); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("authenticated browser session attachment is not implemented; run without browser_session_id for public read-only evidence or complete the profile attach contract first")
}

func ensureBrowserSessionDomainMatchesTask(session sqlite.BrowserSession, task ReadOnlyTask) error {
	allowedDomains, err := normalizeAllowedDomains(task.AllowedDomains)
	if err != nil {
		return err
	}
	sessionDomain := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(session.Domain)), ".")
	if sessionDomain == "" {
		return fmt.Errorf("browser session domain is required")
	}
	if !domainAllowed(sessionDomain, allowedDomains) {
		return fmt.Errorf("browser session domain %q is not in allowed_domains", sessionDomain)
	}
	for _, rawURL := range task.StartURLs {
		host, err := readOnlyURLHost(rawURL)
		if err != nil {
			return err
		}
		if !domainAllowed(host, []string{sessionDomain}) {
			return fmt.Errorf("browser session domain %q cannot be used for start url host %q", sessionDomain, host)
		}
	}
	return nil
}

func browserSessionReference(session sqlite.BrowserSession) BrowserSessionReference {
	ref := BrowserSessionReference{
		ID:                   session.ID,
		Domain:               session.Domain,
		Status:               string(session.Status),
		PermissionTier:       string(session.PermissionTier),
		ProfileStoragePolicy: string(session.ProfileStoragePolicy),
		ProfilePath:          session.ProfilePath,
	}
	if session.LastVerifiedAt != nil {
		ref.LastVerifiedAt = session.LastVerifiedAt.UTC().Format("2006-01-02T15:04:05Z07:00")
	}
	return ref
}

func cloneBrowserSessionReference(ref *BrowserSessionReference) *BrowserSessionReference {
	if ref == nil {
		return nil
	}
	clone := *ref
	return &clone
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

func defaultResultURI(result Result) string {
	for _, uri := range result.VisitedURLs {
		if strings.TrimSpace(uri) != "" {
			return strings.TrimSpace(uri)
		}
	}
	for _, uri := range result.StartURLs {
		if strings.TrimSpace(uri) != "" {
			return strings.TrimSpace(uri)
		}
	}
	return ""
}

func ValidateReadOnlyTask(task ReadOnlyTask) error {
	return validateReadOnlyTask(task, true)
}

func ValidateWorkEvidenceTask(task WorkEvidenceTask) error {
	if task.TaskID <= 0 {
		return fmt.Errorf("task_id must be positive")
	}
	return validateReadOnlyTask(task.readOnlyTask(), false)
}

func validateReadOnlyTask(task ReadOnlyTask, requireGoal bool) error {
	if task.GoalID <= 0 {
		if requireGoal {
			return fmt.Errorf("goal_id must be positive")
		}
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
	if task.BrowserSessionID < 0 {
		return fmt.Errorf("browser_session_id must not be negative")
	}
	if task.BrowserSession != nil && task.BrowserSession.ID <= 0 {
		return fmt.Errorf("browser session id must be positive")
	}
	allowedDomains, err := normalizeAllowedDomains(task.AllowedDomains)
	if err != nil {
		return err
	}
	for _, action := range task.Actions {
		if !isReadOnlyAction(action) {
			return fmt.Errorf("mutation action %q is not allowed for read-only browser tasks", action)
		}
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

func (service Service) nextWorkEvidenceAttempt(ctx context.Context, taskID int64) (int, error) {
	var attempt int
	if err := service.Store.DB().QueryRowContext(ctx, `SELECT COALESCE(MAX(attempt), 0) + 1 FROM runs WHERE task_id = ?`, taskID).Scan(&attempt); err != nil {
		return 0, err
	}
	return attempt, nil
}

func browserWorkEvidenceDetailsJSON(task WorkEvidenceTask, response huginnbrowser.Response) (string, error) {
	browserProofKind, realBrowserEvidence := classifyBrowserProof(task.WorkerMode, response)
	payload, err := json.Marshal(map[string]any{
		"executor":                     WorkEvidenceExecutor,
		"artifact_type":                WorkEvidenceType,
		"status":                       response.Status,
		"task_id":                      task.TaskID,
		"start_urls":                   task.StartURLs,
		"allowed_domains":              task.AllowedDomains,
		"adapter_kind":                 response.AdapterKind,
		"browser_proof_kind":           browserProofKind,
		"real_browser_evidence":        realBrowserEvidence,
		"visited_urls":                 response.VisitedURLs,
		"page_results":                 response.PageResults,
		"page_title":                   firstPageTitle(response.PageResults),
		"url":                          firstVisitedURL(task, response),
		"extracted_text_summary":       response.ExtractedTextSummary,
		"screenshots":                  response.Screenshots,
		"screenshot_metadata":          response.ScreenshotMetadata,
		"selected_links":               response.SelectedLinks,
		"downloaded_files":             response.DownloadedFiles,
		"form_state_summary":           response.FormStateSummary,
		"browser_error_recovery_notes": response.BrowserErrorRecoveryNotes,
		"confidence":                   response.Confidence,
		"limitations":                  response.Limitations,
		"action_log":                   response.ActionLog,
		"error_code":                   response.ErrorCode,
		"error_message":                response.ErrorMessage,
	})
	if err != nil {
		return "", err
	}
	return string(payload), nil
}

func classifyBrowserProofKind(workerMode string, response huginnbrowser.Response) string {
	kind, _ := classifyBrowserProof(workerMode, response)
	return kind
}

func isRealBrowserEvidence(workerMode string, response huginnbrowser.Response) bool {
	_, real := classifyBrowserProof(workerMode, response)
	return real
}

func classifyBrowserProof(workerMode string, response huginnbrowser.Response) (string, bool) {
	adapterKind := strings.ToLower(strings.TrimSpace(response.AdapterKind))
	status := strings.ToLower(strings.TrimSpace(response.Status))
	if adapterKind != "huginn_live" {
		if adapterKind == "stub_local" {
			return "stub_contract_only", false
		}
		return "unknown_adapter", false
	}
	if browserEvidenceFailed(response) || status == "not_implemented" || actionLogContains(response.ActionLog, "no_live_browser_launched") {
		return "live_adapter_not_ready", false
	}
	if strings.EqualFold(strings.TrimSpace(workerMode), "browser") || responseHasBrowserMode(response) {
		if len(response.Screenshots) > 0 || actionLogContains(response.ActionLog, "browser_mode_selected") || actionLogContains(response.ActionLog, "screenshot_captured") {
			return "live_browser_readonly", true
		}
		return "live_browser_unverified", false
	}
	return "live_fetch_readonly", false
}

func responseHasBrowserMode(response huginnbrowser.Response) bool {
	for _, result := range response.PageResults {
		if strings.EqualFold(strings.TrimSpace(result.Mode), "browser") {
			return true
		}
	}
	return false
}

func actionLogContains(actionLog []string, marker string) bool {
	for _, action := range actionLog {
		if strings.EqualFold(strings.TrimSpace(action), marker) {
			return true
		}
	}
	return false
}

func browserEvidenceFailed(response huginnbrowser.Response) bool {
	switch strings.ToLower(strings.TrimSpace(response.Status)) {
	case "", "completed", "recorded", "ok", "success":
		return false
	default:
		return true
	}
}

func browserRecoveryRecommendation(response huginnbrowser.Response) string {
	for _, note := range response.BrowserErrorRecoveryNotes {
		if strings.TrimSpace(note) != "" {
			return strings.TrimSpace(note)
		}
	}
	return "Retry browser evidence capture with a narrower URL set or inspect the browser adapter error before retrying."
}

func firstPageTitle(results []huginnbrowser.PageResult) string {
	for _, result := range results {
		if strings.TrimSpace(result.Title) != "" {
			return strings.TrimSpace(result.Title)
		}
	}
	return ""
}

func firstVisitedURL(task WorkEvidenceTask, response huginnbrowser.Response) string {
	for _, uri := range response.VisitedURLs {
		if strings.TrimSpace(uri) != "" {
			return strings.TrimSpace(uri)
		}
	}
	for _, uri := range task.StartURLs {
		if strings.TrimSpace(uri) != "" {
			return strings.TrimSpace(uri)
		}
	}
	return ""
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

func ClassifyRisk(actions []string) RiskClass {
	for _, action := range actions {
		if !isReadOnlyAction(action) {
			return RiskClassExternalMutation
		}
	}
	return RiskClassReadOnly
}

func mutatingActions(actions []string) []string {
	mutating := make([]string, 0, len(actions))
	for _, action := range actions {
		normalized := strings.ToLower(strings.TrimSpace(action))
		if normalized == "" || isReadOnlyAction(normalized) {
			continue
		}
		mutating = append(mutating, normalized)
	}
	return mutating
}

func defaultString(value string, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return fallback
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
		if !isEvidenceArtifactType(fmt.Sprint(item["type"]), fmt.Sprint(item["evidence_type"])) {
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
		if isEvidenceArtifactType(artifact.Type, artifact.EvidenceType) {
			filtered = append(filtered, artifact)
		}
	}
	return filtered
}

func isEvidenceArtifactType(artifactType string, evidenceType string) bool {
	return artifactType == WorkEvidenceType || evidenceType == EvidenceType || evidenceType == WorkEvidenceType
}
