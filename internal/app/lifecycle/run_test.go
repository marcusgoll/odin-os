package lifecycle

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunStartsInteractiveShell(t *testing.T) {
	t.Parallel()

	root := newLifecycleTestRoot(t)

	stdin := strings.NewReader("/help\n")
	var stdout bytes.Buffer

	err := Run(context.Background(), root, nil, stdin, &stdout)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	output := stdout.String()
	if !strings.Contains(output, "scope=") {
		t.Fatalf("Run() output = %q, want header", output)
	}
	if !strings.Contains(output, "/help") {
		t.Fatalf("Run() output = %q, want help", output)
	}
}

func TestRunWorkStatusShowsDeliveryWorkflowState(t *testing.T) {
	t.Parallel()

	root := newLifecycleTestRoot(t)

	var stdout bytes.Buffer
	err := Run(context.Background(), root, []string{"work", "status"}, strings.NewReader(""), &stdout)
	if err != nil {
		t.Fatalf("Run(work status) error = %v", err)
	}

	output := stdout.String()
	for _, want := range []string{
		"work_items=0",
		"open_work_items=0",
		"active_run_attempts=0",
		"pending_approvals=0",
		"delivery_profiles=0",
		"dispatch=not_implemented",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("Run(work status) output = %q, want %q", output, want)
		}
	}
}

func TestRunWorkProfilesListsDeliveryProfileWorkflows(t *testing.T) {
	t.Parallel()

	root := newLifecycleTestRoot(t)
	if err := os.MkdirAll(filepath.Join(root, "registry", "workflows"), 0o755); err != nil {
		t.Fatalf("mkdir workflows: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "registry", "workflows", "delivery-small.md"), []byte(`---
kind: workflow
key: delivery-small
title: Small Delivery Profile
summary: Routes low-risk Work Items through a compact delivery loop.
status: active
tags:
  - delivery_profile
owners:
  - odin-core
entrypoint: command:odin work start --profile delivery-small
composes:
  - triage-skill
---

# Small Delivery Profile

## Purpose
Route low-risk Work Items through a compact delivery loop.

## When to Use
Use when the work is low-risk and scoped.

## Inputs
Work Item identity and acceptance criteria.

## Procedure
Plan, execute, verify, and hand off.

## Outputs
Verified Work Item evidence.

## Constraints
Do not skip verification or human handoff.

## Success Criteria
The Work Item reaches verified handoff evidence.
`), 0o644); err != nil {
		t.Fatalf("write delivery profile: %v", err)
	}

	var stdout bytes.Buffer
	err := Run(context.Background(), root, []string{"work", "profiles"}, strings.NewReader(""), &stdout)
	if err != nil {
		t.Fatalf("Run(work profiles) error = %v", err)
	}

	output := stdout.String()
	for _, want := range []string{
		"delivery-small",
		"status=active",
		"entrypoint=command:odin work start --profile delivery-small",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("Run(work profiles) output = %q, want %q", output, want)
		}
	}
}

func TestRunWorkStartCreatesQueuedWorkItem(t *testing.T) {
	t.Parallel()

	root := newLifecycleTestRoot(t)

	var startOutput bytes.Buffer
	err := Run(context.Background(), root, []string{"work", "start", "--project", "odin-core", "--title", "Implement delivery surface"}, strings.NewReader(""), &startOutput)
	if err != nil {
		t.Fatalf("Run(work start) error = %v", err)
	}

	for _, want := range []string{
		"work_item_id=",
		"project=odin-core",
		"status=queued",
	} {
		if !strings.Contains(startOutput.String(), want) {
			t.Fatalf("Run(work start) output = %q, want %q", startOutput.String(), want)
		}
	}

	var statusOutput bytes.Buffer
	err = Run(context.Background(), root, []string{"work", "status"}, strings.NewReader(""), &statusOutput)
	if err != nil {
		t.Fatalf("Run(work status) error = %v", err)
	}
	for _, want := range []string{
		"work_items=1",
		"open_work_items=1",
	} {
		if !strings.Contains(statusOutput.String(), want) {
			t.Fatalf("Run(work status) output = %q, want %q", statusOutput.String(), want)
		}
	}
}

func TestRunKnowledgeIngestListSearchAndApproveUse(t *testing.T) {
	t.Parallel()

	root := newLifecycleTestRoot(t)
	sourcePath := filepath.Join(root, "pilot-contract.txt")
	if err := os.WriteFile(sourcePath, []byte("Vacation accrual rules require narrow cited snippets for executor use.\n"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	var ingestOutput bytes.Buffer
	err := Run(context.Background(), root, []string{
		"knowledge",
		"ingest",
		sourcePath,
		"--key", "pilot-contract",
		"--title", "Pilot Contract",
		"--kind", "pilot_contract",
	}, strings.NewReader(""), &ingestOutput)
	if err != nil {
		t.Fatalf("Run(knowledge ingest) error = %v", err)
	}
	for _, want := range []string{
		"source=pilot-contract",
		"lifecycle=ready",
		"restricted=true",
		"artifact_sha256=sha256:",
		"extractor=plain_text:v1",
		"manifest=memory/knowledge/pilot-contract.md",
	} {
		if !strings.Contains(ingestOutput.String(), want) {
			t.Fatalf("ingest output = %q, want %q", ingestOutput.String(), want)
		}
	}

	var listOutput bytes.Buffer
	err = Run(context.Background(), root, []string{"knowledge", "list", "--scope", "global", "--scope-key", "global", "--lifecycle", "ready", "--restricted", "true"}, strings.NewReader(""), &listOutput)
	if err != nil {
		t.Fatalf("Run(knowledge list) error = %v", err)
	}
	for _, want := range []string{
		"source=pilot-contract",
		"title=Pilot Contract",
		"lifecycle=ready",
		"restricted=true",
		"class=text",
		"manifest=memory/knowledge/pilot-contract.md",
	} {
		if !strings.Contains(listOutput.String(), want) {
			t.Fatalf("list output = %q, want %q", listOutput.String(), want)
		}
	}

	var showOutput bytes.Buffer
	err = Run(context.Background(), root, []string{"knowledge", "show", "pilot-contract"}, strings.NewReader(""), &showOutput)
	if err != nil {
		t.Fatalf("Run(knowledge show) error = %v", err)
	}
	for _, want := range []string{
		"source=pilot-contract",
		"title=Pilot Contract",
		"lifecycle=ready",
		"restricted=true",
		"class=text",
		"manifest=memory/knowledge/pilot-contract.md",
	} {
		if !strings.Contains(showOutput.String(), want) {
			t.Fatalf("show output = %q, want %q", showOutput.String(), want)
		}
	}

	var searchOutput bytes.Buffer
	err = Run(context.Background(), root, []string{"knowledge", "search", "Vacation", "--limit", "3"}, strings.NewReader(""), &searchOutput)
	if err != nil {
		t.Fatalf("Run(knowledge search) error = %v", err)
	}
	for _, want := range []string{
		"source=pilot-contract",
		"title=Pilot Contract",
		"chunk_id=",
		"restricted=true",
		"anchor=",
		"snippet=",
	} {
		if !strings.Contains(searchOutput.String(), want) {
			t.Fatalf("search output = %q, want %q", searchOutput.String(), want)
		}
	}

	var refreshOutput bytes.Buffer
	err = Run(context.Background(), root, []string{"knowledge", "refresh", "pilot-contract"}, strings.NewReader(""), &refreshOutput)
	if err != nil {
		t.Fatalf("Run(knowledge refresh) error = %v", err)
	}
	for _, want := range []string{
		"source=pilot-contract",
		"lifecycle=ready",
		"restricted=true",
		"artifact_sha256=sha256:",
		"extractor=plain_text:v1",
		"manifest=memory/knowledge/pilot-contract.md",
	} {
		if !strings.Contains(refreshOutput.String(), want) {
			t.Fatalf("refresh output = %q, want %q", refreshOutput.String(), want)
		}
	}

	var approvalOutput bytes.Buffer
	err = Run(context.Background(), root, []string{
		"knowledge",
		"approve-use",
		"pilot-contract",
		"--use-type", "executor_context_injection",
		"--reason", "Need narrow cited context for current task",
		"--decided-by", "marcus",
	}, strings.NewReader(""), &approvalOutput)
	if err != nil {
		t.Fatalf("Run(knowledge approve-use) error = %v", err)
	}
	for _, want := range []string{
		"approval_id=",
		"source=pilot-contract",
		"use_type=executor_context_injection",
		"decision=approved",
	} {
		if !strings.Contains(approvalOutput.String(), want) {
			t.Fatalf("approval output = %q, want %q", approvalOutput.String(), want)
		}
	}
}

func TestRunKnowledgeInboxPathListAndIngestInbox(t *testing.T) {
	t.Parallel()

	root := newLifecycleTestRoot(t)

	var pathOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"knowledge", "inbox-path"}, strings.NewReader(""), &pathOutput); err != nil {
		t.Fatalf("Run(knowledge inbox-path) error = %v", err)
	}
	inboxPath := strings.TrimSpace(pathOutput.String())
	if inboxPath != filepath.Join(root, "knowledge", "inbox") {
		t.Fatalf("inbox path output = %q, want runtime knowledge inbox", pathOutput.String())
	}
	if err := os.WriteFile(filepath.Join(inboxPath, "pilot-contract.txt"), []byte("Vacation accrual rules copied by scp.\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(inbox source) error = %v", err)
	}

	var listOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{"knowledge", "inbox"}, strings.NewReader(""), &listOutput); err != nil {
		t.Fatalf("Run(knowledge inbox) error = %v", err)
	}
	for _, want := range []string{
		"name=pilot-contract.txt",
		"class=text",
		"supported=true",
	} {
		if !strings.Contains(listOutput.String(), want) {
			t.Fatalf("inbox output = %q, want %q", listOutput.String(), want)
		}
	}

	var ingestOutput bytes.Buffer
	if err := Run(context.Background(), root, []string{
		"knowledge",
		"ingest-inbox",
		"pilot-contract.txt",
		"--kind", "pilot_contract",
		"--restricted",
	}, strings.NewReader(""), &ingestOutput); err != nil {
		t.Fatalf("Run(knowledge ingest-inbox) error = %v", err)
	}
	for _, want := range []string{
		"source=pilot-contract",
		"lifecycle=ready",
		"restricted=true",
		"manifest=memory/knowledge/pilot-contract.md",
	} {
		if !strings.Contains(ingestOutput.String(), want) {
			t.Fatalf("ingest-inbox output = %q, want %q", ingestOutput.String(), want)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "knowledge", "inbox", "pilot-contract.txt")); !os.IsNotExist(err) {
		t.Fatalf("inbox file stat error = %v, want moved out of inbox", err)
	}
	if _, err := os.Stat(filepath.Join(root, "knowledge", "imported", "pilot-contract.txt")); err != nil {
		t.Fatalf("imported file missing: %v", err)
	}
}

func TestRunKnowledgeApproveUseRejectsUnrestrictedSource(t *testing.T) {
	t.Parallel()

	root := newLifecycleTestRoot(t)
	sourcePath := filepath.Join(root, "note.txt")
	if err := os.WriteFile(sourcePath, []byte("Public note for ordinary retrieval.\n"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	var ingestOutput bytes.Buffer
	err := Run(context.Background(), root, []string{
		"knowledge",
		"ingest",
		sourcePath,
		"--key", "public-note",
		"--title", "Public Note",
		"--kind", "note",
	}, strings.NewReader(""), &ingestOutput)
	if err != nil {
		t.Fatalf("Run(knowledge ingest) error = %v", err)
	}
	if !strings.Contains(ingestOutput.String(), "restricted=false") {
		t.Fatalf("ingest output = %q, want unrestricted source", ingestOutput.String())
	}

	var approvalOutput bytes.Buffer
	err = Run(context.Background(), root, []string{
		"knowledge",
		"approve-use",
		"public-note",
		"--use-type", "executor_context_injection",
		"--reason", "Should be rejected for unrestricted sources",
	}, strings.NewReader(""), &approvalOutput)
	if err == nil || !strings.Contains(err.Error(), "restricted knowledge source") {
		t.Fatalf("Run(knowledge approve-use) error = %v, want restricted source failure", err)
	}
}

func newLifecycleTestRoot(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "config"), 0o755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "data"), 0o755); err != nil {
		t.Fatalf("mkdir data: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "registry"), 0o755); err != nil {
		t.Fatalf("mkdir registry: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "state", "cache"), 0o755); err != nil {
		t.Fatalf("mkdir state: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir git root: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "config", "projects.yaml"), []byte(`
version: 1
projects:
  - key: odin-core
    name: Odin Core
    project_class: system_project
    system_project: true
    git_root: ..
    default_branch: main
    policy:
      allowed_commands: [status]
      branch_rules:
        protected_branches: [main]
        require_worktree: true
        require_task_branch: true
        allow_default_branch_mutation: false
      approval_gates:
        require_for_governance_changes: true
        require_for_destructive_operations: true
        require_for_system_project_changes: true
      merge_policy:
        mode: squash
        allow_direct_to_default_branch: false
      destructive_operations:
        allow_reset: false
        allow_clean: false
        allow_force_push: false
        require_explicit_approval: true
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "config", "executors.yaml"), []byte(`
version: 1
executors:
  - key: codex_headless
    adapter: codex_headless
    class: plan_backed_cli
    enabled: true
    priority: 10
routes:
  - name: default
    match:
      task_kinds: [general, plan, build, review, qa, research]
      scopes: [global, odin-core, project, new-project]
    preferred: [codex_headless]
`), 0o644); err != nil {
		t.Fatalf("write executors config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "config", "odin.yaml"), []byte(`
version: 1
runtime:
  root: .
service:
  http_addr: 127.0.0.1:9443
  startup_recovery: true
`), 0o644); err != nil {
		t.Fatalf("write odin config: %v", err)
	}
	return root
}
