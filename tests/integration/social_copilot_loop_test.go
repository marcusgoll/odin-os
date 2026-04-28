package integration_test

import (
	"context"
	"strings"
	"testing"

	"odin-os/internal/store/sqlite"
)

func TestMarcusSocialCopilotLoopCLIIntegration(t *testing.T) {
	ctx := context.Background()
	repoRoot := projectRoot(t)
	odinBinary := buildOdinBinary(t, repoRoot)
	runtimeRoot := t.TempDir()

	stdin := strings.Join([]string{
		"/workflow validate marcus-social-growth-workflow",
		"/workflow use marcus-social-growth-workflow",
		"/workflow social scope replace marcus_own=timeline,mentions target=https://x.com/example/status/123 account=@AviationDaily",
		"/workflow social status",
		"/workflow social wake reason=manual-proof",
		"/jobs",
		"/runs",
		"",
	}, "\n")

	output, err := runOdinCommand(t, repoRoot, odinBinary, runtimeRoot, nil, stdin, "repl")
	if err != nil {
		t.Fatalf("runOdinCommand(interactive social copilot loop) error = %v\n%s", err, output)
	}

	for _, want := range []string{
		"workflow=marcus-social-growth-workflow status=ready",
		"workflow=marcus-social-growth-workflow selected=true status=ready",
		"workflow=marcus-social-growth-workflow status=scope_replaced task=workflow-marcus-social-growth-workflow-social-copilot-loop targets=4 account_actions=none",
		"workflow=marcus-social-growth-workflow status=scheduled task=workflow-marcus-social-growth-workflow-social-copilot-loop targets=4 account_actions=none",
		"wake=manual status=completed workflow=marcus-social-growth-workflow",
		"executor=social_copilot account_actions=none memory_created=0",
		"odin-core workflow-marcus-social-growth-workflow-social-copilot-loop scheduled",
		"workflow-marcus-social-growth-workflow-social-copilot-loop social_copilot completed",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("interactive output missing %q:\n%s", want, output)
		}
	}

	store := openRuntimeStore(t, runtimeRoot)
	defer store.Close()

	outcomes, err := store.ListMemorySummaries(ctx, sqlite.ListMemorySummariesParams{
		Scope:      "workflow",
		ScopeKey:   "marcus-social-growth-workflow",
		MemoryType: "social_outcome",
	})
	if err != nil {
		t.Fatalf("ListMemorySummaries(social_outcome) error = %v", err)
	}
	if len(outcomes) != 0 {
		t.Fatalf("social_outcome summaries len = %d, want no autonomous published outcome", len(outcomes))
	}
}
