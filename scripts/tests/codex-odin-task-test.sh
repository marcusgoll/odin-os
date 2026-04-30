#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
script="$repo_root/scripts/codex-odin-task.sh"

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
trap 'rm -rf "$tmpdir"' EXIT

mkdir -p "$tmpdir/scripts" "$tmpdir/bin"
cp "$script" "$tmpdir/scripts/codex-odin-task.sh"

cat >"$tmpdir/bin/codex" <<EOF
#!/usr/bin/env bash
set -euo pipefail

printf '%s\n' "\$@" > "$tmpdir/codex-args.txt"

output_file=""
while [[ \$# -gt 0 ]]; do
  case "\$1" in
    --output-last-message)
      output_file="\$2"
      shift 2
      ;;
    *)
      shift
      ;;
  esac
done

[[ -n "\$output_file" ]] || exit 2
printf 'fake codex final message\n' > "\$output_file"
EOF
chmod +x "$tmpdir/bin/codex"

task="Add fixture-backed odin e2e scenario for GitHub read-only intake."
PATH="$tmpdir/bin:$PATH" "$tmpdir/scripts/codex-odin-task.sh" "$task"

prompt_file="$tmpdir/.codex-runs/current-task.md"
output_file="$tmpdir/.codex-runs/last-message.md"
args_file="$tmpdir/codex-args.txt"

require_file "$prompt_file"
require_file "$output_file"
require_file "$args_file"

require_contains "$args_file" "exec"
require_contains "$args_file" "--cd"
require_contains "$args_file" "$tmpdir"
require_contains "$args_file" "--output-last-message"
require_contains "$args_file" ".codex-runs/last-message.md"

require_contains "$prompt_file" "You are working in the Odin-OS repository."
require_contains "$prompt_file" "Read AGENTS.md."
require_contains "$prompt_file" "Read WORKFLOW.md."
require_contains "$prompt_file" "Inspect existing implementation."
require_contains "$prompt_file" "$task"
require_contains "$prompt_file" "go fmt ./..."
require_contains "$prompt_file" "go vet ./..."
require_contains "$prompt_file" "go test ./..."
require_contains "$prompt_file" "go build -o ./bin/odin ./cmd/odin-os"
require_contains "$prompt_file" "make odin-e2e-local"
require_contains "$prompt_file" "E2E report"
require_contains "$prompt_file" "Go/No-Go Recommendation"
require_contains "$output_file" "fake codex final message"

echo "ok"
