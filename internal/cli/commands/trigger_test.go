package commands

import (
	"bytes"
	"context"
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
