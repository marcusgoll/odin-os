#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
script="${repo_root}/scripts/odin-phone-release-check.sh"
runbook="${repo_root}/docs/operations/phone-to-odin-runbook.md"
makefile="${repo_root}/Makefile"

fail() {
  echo "odin-phone-release-check-test: $*" >&2
  exit 1
}

[[ -x "${script}" ]] || fail "phone release check script must be executable"
[[ -f "${runbook}" ]] || fail "phone-to-Odin runbook must exist"

grep -Fq "odin-phone-release-check:" "${makefile}" || fail "Makefile must expose odin-phone-release-check"
grep -Fq "odin-mobile-e2e:" "${makefile}" || fail "Makefile must expose odin-mobile-e2e"
grep -Fq "./scripts/odin-phone-release-check.sh" "${makefile}" || fail "Make target must call phone release script"

for required in \
  "binary_mode=source_local" \
  "ODIN_ROOT=" \
  "ODIN_HTTP_ADDR=" \
  "ODIN_ADMIN_TOKEN=" \
  "TestOdinPhoneReleaseCheck" \
  "TestMobileShare" \
  "TestNotification" \
  "TestPWA" \
  "external_mutation=none" \
  "stub:labeled" \
  "push.example.test"; do
  grep -Fq "${required}" "${script}" || fail "script missing proof marker: ${required}"
done

for required in \
  "make odin-phone-release-check" \
  "source-local" \
  "private network" \
  "No public exposure" \
  "Stubbed vs Real Behavior" \
  "Huginn browser evidence" \
  "push.example.test" \
  "ODIN_ROOT"; do
  grep -Fq "${required}" "${runbook}" || fail "runbook missing release guidance: ${required}"
done

echo "ok"
