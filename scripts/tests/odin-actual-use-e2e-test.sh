#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
script="${repo_root}/scripts/odin-actual-use-e2e.sh"
doc="${repo_root}/docs/operations/actual-use-e2e.md"
makefile="${repo_root}/Makefile"

fail() {
  echo "odin-actual-use-e2e-test: $*" >&2
  exit 1
}

[[ -x "${script}" ]] || fail "actual-use harness script must be executable"
[[ -f "${doc}" ]] || fail "actual-use operations doc must exist"

grep -Fq "odin-actual-use-e2e:" "${makefile}" || fail "Makefile must expose odin-actual-use-e2e"
grep -Fq "./scripts/odin-actual-use-e2e.sh" "${makefile}" || fail "Make target must call the harness script"

for required in \
  "Readiness smoke" \
  "Raw intake" \
  "Dedupe" \
  "Approval gate" \
  "Work dispatch" \
  "Delivery loop" \
  "Scheduler" \
  "Review queue" \
  "Observability" \
  "Binary proof"; do
  grep -Fq "${required}" "${script}" || fail "script missing scenario label: ${required}"
  grep -Fq "${required}" "${doc}" || fail "doc missing scenario label: ${required}"
done

grep -Fq "ODIN_ROOT=" "${script}" || fail "script must run against a temp ODIN_ROOT"
grep -Fq "ODIN_PROJECTS_OVERLAY=" "${script}" || fail "script must use a temp project overlay"
grep -Fq "ODIN_CODEX_DRIVER=" "${script}" || fail "script must force the fixture Codex driver"
grep -Fq "fixtures/e2e/raw-intake-delivery-dry-run.yaml" "${script}" || fail "script must run the raw-intake delivery fixture"
grep -Fq "merge_verified" "${script}" || fail "script must assert verified dry-run PR merge evidence"
grep -Fq "handoff_review_roles" "${script}" || fail "script must assert specialist review handoff evidence"
grep -Fq "ODIN_DRY_RUN" "${script}" || fail "script must document dry-run boundaries"
grep -Fq "No live email/calendar/GitHub/production mutation" "${doc}" || fail "doc must state no-live-mutation boundary"
grep -Fq "make odin-actual-use-e2e" "${doc}" || fail "doc must include the command"
grep -Fq "verified dry-run PR merge" "${doc}" || fail "doc must describe the delivery loop PR merge proof"

echo "ok"
