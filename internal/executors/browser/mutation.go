package browser

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"odin-os/internal/executors/drivers"
	"odin-os/internal/store/sqlite"
)

const (
	BrowserMutationEvidenceType         = "browser_mutation_evidence"
	BrowserMutationExecutor             = "huginn_browser_mutation"
	MutationDriverEnvVar                = "ODIN_HUGINN_BROWSER_MUTATION_DRIVER"
	MutationAllowedCommandsEnvVar       = "ODIN_HUGINN_BROWSER_MUTATION_ALLOWED_COMMANDS"
	defaultMutationDriverTimeoutSeconds = 30
)

type MutationDriver interface {
	Run(context.Context, BrowserMutationDriverRequest) (BrowserMutationDriverResponse, error)
}

type BrowserMutationPayload struct {
	SchemaVersion    int      `json:"schema_version"`
	ActionKind       string   `json:"action_kind"`
	AllowedDomains   []string `json:"allowed_domains"`
	StartURL         string   `json:"start_url"`
	BrowserSessionID int64    `json:"browser_session_id,omitempty"`
	RequestedBy      string   `json:"requested_by"`
	RedactionPolicy  string   `json:"redaction_policy"`
}

type browserMutationApprovalRequest struct {
	ActionKind         string
	AllowedDomainsJSON string
	StartURL           string
	BrowserSessionID   *int64
	PayloadJSON        string
	PayloadHash        string
}

type BrowserMutationDriverRequest struct {
	ApprovalID       int64                  `json:"approval_id"`
	TaskID           int64                  `json:"task_id"`
	ActionKind       string                 `json:"action_kind"`
	AllowedDomains   []string               `json:"allowed_domains"`
	StartURL         string                 `json:"start_url"`
	BrowserSessionID int64                  `json:"browser_session_id,omitempty"`
	PayloadHash      string                 `json:"payload_hash"`
	Payload          BrowserMutationPayload `json:"payload"`
}

type BrowserMutationDriverResponse struct {
	Status             string         `json:"status"`
	AdapterKind        string         `json:"adapter_kind"`
	ActionKind         string         `json:"action_kind"`
	FinalURL           string         `json:"final_url"`
	Evidence           map[string]any `json:"evidence,omitempty"`
	InterventionReason string         `json:"intervention_reason,omitempty"`
	Summary            string         `json:"summary,omitempty"`
	ErrorCode          string         `json:"error_code,omitempty"`
	ErrorMessage       string         `json:"error_message,omitempty"`
}

type MutationContinuationResult struct {
	Status             string                        `json:"status"`
	ApprovalID         int64                         `json:"approval_id"`
	TaskID             int64                         `json:"task_id"`
	RunID              int64                         `json:"run_id"`
	RunArtifactID      int64                         `json:"run_artifact_id"`
	EvidenceType       string                        `json:"evidence_type"`
	ActionKind         string                        `json:"action_kind"`
	PayloadHash        string                        `json:"payload_hash"`
	AllowedDomains     []string                      `json:"allowed_domains"`
	StartURL           string                        `json:"start_url"`
	BrowserSessionID   int64                         `json:"browser_session_id,omitempty"`
	AdapterStatus      string                        `json:"adapter_status"`
	AdapterKind        string                        `json:"adapter_kind"`
	FinalURL           string                        `json:"final_url,omitempty"`
	InterventionReason string                        `json:"intervention_reason,omitempty"`
	Evidence           map[string]any                `json:"evidence,omitempty"`
	ErrorCode          string                        `json:"error_code,omitempty"`
	ErrorMessage       string                        `json:"error_message,omitempty"`
	Approval           sqlite.Approval               `json:"-"`
	MutationRequest    sqlite.BrowserMutationRequest `json:"-"`
}

type envMutationDriver struct{}

func newBrowserMutationApprovalRequest(request PluginRequest) (browserMutationApprovalRequest, error) {
	actions := mutatingActions(request.Actions)
	if len(actions) == 0 {
		return browserMutationApprovalRequest{}, fmt.Errorf("mutation action is required")
	}
	if len(request.StartURLs) == 0 {
		return browserMutationApprovalRequest{}, fmt.Errorf("start_urls is required")
	}
	payload := BrowserMutationPayload{
		SchemaVersion:   1,
		ActionKind:      actions[0],
		AllowedDomains:  append([]string{}, request.AllowedDomains...),
		StartURL:        request.StartURLs[0],
		RequestedBy:     defaultString(strings.TrimSpace(request.RequestedBy), defaultEvidenceCreatedBy),
		RedactionPolicy: "secrets_and_sensitive_values",
	}
	var browserSessionID *int64
	if request.BrowserSessionID > 0 {
		payload.BrowserSessionID = request.BrowserSessionID
		browserSessionID = &request.BrowserSessionID
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return browserMutationApprovalRequest{}, err
	}
	allowedDomainsJSON, err := json.Marshal(payload.AllowedDomains)
	if err != nil {
		return browserMutationApprovalRequest{}, err
	}
	hash := sha256.Sum256(payloadJSON)
	return browserMutationApprovalRequest{
		ActionKind:         payload.ActionKind,
		AllowedDomainsJSON: string(allowedDomainsJSON),
		StartURL:           payload.StartURL,
		BrowserSessionID:   browserSessionID,
		PayloadJSON:        string(payloadJSON),
		PayloadHash:        hex.EncodeToString(hash[:]),
	}, nil
}

func (service Service) ContinueApprovedMutation(ctx context.Context, approvalID int64) (MutationContinuationResult, error) {
	if service.Store == nil {
		return MutationContinuationResult{}, fmt.Errorf("browser executor requires store")
	}
	if approvalID <= 0 {
		return MutationContinuationResult{}, fmt.Errorf("approval_id must be positive")
	}
	approval, err := service.Store.GetApproval(ctx, approvalID)
	if err != nil {
		return MutationContinuationResult{}, err
	}
	if approval.Status != "approved" {
		return MutationContinuationResult{}, fmt.Errorf("approval %d must be approved before browser mutation continuation; status=%s", approval.ID, approval.Status)
	}
	mutationRequest, err := service.Store.GetBrowserMutationRequestByApproval(ctx, approval.ID)
	if err != nil {
		return MutationContinuationResult{}, err
	}
	if mutationRequest.TaskID != approval.TaskID {
		return MutationContinuationResult{}, fmt.Errorf("browser mutation request task %d does not match approval task %d", mutationRequest.TaskID, approval.TaskID)
	}
	payload, allowedDomains, err := validateStoredBrowserMutationRequest(mutationRequest)
	if err != nil {
		return MutationContinuationResult{}, err
	}
	attempt, err := service.nextWorkEvidenceAttempt(ctx, approval.TaskID)
	if err != nil {
		return MutationContinuationResult{}, err
	}
	run, err := service.Store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:     approval.TaskID,
		Executor:   BrowserMutationExecutor,
		Attempt:    attempt,
		Status:     "running",
		TaskStatus: "running",
	})
	if err != nil {
		return MutationContinuationResult{}, err
	}
	driver := service.MutationDriver
	if driver == nil {
		driver = envMutationDriver{}
	}
	request := BrowserMutationDriverRequest{
		ApprovalID:     approval.ID,
		TaskID:         approval.TaskID,
		ActionKind:     mutationRequest.ActionKind,
		AllowedDomains: allowedDomains,
		StartURL:       mutationRequest.StartURL,
		PayloadHash:    mutationRequest.PayloadHash,
		Payload:        payload,
	}
	if mutationRequest.BrowserSessionID != nil {
		request.BrowserSessionID = *mutationRequest.BrowserSessionID
	}
	response, driverErr := driver.Run(ctx, request)
	if driverErr != nil {
		response = BrowserMutationDriverResponse{
			Status:       "failed",
			AdapterKind:  "huginn_browser_mutation",
			ActionKind:   mutationRequest.ActionKind,
			Summary:      "Browser mutation continuation driver failed.",
			ErrorCode:    "driver_failed",
			ErrorMessage: driverErr.Error(),
		}
	}
	response = normalizeMutationDriverResponse(response, mutationRequest)
	detailsJSON, err := mutationEvidenceDetailsJSON(approval, mutationRequest, request, response)
	if err != nil {
		return MutationContinuationResult{}, err
	}
	artifact, err := service.Store.RecordRunArtifact(ctx, sqlite.RecordRunArtifactParams{
		RunID:        run.ID,
		ArtifactType: BrowserMutationEvidenceType,
		Summary:      defaultMutationEvidenceSummary(response),
		DetailsJSON:  detailsJSON,
	})
	if err != nil {
		return MutationContinuationResult{}, err
	}
	runStatus, terminalReason := mutationRunStatus(response)
	taskStatus := "failed"
	if runStatus == "completed" {
		taskStatus = "completed"
	} else if runStatus == "intervention_required" {
		taskStatus = "blocked"
	}
	finishArtifacts := fmt.Sprintf(`[{"type":%q,"run_artifact_id":%d,"summary":%q}]`, BrowserMutationEvidenceType, artifact.ID, artifact.Summary)
	if _, _, err := service.Store.FinishRunAndSetTaskStatus(ctx, sqlite.FinishRunAndSetTaskStatusParams{
		RunID:          run.ID,
		RunStatus:      runStatus,
		TaskStatus:     taskStatus,
		Summary:        defaultMutationEvidenceSummary(response),
		TerminalReason: terminalReason,
		ArtifactsJSON:  finishArtifacts,
	}); err != nil {
		return MutationContinuationResult{}, err
	}
	result := MutationContinuationResult{
		Status:             response.Status,
		ApprovalID:         approval.ID,
		TaskID:             approval.TaskID,
		RunID:              run.ID,
		RunArtifactID:      artifact.ID,
		EvidenceType:       BrowserMutationEvidenceType,
		ActionKind:         mutationRequest.ActionKind,
		PayloadHash:        mutationRequest.PayloadHash,
		AllowedDomains:     allowedDomains,
		StartURL:           mutationRequest.StartURL,
		AdapterStatus:      response.Status,
		AdapterKind:        response.AdapterKind,
		FinalURL:           response.FinalURL,
		InterventionReason: response.InterventionReason,
		Evidence:           response.Evidence,
		ErrorCode:          response.ErrorCode,
		ErrorMessage:       response.ErrorMessage,
		Approval:           approval,
		MutationRequest:    mutationRequest,
	}
	if mutationRequest.BrowserSessionID != nil {
		result.BrowserSessionID = *mutationRequest.BrowserSessionID
	}
	return result, nil
}

func (envMutationDriver) Run(ctx context.Context, request BrowserMutationDriverRequest) (BrowserMutationDriverResponse, error) {
	driverPath := strings.TrimSpace(os.Getenv(MutationDriverEnvVar))
	if driverPath == "" {
		return BrowserMutationDriverResponse{}, fmt.Errorf("browser mutation driver is not configured")
	}
	if !mutationDriverAllowed(driverPath, os.Getenv(MutationAllowedCommandsEnvVar)) {
		return BrowserMutationDriverResponse{}, fmt.Errorf("browser mutation driver is not allowlisted")
	}
	var response BrowserMutationDriverResponse
	_, err := drivers.Invoke(ctx, drivers.Options{
		DriverPath: driverPath,
		Label:      "browser mutation",
		Timeout:    defaultMutationDriverTimeoutSeconds * time.Second,
	}, request, &response)
	return response, err
}

func validateStoredBrowserMutationRequest(request sqlite.BrowserMutationRequest) (BrowserMutationPayload, []string, error) {
	if strings.TrimSpace(request.PayloadJSON) == "" {
		return BrowserMutationPayload{}, nil, fmt.Errorf("browser mutation payload is empty")
	}
	hash := sha256.Sum256([]byte(request.PayloadJSON))
	if hex.EncodeToString(hash[:]) != strings.TrimSpace(request.PayloadHash) {
		return BrowserMutationPayload{}, nil, fmt.Errorf("browser mutation payload hash mismatch")
	}
	var payload BrowserMutationPayload
	if err := json.Unmarshal([]byte(request.PayloadJSON), &payload); err != nil {
		return BrowserMutationPayload{}, nil, fmt.Errorf("browser mutation payload is invalid: %w", err)
	}
	if payload.SchemaVersion != 1 {
		return BrowserMutationPayload{}, nil, fmt.Errorf("unsupported browser mutation payload schema_version %d", payload.SchemaVersion)
	}
	var allowedDomains []string
	if err := json.Unmarshal([]byte(request.AllowedDomains), &allowedDomains); err != nil {
		return BrowserMutationPayload{}, nil, fmt.Errorf("browser mutation allowed domains are invalid: %w", err)
	}
	if payload.ActionKind != request.ActionKind || payload.StartURL != request.StartURL {
		return BrowserMutationPayload{}, nil, fmt.Errorf("browser mutation payload does not match stored request")
	}
	if len(allowedDomains) == 0 {
		return BrowserMutationPayload{}, nil, fmt.Errorf("browser mutation allowed domains are required")
	}
	if _, err := readOnlyURLHost(request.StartURL); err != nil {
		return BrowserMutationPayload{}, nil, err
	}
	if err := validateReadOnlyTask(ReadOnlyTask{
		GoalID:             1,
		Objective:          "validate browser mutation continuation boundary",
		AllowedDomains:     allowedDomains,
		StartURLs:          []string{request.StartURL},
		MaxPages:           1,
		MaxDurationSeconds: defaultMutationDriverTimeoutSeconds,
	}, true); err != nil {
		return BrowserMutationPayload{}, nil, err
	}
	return payload, allowedDomains, nil
}

func normalizeMutationDriverResponse(response BrowserMutationDriverResponse, request sqlite.BrowserMutationRequest) BrowserMutationDriverResponse {
	if strings.TrimSpace(response.Status) == "" {
		response.Status = "failed"
	}
	if strings.TrimSpace(response.AdapterKind) == "" {
		response.AdapterKind = "huginn_browser_mutation"
	}
	if strings.TrimSpace(response.ActionKind) == "" {
		response.ActionKind = request.ActionKind
	}
	if response.Evidence == nil {
		response.Evidence = map[string]any{}
	}
	return response
}

func mutationEvidenceDetailsJSON(approval sqlite.Approval, mutationRequest sqlite.BrowserMutationRequest, driverRequest BrowserMutationDriverRequest, response BrowserMutationDriverResponse) (string, error) {
	payload := map[string]any{
		"executor":            BrowserMutationExecutor,
		"artifact_type":       BrowserMutationEvidenceType,
		"approval_id":         approval.ID,
		"task_id":             approval.TaskID,
		"action_kind":         mutationRequest.ActionKind,
		"payload_hash":        mutationRequest.PayloadHash,
		"allowed_domains":     driverRequest.AllowedDomains,
		"start_url":           mutationRequest.StartURL,
		"browser_session_id":  driverRequest.BrowserSessionID,
		"status":              response.Status,
		"adapter_kind":        response.AdapterKind,
		"final_url":           response.FinalURL,
		"intervention_reason": response.InterventionReason,
		"evidence":            response.Evidence,
		"error_code":          response.ErrorCode,
		"error_message":       response.ErrorMessage,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func defaultMutationEvidenceSummary(response BrowserMutationDriverResponse) string {
	if strings.TrimSpace(response.Summary) != "" {
		return strings.TrimSpace(response.Summary)
	}
	if response.Status == "completed" {
		return "Browser mutation continuation completed with redacted evidence."
	}
	if response.InterventionReason != "" {
		return "Browser mutation continuation requires human intervention."
	}
	return "Browser mutation continuation did not complete."
}

func mutationRunStatus(response BrowserMutationDriverResponse) (string, string) {
	switch strings.ToLower(strings.TrimSpace(response.Status)) {
	case "completed", "recorded", "ok", "success":
		return "completed", "browser_mutation_completed"
	case "intervention_required":
		return "intervention_required", "browser_intervention_required"
	default:
		return "failed", "browser_mutation_failed"
	}
}

func mutationDriverAllowed(driverPath string, allowedRaw string) bool {
	driverPath = strings.TrimSpace(driverPath)
	for _, allowed := range strings.FieldsFunc(allowedRaw, func(r rune) bool {
		return r == ',' || r == '\n'
	}) {
		if strings.TrimSpace(allowed) == driverPath {
			return true
		}
	}
	return false
}
