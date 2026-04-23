#!/usr/bin/env bash

[[ -n "${_ODIN_BROWSER_AUTH_LOADED:-}" ]] && return 0
_ODIN_BROWSER_AUTH_LOADED=1

ODIN_DIR="${ODIN_DIR:-${ODIN_ROOT:-${HOME}/.odin}}"
CREDENTIAL_DIR="${ODIN_DIR}/browser-state/credentials"
CREDENTIAL_KEY_FILE="${ODIN_DIR}/sessions/master.key"

auth_check_login_page() {
    local snapshot="${1:-}" lower_snapshot
    [[ -n "${snapshot}" ]] || return 1
    lower_snapshot="$(tr '[:upper:]' '[:lower:]' <<<"${snapshot}")"

    [[ "${lower_snapshot}" == *"sign in"* ]] || \
    [[ "${lower_snapshot}" == *"log in"* ]] || \
    [[ "${lower_snapshot}" == *"login"* ]] || \
    [[ "${lower_snapshot}" == *"welcome back"* ]] || \
    return 1

    [[ "${lower_snapshot}" == *"email"* ]] || \
    [[ "${lower_snapshot}" == *"password"* ]] || \
    [[ "${lower_snapshot}" == *"continue"* ]]
}

auth_detect_login_form() {
    local snapshot="${1:-}"

    [[ -n "${snapshot}" ]] || return 1

    local username_ref="" password_ref="" submit_ref=""
    local -a oauth_providers=()

    username_ref="$(printf '%s' "${snapshot}" | grep -iE '\[ref=e[0-9]+\].*(textbox|input).*(email|username|login|user)' | head -1 | grep -oE 'ref=e[0-9]+' | cut -d= -f2 | head -1 || true)"
    password_ref="$(printf '%s' "${snapshot}" | grep -iE '\[ref=e[0-9]+\].*(textbox|input).*(password|passwd)' | head -1 | grep -oE 'ref=e[0-9]+' | cut -d= -f2 | head -1 || true)"
    submit_ref="$(printf '%s' "${snapshot}" | grep -iE '\[ref=e[0-9]+\].*(button|link).*(sign in|log in|login|submit|continue|next)' | head -1 | grep -oE 'ref=e[0-9]+' | cut -d= -f2 | head -1 || true)"

    if printf '%s' "${snapshot}" | grep -qi 'sign in with google\|continue with google'; then
        oauth_providers+=("google")
    fi
    if printf '%s' "${snapshot}" | grep -qi 'sign in with github\|continue with github'; then
        oauth_providers+=("github")
    fi
    if printf '%s' "${snapshot}" | grep -qi 'sign in with microsoft\|continue with microsoft'; then
        oauth_providers+=("microsoft")
    fi

    if [[ -z "${password_ref}" ]] && (( ${#oauth_providers[@]} == 0 )); then
        return 1
    fi

    local oauth_json="[]"
    if (( ${#oauth_providers[@]} > 0 )); then
        oauth_json="$(printf '%s\n' "${oauth_providers[@]}" | jq -R . | jq -s .)"
    fi

    jq -n \
        --arg uref "${username_ref}" \
        --arg pref "${password_ref}" \
        --arg sref "${submit_ref}" \
        --argjson oauth "${oauth_json}" \
        '{username_ref: $uref, password_ref: $pref, submit_ref: $sref, oauth: $oauth}'
}

auth_detect_oauth_provider() {
    case "${1:-}" in
        *accounts.google.com*) printf 'google'; return 0 ;;
        *github.com/login*) printf 'github'; return 0 ;;
        *login.microsoftonline.com*) printf 'microsoft'; return 0 ;;
        *) return 1 ;;
    esac
}

auth_check_2fa_page() {
    local snapshot="${1:-}" lower_snapshot
    [[ -n "${snapshot}" ]] || return 1
    lower_snapshot="$(tr '[:upper:]' '[:lower:]' <<<"${snapshot}")"

    [[ "${lower_snapshot}" == *"verification code"* ]] || \
    [[ "${lower_snapshot}" == *"authenticator"* ]] || \
    [[ "${lower_snapshot}" == *"two-factor"* ]] || \
    [[ "${lower_snapshot}" == *"two factor"* ]] || \
    [[ "${lower_snapshot}" == *"mfa"* ]]
}

auth_get_credential() {
    local domain="${1:-}" enc_file tmp_plain="" previous_umask

    [[ -n "${domain}" ]] || return 1
    mkdir -p "${CREDENTIAL_DIR}" 2>/dev/null || true
    enc_file="${CREDENTIAL_DIR}/${domain}.enc"
    [[ -f "${enc_file}" ]] || return 1
    [[ -s "${CREDENTIAL_KEY_FILE}" ]] || return 1

    previous_umask="$(umask)"
    umask 077
    tmp_plain="$(mktemp "${CREDENTIAL_DIR}/.decrypt-XXXXXX")"
    umask "${previous_umask}"
    trap 'rm -f "${tmp_plain}"' RETURN

    if ! openssl enc -d -aes-256-cbc -pbkdf2 \
        -pass "file:${CREDENTIAL_KEY_FILE}" \
        -in "${enc_file}" \
        -out "${tmp_plain}" 2>/dev/null; then
        return 1
    fi

    jq -e 'type == "object"' "${tmp_plain}" >/dev/null 2>&1 || return 1
    cat "${tmp_plain}"
}
