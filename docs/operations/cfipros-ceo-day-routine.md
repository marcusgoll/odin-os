# CFIPros CEO Day Routine Operations

Use this runbook to install, prove, and operate the Odin-side CFIPros CEO
routine.

## Current State

Supported runtime path: `odin trigger seed cfipros-ceo-day-routine`,
`odin scheduler tick`, `odin jobs`, `odin skills run <task>`, the
`cfipros-ceo-operating-routine` workflow, and the existing CFIPros CEO operator
agent.

CFIPros owns the product, launch, pricing, metrics, beta, and CEO launch docs.
Odin owns the recurring runtime and approval-safe work materialization.

## Install Routine Triggers

From `/home/orchestrator/odin-os` after building or installing the current
`odin` binary:

```bash
./bin/odin trigger seed cfipros-ceo-day-routine start=2026-05-18 --json
```

Default behavior:

- project/initiative: `cfipros`
- workspace: `default`
- local schedule timezone: `America/New_York`
- quiet-hours timezone: `UTC` because the current trigger evaluator only
  supports UTC quiet windows
- status: `enabled`
- execution intent: `read_only`

The seed creates recurring CFIPros CEO checkpoints for:

- morning launch health
- midday acquisition pipeline
- afternoon revenue readiness
- evening growth closeout
- weekly CEO packet

## Prove Before Running Live

Use dry-run and scheduler proof before relying on the loop:

```bash
./bin/odin trigger test cfipros-ceo-morning-launch-health now=2026-05-18T12:30:00Z --json
./bin/odin scheduler tick now=2026-05-18T12:30:00Z recovery=false --dry-run --json
```

When ready to materialize due work:

```bash
./bin/odin scheduler tick now=2026-05-18T12:30:00Z recovery=false --json
./bin/odin jobs --json
```

Scheduled CEO work appears as `work_kind=skill_invocation`. Run an accepted item
through:

```bash
./bin/odin skills run <task-key> --json
```

## Approval Boundaries

The CFIPros CEO routine may draft, prioritize, review, package, and recommend.
It must not contact customers, send outreach, publish, buy ads, change prices,
change Stripe or billing, deploy, merge, or represent CFIPros externally.

Every external artifact must retain:

```text
Approval required before use: yes
External side effect: <email|post|ad|call|pricing|billing|deploy|none>
Approved by: pending
```

## Operating Check

Daily operator check:

```bash
./bin/odin trigger list --json
./bin/odin scheduler tick --dry-run --json
./bin/odin jobs --json
./bin/odin review list --json
```

Stop if duplicate CFIPros CEO trigger keys exist, if any routine tries to take
external action without approval, if KPI values are invented instead of marked
`unmeasured`, or if CFIPros docs and implementation disagree on pricing,
billing, entitlement, tenant safety, or launch claims.
