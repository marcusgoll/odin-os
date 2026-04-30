#!/usr/bin/env bash
set -euo pipefail

service_name="${ODIN_SERVICE_NAME:-odin-os.service}"
systemctl_cmd="${ODIN_SYSTEMCTL:-systemctl}"

exec "$systemctl_cmd" --user start "$service_name"
