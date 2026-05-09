#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
google_lib="$repo_root/scripts/drivers/lib/google.sh"

fail() {
  echo "test failed: $*" >&2
  exit 1
}

mode_of() {
  stat -c '%a' "$1"
}

future_expiry() {
  python3 - <<'PY'
import time
print(int(time.time()) + 3600)
PY
}

test_cache_write_uses_owner_only_mode() {
  local tmpdir cache mode
  tmpdir="$(mktemp -d)"
  cache="$tmpdir/odin/google-token-cache.json"

  HOME="$tmpdir/home" \
    ODIN_DIR="$tmpdir/odin" \
    GOOGLE_TOKEN_CACHE="$cache" \
    bash -c '
      set -euo pipefail
      source "$1"
      _GOOGLE_ACCESS_TOKEN="token"
      _GOOGLE_TOKEN_EXPIRY="$2"
      umask 022
      _google_cache_write
    ' _ "$google_lib" "$(future_expiry)"

  [[ -f "$cache" ]] || fail "token cache was not written"
  mode="$(mode_of "$cache")"
  [[ "$mode" == "600" ]] || fail "token cache mode = $mode, want 600"
}

test_cache_read_rejects_group_or_world_readable_cache() {
  local tmpdir cache
  tmpdir="$(mktemp -d)"
  cache="$tmpdir/odin/google-token-cache.json"
  mkdir -p "$(dirname "$cache")"
  printf '{"access_token":"token","expiry":%s}\n' "$(future_expiry)" >"$cache"
  chmod 0644 "$cache"

  if HOME="$tmpdir/home" ODIN_DIR="$tmpdir/odin" GOOGLE_TOKEN_CACHE="$cache" bash -c '
      source "$1"
      _google_cache_read
    ' _ "$google_lib"; then
    fail "world-readable token cache was accepted"
  fi
}

test_unsafe_odin_env_is_rejected_and_not_executed() {
  local tmpdir home pwned
  tmpdir="$(mktemp -d)"
  home="$tmpdir/home"
  pwned="$tmpdir/pwned"
  mkdir -p "$home"
  cat >"$home/.odin-env" <<EOF
GOOGLE_OAUTH_CLIENT_ID=client
GOOGLE_OAUTH_CLIENT_SECRET=secret
GOOGLE_OAUTH_REFRESH_TOKEN=refresh
touch "$pwned"
EOF
  chmod 0644 "$home/.odin-env"

  if HOME="$home" bash -c '
      source "$1"
      _google_check_creds
    ' _ "$google_lib"; then
    fail "unsafe .odin-env was accepted"
  fi
  [[ ! -e "$pwned" ]] || fail "unsafe .odin-env executed shell code"
}

test_private_odin_env_loads_without_shell_execution() {
  local tmpdir home pwned
  tmpdir="$(mktemp -d)"
  home="$tmpdir/home"
  pwned="$tmpdir/pwned"
  mkdir -p "$home"
  cat >"$home/.odin-env" <<EOF
export GOOGLE_OAUTH_CLIENT_ID="client"
GOOGLE_OAUTH_CLIENT_SECRET='secret'
GOOGLE_OAUTH_REFRESH_TOKEN=refresh
touch "$pwned"
EOF
  chmod 0600 "$home/.odin-env"

  HOME="$home" bash -c '
    set -euo pipefail
    source "$1"
    _google_check_creds
    [[ "$GOOGLE_OAUTH_CLIENT_ID" == "client" ]]
    [[ "$GOOGLE_OAUTH_CLIENT_SECRET" == "secret" ]]
    [[ "$GOOGLE_OAUTH_REFRESH_TOKEN" == "refresh" ]]
  ' _ "$google_lib"
  [[ ! -e "$pwned" ]] || fail "private .odin-env executed shell code"
}

test_cache_write_uses_owner_only_mode
test_cache_read_rejects_group_or_world_readable_cache
test_unsafe_odin_env_is_rejected_and_not_executed
test_private_odin_env_loads_without_shell_execution

echo "ok"
