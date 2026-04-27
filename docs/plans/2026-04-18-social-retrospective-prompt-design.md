# Social Retrospective Prompt Design

## Problem

`odin-os` already has the parts needed for a useful weekly retrospective:

- workflow-scoped social memory
- normalized `social_outcome` entries
- filtered memory recall
- a dedicated analytics and retrospective skill
- a workflow that already promises a retrospective summary

What it does not yet have is a repeatable operator path that automatically assembles the last week of relevant social history into the analytics prompt. Without that, the retrospective depends on Marcus manually gathering context, and the output is more likely to drift into generic summary language.

The next useful slice is therefore not a new retrospective command. It is bounded retrospective prompt assembly inside the existing `marcus-social-growth-workflow` + `marcus-social-analytics-advisor` path.

## Existing Repo State

### Already real

- `/workflow use` and `/skill use` already shape prompt context through `internal/cli/repl/shell.go`.
- The prompt path already injects workflow and skill definitions through `composeExecutionPrompt(...)`.
- The Marcus workflow already lists retrospective work as part of the procedure and outputs.
- The analytics advisor skill already expects post history, approval outcomes, engagement metrics when available, and qualitative notes.
- Social memory can already be stored and recalled through `/memory remember`, `/memory list`, and `/memory show`.
- `social_outcome` history is now normalized enough to support reliable approval/rejection analysis.

### Partial

- Operators can manually ask for a retrospective, but Odin does not yet assemble recent social memory into the prompt.
- The current prompt path does not distinguish platform voice in the retrospective framing.
- The prompt path does not yet bound the retrospective to a weekly memory window.

### Missing

- automatic last-7-days social-memory assembly
- prompt framing for the X vs LinkedIn voice split
- a repeatable retrospective context block for the analytics advisor

## Approaches

### Option 1: Keep the retrospective manual

Marcus manually recalls memory and pastes context into the prompt.

Pros:

- no code changes

Cons:

- not repeatable
- easy to miss important history
- encourages generic retrospective output

Verdict: reject.

### Option 2: Add a dedicated retrospective command

Example: `/retrospective weekly`

Pros:

- explicit operator UX

Cons:

- duplicates the existing workflow + skill path
- violates the reuse direction already established for the social workflow

Verdict: reject.

### Option 3: Auto-assemble recent social memory only for the workflow + analytics advisor combination

Pros:

- smallest useful change
- keeps the current UX
- uses existing workflow, skill, and memory primitives
- makes the retrospective repeatable without new command surfaces

Cons:

- special-cases one workflow + skill combination in prompt enrichment

Verdict: recommended.

## Selected Design

### 1. Keep the current operator flow

The operator path stays:

```text
/workflow use marcus-social-growth-workflow
/skill use marcus-social-analytics-advisor
<free-text retrospective request>
```

No new command is added.

### 2. Only auto-assemble for the Marcus retrospective path

Automatic retrospective context should appear only when:

- the selected workflow is `marcus-social-growth-workflow`
- the selected skill is `marcus-social-analytics-advisor`

This keeps the behavior narrow and avoids surprising other skill flows.

### 3. Use the last 7 days as the default memory window

The assembled context should include only memory entries with `created_at` within the last 7 days.

This keeps the retrospective weekly, bounded, and predictable.

### 4. Include only the highest-signal social memory types

Use:

- `social_outcome`
- `social_learning`
- `social_research`

Do not include `social_draft` in the retrospective context by default. Drafts are useful during planning, but a retrospective should focus on outcomes, durable learnings, and research signals.

### 5. Bound the amount of memory per type

The assembled context should be capped to a small recent set per type so the prompt stays readable. A practical first slice is:

- up to 6 recent `social_outcome`
- up to 4 recent `social_learning`
- up to 4 recent `social_research`

These should be the most recent entries inside the last-7-days window.

### 6. Add a retrospective context block ahead of the task request

When active, the prompt should include a block such as:

- retrospective window: last 7 days
- approved outcomes
- rejected outcomes
- recent durable learnings
- recent research signals
- platform voice framing

The voice framing should explicitly say:

- `X`: express Marcus’s inner thoughts, perspective, conviction, tension, and concise observations
- `LinkedIn`: use more professional framing, clearer structure, practical lessons, and peer-level authority

This should steer the retrospective away from generic “post better” advice and toward platform-specific next-week guidance.

### 7. Degrade gracefully when no recent memory exists

If no qualifying memory exists in the last 7 days:

- do not fail the prompt
- include a short note that no recent retrospective memory was found
- still pass through workflow + skill context so the model can answer with the requested structure

## Testing Strategy

Add shell tests for:

- retrospective prompt enrichment when the Marcus workflow and analytics advisor are both active
- inclusion of recent `social_outcome`, `social_learning`, and `social_research`
- exclusion of older-than-7-days memory
- exclusion of `social_draft`
- no enrichment for unrelated skill selections

Real `odin` verification should prove:

- recent social memory is automatically injected into the live prompt
- the prompt includes the X vs LinkedIn framing
- older memory is excluded
- the path still works when recent memory is absent

## Non-Goals

- a new retrospective command
- analytics ingestion from official APIs
- automatic metric calculation
- new memory schema or migrations
- generalized auto-context assembly for all skills

## Acceptance Criteria

- with the Marcus workflow + analytics advisor selected, Odin injects last-7-days social memory into the retrospective prompt
- the prompt includes platform voice framing for X and LinkedIn
- only `social_outcome`, `social_learning`, and `social_research` are included
- older memory is excluded
- the feature is proven through the real `odin` binary in `odin-os`
