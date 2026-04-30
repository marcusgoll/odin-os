#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: scripts/install-service.sh [--start] [--force] [--dry-run]

Installs the canonical user systemd service as odin-os.service. The service is
installed but not started unless --start is provided.

Environment overrides:
  ODIN_SERVICE_NAME             Service file name (default: odin-os.service)
  ODIN_SERVICE_SOURCE           Source unit file
  ODIN_ENV_SOURCE               Source env template
  ODIN_CONFIG_HOME              Config root (default: ${XDG_CONFIG_HOME:-$HOME/.config})
  ODIN_INSTALL_SKIP_SYSTEMCTL   Set to 1 to copy files without systemctl calls
USAGE
}

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
service_name="${ODIN_SERVICE_NAME:-odin-os.service}"
service_source="${ODIN_SERVICE_SOURCE:-$repo_root/deploy/systemd/odin-os.service}"
env_source="${ODIN_ENV_SOURCE:-$repo_root/deploy/systemd/odin-os.env.example}"
config_home="${ODIN_CONFIG_HOME:-${XDG_CONFIG_HOME:-$HOME/.config}}"
systemd_user_dir="${ODIN_SYSTEMD_USER_DIR:-$config_home/systemd/user}"
env_dir="${ODIN_ENV_DIR:-$config_home/odin}"
service_target="$systemd_user_dir/$service_name"
env_target="$env_dir/${service_name%.service}.env"
systemctl_cmd="${ODIN_SYSTEMCTL:-systemctl}"
start_service=0
force=0
dry_run=0

while (($#)); do
  case "$1" in
    --start)
      start_service=1
      ;;
    --force)
      force=1
      ;;
    --dry-run)
      dry_run=1
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
  shift
done

run() {
  if [[ "$dry_run" == "1" ]]; then
    printf 'dry-run:'
    printf ' %q' "$@"
    printf '\n'
    return 0
  fi
  "$@"
}

[[ -f "$service_source" ]] || { echo "missing service source: $service_source" >&2; exit 1; }
[[ -f "$env_source" ]] || { echo "missing env source: $env_source" >&2; exit 1; }

run mkdir -p "$systemd_user_dir" "$env_dir"
run cp "$service_source" "$service_target"
if [[ "$force" == "1" || ! -f "$env_target" ]]; then
  run cp "$env_source" "$env_target"
else
  echo "preserved existing env file: $env_target"
fi

if [[ "${ODIN_INSTALL_SKIP_SYSTEMCTL:-0}" != "1" ]]; then
  run "$systemctl_cmd" --user daemon-reload
  if [[ "$start_service" == "1" ]]; then
    run "$systemctl_cmd" --user enable --now "$service_name"
  else
    run "$systemctl_cmd" --user enable "$service_name"
  fi
fi

echo "installed $service_target"
echo "env file $env_target"
