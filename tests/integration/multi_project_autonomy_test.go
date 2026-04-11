package integration_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"odin-os/internal/store/sqlite"
)

type doctorReport struct {
	Status string            `json:"status"`
	Checks []json.RawMessage `json:"checks"`
}

type taskRunResponse struct {
	Status string `json:"status"`
}

type statusResponse struct {
	Health   string            `json:"health"`
	Projects []json.RawMessage `json:"projects"`
}

func TestOperationalAutonomyFreshRuntimeBecomesHealthy(t *testing.T) {
	repoRoot := projectRoot(t)
	odinBinary := buildOdinBinary(t, repoRoot)
	runtimeRoot := t.TempDir()

	output, err := runOdinCommand(t, repoRoot, odinBinary, runtimeRoot, nil, "", "doctor", "--json")
	if err != nil {
		t.Fatalf("runOdinCommand(doctor --json) error = %v\n%s", err, output)
	}

	var report doctorReport
	if err := json.Unmarshal([]byte(output), &report); err != nil {
		t.Fatalf("doctor output = %q, want valid JSON: %v", output, err)
	}
	if report.Status != "healthy" {
		t.Fatalf("status = %q, want healthy", report.Status)
	}
	if len(report.Checks) == 0 {
		t.Fatal("checks empty, want readiness checks")
	}
}

func TestOperationalAutonomyRequiresApprovalForHighRiskMutation(t *testing.T) {
	repoRoot := projectRoot(t)
	odinBinary := buildOdinBinary(t, repoRoot)
	runtimeRoot := seededRuntime(t)

	output, err := runOdinCommand(t, repoRoot, odinBinary, runtimeRoot, nil, "", "task", "run", "--project", "odin-core", "--action", "repo_rewrite")
	if err != nil {
		t.Fatalf("runOdinCommand(task run) error = %v\n%s", err, output)
	}

	var response taskRunResponse
	if err := json.Unmarshal([]byte(output), &response); err != nil {
		t.Fatalf("task run output = %q, want valid JSON: %v", output, err)
	}
	if response.Status != "awaiting_approval" {
		t.Fatalf("status = %q, want awaiting_approval", response.Status)
	}
}

func TestOperationalAutonomySchedulesAcrossMultipleProjects(t *testing.T) {
	repoRoot := projectRoot(t)
	odinBinary := buildOdinBinary(t, repoRoot)
	runtimeRoot := seededRuntimeWithProjects(t, "odin-core", "pbs", "odin-orchestrator")

	output, err := runOdinCommand(t, repoRoot, odinBinary, runtimeRoot, nil, "", "status", "--json")
	if err != nil {
		t.Fatalf("runOdinCommand(status --json) error = %v\n%s", err, output)
	}

	var response statusResponse
	if err := json.Unmarshal([]byte(output), &response); err != nil {
		t.Fatalf("status output = %q, want valid JSON: %v", output, err)
	}
	if len(response.Projects) < 3 {
		t.Fatalf("projects = %d, want at least 3", len(response.Projects))
	}
}

func seededRuntime(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

func seededRuntimeWithProjects(t *testing.T, projectKeys ...string) string {
	t.Helper()

	runtimeRoot := t.TempDir()
	store := openRuntimeStore(t, runtimeRoot)
	defer store.Close()

	for _, key := range projectKeys {
		scope := "project"
		if key == "odin-core" {
			scope = "odin-core"
		}
		repoDir := filepath.Join(runtimeRoot, "repos", key)
		if err := os.MkdirAll(repoDir, 0o755); err != nil {
			t.Fatalf("MkdirAll(%s) error = %v", repoDir, err)
		}
		if _, err := store.CreateProject(t.Context(), sqlite.CreateProjectParams{
			Key:           key,
			Name:          key,
			Scope:         scope,
			GitRoot:       repoDir,
			DefaultBranch: "main",
			ManifestPath:  fmt.Sprintf("seed/%s.yaml", key),
		}); err != nil {
			t.Fatalf("CreateProject(%s) error = %v", key, err)
		}
	}

	return runtimeRoot
}
