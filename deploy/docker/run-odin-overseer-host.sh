#!/usr/bin/env bash
set -euo pipefail

container_name="${ODIN_OVERSEER_CONTAINER_NAME:-odin-overseer}"
env_file="${ODIN_OVERSEER_ENV_FILE:-$HOME/.config/odin/odin-os.env}"
release_link="${ODIN_OVERSEER_RELEASE_LINK:-$HOME/odin-os-live}"
runtime_root="${ODIN_OVERSEER_RUNTIME_ROOT:-$HOME/.local/state/odin-os}"
repo_root="${ODIN_OVERSEER_REPO_ROOT:-$HOME/odin-os}"
nginx_config="${ODIN_OVERSEER_NGINX_CONFIG:-$repo_root/deploy/nginx/odin-pwa-proxy.conf}"

if [[ ! -f "$env_file" ]]; then
  echo "missing env file: $env_file" >&2
  exit 1
fi
if [[ ! -x "$release_link/bin/odin" ]]; then
  echo "missing Odin binary at $release_link/bin/odin" >&2
  exit 1
fi
if [[ ! -f "$nginx_config" ]]; then
  echo "missing nginx config: $nginx_config" >&2
  exit 1
fi

if docker ps -a --format '{{.Names}}' | grep -qx "$container_name"; then
  docker rm -f "$container_name" >/dev/null
fi

docker run -d --name "$container_name" --restart unless-stopped \
  --network host \
  --cap-add NET_BIND_SERVICE \
  --env-file "$env_file" \
  --user "$(id -u):$(id -g)" \
  -w /app \
  -e HOME="$HOME" \
  -e PATH="$HOME/.npm-global/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin" \
  -e PYTHONPATH="$HOME/.local/lib/python3.12/site-packages" \
  -e ODIN_ROOT="$runtime_root" \
  -e ODIN_HTTP_ADDR=127.0.0.1:9444 \
  -e ODIN_CORE_GIT_ROOT="$repo_root" \
  -v "$release_link:/app:ro" \
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
