#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
compose_file="$repo_root/deploy/docker/docker-compose.yml"
project="${ODIN_DOCKER_SMOKE_PROJECT:-odin-os-smoke-$(date +%s)-$$}"
host="${ODIN_DOCKER_SMOKE_HOST:-127.0.0.1}"
port="${ODIN_DOCKER_SMOKE_PORT:-$((20000 + RANDOM % 40000))}"
tmpdir="$(mktemp -d)"

fail() {
  echo "docker smoke failed: $*" >&2
  exit 1
}

cleanup() {
  docker compose -p "$project" -f "$compose_file" down -v --remove-orphans >/dev/null 2>&1 || true
  rm -rf "$tmpdir"
}
trap cleanup EXIT

command -v docker >/dev/null 2>&1 || fail "docker CLI is required"
docker compose version >/dev/null 2>&1 || fail "docker compose plugin is required"
command -v curl >/dev/null 2>&1 || fail "curl is required"

env_file="$tmpdir/odin-os.env"
cat >"$env_file" <<'ENV'
# Non-secret Docker smoke fixture.
ODIN_ROOT=/var/lib/odin-os
ODIN_HTTP_ADDR=0.0.0.0:9444
ODIN_ADMIN_TOKEN=
ENV

export ODIN_COMPOSE_ENV_FILE="$env_file"
export ODIN_COMPOSE_HTTP_BIND="${host}:${port}"

docker compose -p "$project" -f "$compose_file" build odin-os
docker compose -p "$project" -f "$compose_file" up -d odin-os

cid="$(docker compose -p "$project" -f "$compose_file" ps -q odin-os)"
[[ -n "$cid" ]] || fail "compose service did not create a container"

health_url="http://${host}:${port}/health"
curl_error="$tmpdir/curl.err"
for _ in {1..60}; do
  if curl -fsS "$health_url" >"$tmpdir/health.json" 2>"$curl_error"; then
    break
  fi
  sleep 1
done
if [[ ! -s "$tmpdir/health.json" ]]; then
  cat "$curl_error" >&2 || true
  docker compose -p "$project" -f "$compose_file" logs --no-color odin-os >&2 || true
  fail "/health did not respond at $health_url"
fi

container_user="$(docker inspect -f '{{.Config.User}}' "$cid")"
case "$container_user" in
  65532:65532|nonroot:nonroot) ;;
  *) fail "container user = $container_user, want non-root user" ;;
esac

readonly_root="$(docker inspect -f '{{.HostConfig.ReadonlyRootfs}}' "$cid")"
[[ "$readonly_root" == "true" ]] || fail "ReadonlyRootfs = $readonly_root, want true"

cap_drop="$(docker inspect -f '{{json .HostConfig.CapDrop}}' "$cid")"
[[ "$cap_drop" == *'"ALL"'* ]] || fail "CapDrop = $cap_drop, want ALL"

security_opt="$(docker inspect -f '{{json .HostConfig.SecurityOpt}}' "$cid")"
[[ "$security_opt" == *'"no-new-privileges:true"'* ]] || fail "SecurityOpt = $security_opt, want no-new-privileges:true"

printf 'ok docker smoke project=%s url=%s user=%s readonly=%s cap_drop=%s security_opt=%s\n' \
  "$project" "$health_url" "$container_user" "$readonly_root" "$cap_drop" "$security_opt"
