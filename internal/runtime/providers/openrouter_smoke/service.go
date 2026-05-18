package openrouter_smoke

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"odin-os/internal/executors/contract"
	"odin-os/internal/executors/openrouter_api"
	executorrouter "odin-os/internal/executors/router"
	"odin-os/internal/store/sqlite"
)

const (
	ActionKey    = "openrouter_live_smoke"
	ProviderKey  = "openrouter"
	ExecutorKey  = "openrouter_api"
	RequestType  = "openrouter_live_smoke_request"
	ResponseType = "openrouter_live_smoke_result"
)

type Service struct {
	Store         *sqlite.Store
	ModelRegistry executorrouter.ModelRegistry
	ProjectKey    string
	Getenv        func(string) string
	Transport     openrouter_api.Transport
	Now           func() time.Time
}

type PrepareParams struct {
	ModelKey string
}

type PrepareResult struct {
	Task             sqlite.Task
	Run              sqlite.Run
	Approval         sqlite.Approval
	RequestSHA256    string
	RedactedRequest  string
	ExactRunCommand  string
	ProviderModelID  string
	MaxOutputTokens  int
	ApprovalRequired bool
}

type RunParams struct {
	ApprovalID  int64
	ModelKey    string
	Live        bool
	ConfirmLive bool
}

type RunResult struct {
	Task            sqlite.Task
	Run             sqlite.Run
	Approval        sqlite.Approval
	RequestSHA256   string
	ProviderModelID string
	ResponseID      string
	PromptTokens    int
	OutputTokens    int
	NetworkAccess   bool
}

type EvidenceParams struct {
	ApprovalID int64
	RunID      int64
}

type EvidenceResult struct {
	ApprovalID            int64  `json:"approval_id"`
	TaskID                int64  `json:"task_id"`
	TaskKey               string `json:"task_key"`
	PrepareRunID          int64  `json:"prepare_run_id"`
	LiveRunID             *int64 `json:"live_run_id,omitempty"`
	ApprovalStatus        string `json:"approval_status"`
	Status                string `json:"status"`
	ProviderKey           string `json:"provider_key"`
	ModelKey              string `json:"model_key"`
	ProviderModelID       string `json:"provider_model_id"`
	RequestSHA256         string `json:"request_sha256"`
	ResponseID            string `json:"response_id,omitempty"`
	PromptTokens          int    `json:"prompt_tokens,omitempty"`
	CompletionTokens      int    `json:"completion_tokens,omitempty"`
	LatencyMS             int64  `json:"latency_ms,omitempty"`
	NetworkAccess         bool   `json:"network_access"`
	FixtureTransport      bool   `json:"fixture_transport"`
	Redaction             string `json:"redaction"`
	RedactionProven       bool   `json:"redaction_proven"`
	SecretLeakDetected    bool   `json:"secret_leak_detected"`
	RawPromptLeakDetected bool   `json:"raw_prompt_leak_detected"`
	RequestArtifactCount  int    `json:"request_artifact_count"`
	ResultArtifactCount   int    `json:"result_artifact_count"`
	EventCount            int    `json:"event_count"`
	RedactedRequestJSON   string `json:"redacted_request_json,omitempty"`
}

type requestArtifact struct {
	ProviderKey       string `json:"provider_key"`
	ModelKey          string `json:"model_key"`
	ProviderModelID   string `json:"provider_model_id"`
	RequestSHA256     string `json:"request_sha256"`
	RedactedRequest   string `json:"redacted_request_json"`
	FixtureTransport  bool   `json:"fixture_transport"`
	NetworkAccess     bool   `json:"network_access"`
	ApprovalRequired  bool   `json:"approval_required"`
	LiveSmokeStatus   string `json:"live_smoke_status"`
	ExactRunCommand   string `json:"exact_run_command"`
	MaxOutputTokens   int    `json:"max_output_tokens"`
	PreparedPromptSHA string `json:"prepared_prompt_sha256"`
}

type responseArtifact struct {
	ProviderKey      string `json:"provider_key"`
	ModelKey         string `json:"model_key"`
	ProviderModelID  string `json:"provider_model_id"`
	RequestSHA256    string `json:"request_sha256"`
	RedactedRequest  string `json:"redacted_request_json"`
	ResponseID       string `json:"response_id"`
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
	LatencyMS        int64  `json:"latency_ms"`
	NetworkAccess    bool   `json:"network_access"`
	FixtureTransport bool   `json:"fixture_transport"`
	LiveSmokeStatus  string `json:"live_smoke_status"`
	Redaction        string `json:"redaction"`
	ErrorSummary     string `json:"error_summary"`
}

func (service Service) Prepare(ctx context.Context, params PrepareParams) (PrepareResult, error) {
	if service.Store == nil {
		return PrepareResult{}, fmt.Errorf("openrouter smoke store is required")
	}
	model, err := service.smokeModel(params.ModelKey)
	if err != nil {
		return PrepareResult{}, err
	}
	project, err := service.project(ctx)
	if err != nil {
		return PrepareResult{}, err
	}
	now := service.now().UTC()
	taskKey := fmt.Sprintf("openrouter-live-smoke-%s", shortHash(fmt.Sprintf("%s:%s:%d", model.Key, model.LiveProviderModelID, now.UnixNano())))
	task, err := service.Store.CreateTask(ctx, sqlite.CreateTaskParams{
		ProjectID:             project.ID,
		Key:                   taskKey,
		Title:                 fmt.Sprintf("OpenRouter live smoke approval for %s", model.Key),
		AcceptanceCriteria:    []string{"Operator approval is required before any live OpenRouter network call", "OPENROUTER_API_KEY is read only after approval validation", "Artifacts and output stay redacted"},
		ActionKey:             ActionKey,
		Status:                "queued",
		Scope:                 "odin-core",
		RequestedBy:           "operator",
		WorkKind:              "provider_smoke",
		ExecutionIntent:       "governance",
		ExecutionIntentSource: "operator",
		ArtifactsJSON:         "[]",
	})
	if err != nil {
		return PrepareResult{}, err
	}
	run, err := service.Store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:     task.ID,
		Executor:   ExecutorKey,
		Attempt:    1,
		Status:     "running",
		TaskStatus: "running",
	})
	if err != nil {
		return PrepareResult{}, err
	}
	request, err := smokeRequest(task, model)
	if err != nil {
		return PrepareResult{}, err
	}
	proof, err := openrouter_api.RequestProofMetadata(request)
	if err != nil {
		return PrepareResult{}, err
	}
	artifact := requestArtifact{
		ProviderKey:       ProviderKey,
		ModelKey:          model.Key,
		ProviderModelID:   model.LiveProviderModelID,
		RequestSHA256:     proof["openrouter_request_sha256"],
		RedactedRequest:   proof["openrouter_request_json_redacted"],
		FixtureTransport:  true,
		NetworkAccess:     false,
		ApprovalRequired:  true,
		LiveSmokeStatus:   "approval_required",
		ExactRunCommand:   exactRunCommandPlaceholder(model.Key),
		MaxOutputTokens:   request.MaxTokens,
		PreparedPromptSHA: messageContentHash(request.Messages),
	}
	details, err := json.Marshal(artifact)
	if err != nil {
		return PrepareResult{}, err
	}
	if _, err := service.Store.RecordRunArtifact(ctx, sqlite.RecordRunArtifactParams{
		RunID:        run.ID,
		ArtifactType: RequestType,
		Summary:      "OpenRouter live smoke request prepared; approval required before network access",
		DetailsJSON:  string(details),
	}); err != nil {
		return PrepareResult{}, err
	}
	artifactsJSON, err := json.Marshal([]map[string]string{{
		"artifact_type":  RequestType,
		"request_sha256": artifact.RequestSHA256,
		"model_key":      artifact.ModelKey,
	}})
	if err != nil {
		return PrepareResult{}, err
	}
	run, err = service.Store.FinishRun(ctx, sqlite.FinishRunParams{
		RunID:          run.ID,
		Status:         "interrupted",
		Summary:        "OpenRouter live smoke requires operator approval",
		TerminalReason: "approval_required",
		ArtifactsJSON:  string(artifactsJSON),
	})
	if err != nil {
		return PrepareResult{}, err
	}
	task, approval, err := service.Store.BlockTaskAndRequestApproval(ctx, sqlite.BlockTaskAndRequestApprovalParams{
		TaskID:      task.ID,
		RunID:       &run.ID,
		RequestedBy: "operator",
	})
	if err != nil {
		return PrepareResult{}, err
	}
	return PrepareResult{
		Task:             task,
		Run:              run,
		Approval:         approval,
		RequestSHA256:    artifact.RequestSHA256,
		RedactedRequest:  artifact.RedactedRequest,
		ExactRunCommand:  exactRunCommand(approval.ID, model.Key),
		ProviderModelID:  artifact.ProviderModelID,
		MaxOutputTokens:  artifact.MaxOutputTokens,
		ApprovalRequired: true,
	}, nil
}

func (service Service) Run(ctx context.Context, params RunParams) (RunResult, error) {
	if service.Store == nil {
		return RunResult{}, fmt.Errorf("openrouter smoke store is required")
	}
	if !params.Live {
		return RunResult{}, fmt.Errorf("live flag is required")
	}
	if !params.ConfirmLive {
		return RunResult{}, fmt.Errorf("confirm_live_provider_call is required")
	}
	if params.ApprovalID <= 0 {
		return RunResult{}, fmt.Errorf("approval_required")
	}
	approval, err := service.Store.GetApproval(ctx, params.ApprovalID)
	if err != nil {
		return RunResult{}, err
	}
	if approval.Status != "approved" {
		return RunResult{}, fmt.Errorf("approval_%s", strings.TrimSpace(approval.Status))
	}
	if approval.RunID == nil {
		return RunResult{}, fmt.Errorf("approval_missing_prepare_run")
	}
	task, err := service.Store.GetTask(ctx, approval.TaskID)
	if err != nil {
		return RunResult{}, err
	}
	if task.ActionKey != ActionKey {
		return RunResult{}, fmt.Errorf("approval_not_openrouter_live_smoke")
	}
	artifact, err := service.requestArtifact(ctx, *approval.RunID)
	if err != nil {
		return RunResult{}, err
	}
	model, err := service.smokeModel(params.ModelKey)
	if err != nil {
		return RunResult{}, err
	}
	if artifact.ModelKey != model.Key {
		return RunResult{}, fmt.Errorf("model_mismatch")
	}
	if artifact.LiveSmokeStatus != "approval_required" || !artifact.ApprovalRequired {
		return RunResult{}, fmt.Errorf("approval_prepare_artifact_invalid")
	}
	request, err := smokeRequest(task, model)
	if err != nil {
		return RunResult{}, err
	}
	proof, err := openrouter_api.RequestProofMetadata(request)
	if err != nil {
		return RunResult{}, err
	}
	if proof["openrouter_request_sha256"] != artifact.RequestSHA256 {
		return RunResult{}, fmt.Errorf("stale_approval")
	}
	getenv := service.Getenv
	if getenv == nil {
		getenv = func(key string) string { return "" }
	}
	apiKey := strings.TrimSpace(getenv("OPENROUTER_API_KEY"))
	if apiKey == "" {
		return RunResult{}, fmt.Errorf("credential_missing")
	}
	transport := service.Transport
	if transport == nil {
		transport = openrouter_api.LiveTransport{APIKey: apiKey}
	}
	liveRun, err := service.Store.StartRun(ctx, sqlite.StartRunParams{
		TaskID:     task.ID,
		Executor:   ExecutorKey,
		Attempt:    2,
		Status:     "running",
		TaskStatus: "running",
	})
	if err != nil {
		return RunResult{}, err
	}
	started := service.now().UTC()
	response, invokeErr := transport.Invoke(ctx, request)
	latencyMS := service.now().UTC().Sub(started).Milliseconds()
	if latencyMS < 0 {
		latencyMS = 0
	}
	if invokeErr != nil {
		details := map[string]any{
			"provider_key":      ProviderKey,
			"model_key":         model.Key,
			"provider_model_id": model.LiveProviderModelID,
			"request_sha256":    artifact.RequestSHA256,
			"network_access":    true,
			"fixture_transport": false,
			"live_smoke_status": "provider_live_smoke_failed",
			"latency_ms":        latencyMS,
			"error_summary":     redactSummary(invokeErr.Error()),
		}
		encoded, _ := json.Marshal(details)
		_, _ = service.Store.RecordRunArtifact(ctx, sqlite.RecordRunArtifactParams{
			RunID:        liveRun.ID,
			ArtifactType: ResponseType,
			Summary:      "OpenRouter live smoke failed with redacted provider error",
			DetailsJSON:  string(encoded),
		})
		_, _, _ = service.Store.FinishRunAndSetTaskStatus(ctx, sqlite.FinishRunAndSetTaskStatusParams{
			RunID:          liveRun.ID,
			RunStatus:      "failed",
			Summary:        "OpenRouter live smoke failed",
			TerminalReason: "provider_live_smoke_failed",
			ArtifactsJSON:  string(encoded),
			TaskStatus:     "failed",
		})
		return RunResult{}, fmt.Errorf("provider_live_smoke_failed")
	}
	details := map[string]any{
		"provider_key":          ProviderKey,
		"model_key":             model.Key,
		"provider_model_id":     model.LiveProviderModelID,
		"request_sha256":        artifact.RequestSHA256,
		"redacted_request_json": artifact.RedactedRequest,
		"response_id":           redactSummary(response.ID),
		"prompt_tokens":         response.Usage.PromptTokens,
		"completion_tokens":     response.Usage.CompletionTokens,
		"latency_ms":            latencyMS,
		"network_access":        true,
		"fixture_transport":     false,
		"live_smoke_status":     "completed",
		"redaction":             "applied",
	}
	encoded, err := json.Marshal(details)
	if err != nil {
		return RunResult{}, err
	}
	if _, err := service.Store.RecordRunArtifact(ctx, sqlite.RecordRunArtifactParams{
		RunID:        liveRun.ID,
		ArtifactType: ResponseType,
		Summary:      "OpenRouter live smoke completed with redacted evidence",
		DetailsJSON:  string(encoded),
	}); err != nil {
		return RunResult{}, err
	}
	_, finishedRun, err := service.Store.FinishRunAndSetTaskStatus(ctx, sqlite.FinishRunAndSetTaskStatusParams{
		RunID:          liveRun.ID,
		RunStatus:      "completed",
		Summary:        "OpenRouter live smoke completed",
		TerminalReason: "",
		ArtifactsJSON:  string(encoded),
		TaskStatus:     "completed",
	})
	if err != nil {
		return RunResult{}, err
	}
	return RunResult{
		Task:            task,
		Run:             finishedRun,
		Approval:        approval,
		RequestSHA256:   artifact.RequestSHA256,
		ProviderModelID: model.LiveProviderModelID,
		ResponseID:      redactSummary(response.ID),
		PromptTokens:    response.Usage.PromptTokens,
		OutputTokens:    response.Usage.CompletionTokens,
		NetworkAccess:   true,
	}, nil
}

func (service Service) Evidence(ctx context.Context, params EvidenceParams) (EvidenceResult, error) {
	if service.Store == nil {
		return EvidenceResult{}, fmt.Errorf("openrouter smoke store is required")
	}
	if params.ApprovalID <= 0 && params.RunID <= 0 {
		return EvidenceResult{}, fmt.Errorf("approval or run id is required")
	}
	approval, err := service.evidenceApproval(ctx, params)
	if err != nil {
		return EvidenceResult{}, err
	}
	if approval.RunID == nil {
		return EvidenceResult{}, fmt.Errorf("approval_missing_prepare_run")
	}
	task, err := service.Store.GetTask(ctx, approval.TaskID)
	if err != nil {
		return EvidenceResult{}, err
	}
	if task.ActionKey != ActionKey {
		return EvidenceResult{}, fmt.Errorf("approval_not_openrouter_live_smoke")
	}
	prepareRun, err := service.Store.GetRun(ctx, *approval.RunID)
	if err != nil {
		return EvidenceResult{}, err
	}
	requestArtifacts, err := service.Store.ListRunArtifacts(ctx, sqlite.ListRunArtifactsParams{RunID: prepareRun.ID, ArtifactType: RequestType})
	if err != nil {
		return EvidenceResult{}, err
	}
	if len(requestArtifacts) == 0 {
		return EvidenceResult{}, fmt.Errorf("approval_prepare_artifact_missing")
	}
	var request requestArtifact
	if err := json.Unmarshal([]byte(requestArtifacts[len(requestArtifacts)-1].DetailsJSON), &request); err != nil {
		return EvidenceResult{}, err
	}
	liveRun, resultArtifacts, result, err := service.liveRunEvidence(ctx, task.ID, params.RunID)
	if err != nil {
		return EvidenceResult{}, err
	}
	events, err := service.Store.ListEvents(ctx, sqlite.ListEventsParams{TaskID: &task.ID})
	if err != nil {
		return EvidenceResult{}, err
	}
	scanTargets := []string{prepareRun.ArtifactsJSON, requestArtifacts[len(requestArtifacts)-1].DetailsJSON}
	for _, artifact := range resultArtifacts {
		scanTargets = append(scanTargets, artifact.DetailsJSON)
	}
	for _, event := range events {
		scanTargets = append(scanTargets, string(event.Payload))
	}
	status := "approval_" + approval.Status
	networkAccess := false
	fixtureTransport := true
	redaction := "applied"
	if liveRun != nil {
		status = liveRun.Status
		networkAccess = result.NetworkAccess
		fixtureTransport = result.FixtureTransport
		if strings.TrimSpace(result.Redaction) != "" {
			redaction = result.Redaction
		}
		scanTargets = append(scanTargets, liveRun.ArtifactsJSON)
	}
	secretLeak := containsSecretLeak(scanTargets)
	rawPromptLeak := containsRawPromptLeak(scanTargets)
	redactedRequest := request.RedactedRequest
	if strings.TrimSpace(result.RedactedRequest) != "" {
		redactedRequest = result.RedactedRequest
	}
	return EvidenceResult{
		ApprovalID:            approval.ID,
		TaskID:                task.ID,
		TaskKey:               task.Key,
		PrepareRunID:          prepareRun.ID,
		LiveRunID:             optionalRunID(liveRun),
		ApprovalStatus:        approval.Status,
		Status:                status,
		ProviderKey:           firstNonEmpty(result.ProviderKey, request.ProviderKey),
		ModelKey:              firstNonEmpty(result.ModelKey, request.ModelKey),
		ProviderModelID:       firstNonEmpty(result.ProviderModelID, request.ProviderModelID),
		RequestSHA256:         firstNonEmpty(result.RequestSHA256, request.RequestSHA256),
		ResponseID:            result.ResponseID,
		PromptTokens:          result.PromptTokens,
		CompletionTokens:      result.CompletionTokens,
		LatencyMS:             result.LatencyMS,
		NetworkAccess:         networkAccess,
		FixtureTransport:      fixtureTransport,
		Redaction:             redaction,
		RedactionProven:       !secretLeak && !rawPromptLeak && strings.Contains(redactedRequest, "[REDACTED]"),
		SecretLeakDetected:    secretLeak,
		RawPromptLeakDetected: rawPromptLeak,
		RequestArtifactCount:  len(requestArtifacts),
		ResultArtifactCount:   len(resultArtifacts),
		EventCount:            len(events),
		RedactedRequestJSON:   redactedRequest,
	}, nil
}

func (service Service) requestArtifact(ctx context.Context, runID int64) (requestArtifact, error) {
	artifacts, err := service.Store.ListRunArtifacts(ctx, sqlite.ListRunArtifactsParams{RunID: runID, ArtifactType: RequestType})
	if err != nil {
		return requestArtifact{}, err
	}
	if len(artifacts) == 0 {
		return requestArtifact{}, fmt.Errorf("approval_prepare_artifact_missing")
	}
	var artifact requestArtifact
	if err := json.Unmarshal([]byte(artifacts[len(artifacts)-1].DetailsJSON), &artifact); err != nil {
		return requestArtifact{}, err
	}
	return artifact, nil
}

func (service Service) evidenceApproval(ctx context.Context, params EvidenceParams) (sqlite.Approval, error) {
	if params.ApprovalID > 0 {
		return service.Store.GetApproval(ctx, params.ApprovalID)
	}
	run, err := service.Store.GetRun(ctx, params.RunID)
	if err != nil {
		return sqlite.Approval{}, err
	}
	row := service.Store.DB().QueryRowContext(ctx, `
		SELECT id
		FROM approvals
		WHERE task_id = ?
		ORDER BY id DESC
		LIMIT 1
	`, run.TaskID)
	var approvalID int64
	if err := row.Scan(&approvalID); err != nil {
		if err == sql.ErrNoRows {
			return sqlite.Approval{}, fmt.Errorf("approval_not_found_for_run")
		}
		return sqlite.Approval{}, err
	}
	return service.Store.GetApproval(ctx, approvalID)
}

func (service Service) liveRunEvidence(ctx context.Context, taskID int64, requestedRunID int64) (*sqlite.Run, []sqlite.RunArtifact, responseArtifact, error) {
	runIDs := make([]int64, 0)
	if requestedRunID > 0 {
		runIDs = append(runIDs, requestedRunID)
	} else {
		rows, err := service.Store.DB().QueryContext(ctx, `
			SELECT id
			FROM runs
			WHERE task_id = ?
			ORDER BY id DESC
		`, taskID)
		if err != nil {
			return nil, nil, responseArtifact{}, err
		}
		defer rows.Close()
		for rows.Next() {
			var runID int64
			if err := rows.Scan(&runID); err != nil {
				return nil, nil, responseArtifact{}, err
			}
			runIDs = append(runIDs, runID)
		}
		if err := rows.Err(); err != nil {
			return nil, nil, responseArtifact{}, err
		}
	}
	for _, runID := range runIDs {
		artifacts, err := service.Store.ListRunArtifacts(ctx, sqlite.ListRunArtifactsParams{RunID: runID, ArtifactType: ResponseType})
		if err != nil {
			return nil, nil, responseArtifact{}, err
		}
		if len(artifacts) == 0 {
			continue
		}
		run, err := service.Store.GetRun(ctx, runID)
		if err != nil {
			return nil, nil, responseArtifact{}, err
		}
		var result responseArtifact
		if err := json.Unmarshal([]byte(artifacts[len(artifacts)-1].DetailsJSON), &result); err != nil {
			return nil, nil, responseArtifact{}, err
		}
		return &run, artifacts, result, nil
	}
	return nil, nil, responseArtifact{}, nil
}

func (service Service) smokeModel(modelKey string) (executorrouter.ModelConfig, error) {
	modelKey = strings.TrimSpace(modelKey)
	if modelKey == "" {
		modelKey = "openrouter-kimi-k2-6"
	}
	model, ok := service.ModelRegistry.ModelByKey(modelKey)
	if !ok {
		return executorrouter.ModelConfig{}, fmt.Errorf("model_not_configured")
	}
	if !model.IsEnabled() {
		return executorrouter.ModelConfig{}, fmt.Errorf("model_disabled")
	}
	if model.Provider != ProviderKey || model.Adapter != ExecutorKey {
		return executorrouter.ModelConfig{}, fmt.Errorf("model_not_openrouter")
	}
	if model.AllowHighRisk {
		return executorrouter.ModelConfig{}, fmt.Errorf("model_high_risk_enabled")
	}
	if strings.TrimSpace(model.LiveProviderModelID) == "" {
		return executorrouter.ModelConfig{}, fmt.Errorf("live_provider_model_id_missing")
	}
	for _, blocked := range model.BlockedTaskClasses {
		if strings.EqualFold(strings.TrimSpace(blocked), "provider_smoke") {
			return executorrouter.ModelConfig{}, fmt.Errorf("model_blocks_provider_smoke")
		}
	}
	return model, nil
}

func (service Service) project(ctx context.Context) (sqlite.Project, error) {
	key := strings.TrimSpace(service.ProjectKey)
	if key == "" {
		key = "odin-core"
	}
	return service.Store.GetProjectByKey(ctx, key)
}

func (service Service) now() time.Time {
	if service.Now != nil {
		return service.Now()
	}
	return time.Now()
}

func smokeRequest(task sqlite.Task, model executorrouter.ModelConfig) (openrouter_api.ChatCompletionRequest, error) {
	maxOutputTokens := model.MaxOutputTokens
	if maxOutputTokens <= 0 || maxOutputTokens > 32 {
		maxOutputTokens = 32
	}
	return openrouter_api.BuildChatCompletionRequest(contract.TaskSpec{
		ID:        task.Key,
		Kind:      contract.TaskKindQA,
		Scope:     task.Scope,
		TaskClass: "provider_smoke",
		RiskClass: "low",
		Prompt:    "OpenRouter live smoke. Reply with exactly: odin-openrouter-live-smoke-ok",
		Metadata: map[string]string{
			"model_key":         model.Key,
			"provider_key":      ProviderKey,
			"provider_model_id": model.LiveProviderModelID,
		},
		Budget: contract.BudgetHints{
			MaxOutputTokens: maxOutputTokens,
			MaxCostUSD:      0.01,
		},
	})
}

func exactRunCommand(approvalID int64, modelKey string) string {
	return fmt.Sprintf("OPENROUTER_API_KEY=<local-secret> ./bin/odin provider openrouter smoke run --approval %d --model %s --live --confirm-live-provider-call --json", approvalID, modelKey)
}

func exactRunCommandPlaceholder(modelKey string) string {
	return fmt.Sprintf("OPENROUTER_API_KEY=<local-secret> ./bin/odin provider openrouter smoke run --approval <approval-id> --model %s --live --confirm-live-provider-call --json", modelKey)
}

func messageContentHash(messages []openrouter_api.ChatMessage) string {
	hash := sha256.New()
	for _, message := range messages {
		hash.Write([]byte(message.Role))
		hash.Write([]byte{0})
		hash.Write([]byte(message.Content))
		hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func shortHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])[:12]
}

func redactSummary(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	parts := strings.Fields(value)
	for index, part := range parts {
		normalized := strings.ToLower(part)
		if strings.HasPrefix(normalized, "sk-") || strings.HasPrefix(normalized, "bearer ") || strings.Contains(normalized, "openrouter_api_key") {
			parts[index] = "[REDACTED]"
		}
	}
	redacted := strings.Join(parts, " ")
	if strings.Contains(strings.ToLower(redacted), "sk-") {
		return "[REDACTED]"
	}
	return redacted
}

func optionalRunID(run *sqlite.Run) *int64 {
	if run == nil {
		return nil
	}
	runID := run.ID
	return &runID
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func containsSecretLeak(values []string) bool {
	for _, value := range values {
		normalized := strings.ToLower(value)
		if strings.Contains(normalized, "sk-or-") ||
			strings.Contains(normalized, "sk-live-secret") ||
			strings.Contains(normalized, "bearer sk-") ||
			strings.Contains(normalized, "openrouter_api_key\":\"sk-") ||
			strings.Contains(normalized, "openrouter_api_key=sk-") {
			return true
		}
	}
	return false
}

func containsRawPromptLeak(values []string) bool {
	for _, value := range values {
		if strings.Contains(value, "OpenRouter live smoke. Reply") ||
			strings.Contains(value, "odin-openrouter-live-smoke-ok") {
			return true
		}
	}
	return false
}
