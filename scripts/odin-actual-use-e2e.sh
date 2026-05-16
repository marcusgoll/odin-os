#!/usr/bin/env bash
set -euo pipefail

repo_root="$(git rev-parse --show-toplevel)"
odin_bin="${ODIN_ACTUAL_USE_ODIN:-${repo_root}/bin/odin}"
project_key="actual-use-demo"
now_first="2026-01-01T00:00:01Z"
now_second="2026-01-01T00:00:02Z"

artifact_root="${repo_root}/.odin/actual-use-e2e"
run_id="$(date -u +%Y%m%dT%H%M%SZ)"
run_dir="${artifact_root}/${run_id}"
latest_log="${artifact_root}/latest.log"
latest_json="${artifact_root}/latest.json"
mkdir -p "${run_dir}"
: >"${latest_log}"

tmp_root="$(mktemp -d)"
runtime_root="${tmp_root}/runtime"
home_root="${tmp_root}/home"
project_repo="${tmp_root}/${project_key}"
overlay="${tmp_root}/projects.overlay.yaml"
serve_pid=""
current_driver_response='{"status":"completed","output":"actual-use deterministic executor completed"}'

log() {
  printf '%s\n' "$*" | tee -a "${latest_log}"
}

fail() {
  log "FAIL: $*"
  exit 1
}

cleanup_worktrees() {
  if [[ -d "${project_repo}/.git" ]]; then
    while IFS= read -r path; do
      [[ -n "${path}" ]] || continue
      case "${path}" in
        "${project_repo}") ;;
        "${home_root}"/*)
          git -C "${project_repo}" worktree remove --force "${path}" >/dev/null 2>&1 || true
          ;;
      esac
    done < <(git -C "${project_repo}" worktree list --porcelain 2>/dev/null | sed -n 's/^worktree //p')
  fi
}

stop_serve() {
  if [[ -n "${serve_pid}" ]] && kill -0 "${serve_pid}" >/dev/null 2>&1; then
    kill "${serve_pid}" >/dev/null 2>&1 || true
    wait "${serve_pid}" >/dev/null 2>&1 || true
  fi
  serve_pid=""
}

cleanup() {
  stop_serve
  cleanup_worktrees
  if [[ "${ODIN_ACTUAL_USE_KEEP:-}" != "1" ]]; then
    rm -rf "${tmp_root}"
  else
    log "kept temp root: ${tmp_root}"
  fi
}
trap cleanup EXIT

slugify() {
  printf '%s' "$1" | tr '[:upper:]' '[:lower:]' | sed -E 's/[^a-z0-9]+/-/g; s/^-//; s/-$//'
}

run_cmd() {
  local label="$1"
  shift
  local slug output
  slug="$(slugify "${label}")"
  output="${run_dir}/${slug}.out"
  log ""
  log "## ${label}"
  log "$ $*"
  if "$@" >"${output}" 2>&1; then
    sed 's/^/  /' "${output}" | tee -a "${latest_log}" >/dev/null
  else
    local status=$?
    sed 's/^/  /' "${output}" | tee -a "${latest_log}" >/dev/null
    fail "${label} failed with status ${status}; see ${output}"
  fi
  LAST_OUT="${output}"
}

run_cmd_allow_fail() {
  local label="$1"
  shift
  local slug output
  slug="$(slugify "${label}")"
  output="${run_dir}/${slug}.out"
  log ""
  log "## ${label}"
  log "$ $*"
  set +e
  "$@" >"${output}" 2>&1
  LAST_STATUS=$?
  set -e
  sed 's/^/  /' "${output}" | tee -a "${latest_log}" >/dev/null
  LAST_OUT="${output}"
}

odin() {
  env \
    HOME="${home_root}" \
    ODIN_ROOT="${runtime_root}" \
    ODIN_PROJECTS_OVERLAY="${overlay}" \
    ODIN_CODEX_DRIVER="${repo_root}/scripts/drivers/codex-headless.sh" \
    ODIN_CODEX_DRIVER_HEALTH_RESPONSE='{"status":"healthy","details":"actual-use fixture codex driver healthy"}' \
    ODIN_CODEX_DRIVER_RUN_RESPONSE="${current_driver_response}" \
    "$odin_bin" "$@"
}

run_odin() {
  local label="$1"
  shift
  run_cmd "${label}" env \
    HOME="${home_root}" \
    ODIN_ROOT="${runtime_root}" \
    ODIN_PROJECTS_OVERLAY="${overlay}" \
    ODIN_CODEX_DRIVER="${repo_root}/scripts/drivers/codex-headless.sh" \
    ODIN_CODEX_DRIVER_HEALTH_RESPONSE='{"status":"healthy","details":"actual-use fixture codex driver healthy"}' \
    ODIN_CODEX_DRIVER_RUN_RESPONSE="${current_driver_response}" \
    "$odin_bin" "$@"
}

run_odin_allow_fail() {
  local label="$1"
  shift
  run_cmd_allow_fail "${label}" env \
    HOME="${home_root}" \
    ODIN_ROOT="${runtime_root}" \
    ODIN_PROJECTS_OVERLAY="${overlay}" \
    ODIN_CODEX_DRIVER="${repo_root}/scripts/drivers/codex-headless.sh" \
    ODIN_CODEX_DRIVER_HEALTH_RESPONSE='{"status":"healthy","details":"actual-use fixture codex driver healthy"}' \
    ODIN_CODEX_DRIVER_RUN_RESPONSE="${current_driver_response}" \
    "$odin_bin" "$@"
}

json_assert() {
  local file="$1"
  local expression="$2"
  local message="$3"
  python3 - "$file" "$expression" "$message" <<'PY'
import json
import sys

path, expression, message = sys.argv[1:4]
with open(path, "r", encoding="utf-8") as handle:
    data = json.load(handle)

safe_globals = {
    "__builtins__": {},
    "data": data,
    "any": any,
    "all": all,
    "len": len,
    "str": str,
    "int": int,
    "bool": bool,
}
if not eval(expression, safe_globals, {}):
    raise SystemExit(message)
PY
}

json_value() {
  local file="$1"
  local expression="$2"
  python3 - "$file" "$expression" <<'PY'
import json
import sys

path, expression = sys.argv[1:3]
with open(path, "r", encoding="utf-8") as handle:
    data = json.load(handle)
value = eval(expression, {"__builtins__": {}, "data": data, "len": len, "str": str, "int": int}, {})
if value is None:
    raise SystemExit(1)
print(value)
PY
}

assert_contains() {
  local file="$1"
  local needle="$2"
  grep -Fq "$needle" "$file" || fail "expected ${file} to contain ${needle}"
}

setup_project() {
  [[ -x "${odin_bin}" ]] || fail "odin binary not executable at ${odin_bin}; run make build first"
  mkdir -p "${runtime_root}" "${home_root}" "${project_repo}"
  git -C "${project_repo}" init -b main >/dev/null
  git -C "${project_repo}" -c user.name="Odin Actual Use" -c user.email="odin-actual-use@example.invalid" commit --allow-empty -m "initial actual-use fixture" >/dev/null

  cat >"${overlay}" <<YAML
version: 1
projects:
  - key: ${project_key}
    name: Actual Use Demo
    project_class: local_git_project
    git_root: ${project_repo}
    default_branch: main
    system_project: false
    scheduler:
      max_concurrent_runs: 1
      max_starts_per_cycle: 1
      stalled_run_retry_limit: 2
    policy:
      allowed_commands: [status, test, build]
      branch_rules:
        protected_branches: [main]
        require_worktree: true
        require_task_branch: true
        allow_default_branch_mutation: false
      approval_gates:
        require_for_governance_changes: true
        require_for_destructive_operations: true
        require_for_system_project_changes: false
      merge_policy:
        mode: squash
        allow_direct_to_default_branch: false
      destructive_operations:
        allow_reset: false
        allow_clean: false
        allow_force_push: false
        require_explicit_approval: true
cutover:
  pilot_projects:
    - key: ${project_key}
      runtime_owner: odin_os
      primary_controller: odin_os
      stage: cutover
      comparison_context: none
      legacy_primary_required: false
      shadow_graduation: [actual use e2e shadow complete]
      limited_action_graduation: [actual use e2e limited action complete]
      cutover_graduation: [actual use e2e cutover complete]
      legacy_duties_to_retire_in_order: [none]
YAML
}

start_serve_and_wait() {
  local serve_log="${run_dir}/serve.log"
  log ""
  log "## Readiness smoke: serve"
  log "$ ODIN_ROOT=${runtime_root} ODIN_PROJECTS_OVERLAY=${overlay} ODIN_HTTP_ADDR=127.0.0.1:0 ${odin_bin} serve"
  env \
    HOME="${home_root}" \
    ODIN_ROOT="${runtime_root}" \
    ODIN_PROJECTS_OVERLAY="${overlay}" \
    ODIN_HTTP_ADDR="127.0.0.1:0" \
    ODIN_CODEX_DRIVER="${repo_root}/scripts/drivers/codex-headless.sh" \
    ODIN_CODEX_DRIVER_HEALTH_RESPONSE='{"status":"healthy","details":"actual-use fixture codex driver healthy"}' \
    ODIN_CODEX_DRIVER_RUN_RESPONSE="${current_driver_response}" \
    "${odin_bin}" serve >"${serve_log}" 2>&1 &
  serve_pid=$!
  for _ in $(seq 1 80); do
    if odin healthcheck >"${run_dir}/healthcheck.out" 2>&1; then
      sed 's/^/  /' "${run_dir}/healthcheck.out" | tee -a "${latest_log}" >/dev/null
      return
    fi
    if ! kill -0 "${serve_pid}" >/dev/null 2>&1; then
      sed 's/^/  /' "${serve_log}" | tee -a "${latest_log}" >/dev/null
      fail "serve exited before healthcheck passed"
    fi
    sleep 0.1
  done
  sed 's/^/  /' "${serve_log}" | tee -a "${latest_log}" >/dev/null
  fail "healthcheck did not become ready"
}

setup_project

# Safety boundary: this harness mutates only ODIN_ROOT=${runtime_root}, a temp
# HOME, and a temp local git project. ODIN_DRY_RUN is not set globally because
# scheduler, intake, approval, and work state must be durable in the temp store.
# No live email/calendar/GitHub/production mutation commands are invoked.

log "Odin actual-use E2E"
log "artifacts: ${run_dir}"
log "runtime root: ${runtime_root}"
log "project repo: ${project_repo}"

log ""
log "Scenario: Binary proof"
run_cmd "Binary proof: repo-local help" env HOME="${home_root}" ODIN_ROOT="${runtime_root}" ODIN_PROJECTS_OVERLAY="${overlay}" "${odin_bin}" help
assert_contains "${LAST_OUT}" "Commands:"
installed_reason="temp mode: installed binary may point at live release; repo-local bin/odin is authoritative for this isolated harness"
log "installed_binary=skipped reason=${installed_reason}"

log ""
log "Scenario: Readiness smoke"
run_odin "Readiness smoke: doctor" doctor --json
json_assert "${LAST_OUT}" "data.get('status') in ('healthy', 'degraded')" "doctor status must be healthy or degraded in temp mode"
start_serve_and_wait
stop_serve
run_odin "Readiness smoke: overview" overview --json
json_assert "${LAST_OUT}" "'workspace' in data and 'observability' in data" "overview must include workspace and observability lanes"

log ""
log "Scenario: Raw intake"
payload_one="${tmp_root}/raw-intake-one.json"
payload_two="${tmp_root}/raw-intake-two.json"
cat >"${payload_one}" <<'JSON'
{"body":"Prepare a governed intake process review with source evidence.","source_url":"fixture://actual-use/raw-1"}
JSON
cat >"${payload_two}" <<'JSON'
{"body":"Duplicate request retaining raw evidence.","source_url":"fixture://actual-use/raw-2"}
JSON
run_odin "Raw intake: create" intake raw create --source operator --project "${project_key}" --title "Prepare governed intake process review" --type request --dedup-key actual-use-raw-1 --requested-by codex --payload-file "${payload_one}" --json
raw_key="$(json_value "${LAST_OUT}" "data['intake_item']['key']")"
run_odin "Raw intake: list" intake raw list --project "${project_key}" --json
json_assert "${LAST_OUT}" "any(item.get('key') == '${raw_key}' for item in data.get('intake_items', []))" "raw intake list must include created item"
run_odin "Raw intake: show" intake raw show "${raw_key}" --json
json_assert "${LAST_OUT}" "data['intake_item']['evidence']['payload_included'] is True" "raw show must retain payload evidence"
run_odin "Raw intake: process" intake process --id "${raw_key}" --json
json_assert "${LAST_OUT}" "data.get('outcome') == 'review_required' and data.get('routed_outcome') == 'draft_task'" "raw process must create a reviewable draft task item"
run_odin "Raw intake: review list" intake review list --json
json_assert "${LAST_OUT}" "any(item.get('key') == '${raw_key}' for item in data.get('intake_items', []))" "intake review queue must include processed raw item"
run_odin "Raw intake: review show" intake review show "${raw_key}" --json
json_assert "${LAST_OUT}" "data['intake_item']['status'] == 'review_required'" "intake review show must expose review_required state"

log ""
log "Scenario: Dedupe"
run_odin "Dedupe: duplicate create" intake raw create --source operator --project "${project_key}" --title "Prepare governed intake process review" --type request --dedup-key actual-use-raw-1 --requested-by codex --payload-file "${payload_two}" --json
dupe_key="$(json_value "${LAST_OUT}" "data['intake_item']['key']")"
run_odin "Dedupe: duplicate process" intake process --id "${dupe_key}" --json
json_assert "${LAST_OUT}" "data.get('outcome') == 'duplicate_linked_or_suppressed' and data['intake_item'].get('canonical_intake_key') == '${raw_key}'" "duplicate intake must link to canonical raw item"
run_odin "Dedupe: duplicate show" intake raw show "${dupe_key}" --json
json_assert "${LAST_OUT}" "data['intake_item']['canonical_intake_key'] == '${raw_key}' and data['intake_item']['evidence']['payload_included'] is True" "duplicate raw evidence must remain available"

log ""
log "Scenario: Approval gate"
run_odin "Approval gate: work start" work start --project odin-core --title "Govern system policy for actual use e2e" --intent governance
approval_task_key="$(sed -n 's/.*key=\([^ ]*\).*/\1/p' "${LAST_OUT}" | head -n 1)"
[[ -n "${approval_task_key}" ]] || fail "could not parse approval task key"
run_odin "Approval gate: dispatch" work dispatch --task "${approval_task_key}" --json
json_assert "${LAST_OUT}" "data.get('dispatched') is False and data.get('reason') == 'approval_required' and data['task']['status'] == 'blocked'" "governance task must be blocked by approval"
run_odin "Approval gate: approvals all" approvals all --json
json_assert "${LAST_OUT}" "any(item.get('task_key') == '${approval_task_key}' and item.get('status') == 'pending' for item in data.get('approvals', []))" "pending approval must be visible"

log ""
log "Scenario: Work dispatch"
run_odin "Work dispatch: project select" project select "${project_key}"
run_odin "Work dispatch: transition cutover" transition set cutover confirm because actual use e2e temp project
run_odin "Work dispatch: task run" task run --project "${project_key}" --title "Inspect actual use fixture" --acceptance "fixture driver completes the run" --acceptance "completed run is visible in runs list" --json
safe_task_key="$(json_value "${LAST_OUT}" "data['task']['key']")"
safe_run_id="$(json_value "${LAST_OUT}" "data['run']['id']")"
json_assert "${LAST_OUT}" "data['task']['status'] == 'completed' and data['run']['status'] == 'completed' and data['run']['executor'] == 'codex_headless'" "safe internal work must complete through codex_headless fixture driver"
run_odin "Work dispatch: runs list" runs --json
json_assert "${LAST_OUT}" "any(run.get('run_id') == ${safe_run_id} and run.get('status') == 'completed' for run in data.get('runs', []))" "completed safe run must be in canonical runs list"

log ""
log "Scenario: Delivery loop"
run_cmd "Delivery loop: raw intake to PR dry-run" env \
  HOME="${home_root}" \
  ODIN_ROOT="${runtime_root}" \
  ODIN_PROJECTS_OVERLAY="${overlay}" \
  "${odin_bin}" e2e --scenario "${repo_root}/fixtures/e2e/raw-intake-delivery-dry-run.yaml" --json
json_assert "${LAST_OUT}" "data.get('status') == 'passed' and data.get('scenario') == 'raw-intake-delivery-dry-run'" "delivery loop fixture must pass"
json_assert "${LAST_OUT}" "data.get('github', {}).get('mode') == 'fixture' and data.get('github', {}).get('mutated') is False" "delivery loop must not make live GitHub mutations"
json_assert "${LAST_OUT}" "data.get('codex', {}).get('mode') == 'disabled' and data.get('codex', {}).get('invoked') is False" "delivery loop must keep live Codex disabled"
json_assert "${LAST_OUT}" "data.get('intake', {}).get('raw_status') == 'routed' and data.get('intake', {}).get('routed_work_item_key') == 'raw-intake-1'" "delivery loop must route raw intake to a work item"
json_assert "${LAST_OUT}" "data.get('workspace', {}).get('workspace_state') == 'stopped' and data.get('workspace', {}).get('workspace_attached', 0) == 0" "delivery loop must prove tmux/session orchestration is stopped cleanly"
json_assert "${LAST_OUT}" "data.get('delivery', {}).get('handoff_review_state') == 'review_selected' and all(role in data.get('delivery', {}).get('handoff_review_roles', []) for role in ['reviewer', 'qa', 'security'])" "delivery loop must select worker/reviewer/security review handoff roles"
json_assert "${LAST_OUT}" "data.get('delivery', {}).get('approval_status') == 'approved' and data.get('delivery', {}).get('approval_resolver_support') == 'supported'" "delivery loop must pass through supported approval resolution"
json_assert "${LAST_OUT}" "data.get('delivery', {}).get('merge_verified') is True and data.get('delivery', {}).get('merge_task_status') == 'completed'" "delivery loop must verify the dry-run PR merge gate"

log ""
log "Scenario: Scheduler"
run_odin "Scheduler: trigger upsert" trigger upsert actual-use-daily initiative="${project_key}" kind=schedule status=enabled next=2026-01-01T00:00:00Z cadence=24h title=Review_actual_use_daily intent=read_only --json
run_odin "Scheduler: first tick" scheduler tick now="${now_first}" --json
json_assert "${LAST_OUT}" "data['trigger_evaluation']['materialized'] == 1 and data['mutates'] is True" "first scheduler tick must materialize one work item"
run_odin "Scheduler: second tick dedupe" scheduler tick now="${now_first}" --json
json_assert "${LAST_OUT}" "data['trigger_evaluation']['materialized'] == 0" "second scheduler tick at same time must not rematerialize"
run_odin "Scheduler: trigger audit" trigger audit actual-use-daily --json
json_assert "${LAST_OUT}" "any(event.get('event_type') == 'automation_trigger.materialized' for event in data.get('audit_events', []))" "trigger audit must include materialization event"

log ""
log "Scenario: Review queue"
current_driver_response='{"status":"failed","output":"actual-use induced deterministic failure"}'
run_odin "Review queue: failed work start" work start --project "${project_key}" --title "Fail deterministic recovery fixture" --intent read_only --acceptance "fixture driver failure is recorded"
failed_task_key="$(sed -n 's/.*key=\([^ ]*\).*/\1/p' "${LAST_OUT}" | head -n 1)"
[[ -n "${failed_task_key}" ]] || fail "could not parse failed task key"
run_odin "Review queue: failed dispatch one" work dispatch --task "${failed_task_key}" --json
run_odin_allow_fail "Review queue: failed execute one" work execute --task "${failed_task_key}" --json
json_assert "${LAST_OUT}" "data['task']['status'] == 'failed' and data['run']['status'] == 'failed'" "first failed execute must record failed task and run"
run_odin "Review queue: failed retry" work retry --task "${failed_task_key}" --json
run_odin "Review queue: failed dispatch two" work dispatch --task "${failed_task_key}" --json
run_odin_allow_fail "Review queue: failed execute two" work execute --task "${failed_task_key}" --json
current_driver_response='{"status":"completed","output":"actual-use deterministic executor completed"}'
run_odin "Review queue: recovery tick" scheduler tick now="${now_second}" recovery=true --json
json_assert "${LAST_OUT}" "data.get('recovery', {}).get('outcomes', 0) >= 1" "recovery tick must emit at least one outcome"
run_odin "Review queue: context pack proposal" knowledge context-pack task="${safe_task_key}" project="${project_key}" --propose --json
json_assert "${LAST_OUT}" "data.get('proposal', {}).get('id', 0) > 0 and data.get('review', {}).get('status') == 'review_required'" "context pack must create a reviewable artifact"
run_odin "Review queue: list" review list --json
json_assert "${LAST_OUT}" "len(data.get('items', [])) >= 4" "review queue must aggregate multiple reviewable sources"
json_assert "${LAST_OUT}" "all(kind in [item.get('source_type') for item in data.get('items', [])] for kind in ['task_approval', 'failed_work', 'context_pack', 'intake_review', 'recovery'])" "review queue must include approvals, failed work, knowledge artifacts, intake, and recovery"

log ""
log "Scenario: Observability"
run_odin "Observability: overview" overview --json
json_assert "${LAST_OUT}" "data['intake_inbox']['raw_item_count'] >= 2" "overview must show raw intake"
json_assert "${LAST_OUT}" "data['automation_triggers']['materialized_count'] >= 1" "overview must show materialized triggers"
json_assert "${LAST_OUT}" "data['workspace']['pending_approval_count'] >= 1" "overview must show approvals"
json_assert "${LAST_OUT}" "len(data.get('work_items') or []) >= 1 and data['workspace']['open_work_item_count'] >= 1" "overview must show work"
json_assert "${LAST_OUT}" "len(data['observability'].get('recoveries') or []) >= 1 or len(data['observability'].get('incidents') or []) >= 1 or len(data['observability'].get('recovery_guidance') or []) >= 1" "overview must show recovery or incident evidence"
json_assert "${LAST_OUT}" "'freshness' in data['observability'] and 'activity_log' in data['observability']" "overview must expose readiness/freshness and activity from the same store"

python3 - "${latest_json}" "${run_dir}" "${runtime_root}" "${project_key}" "${installed_reason}" <<'PY'
import json
import sys

path, run_dir, runtime_root, project_key, installed_reason = sys.argv[1:6]
payload = {
    "status": "passed",
    "run_dir": run_dir,
    "runtime_root": runtime_root,
    "project_key": project_key,
    "external_mutation": "none",
    "installed_binary": {
        "status": "skipped",
        "reason": installed_reason,
    },
    "scenarios": [
        "Binary proof",
        "Readiness smoke",
        "Raw intake",
        "Dedupe",
        "Approval gate",
        "Work dispatch",
        "Delivery loop",
        "Scheduler",
        "Review queue",
        "Observability",
    ],
}
with open(path, "w", encoding="utf-8") as handle:
    json.dump(payload, handle, indent=2, sort_keys=True)
    handle.write("\n")
PY

log ""
log "PASS: Odin actual-use E2E"
log "latest log: ${latest_log}"
log "latest summary: ${latest_json}"
