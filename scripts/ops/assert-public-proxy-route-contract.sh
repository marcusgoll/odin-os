#!/usr/bin/env bash
set -euo pipefail

config_path="${1:-}"

fail() {
  echo "public-proxy-route-contract: $*" >&2
  exit 1
}

require_line() {
  local pattern="$1"
  local message="$2"
  grep -Eq "$pattern" "$config_path" || fail "$message"
}

forbid_line() {
  local pattern="$1"
  local message="$2"
  if grep -Eq "$pattern" "$config_path"; then
    fail "$message"
  fi
}

[[ -n "$config_path" ]] || fail "usage: $0 <nginx-config>"
[[ -f "$config_path" ]] || fail "missing nginx config: $config_path"

require_line 'location[[:space:]]*=[[:space:]]*/[[:space:]]*\{' "missing exact root redirect route"
require_line 'return[[:space:]]+302[[:space:]]+/app/;' "root route must redirect to /app/"
require_line 'location[[:space:]]*=[[:space:]]*/app[[:space:]]*\{' "missing exact /app redirect route"
require_line 'location[[:space:]]+/app/[[:space:]]*\{' "missing /app/ PWA asset route"
require_line 'location[[:space:]]*=[[:space:]]*/pwa[[:space:]]*\{' "missing exact /pwa email action target route"
require_line 'location[[:space:]]+/mobile/[[:space:]]*\{' "missing /mobile/ API route"
require_line 'location[[:space:]]*=[[:space:]]*/mobile[[:space:]]*\{' "missing exact /mobile route"
require_line 'location[[:space:]]*=[[:space:]]*/healthz[[:space:]]*\{' "missing exact /healthz route"
require_line 'location[[:space:]]*=[[:space:]]*/readyz[[:space:]]*\{' "missing exact /readyz route"
require_line 'location[[:space:]]*=[[:space:]]*/metrics[[:space:]]*\{' "missing exact private /metrics route"
require_line 'allow[[:space:]]+127\.0\.0\.1;' "private /metrics route must allow loopback"
require_line 'allow[[:space:]]+192\.168\.96\.0/20;' "private /metrics route must allow the tailnet range"
require_line 'deny[[:space:]]+all;' "private routes must deny untrusted clients"
require_line 'error_page[[:space:]]+403[[:space:]]+=404[[:space:]]+/__not_found;' "denied routes must be masked as not found"
require_line 'location[[:space:]]*=[[:space:]]*/webhooks/n8n/intake[[:space:]]*\{' "missing exact n8n intake webhook route"
require_line 'location[[:space:]]+/email-actions/[[:space:]]*\{' "missing email action link route"
require_line 'location[[:space:]]*=[[:space:]]*/browser/session/handoff[[:space:]]*\{' "missing exact browser handoff route"
require_line 'location[[:space:]]*=[[:space:]]*/browser/session/handoff/viewer[[:space:]]*\{' "missing exact protected browser viewer route"
require_line 'location[[:space:]]+/browser/session/handoff/viewer/proxy/[[:space:]]*\{' "missing protected browser viewer proxy route"
require_line 'location[[:space:]]*=[[:space:]]*/browser/session/handoff/complete[[:space:]]*\{' "missing exact browser handoff completion route"
require_line 'location[[:space:]]+/[[:space:]]*\{' "missing final public catchall route"
require_line 'return[[:space:]]+404;' "final public catchall must fail closed"

forbid_line 'location[[:space:]]+/api/[[:space:]]*\{' "must not expose broad /api/ paths"
forbid_line 'location[[:space:]]+/browser/[[:space:]]*\{' "must not expose broad /browser/ paths"
forbid_line 'location[[:space:]]+/webhooks/[[:space:]]*\{' "must not expose broad /webhooks/ paths"
forbid_line 'location[[:space:]]+/email-actions[[:space:]]*\{' "must not expose an extension-prone /email-actions route without trailing slash"
forbid_line 'location[[:space:]]+/metrics[[:space:]]*\{' "must not expose broad /metrics paths"

echo "public proxy route contract ok: $config_path"
