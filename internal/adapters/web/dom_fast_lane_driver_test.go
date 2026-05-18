package web

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDOMFastLaneDriverInvokesConfiguredDriver(t *testing.T) {
	requestPath := filepath.Join(t.TempDir(), "request.json")
	script := writeFixtureDriver(t, `#!/usr/bin/env bash
cat >"$ODIN_DRIVER_REQUEST_PATH"
printf '{"status":"completed","tool_key":"browser_dom_fast_lane","summary":"Extracted fixture status table.","artifacts":{"recipe_key":"fixture_status","source_url":"http://127.0.0.1:18080/status-fixture","final_url":"http://127.0.0.1:18080/status-fixture","selector_version":"fixture-v1","snapshot_excerpt":"Ready alpha green"}}'
`)
	t.Setenv("ODIN_HUGINN_DOM_FAST_LANE_DRIVER", script)
	t.Setenv("ODIN_DRIVER_REQUEST_PATH", requestPath)

	driver := NewDOMFastLaneDriver()
	response, err := driver.Invoke(context.Background(), DOMFastLaneRequest{
		Input: DOMFastLaneInput{
			RecipeKey:     "fixture_status",
			TargetURL:     "http://127.0.0.1:18080/status-fixture",
			Label:         "fixture-status",
			WaitMS:        "0",
			Headless:      "true",
			AllowedDomain: "127.0.0.1",
		},
	})
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if response.ToolKey != "browser_dom_fast_lane" {
		t.Fatalf("ToolKey = %q, want browser_dom_fast_lane", response.ToolKey)
	}
	if response.Artifacts["recipe_key"] != "fixture_status" {
		t.Fatalf("Artifacts.recipe_key = %#v, want fixture_status", response.Artifacts["recipe_key"])
	}
	if response.Artifacts["final_url"] != "http://127.0.0.1:18080/status-fixture" {
		t.Fatalf("Artifacts.final_url = %#v, want fixture URL", response.Artifacts["final_url"])
	}
	if response.Artifacts["selector_version"] != "fixture-v1" {
		t.Fatalf("Artifacts.selector_version = %#v, want fixture-v1", response.Artifacts["selector_version"])
	}
	if response.Artifacts["snapshot_excerpt"] != "Ready alpha green" {
		t.Fatalf("Artifacts.snapshot_excerpt = %#v, want visible evidence", response.Artifacts["snapshot_excerpt"])
	}

	requestBytes, err := os.ReadFile(requestPath)
	if err != nil {
		t.Fatalf("ReadFile(request) error = %v", err)
	}
	if !strings.Contains(string(requestBytes), `"tool_key":"browser_dom_fast_lane"`) {
		t.Fatalf("request = %s, want browser_dom_fast_lane tool key", string(requestBytes))
	}
	var request DOMFastLaneRequest
	if err := json.Unmarshal(requestBytes, &request); err != nil {
		t.Fatalf("request json = %v", err)
	}
	if request.Input.RecipeKey != "fixture_status" {
		t.Fatalf("Request.Input.RecipeKey = %q, want fixture_status", request.Input.RecipeKey)
	}
	if request.Input.AllowedDomain != "127.0.0.1" {
		t.Fatalf("Request.Input.AllowedDomain = %q, want 127.0.0.1", request.Input.AllowedDomain)
	}
}

func TestDOMFastLaneDriverAllowsBlockedStatus(t *testing.T) {
	script := writeFixtureDriver(t, `#!/usr/bin/env bash
printf '{"status":"blocked","tool_key":"browser_dom_fast_lane","summary":"Selector drift blocked fixture extraction.","artifacts":{"recipe_key":"fixture_status","intervention_reason":"selector_drift","selector_version":"fixture-v1"}}'
`)
	t.Setenv("ODIN_HUGINN_DOM_FAST_LANE_DRIVER", script)

	driver := NewDOMFastLaneDriver()
	response, err := driver.Invoke(context.Background(), DOMFastLaneRequest{
		Input: DOMFastLaneInput{
			RecipeKey: "fixture_status",
			TargetURL: "http://127.0.0.1:18080/status-fixture",
		},
	})
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if response.Status != "blocked" {
		t.Fatalf("Status = %q, want blocked", response.Status)
	}
	if response.Artifacts["intervention_reason"] != "selector_drift" {
		t.Fatalf("Artifacts.intervention_reason = %#v, want selector_drift", response.Artifacts["intervention_reason"])
	}
}

func TestDOMFastLaneDriverRejectsMissingDriver(t *testing.T) {
	t.Setenv("ODIN_HUGINN_DOM_FAST_LANE_DRIVER", "")

	driver := NewDOMFastLaneDriver()
	if _, err := driver.Invoke(context.Background(), DOMFastLaneRequest{ToolKey: "browser_dom_fast_lane"}); err == nil {
		t.Fatal("Invoke() error = nil, want missing driver config failure")
	}
}

func TestDOMFastLaneDriverRejectsUnexpectedToolKey(t *testing.T) {
	script := writeFixtureDriver(t, `#!/usr/bin/env bash
printf '{"status":"completed","tool_key":"browser_visual_audit","summary":"wrong tool","artifacts":{}}'
`)
	t.Setenv("ODIN_HUGINN_DOM_FAST_LANE_DRIVER", script)

	driver := NewDOMFastLaneDriver()
	_, err := driver.Invoke(context.Background(), DOMFastLaneRequest{
		Input: DOMFastLaneInput{
			RecipeKey: "fixture_status",
			TargetURL: "http://127.0.0.1:18080/status-fixture",
		},
	})
	if err == nil || !strings.Contains(err.Error(), "does not match request") {
		t.Fatalf("Invoke() error = %v, want tool mismatch", err)
	}
}

func TestDOMFastLaneDriverRejectsUnexpectedStatus(t *testing.T) {
	script := writeFixtureDriver(t, `#!/usr/bin/env bash
printf '{"status":"mutated","tool_key":"browser_dom_fast_lane","summary":"mutated state","artifacts":{}}'
`)
	t.Setenv("ODIN_HUGINN_DOM_FAST_LANE_DRIVER", script)

	driver := NewDOMFastLaneDriver()
	_, err := driver.Invoke(context.Background(), DOMFastLaneRequest{
		Input: DOMFastLaneInput{
			RecipeKey: "fixture_status",
			TargetURL: "http://127.0.0.1:18080/status-fixture",
		},
	})
	if err == nil || !strings.Contains(err.Error(), `status "mutated" is not allowed`) {
		t.Fatalf("Invoke() error = %v, want invalid status refusal", err)
	}
}
