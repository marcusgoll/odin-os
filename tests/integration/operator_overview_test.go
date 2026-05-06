package integration_test

import (
	"encoding/json"
	"os"
	"path/filepath"
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

	payloadPath := filepath.Join(t.TempDir(), "intake-payload.json")
	if err := os.WriteFile(payloadPath, []byte(`{"workflow_id":"pbs-ci","run_id":"42"}`), 0o644); err != nil {
		t.Fatalf("WriteFile(intake payload) error = %v", err)
	}
	intakeOutput, err := runOdinCommand(
		t,
		repoRoot,
		odinBinary,
		runtimeRoot,
		nil,
		"",
		"intake",
		"enqueue",
		"--source",
		"n8n",
		"--project",
		"pbs",
		"--title",
		"Review intake overview lane",
		"--type",
		"ci_failure",
		"--dedup-key",
		"ci_failure:pbs:overview",
		"--requested-by",
		"n8n",
		"--payload-file",
		payloadPath,
	)
	if err != nil {
		t.Fatalf("runOdinCommand(intake enqueue) error = %v\n%s", err, intakeOutput)
	}
	if !strings.Contains(intakeOutput, "queued intake task") {
		t.Fatalf("intake enqueue output = %q, want queued intake task", intakeOutput)
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
		"wiring=live source=task_intakes status=linked_evidence count=1",
		"linked intake evidence",
		"linked_intake=1 source=n8n type=ci_failure dedup_key=ci_failure:pbs:overview requested_by=n8n",
		"work_status=queued initiative=pbs",
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
	if strings.Contains(output, "raw_intake=1") {
		t.Fatalf("overview output must not label task_intakes as raw Intake Items:\n%s", output)
	}
	if strings.Contains(output, "source=task_intakes status=raw_review") {
		t.Fatalf("overview output must not label task_intakes as raw governed intake review:\n%s", output)
	}
	if strings.Contains(output, "automation trigger overview projection not implemented") {
		t.Fatalf("overview output still contains placeholder automation trigger note:\n%s", output)
	}
	if strings.Contains(output, "intake overview projection not implemented") {
		t.Fatalf("overview output still contains placeholder intake note:\n%s", output)
	}
}
