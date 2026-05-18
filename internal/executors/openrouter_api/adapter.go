package openrouter_api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"odin-os/internal/executors/contract"
)

const (
	executorKey = "openrouter_api"
	providerKey = "openrouter"
)

type Executor struct {
	transport Transport
	now       func() time.Time
}

type Transport interface {
	Invoke(context.Context, ChatCompletionRequest) (ChatCompletionResponse, error)
}

type FixtureTransport struct{}

type LiveTransport struct {
	Endpoint string
	APIKey   string
	Client   *http.Client
}

type ChatCompletionRequest struct {
	Model       string            `json:"model"`
	Messages    []ChatMessage     `json:"messages"`
	MaxTokens   int               `json:"max_tokens,omitempty"`
	Stream      bool              `json:"stream"`
	Temperature float64           `json:"temperature"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	Headers     map[string]string `json:"-"`
}

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatCompletionResponse struct {
	ID      string     `json:"id"`
	Choices []Choice   `json:"choices"`
	Usage   TokenUsage `json:"usage"`
}

type Choice struct {
	Message ChatMessage `json:"message"`
}

type TokenUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

func New() contract.Executor {
	return NewWithTransport(FixtureTransport{})
}

func NewWithTransport(transport Transport) contract.Executor {
	if transport == nil {
		transport = FixtureTransport{}
	}
	return Executor{transport: transport, now: time.Now}
}

func (executor Executor) Key() string {
	return executorKey
}

func (executor Executor) Class() contract.ExecutorClass {
	return contract.ExecutorClassBroker
}

func (executor Executor) Health(context.Context) (contract.HealthReport, error) {
	now := executor.now
	if now == nil {
		now = time.Now
	}
	return contract.HealthReport{
		Status:    contract.HealthStatusHealthy,
		Details:   "fixture_only_openrouter_invocation",
		CheckedAt: now().UTC(),
	}, nil
}

func (executor Executor) Capabilities(context.Context) (contract.Capabilities, error) {
	return contract.Capabilities{
		ExecutorClass:         contract.ExecutorClassBroker,
		SupportsResume:        true,
		SupportsCancel:        true,
		SupportsTools:         true,
		SupportsCostEstimate:  true,
		SupportsBrokerRouting: true,
		TaskKinds: []contract.TaskKind{
			contract.TaskKindGeneral,
			contract.TaskKindPlan,
			contract.TaskKindBuild,
			contract.TaskKindReview,
			contract.TaskKindQA,
			contract.TaskKindResearch,
		},
		Scopes: []string{"global", "odin-core", "project", "new-project"},
	}, nil
}

func (executor Executor) RunTask(ctx context.Context, spec contract.TaskSpec) (contract.ExecutionResult, error) {
	request, err := BuildChatCompletionRequest(spec)
	if err != nil {
		return contract.ExecutionResult{}, err
	}
	response, err := executor.transport.Invoke(ctx, request)
	if err != nil {
		return contract.ExecutionResult{}, err
	}
	metadata, err := RequestProofMetadata(request)
	if err != nil {
		return contract.ExecutionResult{}, err
	}
	metadata["provider_key"] = providerKey
	metadata["provider_model_id"] = request.Model
	metadata["network_access"] = "false"
	metadata["fixture_transport"] = "true"
	metadata["redaction"] = "applied"
	return contract.ExecutionResult{
		Handle: contract.TaskHandle{
			ExecutorKey: executorKey,
			ExternalID:  response.ID,
			Status:      "completed",
		},
		Status:   "completed",
		Output:   responseOutput(response),
		Metadata: metadata,
	}, nil
}

func (executor Executor) ResumeTask(context.Context, contract.TaskHandle, contract.ResumePacket) (contract.ExecutionResult, error) {
	return contract.ExecutionResult{}, contract.ErrNotImplemented
}

func (executor Executor) CancelTask(context.Context, contract.TaskHandle) error {
	return contract.ErrNotImplemented
}

func (executor Executor) EstimateCost(_ context.Context, spec contract.TaskSpec) (contract.CostEstimate, error) {
	return contract.CostEstimate{
		InputTokens:  spec.Budget.MaxInputTokens,
		OutputTokens: spec.Budget.MaxOutputTokens,
		EstimatedUSD: spec.Budget.MaxCostUSD,
		Currency:     "USD",
	}, nil
}

func BuildChatCompletionRequest(spec contract.TaskSpec) (ChatCompletionRequest, error) {
	model := strings.TrimSpace(spec.Metadata["provider_model_id"])
	if model == "" {
		model = strings.TrimSpace(spec.Metadata["model_key"])
	}
	if model == "" {
		return ChatCompletionRequest{}, fmt.Errorf("openrouter model metadata is required")
	}
	prompt := strings.TrimSpace(spec.Prompt)
	if prompt == "" {
		return ChatCompletionRequest{}, fmt.Errorf("openrouter prompt is required")
	}
	metadata := map[string]string{
		"task_id":              strings.TrimSpace(spec.ID),
		"task_kind":            strings.TrimSpace(string(spec.Kind)),
		"task_class":           strings.TrimSpace(spec.TaskClass),
		"risk_class":           strings.TrimSpace(spec.RiskClass),
		"fixture_prompt_shape": fixturePromptShape(spec),
	}
	for key, value := range metadata {
		if value == "" {
			delete(metadata, key)
		}
	}
	return ChatCompletionRequest{
		Model: model,
		Messages: []ChatMessage{
			{
				Role:    "system",
				Content: fixtureSystemPrompt(spec),
			},
			{
				Role:    "user",
				Content: prompt,
			},
		},
		MaxTokens:   spec.Budget.MaxOutputTokens,
		Stream:      spec.Requirements.NeedsStreaming,
		Temperature: 0,
		Metadata:    metadata,
		Headers: map[string]string{
			"Authorization": "Bearer fixture-token",
			"Content-Type":  "application/json",
			"HTTP-Referer":  "odin-os://fixture",
			"X-Title":       "Odin OS fixture",
		},
	}, nil
}

func fixtureSystemPrompt(spec contract.TaskSpec) string {
	switch strings.ToLower(strings.TrimSpace(spec.TaskClass)) {
	case "backend_build":
		return "You are an OpenRouter-compatible fixture executor for low-risk backend implementation grunt work. Return deterministic test output only. Do not make policy, approval, credential, deployment, security, finance, legal, or production judgment decisions. Do not handle credentials or secrets."
	case "frontend_build":
		return "You are an OpenRouter-compatible fixture executor for low-risk frontend implementation grunt work. Return deterministic test output only. Do not make policy, approval, credential, deployment, security, finance, legal, or production judgment decisions."
	default:
		return "You are an OpenRouter-compatible fixture executor. Return deterministic test output only."
	}
}

func fixturePromptShape(spec contract.TaskSpec) string {
	if !strings.EqualFold(strings.TrimSpace(spec.RiskClass), "low") {
		return "non_low_risk_fixture"
	}
	switch strings.ToLower(strings.TrimSpace(spec.TaskClass)) {
	case "backend_build":
		return "low_risk_backend_build"
	case "frontend_build":
		return "low_risk_frontend_build"
	default:
		return ""
	}
}

func RequestProofMetadata(request ChatCompletionRequest) (map[string]string, error) {
	payload, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	redacted, err := json.Marshal(RedactedRequest(request))
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(payload)
	return map[string]string{
		"openrouter_request_constructed":      "true",
		"openrouter_request_sha256":           hex.EncodeToString(digest[:]),
		"openrouter_request_json_redacted":    string(redacted),
		"openrouter_request_message_count":    fmt.Sprintf("%d", len(request.Messages)),
		"openrouter_request_streaming":        fmt.Sprintf("%t", request.Stream),
		"openrouter_request_max_tokens":       fmt.Sprintf("%d", request.MaxTokens),
		"openrouter_request_redaction_policy": "headers_and_message_content",
	}, nil
}

func RedactedRequest(request ChatCompletionRequest) map[string]any {
	headers := make(map[string]string, len(request.Headers))
	for key, value := range request.Headers {
		if isSensitiveField(key) || isSensitiveValue(value) {
			headers[key] = "[REDACTED]"
			continue
		}
		headers[key] = value
	}
	messages := make([]map[string]string, 0, len(request.Messages))
	for _, message := range request.Messages {
		digest := sha256.Sum256([]byte(message.Content))
		messages = append(messages, map[string]string{
			"role":           message.Role,
			"content":        "[REDACTED]",
			"content_sha256": hex.EncodeToString(digest[:]),
			"content_bytes":  fmt.Sprintf("%d", len(message.Content)),
		})
	}
	return map[string]any{
		"headers": headers,
		"body": map[string]any{
			"model":       request.Model,
			"messages":    messages,
			"max_tokens":  request.MaxTokens,
			"stream":      request.Stream,
			"temperature": request.Temperature,
			"metadata":    redactMetadata(request.Metadata),
		},
	}
}

func (FixtureTransport) Invoke(context.Context, ChatCompletionRequest) (ChatCompletionResponse, error) {
	return ChatCompletionResponse{
		ID: "fixture-openrouter-invocation",
		Choices: []Choice{
			{
				Message: ChatMessage{
					Role:    "assistant",
					Content: "fixture OpenRouter response",
				},
			},
		},
		Usage: TokenUsage{
			PromptTokens:     1,
			CompletionTokens: 1,
		},
	}, nil
}

func (transport LiveTransport) Invoke(ctx context.Context, request ChatCompletionRequest) (ChatCompletionResponse, error) {
	endpoint := strings.TrimSpace(transport.Endpoint)
	if endpoint == "" {
		endpoint = "https://openrouter.ai/api/v1/chat/completions"
	}
	apiKey := strings.TrimSpace(transport.APIKey)
	if apiKey == "" {
		return ChatCompletionResponse{}, fmt.Errorf("openrouter api key is required")
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return ChatCompletionResponse{}, err
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return ChatCompletionResponse{}, err
	}
	for key, value := range request.Headers {
		if strings.EqualFold(key, "Authorization") {
			continue
		}
		httpRequest.Header.Set(key, value)
	}
	httpRequest.Header.Set("Authorization", "Bearer "+apiKey)
	httpRequest.Header.Set("Content-Type", "application/json")

	client := transport.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	response, err := client.Do(httpRequest)
	if err != nil {
		return ChatCompletionResponse{}, err
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return ChatCompletionResponse{}, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return ChatCompletionResponse{}, fmt.Errorf("openrouter live smoke failed: http_status=%d body_sha256=%s", response.StatusCode, sha256Hex(body))
	}
	var decoded ChatCompletionResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return ChatCompletionResponse{}, err
	}
	return decoded, nil
}

func responseOutput(response ChatCompletionResponse) string {
	for _, choice := range response.Choices {
		if content := strings.TrimSpace(choice.Message.Content); content != "" {
			return content
		}
	}
	return "fixture OpenRouter response"
}

func sha256Hex(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func redactMetadata(metadata map[string]string) map[string]string {
	if len(metadata) == 0 {
		return nil
	}
	redacted := make(map[string]string, len(metadata))
	for key, value := range metadata {
		if isSensitiveField(key) || isSensitiveValue(value) {
			redacted[key] = "[REDACTED]"
			continue
		}
		redacted[key] = value
	}
	return redacted
}

func isSensitiveField(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	for _, marker := range []string{"authorization", "token", "secret", "password", "passwd", "api_key", "apikey", "access_key", "private_key", "credential"} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}

func isSensitiveValue(value string) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	return strings.HasPrefix(normalized, "bearer ") ||
		strings.HasPrefix(normalized, "sk-") ||
		strings.HasPrefix(normalized, "ghp_")
}
