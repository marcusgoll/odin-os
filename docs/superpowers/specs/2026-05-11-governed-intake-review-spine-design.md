# Governed Intake And Review Spine Design

Date: 2026-05-11
Status: approved for implementation planning

## Audit Summary

Audited current `origin/main` after PRs #204 and #206 in an isolated worktree at `/home/orchestrator/odin-os/.worktrees/intake-review-queue-design`.

Read and compared:

- `CONTEXT.md`
- `README.md`
- `docs/operations/raw-intake.md`
- `docs/operations/work-intake.md`
- `docs/contracts/tui-overview.md`
- `docs/contracts/external-intake.md`
- `docs/superpowers/specs/2026-05-10-classification-dedupe-routing-design.md`
- `docs/superpowers/specs/2026-05-10-approval-gated-execution-review-queue-design.md`
- root product brief draft at `docs/plans/2026-05-09-odin-os-governed-operating-system.md`
- `internal/cli/commands/intake.go`
- `internal/app/lifecycle/run.go`
- `internal/app/lifecycle/review.go`
- `internal/app/lifecycle/review_sources.go`
- `internal/store/sqlite/intake_items_test.go`
- `internal/app/lifecycle/review_test.go`

Built the repo-local binary and proved the current command path with a temporary `ODIN_ROOT`:

```bash
make build
ODIN_ROOT="$tmp" ./bin/odin intake raw create --source codex-cli --title 'Release readiness research request' --type prompt --dedup-key release-readiness-research --requested-by codex --payload-file - --json
ODIN_ROOT="$tmp" ./bin/odin intake raw list --json
ODIN_ROOT="$tmp" ./bin/odin intake raw show intake-1 --json
ODIN_ROOT="$tmp" ./bin/odin intake process --id intake-1 --json
ODIN_ROOT="$tmp" ./bin/odin intake review list --json
ODIN_ROOT="$tmp" ./bin/odin review list --json
ODIN_ROOT="$tmp" ./bin/odin work status --json
```

The proof showed raw evidence persisted with source, timestamp, dedupe key, requested-by, and original JSON payload; processing produced classification, dedupe, priority score, suggested route, and a review-required draft artifact; `odin review list --json` surfaced the resulting item; `odin work status` reported `work_items=0` and `active_run_attempts=0`.

Focused tests also passed:

```bash
go test ./internal/app/lifecycle -run 'TestReviewQueue|TestRunReview|UnifiedReview|TestReviewQueueIncludesAllGovernedDecisionSources' -count=1
go test ./internal/cli/commands ./internal/store/sqlite ./internal/app/lifecycle -run 'Intake|Raw|Process|Review' -count=1
```

## Existing State

Current `main` already has the core governed intake spine:

- `odin intake raw create/list/show`
- `odin intake process --id <id|key>`
- `odin intake review list/show/accept/reject/clarify/archive`
- `odin intake approval list/show/approve/deny`
- `odin review list/show/approve/reject/act`
- SQLite `intake_items` as raw intake authority
- processing evidence stored in intake routing notes and emitted as runtime events
- one review queue composed through `internal/app/lifecycle/review_sources.go`

The root product brief draft already states the desired product definition and the hard command-proof rule, but those decisions were not yet locked in current `main` authority docs.

## Reused Components

Use the existing components as the canonical implementation path:

- `cmd/odin` and `internal/cli/commands/intake.go` for the operator surface
- `internal/app/lifecycle/run.go` for raw intake creation and processing
- `internal/app/lifecycle/review.go` plus `review_sources.go` for the unified queue
- `internal/store/sqlite` for Intake Items, Work Items, approvals, memory summaries, context packets, skill artifacts, automation triggers, and runtime events
- `internal/runtime/projections` for approval, task, failed-work, and overview read models
- `docs/contracts/tui-overview.md` for review queue contract language

## New Components

No new runtime subsystem is needed.

This design updates documentation only:

- canonical product definition in `CONTEXT.md` and `README.md`
- command-proven implementation rule in `CONTEXT.md` and `README.md`
- `docs/contracts/tui-overview.md` source table updates for `memory_proposal`, failed automation work, and scheduled work requiring approval

## Why New Components Are Necessary

The runtime already has the command path. The gap is contract drift: the product definition and command-proof rule were present in a product brief draft, while the main repo authority docs still used older narrower language. The review queue contract also lagged current implementation by omitting `memory_proposal` and by not explicitly mapping scheduled approval work and failed automations onto existing source types.

Documentation changes are necessary so future implementation agents do not treat plan text, registry prompts, or command help as proof of implementation.

## Locked Domain Decisions

- Canonical product definition: Odin OS is a proven universal inbox, personal/project operating system, agent orchestration layer, governed automation platform, safe prompt-to-production workflow system, and unified review queue without crossing into unsafe autonomous execution.
- No Odin capability counts as implemented unless a real `odin` command invokes it, persists the result, enforces policy where relevant, and emits audit evidence readable through an operator surface.
- Raw Intake Items are durable evidence records and must not automatically create executable Work Items, Run Attempts, branches, PRs, dispatches, approvals, or external mutations.
- `odin intake process --id <id>` is the v1 real Odin path for classification, dedupe, priority, route suggestion, and reviewable draft artifact creation.
- `odin review` is the one review queue. It composes existing source authorities; it does not own a second review table or batch mutation authority.
- Unclear intake and draft ticket/spec/task artifacts surface through `intake_review`.
- Intake that requires explicit approval surfaces through `intake_approval`.
- Existing approvals and scheduled work that has materialized into approval-blocked Work Items surface through `task_approval`.
- Failed automations surface through `failed_work` until a narrower failed-automation source is implemented.
- Memory proposals surface through `memory_proposal`; current queue actions remain unsupported unless a later memory-write resolver is command-proven.

## Selected Design

Treat this slice as a product-definition and contract-locking slice, not a new runtime implementation slice.

Keep current command behavior and make the authority docs match the command-proven runtime:

1. Lock the product definition in `CONTEXT.md` and `README.md`.
2. Lock the command-proven implementation rule in `CONTEXT.md` and `README.md`.
3. Update the review queue contract to include all current governed source families and map the user-requested review categories to existing `source_type` values.
4. Leave runtime behavior unchanged unless a follow-up hardening slice proves a specific uncovered requirement.

## Rejected Alternatives

### Add a second review queue

Rejected. `odin review` is already the canonical queue and has source composition.

### Treat registry classifier/deduper/router prompts as implemented runtime

Rejected. Registry prompts may inform future agent-backed processing, but current v1 proof is the Go `odin intake process` path.

### Create executable work during raw intake or processing

Rejected. Raw intake and processing must stop at reviewable artifacts unless a later explicit review/approval path promotes the item.

## Test And Verification Plan

Focused docs and contract verification:

```bash
go test ./internal/app/lifecycle -run 'TestReviewQueue|TestRunReview|UnifiedReview|TestReviewQueueIncludesAllGovernedDecisionSources' -count=1
go test ./internal/cli/commands ./internal/store/sqlite ./internal/app/lifecycle -run 'Intake|Raw|Process|Review' -count=1
```

Repo-local proof:

```bash
make build
tmp="$(mktemp -d)"
printf '{"original_content":"classify this release-readiness research request without creating executable work"}' | ODIN_ROOT="$tmp" ./bin/odin intake raw create --source codex-cli --title 'Release readiness research request' --type prompt --dedup-key release-readiness-research --requested-by codex --payload-file - --json
ODIN_ROOT="$tmp" ./bin/odin intake raw list --json
ODIN_ROOT="$tmp" ./bin/odin intake raw show intake-1 --json
ODIN_ROOT="$tmp" ./bin/odin intake process --id intake-1 --json
ODIN_ROOT="$tmp" ./bin/odin intake review list --json
ODIN_ROOT="$tmp" ./bin/odin review list --json
ODIN_ROOT="$tmp" ./bin/odin work status --json
rm -rf "$tmp"
```

The follow-up hardening in PR #208 added direct `memory_proposal` coverage and scheduled-approval `task_approval` coverage. Future implementation should focus on a public memory-proposal creation operator path only if durable memory writes become an accepted product requirement.

## Documentation Changes

Changed:

- `CONTEXT.md`
- `README.md`
- `docs/contracts/tui-overview.md`

Added:

- `docs/superpowers/specs/2026-05-11-governed-intake-review-spine-design.md`

No ADR is needed. This records and aligns already-selected product and runtime authority decisions rather than introducing a surprising architecture change.

## Implementation Hardening Status

The first follow-up hardening slice adds direct proof for the review source mappings that were weakly covered during the design pass:

- `TestReviewQueueIncludesAllGovernedDecisionSources` now seeds and requires `memory_proposal`.
- `TestReviewQueueIncludesScheduledApprovalWorkAsTaskApproval` proves scheduled approval work appears as `task_approval` with `work_kind=automation_trigger`.
- `reviewEntryFromPendingApproval` now exposes `task_id`, `task_key`, and `work_kind` from the pending approval projection so scheduled approval work remains visible in the unified queue read model.

Real `odin` proof for scheduled approval work:

```bash
ODIN_ROOT="$tmp" ./bin/odin trigger upsert scheduled-approval-proof initiative=odin-core kind=schedule status=enabled next=2026-05-11T02:00:00Z title=Scheduled_approval_proof summary=scheduled_review intent=governance --json
ODIN_ROOT="$tmp" ./bin/odin trigger evaluate now=2026-05-11T03:00:00Z --json
ODIN_ROOT="$tmp" ./bin/odin review list --json
ODIN_ROOT="$tmp" ./bin/odin work status --json
```

The resulting review item has `source_type=task_approval`, `task_key=automation-scheduled-approval-proof-*`, and `work_kind=automation_trigger`.

## Open Blockers

- The root checkout still has unrelated dirty changes; implementation should continue in isolated worktrees.
- Memory proposal creation still has no public `odin memory ...` creation command. The unified review read path is proven through lifecycle tests that seed `memory_summaries` and then exercise real `odin review list/show/act` command handling.

## Planning Handoff

No additional hardening is required for the source-coverage fixtures in this slice. Future implementation should focus on a public memory-proposal creation operator path only if durable memory writes become an accepted product requirement.

## Implementation Goal Prompt (Completed By PR #208)

```text
/goal Harden governed review queue source proof in /home/orchestrator/odin-os.

Use the approved design at docs/superpowers/specs/2026-05-11-governed-intake-review-spine-design.md. Keep the work PR-sized. Make atomic commits that each leave the repo coherent. Reuse existing odin review, odin intake, internal/app/lifecycle/review_sources.go, runtime projections, SQLite stores, and current source-owned handlers. Do not introduce a second review queue, review table, approval model, or batch mutation path.

Required proof:
- go test ./internal/app/lifecycle -run 'TestReviewQueue|TestRunReview|UnifiedReview|TestReviewQueueIncludesAllGovernedDecisionSources' -count=1
- go test ./internal/cli/commands ./internal/store/sqlite ./internal/app/lifecycle -run 'Intake|Raw|Process|Review' -count=1
- make build
- with a temporary ODIN_ROOT, run ./bin/odin intake raw create, ./bin/odin intake process --id intake-1 --json, ./bin/odin intake review list --json, ./bin/odin review list --json, and ./bin/odin work status

Delivery:
- preserve unrelated dirty worktree changes
- open a PR with Summary, Proven, Unproven, and Commands Run
- monitor checks
- fix failures in follow-up atomic commits
- merge only if checks pass and repo policy permits
```
