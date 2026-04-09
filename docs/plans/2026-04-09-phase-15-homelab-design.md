# Phase 15 Homelab Deployment And Cutover Readiness Design

## Goal

Harden Odin OS for 24/7 homelab operation with simple deployment, bounded restart recovery, machine-readable health, verified backups, and an operator-facing cutover readiness checklist.

## Current Context

Odin OS already has the core runtime pieces needed for a long-lived service:

- SQLite is the canonical runtime authority
- the registry loader, doctor surface, metrics exporter, and runtime projections already exist
- context compaction and wake packets can capture restart handoff state
- self-heal, transition control, and learning records are already auditable

What the system does not yet have is a practical deployment shape for homelab use. Today the binary is effectively CLI-first only, backup and restore procedures are missing, and there is no startup recovery pass that converts interrupted runtime state into a resumable form.

Phase 15 should close that gap without introducing unnecessary distributed infrastructure.

## Approaches Considered

### 1. Native systemd service with a lightweight daemon mode and filesystem-backed state

This is the recommended approach. It fits the Go-first and SQLite-backed runtime, keeps homelab deployment simple, and avoids container overhead while the long-running service shape is still stabilizing.

### 2. Single-container Docker Compose deployment with mounted volumes

This is a reasonable secondary path later, but it adds image and volume management before the native service model is proven. For a SQLite-backed CLI-first service, it is not simpler than `systemd`.

### 3. Shell-wrapper deployment around the existing interactive CLI only

This is not sufficient. It would leave restart recovery, readiness endpoints, and backup procedures underdefined.

## Recommendation

Add a small headless service mode to the existing `odin` binary and make `systemd` the primary deployment target for this phase.

The service mode should:

- bootstrap state directories and the runtime store
- run migrations
- validate project manifests and registry health
- perform a bounded startup recovery pass
- expose narrow operational HTTP endpoints for health and metrics

Interactive CLI use remains the default entrypoint. Phase 15 adds an operational runtime mode, not a new control plane.

## Command Model

Phase 15 should extend the CLI with these operational commands:

- `odin serve`
- `odin healthcheck`
- `odin doctor --json`

`odin serve` should start the long-running service loop and operational endpoints.

`odin healthcheck` should return a narrow machine-oriented success or failure result intended for `systemd` and scripts.

`odin doctor --json` should reuse the existing doctor surface but make the machine-readable mode explicit in the CLI.

## Service Endpoints

Phase 15 should add a small HTTP operational server with:

- `/healthz`
- `/readyz`
- `/metrics`

These endpoints should reuse existing health and metrics services rather than introducing a broad API surface.

Rules:

- `/healthz` answers whether the service process is up and can inspect local state
- `/readyz` answers whether Odin is ready to operate on projects safely
- `/metrics` emits the existing machine-readable metrics snapshot

The HTTP server is operational only. It is not the future web API.

## Startup Recovery Model

Service startup should include a deterministic recovery pass before the service reports ready.

Recommended behavior:

1. open the store and complete migrations
2. load and validate project manifests
3. list runs still marked as `running`
4. convert those runs into an interrupted terminal state for the previous process lifecycle
5. move affected tasks into a resumable queued state when appropriate
6. create restart-triggered wake packets capturing the next-step handoff state
7. append explicit recovery and event records for the transition

This does not attempt to resurrect in-memory execution. It produces auditable handoff state so the next operator or worker can resume safely.

## Deployment Layout

Phase 15 should add native deployment artifacts:

- `deploy/systemd/odin.service`
- `deploy/systemd/odin.env.example`
- `scripts/dev/install-systemd-service.sh`

The service should run the compiled `odin` binary with an explicit working directory and restart-on-failure behavior.

The runtime root should default to the repo-root layout for local development, but support `ODIN_ROOT` so homelab installs can place persistent state under a dedicated path.

## Backup And Restore Model

Phase 15 should add small operator scripts:

- `scripts/dev/backup-odin.sh`
- `scripts/dev/restore-odin.sh`
- `scripts/dev/verify-backup.sh`

Backup scope:

- `data/odin.db`
- `registry/`
- `memory/`
- selected runtime config needed to reopen the system consistently

The backup format should stay simple: timestamped archive output plus SQLite-safe copy behavior. Restore should target a provided destination root and must not overwrite the live installation silently.

Verification should confirm:

- the archive can be unpacked
- the SQLite database opens successfully
- required directories exist after restore

## Health And Resilience Expectations

Phase 15 should make degraded dependencies visible through both CLI and operational endpoints.

At minimum, the health surfaces must show:

- database failure
- registry validation failure
- projection or source freshness degradation
- executor health degradation

The readiness check should fail closed when startup validation or recovery cannot complete safely.

## Testing Strategy

Tests should cover:

1. fresh service bootstrap in a temporary runtime root
2. restart recovery converting active runs into interrupted or resumable state
3. health endpoint and `healthcheck` degradation when required dependencies are missing
4. backup creation and restore into a fresh target root
5. restored runtime reopening successfully after backup verification

The restart recovery tests should explicitly prove that wake packets are created and that resumed work does not depend on raw transcript history.

## Cutover Readiness

Phase 15 should add an operator-facing checklist at `docs/operations/cutover-readiness.md`.

The checklist should include at least:

- deployment install verified
- service restart behavior verified
- doctor and health endpoints verified
- backup and restore drill completed
- managed projects reviewed for transition state
- limited-action and cutover preconditions reviewed
- recovery and incident visibility confirmed

The checklist is for real operational cutover, not just for code completeness.

## Non-Goals

Phase 15 does not include:

- a full multi-node deployment model
- HA SQLite replication
- a full web API or PWA surface
- container-first orchestration as the primary deployment path
- uncontrolled automatic resume of in-flight model sessions
