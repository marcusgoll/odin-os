#!/usr/bin/env bash
# Minimal Google Workspace auth layer for odin-os live drivers.

[[ "${BASH_SOURCE[0]}" == "${0}" ]] && set -euo pipefail

ODIN_DIR="${ODIN_DIR:-${HOME}/.odin}"
GOOGLE_TOKEN_ENDPOINT="${GOOGLE_TOKEN_ENDPOINT:-https://oauth2.googleapis.com/token}"
GOOGLE_TOKEN_CACHE="${GOOGLE_TOKEN_CACHE:-${ODIN_DIR}/google-token-cache.json}"

_GOOGLE_ACCESS_TOKEN="${_GOOGLE_ACCESS_TOKEN:-}"
_GOOGLE_TOKEN_EXPIRY="${_GOOGLE_TOKEN_EXPIRY:-0}"

_google_private_file_ok() {
    local path="$1"
    [[ -f "${path}" ]] || return 1
    python3 - "${path}" <<'PY'
import os
import stat
import sys

path = sys.argv[1]
try:
    info = os.lstat(path)
except OSError:
    raise SystemExit(1)

if not stat.S_ISREG(info.st_mode):
    raise SystemExit(1)
if info.st_uid != os.getuid():
    raise SystemExit(1)
if stat.S_IMODE(info.st_mode) & 0o077:
    raise SystemExit(1)
PY
}

_google_load_env_file() {
    local env_file="$1"
    local parsed key value

    _google_private_file_ok "${env_file}" || return 1
    if ! parsed="$(python3 - "${env_file}" <<'PY'
import shlex
import sys

allowed = {
    "GOOGLE_OAUTH_CLIENT_ID",
    "GOOGLE_OAUTH_CLIENT_SECRET",
    "GOOGLE_OAUTH_REFRESH_TOKEN",
}

with open(sys.argv[1], "r", encoding="utf-8") as handle:
    for raw in handle:
        line = raw.strip()
        if not line or line.startswith("#"):
            continue
        if line.startswith("export "):
            line = line[len("export "):].strip()
        try:
            parts = shlex.split(line, comments=True, posix=True)
        except ValueError:
            raise SystemExit(1)
        if len(parts) != 1 or "=" not in parts[0]:
            continue
        key, value = parts[0].split("=", 1)
        if key in allowed:
            print(f"{key}\t{value}")
PY
)"; then
        return 1
    fi

    while IFS=$'\t' read -r key value; do
        [[ -n "${key}" ]] || continue
        case "${key}" in
            GOOGLE_OAUTH_CLIENT_ID|GOOGLE_OAUTH_CLIENT_SECRET|GOOGLE_OAUTH_REFRESH_TOKEN)
                printf -v "${key}" '%s' "${value}"
                export "${key}"
                ;;
        esac
    done <<<"${parsed}"
}

_google_load_env() {
    if [[ -n "${GOOGLE_OAUTH_CLIENT_ID:-}" && -n "${GOOGLE_OAUTH_CLIENT_SECRET:-}" && -n "${GOOGLE_OAUTH_REFRESH_TOKEN:-}" ]]; then
        return 0
    fi
    if [[ -f "${HOME}/.odin-env" ]]; then
        _google_load_env_file "${HOME}/.odin-env" || return 1
    fi
}

_google_check_creds() {
    _google_load_env
    [[ -n "${GOOGLE_OAUTH_CLIENT_ID:-}" ]] || return 1
    [[ -n "${GOOGLE_OAUTH_CLIENT_SECRET:-}" ]] || return 1
    [[ -n "${GOOGLE_OAUTH_REFRESH_TOKEN:-}" ]] || return 1
}

_google_cache_read() {
    local cache_file="${GOOGLE_TOKEN_CACHE}"
    [[ -f "${cache_file}" ]] || return 1
    _google_private_file_ok "${cache_file}" || return 1

    local fields
    if ! fields="$(python3 - "${cache_file}" <<'PY'
import json
import sys

path = sys.argv[1]
try:
    with open(path, "r", encoding="utf-8") as handle:
        payload = json.load(handle)
except Exception:
    raise SystemExit(1)

token = str(payload.get("access_token") or "")
expiry = int(payload.get("expiry") or 0)
if not token:
    raise SystemExit(1)
print(token)
print(expiry)
PY
)"; then
        return 1
    fi

    local cached_token cached_expiry now
    cached_token="$(printf '%s\n' "${fields}" | sed -n '1p')"
    cached_expiry="$(printf '%s\n' "${fields}" | sed -n '2p')"
    now="$(date +%s)"
    if (( cached_expiry <= now + 60 )); then
        return 1
    fi

    _GOOGLE_ACCESS_TOKEN="${cached_token}"
    _GOOGLE_TOKEN_EXPIRY="${cached_expiry}"
}

_google_cache_write() {
    mkdir -p "$(dirname "${GOOGLE_TOKEN_CACHE}")" >/dev/null 2>&1 || true
    (
        umask 077
        python3 - "${GOOGLE_TOKEN_CACHE}" "${_GOOGLE_ACCESS_TOKEN}" "${_GOOGLE_TOKEN_EXPIRY}" <<'PY'
import json
import os
import sys

path, token, expiry = sys.argv[1:4]
tmp = f"{path}.tmp"
try:
    os.unlink(tmp)
except FileNotFoundError:
    pass
fd = os.open(tmp, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
with os.fdopen(fd, "w", encoding="utf-8") as handle:
    json.dump({"access_token": token, "expiry": int(expiry)}, handle)
os.chmod(tmp, 0o600)
os.replace(tmp, path)
os.chmod(path, 0o600)
PY
    )
}

_google_refresh_token() {
    _google_check_creds || return 1

    local response http_code response_body access_token expires_in now
    response="$(curl -sS -w '\n%{http_code}' \
        -X POST \
        -H "Content-Type: application/x-www-form-urlencoded" \
        -d "client_id=${GOOGLE_OAUTH_CLIENT_ID}" \
        -d "client_secret=${GOOGLE_OAUTH_CLIENT_SECRET}" \
        -d "refresh_token=${GOOGLE_OAUTH_REFRESH_TOKEN}" \
        -d "grant_type=refresh_token" \
        "${GOOGLE_TOKEN_ENDPOINT}")" || return 3

    http_code="$(printf '%s' "${response}" | tail -n1)"
    response_body="$(printf '%s' "${response}" | sed '$d')"

    if [[ "${http_code}" == "429" ]]; then
        return 2
    fi
    if [[ "${http_code}" -ge 500 ]] 2>/dev/null; then
        return 3
    fi
    if [[ "${http_code}" -ge 400 ]] 2>/dev/null; then
        return 1
    fi

    access_token="$(RESPONSE_BODY="${response_body}" python3 - <<'PY'
import json
import os

payload = json.loads(os.environ["RESPONSE_BODY"])
print(str(payload.get("access_token") or ""))
PY
)"
    expires_in="$(RESPONSE_BODY="${response_body}" python3 - <<'PY'
import json
import os

payload = json.loads(os.environ["RESPONSE_BODY"])
print(int(payload.get("expires_in") or 3600))
PY
)"
    [[ -n "${access_token}" ]] || return 1

    now="$(date +%s)"
    _GOOGLE_ACCESS_TOKEN="${access_token}"
    _GOOGLE_TOKEN_EXPIRY=$(( now + expires_in ))
    _google_cache_write >/dev/null 2>&1 || true
    return 0
}

google_ensure_token() {
    local now
    now="$(date +%s)"
    if [[ -n "${_GOOGLE_ACCESS_TOKEN}" ]] && (( _GOOGLE_TOKEN_EXPIRY > now + 60 )); then
        return 0
    fi
    _google_cache_read && return 0
    _google_refresh_token
}

google_api_call() {
    local method="$1"
    local url="$2"
    local body="${3:-}"

    if [[ -n "${ODIN_TEST_GOOGLE_RESPONSE:-}" ]]; then
        if [[ -n "${ODIN_TEST_GOOGLE_CALL_PATH:-}" ]]; then
            printf '%s\t%s\n' "${method}" "${url}" >"${ODIN_TEST_GOOGLE_CALL_PATH}"
        fi
        printf '%s' "${ODIN_TEST_GOOGLE_RESPONSE}"
        return "${ODIN_TEST_GOOGLE_EXIT_STATUS:-0}"
    fi

    google_ensure_token || return $?

    local response http_code response_body
    _google_do_call() {
        local args=(-sS -w '\n%{http_code}' -X "${method}" -H "Authorization: Bearer ${_GOOGLE_ACCESS_TOKEN}")
        if [[ -n "${body}" ]]; then
            args+=(-H "Content-Type: application/json" -d "${body}")
        fi
        curl "${args[@]}" "${url}"
    }

    response="$(_google_do_call)" || return 3
    http_code="$(printf '%s' "${response}" | tail -n1)"
    response_body="$(printf '%s' "${response}" | sed '$d')"

    if [[ "${http_code}" == "401" ]]; then
        _GOOGLE_ACCESS_TOKEN=""
        _GOOGLE_TOKEN_EXPIRY=0
        _google_refresh_token || return $?
        response="$(_google_do_call)" || return 3
        http_code="$(printf '%s' "${response}" | tail -n1)"
        response_body="$(printf '%s' "${response}" | sed '$d')"
    fi

    if [[ "${http_code}" == "429" ]]; then
        printf '%s\n' "${response_body}"
        return 2
    fi
    if [[ "${http_code}" -ge 500 ]] 2>/dev/null; then
        printf '%s\n' "${response_body}"
        return 3
    fi
    if [[ "${http_code}" -ge 400 ]] 2>/dev/null; then
        printf '%s\n' "${response_body}"
        return 1
    fi

    printf '%s\n' "${response_body}"
    return 0
}
