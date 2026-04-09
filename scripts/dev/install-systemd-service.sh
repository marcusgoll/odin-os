#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
service_target="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user/odin.service"
env_target="${XDG_CONFIG_HOME:-$HOME/.config}/odin/odin.env"

mkdir -p "$(dirname "$service_target")" "$(dirname "$env_target")"
cp "$repo_root/deploy/systemd/odin.service" "$service_target"
if [[ ! -f "$env_target" ]]; then
  cp "$repo_root/deploy/systemd/odin.env.example" "$env_target"
fi

systemctl --user daemon-reload
systemctl --user enable --now odin.service

echo "installed $service_target"
