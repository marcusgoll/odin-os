package workspace

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"time"

	coreprojects "odin-os/internal/core/projects"
	healthsvc "odin-os/internal/runtime/health"
)

func DoctorCheck(_ context.Context, registry coreprojects.Registry, getenv func(string) string, lookPath func(string) (string, error)) healthsvc.Check {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	if lookPath == nil {
		lookPath = exec.LookPath
	}

	eligibleProjects := 0
	for _, manifest := range registry.Projects() {
		if ok, _ := workspaceEligible(manifest); ok {
			eligibleProjects++
		}
	}

	check := healthsvc.Check{
		Name:    "workspace_prerequisites",
		Status:  healthsvc.StatusHealthy,
		Summary: "workspace prerequisites not applicable",
		Details: map[string]string{
			"eligible_projects": strconv.Itoa(eligibleProjects),
			"applicable":        "false",
		},
		ObservedAt: time.Now().UTC(),
	}
	if eligibleProjects == 0 {
		return check
	}

	check.Details["applicable"] = "true"
	codexBin := strings.TrimSpace(getenv(EnvCodexBin))
	if codexBin == "" {
		codexBin = "codex"
	}
	check.Details["codex_bin"] = codexBin

	tmuxPath, tmuxErr := lookPath("tmux")
	if tmuxErr == nil {
		check.Details["tmux_path"] = tmuxPath
	}
	codexPath, codexErr := lookPath(codexBin)
	if codexErr == nil {
		check.Details["codex_path"] = codexPath
	}

	if tmuxErr == nil && codexErr == nil {
		check.Summary = "workspace prerequisites are ready"
		return check
	}

	check.Status = healthsvc.StatusDegraded
	check.Summary = "workspace prerequisites are missing"
	if tmuxErr != nil {
		check.Details["tmux_missing"] = "true"
	}
	if codexErr != nil {
		check.Details["codex_missing"] = "true"
	}
	return check
}
