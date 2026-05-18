package openrouter_api

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"odin-os/internal/executors/contract"
)

func TestRunTaskConstructsFixtureRequestAndRedactsProof(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "sk-env-secret")

	executor := New()
	result, err := executor.RunTask(context.Background(), contract.TaskSpec{
		ID:        "task-1",
		Kind:      contract.TaskKindBuild,
		Scope:     "project",
		TaskClass: "frontend_build",
		RiskClass: "low",
		Prompt:    "Build the dashboard. fixture-secret sk-test-secret",
		Budget: contract.BudgetHints{
			MaxOutputTokens: 7000,
		},
		Metadata: map[string]string{
			"model_key":         "openrouter-kimi-k2-6",
			"provider_model_id": "fixture/openrouter-kimi-k2-6",
		},
	})
	if err != nil {
		t.Fatalf("RunTask() error = %v", err)
	}
	if result.Status != "completed" {
		t.Fatalf("Status = %q, want completed", result.Status)
	}
	if result.Handle.ExecutorKey != executorKey || result.Handle.ExternalID != "fixture-openrouter-invocation" {
		t.Fatalf("Handle = %+v, want fixture OpenRouter handle", result.Handle)
	}
	if result.Metadata["network_access"] != "false" || result.Metadata["fixture_transport"] != "true" {
		t.Fatalf("metadata = %+v, want fixture/no-network proof", result.Metadata)
	}
	if result.Metadata["provider_model_id"] != "fixture/openrouter-kimi-k2-6" {
		t.Fatalf("provider_model_id = %q, want fixture model", result.Metadata["provider_model_id"])
	}

	proof := result.Metadata["openrouter_request_json_redacted"]
	encodedMetadata, err := json.Marshal(result.Metadata)
	if err != nil {
		t.Fatalf("Marshal(metadata) error = %v", err)
	}
	for _, forbidden := range []string{"Build the dashboard", "fixture-secret", "sk-test-secret", "fixture-token", "sk-env-secret"} {
		if strings.Contains(proof, forbidden) {
			t.Fatalf("redacted proof leaked %q in %s", forbidden, proof)
		}
		if strings.Contains(string(encodedMetadata), forbidden) {
			t.Fatalf("metadata leaked %q in %s", forbidden, encodedMetadata)
		}
	}
	for _, required := range []string{"fixture/openrouter-kimi-k2-6", "[REDACTED]", "content_sha256", "max_tokens"} {
		if !strings.Contains(proof, required) {
			t.Fatalf("redacted proof missing %q in %s", required, proof)
		}
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(proof), &decoded); err != nil {
		t.Fatalf("redacted proof is not JSON: %v", err)
	}
}

func TestBuildChatCompletionRequestRequiresModelMetadata(t *testing.T) {
	t.Parallel()

	_, err := BuildChatCompletionRequest(contract.TaskSpec{
		ID:     "task-2",
		Kind:   contract.TaskKindBuild,
		Scope:  "project",
		Prompt: "Build dashboard",
	})
	if err == nil || !strings.Contains(err.Error(), "model metadata") {
		t.Fatalf("BuildChatCompletionRequest() error = %v, want model metadata failure", err)
	}
}

func TestBuildChatCompletionRequestShapesLowRiskBackendFixture(t *testing.T) {
	t.Parallel()

	request, err := BuildChatCompletionRequest(contract.TaskSpec{
		ID:        "task-backend",
		Kind:      contract.TaskKindBuild,
		Scope:     "project",
		TaskClass: "backend_build",
		RiskClass: "low",
		Prompt:    "Implement a read-only API fixture",
		Budget: contract.BudgetHints{
			MaxOutputTokens: 4000,
		},
		Metadata: map[string]string{
			"model_key":         "openrouter-kimi-k2-6",
			"provider_model_id": "fixture/openrouter-kimi-k2-6",
		},
	})
	if err != nil {
		t.Fatalf("BuildChatCompletionRequest() error = %v", err)
	}
	if got := request.Metadata["fixture_prompt_shape"]; got != "low_risk_backend_build" {
		t.Fatalf("fixture_prompt_shape = %q, want low_risk_backend_build", got)
	}
	if len(request.Messages) != 2 || request.Messages[0].Role != "system" || request.Messages[1].Role != "user" {
		t.Fatalf("messages = %+v, want system and user fixture messages", request.Messages)
	}
	system := request.Messages[0].Content
	for _, want := range []string{"low-risk backend implementation", "Do not make policy", "Do not handle credentials", "fixture"} {
		if !strings.Contains(system, want) {
			t.Fatalf("backend system prompt = %q, want %q", system, want)
		}
	}
	if strings.Contains(system, "frontend") {
		t.Fatalf("backend system prompt = %q, should not use frontend shape", system)
	}
	proofBytes, err := json.Marshal(RedactedRequest(request))
	if err != nil {
		t.Fatalf("Marshal(redacted request) error = %v", err)
	}
	proof := string(proofBytes)
	if strings.Contains(proof, "Implement a read-only API fixture") || strings.Contains(proof, system) {
		t.Fatalf("redacted proof leaked prompt content: %s", proof)
	}
	if !strings.Contains(proof, `"fixture_prompt_shape":"low_risk_backend_build"`) {
		t.Fatalf("redacted proof = %s, want backend fixture prompt shape metadata", proof)
	}
}

func TestRedactedRequestRedactsSensitiveMetadataAndHeaders(t *testing.T) {
	t.Parallel()

	redacted := RedactedRequest(ChatCompletionRequest{
		Model: "fixture/openrouter-kimi-k2-6",
		Messages: []ChatMessage{
			{Role: "user", Content: "secret prompt"},
		},
		Metadata: map[string]string{
			"task_id": "task-3",
			"api_key": "sk-hidden",
		},
		Headers: map[string]string{
			"Authorization": "Bearer sk-hidden",
			"X-Title":       "Odin OS fixture",
		},
	})
	encoded, err := json.Marshal(redacted)
	if err != nil {
		t.Fatalf("Marshal(redacted) error = %v", err)
	}
	proof := string(encoded)
	for _, forbidden := range []string{"secret prompt", "sk-hidden", "Bearer sk-hidden"} {
		if strings.Contains(proof, forbidden) {
			t.Fatalf("redacted request leaked %q in %s", forbidden, proof)
		}
	}
	if !strings.Contains(proof, `"api_key":"[REDACTED]"`) || !strings.Contains(proof, `"Authorization":"[REDACTED]"`) {
		t.Fatalf("redacted request = %s, want sensitive fields redacted", proof)
	}
}
