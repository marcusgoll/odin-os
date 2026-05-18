package catalog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuiltinDefinitionsIncludeSchemasAndHandlers(t *testing.T) {
	t.Parallel()

	definitions := BuiltinDefinitions()
	if len(definitions) == 0 {
		t.Fatalf("BuiltinDefinitions() len = 0, want > 0")
	}

	taskList, ok := definitions["task_list"]
	if !ok {
		t.Fatalf("missing task_list definition")
	}
	if taskList.Schema == nil {
		t.Fatalf("task_list schema = nil, want schema")
	}
	if taskList.Invoke == nil {
		t.Fatalf("task_list invoke = nil, want handler")
	}
}

func TestBuiltinDefinitionsExposeCanonicalBrowserToolsAndHiddenHuginnAliases(t *testing.T) {
	t.Parallel()

	definitions := BuiltinDefinitions()
	cases := []struct {
		canonical string
		alias     string
	}{
		{canonical: "browser_pbs_session", alias: "huginn_pbs_session"},
		{canonical: "browser_visual_audit", alias: "huginn_visual_audit"},
		{canonical: "browser_x_post_visible_evidence", alias: "huginn_x_post_visible_evidence"},
		{canonical: "browser_x_post_publish", alias: "huginn_x_post_publish"},
		{canonical: "browser_x_weekly_evidence_bundle", alias: "huginn_x_weekly_evidence_bundle"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.canonical, func(t *testing.T) {
			canonical, ok := definitions[tc.canonical]
			if !ok {
				t.Fatalf("missing %s definition", tc.canonical)
			}
			if canonical.Invoke == nil {
				t.Fatalf("%s invoke = nil, want handler", tc.canonical)
			}
			if canonical.Hidden {
				t.Fatalf("%s hidden = true, want false", tc.canonical)
			}
			if canonical.CanonicalKey != tc.canonical {
				got := canonical.CanonicalKey
				t.Fatalf("%s CanonicalKey = %q, want %s", tc.canonical, got, tc.canonical)
			}

			alias, ok := definitions[tc.alias]
			if !ok {
				t.Fatalf("missing %s alias", tc.alias)
			}
			if alias.Invoke == nil {
				t.Fatalf("%s invoke = nil, want handler", tc.alias)
			}
			if !alias.Hidden {
				t.Fatalf("%s hidden = false, want true", tc.alias)
			}
			if alias.CanonicalKey != tc.canonical {
				got := alias.CanonicalKey
				t.Fatalf("%s CanonicalKey = %q, want %s", tc.alias, got, tc.canonical)
			}
		})
	}
}

func TestBuiltinDefinitionsExposeDOMFastLaneTool(t *testing.T) {
	t.Parallel()

	definitions := BuiltinDefinitions()
	definition, ok := definitions["browser_dom_fast_lane"]
	if !ok {
		t.Fatalf("missing browser_dom_fast_lane definition")
	}
	if definition.Invoke == nil {
		t.Fatal("browser_dom_fast_lane invoke = nil, want handler")
	}
	if definition.CanonicalKey != "browser_dom_fast_lane" {
		t.Fatalf("CanonicalKey = %q, want browser_dom_fast_lane", definition.CanonicalKey)
	}
	required, ok := definition.Schema["required"].([]string)
	if !ok {
		t.Fatalf("schema required = %#v, want []string", definition.Schema["required"])
	}
	if !containsString(required, "recipe_key") || !containsString(required, "target_url") {
		t.Fatalf("required = %#v, want recipe_key and target_url", required)
	}
}

func TestBuiltinDefinitionsInvokeDOMFastLaneTool(t *testing.T) {
	t.Setenv("ODIN_HUGINN_DOM_FAST_LANE_DRIVER", writeFixtureDriver(t, `#!/usr/bin/env bash
printf '{"status":"completed","tool_key":"browser_dom_fast_lane","summary":"Extracted fixture status table.","artifacts":{"recipe_key":"fixture_status","source_url":"http://127.0.0.1:18080/status-fixture","final_url":"http://127.0.0.1:18080/status-fixture","page_status":"Ready","selector_version":"fixture-v1","snapshot_excerpt":"Ready alpha green","screenshot_path":"/tmp/fixture-status.png"}}'
`))

	definitions := BuiltinDefinitions()
	result, err := definitions["browser_dom_fast_lane"].Invoke(map[string]string{
		"recipe_key": "fixture_status",
		"target_url": "http://127.0.0.1:18080/status-fixture",
		"label":      "fixture-status",
		"wait_ms":    "0",
		"headless":   "true",
	})
	if err != nil {
		t.Fatalf("browser_dom_fast_lane invoke error = %v", err)
	}
	if result.CapabilityKey != "browser_dom_fast_lane" {
		t.Fatalf("CapabilityKey = %q, want browser_dom_fast_lane", result.CapabilityKey)
	}
	if result.KeyFacts["status"] != "completed" {
		t.Fatalf("status = %q, want completed", result.KeyFacts["status"])
	}
	if result.KeyFacts["recipe_key"] != "fixture_status" {
		t.Fatalf("recipe_key = %q, want fixture_status", result.KeyFacts["recipe_key"])
	}
	if result.KeyFacts["selector_version"] != "fixture-v1" {
		t.Fatalf("selector_version = %q, want fixture-v1", result.KeyFacts["selector_version"])
	}
	if !containsString(result.Artifacts, "page_status=Ready") {
		t.Fatalf("artifacts = %+v, want page_status detail", result.Artifacts)
	}
	if !containsString(result.Artifacts, "screenshot_path=/tmp/fixture-status.png") {
		t.Fatalf("artifacts = %+v, want screenshot path", result.Artifacts)
	}
}

func TestBuiltinDefinitionsPreserveBlockedDOMFastLaneResult(t *testing.T) {
	t.Setenv("ODIN_HUGINN_DOM_FAST_LANE_DRIVER", writeFixtureDriver(t, `#!/usr/bin/env bash
printf '{"status":"blocked","tool_key":"browser_dom_fast_lane","summary":"Selector drift blocked fixture extraction.","artifacts":{"recipe_key":"fixture_status","intervention_reason":"selector_drift","selector_version":"fixture-v1"}}'
`))

	definitions := BuiltinDefinitions()
	result, err := definitions["browser_dom_fast_lane"].Invoke(map[string]string{
		"recipe_key": "fixture_status",
		"target_url": "http://127.0.0.1:18080/status-fixture",
	})
	if err != nil {
		t.Fatalf("browser_dom_fast_lane invoke error = %v", err)
	}
	if result.KeyFacts["status"] != "blocked" {
		t.Fatalf("status = %q, want blocked", result.KeyFacts["status"])
	}
	if result.KeyFacts["intervention_reason"] != "selector_drift" {
		t.Fatalf("intervention_reason = %q, want selector_drift", result.KeyFacts["intervention_reason"])
	}
	if !containsString(result.Artifacts, "intervention_reason=selector_drift") {
		t.Fatalf("artifacts = %+v, want intervention reason", result.Artifacts)
	}
}

func TestIndexToolDefinitionRejectsDuplicateKeys(t *testing.T) {
	t.Parallel()

	index := make(map[string]ToolDefinition, 2)
	indexToolDefinition(index, ToolDefinition{
		Key:          "browser_visual_audit",
		CanonicalKey: "browser_visual_audit",
		Aliases:      []string{"huginn_visual_audit"},
	})

	defer func() {
		if recovered := recover(); recovered == nil {
			t.Fatal("indexToolDefinition duplicate key panic = nil, want value")
		}
	}()

	indexToolDefinition(index, ToolDefinition{Key: "browser_visual_audit"})
}

func TestIndexToolDefinitionClonesAliasMetadata(t *testing.T) {
	t.Parallel()

	index := make(map[string]ToolDefinition, 2)
	indexToolDefinition(index, ToolDefinition{
		Key:          "browser_visual_audit",
		CanonicalKey: "browser_visual_audit",
		Aliases:      []string{"huginn_visual_audit"},
		Scopes:       []string{"global"},
		Tags:         []string{"browser", "visual"},
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"target_url": map[string]any{"type": "string"},
			},
		},
	})

	canonical := index["browser_visual_audit"]
	alias := index["huginn_visual_audit"]

	alias.Scopes[0] = "project"
	alias.Tags[0] = "legacy"
	alias.Schema["type"] = "alias"
	alias.Schema["properties"].(map[string]any)["label"] = map[string]any{"type": "string"}

	if canonical.Scopes[0] != "global" {
		t.Fatalf("canonical scopes = %v, want independent clone", canonical.Scopes)
	}
	if canonical.Tags[0] != "browser" {
		t.Fatalf("canonical tags = %v, want independent clone", canonical.Tags)
	}
	if canonical.Schema["type"] != "object" {
		t.Fatalf("canonical schema type = %#v, want object", canonical.Schema["type"])
	}
	if _, ok := canonical.Schema["properties"].(map[string]any)["label"]; ok {
		t.Fatalf("canonical schema properties = %#v, want no alias mutation", canonical.Schema["properties"])
	}
}

func TestBuiltinDefinitionsInvokeLiveMayBidTools(t *testing.T) {
	requestPath := filepath.Join(t.TempDir(), "request.json")

	t.Setenv("ODIN_GOOGLE_CALENDAR_DRIVER", writeFixtureDriver(t, `#!/usr/bin/env bash
cat >"$ODIN_DRIVER_REQUEST_PATH"
printf '{"status":"completed","tool_key":"google_calendar_off_dates","summary":"Found 2 off-dates for 2026-05.","artifacts":{"bid_period":"2026-05","calendar_id":"primary","timezone":"America/Chicago","off_dates":["2026-05-03","2026-05-04"]}}'
`))
	t.Setenv("ODIN_HUGINN_DRIVER", writeFixtureDriver(t, `#!/usr/bin/env bash
cat >"$ODIN_DRIVER_REQUEST_PATH"
printf '{"status":"completed","tool_key":"browser_pbs_session","summary":"Validated trusted browser session state for the 2026-05 PBS workflow.","artifacts":{"bid_period":"2026-05","workflow_key":"pbs_may_bid","session_state":"ready","session_id":"huginn-session-1842","evidence":["session_alive","window_open","credentials_valid"]}}'
`))
	t.Setenv("ODIN_HUGINN_VISUAL_DRIVER", writeFixtureDriver(t, `#!/usr/bin/env bash
cat >"$ODIN_DRIVER_REQUEST_PATH"
printf '{"status":"completed","tool_key":"browser_visual_audit","summary":"Captured browser visual audit evidence for cfipros-dashboard.","artifacts":{"target_url":"https://example.com/dashboard","final_url":"https://example.com/dashboard","title":"Dashboard","label":"cfipros-dashboard","screenshot_path":"/tmp/dashboard.png","snapshot_excerpt":"Revenue MRR Pipeline","wait_ms":"2000","launch_mode":"--headless"}}'
`))
	t.Setenv("ODIN_HUGINN_X_POST_DRIVER", writeFixtureDriver(t, `#!/usr/bin/env bash
cat >"$ODIN_DRIVER_REQUEST_PATH"
	printf '{"status":"completed","tool_key":"browser_x_post_visible_evidence","summary":"Captured visible X post evidence for marcus-crosswind.","artifacts":{"target_url":"https://x.com/marcus/status/123","final_url":"https://x.com/marcus/status/123","label":"marcus-crosswind","title":"X","screenshot_path":"/tmp/marcus-crosswind.png","snapshot_path":"/tmp/marcus-crosswind.txt","snapshot_excerpt":"Students don\\u0027t need more motivation","post_text":"Students don\\u0027t need more motivation","author_display_name":"Marcus Gollahon","author_handle":"@marcus","reply_count":"4","repost_count":"2","like_count":"18","bookmark_count":"1","view_count":"1400"}}'
`))
	t.Setenv("ODIN_HUGINN_X_PUBLISH_DRIVER", writeFixtureDriver(t, `#!/usr/bin/env bash
cat >"$ODIN_DRIVER_REQUEST_PATH"
printf '{"status":"completed","tool_key":"browser_x_post_publish","summary":"Published approved X post through Browser Control.","artifacts":{"publish_url":"https://x.com/marcus/status/999999999","final_url":"https://x.com/marcus/status/999999999","label":"social-outcome-2","title":"X","screenshot_path":"/tmp/marcus-native-post.png","published_at":"2026-04-20T12:34:56Z","posted_text":"Approved X post ready to publish natively."}}'
`))
	t.Setenv("ODIN_DRIVER_REQUEST_PATH", requestPath)

	definitions := BuiltinDefinitions()
	calendar := definitions["google_calendar_off_dates"]
	if calendar.Invoke == nil {
		t.Fatal("google_calendar_off_dates invoke = nil, want handler")
	}
	calendarResult, err := calendar.Invoke(map[string]string{
		"bid_period":  "2026-05",
		"calendar_id": "primary",
		"timezone":    "America/Chicago",
	})
	if err != nil {
		t.Fatalf("google_calendar_off_dates invoke error = %v", err)
	}
	if calendarResult.Summary != "Found 2 off-dates for 2026-05." {
		t.Fatalf("calendar summary = %q, want fixture summary", calendarResult.Summary)
	}
	if !containsString(calendarResult.Artifacts, "off_dates=2026-05-03,2026-05-04") {
		t.Fatalf("calendar artifacts = %+v, want off-dates detail", calendarResult.Artifacts)
	}

	huginn := definitions["browser_pbs_session"]
	if huginn.Invoke == nil {
		t.Fatal("browser_pbs_session invoke = nil, want handler")
	}
	huginnResult, err := huginn.Invoke(map[string]string{
		"bid_period":   "2026-05",
		"workflow_key": "pbs_may_bid",
		"timezone":     "America/Chicago",
	})
	if err != nil {
		t.Fatalf("browser_pbs_session invoke error = %v", err)
	}
	if !strings.Contains(huginnResult.RawOutput, "huginn-session-1842") {
		t.Fatalf("huginn raw output = %q, want real driver response", huginnResult.RawOutput)
	}
	if len(huginnResult.Artifacts) == 0 {
		t.Fatalf("huginn artifacts = empty, want structured details")
	}

	visual := definitions["browser_visual_audit"]
	if visual.Invoke == nil {
		t.Fatal("browser_visual_audit invoke = nil, want handler")
	}
	visualResult, err := visual.Invoke(map[string]string{
		"target_url": "https://example.com/dashboard",
		"label":      "cfipros-dashboard",
	})
	if err != nil {
		t.Fatalf("browser_visual_audit invoke error = %v", err)
	}
	if !containsString(visualResult.Artifacts, "screenshot_path=/tmp/dashboard.png") {
		t.Fatalf("visual artifacts = %+v, want screenshot path", visualResult.Artifacts)
	}

	xEvidence := definitions["browser_x_post_visible_evidence"]
	if xEvidence.Invoke == nil {
		t.Fatal("browser_x_post_visible_evidence invoke = nil, want handler")
	}
	xEvidenceResult, err := xEvidence.Invoke(map[string]string{
		"target_url": "https://x.com/marcus/status/123",
		"label":      "marcus-crosswind",
	})
	if err != nil {
		t.Fatalf("browser_x_post_visible_evidence invoke error = %v", err)
	}
	if !containsString(xEvidenceResult.Artifacts, "snapshot_path=/tmp/marcus-crosswind.txt") {
		t.Fatalf("x evidence artifacts = %+v, want snapshot path", xEvidenceResult.Artifacts)
	}
	if len(xEvidenceResult.MemoryRecords) != 1 {
		t.Fatalf("x evidence memory records len = %d, want 1", len(xEvidenceResult.MemoryRecords))
	}
	if xEvidenceResult.MemoryRecords[0].MemoryType != "social_evidence" {
		t.Fatalf("x evidence memory type = %q, want social_evidence", xEvidenceResult.MemoryRecords[0].MemoryType)
	}
	if xEvidenceResult.MemoryRecords[0].Fields["channel"] != "x" {
		t.Fatalf("x evidence memory channel = %q, want x", xEvidenceResult.MemoryRecords[0].Fields["channel"])
	}
	if xEvidenceResult.MemoryRecords[0].Fields["evidence_kind"] != "x_post_visible" {
		t.Fatalf("x evidence kind = %q, want x_post_visible", xEvidenceResult.MemoryRecords[0].Fields["evidence_kind"])
	}
	if xEvidenceResult.MemoryRecords[0].Fields["bookmark_count"] != "1" {
		t.Fatalf("x evidence bookmark_count = %q, want 1", xEvidenceResult.MemoryRecords[0].Fields["bookmark_count"])
	}
	if !containsString(xEvidenceResult.Artifacts, "bookmark_count=1") {
		t.Fatalf("x evidence artifacts = %+v, want bookmark_count detail", xEvidenceResult.Artifacts)
	}
	requestBytes, err := os.ReadFile(requestPath)
	if err != nil {
		t.Fatalf("ReadFile(requestPath) error = %v", err)
	}
	if !strings.Contains(string(requestBytes), `"headless":"false"`) {
		t.Fatalf("x evidence request = %q, want headed default", string(requestBytes))
	}

	xPublish := definitions["browser_x_post_publish"]
	if xPublish.Invoke == nil {
		t.Fatal("browser_x_post_publish invoke = nil, want handler")
	}
	xPublishResult, err := xPublish.Invoke(map[string]string{
		"post_text": "Approved X post ready to publish natively.",
		"label":     "social-outcome-2",
	})
	if err == nil {
		t.Fatalf("browser_x_post_publish invoke error = nil, result = %+v, want approval-required refusal", xPublishResult)
	}
	if !strings.Contains(err.Error(), "approval_required") {
		t.Fatalf("browser_x_post_publish invoke error = %v, want approval_required", err)
	}

	xReplyPublishResult, err := xPublish.Invoke(map[string]string{
		"post_text":       "Short, useful reply text.",
		"content_kind":    "reply",
		"in_reply_to_url": "https://x.com/example/status/123",
		"label":           "social-outcome-9",
	})
	if err == nil {
		t.Fatalf("browser_x_post_publish reply invoke error = nil, result = %+v, want approval-required refusal", xReplyPublishResult)
	}
	if !strings.Contains(err.Error(), "approval_required") {
		t.Fatalf("browser_x_post_publish reply invoke error = %v, want approval_required", err)
	}
}

func TestBuiltinDefinitionsInvokeWeeklyXEvidenceBundle(t *testing.T) {
	requestPath := filepath.Join(t.TempDir(), "bundle-request.json")

	t.Setenv("ODIN_HUGINN_X_POST_DRIVER", writeFixtureDriver(t, `#!/usr/bin/env bash
request_path="$(mktemp)"
cat >"$request_path"
python3 - "$request_path" <<'PY'
import json
import sys

with open(sys.argv[1], "r", encoding="utf-8") as handle:
    request = json.load(handle)
target_url = (((request.get("input") or {}).get("target_url")) or "").strip()
label = (((request.get("input") or {}).get("label")) or "").strip() or "x-post-evidence"
suffix = target_url.rsplit("/", 1)[-1] or "post"
print(json.dumps({
    "status": "completed",
    "tool_key": "browser_x_post_visible_evidence",
    "summary": f"Captured visible X post evidence for {label}-{suffix}.",
    "artifacts": {
        "target_url": target_url,
        "final_url": target_url,
        "label": f"{label}-{suffix}",
        "title": "X",
        "screenshot_path": f"/tmp/{suffix}.png",
        "snapshot_path": f"/tmp/{suffix}.txt",
        "snapshot_excerpt": f"Excerpt for {suffix}",
        "post_text": f"Post text for {suffix}",
        "author_display_name": "Marcus Gollahon",
        "author_handle": "@marcus",
        "reply_count": "4",
        "repost_count": "2",
        "like_count": "18",
        "view_count": "1400"
    }
}))
PY
rm -f "$request_path"
`))
	t.Setenv("ODIN_DRIVER_REQUEST_PATH", requestPath)

	definitions := BuiltinDefinitions()
	bundle := definitions["browser_x_weekly_evidence_bundle"]
	if bundle.Invoke == nil {
		t.Fatal("browser_x_weekly_evidence_bundle invoke = nil, want handler")
	}
	result, err := bundle.Invoke(map[string]string{
		"target_urls": "https://x.com/marcus/status/123, https://x.com/marcus/status/456",
		"label":       "weekly-review",
	})
	if err != nil {
		t.Fatalf("browser_x_weekly_evidence_bundle invoke error = %v", err)
	}
	if result.KeyFacts["attempted_urls"] != "2" {
		t.Fatalf("attempted_urls = %q, want 2", result.KeyFacts["attempted_urls"])
	}
	if result.KeyFacts["recorded_evidence"] != "2" {
		t.Fatalf("recorded_evidence = %q, want 2", result.KeyFacts["recorded_evidence"])
	}
	if len(result.MemoryRecords) != 2 {
		t.Fatalf("memory records len = %d, want 2", len(result.MemoryRecords))
	}
	if result.MemoryRecords[0].Fields["bundle_label"] != "weekly-review" {
		t.Fatalf("bundle_label = %q, want weekly-review", result.MemoryRecords[0].Fields["bundle_label"])
	}
	if result.MemoryRecords[1].Fields["bundle_position"] != "2" {
		t.Fatalf("bundle_position = %q, want 2", result.MemoryRecords[1].Fields["bundle_position"])
	}
}

func TestBuiltinDefinitionsRejectMissingRequiredLiveMayBidInputs(t *testing.T) {
	definitions := BuiltinDefinitions()

	calendar := definitions["google_calendar_off_dates"]
	if calendar.Invoke == nil {
		t.Fatal("google_calendar_off_dates invoke = nil, want handler")
	}
	if _, err := calendar.Invoke(map[string]string{
		"calendar_id": "primary",
		"timezone":    "America/Chicago",
	}); err == nil {
		t.Fatal("google_calendar_off_dates invoke error = nil, want missing bid_period failure")
	}

	huginn := definitions["browser_pbs_session"]
	if huginn.Invoke == nil {
		t.Fatal("browser_pbs_session invoke = nil, want handler")
	}
	if _, err := huginn.Invoke(map[string]string{
		"bid_period": "2026-05",
		"timezone":   "America/Chicago",
	}); err == nil {
		t.Fatal("browser_pbs_session invoke error = nil, want missing workflow_key failure")
	}

	visual := definitions["browser_visual_audit"]
	if visual.Invoke == nil {
		t.Fatal("browser_visual_audit invoke = nil, want handler")
	}
	if _, err := visual.Invoke(map[string]string{
		"label": "cfipros-dashboard",
	}); err == nil {
		t.Fatal("browser_visual_audit invoke error = nil, want missing target_url failure")
	}

	xEvidence := definitions["browser_x_post_visible_evidence"]
	if xEvidence.Invoke == nil {
		t.Fatal("browser_x_post_visible_evidence invoke = nil, want handler")
	}
	if _, err := xEvidence.Invoke(map[string]string{
		"label": "marcus-crosswind",
	}); err == nil {
		t.Fatal("browser_x_post_visible_evidence invoke error = nil, want missing target_url failure")
	}

	bundle := definitions["browser_x_weekly_evidence_bundle"]
	if bundle.Invoke == nil {
		t.Fatal("browser_x_weekly_evidence_bundle invoke = nil, want handler")
	}
	if _, err := bundle.Invoke(map[string]string{
		"label": "weekly-review",
	}); err == nil {
		t.Fatal("browser_x_weekly_evidence_bundle invoke error = nil, want missing target_urls failure")
	}
}

func writeFixtureDriver(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "driver.sh")
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("WriteFile(driver) error = %v", err)
	}
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatalf("Chmod(driver) error = %v", err)
	}
	return path
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
