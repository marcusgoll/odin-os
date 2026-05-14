---
title: Odin Homelab Always-On Runbook
status: active
date: 2026-05-12
---

# Odin Homelab Always-On Runbook

This runbook is the single operator path for running Odin OS as an always-on
homelab service after the actual-use E2E harness passes.

It is intentionally conservative:

- Do not deploy, restart, or repoint the live homelab service without explicit operator approval.
- Prove the release locally and in dry-run mode before touching production paths.
- Preserve the previous installed release directory or symlink target for rollback.
- Treat `odin healthcheck` as fail-closed. A runtime root is not ready unless a live `odin serve` process owns it and readiness checks pass.
- Verify a backup archive before cutover. A backup path without `verify-backup` is not a release gate.

## Canonical paths

- Service: `odin-os.service`
- Service unit: `deploy/systemd/odin-os.service`
- Env template: `deploy/systemd/odin-os.env.example`
- Installed env file: `~/.config/odin/odin-os.env`
- Source checkout: `~/odin-os`
- Live symlink: `~/odin-os-live`
- Release root: `~/.local/share/odin-os/releases/<git-sha>`
- Runtime root: `~/.local/state/odin-os`
- Loopback listener: `127.0.0.1:9444`

Phone/PWA access is covered by `docs/operations/odin-mobile-security.md`.
Keep mobile access private by default: use Tailscale or another private-network
path with HTTPS/TLS at the phone-facing ingress, and do not publish Odin on the
public internet without a separate exposure review.

Legacy `odin.service`, `odin.env`, and `scripts/dev/install-systemd-service.sh`
are compatibility assets only. New homelab operation uses `odin-os.service`.

When `odin-core` is loaded from a clean release archive, set
`ODIN_CORE_GIT_ROOT` to the real source checkout. Release archives intentionally
omit `.git`, and the system project registry must point at the checkout for
readiness to stay clean:

```bash
ODIN_CORE_GIT_ROOT=/home/orchestrator/odin-os
```

## Public PWA ingress

`odin.marcusgoll.com` is a public PWA ingress, not the full operator API. The
Cloudflare Tunnel is remote-managed and currently targets `http://odin-overseer:80`
on Docker network `infrastructure_default`.

The public origin container is named `odin-overseer` for compatibility with that
remote tunnel route. It runs the current `~/odin-os-live` release with
`ODIN_HTTP_ADDR=127.0.0.1:9444` inside the container, then places nginx in front
of Odin as a path gate.

Allowed public paths:

- `/` redirects to `/app/`
- `/app/` serves the PWA shell and assets
- `/mobile/*` serves concrete authenticated mobile/PWA API endpoints; the
  `/mobile/` root may return `404` when no root endpoint is registered
- `/healthz` and `/readyz` expose health and fail-closed readiness

All other public paths, including `/metrics` and legacy `/api/v1/*` routes, must
return `404` at nginx before reaching Odin.

Runtime config lives outside the repo at:

```bash
/home/orchestrator/.homelab-runtime/odin-pwa-proxy/
```

The active container must include these environment values:

```bash
PATH=/home/orchestrator/.npm-global/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
ODIN_ROOT=/state
ODIN_HTTP_ADDR=127.0.0.1:9444
ODIN_PROJECTS_OVERLAY=/config/odin-os-projects.local.yaml
ODIN_CODEX_DRIVER=/config/odin-codex-live-driver.sh
ODIN_CORE_GIT_ROOT=/home/orchestrator/odin-os
```

The active container must also mount the registered project git roots read-only
so registry validation sees the same repositories the host sees:

```bash
/home/orchestrator/odin-os:/home/orchestrator/odin-os:ro
/home/orchestrator/pbs:/home/orchestrator/pbs:ro
/home/orchestrator/cfipros:/home/orchestrator/cfipros:ro
/home/orchestrator/marcusgoll:/home/orchestrator/marcusgoll:ro
/home/orchestrator/.config/superpowers/worktrees/family-ops/odin-os-cutover-main:/home/orchestrator/.config/superpowers/worktrees/family-ops/odin-os-cutover-main:ro
```

Because workspace-ready projects are registered, the container must expose the
same operator prerequisites used by readiness:

```bash
/home/orchestrator/.npm-global:/home/orchestrator/.npm-global:ro
/usr/bin/node:/usr/bin/node:ro
/usr/bin/tmux:/usr/bin/tmux:ro
/lib/x86_64-linux-gnu:/lib/x86_64-linux-gnu:ro
/usr/lib/x86_64-linux-gnu:/usr/lib/x86_64-linux-gnu:ro
/lib64/ld-linux-x86-64.so.2:/lib64/ld-linux-x86-64.so.2:ro
```

If the Docker-backed public PWA serve process owns the live runtime root,
`odin-os.service` must be stopped and disabled. Do not run two `odin serve`
controllers against the same `ODIN_ROOT`.

## Dry-run release verification

Run this before install, update, restart, or rollback:

```bash
make homelab-release-dry-run
```

The dry-run path:

- builds `./bin/odin`
- checks `backup`, `restore`, `verify-backup`, and `serve` help without mutation
- runs `scripts/install-service.sh --dry-run --start` with an isolated config root
- prints the update commands it would run for release staging, live symlink repoint, and service restart
- proves release gates against a temporary `ODIN_ROOT`
- confirms `healthcheck` fails closed before `serve`
- reads back `doctor`, `overview`, `work status`, `review list`, `approvals all`, and `logs`

This script does not install service files into the real user config, repoint
`~/odin-os-live`, restart systemd, or mutate the production runtime root.

## Actual-use E2E gate

The actual-use proof is explicit because it builds binaries and starts a
short-lived `./bin/odin serve` against a temporary runtime root:

```bash
make odin-actual-use-e2e
```

Stop if this gate fails for anything other than an explicitly accepted local
environment limitation. Do not use passing unit tests as a substitute for this
operator-path proof.

## Install path

Dry-run first:

```bash
scripts/install-service.sh --dry-run --start
```

Install service files without starting:

```bash
scripts/install-service.sh
```

Install and start only after approval:

```bash
scripts/install-service.sh --start
```

The installer preserves an existing env file unless `--force` is provided.
Review the real env file before enabling the service:

```bash
sed -n '1,160p' ~/.config/odin/odin-os.env
chmod 600 ~/.config/odin/odin-os.env
```

## Update path

Build and stage a clean release directory:

```bash
make build
release_sha="$(git rev-parse HEAD)"
release_dir="$HOME/.local/share/odin-os/releases/$release_sha"
mkdir -p "$release_dir"
rsync -a --delete --exclude .git --exclude .odin --exclude .worktrees ./ "$release_dir/"
```

Preserve the rollback target before repointing:

```bash
previous_target="$(readlink -f "$HOME/odin-os-live" || true)"
printf '%s\n' "$previous_target" > "$HOME/.local/state/odin-os/last-release-target.txt"
```

Repoint only after backup verification and operator approval:

```bash
ln -sfn "$release_dir" "$HOME/odin-os-live"
systemctl --user restart odin-os.service
```

## Start, stop, and status

```bash
scripts/start.sh
systemctl --user status odin-os.service --no-pager
scripts/healthcheck.sh
scripts/stop.sh
```

Expected readiness probes:

```bash
ODIN_ENV_FILE=~/.config/odin/odin-os.env scripts/healthcheck.sh
curl -fsS http://127.0.0.1:9444/healthz
curl -fsS http://127.0.0.1:9444/readyz
curl -fsS http://127.0.0.1:9444/metrics
```

`healthcheck` must fail before `serve` owns the runtime root and after the
service stops.

## Backup and verify before cutover

Create and verify a backup before changing the live release target:

```bash
backup_dir="$HOME/.local/state/odin-os/backups/$(date -u +%Y%m%dT%H%M%SZ)"
mkdir -p "$backup_dir"
"$HOME/odin-os-live/bin/odin" backup "$backup_dir/odin-backup.tar.gz"
"$HOME/odin-os-live/bin/odin" verify-backup "$backup_dir/odin-backup.tar.gz"
```

For restore drills, restore into a clean target root. Do not overwrite the live
runtime root as a test:

```bash
restore_root="$(mktemp -d)"
"$HOME/odin-os-live/bin/odin" restore "$backup_dir/odin-backup.tar.gz" "$restore_root"
ODIN_ROOT="$restore_root" "$HOME/odin-os-live/bin/odin" doctor --json
```

## Release readiness gates

Every release candidate must pass or be explicitly stopped:

```bash
make build
go test ./...
make odin-e2e-local
make odin-actual-use-e2e
make odin-mobile-e2e
make odin-phone-release-check
./bin/odin backup --help
./bin/odin restore --help
./bin/odin verify-backup --help
./bin/odin serve --help
ODIN_ROOT="$(mktemp -d)" ./bin/odin doctor --json
ODIN_ROOT="$(mktemp -d)" ./bin/odin healthcheck
ODIN_ROOT="$(mktemp -d)" ./bin/odin overview --json
ODIN_ROOT="$(mktemp -d)" ./bin/odin work status --json
ODIN_ROOT="$(mktemp -d)" ./bin/odin review list --json
ODIN_ROOT="$(mktemp -d)" ./bin/odin approvals all --json
```

The isolated `healthcheck` command is expected to fail closed before `serve`.
That failure is a pass condition only when the output explains `not ready`.

## Log inspection

Systemd logs:

```bash
journalctl --user -u odin-os.service -n 200 --no-pager
journalctl --user -u odin-os.service -f
```

Runtime event logs:

```bash
"$HOME/odin-os-live/bin/odin" logs --json
"$HOME/odin-os-live/bin/odin" overview --json
"$HOME/odin-os-live/bin/odin" work status --json
"$HOME/odin-os-live/bin/odin" review list --json
"$HOME/odin-os-live/bin/odin" approvals all --json
```

Use these surfaces before inspecting SQLite directly.

## Rollback path

Rollback uses the preserved previous symlink target and the verified backup.

```bash
previous_target="$(cat "$HOME/.local/state/odin-os/last-release-target.txt")"
systemctl --user stop odin-os.service
ln -sfn "$previous_target" "$HOME/odin-os-live"
systemctl --user start odin-os.service
ODIN_ENV_FILE=~/.config/odin/odin-os.env scripts/healthcheck.sh
```

If a state migration was part of the failed release, restore the verified
archive into a clean root first and inspect it before deciding whether to
replace production state:

```bash
restore_root="$(mktemp -d)"
"$previous_target/bin/odin" restore "$backup_dir/odin-backup.tar.gz" "$restore_root"
ODIN_ROOT="$restore_root" "$previous_target/bin/odin" doctor --json
```

Do not restore over the production runtime root without explicit operator
approval.

## Stop conditions

Stop the release or rollback if any condition is true:

- `make odin-actual-use-e2e` fails without an accepted environment-only reason.
- `verify-backup` fails or no fresh verified backup exists.
- `healthcheck` reports ready without a live `odin serve` owner.
- `doctor`, `overview`, `work status`, `review list`, or `approvals all` cannot explain the runtime state.
- `systemctl --user status odin-os.service` shows more than one active controller for the same runtime root.
- The previous release target is unknown.
