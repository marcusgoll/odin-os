#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
WORKFLOW_DIR="${ROOT_DIR}/ops/n8n/workflows"

dispatch_workflow="${WORKFLOW_DIR}/odin-os-dispatch.json"
workflow_files=(
  "${dispatch_workflow}"
  "${WORKFLOW_DIR}/odin-os-sentry-alert.json"
  "${WORKFLOW_DIR}/odin-os-pbs-ci-alert.json"
  "${WORKFLOW_DIR}/odin-os-pbs-github-alert.json"
  "${WORKFLOW_DIR}/odin-os-telegram-bot.json"
)

die() {
  printf 'n8n-workflow-export-lint-test: %s\n' "$*" >&2
  exit 1
}

assert_file_exists() {
  local path="$1"
  [[ -f "${path}" ]] || die "missing workflow export: ${path}"
}

assert_absent() {
  local path="$1"
  local needle="$2"
  if grep -Fq -- "${needle}" "${path}"; then
    die "$(basename "${path}") unexpectedly contains ${needle}"
  fi
}

assert_present() {
  local path="$1"
  local needle="$2"
  if ! grep -Fq -- "${needle}" "${path}"; then
    die "$(basename "${path}") missing ${needle}"
  fi
}

assert_node_type() {
  local path="$1"
  local node_type="$2"
  if ! jq -e --arg node_type "${node_type}" 'any((.nodes // [])[]?; .type == $node_type)' "${path}" >/dev/null; then
    die "$(basename "${path}") missing node type ${node_type}"
  fi
}

assert_inactive_export() {
  local path="$1"
  if ! jq -e '.active == false' "${path}" >/dev/null; then
    die "$(basename "${path}") must set active=false for portable import"
  fi
}

assert_version_id() {
  local path="$1"
  if ! jq -e '(.versionId | type) == "string" and (.versionId | length) > 0' "${path}" >/dev/null; then
    die "$(basename "${path}") must set versionId for portable import"
  fi
}

assert_webhook_path() {
  local path="$1"
  local expected="$2"
  if ! jq -e --arg expected "${expected}" 'any((.nodes // [])[]?; .type == "n8n-nodes-base.webhook" and .parameters.path == $expected)' "${path}" >/dev/null; then
    die "$(basename "${path}") missing webhook path ${expected}"
  fi
}

assert_no_ssh_target() {
  local path="$1"
  assert_absent "${path}" "/home/node/.ssh/odin_ingress"
  assert_absent "${path}" "orchestrator@172.17.0.1"
  assert_absent "${path}" "StrictHostKeyChecking=no"
  assert_absent "${path}" "ConnectTimeout=5 orchestrator@172.17.0.1"
}

assert_normalized_contract() {
  local path="$1"
  assert_present "${path}" "schema_version"
  assert_present "${path}" "source"
  assert_present "${path}" "type"
  assert_present "${path}" "project_key"
  assert_present "${path}" "title"
  assert_present "${path}" "dedup_key"
  assert_present "${path}" "requested_by"
  assert_present "${path}" "payload"
}

for workflow in "${workflow_files[@]}"; do
  assert_file_exists "${workflow}"
  assert_inactive_export "${workflow}"
  assert_version_id "${workflow}"
  assert_absent "${workflow}" "/var/odin"
  assert_absent "${workflow}" "/home/orchestrator/odin-orchestrator/scripts/odin/"
  assert_absent "${workflow}" "nonce-update"
done

assert_node_type "${dispatch_workflow}" "n8n-nodes-base.executeWorkflowTrigger"
assert_node_type "${dispatch_workflow}" "n8n-nodes-base.executeCommand"
assert_present "${dispatch_workflow}" "odin intake enqueue"
assert_present "${dispatch_workflow}" "/home/node/.ssh/odin_os_ingress"
assert_absent "${dispatch_workflow}" "/home/node/.ssh/odin_ingress"
assert_present "${dispatch_workflow}" "orchestrator@172.17.0.1"
assert_normalized_contract "${dispatch_workflow}"

for workflow in "${workflow_files[@]}"; do
  if [[ "${workflow}" == "${dispatch_workflow}" ]]; then
    continue
  fi
  assert_node_type "${workflow}" "n8n-nodes-base.executeWorkflow"
  assert_no_ssh_target "${workflow}"
  assert_normalized_contract "${workflow}"
done

telegram_workflow="${WORKFLOW_DIR}/odin-os-telegram-bot.json"
assert_webhook_path "${WORKFLOW_DIR}/odin-os-pbs-ci-alert.json" "pbs-ci-alert"
assert_webhook_path "${WORKFLOW_DIR}/odin-os-pbs-github-alert.json" "pbs-github-alert"
assert_webhook_path "${telegram_workflow}" "odin-telegram-bot"
assert_present "${telegram_workflow}" "approval-resolve"

printf 'n8n-workflow-export-lint-test: ok\n'
