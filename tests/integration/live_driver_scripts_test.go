package integration_test

import (
	"encoding/json"
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
