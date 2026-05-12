#!/usr/bin/env bash
set -euo pipefail

if [[ "${ODIN_LIVE_PR_HANDOFF_SMOKE:-}" != "1" ]]; then
  echo "set ODIN_LIVE_PR_HANDOFF_SMOKE=1 to run the live PR handoff smoke proof" >&2
  exit 2
fi

repo="${ODIN_LIVE_PR_HANDOFF_REPO:-}"
if [[ ! "${repo}" =~ ^[^/]+/[^/]+$ ]]; then
  echo "ODIN_LIVE_PR_HANDOFF_REPO must be owner/repo for a disposable repository" >&2
  exit 2
fi

head_branch="${ODIN_LIVE_PR_HANDOFF_HEAD_BRANCH:-}"
if [[ -z "${head_branch}" ]]; then
  echo "ODIN_LIVE_PR_HANDOFF_HEAD_BRANCH must name an existing disposable branch in the target repository" >&2
  exit 2
fi

if [[ -z "${GITHUB_TOKEN:-}" ]]; then
  echo "GITHUB_TOKEN must be set with pull request write scope for the disposable repository" >&2
  exit 2
fi

repo_root="$(git rev-parse --show-toplevel)"
odin_bin="${ODIN_LIVE_PR_HANDOFF_ODIN:-${repo_root}/bin/odin}"
if [[ ! -x "${odin_bin}" ]]; then
  echo "odin binary not executable at ${odin_bin}; run make build first or set ODIN_LIVE_PR_HANDOFF_ODIN" >&2
  exit 2
fi

project_key="${ODIN_LIVE_PR_HANDOFF_PROJECT:-live-pr-handoff-smoke}"
runtime_root="${ODIN_LIVE_PR_HANDOFF_ROOT:-$(mktemp -d)}"
base_branch="${ODIN_LIVE_PR_HANDOFF_BASE_BRANCH:-main}"
title="${ODIN_LIVE_PR_HANDOFF_TITLE:-Odin live PR handoff smoke}"
overlay="$(mktemp)"
work_start_output="${runtime_root}/work-start.txt"
approval_output="${runtime_root}/approval-request.json"
resolve_output="${runtime_root}/approval-resolve.json"
prepare_output="${runtime_root}/pr-prepare-live.json"
proof_output="${runtime_root}/work-proof.json"
logs_output="${runtime_root}/logs-trail.json"
cleanup() {
  rm -f "${overlay}"
}
trap cleanup EXIT

mkdir -p "${runtime_root}"

cat >"${overlay}" <<YAML
version: 1
projects:
  - key: ${project_key}
    name: Live PR Handoff Smoke
    project_class: github_backed_project
    git_root: ${repo_root}
    default_branch: ${base_branch}
    github:
      repo: ${repo}
    policy:
      allowed_commands: [status]
      branch_rules:
        protected_branches: [${base_branch}]
        require_worktree: true
        require_task_branch: true
        allow_default_branch_mutation: false
      approval_gates:
        require_for_governance_changes: true
        require_for_destructive_operations: true
        require_for_system_project_changes: true
      merge_policy:
        mode: squash
        allow_direct_to_default_branch: false
      destructive_operations:
        allow_reset: false
        allow_clean: false
        allow_force_push: false
        require_explicit_approval: true
YAML

run_odin() {
  ODIN_ROOT="${runtime_root}" \
    ODIN_PROJECTS_OVERLAY="${overlay}" \
    "${odin_bin}" "$@"
}

run_odin work start \
  --project "${project_key}" \
  --title "${title}" \
  --intent governance >"${work_start_output}"

task_key="$(sed -n 's/.*key=\([^ ]*\).*/\1/p' "${work_start_output}" | head -n 1)"
if [[ -z "${task_key}" ]]; then
  echo "could not parse work item key from work start output" >&2
  cat "${work_start_output}" >&2
  exit 1
fi

run_odin work pr prepare \
  --task "${task_key}" \
  --summary "Live PR handoff smoke for ${repo}." \
  --test "operator verified disposable live PR handoff smoke" \
  --risk "live proof may create or update one disposable pull request; no merge or deploy is authorized" \
  --command "scripts/ops/pr-handoff-live-smoke.sh" \
  --branch "${head_branch}" \
  --title "${title}" \
  --live \
  --json >"${approval_output}"

approval_id="$(
  python3 - <<'PY' "${approval_output}"
import json, sys
with open(sys.argv[1]) as f:
    data = json.load(f)
if data.get("prepared") or not data.get("approval_required") or data.get("external_mutated"):
    raise SystemExit("live prepare must request approval without external mutation")
approval_id = data.get("approval_id")
if not approval_id:
    raise SystemExit("approval_id missing")
print(approval_id)
PY
)"

run_odin approvals resolve "${approval_id}" approve operator approved live PR handoff smoke --json >"${resolve_output}"

run_odin work pr prepare \
  --task "${task_key}" \
  --summary "Live PR handoff smoke for ${repo}." \
  --test "operator verified disposable live PR handoff smoke" \
  --risk "live proof may create or update one disposable pull request; no merge or deploy is authorized" \
  --command "scripts/ops/pr-handoff-live-smoke.sh" \
  --branch "${head_branch}" \
  --title "${title}" \
  --live \
  --approval "${approval_id}" \
  --json >"${prepare_output}"

run_odin work proof --task "${task_key}" --json >"${proof_output}"
run_odin logs trail --task "${task_key}" --json >"${logs_output}"

python3 - <<'PY' "${prepare_output}" "${proof_output}" "${logs_output}" "${repo}" "${task_key}" "${approval_id}"
import json, sys

prepare_path, proof_path, logs_path, repo, task_key, approval_id = sys.argv[1:]
with open(prepare_path) as f:
    prepared = json.load(f)
with open(proof_path) as f:
    proof = json.load(f)
with open(logs_path) as f:
    logs = json.load(f)

pr = prepared.get("pull_request", {})
if not prepared.get("prepared") or prepared.get("approval_required") or not prepared.get("external_mutated"):
    raise SystemExit("live PR handoff did not report prepared external mutation")
if pr.get("repo") != repo or not pr.get("number") or not pr.get("url"):
    raise SystemExit("live PR handoff did not return repo, number, and URL")
if proof.get("mutated") is not True:
    raise SystemExit("work proof did not report mutation evidence")
if proof.get("pull_request", {}).get("number") != pr.get("number"):
    raise SystemExit("work proof did not preserve PR number")

event_types = [item.get("event_type") for item in logs.get("items", [])]
has_handoff = "pull_request.handoff_prepared" in event_types
has_approval = "approval.resolved" in event_types
if not has_handoff or not has_approval:
    raise SystemExit("logs trail missing handoff or approval events")

print(
    "project=live-pr-handoff-smoke "
    f"repo={repo} task={task_key} approval={approval_id} "
    f"pull_request={pr.get('number')} url={pr.get('url')} "
    "external_mutated=True "
    "pull_request.handoff_prepared=True "
    "approval.resolved=True "
    "prs=not_merged deploy=not_started"
)
PY
