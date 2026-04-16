package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"odin-os/internal/adapters/browserhuman"
	"odin-os/internal/tools/invocation"
)

func TestBuiltinCatalogDoesNotExposePlaceholderOperationalTools(t *testing.T) {
	t.Parallel()

	definitions := BuiltinDefinitions()
	for _, key := range []string{"project_status", "task_list", "event_log"} {
		if _, ok := definitions[key]; ok {
			t.Fatalf("%s should not be exposed until it is runtime-backed", key)
		}
	}
}

func TestBuiltinDefinitionsIncludeSchemasAndHandlers(t *testing.T) {
	t.Parallel()

	definitions := BuiltinDefinitions()
	if len(definitions) == 0 {
		t.Fatalf("BuiltinDefinitions() len = 0, want > 0")
	}

	for _, key := range []string{"huginn_browser_session", "plaid_transfer_application"} {
		definition, ok := definitions[key]
		if !ok {
			t.Fatalf("missing %s definition", key)
		}
		if definition.Schema == nil {
			t.Fatalf("%s schema = nil, want schema", key)
		}
		if definition.Invoke == nil {
			t.Fatalf("%s invoke = nil, want handler", key)
		}
	}

	assertSchemaEnum(t, definitions["huginn_browser_session"].Schema, "action", []string{"health", "launch", "snapshot", "screenshot", "stop"})
	assertSchemaRequired(t, definitions["huginn_browser_session"].Schema, "action")
	assertSchemaProperty(t, definitions["huginn_browser_session"].Schema, "url")
	assertSchemaProperty(t, definitions["huginn_browser_session"].Schema, "path")
	assertSchemaProperty(t, definitions["plaid_transfer_application"].Schema, "application_url")
	assertSchemaProperty(t, definitions["plaid_transfer_application"].Schema, "path")

	if !hasTag(definitions["huginn_browser_session"].Tags, "browser") {
		t.Fatalf("huginn_browser_session tags = %#v, want browser tag", definitions["huginn_browser_session"].Tags)
	}
	if !hasTag(definitions["huginn_browser_session"].Tags, "session") {
		t.Fatalf("huginn_browser_session tags = %#v, want session tag", definitions["huginn_browser_session"].Tags)
	}
	if !hasTag(definitions["plaid_transfer_application"].Tags, "plaid") {
		t.Fatalf("plaid_transfer_application tags = %#v, want plaid tag", definitions["plaid_transfer_application"].Tags)
	}
	if !hasTag(definitions["plaid_transfer_application"].Tags, "transfer") {
		t.Fatalf("plaid_transfer_application tags = %#v, want transfer tag", definitions["plaid_transfer_application"].Tags)
	}
}

func TestBuiltinProjectStatusInvokesRuntimeDriver(t *testing.T) {
	t.Parallel()

	invoker := &stubToolInvoker{
		result: invocation.Result{
			Source:  "script",
			Summary: "Project alpha status from runtime.",
			KeyFacts: map[string]string{
				"project_key":     "alpha",
				"open_task_count": "2",
			},
			FollowOnOptions: []string{"inspect tasks"},
			RawRef:          "driver://project_status/alpha",
			RawOutput:       "project=alpha open_tasks=2",
		},
	}

	definitions := BuiltinDefinitionsWithInvoker(invoker)
	result, err := definitions["project_status"].Invoke(map[string]string{"project_key": "alpha"})
	if err != nil {
		t.Fatalf("Invoke(project_status) error = %v", err)
	}
	if invoker.key != "project_status" {
		t.Fatalf("invoked key = %q, want project_status", invoker.key)
	}
	if invoker.args["project_key"] != "alpha" {
		t.Fatalf("project_key arg = %q, want alpha", invoker.args["project_key"])
	}
	if result.Source != "driver" {
		t.Fatalf("result source = %q, want driver", result.Source)
	}
	if result.RawRef != "driver://project_status/alpha" {
		t.Fatalf("raw ref = %q, want driver-backed ref", result.RawRef)
	}
}

func TestBuiltinProjectStatusRequiresRuntimeInvoker(t *testing.T) {
	t.Parallel()

	definitions := BuiltinDefinitions()
	if _, ok := definitions["project_status"]; ok {
		t.Fatal("project_status should not be exposed without a runtime invoker")
	}
}

func TestBrowserHumanBuiltinsInvokeDriverPathAndHaveBoundedFollowOnOptions(t *testing.T) {
	definitions := BuiltinDefinitions()
	driverScript := writeBrowserHumanFixtureDriver(t)
	t.Setenv("ODIN_BROWSER_HUMAN_DRIVER", driverScript)

	cases := []struct {
		name               string
		key                string
		input              map[string]string
		requestPath        string
		responseSummary    string
		responseState      string
		responseURL        string
		responseScreenshot string
		responseNextAction string
		expectedFollowOn   []string
		expectedInput      map[string]any
	}{
		{
			name:               "session",
			key:                "huginn_browser_session",
			input:              map[string]string{"action": "snapshot", "url": "https://example.com", "path": "/tmp/session"},
			requestPath:        filepath.Join(t.TempDir(), "huginn-request.json"),
			responseSummary:    "browser session complete",
			responseState:      "ready",
			responseURL:        "https://example.com",
			responseScreenshot: "/tmp/huginn-session.png",
			responseNextAction: "run plaid transfer workflow",
			expectedFollowOn:   []string{"inspect browser artifacts", "run plaid_transfer_application"},
			expectedInput:      map[string]any{"action": "snapshot", "url": "https://example.com", "path": "/tmp/session"},
		},
		{
			name:               "plaid",
			key:                "plaid_transfer_application",
			input:              map[string]string{"application_url": "https://plaid.com/transfer", "path": "/tmp/plaid"},
			requestPath:        filepath.Join(t.TempDir(), "plaid-request.json"),
			responseSummary:    "Plaid transfer blocked on MFA",
			responseState:      "blocked_on_mfa",
			responseURL:        "https://plaid.com/transfer",
			responseScreenshot: "/tmp/plaid-transfer.png",
			responseNextAction: "wait for human MFA",
			expectedFollowOn:   []string{"inspect browser artifacts", "stop browser session"},
			expectedInput:      map[string]any{"application_url": "https://plaid.com/transfer", "path": "/tmp/plaid"},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("ODIN_BROWSER_REQUEST_PATH", tc.requestPath)
			t.Setenv("ODIN_BROWSER_RESPONSE_TOOL_KEY", tc.key)
			t.Setenv("ODIN_BROWSER_RESPONSE_SUMMARY", tc.responseSummary)
			t.Setenv("ODIN_BROWSER_RESPONSE_SESSION_STATE", tc.responseState)
			t.Setenv("ODIN_BROWSER_RESPONSE_CURRENT_URL", tc.responseURL)
			t.Setenv("ODIN_BROWSER_RESPONSE_SCREENSHOT_PATH", tc.responseScreenshot)
			t.Setenv("ODIN_BROWSER_RESPONSE_NEXT_ACTION", tc.responseNextAction)

			result, err := definitions[tc.key].Invoke(tc.input)
			if err != nil {
				t.Fatalf("Invoke() error = %v", err)
			}
			if result.CapabilityKey != tc.key {
				t.Fatalf("CapabilityKey = %q, want %q", result.CapabilityKey, tc.key)
			}
			if result.Summary != tc.responseSummary {
				t.Fatalf("Summary = %q, want %q", result.Summary, tc.responseSummary)
			}
			expectedRawOutput := fmt.Sprintf(`{"status":"completed","tool_key":"%s","summary":"%s","artifacts":{"session_state":"%s","current_url":"%s","screenshot_path":"%s","next_action":"%s","evidence":["driver invoked"]}}`, tc.key, tc.responseSummary, tc.responseState, tc.responseURL, tc.responseScreenshot, tc.responseNextAction)
			if result.RawOutput != expectedRawOutput {
				t.Fatalf("RawOutput = %q, want %q", result.RawOutput, expectedRawOutput)
			}
			if result.RawRef != fmt.Sprintf("browserhuman://%s/result", tc.key) {
				t.Fatalf("RawRef = %q, want browserhuman://%s/result", result.RawRef, tc.key)
			}
			if got := result.KeyFacts["session_state"]; got != tc.responseState {
				t.Fatalf("KeyFacts.session_state = %q, want %q", got, tc.responseState)
			}
			if got := result.KeyFacts["current_url"]; got != tc.responseURL {
				t.Fatalf("KeyFacts.current_url = %q, want %q", got, tc.responseURL)
			}
			if got := result.KeyFacts["screenshot_path"]; got != tc.responseScreenshot {
				t.Fatalf("KeyFacts.screenshot_path = %q, want %q", got, tc.responseScreenshot)
			}
			if got := result.KeyFacts["next_action"]; got != tc.responseNextAction {
				t.Fatalf("KeyFacts.next_action = %q, want %q", got, tc.responseNextAction)
			}
			if len(result.FollowOnOptions) != len(tc.expectedFollowOn) {
				t.Fatalf("FollowOnOptions len = %d, want %d", len(result.FollowOnOptions), len(tc.expectedFollowOn))
			}
			for i, option := range tc.expectedFollowOn {
				if result.FollowOnOptions[i] != option {
					t.Fatalf("FollowOnOptions[%d] = %q, want %q", i, result.FollowOnOptions[i], option)
				}
			}

			request := readBrowserHumanRequest(t, tc.requestPath)
			if request.ToolKey != tc.key {
				t.Fatalf("request.ToolKey = %q, want %q", request.ToolKey, tc.key)
			}
			if !browserHumanRequestInputEquals(request.Input, tc.expectedInput) {
				t.Fatalf("request.Input = %#v, want %#v", request.Input, tc.expectedInput)
			}
		})
	}
}

func assertSchemaProperty(t *testing.T, schema map[string]any, property string) {
	t.Helper()
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema properties = %#v, want map", schema["properties"])
	}
	if _, ok := properties[property]; !ok {
		t.Fatalf("missing schema property %q", property)
	}
}

func assertSchemaEnum(t *testing.T, schema map[string]any, property string, want []string) {
	t.Helper()
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema properties = %#v, want map", schema["properties"])
	}
	prop, ok := properties[property].(map[string]any)
	if !ok {
		t.Fatalf("schema property %q = %#v, want map", property, properties[property])
	}
	enumValues, ok := prop["enum"].([]string)
	if !ok {
		t.Fatalf("schema property %q enum = %#v, want []string", property, prop["enum"])
	}
	if len(enumValues) != len(want) {
		t.Fatalf("schema property %q enum len = %d, want %d", property, len(enumValues), len(want))
	}
	for i, expected := range want {
		if enumValues[i] != expected {
			t.Fatalf("schema property %q enum[%d] = %q, want %q", property, i, enumValues[i], expected)
		}
	}
}

func assertSchemaRequired(t *testing.T, schema map[string]any, property string) {
	t.Helper()
	required, ok := schema["required"].([]string)
	if !ok {
		t.Fatalf("schema required = %#v, want []string", schema["required"])
	}
	for _, candidate := range required {
		if candidate == property {
			return
		}
	}
	t.Fatalf("schema required = %#v, want %q present", required, property)
}

func readBrowserHumanRequest(t *testing.T, path string) browserhuman.Request {
	t.Helper()

	requestBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(request) error = %v", err)
	}
	var request browserhuman.Request
	if err := json.Unmarshal(requestBytes, &request); err != nil {
		t.Fatalf("json.Unmarshal(request) error = %v", err)
	}
	return request
}

func browserHumanRequestInputEquals(got any, want any) bool {
	if got == nil || want == nil {
		return got == nil && want == nil
	}

	gotMap, ok := got.(map[string]any)
	if !ok {
		return false
	}
	wantMap, ok := want.(map[string]any)
	if !ok {
		return false
	}
	if len(gotMap) != len(wantMap) {
		return false
	}
	for key, wantValue := range wantMap {
		gotValue, ok := gotMap[key]
		if !ok {
			return false
		}
		if key == "path" {
			gotPath, gotOK := gotValue.(string)
			wantPath, wantOK := wantValue.(string)
			if !gotOK || !wantOK {
				return false
			}
			if filepath.Base(gotPath) != filepath.Base(wantPath) {
				return false
			}
			artifactSegment := string(os.PathSeparator) + "artifacts" + string(os.PathSeparator)
			if !strings.Contains(gotPath, artifactSegment) {
				return false
			}
			continue
		}
		if gotValue != wantValue {
			return false
		}
	}
	return true
}

func hasTag(tags []string, tag string) bool {
	for _, candidate := range tags {
		if candidate == tag {
			return true
		}
	}
	return false
}

func writeBrowserHumanFixtureDriver(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "driver.sh")
	script := `#!/usr/bin/env bash
set -eu
cat >"$ODIN_BROWSER_REQUEST_PATH"
printf '{"status":"completed","tool_key":"%s","summary":"%s","artifacts":{"session_state":"%s","current_url":"%s","screenshot_path":"%s","next_action":"%s","evidence":["driver invoked"]}}'   "$ODIN_BROWSER_RESPONSE_TOOL_KEY"   "$ODIN_BROWSER_RESPONSE_SUMMARY"   "$ODIN_BROWSER_RESPONSE_SESSION_STATE"   "$ODIN_BROWSER_RESPONSE_CURRENT_URL"   "$ODIN_BROWSER_RESPONSE_SCREENSHOT_PATH"   "$ODIN_BROWSER_RESPONSE_NEXT_ACTION"
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile(driver) error = %v", err)
	}
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatalf("Chmod(driver) error = %v", err)
	}
	return path
}

type stubToolInvoker struct {
	key    string
	args   map[string]string
	result invocation.Result
}

func (invoker *stubToolInvoker) Invoke(_ context.Context, key string, request invocation.Request) (invocation.Result, error) {
	invoker.key = key
	invoker.args = request.Args
	return invoker.result, nil
}
