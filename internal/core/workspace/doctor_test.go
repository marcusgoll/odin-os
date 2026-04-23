package workspace

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	healthsvc "odin-os/internal/runtime/health"
)

func TestDoctorCheckIsNotApplicableWithoutEligibleProjects(t *testing.T) {
	registry := writeWorkspaceRegistry(t, map[string]string{})

	check := DoctorCheck(context.Background(), registry, func(string) string { return "" }, func(string) (string, error) {
		return "", errors.New("not found")
	})

	if check.Status != healthsvc.StatusHealthy {
		t.Fatalf("Status = %q, want %q", check.Status, healthsvc.StatusHealthy)
	}
	if !strings.Contains(check.Summary, "not applicable") {
		t.Fatalf("Summary = %q, want not applicable", check.Summary)
	}
}

func TestDoctorCheckDegradesWhenTMuxIsMissing(t *testing.T) {
	repoRoot := createGitRepo(t, "main")
	registry := writeWorkspaceRegistry(t, map[string]string{"alpha": repoRoot})

	check := DoctorCheck(context.Background(), registry, func(string) string { return "" }, func(name string) (string, error) {
		if name == "codex" {
			return filepath.Join(t.TempDir(), "codex"), nil
		}
		return "", errors.New("not found")
	})

	if check.Status != healthsvc.StatusDegraded {
		t.Fatalf("Status = %q, want %q", check.Status, healthsvc.StatusDegraded)
	}
	if !strings.Contains(check.Summary, "missing") {
		t.Fatalf("Summary = %q, want missing prereqs", check.Summary)
	}
	if got := check.Details["eligible_projects"]; got != "1" {
		t.Fatalf("eligible_projects = %q, want 1", got)
	}
}
