package integration_test

import (
	"strings"
	"testing"
)

func TestOperatorOverviewUsesCanonicalBoard(t *testing.T) {
	repoRoot := projectRoot(t)
	odinBinary := buildOdinBinary(t, repoRoot)
	runtimeRoot := t.TempDir()

	output, err := runOdinCommand(
		t,
		repoRoot,
		odinBinary,
		runtimeRoot,
		nil,
		"/project pbs\n/overview\n/quit\n",
		"repl",
	)
	if err != nil {
		t.Fatalf("runOdinCommand(repl overview) error = %v\n%s", err, output)
	}

	for _, want := range []string{
		"project=pbs scope=pbs",
		"Workspace",
		"Initiatives",
		"Work Items",
		"Run Attempts",
		"Companions",
		"Capability Catalog",
		"Approvals",
		"Observability",
		"Memory",
		"Intake Inbox",
		"Automation Triggers",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("overview output = %q, want %q", output, want)
		}
	}
	if strings.Contains(output, "Processes") {
		t.Fatalf("overview output = %q, must not introduce Processes lane", output)
	}
}
