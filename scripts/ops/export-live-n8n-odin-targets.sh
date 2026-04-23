#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
DEFAULT_DOC_PATH="${ROOT_DIR}/docs/operations/n8n-cutover-inventory.md"
FORMAT="md"
WRITE_DOC_PATH=""
INPUT_FILE=""

die() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

usage() {
  cat <<EOF
usage: $(basename "$0") [--format tsv|md] [--write-doc [path]] [--input file]

Reads a live n8n workflow export, filters active workflows that target legacy Odin,
and emits a stable TSV or Markdown inventory. With --write-doc, it writes a Markdown
inventory document to the provided path or to ${DEFAULT_DOC_PATH}.

Environment overrides:
  N8N_WORKFLOWS_EXPORT_FILE  Read JSON from a local file instead of the live container.
  N8N_CONTAINER_RUNTIME      Container runtime to use (default: docker, fallback: podman).
  N8N_CONTAINER_NAME         Container name to export from (default: n8n).
  N8N_EXPORT_COMMAND         Command run inside the container to print the export JSON.
EOF
}

while (($#)); do
  case "$1" in
    --format)
      [[ $# -ge 2 ]] || die "--format requires a value"
      FORMAT="$2"
      shift 2
      ;;
    --write-doc)
      if [[ $# -ge 2 && "${2}" != --* ]]; then
        WRITE_DOC_PATH="$2"
        shift 2
      else
        WRITE_DOC_PATH="${DEFAULT_DOC_PATH}"
        shift
      fi
      ;;
    --input)
      [[ $# -ge 2 ]] || die "--input requires a path"
      INPUT_FILE="$2"
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

[[ "${FORMAT}" == "tsv" || "${FORMAT}" == "md" ]] || die "--format must be tsv or md"

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

read_workflow_export() {
  if [[ -n "${INPUT_FILE}" ]]; then
    cat "${INPUT_FILE}"
    return 0
  fi
  if [[ -n "${N8N_WORKFLOWS_EXPORT_FILE:-}" ]]; then
    cat "${N8N_WORKFLOWS_EXPORT_FILE}"
    return 0
  fi
  if [[ -p /dev/stdin ]]; then
    cat
    return 0
  fi

  local runtime container command
  runtime="$(container_runtime)"
  container="${N8N_CONTAINER_NAME:-n8n}"
  command="${N8N_EXPORT_COMMAND:-n8n export:workflow --all --output /tmp/workflows-export.json >/dev/null 2>&1 && cat /tmp/workflows-export.json}"
  "${runtime}" exec "${container}" sh -lc "${command}"
}

generate_tsv() {
  jq -r '
    def workflow_array:
      if type == "array" then .
      elif type == "object" and has("workflows") then .workflows
      elif type == "object" and has("data") then .data
      elif type == "object" and has("items") then .items
      else []
      end;

    def node_strings:
      [(.nodes // [])[]? | .. | strings];

    def has_node_pattern($re):
      any(node_strings[]?; test($re; "i"));

    def classify:
      if has_node_pattern("/var/odin") then "legacy_script"
      elif has_node_pattern("dedup-check|nonce-update") then "legacy_helper"
      elif has_node_pattern("orchestrator@172\\.17\\.0\\.1|odin_ingress") then "dispatch_envelope"
      else empty
      end;

    workflow_array
    | map(select(.active == true) | . + {class: classify})
    | map(select(.class != null and .class != ""))
    | sort_by(.name | ascii_downcase)
    | .[]
    | [(if has("id") and .id != null then (.id | tostring) else (.name // "-") end), (.name // "-"), (.class // "-")]
    | @tsv
  '
}

escape_md() {
  printf '%s' "$1" | sed 's/\\/\\\\/g; s/|/\\|/g'
}

render_markdown() {
  local rows="$1"
  {
    printf '# Live n8n Odin Cutover Inventory\n\n'
    printf 'This inventory captures the active legacy Odin-facing workflows currently visible in the live n8n export.\n\n'
    printf 'Observed transport families:\n\n'
    printf '%s\n' '- `dispatch_envelope`: workflows that push base64-decoded task envelopes through SSH ingress to `orchestrator@172.17.0.1`'
    printf '%s\n' '- `legacy_helper`: workflows that invoke helper verbs such as `dedup-check` or `nonce-update`'
    printf '%s\n' '- `legacy_script`: workflows that directly target legacy shell scripts under `/var/odin` if present in the export'
    printf '\n'
    printf '| ID | Workflow | Class |\n'
    printf '| --- | --- | --- |\n'
    while IFS=$'\t' read -r id name class; do
      [[ -n "${id}${name}${class}" ]] || continue
      printf '| %s | %s | %s |\n' "$(escape_md "${id}")" "$(escape_md "${name}")" "$(escape_md "${class}")"
    done <<<"${rows}"
  }
}

json="$(read_workflow_export)"
rows="$(printf '%s\n' "${json}" | generate_tsv)"

if [[ -n "${WRITE_DOC_PATH}" ]]; then
  doc_dir="$(dirname "${WRITE_DOC_PATH}")"
  mkdir -p "${doc_dir}"
  tmp_doc="$(mktemp)"
  trap 'rm -f "${tmp_doc}"' EXIT
  render_markdown "${rows}" >"${tmp_doc}"
  mv "${tmp_doc}" "${WRITE_DOC_PATH}"
  trap - EXIT
fi

if [[ "${FORMAT}" == "tsv" ]]; then
  printf '%s\n' "${rows}"
else
  render_markdown "${rows}"
fi
