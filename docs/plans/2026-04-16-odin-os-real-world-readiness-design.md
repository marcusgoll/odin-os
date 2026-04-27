# Odin OS Real-World Readiness Design

## Problem

`odin-os` is structurally solid, but several operator-facing surfaces still present scaffolding as if it were live capability. The biggest gaps are a fake `codex_headless` execution lane, health/readiness that can report healthy from synthetic state, mutable worktree cleanup that is only partial, and live driver scripts that still depend on a sibling legacy repo.

For bounded real-world work, the next step is not more surface area. The next step is making one lane truthful end to end and removing any operator-facing claims that exceed that truth.

## Approaches

### Option 1: Documentation-only honesty

Downgrade the README, alpha docs, and acceptance framing without changing runtime behavior.

Pros:
- Lowest effort
- Removes misleading claims quickly

Cons:
- `odin-os` still does not perform real durable work
- Operators still hit fake ask/act behavior
- Does not improve trust in the runtime itself

Verdict: reject as the primary fix. Do this only as a side effect of runtime changes.

### Option 2: One truthful real lane first

Replace the fake `codex_headless` adapter with a driver-backed lane, make health depend on actual executor availability, clean up mutable worktrees deterministically, and make live driver scripts self-contained.

Pros:
- Smallest change set that makes the system operationally honest
- Preserves the current SQLite, manifest, transition, and worktree architecture
- Keeps alpha scope bounded to one real executor lane

Cons:
- Requires contract and test updates across bootstrap, health, jobs, and docs
- Leaves broader multi-provider execution deferred

Verdict: recommended.

### Option 3: Full productionization now

Implement several providers, richer planner/tool orchestration, and unattended multi-project scheduling in the same push.

Pros:
- Broader end-state coverage

Cons:
- Too much moving scope for the current trust gap
- Makes it harder to tell whether failures are from composition or new features
- Violates the repo's own alpha posture

Verdict: reject for now.

## Selected Design

### 1. Real execution truth

`codex_headless` stops being an in-process formatter and becomes a driver-backed executor. Health is `unavailable` when no driver is configured or the probe fails. Capabilities only claim what the driver actually supports. Ask mode and durable Act runs both flow through the same truthful executor contract.

### 2. Truthful readiness

Fresh bootstrap must record executor state based on configured executors, not on whatever adapters happen to return `healthy`. `doctor` and `healthcheck` must evaluate expected executors explicitly so a fresh runtime without a usable lane cannot report `healthy` or `ready`.

### 3. Deterministic mutable-work cleanup

Mutable runs already acquire task-owned branches and worktrees. The missing piece is cleanup. Run completion and failure paths should release the lease and remove the worktree immediately, while `serve` keeps a bounded cleanup loop for stale or crash-left leases.

### 4. Deployment independence

Repo-local live driver scripts must work without a mandatory checkout of `odin-orchestrator`. Any reused shell behavior needed by the live drivers should live under `odin-os/scripts/drivers/lib/` with explicit env overrides preserved only as optional compatibility.

### 5. Operator-visible honesty

Placeholder operator tools and misleading docs should either become real or stop being presented as live capability. The default catalog and alpha docs should only expose what the runtime can actually do.

## Non-Goals

- Multi-provider API execution
- Broad unattended multi-project mutation
- Rich planner-driven tool orchestration
- Resume/cancel support unless the real driver actually implements it

## Success Criteria

- Fresh runtime without a configured Codex driver reports degraded or unavailable executor health and fails `healthcheck`.
- Fresh runtime with a configured driver can answer one ask prompt and complete one durable Act run without returning stub marker text.
- Mutable runs leave no orphaned active worktree lease or worktree directory on normal completion or failure.
- Live Google Calendar and Huginn driver scripts no longer require `/home/orchestrator/odin-orchestrator`.
- README, alpha docs, and acceptance tests describe only composed runtime behavior.
