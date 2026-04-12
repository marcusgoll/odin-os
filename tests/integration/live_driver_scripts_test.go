package integration_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"odin-os/internal/store/sqlite"
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

func TestLiveDriverScripts(t *testing.T) {
	t.Run("project status returns runtime-backed JSON", func(t *testing.T) {
		runtimeRoot := t.TempDir()
		dataDir := filepath.Join(runtimeRoot, "data")
		if err := os.MkdirAll(dataDir, 0o755); err != nil {
			t.Fatalf("MkdirAll(data) error = %v", err)
		}

		store, err := sqlite.Open(filepath.Join(dataDir, "odin.db"))
		if err != nil {
			t.Fatalf("sqlite.Open() error = %v", err)
		}
		defer store.Close()

		ctx := t.Context()
		if err := store.Migrate(ctx); err != nil {
			t.Fatalf("Migrate() error = %v", err)
		}
		project, err := store.CreateProject(ctx, sqlite.CreateProjectParams{
			Key:           "alpha",
			Name:          "Alpha",
			Scope:         "project",
			GitRoot:       runtimeRoot,
			DefaultBranch: "main",
		})
		if err != nil {
			t.Fatalf("CreateProject() error = %v", err)
		}
		if _, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
			ProjectID:   project.ID,
			Key:         "alpha-queued",
			Title:       "Queued runtime task",
			Status:      "queued",
			Scope:       "project",
			RequestedBy: "test",
		}); err != nil {
			t.Fatalf("CreateTask() error = %v", err)
		}
		if _, err := store.CreateTask(ctx, sqlite.CreateTaskParams{
			ProjectID:   project.ID,
			Key:         "alpha-dead-letter",
			Title:       "Dead letter runtime task",
			Status:      "dead_letter",
			Scope:       "project",
			RequestedBy: "test",
		}); err != nil {
			t.Fatalf("CreateTask(dead_letter) error = %v", err)
		}

		repoRoot := projectRoot(t)
		scriptPath := filepath.Join(repoRoot, "scripts", "drivers", "project-status.sh")
		requestBody, err := json.Marshal(map[string]any{
			"tool":         "project_status",
			"runtime_root": runtimeRoot,
			"args": map[string]string{
				"project_key": "alpha",
			},
		})
		if err != nil {
			t.Fatalf("Marshal(request) error = %v", err)
		}

		cmd := exec.Command(scriptPath)
		cmd.Stdin = strings.NewReader(string(requestBody))
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("project-status driver error = %v\n%s", err, string(output))
		}

		var result struct {
			Source    string            `json:"source"`
			KeyFacts  map[string]string `json:"key_facts"`
			RawOutput string            `json:"raw_output"`
		}
		if err := json.Unmarshal(output, &result); err != nil {
			t.Fatalf("driver output = %q, want JSON: %v", string(output), err)
		}
		if result.Source != "driver" {
			t.Fatalf("Source = %q, want driver", result.Source)
		}
		if result.KeyFacts["open_task_count"] != "1" {
			t.Fatalf("open_task_count = %q, want 1", result.KeyFacts["open_task_count"])
		}
		if !strings.Contains(result.RawOutput, "project=alpha") {
			t.Fatalf("RawOutput = %q, want project marker", result.RawOutput)
		}
	})
}
