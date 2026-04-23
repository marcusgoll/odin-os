package render

import (
	"strings"
	"testing"
)

func TestRenderHeaderIncludesScopeModeHealthApprovalsAndActiveTask(t *testing.T) {
	t.Parallel()

	header := Header{
		Scope:            "cfipros",
		Mode:             "ask",
		Health:           "ok",
		PendingApprovals: 2,
		SelectedSkill:    "pixel-perfect-ui-ux-designer",
		SelectedWorkflow: "marcus-social-growth-workflow",
		ActiveTask:       "task-12",
	}

	rendered := RenderHeader(header)

	for _, want := range []string{
		"scope=cfipros",
		"mode=ask",
		"health=ok",
		"approvals=2",
		"skill=pixel-perfect-ui-ux-designer",
		"workflow=marcus-social-growth-workflow",
		"task=task-12",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("RenderHeader() = %q, want substring %q", rendered, want)
		}
	}
}

func TestRenderHeaderIncludesActiveRunWhenPresent(t *testing.T) {
	t.Parallel()

	header := Header{
		Scope:            "odin-core",
		Mode:             "act",
		Health:           "degraded",
		PendingApprovals: 0,
		ActiveRun:        "run-7",
	}

	rendered := RenderHeader(header)
	if !strings.Contains(rendered, "run=run-7") {
		t.Fatalf("RenderHeader() = %q, want active run", rendered)
	}
}
