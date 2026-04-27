# Social Manual Publish Evidence Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a narrow `/memory publish` flow so Marcus can mark approved social outcomes as manually published with real evidence inside `odin-os`.

**Architecture:** Reuse the existing `/memory` command and `social_outcome` memory type. Update an approved `social_outcome` in place with publish evidence fields instead of introducing a separate publish subsystem or automated posting path.

**Tech Stack:** Go, interactive CLI shell, SQLite store, compiled `odin` integration tests.

---

### Task 1: Add failing tests for publish evidence capture

**Files:**
- Modify: `internal/cli/repl/shell_test.go`
- Modify: `tests/integration/social_workflow_test.go`

**Step 1: Write the failing shell tests**

Add tests that:
- publish an approved `social_outcome`
- confirm `publish_status=published`, `publish_url`, and `published_at`
- reject publishing a rejected outcome or an already published one

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/repl -run 'TestShellMemoryPublish'`
Expected: FAIL because `/memory publish` does not exist yet.

**Step 3: Write the failing integration test**

Add a compiled-binary test that:
- drafts through the Marcus workflow
- resolves approval
- marks the approved outcome as published
- verifies `/memory list type=social_outcome field.publish_status=published`

**Step 4: Run test to verify it fails**

Run: `go test ./tests/integration -run TestMarcusSocialPublishEvidenceCLIIntegration`
Expected: FAIL because `/memory publish` does not exist yet.

### Task 2: Implement the narrow `/memory publish` flow

**Files:**
- Modify: `internal/cli/commands/help.go`
- Modify: `internal/cli/repl/shell.go`

**Step 1: Add parser and validation**

Implement:
- `/memory publish <id> url=<value> [published_at=<rfc3339>]`

Rules:
- target must be a visible `social_outcome`
- target must have `result=approved`
- target must not already have `publish_status=published`
- publishing updates the outcome with `publish_status=published`, `publish_url`, and `published_at`

**Step 2: Run targeted shell tests**

Run: `go test ./internal/cli/repl -run 'TestShellMemoryPublish'`
Expected: PASS

### Task 3: Update docs and verify the real workflow

**Files:**
- Modify: `docs/contracts/marcus-social-copilot.md`

**Step 1: Update contract wording**

Reflect that manual publish evidence capture is now live through `/memory publish`, while actual posting still remains manual or official-interface only.

**Step 2: Run focused verification**

Run:
- `go test ./internal/cli/repl ./internal/cli/commands`
- `go test ./tests/integration -run 'TestMarcusSocial(PublishEvidenceCLIIntegration|DraftResolveCLIIntegration|AnalyticsRetrospectiveCLIIntegration)'`
- `go build -o ./bin/odin ./cmd/odin`

**Step 3: Run real odin E2E flow**

Run a real interactive session that:
- drafts one Marcus X post
- resolves it to an approved outcome
- marks the approved outcome published with a real URL
- confirms `/memory list type=social_outcome field.publish_status=published`
