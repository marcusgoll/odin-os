# Odin OS Current-State Briefing

Generated: 2026-05-12T15:47:51Z

## Current State

Odin OS is an active Go-first control-plane repo with a live operator surface, SQLite-backed runtime state, governed review and approval readbacks, intake surfaces, companion/delegation projections, health/readiness checks, and source-main dashboard projection work that has now landed.

The important current-state distinction is release versus source:

- Installed live binary: `/home/orchestrator/.local/bin/odin` -> `/home/orchestrator/odin-os/releases/current/bin/odin`
- Temporary source-main proof worktree used for audit: `/home/orchestrator/odin-os/.worktrees/briefing-audit-main` (removed after verification)
- Source-main commit: `aa9b24a feat: expand operator dashboard projections (#233)`
- PR #233 state: merged, with `go`, `odin-e2e`, push `go`, and GitGuardian checks all successful

The installed live runtime is currently ready:

- `odin status`: `health=healthy pending_approvals=0 stalled_runs=0 active_runs=0 project_transitions=3 companion_swarms=0 registry_healthy=true worker_dispatch=live dry_run=false read_only=false`
- `odin doctor`: `status=healthy checks=7`
- `odin healthcheck`: `ready`

Interpretation: the earlier degraded/not-ready briefing state is stale. The live runtime is currently healthy and ready, but the installed release still lacks the newest source-main overview lanes from PR #233.

## What Already Exists

The installed live workspace projection is active:

- Workspace: `default`, status `active`, control scope `global`
- Initiatives: 5
- Companions: 1
- Open work items: 1 in installed overview
- Active runs: 0
- Pending approvals: 0
- Blocked work items: 0

The installed capability catalog is catalog-backed:

- Agents: 60
- Skills: 19
- Workflows: 4
- Commands: 2
- Tools: 14
- Authored assets: 99
- Runtime-proven count: 0

The source-main capability catalog is newer:

- Agents: 60
- Skills: 19
- Workflows: 9
- Commands: 2
- Tools: 14
- Authored assets: 104
- Runtime-proven count: 1

The installed review queue is live and currently action-heavy:

- Total review items: 45
- Intake review items: 1
- Pending approvals: 0
- Failed-work retry/recovery items: 44

The installed work status reports:

- Total work items: 106
- Open work items: 27
- Active run attempts: 0
- Pending approvals: 0
- Failed retryable work items: 44
- Delivery profiles: 0
- Raw intake items: 1
- Intake review items: 1
- Dispatch mode: `work_dispatch`
- Intake mode: `raw_cli`
- Explicit intent work items: 27
- Fallback intent work items: 79

The source-main `odin overview --json` now has the PR #233 projection lanes:

- `readiness`: live, `status=ready`, `health_status=healthy`, `ready=true`
- `actual_use`: live, `status=action_required`, `action_required_count=45`
- `delivery_profiles`: catalog-backed, 6 profiles
- `execution_intent`: live, 27 explicit open/actionable work items, 0 fallback open/actionable work items
- `binary_source`: live, aligned repo-local binary/source proof

The installed `odin overview --json` still exposes the older lane set only:

- `workspace`
- `initiatives`
- `work_items`
- `companion_swarms`
- `companions`
- `capability_catalog`
- `capability_truth`
- `skill_activity`
- `review_queue`
- `delegation_truth`
- `approvals`
- `observability`
- `memory`
- `knowledge_context_packs`
- `intake_inbox`
- `automation_triggers`

## Repo And Binary State

Primary checkout:

- Path: `/home/orchestrator/odin-os`
- Branch: `chore-ignore-release-artifacts`
- HEAD: `fb50649 fix: clarify universal intake contract docs`
- Branch tracking target is gone: `origin/chore-ignore-release-artifacts [gone]`
- The checkout already contains unrelated dirty tracked and untracked work.

Clean source-main audit worktree used during diagnosis:

- Path: `/home/orchestrator/odin-os/.worktrees/briefing-audit-main`
- Detached at `origin/main`
- HEAD: `aa9b24a feat: expand operator dashboard projections (#233)`
- Used only for read-only source-main proof and repo-local build checks, then removed during cleanup.

Do not use the primary checkout as a broad implementation base unless explicitly directed. For new code changes, start from `origin/main` in an isolated worktree.

## Gaps

### 1. Installed release and source main are out of projection parity

Source main has the new `readiness`, `actual_use`, `delivery_profiles`, `execution_intent`, and `binary_source` overview lanes. The installed live release does not yet expose those lanes through `odin overview --json`.

### 2. Installed `odin work status --json` is not JSON

The installed command accepts `--json` but emits key-value text. Source main emits structured JSON for the same command. Treat this as installed-release drift until the release binary is cut over.

### 3. The live review queue is dominated by failed-work recovery items

The live queue has 45 items, 44 from failed work and 1 from intake. This is not a missing review surface; it is accumulated action-required runtime state that needs operator triage or explicit archival/recovery policy.

### 4. Delivery profiles are source-visible but not live-runtime populated

Source main reports 6 catalog-backed delivery profiles. Installed live work status still reports `delivery_profiles=0`. The gap is release/runtime population, not the absence of authored profiles in source.

### 5. Intake is wired but not flowing beyond one raw review item

Live state has 1 raw intake item and 1 review-required intake item. Automation triggers remain empty. The next useful proof is not "add another intake surface"; it is proving real inputs move through intake, review, approval, and work without bypassing governance.

### 6. Primary checkout remains unsafe for broad mutation

The primary checkout is on a gone upstream branch and already has unrelated dirty work. Continue using isolated worktrees for implementation slices.

## Reuse Plan

Keep using the existing canonical surfaces:

- Runtime truth: SQLite-backed Odin state
- Live operator readback: installed `odin status`, `odin doctor`, `odin healthcheck`, `odin overview --json`, `odin review list --json`, `odin work status`
- Source-main proof: `./bin/odin` after a fresh build in a clean `origin/main` worktree
- Health/readiness: `odin healthcheck` and `/readyz` own fail-closed readiness; `doctor` explains the evidence
- Review governance: `odin review` and `odin approvals`
- Intake path: `odin intake`
- Dashboard/API: `internal/api/http` over runtime projections, not a second runtime authority

Do not create a second review queue, readiness authority, dashboard authority, intake path, or delegation surface.

## New Additions

This update changes only the existing dated briefing:

- `docs/briefings/2026-05-12-odin-os-current-state.md`

No code, schemas, commands, or runtime state were changed by this briefing update.

## Why New Additions Are Necessary

The prior current-state briefing captured real evidence at the time, but it is now stale in two important ways:

- Live readiness is now healthy and ready.
- Source main now includes PR #233 dashboard projection lanes that are not yet installed-release truth.

This updated briefing prevents over-crediting the installed release for source-only projection improvements while also avoiding the stale claim that the runtime is currently not ready.

## Real odin E2E Verification

Commands run from `/home/orchestrator/odin-os` against the installed live binary:

```bash
which odin
realpath "$(which odin)"
odin help
odin status
odin doctor
odin healthcheck
odin review list --json
odin work status --json
odin overview --json
odin intake raw list --json
odin trigger list --json
```

Commands run from `/home/orchestrator/odin-os/.worktrees/briefing-audit-main` against source main:

```bash
go build -o ./bin/odin ./cmd/odin-os
./bin/odin help
./bin/odin status
./bin/odin healthcheck
./bin/odin work status --json
./bin/odin overview --json
./bin/odin intake raw list --json
./bin/odin trigger list --json
./bin/odin tui --help || true
gh pr view 233 --repo marcusgoll/odin-os --json state,mergedAt,mergeCommit,statusCheckRollup,url
```

Observed proof:

- Installed `odin` resolves through `/home/orchestrator/.local/bin/odin` to `/home/orchestrator/odin-os/releases/current/bin/odin`.
- Installed live runtime reports `health=healthy` and `odin healthcheck` returns `ready`.
- Installed overview lacks PR #233's new overview lanes.
- Source-main overview includes `readiness`, `actual_use`, `delivery_profiles`, `execution_intent`, and `binary_source`.
- PR #233 is merged at `aa9b24a` with green `go`, `odin-e2e`, push `go`, and GitGuardian checks.

## Remaining Risks

- The installed release/current binary has not been cut over to the current source-main projection contract.
- Live work status and source overview use different summarization shapes: source `actual_use` is action-oriented, while `work status` includes broader historical totals.
- The live queue contains substantial failed-work recovery debt.
- The primary checkout is dirty and stale; implementation should start from a clean worktree.

## Best Operating Rule Going Forward

State claims must name their authority. Use installed `odin` for live release/runtime truth. Use freshly built `./bin/odin` from clean `origin/main` for source-main truth. Do not collapse those two until release/current has been rebuilt or cut over and proven through the installed binary.
