package huginnbrowser

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStubAdapterReturnsDeterministicReadOnlyEvidence(t *testing.T) {
	response, err := StubAdapter{}.Run(context.Background(), Request{
		Objective:          "Collect public documentation",
		StartURLs:          []string{"https://example.com/docs"},
		AllowedDomains:     []string{"example.com"},
		MaxPages:           2,
		MaxDurationSeconds: 30,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if response.Status != "completed" {
		t.Fatalf("Status = %q, want completed", response.Status)
	}
	if response.AdapterKind != "stub_local" {
		t.Fatalf("AdapterKind = %q, want stub_local", response.AdapterKind)
	}
	if len(response.VisitedURLs) != 1 || response.VisitedURLs[0] != "https://example.com/docs" {
		t.Fatalf("VisitedURLs = %#v, want requested URL", response.VisitedURLs)
	}
	if response.ExtractedTextSummary == "" || len(response.ActionLog) == 0 {
		t.Fatalf("response = %+v, want summary and action log", response)
	}
}

func TestStubAdapterDoesNotAcceptEmptyRequest(t *testing.T) {
	if _, err := (StubAdapter{}).Run(context.Background(), Request{}); err == nil {
		t.Fatal("Run() error = nil, want validation failure")
	}
}

func TestLiveAdapterReturnsStructuredNotImplementedWithoutBrowsing(t *testing.T) {
	script := writeHuginnBrowserFixture(t, `#!/usr/bin/env bash
cat >/dev/null
exit 0
`)
	response, err := LiveAdapter{Command: script, Timeout: time.Second, AllowedCommands: []string{script}}.Run(context.Background(), Request{
		Objective:          "Collect public documentation",
		StartURLs:          []string{"https://example.com/docs"},
		AllowedDomains:     []string{"example.com"},
		MaxPages:           2,
		MaxDurationSeconds: 30,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if response.Status != "not_implemented" {
		t.Fatalf("Status = %q, want not_implemented", response.Status)
	}
	if response.AdapterKind != "huginn_live" {
		t.Fatalf("AdapterKind = %q, want huginn_live", response.AdapterKind)
	}
	for _, want := range []string{"live_adapter_selected", "no_live_browser_launched", "no_external_call_executed"} {
		if !contains(response.ActionLog, want) {
			t.Fatalf("ActionLog = %#v, want %q", response.ActionLog, want)
		}
	}
}

func TestLiveAdapterMissingCommandReturnsStructuredFailure(t *testing.T) {
	response, err := LiveAdapter{Timeout: time.Second}.Run(context.Background(), validRequest())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if response.Status != "failed" || response.ErrorCode != "command_not_configured" {
		t.Fatalf("response = %+v, want command_not_configured failure", response)
	}
}

func TestLiveAdapterZeroExitParsesMinimalJSON(t *testing.T) {
	script := writeHuginnBrowserFixture(t, `#!/usr/bin/env bash
cat >/dev/null
	printf '{"status":"completed","adapter_kind":"huginn_live","visited_urls":["https://example.com/docs"],"page_results":[{"url":"https://example.com/docs","status":"visited","mode":"fetch","title":"Fixture","summary":"fixture summary"}],"extracted_text_summary":"fixture summary","screenshots":["fixture.png"],"action_log":["fixture_read_only"]}'
`)
	response, err := LiveAdapter{Command: script, Timeout: time.Second, AllowedCommands: []string{script}}.Run(context.Background(), validRequest())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if response.Status != "completed" || response.ExtractedTextSummary != "fixture summary" || response.VisitedURLs[0] != "https://example.com/docs" {
		t.Fatalf("response = %+v, want parsed fixture response", response)
	}
	if !contains(response.ActionLog, "live_command_executed") || response.Stderr != "" {
		t.Fatalf("response = %+v, want execution audit action and empty stderr", response)
	}
	if len(response.PageResults) != 1 || response.PageResults[0].Status != "visited" || response.PageResults[0].Title != "Fixture" {
		t.Fatalf("PageResults = %#v, want parsed visited page result", response.PageResults)
	}
}

func TestLiveAdapterRejectsNonListPageResults(t *testing.T) {
	script := writeHuginnBrowserFixture(t, `#!/usr/bin/env bash
cat >/dev/null
printf '{"status":"completed","adapter_kind":"huginn_live","page_results":"https://example.com/docs"}'
`)
	response, err := LiveAdapter{Command: script, Timeout: time.Second, AllowedCommands: []string{script}}.Run(context.Background(), validRequest())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if response.Status != "failed" || response.ErrorCode != "response_contract_invalid" {
		t.Fatalf("response = %+v, want non-list page_results rejected", response)
	}
}

func TestLiveAdapterNonzeroExitReturnsStructuredFailure(t *testing.T) {
	script := writeHuginnBrowserFixture(t, `#!/usr/bin/env bash
printf 'partial output'
printf 'fixture failed' >&2
exit 7
`)
	response, err := LiveAdapter{Command: script, Timeout: time.Second, AllowedCommands: []string{script}}.Run(context.Background(), validRequest())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if response.Status != "failed" || response.ErrorCode != "command_failed" || response.ExitCode != 7 || response.Stdout != "partial output" || response.Stderr != "fixture failed" {
		t.Fatalf("response = %+v, want structured nonzero failure", response)
	}
}

func TestLiveAdapterTimeoutReturnsStructuredTimeout(t *testing.T) {
	script := writeHuginnBrowserFixture(t, `#!/usr/bin/env bash
sleep 2
`)
	start := time.Now()
	response, err := LiveAdapter{Command: script, Timeout: 50 * time.Millisecond, AllowedCommands: []string{script}}.Run(context.Background(), validRequest())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("Run() elapsed = %v, want bounded timeout without retries", elapsed)
	}
	if response.Status != "timeout" || response.ErrorCode != "command_timeout" {
		t.Fatalf("response = %+v, want structured timeout", response)
	}
}

func TestLiveAdapterAllowedCommandRuns(t *testing.T) {
	script := writeHuginnBrowserFixture(t, `#!/usr/bin/env bash
cat >/dev/null
printf '{"status":"completed","adapter_kind":"huginn_live","visited_urls":["https://example.com/docs"],"extracted_text_summary":"fixture summary","action_log":["fixture_read_only"]}'
`)
	response, err := LiveAdapter{Command: script, Timeout: time.Second, AllowedCommands: []string{script}}.Run(context.Background(), validRequest())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if response.Status != "completed" || response.AdapterKind != "huginn_live" || response.ExtractedTextSummary != "fixture summary" {
		t.Fatalf("response = %+v, want parsed allowlisted fixture response", response)
	}
}

func TestLiveAdapterDisallowedCommandIsBlocked(t *testing.T) {
	sentinel := filepath.Join(t.TempDir(), "executed")
	script := writeHuginnBrowserFixture(t, `#!/usr/bin/env bash
touch "$1"
cat >/dev/null
printf '{"status":"completed","adapter_kind":"huginn_live"}'
`)
	response, err := LiveAdapter{Command: script + " " + sentinel, Timeout: time.Second, AllowedCommands: []string{"/not/allowed"}}.Run(context.Background(), validRequest())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if response.Status != "failed" || response.ErrorCode != "command_not_allowed" {
		t.Fatalf("response = %+v, want command_not_allowed failure", response)
	}
	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Fatalf("sentinel stat error = %v, want command not executed", err)
	}
}

func TestLiveAdapterRejectsArgsWhenOnlyExecutableIsAllowlisted(t *testing.T) {
	sentinel := filepath.Join(t.TempDir(), "executed")
	script := writeHuginnBrowserFixture(t, `#!/usr/bin/env bash
touch "$1"
cat >/dev/null
printf '{"status":"completed","adapter_kind":"huginn_live"}'
`)
	response, err := LiveAdapter{Command: script + " " + sentinel, Timeout: time.Second, AllowedCommands: []string{script}}.Run(context.Background(), validRequest())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if response.Status != "failed" || response.ErrorCode != "command_not_allowed" {
		t.Fatalf("response = %+v, want command_not_allowed failure", response)
	}
	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Fatalf("sentinel stat error = %v, want arg-bearing command not executed without exact allowlist entry", err)
	}
}

func TestLiveAdapterEmptyAllowlistBlocksExecution(t *testing.T) {
	sentinel := filepath.Join(t.TempDir(), "executed")
	script := writeHuginnBrowserFixture(t, `#!/usr/bin/env bash
touch "$1"
cat >/dev/null
printf '{"status":"completed","adapter_kind":"huginn_live"}'
`)
	response, err := LiveAdapter{Command: script + " " + sentinel, Timeout: time.Second}.Run(context.Background(), validRequest())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if response.Status != "failed" || response.ErrorCode != "command_allowlist_empty" {
		t.Fatalf("response = %+v, want command_allowlist_empty failure", response)
	}
	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Fatalf("sentinel stat error = %v, want command not executed", err)
	}
}

func TestLiveAdapterInvalidResponseContractFails(t *testing.T) {
	script := writeHuginnBrowserFixture(t, `#!/usr/bin/env bash
cat >/dev/null
printf '{"adapter_kind":"huginn_live","visited_urls":["https://example.com/docs"]}'
`)
	response, err := LiveAdapter{Command: script, Timeout: time.Second, AllowedCommands: []string{script}}.Run(context.Background(), validRequest())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if response.Status != "failed" || response.ErrorCode != "response_contract_invalid" {
		t.Fatalf("response = %+v, want response_contract_invalid failure", response)
	}
}

func TestLiveAdapterRejectsMutationResponseFields(t *testing.T) {
	script := writeHuginnBrowserFixture(t, `#!/usr/bin/env bash
cat >/dev/null
printf '{"status":"completed","adapter_kind":"huginn_live","visited_urls":["https://example.com/docs"],"submitted_forms":["login"]}'
`)
	response, err := LiveAdapter{Command: script, Timeout: time.Second, AllowedCommands: []string{script}}.Run(context.Background(), validRequest())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if response.Status != "failed" || response.ErrorCode != "response_contract_invalid" {
		t.Fatalf("response = %+v, want mutation-looking response rejected", response)
	}
}

func TestLiveAdapterRejectsNonListVisitedURLs(t *testing.T) {
	script := writeHuginnBrowserFixture(t, `#!/usr/bin/env bash
cat >/dev/null
printf '{"status":"completed","adapter_kind":"huginn_live","visited_urls":"https://example.com/docs"}'
`)
	response, err := LiveAdapter{Command: script, Timeout: time.Second, AllowedCommands: []string{script}}.Run(context.Background(), validRequest())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if response.Status != "failed" || response.ErrorCode != "response_contract_invalid" {
		t.Fatalf("response = %+v, want non-list visited_urls rejected", response)
	}
}

func TestHuginnBrowserWorkerResponseFixturesValidateContract(t *testing.T) {
	for _, test := range []struct {
		name     string
		path     string
		wantCode string
	}{
		{name: "completed", path: "testdata/worker_response_completed.json"},
		{name: "failed", path: "testdata/worker_response_failed.json"},
		{name: "timeout", path: "testdata/worker_response_timeout.json"},
		{name: "mutation", path: "testdata/worker_response_invalid_mutation.json", wantCode: "response_contract_invalid"},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw, err := os.ReadFile(test.path)
			if err != nil {
				t.Fatalf("ReadFile(%q) error = %v", test.path, err)
			}
			code, message := validateLiveResponseContract(raw)
			if code != test.wantCode {
				t.Fatalf("validateLiveResponseContract() code=%q message=%q, want code %q", code, message, test.wantCode)
			}
			if test.wantCode == "" {
				var response Response
				if err := json.Unmarshal(raw, &response); err != nil {
					t.Fatalf("fixture does not unmarshal into Response: %v", err)
				}
			}
		})
	}
}

func TestLiveAdapterRequestJSONIncludesEvidenceRequired(t *testing.T) {
	request := Request{
		Objective:          "Collect public documentation",
		StartURLs:          []string{"https://example.com/docs"},
		AllowedDomains:     []string{"example.com"},
		MaxPages:           2,
		MaxDurationSeconds: 30,
		EvidenceRequired:   true,
	}
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	var envelope map[string]any
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if envelope["evidence_required"] != true {
		t.Fatalf("request JSON = %s, want evidence_required=true", raw)
	}
}

func TestSelectAdapterDefaultsToStubAndAllowsLiveEnv(t *testing.T) {
	t.Setenv(AdapterEnvVar, "")
	if _, ok := SelectAdapterFromEnv().(StubAdapter); !ok {
		t.Fatalf("SelectAdapterFromEnv() = %T, want StubAdapter", SelectAdapterFromEnv())
	}
	t.Setenv(AdapterEnvVar, "live")
	t.Setenv(LiveCommandEnvVar, "/usr/local/bin/huginn-browser")
	t.Setenv(LiveAllowedCommandsEnvVar, "/usr/local/bin/huginn-browser")
	if _, ok := SelectAdapterFromEnv().(LiveAdapter); !ok {
		t.Fatalf("SelectAdapterFromEnv() = %T, want LiveAdapter", SelectAdapterFromEnv())
	}
}

func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func validRequest() Request {
	return Request{
		Objective:          "Collect public documentation",
		StartURLs:          []string{"https://example.com/docs"},
		AllowedDomains:     []string{"example.com"},
		MaxPages:           2,
		MaxDurationSeconds: 30,
	}
}

func writeHuginnBrowserFixture(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "huginn-browser-fixture.sh")
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return path
}
