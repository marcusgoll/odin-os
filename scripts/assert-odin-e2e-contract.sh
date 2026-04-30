#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

REPORT_DIR=".odin/e2e"
METADATA="$REPORT_DIR/run-metadata.json"
LATEST_JSON="$REPORT_DIR/latest.json"
LATEST_LOG="$REPORT_DIR/latest.log"

fail() {
  echo "odin e2e contract failed: $*" >&2
  exit 1
}

require_file() {
  [[ -f "$1" ]] || fail "missing file: $1"
}

require_log_contains() {
  local expected="$1"
  grep -q -- "$expected" "$LATEST_LOG" || fail "latest.log missing: $expected"
}

require_file "$METADATA"
require_file "$LATEST_JSON"
require_file "$LATEST_LOG"

require_log_contains "go fmt"
require_log_contains "go vet"
require_log_contains "go test"
require_log_contains "go build"
require_log_contains "doctor"
require_log_contains "odin e2e"

if command -v jq >/dev/null 2>&1; then
  jq -e '.command == "make odin-e2e-local"' "$METADATA" >/dev/null || fail "run metadata command mismatch"
  jq -e '.status == "passed"' "$LATEST_JSON" >/dev/null || fail "latest.json status is not passed"
else
  grep -q '"command": "make odin-e2e-local"' "$METADATA" || fail "run metadata command mismatch"
  grep -q '"status": "passed"' "$LATEST_JSON" || fail "latest.json status is not passed"
fi

echo "ok"
