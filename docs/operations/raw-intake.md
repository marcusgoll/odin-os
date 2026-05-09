# Raw Intake Operations

`odin intake raw create/list/show` is the canonical operator surface for the raw
Intake Inbox. It records untrusted prompts, notes, logs, copied errors, and raw
event payloads as durable `intake_items` before any review, work creation, or
execution decision.

## Create

Use explicit metadata when the source is known:

```bash
odin intake raw create --source <source> --title <title> --type <type> --dedup-key <key> [--project <key>] [--requested-by <actor>] [--payload-file <path|-] [--json]
```

For an operator note, use the shorthand:

```bash
odin intake raw create --text <text> [--json]
```

Raw intake creation:

- stores one row in SQLite `intake_items`
- persists source, source type, dedupe key, requested-by, source facts, payload
  policy, received time, created time, and updated time
- stores `--text` content as raw payload evidence under `source_facts_json`
- appends an `intake.item_created` runtime event with non-secret provenance
- does not create a Work Item, Run Attempt, branch, PR, approval, or dispatch

## List And Show

Use list for inbox triage metadata:

```bash
odin intake raw list [--project <key>] [--status <status>] [--json]
```

Use show for full raw evidence:

```bash
odin intake raw show <intake-id|dedupe-key> [--json]
```

`show --json` includes the stored payload evidence and processing status when
the item has moved through intake processing or review.

## Process

Use processing to classify a raw item into an auditable review outcome:

```bash
odin intake process --id <intake-id|dedupe-key> [--json]
```

Processing:

- appends `intake.processing_started`, `intake.classified`,
  `intake.dedupe_reviewed`, `intake.routed`, and `intake.processed` events
- routes clear items to `review_required` with a draft artifact, not a Work Item
- routes ambiguous items to `needs_clarification`
- links exact duplicate dedupe-key hits and deterministic normalized-title
  near-duplicates to the earlier canonical Intake Item as
  `duplicate_linked_or_suppressed`
- preserves every raw Intake Item and its payload evidence, including duplicate
  arrivals
- does not create a Work Item, Run Attempt, approval, dispatch, branch, PR, or
  external mutation

Goal-like raw items may create a reviewable unapproved Goal with the intake row
linked to the created goal. Goal conversion does not approve, tick, run, or
mutate external systems.

## Boundary

`odin work intake` remains the GitHub issue sync surface. It is not the raw
inbox authority.

Raw intake may later be processed, reviewed, suppressed, or accepted into work,
but those are separate commands and explicit operator decisions.
