#!/usr/bin/env bash
set -euo pipefail

if [[ "${ODIN_LIVE_WORK_INTAKE_SMOKE:-}" != "1" ]]; then
  echo "set ODIN_LIVE_WORK_INTAKE_SMOKE=1 to run the live work-intake smoke proof" >&2
  exit 2
fi

repo="${ODIN_LIVE_WORK_INTAKE_REPO:-}"
if [[ ! "${repo}" =~ ^[^/]+/[^/]+$ ]]; then
  echo "ODIN_LIVE_WORK_INTAKE_REPO must be owner/repo for a disposable repository" >&2
  exit 2
fi

if [[ -z "${GITHUB_TOKEN:-}" ]]; then
  echo "GITHUB_TOKEN must be set with issue read scope for the disposable repository" >&2
  exit 2
fi

repo_root="$(git rev-parse --show-toplevel)"
odin_bin="${ODIN_LIVE_WORK_INTAKE_ODIN:-${repo_root}/bin/odin}"
if [[ ! -x "${odin_bin}" ]]; then
  echo "odin binary not executable at ${odin_bin}; run make build first or set ODIN_LIVE_WORK_INTAKE_ODIN" >&2
  exit 2
fi

project_key="${ODIN_LIVE_WORK_INTAKE_PROJECT:-live-intake-smoke}"
runtime_root="${ODIN_LIVE_WORK_INTAKE_ROOT:-$(mktemp -d)}"
overlay="$(mktemp)"
cleanup() {
  rm -f "${overlay}"
}
trap cleanup EXIT

cat >"${overlay}" <<YAML
version: 1
projects:
  - key: ${project_key}
    name: Live Intake Smoke
    project_class: github_backed_project
    git_root: ${repo_root}
    default_branch: main
    github:
      repo: ${repo}
    policy:
      allowed_commands: [status]
      branch_rules:
        protected_branches: [main]
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

output="$(
  ODIN_ROOT="${runtime_root}" \
    ODIN_PROJECTS_OVERLAY="${overlay}" \
    "${odin_bin}" work intake --project "${project_key}" --dry-run
)"

printf '%s\n' "${output}"

case "${output}" in
  *"project=${project_key}"*"repo=${repo}"*"dry_run=true"*"persisted=0"*"dispatch=not_started"*"prs=not_created"*) ;;
  *)
    echo "unexpected work intake smoke output" >&2
    exit 1
    ;;
esac

fetched="$(sed -n 's/.*fetched=\([0-9][0-9]*\).*/\1/p' <<<"${output}" | head -n 1)"
if [[ -z "${fetched}" || "${fetched}" -lt 1 ]]; then
  echo "live intake smoke fetched=${fetched:-missing}; create a disposable open issue labeled odin:ready" >&2
  exit 1
fi
