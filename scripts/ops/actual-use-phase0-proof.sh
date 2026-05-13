#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
git_common_dir="$(git -C "${repo_root}" rev-parse --git-common-dir)"
case "${git_common_dir}" in
  /*) ;;
  *) git_common_dir="${repo_root}/${git_common_dir}" ;;
esac
primary_root="$(cd "$(dirname "${git_common_dir}")" && pwd)"

fail() {
  echo "actual-use-phase0-proof: $*" >&2
  exit 1
}

if [[ "${ODIN_ACTUAL_USE_PHASE0_PROOF:-}" != "1" ]]; then
  cat >&2 <<'MSG'
actual-use-phase0-proof is opt-in because it builds repo-local binaries and
starts a short-lived ./bin/odin serve process against a temporary ODIN_ROOT.

Run with:
  ODIN_ACTUAL_USE_PHASE0_PROOF=1 ./scripts/ops/actual-use-phase0-proof.sh
MSG
  exit 2
fi

cd "${repo_root}"

echo "== checkout =="
git status --short --branch
git worktree list
if [[ "${ODIN_PHASE0_REQUIRE_CLEAN:-}" == "1" ]]; then
  [[ -z "$(git status --short)" ]] || fail "worktree is dirty"
fi

proof_tmp="$(mktemp -d)"
runtime_root="${proof_tmp}/runtime"
installed_runtime_root="${proof_tmp}/installed-runtime"
mkdir -p "${runtime_root}"
mkdir -p "${installed_runtime_root}"
serve_pid=""
cleanup() {
  if [[ -n "${serve_pid}" ]] && kill -0 "${serve_pid}" 2>/dev/null; then
    kill "${serve_pid}" 2>/dev/null || true
    wait "${serve_pid}" 2>/dev/null || true
  fi
  rm -rf "${proof_tmp}"
}
trap cleanup EXIT

echo "== installed odin =="
installed_odin="$(which odin)" || fail "odin is not on PATH"
printf '%s\n' "${installed_odin}"
installed_realpath="$(realpath "${installed_odin}")"
printf '%s\n' "${installed_realpath}"
expected_installed="${ODIN_EXPECTED_INSTALLED_ODIN_REALPATH:-${primary_root}/releases/current/bin/odin}"
[[ -e "${expected_installed}" ]] || fail "expected installed odin is missing: ${expected_installed}"
expected_installed_realpath="$(realpath "${expected_installed}")"
[[ "${installed_realpath}" == "${expected_installed_realpath}" ]] || fail "installed odin resolves to ${installed_realpath}, want ${expected_installed_realpath}"
ODIN_ROOT="${installed_runtime_root}" odin help >"${proof_tmp}/installed-help.txt"
grep -Fq "Commands:" "${proof_tmp}/installed-help.txt" || fail "installed odin help did not print command list"

echo "== repo-local odin =="
make build
[[ -x ./bin/odin ]] || fail "./bin/odin was not built"
./bin/odin help >"${proof_tmp}/local-help.txt"
grep -Fq "Commands:" "${proof_tmp}/local-help.txt" || fail "repo-local odin help did not print command list"

echo "== readiness before serve =="
set +e
before_output="$(ODIN_ROOT="${runtime_root}" ./bin/odin healthcheck 2>&1)"
before_status=$?
set -e
[[ "${before_status}" -ne 0 ]] || fail "fresh controlled runtime root was ready before serve"
grep -Eq "not ready|runtime not ready|no live odin serve process" <<<"${before_output}" || fail "pre-serve healthcheck did not explain not-ready state"
printf '%s\n' "${before_output}"

echo "== start serve =="
ODIN_ROOT="${runtime_root}" ODIN_HTTP_ADDR=127.0.0.1:0 ./bin/odin serve >"${proof_tmp}/serve.log" 2>&1 &
serve_pid=$!

ready=false
for _ in $(seq 1 60); do
  if ODIN_ROOT="${runtime_root}" ./bin/odin healthcheck >"${proof_tmp}/ready.txt" 2>&1; then
    ready=true
    break
  fi
  if ! kill -0 "${serve_pid}" 2>/dev/null; then
    cat "${proof_tmp}/serve.log" >&2 || true
    fail "serve exited before readiness"
  fi
  sleep 0.25
done
[[ "${ready}" == "true" ]] || {
  cat "${proof_tmp}/serve.log" >&2 || true
  fail "controlled runtime root did not become ready while serve was running"
}
cat "${proof_tmp}/ready.txt"

ODIN_ROOT="${runtime_root}" ./bin/odin doctor --json >"${proof_tmp}/doctor.json"
grep -Fq '"status"' "${proof_tmp}/doctor.json" || fail "doctor json did not include status"

echo "== stop serve =="
kill "${serve_pid}" 2>/dev/null || true
wait "${serve_pid}" 2>/dev/null || true
serve_pid=""

set +e
after_output="$(ODIN_ROOT="${runtime_root}" ./bin/odin healthcheck 2>&1)"
after_status=$?
set -e
[[ "${after_status}" -ne 0 ]] || fail "controlled runtime root stayed ready after serve stopped"
grep -Eq "not ready|runtime not ready|no live odin serve process|draining|stopped" <<<"${after_output}" || fail "post-serve healthcheck did not explain not-ready state"
printf '%s\n' "${after_output}"

echo "actual-use-phase0-proof ok"
