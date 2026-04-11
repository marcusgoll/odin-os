package integration_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCodexHeadlessDriverScriptReturnsStructuredJSON(t *testing.T) {
	t.Setenv("ODIN_CODEX_DRIVER_MODE", "fixture")

	repoRoot := projectRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "drivers", "codex-headless.sh")
	cmd := exec.Command(scriptPath)
	cmd.Stdin = strings.NewReader(`{"id":"driver-smoke","kind":"general","scope":"project","prompt":"say ready","metadata":{"project_key":"alpha"}}`)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("driver script error = %v\n%s", err, string(output))
	}

	var result struct {
		Status   string            `json:"status"`
		Output   string            `json:"output"`
		Metadata map[string]string `json:"metadata"`
	}
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("driver output = %q, want JSON: %v", string(output), err)
	}
	if result.Status != "completed" {
		t.Fatalf("Status = %q, want completed", result.Status)
	}
	if result.Metadata["driver"] != "codex_headless_script" {
		t.Fatalf("driver metadata = %q, want codex_headless_script", result.Metadata["driver"])
	}
}

func TestCodexHeadlessDriverScriptDelegatesToConfiguredCommand(t *testing.T) {
	repoRoot := projectRoot(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "drivers", "codex-headless.sh")
	stubPath := filepath.Join(t.TempDir(), "codex-driver-stub.sh")
	if err := os.WriteFile(stubPath, []byte(`#!/usr/bin/env bash
python3 -c 'import json,sys; spec=json.load(sys.stdin); json.dump({"status":"completed","output":"delegated "+spec.get("id",""),"metadata":{"driver":"delegated_stub"}}, sys.stdout)'
`), 0o755); err != nil {
		t.Fatalf("WriteFile(stub) error = %v", err)
	}

	cmd := exec.Command(scriptPath)
	cmd.Env = append(os.Environ(), "ODIN_CODEX_DRIVER_COMMAND="+stubPath)
	cmd.Stdin = strings.NewReader(`{"id":"driver-smoke","kind":"general","scope":"project","prompt":"say ready","metadata":{"project_key":"alpha"}}`)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("driver script error = %v\n%s", err, string(output))
	}

	var result struct {
		Status   string            `json:"status"`
		Output   string            `json:"output"`
		Metadata map[string]string `json:"metadata"`
	}
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("driver output = %q, want JSON: %v", string(output), err)
	}
	if result.Metadata["driver"] != "delegated_stub" {
		t.Fatalf("driver metadata = %q, want delegated_stub", result.Metadata["driver"])
	}
	if result.Output != "delegated driver-smoke" {
		t.Fatalf("Output = %q, want delegated driver-smoke", result.Output)
	}
}
