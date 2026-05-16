#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${repo_root}"

release_sha="$(git rev-parse --short=12 HEAD)"
release_root="${ODIN_DRY_RUN_RELEASE_ROOT:-$HOME/.local/share/odin-os/releases/${release_sha}}"
live_link="${ODIN_LIVE_LINK:-$HOME/odin-os-live}"
service_name="${ODIN_SERVICE_NAME:-odin-os.service}"
proof_tmp="$(mktemp -d)"
cleanup() {
  rm -rf "${proof_tmp}"
}
trap cleanup EXIT

quote_command() {
  printf 'dry-run:'
  printf ' %q' "$@"
  printf '\n'
}

require_file() {
  local path="$1"
  [[ -f "${path}" ]] || {
    echo "homelab-release-dry-run: missing required file: ${path}" >&2
    exit 1
  }
}

require_executable() {
  local path="$1"
  [[ -x "${path}" ]] || {
    echo "homelab-release-dry-run: missing executable: ${path}" >&2
    exit 1
  }
}

echo "== checkout =="
git status --short --branch
git worktree list

require_file "deploy/systemd/odin-os.service"
require_file "deploy/systemd/odin-os.env.example"
require_file "deploy/nginx/odin-pwa-proxy.conf"
require_executable "scripts/install-service.sh"
require_executable "scripts/start.sh"
require_executable "scripts/stop.sh"
require_executable "scripts/healthcheck.sh"

echo "== build =="
if [[ "${ODIN_HOMELAB_RELEASE_SKIP_BUILD:-0}" == "1" ]]; then
  echo "skipped by ODIN_HOMELAB_RELEASE_SKIP_BUILD=1"
else
  make build
fi
require_executable "./bin/odin"

echo "== command help =="
./bin/odin backup --help
./bin/odin restore --help
./bin/odin verify-backup --help
./bin/odin serve --help

echo "== install dry-run =="
ODIN_CONFIG_HOME="${proof_tmp}/config" scripts/install-service.sh --dry-run --start

echo "== update dry-run =="
quote_command mkdir -p "${release_root}"
quote_command rsync -a --delete --exclude .git --exclude .odin --exclude .worktrees "${repo_root}/" "${release_root}/"
quote_command ln -sfn "${release_root}" "${live_link}"
quote_command systemctl --user restart "${service_name}"

echo "== release gates on isolated runtime =="
runtime_root="${proof_tmp}/runtime"
mkdir -p "${runtime_root}"
ODIN_ROOT="${runtime_root}" ./bin/odin doctor --json >/dev/null

set +e
health_output="$(ODIN_ROOT="${runtime_root}" ./bin/odin healthcheck 2>&1)"
health_status=$?
set -e
[[ "${health_status}" -ne 0 ]] || {
  echo "homelab-release-dry-run: fresh runtime unexpectedly ready before serve" >&2
  exit 1
}
grep -Eq "not ready|runtime not ready|no live odin serve process" <<<"${health_output}" || {
  echo "homelab-release-dry-run: healthcheck did not fail closed with an explanatory message" >&2
  printf '%s\n' "${health_output}" >&2
  exit 1
}
printf '%s\n' "${health_output}"

ODIN_ROOT="${runtime_root}" ./bin/odin overview --json >/dev/null
ODIN_ROOT="${runtime_root}" ./bin/odin work status --json >/dev/null
ODIN_ROOT="${runtime_root}" ./bin/odin review list --json >/dev/null
ODIN_ROOT="${runtime_root}" ./bin/odin approvals all --json >/dev/null
ODIN_ROOT="${runtime_root}" ./bin/odin logs --json >/dev/null

echo "homelab release dry-run ok"
