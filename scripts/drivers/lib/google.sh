#!/usr/bin/env bash
# Minimal Google Workspace auth layer for odin-os live drivers.

[[ "${BASH_SOURCE[0]}" == "${0}" ]] && set -euo pipefail

ODIN_DIR="${ODIN_DIR:-${HOME}/.odin}"
GOOGLE_TOKEN_ENDPOINT="${GOOGLE_TOKEN_ENDPOINT:-https://oauth2.googleapis.com/token}"
GOOGLE_TOKEN_CACHE="${GOOGLE_TOKEN_CACHE:-${ODIN_DIR}/google-token-cache.json}"

_GOOGLE_ACCESS_TOKEN="${_GOOGLE_ACCESS_TOKEN:-}"
_GOOGLE_TOKEN_EXPIRY="${_GOOGLE_TOKEN_EXPIRY:-0}"

_google_load_env() {
    if [[ -n "${GOOGLE_OAUTH_CLIENT_ID:-}" && -n "${GOOGLE_OAUTH_CLIENT_SECRET:-}" && -n "${GOOGLE_OAUTH_REFRESH_TOKEN:-}" ]]; then
        return 0
    fi
    if [[ -f "${HOME}/.odin-env" ]]; then
        # shellcheck source=/dev/null
        source "${HOME}/.odin-env" >/dev/null 2>&1 || true
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
    python3 - "${GOOGLE_TOKEN_CACHE}" "${_GOOGLE_ACCESS_TOKEN}" "${_GOOGLE_TOKEN_EXPIRY}" <<'PY'
import json
import os
import sys

path, token, expiry = sys.argv[1:4]
tmp = f"{path}.tmp"
with open(tmp, "w", encoding="utf-8") as handle:
    json.dump({"access_token": token, "expiry": int(expiry)}, handle)
os.replace(tmp, path)
PY
}

_google_refresh_token() {
    _google_check_creds || return 1

    local response http_code response_body access_token expires_in now
    response="$(curl -sS -w '\n%{http_code}' \
        -X POST "${GOOGLE_TOKEN_ENDPOINT}" \
        -H "Content-Type: application/x-www-form-urlencoded" \
        -d "client_id=${GOOGLE_OAUTH_CLIENT_ID}" \
        -d "client_secret=${GOOGLE_OAUTH_CLIENT_SECRET}" \
        -d "refresh_token=${GOOGLE_OAUTH_REFRESH_TOKEN}" \
        -d "grant_type=refresh_token")" || return 3

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

    access_token="$(printf '%s' "${response_body}" | python3 - <<'PY'
import json
import sys
payload = json.load(sys.stdin)
print(str(payload.get("access_token") or ""))
PY
)"
    expires_in="$(printf '%s' "${response_body}" | python3 - <<'PY'
import json
import sys
payload = json.load(sys.stdin)
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
