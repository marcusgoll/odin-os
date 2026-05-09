#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

fail() {
  echo "test failed: $*" >&2
  exit 1
}

require_contains() {
  local path="$1"
  local expected="$2"
  grep -Fq -- "$expected" "$path" || fail "$path missing: $expected"
}

make_output="$(make -n -C "$repo_root" docker-smoke)"
grep -Fqx 'bash scripts/tests/docker-compose-smoke.sh' <<<"$make_output" || fail "missing docker-smoke target"

require_contains "$repo_root/deploy/docker/docker-compose.yml" 'ODIN_COMPOSE_HTTP_BIND'
require_contains "$repo_root/deploy/docker/Dockerfile" '/var/lib/odin-os'
require_contains "$repo_root/deploy/docker/Dockerfile" '--chown=nonroot:nonroot'
require_contains "$repo_root/scripts/tests/docker-compose-smoke.sh" 'docker compose'
require_contains "$repo_root/scripts/tests/docker-compose-smoke.sh" '/health'
require_contains "$repo_root/scripts/tests/docker-compose-smoke.sh" 'ReadonlyRootfs'
require_contains "$repo_root/scripts/tests/docker-compose-smoke.sh" 'CapDrop'
require_contains "$repo_root/scripts/tests/docker-compose-smoke.sh" 'no-new-privileges:true'
require_contains "$repo_root/docs/DEPLOYMENT.md" 'scripts/tests/docker-compose-smoke.sh'
require_contains "$repo_root/docs/DEPLOYMENT.md" 'Docker daemon'

echo "ok"
