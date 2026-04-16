# Phase Gap Matrix

Date: 2026-04-09

Sorted by severity.

| Severity | Phase | Status | Gap | Evidence | Minimum Fix |
| --- | --- | --- | --- | --- | --- |
| critical | 10, 15 | partial / mostly_complete | Fresh runtimes never become ready on their own. | [bootstrap.go](/home/orchestrator/odin-os/internal/app/bootstrap/bootstrap.go) does not load the Markdown registry or write freshness rows. [run.go](/home/orchestrator/odin-os/internal/app/lifecycle/run.go) serves health directly from DB state. Fresh `doctor --json` shows degraded executor, projections, and source freshness; fresh `healthcheck` exits non-zero. | Bootstrap registry compilation, executor health sampling, and projection freshness updates; load and use [telemetry.yaml](/home/orchestrator/odin-os/config/telemetry.yaml). |
| critical | 06 | partial | Executor abstraction is still mostly inert. | [types.go](/home/orchestrator/odin-os/internal/executors/contract/types.go) returns `ErrNotImplemented` for execution methods. Selector usage appears in tests, not runtime callers. | Implement one real executor lane and route queued Act tasks through it. |
| high | 04, 13 | mostly_complete / partial | Governance and transition rules are not enforced at the execution boundary. | Policy fields are only used in [validate.go](/home/orchestrator/odin-os/internal/core/projects/validate.go). Transition authorization lives in [service.go](/home/orchestrator/odin-os/internal/core/projects/service.go), but callers are tests only. | Gate mutable execution through project policy plus transition authorization before runs start. |
| high | 09 | mostly_complete | Worktree isolation is not mandatory end-to-end, and the default root is wrong. | [paths.go](/home/orchestrator/odin-os/internal/vcs/worktrees/paths.go) uses literal `~/.config/...`. [manager.go](/home/orchestrator/odin-os/internal/vcs/leases/manager.go) is used in tests only. | Expand the home directory properly and require all mutating execution to acquire a lease and worktree. |
| high | 11 | partial | Self-heal is implemented as a library, not as a running subsystem. | [service.go](/home/orchestrator/odin-os/internal/runtime/recovery/service.go) exposes `RunCycle`, but `serve` only calls [startup.go](/home/orchestrator/odin-os/internal/runtime/recovery/startup.go). | Schedule bounded self-heal cycles in serve mode and surface results in logs and metrics. |
| high | 14 | partial | Self-improvement promotions do not have a separate approval gate and do not affect live runtime behavior. | [service.go](/home/orchestrator/odin-os/internal/learning/promotion/service.go) promotes once proposal status is `approved`. No runtime consumer reads active promotions. | Add explicit approval before promotion and wire one bounded consumer, preferably executor routing. |
| medium | 07 | partial | Dynamic tool loading proves catalog mechanics, not real tool execution. | [builtin.go](/home/orchestrator/odin-os/internal/tools/catalog/builtin.go) returns canned summaries. Broker usage is limited to tests and planner helpers. | Replace at least one built-in canned tool with a runtime-backed invocation and use broker expansion in the real planning path. |
| medium | 08 | partial | Compaction trigger coverage is incomplete. | [types.go](/home/orchestrator/odin-os/internal/runtime/checkpoints/types.go) defines seven triggers, but live callers only use `approval_wait` and `restart`. | Wire completion, handoff, and approval flows first; add model-switch and token-threshold later if still needed. |
| medium | 10 | partial | Structured logging is not operationally wired. | [logger.go](/home/orchestrator/odin-os/internal/telemetry/logs/logger.go) is barely used and writes JSON without newline delimiters. `config/telemetry.yaml` is unused. | Initialize logger in runtime services and emit newline-delimited JSON records to an operator-visible sink. |
| medium | alpha suite | incorrect | The acceptance suite proves too much with seeded state and isolated services. | [alpha_acceptance_test.go](/home/orchestrator/odin-os/tests/integration/alpha_acceptance_test.go) seeds health rows directly and validates service helpers more often than composed runtime execution. | Extend `test-alpha` to exercise fresh-bootstrap readiness, one real executor run, mandatory worktree use, and runtime transition enforcement. |
| medium | 15 | mostly_complete | Homelab service stop behavior is not graceful. | [main.go](/home/orchestrator/odin-os/cmd/odin/main.go) uses `context.Background()` and does not install signal handling. | Use `signal.NotifyContext` and test clean shutdown under SIGTERM. |
| medium | 15 | mostly_complete | Deployment defaults are inconsistent. | [install-systemd-service.sh](/home/orchestrator/odin-os/scripts/dev/install-systemd-service.sh) installs a user service, while [odin.env.example](/home/orchestrator/odin-os/deploy/systemd/odin.env.example) defaults `ODIN_ROOT` to `/var/odin`. | Align the default runtime root with the service type, or document the privilege/setup requirement explicitly. |
| low | 02 | mostly_complete | Registry loading is not part of bootstrap. | [bootstrap.go](/home/orchestrator/odin-os/internal/app/bootstrap/bootstrap.go) loads project manifests only. | Compile the Markdown registry during startup and make its diagnostics part of runtime state. |
| low | 12 | mostly_complete | `migrate_as_is` is defined but never emitted. | [types.go](/home/orchestrator/odin-os/internal/migration/extractor/types.go) defines the classification, but [classify.go](/home/orchestrator/odin-os/internal/migration/extractor/classify.go) never returns it. | Add at least one deterministic `migrate_as_is` rule and cover it with tests. |
| low | docs | drifted | README overstates operational completeness. | [README.md](/home/orchestrator/odin-os/README.md) says Phase 00 through Phase 15 are “in place,” but several late phases are library-only or not runtime-wired. | Update README after stabilization so it reflects composed runtime behavior, not package presence. |

## Minimal Alpha Fix Set

1. Make fresh-bootstrap readiness healthy without manual seeding.
2. Implement and wire one real executor lane.
3. Enforce policy, transition, and system-project safety in the runtime execution path.
4. Make leased worktrees mandatory for mutable runs.
5. Start the self-heal loop in serve mode.
6. Add a real promotion gate and one runtime consumer for active improvements.
7. Fix service shutdown and deployment-default mismatches.
