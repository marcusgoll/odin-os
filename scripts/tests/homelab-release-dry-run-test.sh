#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
script="${repo_root}/scripts/ops/homelab-release-dry-run.sh"
runbook="${repo_root}/docs/operations/homelab-odin-runbook.md"
makefile="${repo_root}/Makefile"

fail() {
  echo "homelab-release-dry-run-test: $*" >&2
  exit 1
}

[[ -x "${script}" ]] || fail "dry-run script must be executable"
[[ -f "${runbook}" ]] || fail "homelab runbook must exist"

grep -Fq "scripts/install-service.sh --dry-run --start" "${script}" || fail "script must prove service install dry-run"
grep -Fq "./bin/odin backup --help" "${script}" || fail "script must check backup help"
grep -Fq "./bin/odin restore --help" "${script}" || fail "script must check restore help"
grep -Fq "./bin/odin verify-backup --help" "${script}" || fail "script must check verify-backup help"
grep -Fq "./bin/odin serve --help" "${script}" || fail "script must check serve help"
grep -Fq "./bin/odin doctor --json" "${script}" || fail "script must gate on doctor"
grep -Fq "./bin/odin healthcheck" "${script}" || fail "script must gate on healthcheck"
grep -Fq "./bin/odin overview --json" "${script}" || fail "script must gate on overview"
grep -Fq "./bin/odin work status --json" "${script}" || fail "script must gate on work status"
grep -Fq "./bin/odin review list --json" "${script}" || fail "script must gate on review queue"
grep -Fq "./bin/odin approvals all --json" "${script}" || fail "script must gate on approvals"
grep -Fq "make odin-actual-use-e2e" "${runbook}" || fail "runbook must include actual-use e2e gate"
grep -Fq "verify-backup" "${runbook}" || fail "runbook must require backup verification"
grep -Fq "rollback" "${runbook}" || fail "runbook must document rollback"
grep -Fq "homelab-release-dry-run" "${makefile}" || fail "Makefile must expose dry-run target"
grep -Fq "odin-actual-use-e2e" "${makefile}" || fail "Makefile must expose actual-use target"

echo "ok"
