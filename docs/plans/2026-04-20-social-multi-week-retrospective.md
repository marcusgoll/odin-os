# Social Multi-Week Retrospective Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a compact multi-week comparison block to the existing Marcus social analytics retrospective prompt using the last 4 rolling weekly windows.

**Architecture:** Extend the existing workflow-aware prompt enrichment path in `internal/cli/repl/shell.go`. Keep the current 7-day retrospective block intact, then append a comparison block derived from the same workflow-scoped social memory records without adding a new CLI command, memory schema, or registry surface.

**Tech Stack:** Go, existing CLI shell prompt composition, SQLite-backed knowledge memory, Go test, Make build

---

### Task 1: Write failing shell tests for multi-week comparison

**Files:**
- Modify: `internal/cli/repl/shell_test.go`
- Read: `internal/cli/repl/shell.go`

**Step 1: Add a failing comparison test with 4 weekly windows**

Add a test near the existing retrospective tests that seeds:

- current-week `social_outcome` approved `linkedin/post`
- prior-week `social_outcome` approved `linkedin/post`
- current-week `social_outcome` rejected `x/reply`
- prior-week `social_outcome` rejected `x/reply`
- current-week `social_learning`
- prior-week matching `social_learning`
- current-week `social_research`
- prior-week matching `social_research`
- a current-week-only signal that should land under `New This Week`

Target shape:

```go
func TestAskModeAnalyticsSkillIncludesMultiWeekComparison(t *testing.T) {
    // seed 4 weekly windows with recordWorkflowMemoryAtTime(...)
    // select workflow + analytics skill
    // ask for retrospective
    // assert output contains:
    // - "Comparison Window: last 4 weekly windows"
    // - 4 week lines
    // - recurring approval/rejection/learning/research sections
    // - "New This Week:"
}
```

**Step 2: Add a sparse-history failing test**

Add a test that seeds only current-week memory and verifies:

- the comparison block still appears
- recurring sections show `- none`
- new current-week signals still appear

Target shape:

```go
func TestAskModeAnalyticsSkillMultiWeekComparisonDegradesGracefully(t *testing.T) {
    // seed only current-week memory
    // assert comparison block appears with sparse-history output
}
```

**Step 3: Run only the new retrospective comparison tests and confirm failure**

Run:

```bash
go test ./internal/cli/repl -run 'TestAskModeAnalyticsSkill(IncludesMultiWeekComparison|MultiWeekComparisonDegradesGracefully)$' -count=1
```

Expected:

- FAIL because the prompt does not yet include the multi-week comparison block

### Task 2: Implement minimal multi-week comparison helpers

**Files:**
- Modify: `internal/cli/repl/shell.go`
- Read: `internal/cli/repl/shell_test.go`

**Step 1: Extend the retrospective assembler to fetch a 28-day comparison horizon**

Inside `socialRetrospectivePromptContext(...)`, keep the current 7-day lookback and also fetch memory for:

- `social_outcome`
- `social_learning`
- `social_research`

across the last 28 days for comparison use.

Prefer a new helper rather than changing the existing 7-day helper signature everywhere.

**Step 2: Add minimal weekly bucketing helpers**

Implement helpers that:

- split summaries into 4 rolling 7-day windows ending at `now`
- keep windows in descending recency order
- count per window:
  - approved outcomes
  - rejected outcomes
  - learnings
  - research

Use the existing `sqlite.MemorySummary.CreatedAt` timestamps.

**Step 3: Add comparison-label extraction**

Implement compact labels:

- outcomes: `channel/content_kind result`
- learnings: existing formatted retrospective line
- research: existing formatted retrospective line

Example:

```go
func socialOutcomePatternLabel(fields map[string]string) string {
    return fmt.Sprintf("%s/%s %s", fields["channel"], fields["content_kind"], fields["result"])
}
```

Normalize defensively so missing fields do not panic.

**Step 4: Append the comparison block to the existing retrospective context**

Add a helper such as:

```go
func summarizeSocialMultiWeekComparison(now time.Time, outcomes, learnings, research []sqlite.MemorySummary) string
```

This helper should append:

- `Comparison Window: last 4 weekly windows`
- one line per week range with counts
- `Recurring Approval Patterns:`
- `Recurring Rejection Patterns:`
- `Recurring Learning Signals:`
- `Recurring Research Signals:`
- `New This Week:`

Use `- none` for empty sections.

**Step 5: Run the new focused tests and make them pass**

Run:

```bash
go test ./internal/cli/repl -run 'TestAskModeAnalyticsSkill(IncludesMultiWeekComparison|MultiWeekComparisonDegradesGracefully)$' -count=1
```

Expected:

- PASS

### Task 3: Protect current behavior with the broader shell test set

**Files:**
- Modify: `internal/cli/repl/shell_test.go` if needed

**Step 1: Run the broader retrospective and memory shell tests**

Run:

```bash
go test ./internal/cli/repl -run 'TestAskModeAnalyticsSkill|TestMemory' -count=1
```

Expected:

- PASS
- existing weekly retrospective behavior still present
- no regression for draft exclusion, old-memory exclusion, or non-analytics prompts

**Step 2: Adjust only if regressions show up**

Keep the implementation narrow. Do not refactor unrelated shell behavior.

### Task 4: Update live docs to match the new runtime behavior

**Files:**
- Modify: `docs/contracts/marcus-social-copilot.md`
- Modify: `memory/users/marcus-social-copilot.md`

**Step 1: Mark the multi-week comparison as live**

Move the roadmap status so the current live phase includes:

- repeatable weekly retrospective prompts
- multi-week comparison across the last 4 rolling weekly windows

**Step 2: Update examples**

Add or refine examples that show Odin can now compare recurring weekly patterns while preserving:

- X as inner-thought-driven
- LinkedIn as professionally framed

**Step 3: Verify docs stay aligned with the implementation**

Re-read the runtime output after real `odin` verification and keep the wording honest.

### Task 5: Run full slice verification and real odin E2E proof

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

**Step 2: Prove populated-history multi-week output through the real binary**

Run a real shell session that:

- selects `marcus-social-growth-workflow`
- records current-week and prior-week social memory
- selects `marcus-social-analytics-advisor`
- asks for the retrospective

Assert the output contains:

- the original `Retrospective Window: last 7 days`
- `Comparison Window: last 4 weekly windows`
- recurring pattern sections
- `New This Week:`

**Step 3: Prove sparse-history behavior through the real binary**

Run a real shell session with only current-week memory and verify:

- the comparison block still appears
- recurring sections degrade to `- none` where appropriate

**Step 4: Report completion without overclaiming**

The final report must explicitly include:

- Existing state found
- Reused components
- New components added
- Why new components were necessary
- Real `odin` command E2E checks performed
