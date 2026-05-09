# Cutover Readiness Checklist

Use this checklist before treating Odin OS as the active homelab controller.

## Deployment

- `odin` binary is built and installed at the intended runtime path
- `deploy/systemd/odin-os.service` is installed and enabled
- `deploy/systemd/odin-os.env.example` has been copied to `~/.config/odin/odin-os.env` and reviewed
- the configured `ODIN_ROOT` exists and has the expected permissions

## Restart safety

- `systemctl --user restart odin-os.service` completes cleanly
- interrupted runs are converted into auditable restart recovery records
- restart-triggered wake packets are visible for recovered work

## Health and metrics

- `odin healthcheck` succeeds on a healthy runtime
- `odin doctor --json` returns structured output
- `/healthz`, `/readyz`, and `/metrics` respond from the running service
- degraded dependencies produce degraded readiness instead of a false healthy state

## Backup and restore

- a fresh backup archive has been created
- backup verification has passed
- restore into a fresh target root has been exercised
- the restored SQLite database opens successfully

## Project governance

- `odin-core` remains registered as a system project
- managed projects have valid transition states
- projects in `shadow` or `compare` remain read-only
- projects in `limited_action` have reviewed low-risk allowlists
- no project is under dual mutation authority

## Observability and recovery

- incidents and recoveries are visible through Odin projections
- queue pressure, projection freshness, source freshness, and executor health are visible in doctor output
- self-heal escalation paths are visible instead of looping silently

## Media profile

- media supervision is enabled only through the bounded media profile contract
- `make test-media` passes against the current repo state before cutover
- mount audit passes before any approved media maintenance
- safe automatic actions, approval-required actions, and forbidden actions have been reviewed for the media profile
- operator has reviewed explicit safe vs unsafe media automation boundaries
- media-specific backup freshness and rollback expectations are documented for approved maintenance

## Final cutover review

- the active project portfolio view has been reviewed
- any remaining degraded checks have an explicit operator decision
- backup archive location and restore procedure are documented for the homelab
- the operator is ready to treat Odin OS as the active controller for cutover-approved projects
