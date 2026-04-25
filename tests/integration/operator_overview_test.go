package integration_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestOperatorOverviewUsesCanonicalBoard(t *testing.T) {
	repoRoot := projectRoot(t)
	odinBinary := buildOdinBinary(t, repoRoot)
	runtimeRoot := t.TempDir()

	bootstrapOutput, err := runOdinCommand(
		t,
		repoRoot,
		odinBinary,
		runtimeRoot,
		nil,
		"/project pbs\n/transition set shadow because register pbs managed project for overview proof\n/quit\n",
		"repl",
	)
	if err != nil {
		t.Fatalf("runOdinCommand(repl transition setup) error = %v\n%s", err, bootstrapOutput)
	}
	if !strings.Contains(bootstrapOutput, "project=pbs state=shadow") {
		t.Fatalf("transition setup output = %q, want project=pbs state=shadow", bootstrapOutput)
	}

	addOutput, err := runOdinCommand(
		t,
		repoRoot,
		odinBinary,
		runtimeRoot,
		nil,
		"",
		"followup",
		"add",
		"--initiative",
		"pbs",
		"--title",
		"Review automation trigger lane",
		"--cadence",
		"daily",
	)
	if err != nil {
		t.Fatalf("runOdinCommand(followup add) error = %v\n%s", err, addOutput)
	}
	if !strings.Contains(addOutput, "created follow-up") {
		t.Fatalf("followup add output = %q, want created follow-up", addOutput)
	}

	listOutput, err := runOdinCommand(
		t,
		repoRoot,
		odinBinary,
		runtimeRoot,
		nil,
		"",
		"followup",
		"list",
		"--json",
	)
	if err != nil {
		t.Fatalf("runOdinCommand(followup list --json) error = %v\n%s", err, listOutput)
	}
	var followupsView struct {
		Obligations []struct {
			NextDueAt time.Time `json:"next_due_at"`
		} `json:"obligations"`
	}
	if err := json.Unmarshal([]byte(listOutput), &followupsView); err != nil {
		t.Fatalf("json.Unmarshal(followup list) error = %v\n%s", err, listOutput)
	}
	if len(followupsView.Obligations) != 1 {
		t.Fatalf("followup list obligations len = %d, want 1", len(followupsView.Obligations))
	}
	fakeNow := followupsView.Obligations[0].NextDueAt.Add(time.Hour).UTC().Format(time.RFC3339Nano)

	output, err := runOdinCommand(
		t,
		repoRoot,
		odinBinary,
		runtimeRoot,
		map[string]string{"ODIN_NOW": fakeNow},
		"/project pbs\n/overview\nshow workspace overview\n/quit\n",
		"repl",
	)
	if err != nil {
		t.Fatalf("runOdinCommand(repl overview) error = %v\n%s", err, output)
	}

	for _, want := range []string{
		"project=pbs scope=pbs",
		"Attention",
		"Active Execution",
		"Workspace",
		"Initiatives",
		"pbs title=PBS",
		"Work Items",
		"Run Attempts",
		"Companions",
		"Capability Catalog",
		"Approvals",
		"Observability",
		"Memory",
		"Intake Inbox",
		"Automation Triggers",
		"wiring=live count=1",
		"trigger=1 title=Review automation trigger lane status=active",
		"due_status=due",
		"initiative=pbs",
		"target_project=pbs",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("overview output = %q, want %q", output, want)
		}
	}
	if strings.Contains(output, "Processes") {
		t.Fatalf("overview output = %q, must not introduce Processes lane", output)
	}
	if strings.Contains(output, "created task") {
		t.Fatalf("ask-mode overview should not create durable work:\n%s", output)
	}
	if strings.Contains(output, "automation trigger overview projection not implemented") {
		t.Fatalf("overview output still contains placeholder automation trigger note:\n%s", output)
	}
}
