#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
script="$repo_root/scripts/odin-e2e-local.sh"

fail() {
  echo "test failed: $*" >&2
  exit 1
}

require_file() {
  [[ -f "$1" ]] || fail "missing file: $1"
}

require_contains() {
  local file="$1"
  local expected="$2"
  grep -Fq -- "$expected" "$file" || fail "$file missing: $expected"
}

require_file "$script"

tmpdir="$(mktemp -d)"
faildir=""
trap 'rm -rf "$tmpdir" "$faildir"' EXIT

mkdir -p "$tmpdir/scripts" "$tmpdir/bin" "$tmpdir/fixtures/e2e" "$tmpdir/cmd/odin-os"
cp "$script" "$tmpdir/scripts/odin-e2e-local.sh"
chmod +x "$tmpdir/scripts/odin-e2e-local.sh"
cat >"$tmpdir/fixtures/e2e/fixture-one.yaml" <<'EOF'
name: fixture-one
EOF
cat >"$tmpdir/bin/go" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf 'GOCACHE=%s\n' "${GOCACHE:-}" >> go-env.log
printf '%s\n' "go $*" >> go-commands.log
if [[ "$1" == "list" ]]; then
  printf 'fixture/module\n'
  exit 0
fi
if [[ "$1" == "build" ]]; then
  output=""
  shift
  while [[ $# -gt 0 ]]; do
    case "$1" in
      -o)
        output="$2"
        shift 2
        ;;
      *)
        shift
        ;;
    esac
  done
  if [[ -n "$output" ]]; then
    cat >"$output" <<'EOS'
#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "doctor" ]]; then
  printf 'ODIN_ROOT=%s\n' "${ODIN_ROOT:-}" >> odin-env.log
  printf 'ODIN_DRY_RUN=%s\n' "${ODIN_DRY_RUN:-}" >> odin-env.log
  printf '{"status":"healthy","checks":[]}\n'
  exit 0
fi
if [[ "${1:-}" == "e2e" ]]; then
  printf '{"status":"passed","scenario":"fixture-one"}\n'
  exit 0
fi
printf 'unexpected odin command: %s\n' "$*" >&2
exit 1
EOS
    chmod +x "$output"
  fi
fi
EOF
chmod +x "$tmpdir/bin/go"

git -C "$tmpdir" init -q
git -C "$tmpdir" config user.email "odin@example.test"
git -C "$tmpdir" config user.name "Odin Test"
git -C "$tmpdir" add .
git -C "$tmpdir" commit -q -m "fixture"

PATH="$tmpdir/bin:$PATH" "$tmpdir/scripts/odin-e2e-local.sh"

require_file "$tmpdir/.odin/e2e/run-metadata.json"
require_file "$tmpdir/.odin/e2e/latest.json"
require_file "$tmpdir/.odin/e2e/latest.log"
require_file "$tmpdir/.odin/e2e/doctor.json"
require_file "$tmpdir/.odin/e2e/fixture-one.json"
require_file "$tmpdir/.odin/e2e/fixture-one.log"

require_contains "$tmpdir/go-commands.log" "go fmt ./..."
require_contains "$tmpdir/go-commands.log" "go list ./..."
require_contains "$tmpdir/go-commands.log" "go vet fixture/module"
require_contains "$tmpdir/go-commands.log" "go test fixture/module"
require_contains "$tmpdir/go-commands.log" "go build -o ./bin/odin ./cmd/odin-os"
require_contains "$tmpdir/go-env.log" "GOCACHE=$tmpdir/.odin/e2e/go-build-cache"
require_contains "$tmpdir/.odin/e2e/run-metadata.json" '"command": "make odin-e2e-local"'
require_contains "$tmpdir/.odin/e2e/run-metadata.json" '"git_sha":'
require_contains "$tmpdir/.odin/e2e/run-metadata.json" '"git_branch":'
require_contains "$tmpdir/.odin/e2e/run-metadata.json" '"runner":'
require_contains "$tmpdir/.odin/e2e/run-metadata.json" '"pwd":'
require_contains "$tmpdir/.odin/e2e/run-metadata.json" '"odin_root":'
require_contains "$tmpdir/.odin/e2e/doctor.json" '"status":"healthy"'
require_contains "$tmpdir/.odin/e2e/latest.log" "go fmt ./..."
require_contains "$tmpdir/.odin/e2e/latest.log" "./bin/odin doctor --json"
require_contains "$tmpdir/.odin/e2e/latest.json" '"scenario":"fixture-one"'
require_contains "$tmpdir/.odin/e2e/latest.log" "odin e2e fixtures/e2e/fixture-one.yaml"
require_contains "$tmpdir/odin-env.log" "ODIN_ROOT=$tmpdir/.odin/e2e/runtime"
require_contains "$tmpdir/odin-env.log" "ODIN_DRY_RUN=true"

faildir="$(mktemp -d)"
mkdir -p "$faildir/scripts" "$faildir/bin" "$faildir/fixtures/e2e" "$faildir/cmd/odin-os"
cp "$script" "$faildir/scripts/odin-e2e-local.sh"
chmod +x "$faildir/scripts/odin-e2e-local.sh"
cat >"$faildir/fixtures/e2e/fixture-one.yaml" <<'EOF'
name: fixture-one
EOF
cat >"$faildir/bin/go" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
if [[ "$1" == "vet" ]]; then
  echo "vet failed intentionally" >&2
  exit 42
fi
if [[ "$1" == "list" ]]; then
  printf 'fixture/module\n'
  exit 0
fi
exit 0
EOF
chmod +x "$faildir/bin/go"
git -C "$faildir" init -q
git -C "$faildir" config user.email "odin@example.test"
git -C "$faildir" config user.name "Odin Test"
git -C "$faildir" add .
git -C "$faildir" commit -q -m "fixture"

if PATH="$faildir/bin:$PATH" "$faildir/scripts/odin-e2e-local.sh" >/tmp/odin-e2e-fail.log 2>&1; then
  fail "script succeeded after go vet failed"
fi
require_contains "$faildir/.odin/e2e/latest.log" "go vet fixture/module"

echo "ok"
