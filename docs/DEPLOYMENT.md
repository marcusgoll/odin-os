---
title: Odin OS Deployment
status: active
date: 2026-04-30
---

# Odin OS Deployment

This document describes the supported deployment paths for Odin OS. It preserves
the current user-systemd deployment through the hardened `odin-os.service`
path.

## Current Server Path

The live user service on this host is expected to be:

- service: `odin-os.service`
- release directory: `~/odin-os-live`
- env file: `~/.config/odin/odin-os.env`
- runtime state: `~/.local/state/odin-os`
- listener: `127.0.0.1:9444`

Older repo assets named `odin.service` and `odin.env` are compatibility assets.
Do not treat them as proof of the current production controller without checking
the live service:

```bash
systemctl --user status odin-os.service --no-pager
systemctl --user cat odin-os.service
```

See `docs/operations/legacy-systemd-disposition.md` for the full inventory of
legacy `odin.service` and `odin.env` references and the migration decision for
each one.

## Build

Build both binaries from a clean checkout:

```bash
make build
```

The systemd service uses the canonical `bin/odin` command surface.

## User systemd Install

Install the canonical user service without starting it:

```bash
scripts/install-service.sh
```

Install and start it:

```bash
scripts/install-service.sh --start
```

For dry-run inspection:

```bash
scripts/install-service.sh --dry-run --start
```

The installer copies:

- `deploy/systemd/odin-os.service` to `~/.config/systemd/user/odin-os.service`
- `deploy/systemd/odin-os.env.example` to `~/.config/odin/odin-os.env` if the env file does not already exist

It does not overwrite an existing env file unless `--force` is provided.

Do not use `scripts/dev/install-systemd-service.sh` for new deployments. That
script remains only as a compatibility installer for the older `odin.service`
unit and must not be run against a live host unless a human operator explicitly
approves the legacy path.

## Environment Files and Secrets

Secrets belong only in the machine-local env file, never in Git or Docker image
layers.

Use:

```bash
cp deploy/systemd/odin-os.env.example ~/.config/odin/odin-os.env
chmod 600 ~/.config/odin/odin-os.env
```

Then edit machine-local values such as:

- `ODIN_ROOT`
- `ODIN_HTTP_ADDR`
- `ODIN_ADMIN_TOKEN`
- `ODIN_PROJECTS_OVERLAY`
- local driver paths

Codex workers must not receive production env files. Worker prompts should get
sanitized context and task-specific workspace paths only.

## Start, Stop, and Healthcheck

```bash
scripts/start.sh
scripts/healthcheck.sh
scripts/stop.sh
```

Equivalent systemd commands:

```bash
systemctl --user start odin-os.service
systemctl --user status odin-os.service --no-pager
systemctl --user stop odin-os.service
```

Machine readiness:

```bash
ODIN_ENV_FILE=~/.config/odin/odin-os.env scripts/healthcheck.sh
curl -fsS http://127.0.0.1:9444/readyz
```

## Goal Tick Loop

`odin serve` runs the deterministic goal runner tick once during startup and
then on a conservative 30 second cadence. Operators can still run the same path
manually with:

```bash
odin goal tick --json
```

Goal tick state remains in SQLite and audit events appear in `odin logs --json`.

## Kill Switch

When dashboard admin auth is configured with `ODIN_ADMIN_TOKEN`, the kill switch
can force readiness closed:

```bash
curl -fsS -X POST \
  -H "Authorization: Bearer $ODIN_ADMIN_TOKEN" \
  http://127.0.0.1:9444/kill-switch/on
```

Turn it off:

```bash
curl -fsS -X POST \
  -H "Authorization: Bearer $ODIN_ADMIN_TOKEN" \
  http://127.0.0.1:9444/kill-switch/off
```

Keep the HTTP listener on loopback unless a reviewed reverse proxy, SSH tunnel,
or firewall policy protects admin endpoints.

Use `docs/operations/dashboard-admin-hardening.md` for the full admin endpoint
runbook, including token setup, rotation, SSH tunnel, reverse proxy/TLS, and
audit expectations.

## Dry Run

There is no global production dispatch dry-run switch yet. `odin status --json`
and the daemon `/status` endpoint surface the current worker-dispatch posture
under `worker_dispatch`:

- `mode=live` means readiness currently allows the service loop to attempt
  queued or dispatched work.
- `mode=paused` means readiness currently prevents worker dispatch.
- `dry_run=false` and `read_only=false` mean no global runtime dry-run or
  read-only switch is active. Project transition, approval, and worktree policy
  still gate individual task mutation.

Until a global runtime switch exists, dry-run means:

- use `scripts/install-service.sh --dry-run` before installing service files
- keep new integrations in read-only intake or shadow mode
- keep human approval required before merge or deployment
- keep production secrets out of worker environments

Any future runtime dry-run switch must be documented here and reflected in
`worker_dispatch.dry_run` or `worker_dispatch.read_only`.

## Docker Compose

Docker is optional and not the current live path. The provided Compose file runs
as a non-root user, binds HTTP to loopback, and stores runtime state in a named
volume:

```bash
docker compose -f deploy/docker/docker-compose.yml build
docker compose -f deploy/docker/docker-compose.yml up -d
docker compose -f deploy/docker/docker-compose.yml ps
```

Run the local runtime smoke:

```bash
make docker-smoke
```

The smoke builds `deploy/docker/Dockerfile`, starts the Compose service with a
non-secret env fixture, verifies `GET /health`, confirms the container runs as a
non-root user, and checks the Compose hardening settings: read-only root
filesystem, `cap_drop: ALL`, and `no-new-privileges:true`.
The target delegates to `scripts/tests/docker-compose-smoke.sh`.

Required host prerequisites:

- Docker daemon reachable by the current user
- Docker Compose plugin
- `curl`
- a free loopback port; the smoke chooses one automatically unless
  `ODIN_DOCKER_SMOKE_PORT` is set

The Compose listener defaults to `127.0.0.1:9444`. Override it for local smoke
or port-conflict avoidance:

```bash
ODIN_COMPOSE_HTTP_BIND=127.0.0.1:19444 docker compose -f deploy/docker/docker-compose.yml up -d
```

Use a machine-local env file for real secrets:

```bash
ODIN_COMPOSE_ENV_FILE=/path/to/odin-os.env docker compose -f deploy/docker/docker-compose.yml up -d
```

## Rollback

Before a production cutover:

1. Build and test from a clean checkout.
2. Back up the runtime state:

   ```bash
   ./bin/odin backup "$HOME/.local/state/odin-os/backups/pre-cutover.tar.gz"
   ```

3. Keep the previous release directory or symlink target.
4. Start the new service and verify readiness.

Rollback sequence:

```bash
systemctl --user stop odin-os.service
ln -sfn "$HOME/.local/share/odin-os/releases/<previous-sha>" "$HOME/odin-os-live"
systemctl --user start odin-os.service
scripts/healthcheck.sh
```

If state migration is involved, restore from the backup instead of editing the
SQLite database in place:

```bash
./bin/odin restore "$HOME/.local/state/odin-os/backups/pre-cutover.tar.gz"
```

## Production Guardrails

- No autonomous production deploy.
- No autonomous merge.
- No direct commits to `main`.
- Run as a non-root user where practical.
- Keep admin endpoints on loopback or behind reviewed access control.
- Keep production secrets out of Docker images, Git, prompts, logs, and worker
  environments.
- Prefer clean release directories under `~/.local/share/odin-os/releases/<sha>`
  and repoint `~/odin-os-live` only after proof.
