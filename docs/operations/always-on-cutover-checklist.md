# Always-On Cutover Checklist

Use this checklist before treating a single-daemon `odin serve` deployment as the always-on controller for a runtime root.

## Install and service restart

- `make build` completes and the intended `odin` binary is installed on the host path used by the service manager.
- `deploy/systemd/odin-os.service` and the real env file derived from `deploy/systemd/odin-os.env.example` are installed and reviewed.
- `ODIN_ROOT` points at the intended runtime root and the service account can read and write `data/`, `state/`, and `runs/`.
- `systemctl --user restart odin-os.service` completes cleanly.
- `systemctl --user status odin-os.service --no-pager` shows one active daemon for the runtime root.

## Health and readiness verification

- `odin doctor --json` returns structured output and no unexpected degraded checks.
- `odin healthcheck` fails closed before the daemon is running, succeeds while the daemon is healthy, and fails closed again after the daemon stops.
- `/healthz` responds while the daemon is alive.
- `/readyz` returns success only when dispatch is safe.
- `/metrics` is reachable from the running daemon.

## Startup recovery drill

- Stop the daemon during an in-flight run, then start it again.
- Confirm the interrupted run is recorded as `interrupted`.
- Confirm a restart-triggered wake packet exists for the affected task.
- Confirm a recovery record or recovery event is visible for the restart repair.

## Blocked approval drill

- Create or stage a task that requires explicit approval.
- Confirm the task becomes `blocked` with `blocked_reason = approval_required`.
- Confirm the latest wake packet is an approval-wait handoff.
- Confirm blocked work is visible through the task-status and blocked-item projections before approval is granted.

## Lease cleanup drill

- Start a mutating task that acquires a leased worktree.
- Interrupt or stop the daemon before the task completes.
- Confirm startup recovery or lease maintenance marks the lease stale or released.
- Confirm the maintenance loop eventually removes the released or stale worktree.

## Backup and restore drill

- Create a fresh backup archive from the live runtime root with `odin backup <archive-path>`.
- Run `odin verify-backup <archive-path>` and confirm it passes before release cutover.
- Restore into a clean target root instead of overwriting the live one.
- Open the restored SQLite database and confirm `odin doctor --json` can inspect it.

## Release dry-run gate

- `make homelab-release-dry-run` completes without installing, updating, restarting, or repointing production paths.
- `make odin-actual-use-e2e` completes, or any failure is recorded as an explicit stop condition before cutover.
- `./bin/odin backup --help`, `./bin/odin restore --help`, `./bin/odin verify-backup --help`, and `./bin/odin serve --help` return bounded usage output.
- Service readiness gates include `doctor`, fail-closed `healthcheck`, `overview`, `work status`, `review list`, `approvals all`, and actual-use E2E proof.

## Operator sign-off

- Blocked items, pending approvals, incidents, and recoveries are visible through Odin projections.
- Any remaining degraded checks have an explicit operator decision and follow-up.
- The restore location and last verified archive path are recorded for the host.
- The operator is ready to let the single daemon own runtime state for the approved project set.
