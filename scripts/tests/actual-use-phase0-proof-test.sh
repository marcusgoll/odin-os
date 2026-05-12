#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
script="${repo_root}/scripts/ops/actual-use-phase0-proof.sh"
doc="${repo_root}/docs/operations/actual-use-phase0-proof.md"

fail() {
  echo "actual-use-phase0-proof-test: $*" >&2
  exit 1
}

[[ -x "${script}" ]] || fail "proof script must be executable"
[[ -f "${doc}" ]] || fail "operations doc must exist"

set +e
output="$("${script}" 2>&1)"
status=$?
set -e
[[ "${status}" -eq 2 ]] || fail "ungated script status = ${status}, want 2"
grep -Fq "ODIN_ACTUAL_USE_PHASE0_PROOF=1" <<<"${output}" || fail "ungated script did not explain opt-in env gate"

grep -Fq "git status --short --branch" "${script}" || fail "script must report checkout state"
grep -Fq "git worktree list" "${script}" || fail "script must report worktree list"
grep -Fq "which odin" "${script}" || fail "script must locate installed odin"
grep -Fq "realpath" "${script}" || fail "script must resolve installed odin path"
grep -Fq "ODIN_EXPECTED_INSTALLED_ODIN_REALPATH" "${script}" || fail "script must enforce intended installed odin path"
grep -Fq "odin help" "${script}" || fail "script must prove installed odin help"
grep -Fq 'ODIN_ROOT="${installed_runtime_root}" odin help' "${script}" || fail "script must isolate installed odin help from live runtime"
grep -Fq "make build" "${script}" || fail "script must build repo-local odin"
grep -Fq "./bin/odin help" "${script}" || fail "script must prove repo-local odin help"
grep -Fq 'ODIN_ROOT="${runtime_root}" ./bin/odin healthcheck' "${script}" || fail "script must use controlled runtime healthcheck"
grep -Fq "ODIN_HTTP_ADDR=127.0.0.1:0 ./bin/odin serve" "${script}" || fail "script must start serve on an ephemeral address"
grep -Fq "./bin/odin doctor --json" "${script}" || fail "script must collect doctor json"
grep -Fq "fresh controlled runtime root was ready before serve" "${script}" || fail "script must fail if fresh root is ready before serve"
grep -Fq "stayed ready after serve stopped" "${script}" || fail "script must fail if readiness remains ready after serve"

grep -Fq "does not mutate the live runtime root" "${doc}" || fail "doc must state live runtime mutation boundary"
grep -Fq "Stop Conditions" "${doc}" || fail "doc must include stop conditions"
grep -Fq "which odin" "${doc}" || fail "doc must include installed binary proof"
grep -Fq "./bin/odin healthcheck" "${doc}" || fail "doc must include repo-local readiness proof"

echo "ok"
