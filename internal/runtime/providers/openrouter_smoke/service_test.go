package openrouter_smoke

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"odin-os/internal/executors/openrouter_api"
	executorrouter "odin-os/internal/executors/router"
	approvalsvc "odin-os/internal/runtime/approvals"
	"odin-os/internal/store/sqlite"
)

type fakeTransport struct {
	called  bool
	request openrouter_api.ChatCompletionRequest
}

func (transport *fakeTransport) Invoke(_ context.Context, request openrouter_api.ChatCompletionRequest) (openrouter_api.ChatCompletionResponse, error) {
	transport.called = true
	transport.request = request
	return openrouter_api.ChatCompletionResponse{
		ID: "fake-live-response",
		Choices: []openrouter_api.Choice{
			{Message: openrouter_api.ChatMessage{Role: "assistant", Content: "odin-openrouter-live-smoke-ok"}},
		},
		Usage: openrouter_api.TokenUsage{PromptTokens: 7, CompletionTokens: 3},
	}, nil
}

func TestPrepareCreatesApprovalBackedFixtureRequest(t *testing.T) {
	ctx := context.Background()
	service, store := newTestService(t, nil, nil)

	result, err := service.Prepare(ctx, PrepareParams{ModelKey: "openrouter-kimi-k2-6"})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if result.Approval.Status != "pending" || result.Approval.RunID == nil || *result.Approval.RunID != result.Run.ID {
		t.Fatalf("approval = %+v run=%d, want pending linked approval", result.Approval, result.Run.ID)
	}
	if result.Run.Status != "interrupted" || result.Task.Status != "blocked" || result.Task.BlockedReason != "approval_required" {
		t.Fatalf("task/run status task=%s blocked_reason=%s run=%s, want blocked approval wait", result.Task.Status, result.Task.BlockedReason, result.Run.Status)
	}
	artifacts, err := store.ListRunArtifacts(ctx, sqlite.ListRunArtifactsParams{RunID: result.Run.ID, ArtifactType: RequestType})
	if err != nil {
		t.Fatalf("ListRunArtifacts() error = %v", err)
	}
	if len(artifacts) != 1 {
		t.Fatalf("request artifacts len = %d, want 1", len(artifacts))
	}
	details := artifacts[0].DetailsJSON
	for _, want := range []string{`"provider_key":"openrouter"`, `"model_key":"openrouter-kimi-k2-6"`, `"provider_model_id":"moonshotai/kimi-k2-thinking"`, `"network_access":false`, `"fixture_transport":true`, `"approval_required":true`, `"live_smoke_status":"approval_required"`} {
		if !strings.Contains(details, want) {
			t.Fatalf("artifact details = %s, want %s", details, want)
		}
	}
	if strings.Contains(details, "OpenRouter live smoke. Reply") || strings.Contains(details, "fixture-token") {
		t.Fatalf("artifact details leaked prompt or token: %s", details)
	}
}

func TestRunRejectsPendingApprovalBeforeCredentialRead(t *testing.T) {
	ctx := context.Background()
	getenvCalls := 0
	service, _ := newTestService(t, func(string) string {
		getenvCalls++
		return "sk-live-secret-for-test"
	}, &fakeTransport{})
	prepare, err := service.Prepare(ctx, PrepareParams{})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}

	_, err = service.Run(ctx, RunParams{ApprovalID: prepare.Approval.ID, ModelKey: "openrouter-kimi-k2-6", Live: true, ConfirmLive: true})
	if err == nil || !strings.Contains(err.Error(), "approval_pending") {
		t.Fatalf("Run() error = %v, want approval_pending", err)
	}
	if getenvCalls != 0 {
		t.Fatalf("getenv calls = %d, want 0 before approval validation passes", getenvCalls)
	}
}

func TestRunRequiresCredentialAfterApprovalValidation(t *testing.T) {
	ctx := context.Background()
	getenvCalls := 0
	service, _ := newTestService(t, func(string) string {
		getenvCalls++
		return ""
	}, &fakeTransport{})
	prepare, err := service.Prepare(ctx, PrepareParams{})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if _, err := (approvalsvc.Service{Store: service.Store}).Resolve(ctx, approvalsvc.ResolveParams{
		ApprovalID: prepare.Approval.ID,
		Action:     "approve",
		DecisionBy: "operator",
		Reason:     "approved for one request",
	}); err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	_, err = service.Run(ctx, RunParams{ApprovalID: prepare.Approval.ID, ModelKey: "openrouter-kimi-k2-6", Live: true, ConfirmLive: true})
	if err == nil || !strings.Contains(err.Error(), "credential_missing") {
		t.Fatalf("Run() error = %v, want credential_missing", err)
	}
	if getenvCalls != 1 {
		t.Fatalf("getenv calls = %d, want 1 after approval validation", getenvCalls)
	}
}

func TestRunUsesInjectedTransportAndRedactsSentinelSecret(t *testing.T) {
	ctx := context.Background()
	transport := &fakeTransport{}
	service, store := newTestService(t, func(key string) string {
		if key != "OPENROUTER_API_KEY" {
			t.Fatalf("getenv(%q), want OPENROUTER_API_KEY", key)
		}
		return "sk-live-secret-for-test"
	}, transport)
	prepare, err := service.Prepare(ctx, PrepareParams{})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if _, err := (approvalsvc.Service{Store: service.Store}).Resolve(ctx, approvalsvc.ResolveParams{
		ApprovalID: prepare.Approval.ID,
		Action:     "approve",
		DecisionBy: "operator",
		Reason:     "approved for one request",
	}); err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	result, err := service.Run(ctx, RunParams{ApprovalID: prepare.Approval.ID, ModelKey: "openrouter-kimi-k2-6", Live: true, ConfirmLive: true})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !transport.called {
		t.Fatal("transport was not called")
	}
	if result.Run.Status != "completed" || !result.NetworkAccess || result.ResponseID != "fake-live-response" {
		t.Fatalf("result = %+v, want completed live smoke", result)
	}
	if transport.request.Model != "moonshotai/kimi-k2-thinking" {
		t.Fatalf("transport model = %q, want live OpenRouter model", transport.request.Model)
	}
	artifacts, err := store.ListRunArtifacts(ctx, sqlite.ListRunArtifactsParams{RunID: result.Run.ID, ArtifactType: ResponseType})
	if err != nil {
		t.Fatalf("ListRunArtifacts(response) error = %v", err)
	}
	if len(artifacts) != 1 {
		t.Fatalf("response artifacts len = %d, want 1", len(artifacts))
	}
	for _, output := range []string{artifacts[0].DetailsJSON, result.Run.ArtifactsJSON} {
		if strings.Contains(output, "sk-live-secret-for-test") || strings.Contains(output, "OpenRouter live smoke. Reply") {
			t.Fatalf("live smoke output leaked secret or prompt: %s", output)
		}
	}
}

func TestEvidenceSummarizesCompletedSmokeAndProvesRedaction(t *testing.T) {
	ctx := context.Background()
	transport := &fakeTransport{}
	service, _ := newTestService(t, func(string) string {
		return "sk-live-secret-for-test"
	}, transport)
	prepare, err := service.Prepare(ctx, PrepareParams{})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if _, err := (approvalsvc.Service{Store: service.Store}).Resolve(ctx, approvalsvc.ResolveParams{
		ApprovalID: prepare.Approval.ID,
		Action:     "approve",
		DecisionBy: "operator",
		Reason:     "approved for one request",
	}); err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	run, err := service.Run(ctx, RunParams{ApprovalID: prepare.Approval.ID, ModelKey: "openrouter-kimi-k2-6", Live: true, ConfirmLive: true})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	evidence, err := service.Evidence(ctx, EvidenceParams{ApprovalID: prepare.Approval.ID})
	if err != nil {
		t.Fatalf("Evidence() error = %v", err)
	}
	if evidence.ApprovalID != prepare.Approval.ID || evidence.PrepareRunID != prepare.Run.ID || evidence.LiveRunID == nil || *evidence.LiveRunID != run.Run.ID {
		t.Fatalf("evidence ids = %+v, want approval/prepare/live run linkage", evidence)
	}
	if evidence.Status != "completed" || !evidence.NetworkAccess || !evidence.RedactionProven || evidence.SecretLeakDetected || evidence.RawPromptLeakDetected {
		t.Fatalf("evidence summary = %+v, want completed redacted live smoke", evidence)
	}
	if evidence.EventCount == 0 || evidence.ResultArtifactCount != 1 || evidence.RequestArtifactCount != 1 {
		t.Fatalf("evidence counts = %+v, want events and request/result artifacts", evidence)
	}
	if strings.Contains(evidence.RedactedRequestJSON, "OpenRouter live smoke. Reply") || strings.Contains(evidence.RedactedRequestJSON, "sk-live-secret-for-test") {
		t.Fatalf("redacted request leaked sensitive content: %s", evidence.RedactedRequestJSON)
	}
}

func TestEvidenceByRunFindsLinkedApproval(t *testing.T) {
	ctx := context.Background()
	service, _ := newTestService(t, func(string) string {
		return "sk-live-secret-for-test"
	}, &fakeTransport{})
	prepare, err := service.Prepare(ctx, PrepareParams{})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if _, err := (approvalsvc.Service{Store: service.Store}).Resolve(ctx, approvalsvc.ResolveParams{
		ApprovalID: prepare.Approval.ID,
		Action:     "approve",
		DecisionBy: "operator",
		Reason:     "approved for one request",
	}); err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	run, err := service.Run(ctx, RunParams{ApprovalID: prepare.Approval.ID, ModelKey: "openrouter-kimi-k2-6", Live: true, ConfirmLive: true})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	evidence, err := service.Evidence(ctx, EvidenceParams{RunID: run.Run.ID})
	if err != nil {
		t.Fatalf("Evidence(run) error = %v", err)
	}
	if evidence.ApprovalID != prepare.Approval.ID || evidence.LiveRunID == nil || *evidence.LiveRunID != run.Run.ID {
		t.Fatalf("evidence = %+v, want approval found from live run", evidence)
	}
}

func newTestService(t *testing.T, getenv func(string) string, transport openrouter_api.Transport) (Service, *sqlite.Store) {
	t.Helper()
	ctx := context.Background()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "odin.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	if _, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
		Key:           "odin-core",
		Name:          "Odin Core",
		Scope:         "system",
		GitRoot:       t.TempDir(),
		DefaultBranch: "main",
		ManifestPath:  "config/projects.yaml",
	}); err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	return Service{
		Store:         store,
		ModelRegistry: testModelRegistry(),
		ProjectKey:    "odin-core",
		Getenv:        getenv,
		Transport:     transport,
	}, store
}

func testModelRegistry() executorrouter.ModelRegistry {
	return executorrouter.ModelRegistry{
		Version: 1,
		Models: []executorrouter.ModelConfig{
			{
				Key:                  "openrouter-kimi-k2-6",
				Provider:             "openrouter",
				ProviderModelID:      "fixture/openrouter-kimi-k2-6",
				LiveProviderModelID:  "moonshotai/kimi-k2-thinking",
				Access:               "broker",
				Adapter:              "openrouter_api",
				Capabilities:         []string{"code"},
				SupportedTaskClasses: []string{"provider_smoke"},
				ContextWindowTokens:  128000,
				MaxOutputTokens:      32,
				LatencyTier:          "batch",
				RiskTier:             "external_grunt",
				BlockedTaskClasses:   []string{"finance", "legal", "medical", "security_decision", "approval_resolution", "production_deploy", "public_publish"},
			},
		},
	}
}
