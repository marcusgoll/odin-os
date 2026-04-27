# Social Carry-Forward Guidance Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a prompt-only `Next-Week Carry-Forward` block to the Marcus analytics retrospective path so Odin turns recent social signal into explicit next-week guidance.

**Architecture:** Extend the existing retrospective/comparison prompt assembly in `internal/cli/repl/shell.go`. Reuse the current recurring and new-signal outputs, then append one compact carry-forward block with `Keep`, `Avoid`, `Test Next`, `X Direction`, and `LinkedIn Direction`, without adding a new CLI command or memory type.

**Tech Stack:** Go, existing CLI shell prompt composition, SQLite-backed knowledge memory, Go test, Make build

---

### Task 1: Write failing tests for the carry-forward block

**Files:**
- Modify: `internal/cli/repl/shell_test.go`
- Read: `internal/cli/repl/shell.go`

**Step 1: Add a populated-history failing test**

Add a test beside the existing multi-week comparison tests that seeds:

- recurring approved `linkedin/post`
- recurring rejected `x/reply`
- recurring `social_learning` for X
- recurring `social_research` for LinkedIn
- a current-week-only LinkedIn signal

Then select:

- `/workflow use marcus-social-growth-workflow`
- `/skill use marcus-social-analytics-advisor`

Ask for the retrospective and assert the output contains:

- `Next-Week Carry-Forward`
- `Keep:`
- `- linkedin/post approved`
- `Avoid:`
- `- x/reply rejected`
- `Test Next:`
- the recurring X learning
- the recurring LinkedIn research signal
- the current-week-only LinkedIn signal
- `X Direction:`
- baseline X wording such as `inner thoughts`
- `LinkedIn Direction:`
- baseline LinkedIn wording such as `professionally framed`

**Step 2: Add a sparse-history failing test**

Seed only one current-week X learning and verify:

- the carry-forward block still appears
- `Keep:` and `Avoid:` show `- none`
- `Test Next:` shows the current-week X learning
- both platform direction sections still appear

**Step 3: Run the new carry-forward tests and confirm failure**

Run:

```bash
go test ./internal/cli/repl -run 'TestAskModeAnalyticsSkill(ProvidesCarryForwardGuidance|CarryForwardGuidanceDegradesGracefully)$' -count=1
```

Expected:

- FAIL because the prompt does not yet include the carry-forward block

### Task 2: Implement minimal carry-forward prompt assembly

**Files:**
- Modify: `internal/cli/repl/shell.go`
- Read: `internal/cli/repl/shell_test.go`

**Step 1: Reuse the current comparison outputs**

Keep the existing retrospective and comparison blocks intact.

Implement a helper such as:

```go
func summarizeSocialCarryForward(recurringApproved, recurringRejected, recurringLearnings, recurringResearch, newThisWeek []string) string
```

This helper should derive:

- `Keep` from recurring approvals
- `Avoid` from recurring rejections
- `Test Next` from recurring learnings, recurring research, and new-this-week signals

**Step 2: Add X and LinkedIn direction helpers**

Add simple helpers that:

- always emit baseline X direction text
- always emit baseline LinkedIn direction text
- append any matching X-specific or LinkedIn-specific carry-forward signals based on the existing label format

Use existing line prefixes such as:

- `- [x] ...`
- `- [linkedin] ...`
- `- x/...`
- `- linkedin/...`

Keep this first slice string-based and defensive.

**Step 3: Append the carry-forward block after the comparison block**

Update the retrospective context assembly so the final prompt becomes:

- weekly retrospective
- multi-week comparison
- next-week carry-forward

Do not change the operator flow or add any persistence.

**Step 4: Run the focused carry-forward tests and make them pass**

Run:

```bash
go test ./internal/cli/repl -run 'TestAskModeAnalyticsSkill(ProvidesCarryForwardGuidance|CarryForwardGuidanceDegradesGracefully)$' -count=1
```

Expected:

- PASS

### Task 3: Verify no regression in the broader retrospective path

**Files:**
- Modify: `internal/cli/repl/shell_test.go` only if regressions require narrow fixes

**Step 1: Run the broader retrospective shell tests**

Run:

```bash
go test ./internal/cli/repl -run 'TestAskModeAnalyticsSkill|TestMemory' -count=1
```

Expected:

- PASS
- existing weekly retrospective behavior still holds
- existing 4-window comparison still holds
- non-analytics prompts still avoid retrospective injection

**Step 2: Keep fixes narrow**

Do not refactor unrelated prompt behavior.

### Task 4: Update live docs to match the carry-forward behavior

**Files:**
- Modify: `docs/contracts/marcus-social-copilot.md`
- Modify: `memory/users/marcus-social-copilot.md`

**Step 1: Mark carry-forward guidance as live**

Update the roadmap so Phase 1 includes prompt-only carry-forward guidance from the analytics advisor.

Adjust Phase 2 so it now refers to later persistence or longer-horizon interpretation, not this slice.

**Step 2: Update examples**

Add or refine examples so the live prompts mention:

- next-week carry-forward guidance
- X staying closer to Marcus’s inner thoughts
- LinkedIn staying more professionally framed

**Step 3: Keep docs honest**

Do not imply persistence or automatic plan handoff if the implementation is prompt-only.

### Task 5: Run full verification and prove the real odin CLI path

**Files:**
- Read: `internal/cli/repl/shell.go`
- Read: `docs/contracts/marcus-social-copilot.md`

**Step 1: Run targeted package verification**

Run:

```bash
go test ./internal/cli/repl ./internal/cli/commands ./internal/memory/knowledge -count=1
make build
```

Expected:

- PASS
- `bin/odin` rebuilt successfully

**Step 2: Prove populated-history carry-forward output through the real binary**

Run a real shell session that:

- selects `marcus-social-growth-workflow`
- records current-week and prior-week social memory
- selects `marcus-social-analytics-advisor`
- asks for the retrospective

Assert the output contains:

- `Next-Week Carry-Forward`
- `Keep:`
- `Avoid:`
- `Test Next:`
- `X Direction:`
- `LinkedIn Direction:`

**Step 3: Prove sparse-history behavior through the real binary**

Run a real shell session with only one current-week learning and verify:

- the carry-forward block still appears
- empty sections degrade to `- none`
- platform direction sections still appear

**Step 4: Report completion without overclaiming**

The final report must explicitly include:

- Existing state found
- Reused components
- New components added
- Why new components were necessary
- Real `odin` command E2E checks performed
