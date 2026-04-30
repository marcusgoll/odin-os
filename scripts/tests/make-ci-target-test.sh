#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

fail() {
  echo "test failed: $*" >&2
  exit 1
}

output="$(make -n -C "$repo_root" ci)"

grep -Fq 'test -z "$(gofmt -l ' <<<"$output" || fail "missing fmtcheck command"
grep -Fqx 'go vet ./...' <<<"$output" || fail "missing lint command"
grep -Fqx 'go test ./...' <<<"$output" || fail "missing test command"
grep -Fqx 'bash scripts/tests/make-ci-target-test.sh' <<<"$output" || fail "missing ci target dry-run test"
grep -Fqx 'bash scripts/tests/verify-pr-template-test.sh' <<<"$output" || fail "missing pr template validator test"
grep -Fqx 'bash scripts/tests/install-service-test.sh' <<<"$output" || fail "missing install service test"
grep -Fqx 'go test ./tests/integration -run TestAlphaAcceptance -count=1 -v' <<<"$output" || fail "missing alpha acceptance command"
grep -Fqx 'mkdir -p bin' <<<"$output" || fail "missing build mkdir"
grep -Fqx 'go build -o bin/odin ./cmd/odin' <<<"$output" || fail "missing build command"

echo "ok"
