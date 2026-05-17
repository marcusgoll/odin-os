# CFIPros CEO Day Routine Runtime Plan

## Goal

Implement the Odin-side routine that lets the CFIPros CEO operator run
throughout the launch day without bypassing CFIPros approval gates.

## Approval Source

The active Codex Goal requests an Odin OS routine for the CFIPros CEO operator
to run throughout the day, with daily cadence checkpoints, inputs, outputs, KPI
readbacks, approval-required actions, stop conditions, run evidence, and proof
through repo-owned Odin commands.

## Existing State

- `config/projects.yaml` already enrolls `cfipros` as a governed GitHub-backed
  managed project with worktree and protected-branch rules.
- `registry/agents/cfipros-ceo-operator-agent.md` already defines the CFIPros
  CEO operator, approval boundaries, launch packet sections, and delegatable
  child review roles.
- The CFIPros repo owns `docs/project/CEO_OPERATOR_LAUNCH_PLAN.md`, including
  the daily habit, weekly packet, KPI scorecard, approval-required artifacts,
  and stop conditions.
- `odin trigger seed marcus-brand-os` proves the current seed pattern for
  throughout-day scheduled work.
- `internal/runtime/triggers.Service` already materializes schedule triggers
  into Work Items with explicit execution intent and skill-invocation artifacts.
- `odin scheduler tick`, `odin trigger test`, `odin trigger audit`,
  `odin jobs`, `odin review`, and `odin skills run <task>` are the existing
  operator proof surfaces.

## Reuse Plan

- Reuse automation triggers and `scheduler-created-workflow` for recurrence.
- Reuse the `cfipros` project manifest for project scope and governance.
- Reuse the CFIPros CEO operator agent as the routine handoff authority.
- Reuse skill-invocation bindings so scheduled work creates reviewable Work
  Items without directly running handler scripts.
- Reuse `odin skills run` and `odin review` for operator-visible evidence.

## New Additions

1. Add `odin trigger seed cfipros-ceo-day-routine`.
2. Add five read-only, review-required CFIPros CEO checkpoints:
   - morning launch health
   - midday acquisition pipeline
   - afternoon revenue readiness
   - evening growth closeout
   - weekly CEO packet
3. Add `registry/workflows/cfipros-ceo-operating-routine.md`.
4. Add `registry/skills/cfipros-ceo-operator.md` plus a restricted handler that
   records the intended `cfipros-ceo-operator-agent` handoff.
5. Add command tests proving seed, schedule metadata, approval boundaries, and
   trigger materialization.
6. Add an operations runbook with the exact install, proof, run, and stop paths.

## Runtime Boundaries

- The routine may create internal read-only CEO review Work Items.
- The routine may draft, prioritize, and prepare approval requests.
- Customer contact, publishing, ad spend, pricing, Stripe, billing,
  entitlement, production deploy, and merge actions require explicit human
  approval and normal CFIPros workflow gates.
- Missing KPI values must be reported as `unmeasured`.
- The slice must not add another daemon, scheduler, queue, approval table, or
  external-worker path.

## Verification

- `go test ./internal/cli/commands ./internal/runtime/triggers ./internal/app/lifecycle ./internal/registry/...`
- `make build`
- `which odin`
- `ODIN_ROOT=<tmp> ./bin/odin trigger seed cfipros-ceo-day-routine start=2026-05-18 --json`
- `ODIN_ROOT=<tmp> ./bin/odin trigger test cfipros-ceo-morning-launch-health now=2026-05-18T12:30:00Z --json`
- `ODIN_ROOT=<tmp> ./bin/odin scheduler tick now=2026-05-18T12:30:00Z recovery=false --json`
- `ODIN_ROOT=<tmp> ./bin/odin jobs --json`
- `ODIN_ROOT=<tmp> ./bin/odin skills run <task-key> --json`
