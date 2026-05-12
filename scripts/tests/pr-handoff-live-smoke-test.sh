#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
script="${repo_root}/scripts/ops/pr-handoff-live-smoke.sh"
runbook="${repo_root}/docs/operations/pr-handoff-live-smoke.md"

fail() {
  echo "pr-handoff-live-smoke-test: $*" >&2
  exit 1
}

[[ -x "${script}" ]] || fail "live smoke script must be executable"
[[ -f "${runbook}" ]] || fail "live smoke runbook must exist"

set +e
output="$("${script}" 2>&1)"
status=$?
set -e
[[ "${status}" -eq 2 ]] || fail "ungated script status = ${status}, want 2"
grep -Fq "ODIN_LIVE_PR_HANDOFF_SMOKE=1" <<<"${output}" || fail "ungated script did not explain opt-in env gate"

grep -Fq "work pr prepare" "${script}" || fail "script must use canonical odin work pr prepare"
grep -Fq -- "--live" "${script}" || fail "script must exercise live PR handoff mode"
grep -Fq -- "--approval" "${script}" || fail "script must require an approved Odin approval before live handoff"
grep -Fq "approvals resolve" "${script}" || fail "script must use canonical odin approvals resolve"
grep -Fq "work proof" "${script}" || fail "script must read back proof through odin work proof"
grep -Fq "logs trail" "${script}" || fail "script must read back audit events through odin logs trail"
grep -Fq "ODIN_PROJECTS_OVERLAY" "${script}" || fail "script must use a project overlay instead of editing config/projects.yaml"
grep -Fq "external_mutated=True" "${script}" || fail "script must assert live external mutation evidence"
grep -Fq "pull_request.handoff_prepared=True" "${script}" || fail "script must assert handoff audit evidence"
grep -Fq "prs=not_merged" "${script}" || fail "script must assert no merge/deploy follow-up"

grep -Fq "disposable repository" "${runbook}" || fail "runbook must require a disposable repository"
grep -Fq "Do not run" "${runbook}" || fail "runbook must include stop conditions"
grep -Fq "does not merge" "${runbook}" || fail "runbook must document merge boundary"
