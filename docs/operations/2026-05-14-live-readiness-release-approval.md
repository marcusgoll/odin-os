---
title: Odin OS Live Readiness Release Approval Packet
status: approval-required
date: 2026-05-14
minimum_code_release: 26ea86333ad4ec59d85ac913da40220ba3261cc2
---

# Odin OS Live Readiness Release Approval Packet

This packet is the approval checklist for installing the merged production
readiness fixes into the live homelab origin. It is intentionally not an
approval by itself. Do not run the mutation commands until the operator approves
the cutover window.

## Current Read-Only Findings

- Minimum source release containing the merged readiness evidence and fixes:
  `26ea86333ad4ec59d85ac913da40220ba3261cc2`.
- Installed host command: `/home/orchestrator/.local/bin/odin` resolves to
  `/home/orchestrator/odin-os/releases/current/bin/odin`.
- Public origin container: `odin-overseer`, image `nginx:alpine`, serving
  port `5173:80`.
- Public origin runtime owner: Docker container `odin-overseer`; the user
  `odin-os.service` is inactive and disabled.
- Public origin bind mount: `/home/orchestrator/odin-os-live:/app:ro`.
- Runtime root bind mount: `/home/orchestrator/.local/state/odin-os:/state`.
- Runtime config root bind mount: `/home/orchestrator/.config/odin:/config:ro`.
- Public ingress gate: `/app/*`, `/mobile/*`, `/healthz`, `/readyz`, and the
  exact metadata-only browser handoff paths are proxied; all other public paths
  return `404` at nginx.
- Live `/readyz` currently fails closed with HTTP `503`; this is a required
  post-cutover proof point, not an ingress exposure issue.
- A 2026-05-14 read-only probe found the Docker container config includes
  `/home/orchestrator/.npm-global/bin` in `PATH`, but the live `odin serve`
  process environment did not. `doctor --json` degraded with
  `workspace_prerequisites.codex_missing=true` even though the Codex binary was
  mounted and executable by absolute path. Treat launcher PATH preservation as a
  pre-cutover gate.
- PR #258 fixed a separate health-loop blocker where an unbounded periodic
  health cycle could leave readiness stuck after a blocking executor health
  probe or SQLite busy event. The cutover target must include that fix.

## Approval Scope

Approved scope, when the operator explicitly approves:

- stage a clean release directory from `origin/main`;
- create and verify a fresh backup archive;
- preserve the previous release target for rollback;
- repoint `/home/orchestrator/odin-os-live` to the staged release;
- recreate `odin-overseer` through the existing
  `/home/orchestrator/.homelab-runtime/odin-pwa-proxy/run-container.sh`;
- prove installed/live readiness through the public and container-owned paths;
- prove that the enabled production-readiness trigger can create dispatchable
  work after the acceptance-criteria fix is installed.

Not approved by this packet:

- restoring over the production runtime root;
- enabling `odin-os.service` while `odin-overseer` owns the same runtime root;
- widening public ingress beyond the documented path gate;
- claiming authenticated browser attach readiness;
- claiming multi-provider executor readiness beyond `codex_headless`.

## Required Pre-Cutover Gates

Run these from a clean checkout of the target release before touching the live
origin:

```bash
git fetch origin main
git switch --detach origin/main
git merge-base --is-ancestor 26ea86333ad4ec59d85ac913da40220ba3261cc2 HEAD
git status --short --branch
make homelab-release-dry-run
make odin-actual-use-e2e
make odin-pwa-e2e
```

Stop if any command fails. `make homelab-release-dry-run` must remain
non-mutating; it must not install, repoint, restart, or mutate the production
runtime root.

Before running the cutover commands, inspect the homelab launcher and stop if it
starts Odin through a login shell that can reset `PATH`. The live Odin process
must inherit `/home/orchestrator/.npm-global/bin` so `workspace_prerequisites`
can find `codex`:

```bash
grep -n -- '--entrypoint /bin/sh\\|-lc\\|-c\\|PATH=' /home/orchestrator/.homelab-runtime/odin-pwa-proxy/run-container.sh
```

## Backup And Rollback Handles

Create and verify a fresh backup before repointing the live path:

```bash
backup_dir="$HOME/.local/state/odin-os/backups/$(date -u +%Y%m%dT%H%M%SZ)"
mkdir -p "$backup_dir"

current_odin="/app/bin/odin"
docker exec odin-overseer "$current_odin" backup "/state/backups/$(basename "$backup_dir")/odin-backup.tar.gz"
docker exec odin-overseer "$current_odin" verify-backup "/state/backups/$(basename "$backup_dir")/odin-backup.tar.gz"
```

If the container path is unavailable, stop and use the host-side installed
binary only after confirming which binary owns the live runtime root.

Preserve the rollback target before repointing:

```bash
live_target="$HOME/odin-os-live"
previous_target="$(readlink -f "$live_target" || true)"
previous_target_kind="missing"
if [ -L "$live_target" ]; then
  previous_target_kind="symlink"
elif [ -e "$live_target" ]; then
  previous_target_kind="directory"
fi
test -n "$previous_target"
test "$previous_target_kind" != "missing"
test -x "$previous_target/bin/odin"
```

If `previous_target` is empty, the target kind is `missing`, or
`$previous_target/bin/odin` is not executable, stop and record the active
container mount state before proceeding. The current homelab target may be a
real directory rather than a symlink; do not use plain `ln -sfn` over an
existing directory because it can create a nested symlink instead of repointing
the live target.

## Cutover Commands

Run only after explicit approval and passing pre-cutover gates:

```bash
backup_dir="${backup_dir:?run the backup section in this shell before cutover}"
live_target="${live_target:-$HOME/odin-os-live}"
previous_target="${previous_target:-$(readlink -f "$live_target" || true)}"
previous_target_kind="${previous_target_kind:-missing}"
if [ "$previous_target_kind" = "missing" ]; then
  if [ -L "$live_target" ]; then
    previous_target_kind="symlink"
  elif [ -e "$live_target" ]; then
    previous_target_kind="directory"
  fi
fi

release_sha="$(git rev-parse HEAD)"
release_dir="$HOME/.local/share/odin-os/releases/$release_sha"

mkdir -p "$release_dir"
rsync -a --delete --exclude .git --exclude .odin --exclude .worktrees ./ "$release_dir/"

if [ "$previous_target_kind" = "directory" ]; then
  preserved_target="$backup_dir/odin-os-live.previous"
  test ! -e "$preserved_target"
  mv "$live_target" "$preserved_target"
  previous_target="$preserved_target"
fi

printf '%s\n' "$previous_target" > "$HOME/.local/state/odin-os/last-release-target.txt"
ln -sfnT "$release_dir" "$live_target"
test "$(readlink -f "$live_target")" = "$release_dir"
grep -n 'healthCtx, cancel := serveOperationContext(operationCtx)' "$live_target/internal/app/lifecycle/run.go"
/home/orchestrator/.homelab-runtime/odin-pwa-proxy/run-container.sh
```

Keep `odin-os.service` disabled while the Docker origin owns
`/home/orchestrator/.local/state/odin-os`.

## Immediate Post-Cutover Proof

Run these before declaring the release installed:

```bash
docker ps --filter name=odin-overseer --format '{{.Names}} {{.Status}} {{.Ports}}'
docker exec odin-overseer /app/bin/odin doctor --json
docker exec odin-overseer sh -c 'pid="$(pgrep -f "/app/bin/odin serve" | head -1)"; test -n "$pid"; tr "\0" "\n" </proc/"$pid"/environ | grep "^PATH="'
docker exec odin-overseer sh -c 'pid="$(pgrep -f "/app/bin/odin serve" | head -1)"; test -n "$pid"; tr "\0" "\n" </proc/"$pid"/environ | grep "^PATH=" | grep -F "/home/orchestrator/.npm-global/bin"'
docker exec odin-overseer /app/bin/odin healthcheck
curl -fsS http://127.0.0.1:5173/healthz
curl -fsS http://127.0.0.1:5173/readyz
```

Then re-run the public ingress matrix from the production readiness briefing:

```bash
curl -sk -o /tmp/odin-route-body -w '%{http_code} %{content_type} %{redirect_url}\n' \
  https://odin.marcusgoll.com/ \
  https://odin.marcusgoll.com/app/ \
  https://odin.marcusgoll.com/app/app.js \
  https://odin.marcusgoll.com/app/manifest.webmanifest

curl -sk -o /tmp/odin-route-body -w '%{http_code} %{content_type}\n' \
  https://odin.marcusgoll.com/mobile/status \
  https://odin.marcusgoll.com/mobile/overview \
  https://odin.marcusgoll.com/mobile/review-queue \
  https://odin.marcusgoll.com/mobile/browser/status \
  https://odin.marcusgoll.com/healthz \
  https://odin.marcusgoll.com/readyz \
  https://odin.marcusgoll.com/metrics \
  https://odin.marcusgoll.com/api/v1/status \
  https://odin.marcusgoll.com/admin
```

Expected post-cutover public behavior:

- `/`, `/app/`, `/app/app.js`, and the manifest stay reachable.
- Concrete `/mobile/*` routes stay authenticated or method-gated.
- `/healthz` returns HTTP `200`.
- `/readyz` must return HTTP `200` before the live release is credited as
  ready.
- `/browser/session/handoff?handoff_id=<requested-handoff-id>` must return HTTP
  `200` through the operator ingress for a current requested login handoff.
- `/metrics`, `/api/v1/*`, `/api/health`, and `/admin` return `404` at nginx.

## Trigger Dispatch Reproof

After the cutover is installed and readiness is healthy, prove the installed
release creates dispatchable trigger work:

```bash
docker exec odin-overseer /app/bin/odin trigger show production-readiness-daily-check --json
docker exec odin-overseer /app/bin/odin trigger audit production-readiness-daily-check --json
docker exec odin-overseer /app/bin/odin scheduler tick now=2026-05-16T15:00:01Z recovery=false --json
docker exec odin-overseer /app/bin/odin jobs --json
docker exec odin-overseer /app/bin/odin runs --json
docker exec odin-overseer /app/bin/odin review list --json
docker exec odin-overseer /app/bin/odin overview --json
```

The proof passes only if the materialized work item has acceptance criteria and
does not fail with `template "go-orchestrator" requires acceptance criteria
before dispatch`.

## Rollback Commands

Rollback uses the preserved target and verified backup. Run only when a stop
condition is hit:

```bash
previous_target="$(cat "$HOME/.local/state/odin-os/last-release-target.txt")"
test -n "$previous_target"
ln -sfnT "$previous_target" "$HOME/odin-os-live"
/home/orchestrator/.homelab-runtime/odin-pwa-proxy/run-container.sh
curl -fsS http://127.0.0.1:5173/healthz
curl -fsS http://127.0.0.1:5173/readyz
```

Do not restore over `/home/orchestrator/.local/state/odin-os` without a second
explicit approval. If state inspection is needed, restore into a temporary root
and inspect it there.

## Stop Conditions

Stop or rollback if any of these occur:

- backup creation or `verify-backup` fails;
- the previous release target is empty, missing, or not executable;
- `odin-overseer` does not restart cleanly;
- `docker exec odin-overseer /app/bin/odin doctor --json` is degraded for an
  unexplained reason;
- the live `odin serve` process environment does not include
  `/home/orchestrator/.npm-global/bin` in `PATH`, or doctor reports
  `workspace_prerequisites.codex_missing=true`;
- `healthcheck`, `/healthz`, or `/readyz` fails after the container has started;
- public denied routes no longer return `404`;
- mobile routes bypass authentication;
- trigger materialization still creates a task without acceptance criteria;
- more than one `odin serve` controller owns the production runtime root.

## Remaining Non-Cutover Risks

Even after a successful release install, Odin OS is not broadly production-ready
until these are resolved or explicitly accepted:

- the 45 active follow-up obligations are resolved or intentionally closed;
- authenticated browser attach and open soak are implemented and proven, or the
  fail-closed boundary remains the documented production stance;
- executor/provider breadth is promoted beyond `codex_headless`, or the
  provider readiness envelope remains the documented production stance.
