#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
workflow="$repo_root/.github/workflows/odin-e2e.yml"

fail() {
  echo "test failed: $*" >&2
  exit 1
}

require_contains() {
  local expected="$1"
  grep -Fq -- "$expected" "$workflow" || fail "workflow missing: $expected"
}

[[ -f "$workflow" ]] || fail "missing workflow: $workflow"

require_contains "name: Odin E2E"
require_contains "pull_request:"
require_contains "push:"
require_contains "branches: [main]"
require_contains "uses: actions/checkout@v4"
require_contains "uses: actions/setup-go@v5"
require_contains "go-version-file: go.mod"
require_contains "run: |"
require_contains "make odin-e2e-local"
require_contains "make odin-e2e-contract"
require_contains "uses: actions/upload-artifact@v4"
require_contains "name: odin-e2e"
require_contains ".odin/e2e/*.json"
require_contains ".odin/e2e/*.log"

echo "ok"
