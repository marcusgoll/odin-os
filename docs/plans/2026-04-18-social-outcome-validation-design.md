# Social Outcome Validation Design

## Problem

`odin-os` can already capture `social_outcome` entries through `/memory remember` and recall them through `/memory list` and `/memory show`. That proves storage and retrieval, but it does not yet guarantee that the stored history is explicit enough to support approval analysis or future retrospective prompts.

Right now, `social_outcome` is convention-based:

- an entry may include `result`
- an entry may include `channel`
- an entry may or may not identify what kind of content was approved or rejected

That leaves approval history too loose for reliable filtering and future reporting.

The next useful step is to validate `social_outcome` entries at write time inside the existing `/memory remember` flow.

## Existing Repo State

### Already real

- `/memory remember`, `/memory list`, and `/memory show` already exist in `internal/cli/repl/shell.go`.
- Social workflow docs already define `social_outcome` as a recommended capture type in `docs/contracts/marcus-social-copilot.md`.
- Filtered recall already works against `details_json.fields`, so future history review can already query `field.result=...`, `field.channel=...`, and similar keys.
- Workflow-scoped social memory is already proven through the real `odin` shell.

### Partial

- `social_outcome` entries can be filtered by result or channel only if those fields were supplied consistently.
- There is no required `content_kind`, so approved and rejected history does not consistently distinguish posts from replies, threads, or article seeds.
- The shell accepts any field values today, including typos or incomplete entries.

### Missing

- required fields for `social_outcome`
- validation of allowed values
- clear operator errors for invalid outcome logging
- updated examples that show explicit approved vs rejected history logging

## Approaches

### Option 1: Validate `social_outcome` inside `/memory remember`

Pros:

- smallest useful change
- reuses the current CLI surface
- immediately improves recall quality without new storage

Cons:

- validation logic lives in shell-side memory command handling

Verdict: recommended.

### Option 2: Add a new `/memory outcome ...` helper

Pros:

- more specialized syntax

Cons:

- duplicates the existing `/memory remember` behavior
- expands command surface without necessity

Verdict: reject.

### Option 3: Skip validation and build retrospective prompts first

Pros:

- faster path to higher-level summaries

Cons:

- builds on loosely structured history
- makes retrospective output less trustworthy

Verdict: reject for this pass.

## Selected Design

### 1. Keep `/memory remember` as the only write path

Do not add a new command.

All outcome-history normalization should happen inside the existing `/memory remember <memory_type> ...` flow.

### 2. Add memory-type-specific validation for `social_outcome`

When `memory_type == social_outcome`, require these fields:

- `result=approved|rejected`
- `channel=x|linkedin`
- `content_kind=post|reply|thread|article_seed`

Allow optional fields such as:

- `draft_key`
- `reason`
- `scheduled_for`

Do not validate optional field names beyond normal `key=value` parsing.

### 3. Reject invalid outcome logging before persistence

If required fields are missing or invalid:

- do not persist the memory summary
- print a clear operator error
- include the normal `/memory` usage line so the correction path is obvious

### 4. Keep all recall behavior unchanged

No new recall command is needed.

The existing recall paths already support:

- `/memory list type=social_outcome field.result=approved`
- `/memory list type=social_outcome field.result=rejected`
- `/memory list type=social_outcome field.channel=linkedin`
- `/memory show <id>`

This slice improves the quality of the stored history, not the recall surface.

### 5. Update social examples to show explicit approval history

The Marcus social contract should show both approved and rejected outcome logging with `content_kind`, so the examples match the enforced rules.

## Testing Strategy

Add shell tests for:

- valid approved outcome logging
- valid rejected outcome logging
- rejected write when `result` is missing
- rejected write when `result` is invalid
- filtered recall for `field.result=approved` and `field.result=rejected`

Real `odin` verification should prove:

- valid approved logging is stored
- valid rejected logging is stored
- invalid logging is rejected
- filtered recall separates approved vs rejected history

## Non-Goals

- weekly retrospective prompts
- outcome aggregation reports
- new memory schema or migration
- dedicated outcome commands
- analytics ingestion

## Acceptance Criteria

- `social_outcome` writes require `result`, `channel`, and `content_kind`
- invalid values are rejected with a clear shell error
- approved and rejected outcome history can be filtered reliably through the existing `/memory list` path
- docs/examples reflect the normalized outcome format
- the behavior is proven through the real `odin` binary in `odin-os`
