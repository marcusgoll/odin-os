#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
script="$repo_root/scripts/assert-odin-e2e-contract.sh"

fail() {
  echo "test failed: $*" >&2
  exit 1
}

require_contains() {
  local file="$1"
  local expected="$2"
  grep -Fq -- "$expected" "$file" || fail "$file missing: $expected"
}

[[ -x "$script" ]] || fail "missing executable script: $script"

tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

mkdir -p "$tmpdir/scripts" "$tmpdir/.odin/e2e"
cp "$script" "$tmpdir/scripts/assert-odin-e2e-contract.sh"
chmod +x "$tmpdir/scripts/assert-odin-e2e-contract.sh"

cat >"$tmpdir/.odin/e2e/run-metadata.json" <<'EOF'
{
  "command": "make odin-e2e-local"
}
EOF

cat >"$tmpdir/.odin/e2e/latest.json" <<'EOF'
{
  "status": "passed"
}
EOF

cat >"$tmpdir/.odin/e2e/latest.log" <<'EOF'
go fmt ./...
go vet ./...
go test ./...
go build -o ./bin/odin ./cmd/odin-os
./bin/odin doctor --json
odin e2e fixtures/e2e/github-readonly-intake.yaml
EOF

output="$("$tmpdir/scripts/assert-odin-e2e-contract.sh")"
[[ "$output" == "ok" ]] || fail "unexpected success output: $output"

sed -i '/go vet/d' "$tmpdir/.odin/e2e/latest.log"
if "$tmpdir/scripts/assert-odin-e2e-contract.sh" >"$tmpdir/fail.out" 2>"$tmpdir/fail.err"; then
  fail "contract script succeeded without go vet proof"
fi
require_contains "$tmpdir/fail.err" "latest.log missing: go vet"

echo "ok"
