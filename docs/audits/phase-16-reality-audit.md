# Phase 16 Reality Audit

Date: 2026-04-09

## Verdict

The repository is structurally coherent and well-tested at the package level, but it is not yet a fully trustworthy alpha in the operational sense claimed by the current acceptance framing.

The strongest parts are the authored contracts, SQLite event store, manifest validation, registry parsing, and the library-level implementations for compaction, worktrees, recovery, transition state, and self-improvement records.

The main drift is composition drift: several later-phase systems exist as isolated libraries and tests, but are not wired into one runtime execution path. The biggest examples are executor routing, dynamic tool use, transition enforcement, self-heal scheduling, runtime application of promoted improvements, and health/readiness bootstrapping.

## Evidence Summary

### Runtime spot checks

- `ODIN_ROOT=$(mktemp -d) go run ./cmd/odin doctor --json`
  result: fresh runtime starts `degraded`
  checks missing by default: `executor`, `projections`, `source_freshness`
- `ODIN_ROOT=$(mktemp -d) go run ./cmd/odin healthcheck`
  result: exits non-zero with `not ready: degraded`
- `ODIN_ROOT=$(mktemp -d) go run ./cmd/odin` with `/project odin-core`, `/mode act`, and a task line
  result: the CLI works and queues a task, but health remains degraded and no run/executor path is started

### Architecture-level findings

- No meaningful legacy-runtime overlap was found in the new repo. Legacy material is isolated under `docs/migration/` and `state/migration/`.
- Provider-specific coupling is mostly contained to `internal/executors/*` and config files. Core packages do not directly depend on provider-specific adapters.
- Shared mutable workspace safety is only partially real: the worktree/lease manager exists, but no runtime execution path currently forces mutating work through it.
- Compaction is structurally real, not transcript stuffing, but trigger coverage and execution-loop integration are incomplete.
- `odin-core` governance is defined and validated, but execution-time enforcement is not yet in place.

## Phase Assessments

### Phase 00

Status: `complete`

Satisfies the phase:

- [0001-canonical-authority.md](/home/orchestrator/odin-os/docs/adr/0001-canonical-authority.md)
- [0002-migration-policy.md](/home/orchestrator/odin-os/docs/adr/0002-migration-policy.md)
- [repo-layout.md](/home/orchestrator/odin-os/docs/contracts/repo-layout.md)
- [phase-exit-criteria.md](/home/orchestrator/odin-os/docs/contracts/phase-exit-criteria.md)
- [README.md](/home/orchestrator/odin-os/README.md)

Gaps:

- No automated contract linting or policy conformance checks enforce the ADRs.

Incorrect implementations:

- None found.

Risky shortcuts:

- The architecture authority is doc-first only. Later phases can drift unless stabilized by tests or startup checks.

Test coverage quality:

- Light. This phase is documentation-backed, not behavior-backed.

### Phase 01

Status: `complete`

Satisfies the phase:

- [main.go](/home/orchestrator/odin-os/cmd/odin/main.go)
- [run.go](/home/orchestrator/odin-os/internal/app/lifecycle/run.go)
- [Makefile](/home/orchestrator/odin-os/Makefile)
- [ci.yml](/home/orchestrator/odin-os/.github/workflows/ci.yml)
- repo scaffold across `cmd/`, `internal/`, `registry/`, `prompts/`, `memory/`, `config/`, `docs/`, `scripts/`, and `tests/`

Gaps:

- Many packages remain scaffold-only, but that is consistent with the phase intent.

Incorrect implementations:

- None found.

Risky shortcuts:

- The scaffold intentionally front-loaded many empty packages. That is acceptable, but later-phase claims should not treat those directories as implemented systems.

Test coverage quality:

- Adequate for a scaffold phase. Build, test, and CI surfaces exist.

### Phase 02

Status: `mostly_complete`

Satisfies the phase:

- [registry-format.md](/home/orchestrator/odin-os/docs/contracts/registry-format.md)
- [types.go](/home/orchestrator/odin-os/internal/registry/types.go)
- [parse.go](/home/orchestrator/odin-os/internal/registry/parser/parse.go)
- [validate.go](/home/orchestrator/odin-os/internal/registry/validator/validate.go)
- [compile.go](/home/orchestrator/odin-os/internal/registry/compiler/compile.go)
- [load.go](/home/orchestrator/odin-os/internal/registry/loader/load.go)
- example assets under [registry/](/home/orchestrator/odin-os/registry)

Gaps:

- The watcher is only a future-facing stub in [watcher.go](/home/orchestrator/odin-os/internal/registry/watcher/watcher.go).
- Runtime bootstrap does not load a compiled registry snapshot; it only loads project manifests in [bootstrap.go](/home/orchestrator/odin-os/internal/app/bootstrap/bootstrap.go).

Incorrect implementations:

- None in the parser/validator/compiler path.

Risky shortcuts:

- The Markdown registry is real as a library, but not a runtime-initialized authority yet.

Test coverage quality:

- Strong for parse/validate/load success and failure cases.

### Phase 03

Status: `complete`

Satisfies the phase:

- [0001_runtime.sql](/home/orchestrator/odin-os/internal/store/sqlite/migrations/0001_runtime.sql)
- [migrations.go](/home/orchestrator/odin-os/internal/store/sqlite/migrations.go)
- [store.go](/home/orchestrator/odin-os/internal/store/sqlite/store.go)
- [models.go](/home/orchestrator/odin-os/internal/store/sqlite/models.go)
- [events.go](/home/orchestrator/odin-os/internal/runtime/events/events.go)
- [projections.go](/home/orchestrator/odin-os/internal/runtime/projections/projections.go)
- [runtime-events.md](/home/orchestrator/odin-os/docs/contracts/runtime-events.md)

Gaps:

- None material for the stated phase.

Incorrect implementations:

- None found in the store/event/projection baseline.

Risky shortcuts:

- Projections are query helpers, not separately refreshed materializations. That is consistent with the current design.

Test coverage quality:

- Strong. Migrations, lifecycle persistence, and event-backed reads are well covered.

### Phase 04

Status: `mostly_complete`

Satisfies the phase:

- [project-manifest.md](/home/orchestrator/odin-os/docs/contracts/project-manifest.md)
- [cli-scope.md](/home/orchestrator/odin-os/docs/contracts/cli-scope.md)
- [manifest.go](/home/orchestrator/odin-os/internal/core/projects/manifest.go)
- [validate.go](/home/orchestrator/odin-os/internal/core/projects/validate.go)
- [register.go](/home/orchestrator/odin-os/internal/core/projects/register.go)
- [scope.go](/home/orchestrator/odin-os/internal/cli/scope/scope.go)

Gaps:

- Policy fields are validated, but not enforced during runtime execution.
- `odin-core` special rules are checked in manifest validation only.
- The shipped manifest in [projects.yaml](/home/orchestrator/odin-os/config/projects.yaml) contains only `odin-core`; local and GitHub-backed project classes are exercised in tests, not in the live config.

Incorrect implementations:

- None in schema parsing or validation.

Risky shortcuts:

- Runtime behavior can still bypass governance because branch rules, merge rules, approval gates, and destructive-operation rules are not consumed outside validation.

Test coverage quality:

- Good for manifest parsing and validation.
- Weak for execution-time governance because no such enforcement path exists.

### Phase 05

Status: `mostly_complete`

Satisfies the phase:

- [shell.go](/home/orchestrator/odin-os/internal/cli/repl/shell.go)
- [session.go](/home/orchestrator/odin-os/internal/cli/repl/session.go)
- [commands.go](/home/orchestrator/odin-os/internal/cli/commands/commands.go)
- [header.go](/home/orchestrator/odin-os/internal/cli/render/header.go)
- [service.go](/home/orchestrator/odin-os/internal/runtime/jobs/service.go)
- [service.go](/home/orchestrator/odin-os/internal/runtime/runs/service.go)
- [cli-session.md](/home/orchestrator/odin-os/docs/contracts/cli-session.md)

Gaps:

- Ask mode is a keyword router, not a richer local operational responder.
- Act mode queues tasks only. It does not start runs or route to executors.
- `/logs` is event-listing only; it does not expose structured log output.
- `/approvals` lists only pending approvals; there is no CLI approval action flow.

Incorrect implementations:

- None material inside the REPL loop.

Risky shortcuts:

- Fresh-shell health is usually degraded because runtime freshness tables are not bootstrapped automatically.

Test coverage quality:

- Good for mode/scope/session/header behavior.
- Moderate for operator realism because the shell is tested against seeded state.

### Phase 06

Status: `partial`

Satisfies the phase:

- [types.go](/home/orchestrator/odin-os/internal/executors/contract/types.go)
- [config.go](/home/orchestrator/odin-os/internal/executors/router/config.go)
- [router.go](/home/orchestrator/odin-os/internal/executors/router/router.go)
- adapter skeletons under [internal/executors/](/home/orchestrator/odin-os/internal/executors)
- [executors.yaml](/home/orchestrator/odin-os/config/executors.yaml)
- [models.yaml](/home/orchestrator/odin-os/config/models.yaml)

Gaps:

- All adapter execution methods return `ErrNotImplemented` in [types.go](/home/orchestrator/odin-os/internal/executors/contract/types.go).
- The runtime never loads `config/executors.yaml` or calls the selector outside tests.
- `config/models.yaml` is authored but unused by runtime code.
- No task path calls `RunTask`, `ResumeTask`, `CancelTask`, or `EstimateCost`.

Incorrect implementations:

- The acceptance suite proves route selection only, not portable task execution.

Risky shortcuts:

- The executor layer looks present in the repo structure, but it is not an operating lane yet.

Test coverage quality:

- Strong for route matching and fallback logic.
- Missing for end-to-end execution behavior because there is none.

### Phase 07

Status: `partial`

Satisfies the phase:

- [types.go](/home/orchestrator/odin-os/internal/tools/catalog/types.go)
- [builtin.go](/home/orchestrator/odin-os/internal/tools/catalog/builtin.go)
- [broker.go](/home/orchestrator/odin-os/internal/tools/broker/broker.go)
- [budgets.go](/home/orchestrator/odin-os/internal/tools/budgets/budgets.go)
- [service.go](/home/orchestrator/odin-os/internal/workers/planner/service.go)

Gaps:

- Built-in tools return canned summaries like `project=... status=ready` and `events=0` in [builtin.go](/home/orchestrator/odin-os/internal/tools/catalog/builtin.go); they do not invoke runtime-backed tools.
- The broker is not loaded by bootstrap, CLI, or executor code.
- Sub-agent expansion exists as a data structure only. No spawn path is wired.
- Full-definition expansion is only exercised in broker/planner tests.

Incorrect implementations:

- The system currently demonstrates thin-card mechanics, not real on-demand tool execution.

Risky shortcuts:

- The acceptance suite treats catalog expansion plus canned built-ins as proof that dynamic tool access is working operationally.

Test coverage quality:

- Good for selection, expansion, and budget denial.
- Weak for actual runtime usefulness.

### Phase 08

Status: `partial`

Satisfies the phase:

- [context-compaction.md](/home/orchestrator/odin-os/docs/contracts/context-compaction.md)
- [types.go](/home/orchestrator/odin-os/internal/runtime/checkpoints/types.go)
- [service.go](/home/orchestrator/odin-os/internal/runtime/checkpoints/service.go)
- [0002_context_packets_envelope.sql](/home/orchestrator/odin-os/internal/store/sqlite/migrations/0002_context_packets_envelope.sql)

Gaps:

- Only `approval_wait` and `restart` have real callers.
- `handoff`, `model_switch`, `token_threshold`, `idle_pause`, and `completion` exist as enums but are not wired into runtime behavior.
- There is no general executor/task loop that automatically checkpoints before handoff or model changes.

Incorrect implementations:

- None in packet persistence or resume loading.

Risky shortcuts:

- The packet format is real, but trigger coverage is incomplete enough that “wake packets work” is true only for narrow flows.

Test coverage quality:

- Strong for packet persistence, supersession, and resume loading.
- Weak for trigger completeness.

### Phase 09

Status: `mostly_complete`

Satisfies the phase:

- [git-worktrees.md](/home/orchestrator/odin-os/docs/contracts/git-worktrees.md)
- [naming.go](/home/orchestrator/odin-os/internal/vcs/branches/naming.go)
- [paths.go](/home/orchestrator/odin-os/internal/vcs/worktrees/paths.go)
- [adapter.go](/home/orchestrator/odin-os/internal/vcs/git/adapter.go)
- [manager.go](/home/orchestrator/odin-os/internal/vcs/leases/manager.go)
- [manager.go](/home/orchestrator/odin-os/internal/vcs/worktrees/manager.go)
- [0003_worktree_leases.sql](/home/orchestrator/odin-os/internal/store/sqlite/migrations/0003_worktree_leases.sql)

Gaps:

- No runtime execution path acquires a mutable lease before mutating work.
- Branch/worktree isolation is therefore not enforced end-to-end.

Incorrect implementations:

- [paths.go](/home/orchestrator/odin-os/internal/vcs/worktrees/paths.go) uses a literal `~/.config/superpowers/worktrees/odin-os` default. `~` is not expanded by `filepath.Join` or `exec.Command`, so the default path is wrong at runtime.

Risky shortcuts:

- The worktree model is a library, not yet the mandatory mutation entrypoint.

Test coverage quality:

- Good for naming, leasing, conflicts, and cleanup.
- Missing for actual runtime ownership enforcement.

### Phase 10

Status: `partial`

Satisfies the phase:

- [observability.md](/home/orchestrator/odin-os/docs/contracts/observability.md)
- [service.go](/home/orchestrator/odin-os/internal/runtime/health/service.go)
- [service.go](/home/orchestrator/odin-os/internal/telemetry/metrics/service.go)
- [logger.go](/home/orchestrator/odin-os/internal/telemetry/logs/logger.go)
- [operational.go](/home/orchestrator/odin-os/internal/api/http/operational.go)
- operator projections in [projections.go](/home/orchestrator/odin-os/internal/runtime/projections/projections.go)

Gaps:

- Fresh runtimes stay degraded because nothing automatically records `registry_versions`, `executor_health`, or `projection_freshness`.
- [telemetry.yaml](/home/orchestrator/odin-os/config/telemetry.yaml) is not loaded anywhere.
- Structured logging is not initialized in bootstrap or serve mode.
- `runs/logs/` is never written despite the config contract implying it.

Incorrect implementations:

- [logger.go](/home/orchestrator/odin-os/internal/telemetry/logs/logger.go) writes JSON without a newline, which is wrong for appendable structured logs.

Risky shortcuts:

- Doctor and metrics are useful only after tests or manual code seed the relevant rows.
- The acceptance suite seeds healthy observability directly instead of proving runtime production of those signals.

Test coverage quality:

- Good for service-level health and metrics logic.
- Weak for operational reality because the data production path is mostly absent.

### Phase 11

Status: `partial`

Satisfies the phase:

- [self-heal.md](/home/orchestrator/odin-os/docs/contracts/self-heal.md)
- [monitors.go](/home/orchestrator/odin-os/internal/runtime/recovery/monitors.go)
- [diagnosis.go](/home/orchestrator/odin-os/internal/runtime/recovery/diagnosis.go)
- [builtin.go](/home/orchestrator/odin-os/internal/runtime/recovery/builtin.go)
- [executor.go](/home/orchestrator/odin-os/internal/runtime/recovery/executor.go)
- [service.go](/home/orchestrator/odin-os/internal/runtime/recovery/service.go)

Gaps:

- `RunCycle` is never scheduled or invoked by `odin serve`.
- Only startup recovery is wired into runtime in [run.go](/home/orchestrator/odin-os/internal/app/lifecycle/run.go).
- Several playbooks refresh bookkeeping surfaces rather than repairing live execution lanes.

Incorrect implementations:

- None in the bounded retry/cooldown/escalation mechanics.

Risky shortcuts:

- Self-heal is real as a library, but not yet a running subsystem.

Test coverage quality:

- Strong for monitors, diagnoser, executor, and startup recovery.
- Missing for long-running operational execution.

### Phase 12

Status: `mostly_complete`

Satisfies the phase:

- [service.go](/home/orchestrator/odin-os/internal/migration/extractor/service.go)
- [scan.go](/home/orchestrator/odin-os/internal/migration/extractor/scan.go)
- [duplicates.go](/home/orchestrator/odin-os/internal/migration/extractor/duplicates.go)
- [classify.go](/home/orchestrator/odin-os/internal/migration/extractor/classify.go)
- [drafts.go](/home/orchestrator/odin-os/internal/migration/extractor/drafts.go)
- [extract-odin-orchestrator.go](/home/orchestrator/odin-os/scripts/migrate/extract-odin-orchestrator.go)
- generated reports in [docs/migration/](/home/orchestrator/odin-os/docs/migration)

Gaps:

- `migrate_as_is` exists as a classification constant but [classify.go](/home/orchestrator/odin-os/internal/migration/extractor/classify.go) never returns it.
- The inventory is generated successfully, but the classification policy is more conservative than the contract implies.

Incorrect implementations:

- None found in scanning or duplicate grouping.

Risky shortcuts:

- The extractor is likely to overclassify toward `rewrite` and `archive`.

Test coverage quality:

- Good for scan/classify/duplicate/draft behavior.

### Phase 13

Status: `partial`

Satisfies the phase:

- [project-transition.md](/home/orchestrator/odin-os/docs/contracts/project-transition.md)
- [transition.go](/home/orchestrator/odin-os/internal/core/projects/transition.go)
- [service.go](/home/orchestrator/odin-os/internal/core/projects/service.go)
- [0005_project_transitions.sql](/home/orchestrator/odin-os/internal/store/sqlite/migrations/0005_project_transitions.sql)
- transition projections in [projections.go](/home/orchestrator/odin-os/internal/runtime/projections/projections.go)

Gaps:

- There is no operator surface to set or inspect transition state beyond tests and projections.
- No mutating path calls `Service.AuthorizeAction`.
- Shadow observations and compare reports can be recorded, but no runtime observation path produces them.

Incorrect implementations:

- None in the state machine or persistence path.

Risky shortcuts:

- Transition safety exists on paper and in tests, but not at the execution boundary where it matters.

Test coverage quality:

- Good for transition state, reports, and gate decisions.
- Missing for runtime enforcement.

### Phase 14

Status: `partial`

Satisfies the phase:

- [self-improvement.md](/home/orchestrator/odin-os/docs/contracts/self-improvement.md)
- [service.go](/home/orchestrator/odin-os/internal/learning/proposals/service.go)
- [service.go](/home/orchestrator/odin-os/internal/learning/evaluator/service.go)
- [service.go](/home/orchestrator/odin-os/internal/learning/promotion/service.go)
- [0006_learning.sql](/home/orchestrator/odin-os/internal/store/sqlite/migrations/0006_learning.sql)

Gaps:

- There is no explicit approval artifact between evaluation and promotion.
- Active promotions are stored, but nothing in the runtime reads them to alter routing, retry policy, prompts, or playbooks.
- Replay fixtures are ephemeral inputs, not stored reusable fixtures or sandbox runs.

Incorrect implementations:

- The current service allows promotion immediately after evaluator-driven `approved` status; that is weaker than a distinct promotion gate.

Risky shortcuts:

- The self-improvement subsystem is currently a recorded lifecycle, not a live-but-bounded improvement mechanism.

Test coverage quality:

- Strong for storage lifecycle and rollback semantics.
- Missing for runtime effect because there is none.

### Phase 15

Status: `mostly_complete`

Satisfies the phase:

- [homelab-operations.md](/home/orchestrator/odin-os/docs/contracts/homelab-operations.md)
- [cutover-readiness.md](/home/orchestrator/odin-os/docs/operations/cutover-readiness.md)
- [odin.service](/home/orchestrator/odin-os/deploy/systemd/odin.service)
- [odin.env.example](/home/orchestrator/odin-os/deploy/systemd/odin.env.example)
- backup/restore scripts under [scripts/dev/](/home/orchestrator/odin-os/scripts/dev)
- [service.go](/home/orchestrator/odin-os/internal/app/backup/service.go)
- startup recovery in [startup.go](/home/orchestrator/odin-os/internal/runtime/recovery/startup.go)
- operational endpoints in [operational.go](/home/orchestrator/odin-os/internal/api/http/operational.go)

Gaps:

- Fresh installs are not ready until observability rows are manually seeded.
- No signal handling is installed in [main.go](/home/orchestrator/odin-os/cmd/odin/main.go); `serve` runs from `context.Background()`.
- The deployment path assumes a user service in [install-systemd-service.sh](/home/orchestrator/odin-os/scripts/dev/install-systemd-service.sh), while [odin.env.example](/home/orchestrator/odin-os/deploy/systemd/odin.env.example) defaults to `/var/odin`, which typical user services cannot write without extra setup.

Incorrect implementations:

- Graceful shutdown is incomplete for real systemd operation because no signal-aware context drives server shutdown.

Risky shortcuts:

- The cutover checklist is better than the runtime readiness automation behind it.

Test coverage quality:

- Good for backup, restore, verify, serve bootstrap, and startup recovery.
- Missing for signal-driven shutdown and full deployment realism.

## Architecture Violations And Drift

### Legacy overlap

Low risk. No large legacy directories were copied into the runtime tree. The migration outputs are isolated under `docs/migration/` and `state/migration/`.

### Shared mutable workspaces

High risk by omission. The worktree model exists, but there is no runtime mutation path that must acquire a lease before acting. Isolation is therefore not enforced end-to-end.

### Provider-specific coupling

Low to moderate risk. Core packages remain mostly provider-agnostic. The problem is not vendor spaghetti; the problem is that the provider-agnostic executor layer is still largely inert.

### Missing observability

High risk. The doctor and metrics surfaces exist, but the runtime does not produce the freshness and health rows they depend on. A fresh runtime therefore looks degraded until manually seeded.

### Fake compaction

Moderate risk. The packet format and resume path are real, but trigger coverage and runtime integration are incomplete enough that compaction is only partially operational.

### Incomplete worktree isolation

High risk. Worktree/branch logic is good in isolation, but it is not the enforced mutation path. The default worktree root is also wrong because `~` is not expanded.

### Unsafe system-project behavior

High risk. `odin-core` rules are validated in manifests, but runtime code does not enforce those rules before task creation, mutation routing, or transition-controlled actions.

## Prioritized Alpha Blockers

1. Fresh-runtime readiness is not self-bootstrapping.
   evidence:
   `doctor --json` is degraded and `healthcheck` fails on a clean `ODIN_ROOT` because bootstrap/serve never record registry compilation, executor health, or projection freshness.
2. The executor layer is not real end-to-end.
   evidence:
   adapter execution methods return `ErrNotImplemented`, and selector usage appears in tests only.
3. Governance and transition rules are not enforced at execution time.
   evidence:
   policy fields are only consumed in manifest validation; transition authorization is only exercised in tests and service helpers.
4. Worktree isolation is not mandatory in runtime execution.
   evidence:
   lease/worktree managers are never called by an executor or run orchestration path, and the default path uses an unexpanded `~`.
5. Self-heal is not running as a subsystem.
   evidence:
   `RunCycle` is not scheduled or invoked by `serve`; only startup recovery is wired.
6. Self-improvement has no explicit promotion approval gate and no runtime consumer.
   evidence:
   promotion works after evaluator approval alone, and active promotions are not read by router/policy/runtime code.
7. Homelab service shutdown is not production-safe yet.
   evidence:
   `main` uses `context.Background()` and does not handle signals, so graceful stop behavior depends on external process termination rather than controlled shutdown.

## Recommended Prompt 17 Stabilization Plan

### Goal

Turn the current library-first alpha into a composed, runtime-credible alpha without expanding scope.

### Minimum work

1. Bootstrap real runtime health state.
   - Load and compile the Markdown registry during bootstrap.
   - Record registry version on startup.
   - Run one executor health sampling pass on startup.
   - Refresh projection freshness on startup and after major state changes.
   - Load and apply `config/telemetry.yaml`.

2. Wire one real execution lane.
   - Implement one working executor path, preferably one headless CLI lane or one API lane.
   - Route queued Act tasks through executor selection and run creation.
   - Keep all other adapters as unsupported until implemented.

3. Enforce governance at the execution boundary.
   - Before any mutating task starts, require transition authorization, project policy checks, and `odin-core` special-rule enforcement.
   - Make policy denial append auditable events.

4. Make worktree isolation mandatory for mutating runs.
   - Expand the default global worktree root correctly.
   - Acquire leases and create task-owned branches/worktrees from the real execution path.
   - Reject mutable work that cannot obtain an isolated lease.

5. Promote compaction from library to lifecycle.
   - Trigger wake packets for approval wait, completion, handoff, and restart first.
   - Defer token-threshold and model-switch automation if needed, but make the high-value triggers real.

6. Start the self-heal loop.
   - Run a bounded recovery cycle in serve mode on a timer.
   - Wire structured logging for recovery actions.
   - Record outputs into run logs or another operator-visible sink.

7. Tighten self-improvement promotion.
   - Introduce a distinct approval step before promotion.
   - Make one runtime subsystem consume active promotions, preferably executor routing.

8. Fix homelab operational sharp edges.
   - Use `signal.NotifyContext` in `main`.
   - Align the service installation path and default runtime root.
   - Re-run backup/restore and readiness checks against the real service path.

### Exit condition for stabilization

Prompt 17 should be considered successful only if:

- a fresh runtime reaches `healthy` readiness without manual SQL seeding
- an Act task can create a run through one real executor lane
- mutating work is forced through a leased worktree and task-owned branch
- transition and system-project rules block unsafe actions in the real execution path
- at least one self-heal cycle runs automatically in serve mode
- promoted improvements change bounded runtime behavior through an auditable consumer
