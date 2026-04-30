# Alpha Readiness

Odin OS is ready for cautious alpha dogfooding when the checks below stay true in the current repo and runtime root.

Proof expectations follow [docs/contracts/verification-model.md](/home/orchestrator/odin-os/docs/contracts/verification-model.md). Passing internal tests alone is not sufficient evidence for operator-visible behavior.

For agency workflow promotion after the implementation prompt suite is merged, follow [Staged Operational Proving](staged-operational-proving.md) before enabling any live or unattended behavior.

## Resolved blockers

- Fresh runtimes no longer stay degraded by default. Bootstrap now records registry freshness, executor health, and baseline projection freshness so `odin healthcheck` can succeed on a clean `ODIN_ROOT`.
- The executor path is no longer contract-only. `codex_headless` is a live local alpha lane and queued tasks can be executed through the shared router.
- Execution-time safety is now enforced in the runtime path. Task execution checks transition authority, system-project approval gates, and mutable branch/worktree policy before the executor runs.
- Mutable worktree isolation is now mandatory in the runtime mutation path, and the default global worktree root expands `~` correctly.
- `odin serve` now runs bounded background task execution and self-heal loops instead of only exposing passive health endpoints.
- Routing promotions now require a distinct promotion approval step and active routing refinements are consumed at runtime without rewriting canonical config files.
- Structured service logs are newline-delimited JSON records again, which makes long-running log inspection and parsing reliable.

## Alpha checklist

- `make test-alpha` passes.
- `make test` and `make build` pass.
- `odin healthcheck` succeeds on a fresh runtime root.
- `odin doctor --json` returns structured output and shows healthy or honestly degraded state.
- `odin serve` can restart cleanly and produce restart wake packets for interrupted work.
- Backup, verify, and restore succeed against the current runtime root.
- Alpha verification notes clearly distinguish what was proven by real `odin` commands versus what remains unproven.
- `odin-core` stays governed as a system project and mutating work is denied without explicit approval.
- Any external project used in alpha is explicitly registered and kept in `shadow` mode unless an audited transition says otherwise.
- Any project allowed to mutate is in `cutover` or an explicitly allowlisted `limited_action` state.
- Mutating task runs use leased task-owned branches and worktrees.
- Only the `codex_headless` lane is relied on for live execution in this alpha. Other executor adapters remain contract-level or fallback-only until promoted later.

## Explicit deferrals

- Full provider-backed API execution remains deferred.
- System-project mutation approval flow remains manual and explicit; Phase 17 only enforces the gate.
- Broader scheduler behavior and parallel autonomous work remain deferred.
- Richer routing improvement types beyond `routing_rule_refinement` remain audit-only.

## Dogfood recommendation

Use Odin OS alpha in two ways only:

- dogfood the CLI, health, recovery, backup, and transition surfaces on `odin-core`
- onboard one external project in `shadow` mode and confirm observability, migration context, and read-only governance before any cutover

Do not treat this alpha as a general unattended multi-project mutation controller yet.
