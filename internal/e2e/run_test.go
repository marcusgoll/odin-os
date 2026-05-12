package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestFixtureSetRunsLocallyWithoutLiveSystems(t *testing.T) {
	t.Parallel()

	repoRoot := filepath.Clean(filepath.Join("..", ".."))
	fixtures := []string{
		"github-readonly-intake.yaml",
		"github-issue-delivery-dry-run.yaml",
		"tracker-dry-run-lifecycle.yaml",
		"workspace-safe-creation.yaml",
		"prompt-rendering-brownfield.yaml",
		"failure-analysis.yaml",
	}

	for _, fixture := range fixtures {
		fixture := fixture
		t.Run(fixture, func(t *testing.T) {
			t.Parallel()

			var stdout bytes.Buffer
			err := Run(context.Background(), repoRoot, []string{
				"--scenario", filepath.Join("fixtures", "e2e", fixture),
				"--json",
			}, &stdout)
			if err != nil {
				t.Fatalf("Run(%s) error = %v\noutput:\n%s", fixture, err, stdout.String())
			}

			var report struct {
				Status   string `json:"status"`
				Scenario string `json:"scenario"`
				OdinRoot string `json:"odin_root"`
				Stages   []struct {
					Name   string `json:"name"`
					Status string `json:"status"`
				} `json:"stages"`
				GitHub struct {
					Mode    string `json:"mode"`
					Mutated bool   `json:"mutated"`
				} `json:"github"`
				Codex struct {
					Mode    string `json:"mode"`
					Invoked bool   `json:"invoked"`
				} `json:"codex"`
			}
			if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
				t.Fatalf("json.Unmarshal(%s) error = %v\noutput:\n%s", fixture, err, stdout.String())
			}
			if report.Status != "passed" {
				t.Fatalf("report.Status = %q, want passed\noutput:\n%s", report.Status, stdout.String())
			}
			if report.OdinRoot == "" || filepath.Clean(report.OdinRoot) == repoRoot {
				t.Fatalf("odin_root = %q, want temp root distinct from repo root %q", report.OdinRoot, repoRoot)
			}
			if report.GitHub.Mode != "fixture" || report.GitHub.Mutated {
				t.Fatalf("github guard = %+v, want fixture and not mutated", report.GitHub)
			}
			if report.Codex.Mode != "disabled" || report.Codex.Invoked {
				t.Fatalf("codex guard = %+v, want disabled and not invoked", report.Codex)
			}
			for _, stage := range report.Stages {
				if stage.Name == "" || stage.Status != "passed" {
					t.Fatalf("stage = %+v, want named passed stage", stage)
				}
			}
		})
	}
}
