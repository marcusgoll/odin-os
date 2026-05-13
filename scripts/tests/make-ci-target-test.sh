#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

fail() {
  echo "test failed: $*" >&2
  exit 1
}

output="$(make -n -C "$repo_root" ci)"
makefile="$(<"$repo_root/Makefile")"

grep -Fq "git ls-files '*.go'" <<<"$makefile" || fail "GOFILES must use tracked files only"
if grep -Fq "find . -type f -name '*.go'" <<<"$makefile"; then
  fail "GOFILES must not recursively scan nested worktrees"
fi

grep -Fq 'test -z "$(gofmt -l ' <<<"$output" || fail "missing fmtcheck command"
grep -Fqx 'go vet ./...' <<<"$output" || fail "missing lint command"
grep -Fqx 'go test ./...' <<<"$output" || fail "missing test command"
grep -Fqx 'bash scripts/tests/make-ci-target-test.sh' <<<"$output" || fail "missing ci target dry-run test"
grep -Fqx 'bash scripts/tests/verify-pr-template-test.sh' <<<"$output" || fail "missing pr template validator test"
grep -Fqx 'bash scripts/tests/install-service-test.sh' <<<"$output" || fail "missing install service test"
grep -Fqx 'bash scripts/tests/assert-odin-e2e-contract-test.sh' <<<"$output" || fail "missing odin e2e contract test"
grep -Fqx 'bash scripts/tests/odin-e2e-workflow-test.sh' <<<"$output" || fail "missing odin e2e workflow test"
grep -Fqx 'bash scripts/tests/actual-use-phase0-proof-test.sh' <<<"$output" || fail "missing actual-use phase0 proof contract test"
grep -Fqx 'bash scripts/tests/github-actions-permissions-test.sh' <<<"$output" || fail "missing GitHub Actions permissions test"
grep -Fqx 'bash scripts/tests/google-driver-security-test.sh' <<<"$output" || fail "missing Google driver security test"
grep -Fqx 'bash scripts/tests/work-intake-live-smoke-test.sh' <<<"$output" || fail "missing work intake live smoke contract test"
grep -Fqx 'bash scripts/tests/docker-compose-smoke-test.sh' <<<"$output" || fail "missing Docker Compose smoke contract test"
grep -Fqx 'go test ./tests/integration -run TestAlphaAcceptance -count=1 -v' <<<"$output" || fail "missing alpha acceptance command"
grep -Fqx 'mkdir -p bin' <<<"$output" || fail "missing build mkdir"
grep -Fqx 'go build -o bin/odin ./cmd/odin' <<<"$output" || fail "missing build command"

docker_smoke_output="$(make -n -C "$repo_root" docker-smoke)"
grep -Fqx 'bash scripts/tests/docker-compose-smoke.sh' <<<"$docker_smoke_output" || fail "missing docker-smoke script invocation"

e2e_output="$(make -n -C "$repo_root" odin-e2e-local)"
grep -Fqx './scripts/odin-e2e-local.sh' <<<"$e2e_output" || fail "missing odin e2e script invocation"
if grep -Fq 'go build -o bin/odin ./cmd/odin' <<<"$e2e_output"; then
  fail "odin-e2e-local should delegate to the canonical script instead of Makefile build"
fi

contract_output="$(make -n -C "$repo_root" odin-e2e-contract)"
grep -Fqx './scripts/assert-odin-e2e-contract.sh' <<<"$contract_output" || fail "missing odin e2e contract script invocation"

echo "ok"
