package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"odin-os/internal/runtime/triggers"
	"odin-os/internal/store/sqlite"
)

func TestRunTriggerHelpPrintsUsage(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	err := RunTrigger(context.Background(), triggers.Service{}, []string{"--help"}, &stdout)
	if err != nil {
		t.Fatalf("RunTrigger(--help) error = %v", err)
	}
	if got := stdout.String(); !strings.Contains(got, "usage: odin trigger") {
		t.Fatalf("stdout = %q, want trigger usage", got)
	}
	for _, want := range []string{
		"event=external.github.issue",
		"odin trigger test <key> source=events",
		"odin trigger seed cfipros-ceo-day-routine",
	} {
		if got := stdout.String(); !strings.Contains(got, want) {
			t.Fatalf("stdout = %q, want %q", got, want)
		}
	}
	if got := stdout.String(); strings.Contains(got, "external.github_issue") {
		t.Fatalf("stdout = %q, want no underscore GitHub issue event type example", got)
	}
}

func TestRunTriggerNoArgsReturnsUsageError(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	err := RunTrigger(context.Background(), triggers.Service{}, nil, &stdout)
	if err == nil {
		t.Fatal("RunTrigger() error = nil, want usage error")
	}
	if got := err.Error(); !strings.Contains(got, "usage: odin trigger") {
		t.Fatalf("error = %q, want trigger usage", got)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty output", stdout.String())
	}
}

func TestRunTriggerTestEventsReportsReadOnlyProof(t *testing.T) {
	ctx := context.Background()
	store := openWorkspaceCommandTestStore(t)
	defer store.Close()

	repoRoot := createWorkspaceCommandGitRepo(t, "main")
	service := triggers.Service{
		Store:    store,
		Registry: writeWorkspaceCommandRegistry(t, map[string]string{"odin-core": repoRoot}),
	}
	run := func(args ...string) string {
		t.Helper()
		var stdout bytes.Buffer
		if err := RunTrigger(ctx, service, args, &stdout); err != nil {
			t.Fatalf("RunTrigger(%v) error = %v\nstdout=%s", args, err, stdout.String())
		}
		return stdout.String()
	}

	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	store.Now = func() time.Time {
		return now
	}
	run("create", "gh-opened",
		"initiative=odin-core",
		"kind=event",
		"status=enabled",
		"event=external.github.issue",
		"match_provider=github",
		"match_repo=marcusgoll/odin-os",
		"title=GH_opened",
		"summary=github_opened",
		"intent=governance",
		"--json",
	)

	store.Now = func() time.Time {
		return now.Add(time.Minute)
	}
	run("ingest", "github-issue",
		"project=odin-core",
		"repo=marcusgoll/odin-os",
		"number=123",
		"action=opened",
		"title=Issue_opened",
		"labels=bug",
		"--json",
	)

	beforeTasks := countCommandTasks(t, ctx, store)
	output := run("test", "gh-opened", "source=events", "now=2026-05-10T12:02:00Z", "--json")
	for _, want := range []string{
		`"decision": "run"`,
		`"event_type": "external.github.issue"`,
		`"event_envelope"`,
		`"source": "event"`,
		`"dedupe_key": "default:gh-opened:event:external-github-issue-marcusgoll-odin-os-123-opened"`,
		`"candidate_events": 1`,
		`"matched_events"`,
		`"approval_required": true`,
		`"mutates": false`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("trigger test output = %s, want %s", output, want)
		}
	}
	if tasks := countCommandTasks(t, ctx, store); tasks != beforeTasks {
		t.Fatalf("task count after trigger test = %d, want unchanged %d", tasks, beforeTasks)
	}
	if materializations := countCommandAutomationTriggerMaterializations(t, ctx, store); materializations != 0 {
		t.Fatalf("materialization count after trigger test = %d, want 0", materializations)
	}

	auditOutput := run("audit", "gh-opened", "--json")
	if !strings.Contains(auditOutput, `"event_type": "automation_trigger.tested"`) {
		t.Fatalf("trigger audit output = %s, want tested audit event", auditOutput)
	}
	for _, want := range []string{
		`"envelope"`,
		`"source": "event"`,
		`"dedupe_key": "default:gh-opened:event:external-github-issue-marcusgoll-odin-os-123-opened"`,
	} {
		if !strings.Contains(auditOutput, want) {
			t.Fatalf("trigger audit output = %s, want %s", auditOutput, want)
		}
	}
}

func TestRunTriggerSeedMarcusBrandOSCreatesSkillInvocationSchedules(t *testing.T) {
	ctx := context.Background()
	store := openWorkspaceCommandTestStore(t)
	defer store.Close()

	repoRoot := createWorkspaceCommandGitRepo(t, "main")
	service := triggers.Service{
		Store:    store,
		Registry: writeWorkspaceCommandRegistry(t, map[string]string{"marcusgoll": repoRoot}),
	}
	run := func(args ...string) string {
		t.Helper()
		var stdout bytes.Buffer
		if err := RunTrigger(ctx, service, args, &stdout); err != nil {
			t.Fatalf("RunTrigger(%v) error = %v\nstdout=%s", args, err, stdout.String())
		}
		return stdout.String()
	}

	output := run("seed", "marcus-brand-os", "start=2026-05-18", "--json")
	var seed struct {
		Seed       string `json:"seed"`
		Workspace  string `json:"workspace"`
		Initiative string `json:"initiative"`
		Status     string `json:"status"`
		Timezone   string `json:"timezone"`
		Triggers   []struct {
			Key            string  `json:"key"`
			Status         string  `json:"status"`
			Kind           string  `json:"kind"`
			RuleJSON       string  `json:"rule_json"`
			NextEligibleAt *string `json:"next_eligible_at"`
		} `json:"triggers"`
	}
	if err := json.Unmarshal([]byte(output), &seed); err != nil {
		t.Fatalf("json.Unmarshal(seed) error = %v\n%s", err, output)
	}
	if seed.Seed != "marcus-brand-os" || seed.Initiative != "marcusgoll" || seed.Status != "enabled" || seed.Timezone != "America/New_York" {
		t.Fatalf("seed output = %+v, want marcus brand defaults", seed)
	}
	if len(seed.Triggers) != len(marcusBrandOSRoutines) {
		t.Fatalf("seed triggers = %d, want %d", len(seed.Triggers), len(marcusBrandOSRoutines))
	}

	rulesByKey := map[string]string{}
	for _, trigger := range seed.Triggers {
		rulesByKey[trigger.Key] = trigger.RuleJSON
		if trigger.Status != "enabled" || trigger.Kind != "schedule" || trigger.NextEligibleAt == nil {
			t.Fatalf("seed trigger = %+v, want enabled schedule with next run", trigger)
		}
		var rule struct {
			ExecutionIntent string `json:"execution_intent"`
			QuietTimezone   string `json:"quiet_timezone"`
			SkillInvocation struct {
				SkillKey              string `json:"skill_key"`
				ProjectKey            string `json:"project_key"`
				ExecutionIntent       string `json:"execution_intent"`
				ExecutionIntentSource string `json:"execution_intent_source"`
				ReviewState           string `json:"review_state"`
			} `json:"skill_invocation"`
		}
		if err := json.Unmarshal([]byte(trigger.RuleJSON), &rule); err != nil {
			t.Fatalf("json.Unmarshal(rule_json) error = %v\n%s", err, trigger.RuleJSON)
		}
		if rule.ExecutionIntent != "read_only" || rule.QuietTimezone != "UTC" || rule.SkillInvocation.ProjectKey != "marcusgoll" || rule.SkillInvocation.ExecutionIntent != "read_only" || rule.SkillInvocation.ExecutionIntentSource != "trigger" || rule.SkillInvocation.ReviewState != "review_required" {
			t.Fatalf("rule = %+v, want read-only review-required skill binding", rule)
		}
	}
	if got, ok := rulesByKey["marcus-brand-morning-editorial-scan"]; !ok || !strings.Contains(got, `"skill_key":"marcus-editorial-strategist"`) {
		t.Fatalf("morning editorial rule = %s, want editorial strategist binding", got)
	}

	fireOutput := run("fire", "marcus-brand-morning-editorial-scan", "reason=seed-proof", "--json")
	for _, want := range []string{
		`"work_kind": "skill_invocation"`,
		`"execution_intent": "read_only"`,
		`"execution_intent_source": "skill_binding:trigger"`,
		`"created_work_item": true`,
	} {
		if !strings.Contains(fireOutput, want) {
			t.Fatalf("fire output = %s, want %s", fireOutput, want)
		}
	}
}

func TestRunTriggerSeedCFIProsCEODayRoutineCreatesCEOAgentRoutineSchedules(t *testing.T) {
	ctx := context.Background()
	store := openWorkspaceCommandTestStore(t)
	defer store.Close()

	repoRoot := createWorkspaceCommandGitRepo(t, "main")
	service := triggers.Service{
		Store:    store,
		Registry: writeWorkspaceCommandRegistry(t, map[string]string{"cfipros": repoRoot}),
	}
	run := func(args ...string) string {
		t.Helper()
		var stdout bytes.Buffer
		if err := RunTrigger(ctx, service, args, &stdout); err != nil {
			t.Fatalf("RunTrigger(%v) error = %v\nstdout=%s", args, err, stdout.String())
		}
		return stdout.String()
	}

	output := run("seed", "cfipros-ceo-day-routine", "start=2026-05-18", "--json")
	var seed struct {
		Seed       string `json:"seed"`
		Workspace  string `json:"workspace"`
		Initiative string `json:"initiative"`
		Status     string `json:"status"`
		Timezone   string `json:"timezone"`
		Triggers   []struct {
			Key            string  `json:"key"`
			Status         string  `json:"status"`
			Kind           string  `json:"kind"`
			RuleJSON       string  `json:"rule_json"`
			NextEligibleAt *string `json:"next_eligible_at"`
		} `json:"triggers"`
	}
	if err := json.Unmarshal([]byte(output), &seed); err != nil {
		t.Fatalf("json.Unmarshal(seed) error = %v\n%s", err, output)
	}
	if seed.Seed != "cfipros-ceo-day-routine" || seed.Initiative != "cfipros" || seed.Status != "enabled" || seed.Timezone != "America/New_York" {
		t.Fatalf("seed output = %+v, want CFIPros CEO defaults", seed)
	}
	if len(seed.Triggers) != len(cfiprosCEORoutines) {
		t.Fatalf("seed triggers = %d, want %d", len(seed.Triggers), len(cfiprosCEORoutines))
	}

	rulesByKey := map[string]string{}
	for _, trigger := range seed.Triggers {
		rulesByKey[trigger.Key] = trigger.RuleJSON
		if trigger.Status != "enabled" || trigger.Kind != "schedule" || trigger.NextEligibleAt == nil {
			t.Fatalf("seed trigger = %+v, want enabled schedule with next run", trigger)
		}
		var rule struct {
			ExecutionIntent string `json:"execution_intent"`
			QuietTimezone   string `json:"quiet_timezone"`
			SkillInvocation struct {
				SkillKey              string `json:"skill_key"`
				ProjectKey            string `json:"project_key"`
				ExecutionIntent       string `json:"execution_intent"`
				ExecutionIntentSource string `json:"execution_intent_source"`
				ReviewState           string `json:"review_state"`
				InputJSON             struct {
					AgentKey         string `json:"agent_key"`
					WorkflowKey      string `json:"workflow_key"`
					ApprovalBoundary string `json:"approval_boundary"`
				} `json:"input_json"`
			} `json:"skill_invocation"`
		}
		if err := json.Unmarshal([]byte(trigger.RuleJSON), &rule); err != nil {
			t.Fatalf("json.Unmarshal(rule_json) error = %v\n%s", err, trigger.RuleJSON)
		}
		if rule.ExecutionIntent != "read_only" || rule.QuietTimezone != "UTC" || rule.SkillInvocation.SkillKey != "cfipros-ceo-operator" || rule.SkillInvocation.ProjectKey != "cfipros" || rule.SkillInvocation.ExecutionIntent != "read_only" || rule.SkillInvocation.ExecutionIntentSource != "trigger" || rule.SkillInvocation.ReviewState != "review_required" {
			t.Fatalf("rule = %+v, want read-only review-required CFIPros CEO skill binding", rule)
		}
		if rule.SkillInvocation.InputJSON.AgentKey != "cfipros-ceo-operator-agent" || rule.SkillInvocation.InputJSON.WorkflowKey != "cfipros-ceo-operating-routine" {
			t.Fatalf("rule input = %+v, want CEO agent and workflow handoff", rule.SkillInvocation.InputJSON)
		}
		if !strings.Contains(rule.SkillInvocation.InputJSON.ApprovalBoundary, "customer contact") || !strings.Contains(rule.SkillInvocation.InputJSON.ApprovalBoundary, "billing") {
			t.Fatalf("approval boundary = %q, want customer and billing limits", rule.SkillInvocation.InputJSON.ApprovalBoundary)
		}
	}
	if got, ok := rulesByKey["cfipros-ceo-morning-launch-health"]; !ok || !strings.Contains(got, `"agent_key":"cfipros-ceo-operator-agent"`) {
		t.Fatalf("morning launch rule = %s, want CEO agent handoff", got)
	}

	fireOutput := run("fire", "cfipros-ceo-morning-launch-health", "reason=seed-proof", "--json")
	for _, want := range []string{
		`"work_kind": "skill_invocation"`,
		`"execution_intent": "read_only"`,
		`"execution_intent_source": "skill_binding:trigger"`,
		`"created_work_item": true`,
	} {
		if !strings.Contains(fireOutput, want) {
			t.Fatalf("fire output = %s, want %s", fireOutput, want)
		}
	}
}

func countCommandTasks(t *testing.T, ctx context.Context, store *sqlite.Store) int {
	t.Helper()
	row := store.DB().QueryRowContext(ctx, `SELECT COUNT(1) FROM tasks`)
	var count int
	if err := row.Scan(&count); err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	return count
}

func countCommandAutomationTriggerMaterializations(t *testing.T, ctx context.Context, store *sqlite.Store) int {
	t.Helper()
	row := store.DB().QueryRowContext(ctx, `SELECT COUNT(1) FROM automation_trigger_materializations`)
	var count int
	if err := row.Scan(&count); err != nil {
		t.Fatalf("count automation trigger materializations: %v", err)
	}
	return count
}
