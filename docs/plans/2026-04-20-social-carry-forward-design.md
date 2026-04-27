# Social Carry-Forward Guidance Design

## Problem

`odin-os` already gives Marcus a strong retrospective prompt through the existing `marcus-social-growth-workflow` + `marcus-social-analytics-advisor` path:

- the last 7 days of approved and rejected outcomes
- recent learnings and research signals
- explicit X vs LinkedIn voice framing
- a 4-window comparison block with recurring versus new signals

What it still does not do is convert that signal into explicit next-week guidance. Odin can now describe what repeated and what changed, but it still leaves the user or the model to infer:

- what to keep doing
- what to avoid repeating
- what to test next week
- how X direction should differ from LinkedIn direction in the next cycle

The next slice should add that carry-forward guidance without changing the command surface or introducing a new persistence model.

## Existing Repo State

### Already real

- The Marcus analytics prompt path is already gated to the selected workflow + skill combination.
- The current prompt already computes:
  - recurring approval patterns
  - recurring rejection patterns
  - recurring learning signals
  - recurring research signals
  - new-this-week signals
- The current prompt already includes explicit X and LinkedIn voice guidance.
- The live contract already points toward experiment carryover as the next practical extension.

### Partial

- The prompt includes signal, but not explicit carry-forward recommendations.
- The prompt knows what is recurring and what is new, but it does not yet label those as keep/avoid/test guidance.
- The prompt contains platform voice rules, but not explicit next-week platform direction sections.

### Missing

- a `Next-Week Carry-Forward` block
- explicit `Keep`, `Avoid`, and `Test Next` sections
- explicit `X Direction` and `LinkedIn Direction` sections grounded in the current social signals

## Approaches

### Option 1: Persist carry-forward experiments immediately

Pros:

- durable experiment state
- future planning could recall prior recommendations directly

Cons:

- introduces new write behavior and review questions
- adds storage semantics before the guidance shape has been validated in use

Verdict: reject for this slice.

### Option 2: Add a new planning handoff command

Example: `/planning handoff`

Pros:

- explicit operator UX

Cons:

- duplicates the existing workflow + analytics path
- violates the repo’s reuse rule

Verdict: reject.

### Option 3: Add a prompt-only carry-forward block inside the existing Marcus analytics prompt

Pros:

- smallest useful change
- preserves the current operator flow
- reuses already-computed retrospective signals
- easy to prove through the real `odin` shell

Cons:

- recommendations are regenerated each time instead of stored

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

### 2. Append one compact carry-forward block after the comparison section

The prompt should append:

- `Next-Week Carry-Forward`
- `Keep`
- `Avoid`
- `Test Next`
- `X Direction`
- `LinkedIn Direction`

This should come after the existing comparison block, not replace it.

### 3. Derive each section from existing prompt signals

#### Keep

Use recurring approval patterns.

This turns repeated approval history into explicit “do more of this” guidance.

#### Avoid

Use recurring rejection patterns.

This turns repeated rejection history into explicit “stop doing this” guidance.

#### Test Next

Use:

- recurring learning signals
- recurring research signals
- new-this-week signals

This gives Odin a small set of grounded experiments rather than generic brainstorming.

### 4. Add platform-specific direction lines

#### X Direction

Always include the baseline direction:

- keep X closer to Marcus’s inner thoughts, conviction, tension, and concise observations

Then reinforce any carry-forward signals that are clearly X-specific, using labels already present in the prompt context.

#### LinkedIn Direction

Always include the baseline direction:

- keep LinkedIn more professionally framed, practical, structured, and peer-level

Then reinforce any carry-forward signals that are clearly LinkedIn-specific.

### 5. Keep the logic prompt-only

Do not:

- write carry-forward items to memory
- introduce a new memory type
- require approval before generating the carry-forward block

This slice should only improve the prompt context for the analytics advisor.

### 6. Reuse existing label formats

The first slice should not introduce a new normalization layer.

Use:

- recurring approval and rejection labels as they already appear
- recurring learning and research labels as they already appear
- new-this-week labels as they already appear

This keeps the implementation narrow and consistent with the current comparison block.

### 7. Graceful degradation

If a section has no entries:

- emit the section anyway
- show `- none`

If no platform-specific signals exist:

- still include the platform direction sections with the baseline X and LinkedIn direction text

## Prompt Shape

The new block should look roughly like:

- `Next-Week Carry-Forward`
- `Keep:`
- `Avoid:`
- `Test Next:`
- `X Direction:`
- `LinkedIn Direction:`

The content should be short, list-shaped, and machine-oriented because it is prompt context rather than user-facing copy.

## Testing Strategy

Add shell tests for:

- the carry-forward block appears only for the Marcus analytics path
- recurring approvals appear under `Keep`
- recurring rejections appear under `Avoid`
- recurring learnings, recurring research, and new-this-week signals appear under `Test Next`
- `X Direction` includes the baseline X guidance and any X-specific carry-forward signals
- `LinkedIn Direction` includes the baseline LinkedIn guidance and any LinkedIn-specific carry-forward signals
- sparse-history cases degrade gracefully with `- none` where appropriate

Real `odin` verification should prove:

- the current retrospective block still appears
- the current comparison block still appears
- the new carry-forward block appears beneath them
- populated-history and sparse-history sessions both work through the live shell

## Non-Goals

- experiment persistence
- approval queues for carry-forward items
- a new planning command
- semantic clustering of themes
- changing the current memory schema

## Acceptance Criteria

- the Marcus analytics prompt includes a `Next-Week Carry-Forward` block
- `Keep`, `Avoid`, and `Test Next` are derived from existing retrospective/comparison signals
- `X Direction` and `LinkedIn Direction` are explicit and remain platform-specific
- the feature is prompt-only and does not add new persistence
- the feature is proven through the real `odin` binary in `odin-os`
