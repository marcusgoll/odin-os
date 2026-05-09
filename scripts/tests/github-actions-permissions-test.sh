#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

fail() {
  echo "test failed: $*" >&2
  exit 1
}

require_contains() {
  local path="$1"
  local expected="$2"
  grep -Fq -- "$expected" "$path" || fail "$path missing: $expected"
}

require_absent() {
  local path="$1"
  local unexpected="$2"
  if grep -Fq -- "$unexpected" "$path"; then
    fail "$path must not contain: $unexpected"
  fi
}

shopt -s nullglob
workflows=("$repo_root"/.github/workflows/*.yml "$repo_root"/.github/workflows/*.yaml)
[[ "${#workflows[@]}" -gt 0 ]] || fail "no GitHub Actions workflows found"

for workflow in "${workflows[@]}"; do
  require_contains "$workflow" "permissions:"
  require_contains "$workflow" "contents: read"
  require_absent "$workflow" "pull_request_target:"
  require_absent "$workflow" "secrets."

  if grep -Eq '^[[:space:]]+write-all[[:space:]]*$' "$workflow"; then
    fail "$workflow must not use write-all permissions"
  fi

  if grep -Eq '^[[:space:]]+[A-Za-z-]+:[[:space:]]+write[[:space:]]*$' "$workflow"; then
    fail "$workflow has write permissions; document and test the need before allowing them"
  fi
done

echo "ok"
