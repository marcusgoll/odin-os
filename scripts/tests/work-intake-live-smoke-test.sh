#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
script="${repo_root}/scripts/ops/work-intake-live-smoke.sh"

fail() {
  echo "work-intake-live-smoke-test: $*" >&2
  exit 1
}

[[ -x "${script}" ]] || fail "live smoke script must be executable"

set +e
output="$("${script}" 2>&1)"
status=$?
set -e
[[ "${status}" -eq 2 ]] || fail "ungated script status = ${status}, want 2"
grep -Fq "ODIN_LIVE_WORK_INTAKE_SMOKE=1" <<<"${output}" || fail "ungated script did not explain opt-in env gate"

grep -Fq "work intake --project" "${script}" || fail "script must use the canonical odin work intake command"
grep -Fq -- "--dry-run" "${script}" || fail "script must keep live intake proof in dry-run mode"
grep -Fq "ODIN_PROJECTS_OVERLAY" "${script}" || fail "script must use a project overlay instead of editing config/projects.yaml"
grep -Fq "persisted=0" "${script}" || fail "script must assert no dry-run persistence"
grep -Fq "dispatch=not_started" "${script}" || fail "script must assert no dispatch"
grep -Fq "prs=not_created" "${script}" || fail "script must assert no PR creation"
