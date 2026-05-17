#!/usr/bin/env bash
set -euo pipefail

container_name="${ODIN_OVERSEER_CONTAINER_NAME:-odin-overseer}"
env_file="${ODIN_OVERSEER_ENV_FILE:-$HOME/.config/odin/odin-os.env}"
release_link="${ODIN_OVERSEER_RELEASE_LINK:-$HOME/odin-os-live}"
release_root="$(readlink -f "$release_link")"
runtime_root="${ODIN_OVERSEER_RUNTIME_ROOT:-$HOME/.local/state/odin-os}"
repo_root="${ODIN_OVERSEER_REPO_ROOT:-$HOME/odin-os}"
nginx_config="${ODIN_OVERSEER_NGINX_CONFIG:-$release_root/deploy/nginx/odin-pwa-proxy.conf}"
network="${ODIN_OVERSEER_NETWORK:-infrastructure_default}"
monitoring_network="${ODIN_OVERSEER_MONITORING_NETWORK:-odin-monitoring_default}"
container_user="${ODIN_OVERSEER_USER:-0:0}"
memory_limit="${ODIN_OVERSEER_MEMORY:-8g}"
memory_swap="${ODIN_OVERSEER_MEMORY_SWAP:-9g}"
gomemlimit="${ODIN_OVERSEER_GOMEMLIMIT:-6144MiB}"
gogc="${ODIN_OVERSEER_GOGC:-100}"
projects_overlay="${ODIN_PROJECTS_OVERLAY:-$HOME/.config/odin/odin-os-projects.local.yaml}"
codex_driver="${ODIN_CODEX_DRIVER:-$HOME/.config/odin/odin-codex-live-driver.sh}"
handoff_base_url="${ODIN_BROWSER_HANDOFF_BASE_URL:-https://odin.marcusgoll.com/browser/session/handoff}"
family_ops_worktree="${ODIN_FAMILY_OPS_CUTOVER_WORKTREE:-$HOME/.config/superpowers/worktrees/family-ops/odin-os-cutover-main}"
msmtp_bin="${ODIN_EMAIL_ACTION_MSMTP_BIN:-/usr/bin/msmtp}"
msmtp_config="${ODIN_EMAIL_ACTION_MSMTP_CONFIG:-/etc/msmtprc}"

if [[ ! -f "$env_file" ]]; then
  echo "missing env file: $env_file" >&2
  exit 1
fi
if [[ -z "$release_root" || ! -d "$release_root" ]]; then
  echo "missing release root: $release_link" >&2
  exit 1
fi
if [[ ! -x "$release_root/bin/odin" ]]; then
  echo "missing Odin binary at $release_root/bin/odin" >&2
  exit 1
fi
if [[ ! -f "$nginx_config" ]]; then
  echo "missing nginx config: $nginx_config" >&2
  exit 1
fi

email_mounts=()
if [[ -x "$msmtp_bin" ]]; then
  email_mounts+=(-v "$msmtp_bin:/usr/bin/msmtp:ro")
fi
if [[ -f "$msmtp_config" ]]; then
  email_mounts+=(-v "$msmtp_config:/etc/msmtprc:ro")
fi
if [[ -d /etc/ssl/certs ]]; then
  email_mounts+=(-v /etc/ssl/certs:/etc/ssl/certs:ro)
fi

if docker ps -a --format '{{.Names}}' | grep -qx "$container_name"; then
  docker rm -f "$container_name" >/dev/null
fi

docker_args=(-d --name "$container_name" --restart unless-stopped)
if [[ "$network" == "host" ]]; then
  docker_args+=(--network host --cap-add NET_BIND_SERVICE)
else
  docker_args+=(--network "$network")
fi

docker run "${docker_args[@]}" \
  --env-file "$env_file" \
  --user "$container_user" \
  --memory "$memory_limit" \
  --memory-swap "$memory_swap" \
  -w /app \
  -e HOME="$HOME" \
  -e PATH="$HOME/.npm-global/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin" \
  -e PYTHONPATH="$HOME/.local/lib/python3.12/site-packages" \
  -e GOMEMLIMIT="$gomemlimit" \
  -e GOGC="$gogc" \
  -e ODIN_ROOT="$runtime_root" \
  -e ODIN_HTTP_ADDR=127.0.0.1:9444 \
  -e ODIN_PROJECTS_OVERLAY="$projects_overlay" \
  -e ODIN_CODEX_DRIVER="$codex_driver" \
  -e ODIN_CORE_GIT_ROOT="$repo_root" \
  -e ODIN_BROWSER_HANDOFF_BASE_URL="$handoff_base_url" \
  -v "$release_root:/app:ro" \
  -v "$runtime_root:$runtime_root" \
  -v "$HOME/.config/odin:$HOME/.config/odin:ro" \
  -v "$HOME/.config/odin:/config:ro" \
  -v "$HOME/.local/bin:$HOME/.local/bin:ro" \
  -v "$HOME/.local/lib:$HOME/.local/lib:ro" \
  -v "$HOME/.npm-global:$HOME/.npm-global:ro" \
  -v "$repo_root:$repo_root:ro" \
  -v "$HOME/pbs:$HOME/pbs:ro" \
  -v "$HOME/cfipros:$HOME/cfipros:ro" \
  -v "$HOME/marcusgoll:$HOME/marcusgoll:ro" \
  -v "$family_ops_worktree:$family_ops_worktree:ro" \
  "${email_mounts[@]}" \
  -v /var/odin/browser-state/novnc-root:/var/odin/browser-state/novnc-root:ro \
  -v /opt/google/chrome:/opt/google/chrome:ro \
  -v /etc/alternatives/google-chrome:/etc/alternatives/google-chrome:ro \
  -v /bin/bash:/bin/bash:ro \
  -v /usr/bin/bash:/usr/bin/bash:ro \
  -v /usr/bin/google-chrome:/usr/bin/google-chrome:ro \
  -v /usr/bin/google-chrome-stable:/usr/bin/google-chrome-stable:ro \
  -v /usr/bin/Xvfb:/usr/bin/Xvfb:ro \
  -v /usr/bin/x11vnc:/usr/bin/x11vnc:ro \
  -v /usr/bin/xwininfo:/usr/bin/xwininfo:ro \
  -v /usr/bin/xkbcomp:/usr/bin/xkbcomp:ro \
  -v /usr/bin/node:/usr/bin/node:ro \
  -v /usr/bin/python3:/usr/bin/python3:ro \
  -v /usr/bin/python3.12:/usr/bin/python3.12:ro \
  -v /usr/bin/tmux:/usr/bin/tmux:ro \
  -v /usr/lib/python3.12:/usr/lib/python3.12:ro \
  -v /usr/share/X11:/usr/share/X11:ro \
  -v /usr/share/fonts:/usr/share/fonts:ro \
  -v /lib/x86_64-linux-gnu:/lib/x86_64-linux-gnu:ro \
  -v /usr/lib/x86_64-linux-gnu:/usr/lib/x86_64-linux-gnu:ro \
  -v /lib64/ld-linux-x86-64.so.2:/lib64/ld-linux-x86-64.so.2:ro \
  -v "$nginx_config:/etc/nginx/nginx.conf:ro" \
  --entrypoint /bin/sh \
  nginx:alpine \
  -c 'export PATH="$HOME/.npm-global/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"; export PYTHONPATH="$HOME/.local/lib/python3.12/site-packages"; mkdir -p /tmp/nginx-client-body /tmp/nginx-proxy /tmp/nginx-fastcgi /tmp/nginx-uwsgi /tmp/nginx-scgi; /app/bin/odin serve & odin_pid=$!; trap "kill $odin_pid 2>/dev/null || true" TERM INT; exec nginx -c /etc/nginx/nginx.conf -g "daemon off;"'

if [[ "$network" != "host" ]] && [[ -n "$monitoring_network" ]] && docker network inspect "$monitoring_network" >/dev/null 2>&1; then
  docker network connect --alias "$container_name" "$monitoring_network" "$container_name" >/dev/null
fi
