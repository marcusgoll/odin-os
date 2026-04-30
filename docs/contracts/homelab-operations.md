---
title: Homelab Operations Contract
status: active
date: 2026-04-09
phase: "15"
---

# Homelab Operations Contract

Phase 15 defines the minimum operational contract for running Odin OS as a long-lived homelab service.

## Runtime modes

The `odin` binary supports:

- interactive shell by default
- `odin serve` for long-running service mode
- `odin healthcheck` for machine-oriented readiness checks
- `odin doctor --json` for machine-readable health reporting

## Operational endpoints

When `odin serve` is running, the operational server exposes:

- `/healthz`
- `/readyz`
- `/metrics`

These endpoints are narrow operational surfaces only. They are not the future Odin web API.

## Health semantics

- `/healthz` reports whether the process is up and can inspect local runtime state
- `/readyz` reports whether Odin is safe to operate on managed projects
- `odin healthcheck` uses the same readiness model and returns non-zero on degraded or failed readiness

## Startup recovery

Before reporting ready, `odin serve` must run a bounded startup recovery pass.

The recovery pass:

- finds runs left in `running`
- marks them as interrupted
- requeues affected tasks when the work is resumable
- writes restart-triggered wake packets
- records recovery actions in events and recovery records

Startup recovery does not attempt to restore raw in-memory execution state.

## Runtime root

Phase 15 uses the repo-root layout by default for local development and supports `ODIN_ROOT` for dedicated homelab state roots.

The runtime root contains:

- `data/`
- `state/`
- `runs/`

Registry, memory, and authored config remain repo-managed unless a later phase explicitly promotes a different deployment layout.

## Backup scope

Phase 15 backup and restore covers:

- `data/odin.db`
- `registry/`
- `memory/`
- `config/odin.yaml`
- `config/projects.yaml`
- `config/policies.yaml`
- `config/telemetry.yaml`
- `config/executors.yaml`
- `config/models.yaml`

Restore must target an explicit destination root and must not overwrite a live runtime silently.

## Deployment target

Phase 15 treats `systemd` as the primary homelab deployment path.

Supporting artifacts include:

- `deploy/systemd/odin.service`
- `deploy/systemd/odin.env.example`
- `scripts/dev/install-systemd-service.sh`

## FLICA Tradeboard integration

Odin is the operator surface for FLICA Tradeboard actions, but PBS remains the browser and credential owner. The `/tradeboard` shell command calls the PBS/flight-api service configured by `ODIN_TRADEBOARD_API_BASE_URL`, `ODIN_TRADEBOARD_API_TOKEN`, and `ODIN_TRADEBOARD_API_TIMEOUT_SECONDS` (or the legacy `PBS_API_BASE_URL` and `FLIGHT_API_TOKEN` fallback variables).

The broader Marcus/FLICA boundary is recorded in the root `CONTEXT.md`: Odin owns the Marcus FLICA Operations Workflow Suite as an operator workflow suite, while PBS owns the FLICA-backed airline bidding, TradeBoard, vacation, and schedule domain capabilities.

AA credentials and FLICA browser session state stay in PBS:

- `AA_ID`
- `AA_PASSWORD`
- `FLICA_PLAYWRIGHT_BROWSER`
- `FLICA_DUO_WAIT_SECONDS`
- `DATA_DIR/playwright/.auth.json`

When AA SSO presents Duo, the PBS FLICA scanner should use the operator-selected Duo method, including `Call Me` when requested, and wait within `FLICA_DUO_WAIT_SECONDS`. If approval does not complete inside that window, the operation must fail as an operator-attended authentication timeout rather than as a credential failure.

The repeatable operator procedure for partial-pairing TradeBoard posts is authored in `registry/workflows/flica-tradeboard-split-post.md`. That workflow is operator-invoked only, uses Huginn for live browser proof, and requires FLICA My Requests readback before the post is treated as complete.

## Audit expectations

Phase 15 operational actions must remain inspectable:

- startup recovery writes runtime events and recovery records
- health and readiness are machine-readable
- backup verification is explicit rather than assumed
- cutover readiness is documented as a checklist, not tribal knowledge
