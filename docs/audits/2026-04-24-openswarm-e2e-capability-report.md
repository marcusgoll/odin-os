# OpenSwarm Capability E2E Report

**Date:** 2026-04-24
**Odin baseline:** `origin/main` at `59f497a` (`[codex] Add OpenSwarm-inspired approval operator surface (#28)`)
**OpenSwarm source:** <https://github.com/openswarm-ai/openswarm>, README reviewed 2026-04-24
**Purpose:** Compare Odin's current, proven operator capabilities with OpenSwarm's published workbench features, separating evidence-backed behavior from remaining gaps.

## Summary

Odin should not copy OpenSwarm's agent-card dashboard model. OpenSwarm is centered on a local desktop workbench for launching, monitoring, and approving many coding agents from a spatial UI. Odin is centered on governed runtime authority: workspace, initiative, work item, run attempt, approvals, memory, policy, and durable SQLite-backed state.

The useful OpenSwarm lesson now implemented in Odin is the operator-surface slice: a single overview board, visible approval handles, linked run evidence, resolver support, and safe human approval actions. This report only marks behavior as proven when a real `./bin/odin` command or targeted test demonstrated it.

## Evidence Captured

- `/tmp/odin-openswarm-e2e-tui.txt`
  - Real `./bin/odin repl` with seeded unsupported approval.
  - Proved `/overview`, `/approvals`, `/approvals show`, unsupported `/approvals resolve`, and JSON unsupported refusal.
- `/tmp/odin-openswarm-supported-approval-e2e.txt`
  - Real `./bin/odin repl` with deterministic Robinhood transfer driver.
  - Proved supported approval display, approval resolution, submit continuation run, artifact recording, and cleared approval lane.
- `/tmp/odin-openswarm-supported-json-e2e.txt`
  - Real `./bin/odin repl` for prepare plus top-level `./bin/odin approvals resolve --json`.
  - Proved supported machine-readable approval result with `resolver_support=supported`, `result=approved`, and `submit_run_id`.

## Commands Proven

```bash
go build -o ./bin/odin ./cmd/odin
go test ./internal/runtime/approvals ./internal/cli/overview ./internal/cli/render ./internal/cli/repl -run 'Test(DetailMarksUnsupportedApproval|ResolveApproveUnsupportedApprovalLeavesPending|ResolveDenyUnsupportedApprovalLeavesPending|BuildReturnsCanonicalOverviewFromCurrentAuthority|RenderOverviewUsesCanonicalLanes|ShellOverviewRendersCanonicalBoard|ShellApprovalsListsHandlesAndResolverSupport|ShellApprovalsShowIncludesEvidencePointerAndResolverSupport|ShellApprovalsResolveUnsupportedApproveDoesNotMutate|ShellApprovalsResolveUnsupportedDenyDoesNotMutate)' -v
go test ./tests/integration -run 'Test.*Overview' -v
go test ./internal/runtime/approvals ./internal/cli/repl -run 'Test(ResolveApprovePreparedTransferStartsSubmitContinuation|ShellTransferApproveShowsSubmitContinuationRun|ShellTransferPreparePrintsReceiptAndCreatesApprovalWait)' -v
go test ./internal/app/lifecycle ./internal/runtime/approvals ./internal/cli/repl -run 'Test(RunApprovalsResolveUnsupported|ResolveApprovePreparedTransferStartsSubmitContinuation|ShellTransferApproveShowsSubmitContinuationRun|ShellTransferPreparePrintsReceiptAndCreatesApprovalWait)' -v
```

Full `go test ./...` was also run once after the unsupported proof. It failed only in `TestMediaStackAcceptance/doctor_and_healthcheck_respect_media_fixtures` with `serve never became ready`; that exact subtest passed on immediate rerun. Treat that as an unrelated integration flake, not OpenSwarm-slice evidence.

## Capability Matrix

| OpenSwarm feature area | Odin current status | Evidence / gap |
| --- | --- | --- |
| One-screen workbench / dashboard | **Partially proven** | `/overview` renders canonical `Workspace`, `Attention`, `Active Execution`, `Initiatives`, `Work Items`, `Run Attempts`, `Companions`, `Capability Catalog`, `Approvals`, `Observability`, `Memory`, `Intake Inbox`, and `Automation Triggers`. It is textual/TUI-first, not spatial drag-and-drop. |
| Unified human approvals | **Proven for Odin approvals** | `/overview`, `/approvals`, and `/approvals show <id>` expose approval id, task/work item, run id, status, resolver support, and evidence pointer. Unsupported approvals refuse mutation and stay pending. |
| Supported approval continuation | **Proven for prepared transfer workflow** | `/transfer prepare` produced `resolver=supported`; `/approvals resolve 1 approve because final confirmation` produced `run=2`; `/runs show active` showed `executor=robinhood_transfer_submit` and completed driver artifact. |
| Machine-readable approval API | **Proven** | Top-level `odin approvals resolve --json` returned `status=approved`, `resolver_support=supported`, `result=approved`, and `submit_run_id=2` for supported approval. Unsupported JSON returns `status=pending`, `resolver_support=unsupported`, and `result=not_resolved`. |
| Approval batch actions / keyboard shortcuts | **Not implemented** | Odin has explicit command receipts. No batch approve/deny or shortcut layer is proven. This is intentionally deferred because Odin requires resolver ownership before mutation. |
| Spatial canvas with agent/view/browser cards | **Not implemented** | Odin uses canonical operator lanes. No draggable dashboard cards or embedded browser cards are proven. |
| Parallel agent launch and monitor | **Partially present outside this slice** | Odin has work item/run/delegation primitives, but this report did not re-prove parallel agent dashboard parity. Do not claim OpenSwarm-style card-level parallel dashboard parity from this evidence. |
| Git worktree isolation | **Present as project/operator primitive, not re-proven here** | The E2E work used Git worktrees for clean verification, but the report does not prove a user-facing multi-agent worktree dashboard. |
| Diff viewer | **Not proven** | No Odin TUI diff viewer was exercised. |
| Conversation branching / resume | **Not proven here** | Odin has runtime conversation/checkpoint packages, but no OpenSwarm-style branch navigation was tested in this slice. |
| Prompt templates / skills library | **Partially present, not E2E-proven here** | Odin has registry skills/workflows and commands. This report did not prove interactive template authoring or marketplace-style install flows. |
| MCP/tools library discovery | **Partially present, not parity** | `/overview` shows `Capability Catalog` counts and Odin has tool catalog packages. OpenSwarm-style MCP registry browsing and OAuth tool setup were not tested. |
| Views and generated outputs | **Not proven** | Odin's current `/overview` is textual. No iframe artifact or generated view surface was tested. |
| Cost tracking | **Not proven** | No per-session spend accounting was tested. |
| Dark/light themes | **Not applicable to current Odin TUI** | Current proof is text output. |

## Proven TUI Snapshot

Unsupported approval snapshot:

```text
Attention
  approvals=1 incidents=0 blocked_work=1 recoveries=0 blocked_swarms=0
  approval=1 work_item=manual-approval-review project=alpha companion=primary run=1 status=pending resolver=unsupported
  blocked work_item=manual-approval-review project=alpha companion=primary source=approval reason=pending

Approvals
  approval=1 work_item=manual-approval-review project=alpha companion=primary run=1 status=pending resolver=unsupported
```

Supported approval snapshot:

```text
Approvals
  approval=1 work_item=robinhood-transfer-20260424-222340 project=pbs companion=none run=1 status=pending resolver=supported

odin> /approvals resolve 1 approve because final confirmation
approval=1 status=resolved result=approved run=2
summary=approval granted; submit continuation started
```

Supported JSON snapshot:

```json
{
  "id": 1,
  "status": "approved",
  "decision_by": "operator",
  "reason": "final confirmation",
  "resolver_support": "supported",
  "result": "approved",
  "submit_run_id": 2,
  "summary": "approval granted; submit continuation started"
}
```

## Current Operating Boundary

- Odin's approval mutation boundary is workflow-owned resolver support.
- Unsupported approvals are visible, inspectable, and immutable through the operator resolve path.
- Supported approval continuation is proven for prepared Robinhood transfer with a deterministic driver.
- Live Robinhood execution is intentionally not proven by this report and requires explicit attended finance approval.
- OpenSwarm's spatial UI and broad desktop-workbench features remain product gaps, not hidden Odin capabilities.

## Recommended Next Slices

1. Add a compact `/overview --json` or top-level `odin overview --json` read-only surface for machine inspection.
2. Add batch-safe approval listing filters such as supported-only and unsupported-only, without batch mutation.
3. Decide whether Odin needs a visual dashboard adapter or whether the canonical TUI remains the operator surface.
4. Prove existing delegation/worktree/run primitives against OpenSwarm's parallel-agent monitor claims in a separate E2E report.
