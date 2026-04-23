#!/usr/bin/env bash
set -euo pipefail

ODIN_BIN="${ODIN_BIN:-odin}"
ODIN_ROOT="${ODIN_ROOT:-$(pwd)}"
ODIN_N8N_SSH_DEDUP_COOLDOWN_SECONDS="${ODIN_N8N_SSH_DEDUP_COOLDOWN_SECONDS:-300}"
ODIN_N8N_SSH_APPROVAL_ACTOR="${ODIN_N8N_SSH_APPROVAL_ACTOR:-telegram}"
ODIN_N8N_SSH_LOCK_STALE_SECONDS="${ODIN_N8N_SSH_LOCK_STALE_SECONDS:-30}"
ODIN_N8N_SSH_LOCK_WAIT_SECONDS="${ODIN_N8N_SSH_LOCK_WAIT_SECONDS:-5}"

die() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

require_jq() {
  command -v jq >/dev/null 2>&1 || die "jq is required"
}

sanitize_component() {
  printf '%s' "$1" | tr -cs 'A-Za-z0-9._-' '_'
}

dedup_stamp_path() {
  local kind="$1"
  local project="$2"
  local kind_safe project_safe
  kind_safe="$(sanitize_component "${kind}")"
  project_safe="$(sanitize_component "${project}")"
  printf '%s/state/n8n-ssh-router/dedup/%s/%s.stamp\n' "${ODIN_ROOT}" "${kind_safe}" "${project_safe}"
}

dedup_lock_path() {
  printf '%s.lock\n' "$(dedup_stamp_path "$1" "$2")"
}

lock_acquired_at_path() {
  printf '%s/acquired_at\n' "$1"
}

current_epoch() {
  date +%s
}

lock_acquired_epoch() {
  local lock_dir="$1"
  local acquired_at_path acquired_at

  acquired_at_path="$(lock_acquired_at_path "${lock_dir}")"
  if [[ -f "${acquired_at_path}" ]]; then
    acquired_at="$(cat "${acquired_at_path}" 2>/dev/null || true)"
    if [[ "${acquired_at}" =~ ^[0-9]+$ ]]; then
      printf '%s\n' "${acquired_at}"
      return 0
    fi
  fi

  stat -c '%Y' "${lock_dir}" 2>/dev/null || true
}

maybe_reap_stale_lock_dir() {
  local lock_dir="$1"
  local now="$2"
  local acquired_at

  acquired_at="$(lock_acquired_epoch "${lock_dir}")"
  if [[ "${acquired_at}" =~ ^[0-9]+$ ]] && (( now - acquired_at >= ODIN_N8N_SSH_LOCK_STALE_SECONDS )); then
    rm -rf "${lock_dir}"
  fi
}

acquire_lock_dir() {
  local lock_dir="$1"
  local deadline now acquired_at_path

  deadline=$(( $(current_epoch) + ODIN_N8N_SSH_LOCK_WAIT_SECONDS ))
  acquired_at_path="$(lock_acquired_at_path "${lock_dir}")"

  while ! mkdir "${lock_dir}" 2>/dev/null; do
    now="$(current_epoch)"
    maybe_reap_stale_lock_dir "${lock_dir}" "${now}"
    if (( now >= deadline )); then
      die "timed out waiting for dedup lock: ${lock_dir}"
    fi
    sleep 0.05
  done

  printf '%s\n' "$(current_epoch)" >"${acquired_at_path}"
}

release_lock_dir() {
  local lock_dir="$1"
  rm -f "$(lock_acquired_at_path "${lock_dir}")"
  rmdir "${lock_dir}" 2>/dev/null || true
}

strip_wrapped_quotes() {
  local value="$1"
  local first_char last_char last_index
  if [[ ${#value} -ge 2 ]]; then
    first_char="${value:0:1}"
    last_index=$((${#value} - 1))
    last_char="${value:${last_index}:1}"
    if [[ "${first_char}" == '"' && "${last_char}" == '"' ]]; then
      value="${value:1:${#value}-2}"
    elif [[ "${first_char}" == "'" && "${last_char}" == "'" ]]; then
      value="${value:1:${#value}-2}"
    fi
  fi
  printf '%s' "${value}"
}

validate_dedup_component() {
  local value="$1"
  if [[ ! "${value}" =~ ^[A-Za-z0-9._-]+$ ]]; then
    die "dedup-check kind and project must contain only A-Za-z0-9._-"
  fi
}

route_intake() {
  require_jq

  local envelope source intake_type project_key title action_key dedup_key requested_by payload_json
  envelope="$(cat)"
  [[ -n "${envelope}" ]] || die "stdin intake envelope is required"

  source="$(printf '%s' "${envelope}" | jq -er '.source')"
  intake_type="$(printf '%s' "${envelope}" | jq -er '.type')"
  project_key="$(printf '%s' "${envelope}" | jq -er '.project_key')"
  title="$(printf '%s' "${envelope}" | jq -er '.title')"
  action_key="$(printf '%s' "${envelope}" | jq -er '.action_key // ""')"
  dedup_key="$(printf '%s' "${envelope}" | jq -er '.dedup_key // ""')"
  requested_by="$(printf '%s' "${envelope}" | jq -er '.requested_by // .source')"
  payload_json="$(printf '%s' "${envelope}" | jq -ec '.payload')"

  local cmd=(
    "${ODIN_BIN}"
    intake
    enqueue
    --source "${source}"
    --project "${project_key}"
    --title "${title}"
    --type "${intake_type}"
    --requested-by "${requested_by}"
    --payload-file -
    --json
  )
  if [[ -n "${action_key}" ]]; then
    cmd+=(--action-key "${action_key}")
  fi
  if [[ -n "${dedup_key}" ]]; then
    cmd+=(--dedup-key "${dedup_key}")
  fi

  printf '%s\n' "${payload_json}" | "${cmd[@]}"
}

route_dedup_check() {
  if [[ $# -ne 2 ]]; then
    die "usage: dedup-check <kind> <project>"
  fi

  local kind="$1"
  local project="$2"
  local path

  validate_dedup_component "${kind}"
  validate_dedup_component "${project}"
  path="$(dedup_stamp_path "${kind}" "${project}")"
  mkdir -p "$(dirname "${path}")"
  (
    local lock_dir now last_seen expires_at remaining

    lock_dir="$(dedup_lock_path "${kind}" "${project}")"
    acquire_lock_dir "${lock_dir}"
    trap 'release_lock_dir "${lock_dir}"' EXIT

    now="$(date +%s)"
    if [[ -f "${path}" ]]; then
      last_seen="$(cat "${path}")"
      if [[ "${last_seen}" =~ ^[0-9]+$ ]]; then
        expires_at=$((last_seen + ODIN_N8N_SSH_DEDUP_COOLDOWN_SECONDS))
        if (( now < expires_at )); then
          remaining=$((expires_at - now))
          printf 'cooldown:%d\n' "${remaining}"
          exit 0
        fi
      fi
    fi

    printf '%s\n' "${now}" >"${path}"
    printf 'ok\n'
  )
}

route_approval_resolve() {
  if [[ $# -lt 3 ]]; then
    die "usage: approval-resolve <approval_id> <approve|deny> <reason...>"
  fi

  local approval_id="$1"
  local decision="$2"
  shift 2
  local reason="$*"

  case "${decision}" in
    deny)
      decision="reject"
      ;;
    approve|approved|reject|rejected)
      ;;
    *)
      die "approval-resolve decision must be approve or deny"
      ;;
  esac

  exec "${ODIN_BIN}" approvals resolve \
    --id "${approval_id}" \
    --decision "${decision}" \
    --reason "${reason}" \
    --by "${ODIN_N8N_SSH_APPROVAL_ACTOR}" \
    --json
}

main() {
  local original_command="${SSH_ORIGINAL_COMMAND:-}"
  if [[ -z "${original_command}" ]]; then
    route_intake
    return 0
  fi

  case "${original_command}" in
    nonce-update*)
      die "nonce-update is not supported"
      ;;
  esac

  if [[ "${original_command}" =~ ^dedup-check[[:space:]]+([^[:space:]]+)[[:space:]]+([^[:space:]]+)$ ]]; then
    route_dedup_check "${BASH_REMATCH[1]}" "${BASH_REMATCH[2]}"
    return 0
  fi

  if [[ "${original_command}" =~ ^approval-resolve[[:space:]]+([^[:space:]]+)[[:space:]]+([^[:space:]]+)[[:space:]]+(.+)$ ]]; then
    route_approval_resolve \
      "${BASH_REMATCH[1]}" \
      "${BASH_REMATCH[2]}" \
      "$(strip_wrapped_quotes "${BASH_REMATCH[3]}")"
    return 0
  fi

  die "unsupported SSH_ORIGINAL_COMMAND"
}

main "$@"
