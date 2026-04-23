#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
WORKFLOW_DIR="${ROOT_DIR}/ops/n8n/workflows"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "${TMP_DIR}"' EXIT

workflow_selector=""
dispatch_id=""
project_id=""

die() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

usage() {
  cat <<EOF
usage: $(basename "$0") [--workflow file-or-name] [--dispatch-id id] [--project-id id]

Imports canonical Odin OS n8n workflow exports into the live n8n container.

Examples:
  $(basename "$0") --workflow odin-os-dispatch
  $(basename "$0") --workflow odin-os-pbs-ci-alert --dispatch-id 4Q4d3M4dwiZSUFos
  $(basename "$0") --dispatch-id 4Q4d3M4dwiZSUFos

Notes:
  Import the shared dispatch workflow first, note its n8n workflow ID, then import the
  dependent workflows with --dispatch-id so their Execute Workflow nodes point at it.

Environment overrides:
  N8N_CONTAINER_RUNTIME   Container runtime to use (default: docker, fallback: podman)
  N8N_CONTAINER_NAME      Container name (default: n8n)
EOF
}

while (($#)); do
  case "$1" in
    --workflow)
      [[ $# -ge 2 ]] || die "--workflow requires a value"
      workflow_selector="$2"
      shift 2
      ;;
    --dispatch-id)
      [[ $# -ge 2 ]] || die "--dispatch-id requires a value"
      dispatch_id="$2"
      shift 2
      ;;
    --project-id)
      [[ $# -ge 2 ]] || die "--project-id requires a value"
      project_id="$2"
      shift 2
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    *)
      die "unknown argument: $1"
      ;;
  esac
done

container_runtime() {
  if [[ -n "${N8N_CONTAINER_RUNTIME:-}" ]]; then
    printf '%s\n' "${N8N_CONTAINER_RUNTIME}"
    return 0
  fi
  if command -v docker >/dev/null 2>&1; then
    printf '%s\n' docker
    return 0
  fi
  if command -v podman >/dev/null 2>&1; then
    printf '%s\n' podman
    return 0
  fi
  die "no container runtime found"
}

resolve_workflow_path() {
  local selector="$1"
  if [[ -z "${selector}" ]]; then
    return 0
  fi
  if [[ -f "${selector}" ]]; then
    printf '%s\n' "${selector}"
    return 0
  fi

  local candidate="${WORKFLOW_DIR}/${selector}"
  if [[ -f "${candidate}" ]]; then
    printf '%s\n' "${candidate}"
    return 0
  fi

  candidate="${WORKFLOW_DIR}/${selector}.json"
  if [[ -f "${candidate}" ]]; then
    printf '%s\n' "${candidate}"
    return 0
  fi

  die "workflow not found: ${selector}"
}

is_dispatch_workflow() {
  [[ "$(basename "$1")" == "odin-os-dispatch.json" ]]
}

render_workflow() {
  local source_path="$1"
  local rendered_path="$2"

  if is_dispatch_workflow "${source_path}"; then
    cp "${source_path}" "${rendered_path}"
    return 0
  fi

  [[ -n "${dispatch_id}" ]] || die "--dispatch-id is required when importing dependent workflows"
  jq --arg dispatch_id "${dispatch_id}" '
    .nodes |= map(
      if .type == "n8n-nodes-base.executeWorkflow" and .parameters.workflowId == "__ODIN_OS_DISPATCH_WORKFLOW_ID__"
      then .parameters.workflowId = $dispatch_id
      else .
      end
    )
  ' "${source_path}" >"${rendered_path}"
}

import_workflow() {
  local runtime="$1"
  local container="$2"
  local source_path="$3"
  local rendered_path="${TMP_DIR}/$(basename "${source_path}")"
  local container_path="/tmp/$(basename "${source_path}")"
  local import_args=("${runtime}" exec "${container}" n8n import:workflow "--input=${container_path}")

  render_workflow "${source_path}" "${rendered_path}"

  if [[ -n "${project_id}" ]]; then
    import_args+=("--projectId=${project_id}")
  fi

  "${runtime}" exec -i "${container}" sh -lc 'cat > "$1"' sh "${container_path}" <"${rendered_path}"
  "${import_args[@]}"
  "${runtime}" exec "${container}" rm -f "${container_path}" >/dev/null 2>&1 || true
}

runtime="$(container_runtime)"
container="${N8N_CONTAINER_NAME:-n8n}"

declare -a workflow_paths
if [[ -n "${workflow_selector}" ]]; then
  workflow_paths=("$(resolve_workflow_path "${workflow_selector}")")
else
  workflow_paths=(
    "${WORKFLOW_DIR}/odin-os-dispatch.json"
    "${WORKFLOW_DIR}/odin-os-sentry-alert.json"
    "${WORKFLOW_DIR}/odin-os-pbs-ci-alert.json"
    "${WORKFLOW_DIR}/odin-os-pbs-github-alert.json"
    "${WORKFLOW_DIR}/odin-os-telegram-bot.json"
  )
fi

if [[ ${#workflow_paths[@]} -gt 1 && -z "${dispatch_id}" ]]; then
  die "--dispatch-id is required when importing dependent workflows; import odin-os-dispatch first, note its workflow ID, then rerun with --dispatch-id"
fi

for workflow_path in "${workflow_paths[@]}"; do
  [[ -f "${workflow_path}" ]] || die "missing workflow export: ${workflow_path}"
  printf 'importing %s\n' "$(basename "${workflow_path}")"
  import_workflow "${runtime}" "${container}" "${workflow_path}"
done
