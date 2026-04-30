package commands

import (
	"testing"
)

func TestParseKnowledgeIngestCommand(t *testing.T) {
	t.Parallel()

	cmd, err := ParseKnowledge([]string{
		"ingest",
		"/tmp/manual.txt",
		"--key", "pilot-manual",
		"--title", "Pilot Manual",
		"--scope", "global",
		"--scope-key", "global",
		"--kind", "manual",
		"--restricted",
		"--source-class", "text",
	})
	if err != nil {
		t.Fatalf("ParseKnowledge() error = %v", err)
	}

	if cmd.Action != "ingest" {
		t.Fatalf("Action = %q, want ingest", cmd.Action)
	}
	if cmd.Path != "/tmp/manual.txt" {
		t.Fatalf("Path = %q, want source path", cmd.Path)
	}
	if cmd.Key != "pilot-manual" || cmd.Title != "Pilot Manual" {
		t.Fatalf("key/title = %q/%q, want pilot-manual/Pilot Manual", cmd.Key, cmd.Title)
	}
	if cmd.Scope != "global" || cmd.ScopeKey != "global" || cmd.SourceKind != "manual" {
		t.Fatalf("scope/scope_key/kind = %q/%q/%q, want global/global/manual", cmd.Scope, cmd.ScopeKey, cmd.SourceKind)
	}
	if !cmd.Restricted {
		t.Fatalf("Restricted = false, want true")
	}
	if cmd.SourceClass != "text" {
		t.Fatalf("SourceClass = %q, want text", cmd.SourceClass)
	}
}

func TestParseKnowledgeSearchCommand(t *testing.T) {
	t.Parallel()

	cmd, err := ParseKnowledge([]string{"search", "vacation accrual", "--scope", "global", "--scope-key", "global", "--limit", "7"})
	if err != nil {
		t.Fatalf("ParseKnowledge() error = %v", err)
	}

	if cmd.Action != "search" {
		t.Fatalf("Action = %q, want search", cmd.Action)
	}
	if cmd.Query != "vacation accrual" {
		t.Fatalf("Query = %q, want vacation accrual", cmd.Query)
	}
	if cmd.Scope != "global" || cmd.ScopeKey != "global" {
		t.Fatalf("scope/scope_key = %q/%q, want global/global", cmd.Scope, cmd.ScopeKey)
	}
	if cmd.Limit != 7 {
		t.Fatalf("Limit = %d, want 7", cmd.Limit)
	}
}

func TestParseKnowledgeApproveUseCommand(t *testing.T) {
	t.Parallel()

	cmd, err := ParseKnowledge([]string{
		"approve-use",
		"pilot-contract",
		"--use-type", "executor_context_injection",
		"--reason", "Need narrow cited context for current task",
		"--decided-by", "marcus",
		"--decision", "approved",
		"--evidence-json", `{"task":"task-5"}`,
	})
	if err != nil {
		t.Fatalf("ParseKnowledge() error = %v", err)
	}

	if cmd.Action != "approve-use" {
		t.Fatalf("Action = %q, want approve-use", cmd.Action)
	}
	if cmd.Key != "pilot-contract" {
		t.Fatalf("Key = %q, want pilot-contract", cmd.Key)
	}
	if cmd.UseType != "executor_context_injection" {
		t.Fatalf("UseType = %q, want executor_context_injection", cmd.UseType)
	}
	if cmd.Reason != "Need narrow cited context for current task" || cmd.DecidedBy != "marcus" || cmd.Decision != "approved" {
		t.Fatalf("reason/decided_by/decision = %q/%q/%q", cmd.Reason, cmd.DecidedBy, cmd.Decision)
	}
	if cmd.EvidenceJSON != `{"task":"task-5"}` {
		t.Fatalf("EvidenceJSON = %q, want explicit JSON", cmd.EvidenceJSON)
	}
}
