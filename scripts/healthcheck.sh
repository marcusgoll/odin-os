#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
env_file="${ODIN_ENV_FILE:-${XDG_CONFIG_HOME:-$HOME/.config}/odin/odin-os.env}"

if [[ -f "$env_file" ]]; then
  set -a
  # shellcheck disable=SC1090
  . "$env_file"
  set +a
fi

if [[ -n "${ODIN_BIN:-}" ]]; then
  odin_bin="$ODIN_BIN"
elif [[ -x "$HOME/odin-os-live/bin/odin" ]]; then
  odin_bin="$HOME/odin-os-live/bin/odin"
else
  odin_bin="$repo_root/bin/odin"
fi

exec "$odin_bin" healthcheck
