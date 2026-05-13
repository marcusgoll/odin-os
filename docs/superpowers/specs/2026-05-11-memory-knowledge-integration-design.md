---
title: Memory and Knowledge Integration Design
date: 2026-05-11
status: approved-for-implementation-planning
scope: odin-os memory and knowledge integration v1, slice 1
---

# Memory and Knowledge Integration Design

## Purpose

Odin should preserve useful operational context only when the context is scoped,
reviewable, provenance-backed, and safe to recall later.

This design separates two overloaded ideas:

- **Knowledge** is read-only retrieval and context-pack construction from
  existing Odin runtime state.
- **Memory** is durable context that Odin may recall later, and new memory must
  pass through an explicit proposal and review lifecycle before it becomes
  active recall material.

## Audit Summary

Inspected:

- `README.md`
- `CONTEXT.md`
- `WORKFLOW.md`
- `docs/contracts/repo-layout.md`
- `docs/contracts/workspace-context-map.md`
- `docs/contracts/tui-overview.md`
- `docs/contracts/runtime-events.md`
- `docs/contracts/companion-contract.md`
- `docs/superpowers/specs/2026-05-10-approval-gated-execution-review-queue-design.md`
- `docs/superpowers/specs/2026-05-10-approval-gated-execution-policy-parity-design.md`
- `internal/app/lifecycle/run.go`
- `internal/app/lifecycle/review.go`
- `internal/app/lifecycle/review_sources.go`
- `internal/app/lifecycle/run_test.go`
- `internal/cli/commands/knowledge.go`
- `internal/memory`
- `internal/runtime/knowledge`
- `internal/runtime/projections`
- `internal/store/sqlite/migrations/0010_memory_and_conversations.sql`
- `internal/store/sqlite/migrations/0016_memory_scopes.sql`
- `internal/store/sqlite/store.go`
- `internal/store/sqlite/models.go`

Verified from an isolated worktree based on `origin/main` at
`1c3069a6db00a2a6a8cf9581f4726de69d0249a5`:

```bash
make build
go test ./...
which odin
ODIN_ROOT="$tmp" ./bin/odin help
ODIN_ROOT="$tmp" ./bin/odin knowledge help
ODIN_ROOT="$tmp" ./bin/odin knowledge search query=memory --json
ODIN_ROOT="$tmp" ./bin/odin memory help
```

Observed:

- `odin help` includes `knowledge`.
- `odin knowledge help` exposes `search`, `context-pack`, `context-pack show`,
  and `context-packs`.
- `odin knowledge search query=memory --json` is read-only and returns
  `persistence=none`.
- `odin memory` is still not a top-level command.
- `internal/runtime/knowledge.Service` can propose context packs and accepts
  context-pack review through `odin review act context-pack:<id> accept`.
- Accepted context-pack proposals record a `memory_summaries` row with
  `memory_type=context_pack` and `source_context_pack_id`.
- `odin review` already lists `memory-proposal:<id>` rows sourced from
  `memory_summaries`, but `review act memory-proposal:<id> ...` is unsupported
  and returns `memory_proposal_review_not_implemented`.
- `memory_entries` exists for scoped workspace, initiative, companion, task,
  and run memory entries, while `memory_summaries` exists for summary-style
  runtime memory.

## Existing State

Odin already has three useful foundations.

First, `odin knowledge` is a real top-level operator command. It is currently a
read-only retrieval and context-pack surface over runtime tasks, events, runs,
and context packets. It does not mutate logs, jobs, runs, approvals, or memory
unless the operator explicitly uses `--propose` for a context pack.

Second, context-pack proposal review is already end-to-end. A proposed context
pack is stored as a `context_packet`, appears in `odin review` as
`context-pack:<id>`, and on acceptance records a scoped `memory_summaries`
record.

Third, memory proposal visibility already exists as a partial review source.
`memory_summaries` rows whose details contain `approval=pending` or
`status=pending` appear as `memory-proposal:<id>` in `odin review`, with
governance risk and no allowed actions.

## Gaps

The remaining gap is not a knowledge command surface. That exists.

The missing v1 boundary is explicit memory proposal and resolution:

- no top-level `odin memory` command exists for proposal creation, listing, or
  inspection;
- memory proposals are visible in `odin review`, but review actions fail closed;
- normal recall paths do not yet have a clearly documented rule to exclude
  pending or rejected proposal records;
- sensitive memory has no first-class pending proposal envelope that can store
  redacted review text and source references without making the sensitive
  content active memory;
- provenance lives in ad hoc JSON details rather than a small documented v1
  schema;
- `odin knowledge` and future `odin memory` responsibilities are easy to
  confuse unless the command contract is explicit.

## Reused Components

Implementation should reuse:

- `odin knowledge` command family for read-only retrieval and context-pack
  proposal creation.
- `internal/runtime/knowledge.Service` for context-pack proposal review.
- `odin review list/show/act` and the existing source-adapter pattern in
  `internal/app/lifecycle/review_sources.go`.
- Existing `memoryProposalReviewQueueSource` visibility.
- Existing `memory_summaries` storage for summary-style durable memory.
- Existing `memory_entries` storage for scoped entry-style memory when later
  recall workflows need entry bodies.
- `UpdateMemorySummaryDetails` and existing memory-summary lineage validation.
- Runtime events for durable audit evidence.
- Existing `scope`, project, task, run, context-packet, and review identifiers
  rather than introducing unscoped note IDs.

## New Components

Add the smallest set of missing pieces:

- `odin memory` top-level command group:
  - `memory propose ...`
  - `memory list [scope=<scope>] [project=<key>] [type=<type>] [status=<status>]`
  - `memory show <id|memory-proposal:<id>>`
  - `memory resolve <id|memory-proposal:<id>> <accept|reject|archive> because <reason...>`
- a typed **Memory Proposal** details envelope stored in
  `memory_summaries.details_json`;
- a source-owned memory proposal resolver reused by both `odin memory resolve`
  and `odin review act memory-proposal:<id> ...`;
- active-memory filtering so pending, rejected, and archived proposal records
  are not returned by normal recall or list views unless explicitly requested;
- provenance fields for source type, source ID, task, run, context packet,
  source URL, creator, reviewer, sensitivity, and redaction notes;
- runtime audit events for memory proposal creation and resolution.

No new table is required for the first slice unless implementation discovers
that filtering proposal rows out of existing recall paths cannot be made safe
without schema support.

## Why New Components Are Necessary

`odin knowledge` already answers "what context can Odin retrieve or package
from existing runtime truth?" It should not become a write surface for durable
memory.

The missing command is `odin memory`, because the operator needs to answer
"what should Odin be allowed to remember later?" That is a governed decision,
not a search operation.

The resolver is necessary because review queue visibility without mutation is
not enough. Operators can see `memory-proposal:<id>`, but cannot approve,
reject, or archive it through the canonical queue. That leaves memory
persistence halfway between a runtime fact and an operator decision.

The provenance envelope is necessary because memory is only useful if Odin can
later explain why it remembers something, where it came from, who approved it,
and whether it was redacted or sensitivity-limited.

## Locked Domain Decisions

- **Knowledge** means read-only retrieval and context-pack construction from
  existing Odin runtime state.
- **Memory** means durable recallable context.
- **Memory Proposal** means a candidate memory record that is not active recall
  material until accepted.
- `odin knowledge` remains read-only except for explicit context-pack proposal
  creation with `--propose`.
- `odin memory` owns memory proposal creation, inspection, and resolution.
- `odin review` remains the unified governed decision queue.
- `memory-proposal:<id>` is the canonical review queue ID for generic memory
  proposals.
- Pending, rejected, and archived memory proposals must not appear in normal
  active-memory recall.
- Sensitive memory proposals must not copy raw sensitive material into active
  memory before explicit acceptance.
- A pending proposal may store redacted summary text and source references so
  the operator can make a decision.
- Every accepted memory must carry source/provenance details sufficient for
  later inspection.
- The v1 implementation should reuse `memory_summaries` before adding a new
  table.

## Selected Design

### Operator Workflow

1. Odin or an operator creates a memory proposal:

   ```bash
   odin memory propose scope=project project=pbs type=operating_note \
     source_type=run source_id=42 sensitivity=normal \
     --summary "PBS logbook sync needs CRJ tail evidence before type correction"
   ```

2. The proposal appears in:

   ```bash
   odin memory list status=pending --json
   odin review list --json
   odin review show memory-proposal:<id> --json
   ```

3. The operator resolves it from either surface:

   ```bash
   odin memory resolve memory-proposal:<id> accept because source evidence is durable
   odin review act memory-proposal:<id> accept --json
   ```

4. Acceptance marks the memory active and recallable. Rejection and archive keep
   the audit record but exclude it from active recall.

### Memory Proposal Envelope

Store a v1 envelope in `memory_summaries.details_json`:

```json
{
  "schema": "memory_proposal.v1",
  "status": "pending",
  "approval": "pending",
  "source": {
    "type": "run",
    "id": "42",
    "key": "run-42",
    "url": "",
    "context_packet_id": 17
  },
  "provenance": {
    "created_by": "odin",
    "created_via": "memory.propose",
    "reviewed_by": "",
    "review_reason": ""
  },
  "safety": {
    "sensitivity": "normal",
    "redacted": false,
    "restricted_recall": false
  }
}
```

Allowed proposal statuses:

- `pending`
- `accepted`
- `rejected`
- `archived`

The existing review source may continue to treat `approval=pending` and
`status=pending` as pending during migration, but new writes should use the
v1 envelope.

### Command Contract

`odin memory propose` creates a pending `memory_summaries` record. It must
require:

- explicit scope;
- memory type;
- summary;
- source type and at least one source handle;
- sensitivity classification.

`odin memory list` defaults to accepted memory only. It may list pending,
rejected, or archived proposals only when `status=<status>` is supplied.

`odin memory show` shows the memory summary, typed details, and provenance. It
must identify whether the record is active recall material.

`odin memory resolve` calls the same resolver used by `odin review act`.

### Review Resolver

`odin review act memory-proposal:<id> accept` should:

- validate that the proposal is pending;
- require a reason;
- update the proposal envelope to `status=accepted` and `approval=accepted`;
- record reviewer and reason;
- emit a memory proposal resolved event;
- return a standard review action receipt with `status=resolved`,
  `result=accepted`, `supported=true`, `mutation_scope=review_state`,
  and `mutated=true`.

Reject and archive follow the same receipt contract but do not make the memory
active.

### Active Recall Filtering

Any current or future memory recall path must treat active memory as:

- `memory_summaries` with no proposal envelope; or
- `memory_summaries` with `schema=memory_proposal.v1` and `status=accepted`.

Pending, rejected, and archived proposals are inspectable through proposal
surfaces only.

### Event Contract

Add runtime event types:

- `memory.proposal_created`
- `memory.proposal_resolved`

Payloads must include memory ID, scope, scope key, memory type, status,
decision, source type, source ID or key, sensitivity, reviewer when present,
and reason when present. Payloads must not include raw sensitive content.

## Rejected Alternatives

### Put Memory Writes Under `odin knowledge`

Rejected because `odin knowledge` is already documented and proven as
read-only retrieval except explicit context-pack proposal creation. Making it
the generic memory write surface would blur retrieval and persistence.

### Create A Parallel Memory Review Queue

Rejected because `odin review` is already the unified governed decision queue.
Adding a memory-only queue would split operator attention and duplicate review
receipt semantics.

### Add A New `memory_proposals` Table First

Deferred. A dedicated table may become useful once proposals need richer body
storage, encryption, retention controls, or batch review. The first slice can
reuse `memory_summaries` because pending memory proposals already appear there
and are visible in `odin review`.

### Let Pending Memory Be Recallable With A Flag

Rejected. Pending memory is not approved memory. Recall filtering must fail
closed so unapproved or sensitive candidates do not silently influence future
work.

## Test And Verification Plan

Add focused tests for:

- `odin memory help` appears in `odin help`.
- `odin memory propose ... --json` creates a pending memory proposal with a v1
  envelope.
- `odin memory list` excludes pending proposals by default.
- `odin memory list status=pending --json` includes pending proposals.
- `odin review list --json` includes `memory-proposal:<id>` with allowed
  actions.
- `odin review act memory-proposal:<id> accept --json` resolves the proposal
  and returns the standard receipt shape.
- accepted memory appears in default `odin memory list`.
- rejected and archived proposals remain excluded from active recall.
- pending sensitive proposals store redacted review text and source references,
  not raw sensitive content.
- duplicate resolution is idempotent or fails closed with a stable reason.

Run:

```bash
go test ./internal/app/lifecycle -run 'Memory|ReviewQueue|Knowledge' -count=1
go test ./internal/memory/... ./internal/runtime/knowledge ./internal/store/sqlite -run 'Memory|Knowledge|Proposal|Review' -count=1
go test ./...
make build
```

Real command proof:

```bash
tmp="$(mktemp -d)"
ODIN_ROOT="$tmp" ./bin/odin help
ODIN_ROOT="$tmp" ./bin/odin memory help
ODIN_ROOT="$tmp" ./bin/odin memory propose scope=project project=pbs type=operating_note source_type=operator source_key=manual sensitivity=normal --summary "Pilot proof memory"
ODIN_ROOT="$tmp" ./bin/odin memory list status=pending --json
ODIN_ROOT="$tmp" ./bin/odin review list --json
ODIN_ROOT="$tmp" ./bin/odin review act memory-proposal:1 accept --json
ODIN_ROOT="$tmp" ./bin/odin memory list --json
rm -rf "$tmp"
```

## Documentation Changes

Implementation should update:

- `docs/contracts/tui-overview.md` to clarify that `Memory` shows active memory
  plus proposal filters, while `Knowledge` remains retrieval/context-pack.
- `docs/contracts/runtime-events.md` with memory proposal events.
- `docs/contracts/repo-layout.md` only if the implementation adds a new store
  table or package boundary.
- CLI help text and tests for `odin memory`.

An ADR is not required for this slice. The design reuses existing review,
memory, and knowledge boundaries rather than making a hard-to-reverse
architecture choice.

## Open Blockers

No design blockers remain for the first implementation slice.

Implementation must still verify whether active-memory filtering can be added
safely with the existing `memory_summaries` details envelope. If not, the
implementation should stop and propose the narrowest schema addition rather
than shipping recall paths that can include pending memory.

## Planning Handoff

Implement one PR-sized slice:

- add the top-level `odin memory` command group;
- create pending memory proposals with a typed details envelope;
- make `memory-proposal:<id>` review actions supported;
- filter active memory from pending/rejected/archived proposals;
- add runtime events and real command proof.

Do not implement document ingestion, vector search, external RAG, encryption,
batch review, or broad memory recommendation generation in this slice.

## Implementation Goal Prompt

```text
/goal Implement memory proposal resolution in /home/orchestrator/odin-os.

Use the approved design at docs/superpowers/specs/2026-05-11-memory-knowledge-integration-design.md. Keep the work PR-sized. Make atomic commits that each leave the repo coherent. Reuse existing odin review, internal/app/lifecycle review handlers, internal/memory, internal/runtime/knowledge, memory_summaries, memory_entries, and runtime events. Do not introduce a parallel review queue or turn odin knowledge into the generic memory write surface.

Required proof:
- go test ./internal/app/lifecycle -run 'Memory|ReviewQueue|Knowledge' -count=1
- go test ./internal/memory/... ./internal/runtime/knowledge ./internal/store/sqlite -run 'Memory|Knowledge|Proposal|Review' -count=1
- go test ./...
- make build
- real temp-root ./bin/odin proof for memory help, memory propose, memory list status=pending, review list, review act memory-proposal accept, and accepted memory list

Delivery:
- open a PR with Summary, Proven, Unproven, and Commands Run
- monitor checks
- fix failures in follow-up atomic commits
- merge only if checks pass and repo policy permits
```
