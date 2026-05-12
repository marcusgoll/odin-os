# Odin OS Gap Analysis Briefing

Generated: 2026-05-12T15:47:51Z

## Current State

The older gap-analysis material remains useful as an architectural direction check, but some of its negatives are now stale. The current diagnosis separates three evidence layers:

1. Installed live release truth: `/home/orchestrator/.local/bin/odin` -> `/home/orchestrator/odin-os/releases/current/bin/odin`
2. Source-main truth: clean temporary worktree at `/home/orchestrator/odin-os/.worktrees/briefing-audit-main`, HEAD `aa9b24a`, removed after verification
3. Dirty primary checkout state: `/home/orchestrator/odin-os` on `chore-ignore-release-artifacts`, with unrelated tracked and untracked work

Current installed live proof:

- `odin status`: `health=healthy pending_approvals=0 stalled_runs=0 active_runs=0 project_transitions=3 companion_swarms=0 registry_healthy=true worker_dispatch=live dry_run=false read_only=false`
- `odin doctor`: `status=healthy checks=7`
- `odin healthcheck`: `ready`
- `odin review list --json`: 45 items, including 1 intake review item and 44 failed-work items
- `odin work status --json`: emits key-value text in the installed release; reports 106 work items, 27 open work items, 0 active runs, 0 pending approvals, 44 failed retryable work items, 0 delivery profiles, 27 explicit-intent work items, 79 fallback-intent work items
- `odin overview --json`: live older overview lanes, but not the new PR #233 lanes

Current source-main proof:

- PR #233 is merged at `aa9b24a` with successful `go`, `odin-e2e`, push `go`, and GitGuardian checks
- Fresh source-main `./bin/odin overview --json` includes `readiness`, `actual_use`, `delivery_profiles`, `execution_intent`, and `binary_source`
- Fresh source-main `./bin/odin work status --json` emits structured JSON and reports 6 delivery profiles

Interpretation: the product is healthier than the prior current-state briefing suggested, but there is now a clear release/source parity gap.

## What Already Exists

Strong reusable foundations still hold:

- `cmd/odin` and `internal/app/lifecycle` remain the canonical operator and composition center.
- SQLite remains the runtime authority through `internal/store/sqlite`.
- Runtime projections, review, approvals, intake items, triggers, skill artifacts, goals, browser sessions, follow-ups, work items, run attempts, and execution intent all have migrations or runtime surfaces.
- `internal/runtime/jobs`, `internal/runtime/recovery`, `internal/runtime/supervision`, `internal/runtime/triggers`, `internal/runtime/approvals`, and `internal/runtime/projections` are real runtime seams.
- `internal/vcs` has branch, worktree, lease, and git-adapter packages.
- `internal/executors` has the canonical executor seam plus multiple adapters.
- `odin review` is a real unified review queue.
- `odin intake` and overview intake lanes are live.
- `odin trigger` exists, although the current live trigger list is empty.
- Companion/delegation readback exists through overview, even though the current live companion swarm count is 0.
- Capability catalog readback exists in installed and source-main overview surfaces.
- Source main now has an explicit actual-use dashboard projection path across CLI overview, metrics, TUI, and HTTP status code from PR #233.

Gap docs that are stale or need qualification:

- Runtime readiness is no longer a current live gap; `odin healthcheck` now returns `ready`.
- Older notes that `odin knowledge` or `odin memory` are absent are no longer current; installed help includes both.
- Older negative proof that overview intake was `not_yet_wired` is no longer current; overview reports a live intake inbox.
- Older `work status` output of `dispatch=not_implemented intake=manual_read_only` is no longer current; installed `odin work status` reports `dispatch=work_dispatch intake=raw_cli`.
- The brownfield audit's TypeScript scaffold concern is partly stale for source main: `src/`, `package.json`, `package-lock.json`, `tsconfig.json`, and `eslint.config.js` are absent in the clean source-main worktree. The duplicate `configs/` root remains.

## Gaps

### 1. Release/source parity is the highest current dashboard gap

Source main has PR #233's new projection lanes:

- `readiness`
- `actual_use`
- `delivery_profiles`
- `execution_intent`
- `binary_source`

The installed release/current binary does not expose those lanes through `odin overview --json` yet. This is now the main dashboard gap: cut over or rebuild release/current, then prove the installed binary reports the source-main projection contract.

### 2. Installed `work status --json` contract lags source main

The installed command emits key-value text even with `--json`. Source main emits structured JSON. This is an operator/API contract drift and should be fixed through release cutover or an explicit compatibility decision.

### 3. The live queue needs failed-work triage, not another queue

The review queue exists and is live. The current action burden is 44 failed-work items plus 1 intake review item. The gap is recovery/triage policy and operator workflow for accumulated failed work, not a new review authority.

### 4. Delivery profiles are catalog-visible but release/runtime use is not aligned

Source main reports 6 catalog-backed delivery profiles. Installed live work status reports `delivery_profiles=0`. Treat delivery-profile support as source-visible but not installed-release proven until release/current exposes and uses the same count.

### 5. Intake and automation are wired but still thin

There is 1 raw intake item and 1 intake review item. Automation trigger list is empty. The next gap is proving real external or scheduled inputs flowing through intake, review, approval, and work surfaces, without bypassing governance.

### 6. Duplicate or shallow seams remain in source main

The clean source-main worktree still contains these duplicate or shallow paths:

- `configs`
- `internal/config`
- `internal/dashboard`
- `internal/db`
- `internal/logging`
- `internal/orchestrator`
- `internal/prompts`
- `internal/review`
- `internal/runner`
- `internal/security`
- `internal/tracker`
- `internal/utils`

These should not become parallel authorities. Promote useful pieces into existing canonical seams or quarantine/remove them through explicit cleanup slices.

### 7. Real prompt-to-production execution remains incomplete

The architecture gap analysis still holds on the core agency path:

- GitHub issue intake and PR management are not yet a proven end-to-end live workflow.
- Draft PR creation/update is not proven as a runtime module.
- Real Codex subprocess execution remains a security-sensitive boundary and must stay behind the canonical executor contract.
- Review, QA, and security roles exist as vocabulary/tests/scaffolds in places, but they are not yet a proven multi-run delivery chain from intake through handoff.

### 8. Primary checkout is not a reliable implementation base

The primary checkout tracks a gone branch and has unrelated dirty work. This is operational risk for follow-up implementation, not a source architecture gap. Use isolated worktrees for PR-sized slices.

## Reuse Plan

Use the following existing centers of gravity:

- Operator commands: installed `odin` for live release/runtime proof
- Source-main proof: clean `origin/main` worktree plus freshly built `./bin/odin`
- Command composition: `internal/app/lifecycle`
- Runtime authority: `internal/store/sqlite`
- Work execution and governance: `internal/runtime/jobs`, `internal/runtime/approvals`, `internal/runtime/events`, `internal/runtime/runs`
- Review queue: `odin review` and lifecycle review composition
- Intake: `odin intake`, `internal/runtime/triggers`, intake item projections, and task intake evidence
- Execution adapters: `internal/executors`, not `internal/runner`
- Work isolation: `internal/vcs`, not a parallel workspace manager
- Dashboard/API: `internal/api/http` over projections, not a second runtime
- Registry and authored capabilities: `registry/` and `internal/registry`

## New Additions

This update changes only the existing dated gap-analysis briefing:

- `docs/briefings/2026-05-12-odin-os-gap-analysis-briefing.md`

No code, schemas, commands, or runtime state were changed by this briefing update.

## Why New Additions Are Necessary

The previous gap-analysis briefing was based on evidence that has since changed:

- Live runtime readiness is now healthy and ready.
- PR #233 merged source-main dashboard projection improvements.
- Installed release/current has not yet caught up to source-main overview projection shape.

The updated briefing preserves the useful architecture gaps while removing stale negatives and separating live-release truth from source-main truth.

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

- Installed `odin` is healthy and ready.
- Installed `odin overview --json` lacks PR #233's new lanes.
- Source-main `./bin/odin overview --json` includes PR #233's new lanes.
- Source-main `./bin/odin work status --json` emits structured JSON and reports 6 delivery profiles.
- PR #233 is merged at `aa9b24a` with green checks.

## Remaining Risks

- Release/current has not been cut over to source-main projection behavior.
- Installed `work status --json` shape is still not structured JSON.
- Failed-work recovery debt remains high in the live review queue.
- The primary checkout is dirty and stale.
- Codex subprocess execution, GitHub writes, PR creation, worktree mutation, and deployment remain security-sensitive and require explicit review and real operator proof.

## Best Operating Rule Going Forward

For Odin gap work, score only proven behavior against the authority being claimed. Installed `odin` proves live release/runtime state. Freshly built `./bin/odin` from clean `origin/main` proves source-main behavior. A capability is current live truth only after the installed binary invokes it, persists or reads back the relevant state, and exposes the result through an operator surface.
