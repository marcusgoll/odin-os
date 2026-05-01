package commands

import (
	"context"
	"strings"
	"testing"

	"odin-os/internal/registry"
	"odin-os/internal/store/sqlite"
)

func TestRunWorkSuperviseE2ERequiresJSON(t *testing.T) {
	ctx := context.Background()
	store := openWorkCommandStore(t)
	defer store.Close()

	err, output := runWorkSuperviseE2EForError(t, ctx, store, []string{"supervise", "e2e", "prepare-issue", "--project", "alpha"})
	if err == nil {
		t.Fatalf("RunWork(supervise e2e prepare-issue without --json) error = nil, want required JSON error\noutput:\n%s", output)
	}
	if !strings.Contains(err.Error(), "--json is required for work supervise in this slice") {
		t.Fatalf("error = %q, want required JSON error", err.Error())
	}
	assertNoSuperviseSideEffects(t, ctx, store)
}

func TestRunWorkSuperviseE2EUsageShowsPrepareIssueJSON(t *testing.T) {
	ctx := context.Background()
	store := openWorkCommandStore(t)
	defer store.Close()

	var output strings.Builder
	if err := RunWork(ctx, store, commandProjectRegistry(t), registry.Snapshot{}, []string{"supervise", "e2e", "prepare-issue"}, &output); err == nil {
		t.Fatalf("RunWork(supervise e2e prepare-issue usage probe) error = nil, want required JSON error\noutput:\n%s", output.String())
	} else if !strings.Contains(err.Error(), "e2e prepare-issue --project <key> --json") {
		t.Fatalf("error = %q, want prepare-issue usage to include --json", err.Error())
	}

	var workOutput strings.Builder
	if err := RunWork(ctx, store, commandProjectRegistry(t), registry.Snapshot{}, []string{"help"}, &workOutput); err != nil {
		t.Fatalf("RunWork(help) error = %v", err)
	}
	if !strings.Contains(workOutput.String(), "e2e prepare-issue --project <key> --json") {
		t.Fatalf("work usage = %q, want prepare-issue usage to include --json", workOutput.String())
	}
}

func TestRunWorkSuperviseE2EPrepareIssueRequiresProject(t *testing.T) {
	ctx := context.Background()
	store := openWorkCommandStore(t)
	defer store.Close()

	err, output := runWorkSuperviseE2EForError(t, ctx, store, []string{"supervise", "e2e", "prepare-issue", "--json"})
	if err == nil {
		t.Fatalf("RunWork(supervise e2e prepare-issue without --project) error = nil, want project error\noutput:\n%s", output)
	}
	if !strings.Contains(err.Error(), "missing --project for work supervise e2e prepare-issue") {
		t.Fatalf("error = %q, want missing project error", err.Error())
	}

	err, output = runWorkSuperviseE2EForError(t, ctx, store, []string{"supervise", "e2e", "prepare-issue", "--project", "alpha", "--json"})
	if err == nil {
		t.Fatalf("RunWork(supervise e2e prepare-issue) error = nil, want not_implemented\noutput:\n%s", output)
	}
	if !strings.Contains(err.Error(), "not_implemented: work supervise e2e prepare-issue") {
		t.Fatalf("error = %q, want not_implemented after validation", err.Error())
	}
	assertNoSuperviseSideEffects(t, ctx, store)
}

func TestRunWorkSuperviseE2ERunOnceRequiresExplicitIssue(t *testing.T) {
	ctx := context.Background()
	store := openWorkCommandStore(t)
	defer store.Close()

	err, output := runWorkSuperviseE2EForError(t, ctx, store, []string{"supervise", "e2e", "run-once", "--project", "alpha", "--json"})
	if err == nil {
		t.Fatalf("RunWork(supervise e2e run-once without --issue) error = nil, want issue error\noutput:\n%s", output)
	}
	if !strings.Contains(err.Error(), "missing --issue for work supervise e2e run-once") {
		t.Fatalf("error = %q, want missing issue error", err.Error())
	}

	err, output = runWorkSuperviseE2EForError(t, ctx, store, []string{"supervise", "e2e", "run-once", "--project", "alpha", "--issue", "42", "--json"})
	if err == nil {
		t.Fatalf("RunWork(supervise e2e run-once) error = nil, want not_implemented\noutput:\n%s", output)
	}
	if !strings.Contains(err.Error(), "not_implemented: work supervise e2e run-once") {
		t.Fatalf("error = %q, want not_implemented after validation", err.Error())
	}
	assertNoSuperviseSideEffects(t, ctx, store)
}

func runWorkSuperviseE2EForError(t *testing.T, ctx context.Context, store *sqlite.Store, args []string) (error, string) {
	t.Helper()

	var output strings.Builder
	err := RunWork(ctx, store, commandProjectRegistry(t), registry.Snapshot{}, args, &output)
	return err, output.String()
}
