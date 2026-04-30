#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

ODIN_CONFIG_HOME="$tmpdir/config" \
ODIN_INSTALL_SKIP_SYSTEMCTL=1 \
"$repo_root/scripts/install-service.sh"

service_file="$tmpdir/config/systemd/user/odin-os.service"
env_file="$tmpdir/config/odin/odin-os.env"

[[ -f "$service_file" ]] || { echo "missing installed service" >&2; exit 1; }
[[ -f "$env_file" ]] || { echo "missing installed env" >&2; exit 1; }

grep -Fq 'ExecStart=%h/odin-os-live/bin/odin serve' "$service_file"
grep -Fq 'NoNewPrivileges=true' "$service_file"
grep -Fq 'EnvironmentFile=%h/.config/odin/odin-os.env' "$service_file"
grep -Fq 'ODIN_ROOT=' "$env_file"
grep -Fq 'ODIN_HTTP_ADDR=' "$env_file"
! grep -Eq 'TOKEN=.*[^[:space:]]' "$env_file"

printf 'ODIN_ROOT=/custom\n' >"$env_file"
ODIN_CONFIG_HOME="$tmpdir/config" \
ODIN_INSTALL_SKIP_SYSTEMCTL=1 \
"$repo_root/scripts/install-service.sh" >/tmp/odin-install-service-test.log
grep -Fq 'ODIN_ROOT=/custom' "$env_file"

ODIN_CONFIG_HOME="$tmpdir/config" \
ODIN_INSTALL_SKIP_SYSTEMCTL=1 \
"$repo_root/scripts/install-service.sh" --force >/tmp/odin-install-service-force-test.log
grep -Fq 'ODIN_HTTP_ADDR=127.0.0.1:9444' "$env_file"

ODIN_CONFIG_HOME="$tmpdir/config" \
"$repo_root/scripts/install-service.sh" --dry-run --start >/tmp/odin-install-service-dry-run-test.log
grep -Fq 'dry-run:' /tmp/odin-install-service-dry-run-test.log
grep -Fq 'enable' /tmp/odin-install-service-dry-run-test.log

echo "ok"
