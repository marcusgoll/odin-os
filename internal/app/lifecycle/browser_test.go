package lifecycle

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"odin-os/internal/store/sqlite"
)

func TestRunBrowserRunRecordsGoalEvidenceAndKeepsGoalStatus(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := testRepoRoot(t)

	run := func(args ...string) string {
		t.Helper()
		var output bytes.Buffer
		if err := Run(context.Background(), root, args, strings.NewReader(""), &output); err != nil {
			t.Fatalf("Run(%v) error = %v\noutput=%s", args, err, output.String())
		}
		return output.String()
	}

	created := decodeGoalEnvelope(t, []byte(run("goal", "create", "--title", "Collect browser evidence", "--json")))
	browserRun := run("browser", "run", "--goal-id", int64String(created.ID), "--url", "https://example.com/research", "--objective", "Collect public documentation", "--allowed-domain", "example.com", "--max-pages", "2", "--max-duration-seconds", "30", "--evidence-required", "--json")
	for _, want := range []string{
		`"status": "recorded"`,
		`"goal_id": 1`,
		`"evidence_id": 1`,
		`"adapter_kind": "stub_local"`,
		`"page_results":`,
		`"status": "visited"`,
		`"no_live_browser_launched"`,
	} {
		if !strings.Contains(browserRun, want) {
			t.Fatalf("browser run output = %s, want %s", browserRun, want)
		}
	}

	shown := decodeGoalEnvelope(t, []byte(run("goal", "show", "--id", int64String(created.ID), "--json")))
	if shown.Status != string(sqlite.GoalStatusCreated) {
		t.Fatalf("goal status = %q, want unchanged created", shown.Status)
	}

	logs := run("logs", "--json")
	if !strings.Contains(logs, `"type": "goal.evidence_recorded"`) || !strings.Contains(logs, `"evidence_type": "browser_readonly"`) {
		t.Fatalf("logs output = %s, want browser evidence audit event", logs)
	}
}

func TestRunBrowserRunRejectsUnsafeInputs(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := testRepoRoot(t)

	var createOut bytes.Buffer
	if err := Run(context.Background(), root, []string{"goal", "create", "--title", "Collect browser evidence", "--json"}, strings.NewReader(""), &createOut); err != nil {
		t.Fatalf("Run(goal create) error = %v", err)
	}
	created := decodeGoalEnvelope(t, createOut.Bytes())

	var domainOut bytes.Buffer
	err := Run(context.Background(), root, []string{"browser", "run", "--goal-id", int64String(created.ID), "--url", "https://not-example.test/research", "--objective", "Collect public documentation", "--allowed-domain", "example.com", "--json"}, strings.NewReader(""), &domainOut)
	if err == nil || !strings.Contains(err.Error(), "disallowed domain") {
		t.Fatalf("Run(browser disallowed domain) error = %v output=%s, want disallowed domain", err, domainOut.String())
	}

	var actionOut bytes.Buffer
	err = Run(context.Background(), root, []string{"browser", "run", "--goal-id", int64String(created.ID), "--url", "https://example.com/research", "--objective", "Collect public documentation", "--action", "submit_form", "--json"}, strings.NewReader(""), &actionOut)
	if err == nil || !strings.Contains(err.Error(), "mutation action") {
		t.Fatalf("Run(browser mutation action) error = %v output=%s, want mutation action rejection", err, actionOut.String())
	}
}
