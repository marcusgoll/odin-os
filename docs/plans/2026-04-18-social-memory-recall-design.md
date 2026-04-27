# Social Memory Recall Design

## Problem

`odin-os` can already capture workflow-scoped social memories through `/memory remember` and display them through `/memory list`. That is enough to prove storage, but not enough to make the Marcus social workflow operationally useful over time.

The real gap is recall:

- Marcus cannot inspect one specific saved item by id.
- Marcus cannot quickly narrow history to pending approvals, approved LinkedIn outcomes, or X drafts.
- Operators must read raw unfiltered history even when the relevant data is already stored in `details_json`.

The next useful feature is therefore not more storage or a social-only command. It is better review and recall on top of the existing generic Odin memory surface.

## Existing Repo State

### Already real

- `/memory remember` and `/memory list` already exist in `internal/cli/repl/shell.go`.
- Workflow-scoped memory already persists through `internal/memory/knowledge/service.go`.
- The store already supports `RecordMemorySummary` and `ListMemorySummaries` in `internal/store/sqlite/store.go`.
- Social workflow docs already define `social_draft`, `social_outcome`, `social_learning`, and `social_research` as the recommended capture types in `docs/contracts/marcus-social-copilot.md`.
- Real `odin` verification has already proven:
  - `/workflow use marcus-social-growth-workflow`
  - `/memory remember ...`
  - `/memory list type=<type>`

### Partial

- `ListMemorySummaries` can filter by scope and memory type, but not by summary text or structured field values.
- `details_json` already stores structured `fields`, but the shell only prints it raw.
- There is no `show` command for memory ids.
- There is no operator-level limit or sort control for memory review.

### Missing

- exact-id recall
- recent-first review
- text filtering
- field-level filtering on `details_json.fields`
- memory-focused help text and contract examples for recall

## Approaches

### Option 1: Add a social-only history command

Example: `/social history ...`

Pros:

- tailored output for Marcus

Cons:

- duplicates Odin primitives
- violates the reuse rule
- creates a new command group without first extending `/memory`

Verdict: reject.

### Option 2: Expand the store and service into a full query engine first

Pros:

- better long-term query efficiency

Cons:

- larger scope than the immediate operator gap
- likely requires schema or query-surface expansion before there is proof the UX is right

Verdict: reject for this pass.

### Option 3: Extend `/memory` with show and filtered list on top of current storage

Pros:

- smallest useful improvement
- reuses existing CLI, service, store, and memory schema
- easy to prove through the real `odin` path

Cons:

- some filtering will happen in shell/service code rather than directly in SQL

Verdict: recommended.

## Selected Design

### 1. Keep `/memory` as the only operator surface

Add:

- `/memory show <id>`
- richer `/memory list` filters

Do not add a new `/social` command.

### 2. Keep storage unchanged for this pass

Do not add new tables or migrations.

Continue to rely on:

- `memory_summaries.summary`
- `memory_summaries.details_json`
- workflow/project/global scope resolution already implemented in the shell

### 3. Add narrow recall filters that match real operator needs

The first recall slice should support:

- `type=<memory_type>`
- `contains=<text>`
- `field.<name>=<value>`
- `limit=<n>`
- `order=asc|desc`

Examples:

```text
/memory list type=social_draft field.approval=pending order=desc limit=5
/memory list type=social_outcome field.channel=linkedin field.result=approved
/memory list contains=crosswind
/memory show 12
```

### 4. Filter against structured details already being stored

The CLI already writes `details_json` with this shape:

```json
{
  "source": "cli",
  "selected_workflow_key": "marcus-social-growth-workflow",
  "selected_skill_key": "marcus-x-drafting-assistant",
  "scope": "workflow",
  "scope_key": "marcus-social-growth-workflow",
  "fields": {
    "approval": "pending",
    "channel": "x"
  }
}
```

The recall filters should parse this payload and match only against `fields.<name>` for field filters. This avoids inventing a broader JSON query language.

### 5. Keep output readable and auditable

For `list`:

- keep the existing memory header line
- keep `summary=...`
- keep `details_json=...`
- add a compact `fields=` line when structured fields exist

For `show`:

- return the same memory header plus summary/details
- include `fields=` when available
- error cleanly if the id is not visible in the current resolved scope

### 6. Scope visibility remains unchanged

Recall should use the same scope resolution as existing memory commands:

- selected workflow scope first
- otherwise project or global scope as already implemented

This avoids new authorization or scope semantics.

## Testing Strategy

### Unit and shell tests

Add failing tests for:

- `/memory show <id>` returns one visible memory entry
- `/memory show <id>` rejects ids outside the current scope
- `/memory list field.approval=pending` filters correctly
- `/memory list contains=<text>` filters summary text correctly
- `/memory list limit=1 order=desc` returns the newest entry first

### Real `odin` verification

Use the built binary to prove:

```text
/workflow use marcus-social-growth-workflow
/memory remember social_draft channel=x approval=pending -- Draft A
/memory remember social_outcome channel=linkedin result=approved -- Outcome B
/memory list type=social_draft field.approval=pending order=desc limit=5
/memory show <returned-id>
```

Success means the real shell can record, filter, and inspect workflow-scoped social memory without any social-only command path.

## Non-Goals

- publish automation
- analytics ingestion
- social-specific dashboarding
- free-form JSON queries
- new memory schema or migrations

## Acceptance Criteria

- `odin` help documents the expanded `/memory` command
- operators can inspect a single memory by id
- operators can filter memory by type, summary text, and structured fields
- the Marcus social contract examples include the new recall path where useful
- the feature is proven through the real built `odin` command
