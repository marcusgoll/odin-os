# Classification, Dedupe, And Routing Design

Date: 2026-05-10
Status: approved for implementation planning

## Audit Summary

Inspected the current `odin-os` repo rules, domain context, contracts, runtime code, store schema, CLI parser, review queue, overview projection, registry agent prompts, and current repo-local `./bin/odin` command surface.

Verified the repo-local operator path with a temporary `ODIN_ROOT`:

```bash
ODIN_ROOT="$tmpdir" ./bin/odin intake raw create --text "Research release readiness constraints" --json
ODIN_ROOT="$tmpdir" ./bin/odin intake process --id intake-1 --json
ODIN_ROOT="$tmpdir" ./bin/odin intake raw create --text "Research release readiness constraints" --json
ODIN_ROOT="$tmpdir" ./bin/odin intake process --id intake-2 --json
ODIN_ROOT="$tmpdir" ./bin/odin intake raw create --text "fix this" --json
ODIN_ROOT="$tmpdir" ./bin/odin intake process --id intake-3 --json
ODIN_ROOT="$tmpdir" ./bin/odin intake review list --json
```

Observed results:

- clear research intake became `review_required` with classification `research_item`, dedupe `unique`, route `draft_research`, and a reviewable proposal.
- repeated equivalent intake became `duplicate_linked_or_suppressed` with canonical link `intake-1`.
- vague intake became `needs_clarification` with clarification prompts.
- `odin intake review list --json` showed review-required, duplicate-linked, and clarification-needed intake items.

## Existing State

The current runtime already has a real operator command family:

- `odin intake raw create/list/show`
- `odin intake process`
- `odin intake review list/show/accept/reject/clarify/archive`
- `odin intake approval list/show/approve/deny`
- unified `odin review` integration for intake, intake approvals, goal conversion, approvals, skill artifacts, knowledge context packs, and failed work.

The current storage authority is `intake_items` in SQLite. It persists:

- Workspace ID
- source facts JSON
- dedupe key
- dedupe recipe version
- status
- optional scope and scope key
- summary
- conversation transcript link
- canonical duplicate intake link
- goal link
- suppression reason
- routing notes

The current runtime also emits intake event types for processing, classification, dedupe review, routing, draft artifact creation, clarification, duplicate linking or suppression, review decisions, and approval decisions.

Registry prompt assets exist for `classifier-agent`, `deduper-agent`, and `router-agent`, but they are prompt assets, not current runtime authority.

## Reused Components

Implementation should reuse:

- `internal/cli/commands/intake.go` for operator parsing.
- `internal/app/lifecycle/run.go` for current intake command execution.
- `internal/app/lifecycle/review.go` for the unified review surface.
- `internal/core/intake/envelope.go` for source facts and core dedupe key derivation.
- `internal/core/intake/proposal.go` for reviewable proposal shape.
- `internal/store/sqlite` and the existing `intake_items` table as the persistence authority.
- `internal/runtime/events` for audit events.
- `internal/cli/overview` for operator readback.

## New Components

No new runtime subsystem is needed for this slice.

The implementation should add or refine small in-place structures only where needed:

- a typed intake processing evidence model if the current ad hoc routing notes shape remains too loose for tests and readback.
- focused tests around status vocabulary, classification categories, dedupe outcomes, route outcomes, proposal reconstruction, and review queue visibility.
- contract documentation updates if the implementation exposes or renames fields.

## Why New Components Are Necessary

The current system has the real command path, store, events, and review surface. The remaining problem is proof and contract clarity, not missing architecture.

Small typed evidence and tests are necessary because the product promise depends on reconstructing the full chain:

raw signal -> classification -> dedupe -> route -> reviewable artifact -> human decision -> optional work or goal.

Without stable evidence and tests, Odin can appear to classify and route while the review surface depends on fragile JSON notes or status aliases.

## Locked Domain Decisions

- Raw inputs must become durable **Intake Items** before work, goal, approval, or execution state is created.
- **Initiative Intake** owns generic classification and routing before project-specific intake workflows.
- V1 stored **Intake Item** statuses are: `received`, `processing`, `review_required`, `needs_clarification`, `duplicate_linked_or_suppressed`, `approval_required`, `accepted`, `rejected`, `approval_denied`, `archived`, `errored`.
- `triaging`, `resolved`, and `suppressed` are compatibility or derived readback language in V1, not preferred stored row statuses.
- Classification category is not lifecycle state. Examples: `task`, `project`, `idea`, `bug`, `research_item`, `writing_request`, `admin_item`, `routine`, `waiting_for_item`, `clarification_needed`, `archive_worthy_noise`.
- Dedupe result is not lifecycle state. Examples: `unique`, `duplicate_linked`, `semantic_duplicate_linked`.
- Route outcome is not lifecycle state. Examples: `draft_task`, `draft_research`, `draft_document`, `draft_admin_task`, `draft_incident_review`, `draft_routine`, `draft_follow_up`, `draft_idea`, `archive_candidate`, `goal_created`, `needs_clarification`, `duplicate_linked_or_suppressed`.
- Duplicate arrivals remain individual **Intake Items** and reference a canonical **Intake Item**. V1 must not add a first-class duplicate-group aggregate.
- Dedupe remains Workspace-local and may narrow further by scope after routing evidence is known. It must not collapse signals across Workspaces.
- Registry classifier/deduper/router agents may inform future behavior, but the v1 implementation must treat current Go runtime logic and persisted evidence as authority unless a later slice explicitly wires agent invocation.
- Intake processing must not create Work Items, Run Attempts, dispatches, approvals, or external mutations by default.
- Review acceptance may promote only through the existing review and policy gates.

## Selected Design

Harden the current `odin intake process` path as the canonical v1 classification, dedupe, and routing surface.

Processing should:

1. Load a durable `Intake Item`.
2. Classify it into one primary category with reason and priority score.
3. Review dedupe against prior active canonical items in the same Workspace.
4. Route the intake into one reviewable outcome.
5. Persist reconstruction evidence on the intake item.
6. Emit runtime events for the processing stages.
7. Expose the resulting proposal through `odin intake raw show`, `odin intake review show`, `odin intake review list`, and the unified `odin review` queue.

The first implementation slice should be a hardening slice, not an agent-invocation slice. It should preserve current runtime behavior while making status vocabulary, evidence shape, and readback expectations explicit and tested.

## Rejected Alternatives

### Create a new classifier service

Rejected for this slice. The repo already has a real `odin intake process` path, and adding a parallel service would split runtime authority.

### Treat registry agents as implemented classification runtime

Rejected. Registry agent Markdown is useful prompt inventory, but no live command proof showed those prompts being invoked as the runtime classifier, deduper, or router.

### Keep the older tiny intake status enum

Rejected. The current operator surface already persists and routes around review-oriented statuses. Treating them as non-canonical would preserve docs drift and force every readback to reinterpret routing notes.

### Add a duplicate-group aggregate

Rejected. Existing domain decisions require duplicate arrivals to remain individual `Intake Items` linked to a canonical item, with duplicate-wave summaries derived as projections.

## Test And Verification Plan

Focused local tests:

```bash
go test ./internal/core/intake ./internal/store/sqlite ./internal/cli/commands ./internal/app/lifecycle ./internal/cli/overview -run 'Intake|ReviewableProposal|ReviewQueue|Overview' -count=1
```

Broader local verification:

```bash
go test ./...
make build
```

Real operator proof after build:

```bash
tmpdir="$(mktemp -d)"
ODIN_ROOT="$tmpdir" ./bin/odin intake raw create --text "Research release readiness constraints" --json
ODIN_ROOT="$tmpdir" ./bin/odin intake process --id intake-1 --json
ODIN_ROOT="$tmpdir" ./bin/odin intake raw create --text "Research release readiness constraints" --json
ODIN_ROOT="$tmpdir" ./bin/odin intake process --id intake-2 --json
ODIN_ROOT="$tmpdir" ./bin/odin intake raw create --text "fix this" --json
ODIN_ROOT="$tmpdir" ./bin/odin intake process --id intake-3 --json
ODIN_ROOT="$tmpdir" ./bin/odin intake review list --json
ODIN_ROOT="$tmpdir" ./bin/odin review list --json
ODIN_ROOT="$tmpdir" ./bin/odin overview --json
rm -rf "$tmpdir"
```

E2E proof when available:

```bash
make odin-e2e-local
```

## Documentation Changes

Updated `CONTEXT.md` to canonize the v1 stored intake statuses and mark `triaging`, `resolved`, and `suppressed` as compatibility or derived readback language.

This design spec records the selected design and implementation handoff.

No ADR is needed. The decision is important, but it aligns the docs with existing runtime behavior rather than introducing a surprising or hard-to-reverse architecture choice.

## Open Blockers

None for implementation planning.

Implementation should still account for the current dirty checkout by starting in an isolated worktree or otherwise preserving unrelated local changes.

## Planning Handoff

Implement one PR-sized hardening slice:

- keep the current `odin intake process` operator path.
- make status/category/dedupe/route/proposal evidence explicit and test-backed.
- preserve review gating.
- do not invoke registry agents in this slice.
- do not create executable Work Items or Run Attempts during processing.
- prove behavior through real `./bin/odin` commands after `make build`.

## Implementation Goal Prompt

```text
/goal Implement classification, dedupe, and routing hardening in /home/orchestrator/odin-os.

Use the approved design at docs/superpowers/specs/2026-05-10-classification-dedupe-routing-design.md. Keep the work PR-sized. Make atomic commits that each leave the repo coherent. Reuse existing odin intake commands, internal/app/lifecycle intake processing, internal/core/intake proposal/envelope types, internal/store/sqlite intake_items authority, runtime events, overview projections, and the unified odin review surface. Do not introduce parallel classifier, deduper, router, queue, or duplicate-group abstractions. Do not invoke registry agent prompts in this slice unless the existing runtime already does so.

Required proof:
- go test ./internal/core/intake ./internal/store/sqlite ./internal/cli/commands ./internal/app/lifecycle ./internal/cli/overview -run 'Intake|ReviewableProposal|ReviewQueue|Overview' -count=1
- go test ./...
- make build
- tmpdir="$(mktemp -d)"; ODIN_ROOT="$tmpdir" ./bin/odin intake raw create --text "Research release readiness constraints" --json; ODIN_ROOT="$tmpdir" ./bin/odin intake process --id intake-1 --json; ODIN_ROOT="$tmpdir" ./bin/odin intake raw create --text "Research release readiness constraints" --json; ODIN_ROOT="$tmpdir" ./bin/odin intake process --id intake-2 --json; ODIN_ROOT="$tmpdir" ./bin/odin intake raw create --text "fix this" --json; ODIN_ROOT="$tmpdir" ./bin/odin intake process --id intake-3 --json; ODIN_ROOT="$tmpdir" ./bin/odin intake review list --json; ODIN_ROOT="$tmpdir" ./bin/odin review list --json; ODIN_ROOT="$tmpdir" ./bin/odin overview --json; rm -rf "$tmpdir"
- make odin-e2e-local

Delivery:
- preserve unrelated dirty worktree changes
- open a PR with Summary, Proven, Unproven, and Commands Run
- monitor checks
- fix failures in follow-up atomic commits
- merge only if checks pass and repo policy permits
```
