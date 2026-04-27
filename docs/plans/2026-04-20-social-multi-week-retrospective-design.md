# Social Multi-Week Retrospective Design

## Problem

`odin-os` can already assemble a useful weekly retrospective for Marcus through the existing `marcus-social-growth-workflow` + `marcus-social-analytics-advisor` path. That gives Odin the last 7 days of approved outcomes, rejected outcomes, learnings, research signals, and explicit X vs LinkedIn voice guidance.

What is still missing is comparison across repeated weekly cycles. Odin can say what happened this week, but it cannot yet tell Marcus:

- which approval patterns are recurring
- which rejection patterns keep repeating
- which learnings are holding across several weeks
- which signals are new versus durable

The next slice should add that comparison without creating a new command, schema, or social-only subsystem.

## Existing Repo State

### Already real

- The shell already injects retrospective context only when the active workflow is `marcus-social-growth-workflow` and the active skill is `marcus-social-analytics-advisor`.
- The current retrospective helper already queries workflow-scoped social memory and filters it by `created_at`.
- `social_outcome` is already normalized with required `result`, `channel`, and `content_kind`.
- `social_learning` and `social_research` can already be recorded and recalled through the generic `/memory` command.
- The live Marcus contract already lists multi-week comparison as the next practical extension.

### Partial

- The current prompt only looks back 7 days.
- The current prompt separates approved and rejected outcomes, but only inside one weekly window.
- Learnings and research are included, but only as current-week context.

### Missing

- rolling multi-week bucket assembly
- compact comparison summaries across several weekly windows
- explicit distinction between recurring signals and newly appearing signals

## Approaches

### Option 1: Replace the current 7-day retrospective with a 4-week summary

Pros:

- one summary block
- no need to preserve the current-week section

Cons:

- loses the current weekly focus
- makes the prompt more abstract and less actionable

Verdict: reject.

### Option 2: Add a separate multi-week retrospective command

Example: `/retrospective compare`

Pros:

- explicit operator UX

Cons:

- duplicates the current workflow + skill path
- violates the repo’s reuse rule

Verdict: reject.

### Option 3: Keep the current 7-day retrospective and add a compact multi-week comparison block inside the same prompt path

Pros:

- preserves the existing operator flow
- reuses the same workflow, skill, memory, and prompt assembly primitives
- keeps the weekly snapshot while adding historical pattern detection

Cons:

- adds more prompt context, so the comparison block must stay tightly bounded

Verdict: recommended.

## Selected Design

### 1. Keep the current operator flow unchanged

The operator path remains:

```text
/workflow use marcus-social-growth-workflow
/skill use marcus-social-analytics-advisor
<free-text retrospective request>
```

No new command is added.

### 2. Use the last 4 rolling weekly windows

The comparison horizon should be the most recent 4 rolling 7-day windows ending at `now`, not calendar weeks. This preserves compatibility with the current “last 7 days” retrospective and avoids waiting for week boundaries.

The windows should be:

- Window 1: now minus 7 days through now
- Window 2: now minus 14 days through now minus 7 days
- Window 3: now minus 21 days through now minus 14 days
- Window 4: now minus 28 days through now minus 21 days

### 3. Reuse the same memory types

Use the same social memory types already trusted by the weekly retrospective:

- `social_outcome`
- `social_learning`
- `social_research`

Do not include `social_draft`.

### 4. Keep the current 7-day block intact

The existing weekly retrospective block should remain unchanged in spirit:

- last 7 days
- approved outcomes
- rejected outcomes
- learnings
- research
- X vs LinkedIn voice guidance

The multi-week comparison should be appended after that block, not replace it.

### 5. Add a compact comparison block

The new block should include:

- `Comparison Window: last 4 weekly windows`
- one compact line per weekly window with date range and counts
- recurring approval patterns
- recurring rejection patterns
- recurring learning themes
- recurring research signals
- newly appearing signals in the current week

The comparison block should stay short enough that it supports the prompt rather than dominating it.

### 6. Derive patterns from existing fields and formatted lines

#### Outcomes

Outcomes should be grouped by:

- `result`
- `channel`
- `content_kind`

That yields compact recurring labels such as:

- `linkedin/post approved`
- `x/reply rejected`

This is more stable than comparing full free-text summaries.

#### Learnings and research

Learnings and research should use their already formatted retrospective lines as the comparison label. This keeps the first slice simple and avoids introducing a new theme schema.

### 7. Distinguish recurring signals from new signals

For the current week:

- a signal is `recurring` if its comparison label appears in at least one prior weekly window
- a signal is `new` if it appears in the current week and in no prior weekly window

This allows Odin to tell Marcus which patterns are persistent versus recently emerging.

### 8. Graceful degradation

If there is not enough older memory for useful comparison:

- do not fail
- still emit the comparison window section
- show `- none` for missing pattern lists

The current 7-day retrospective should continue to work even when historical comparison data is sparse.

## Prompt Shape

The new block should look roughly like:

- `Comparison Window: last 4 weekly windows`
- `Week 1 (YYYY-MM-DD to YYYY-MM-DD): approved=X rejected=Y learnings=Z research=Q`
- `Week 2 (...) ...`
- `Week 3 (...) ...`
- `Week 4 (...) ...`
- `Recurring Approval Patterns:`
- `Recurring Rejection Patterns:`
- `Recurring Learning Signals:`
- `Recurring Research Signals:`
- `New This Week:`

The exact prose can stay machine-oriented because this is prompt context, not end-user copy.

## Testing Strategy

Add shell tests for:

- comparison block appears only for the Marcus workflow + analytics advisor path
- four weekly windows are represented in descending recency order
- recurring approval and rejection patterns are detected from prior windows
- recurring learning and research signals are detected
- current-week-only signals appear in the `New This Week` section
- `social_draft` still stays excluded
- sparse history degrades gracefully without breaking the prompt

Real `odin` verification should prove:

- the current weekly retrospective still appears
- the new multi-week comparison block appears beneath it
- recurring and new signals are visible in the live prompt
- the path still works when older weeks are missing

## Non-Goals

- new CLI commands
- memory migrations
- semantic clustering of free-text learnings
- official analytics import
- replacing the weekly retrospective with a long historical report

## Acceptance Criteria

- the Marcus analytics prompt includes a new multi-week comparison block using the last 4 rolling weekly windows
- the current 7-day retrospective block still appears
- outcomes are compared by `result/channel/content_kind`
- learnings and research are compared using their existing formatted labels
- the prompt distinguishes recurring signals from new current-week signals
- the feature is proven through the real `odin` binary in `odin-os`
