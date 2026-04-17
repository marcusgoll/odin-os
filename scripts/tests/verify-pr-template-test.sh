#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
script="$repo_root/scripts/ci/verify-pr-template.sh"

fail() {
  echo "test failed: $*" >&2
  exit 1
}

require_file() {
  [[ -f "$1" ]] || fail "missing file: $1"
}

write_body() {
  local path="$1"
  cat >"$path"
}

run_pass() {
  local body_file="$1"
  if ! bash "$script" "$body_file" >/dev/null; then
    fail "expected success for $body_file"
  fi
}

run_fail() {
  local body_file="$1"
  if bash "$script" "$body_file" >/dev/null 2>&1; then
    fail "expected failure for $body_file"
  fi
}

require_file "$script"

tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

valid_non_runtime="$tmpdir/valid-non-runtime.md"
write_body "$valid_non_runtime" <<'EOF'
## Summary

- add a PR template contract check

## Verification Contract

- [x] unit coverage added or updated where applicable
- [x] contract coverage added or updated where applicable
- [x] integration coverage added or updated where applicable
- [ ] this PR changes user-visible or orchestration-facing behavior
- [ ] if the box above is checked, real `odin` command proof is included below

## Proven

- the PR body validator rejects placeholder content

## Unproven

- the workflow has not been exercised on GitHub in this local run

## Commands Run

```bash
bash scripts/tests/verify-pr-template-test.sh
```
EOF
run_pass "$valid_non_runtime"

valid_runtime="$tmpdir/valid-runtime.md"
write_body "$valid_runtime" <<'EOF'
## Summary

- tighten runtime verification reporting for operator-facing changes

## Verification Contract

- [x] unit coverage added or updated where applicable
- [x] contract coverage added or updated where applicable
- [x] integration coverage added or updated where applicable
- [x] this PR changes user-visible or orchestration-facing behavior
- [x] if the box above is checked, real `odin` command proof is included below

## Proven

- fresh runtime health was proven through the real CLI

## Unproven

- long-running production traffic was not exercised in this local run

## Commands Run

```bash
make test-alpha
ODIN_ROOT="$(mktemp -d)" ./bin/odin healthcheck
```
EOF
run_pass "$valid_runtime"

placeholder_summary="$tmpdir/placeholder-summary.md"
write_body "$placeholder_summary" <<'EOF'
## Summary

- describe the change in a few concrete bullets

## Verification Contract

- [x] unit coverage added or updated where applicable
- [x] contract coverage added or updated where applicable
- [x] integration coverage added or updated where applicable
- [ ] this PR changes user-visible or orchestration-facing behavior
- [ ] if the box above is checked, real `odin` command proof is included below

## Proven

- the validator exists

## Unproven

- GitHub execution was not exercised locally

## Commands Run

```bash
bash scripts/tests/verify-pr-template-test.sh
```
EOF
run_fail "$placeholder_summary"

missing_proven="$tmpdir/missing-proven.md"
write_body "$missing_proven" <<'EOF'
## Summary

- add PR validation

## Verification Contract

- [x] unit coverage added or updated where applicable
- [x] contract coverage added or updated where applicable
- [x] integration coverage added or updated where applicable
- [ ] this PR changes user-visible or orchestration-facing behavior
- [ ] if the box above is checked, real `odin` command proof is included below

## Unproven

- GitHub execution was not exercised locally

## Commands Run

```bash
bash scripts/tests/verify-pr-template-test.sh
```
EOF
run_fail "$missing_proven"

comment_only_commands="$tmpdir/comment-only-commands.md"
write_body "$comment_only_commands" <<'EOF'
## Summary

- add PR validation

## Verification Contract

- [x] unit coverage added or updated where applicable
- [x] contract coverage added or updated where applicable
- [x] integration coverage added or updated where applicable
- [ ] this PR changes user-visible or orchestration-facing behavior
- [ ] if the box above is checked, real `odin` command proof is included below

## Proven

- the validator exists

## Unproven

- GitHub execution was not exercised locally

## Commands Run

```bash
# tests only
```
EOF
run_fail "$comment_only_commands"

runtime_without_odin="$tmpdir/runtime-without-odin.md"
write_body "$runtime_without_odin" <<'EOF'
## Summary

- change CLI behavior

## Verification Contract

- [x] unit coverage added or updated where applicable
- [x] contract coverage added or updated where applicable
- [x] integration coverage added or updated where applicable
- [x] this PR changes user-visible or orchestration-facing behavior
- [x] if the box above is checked, real `odin` command proof is included below

## Proven

- the runtime path was updated

## Unproven

- production rollout was not exercised locally

## Commands Run

```bash
make test-alpha
```
EOF
run_fail "$runtime_without_odin"

runtime_without_checkbox="$tmpdir/runtime-without-checkbox.md"
write_body "$runtime_without_checkbox" <<'EOF'
## Summary

- change CLI behavior

## Verification Contract

- [x] unit coverage added or updated where applicable
- [x] contract coverage added or updated where applicable
- [x] integration coverage added or updated where applicable
- [x] this PR changes user-visible or orchestration-facing behavior
- [ ] if the box above is checked, real `odin` command proof is included below

## Proven

- fresh runtime health was proven through the real CLI

## Unproven

- production rollout was not exercised locally

## Commands Run

```bash
ODIN_ROOT="$(mktemp -d)" ./bin/odin healthcheck
```
EOF
run_fail "$runtime_without_checkbox"

echo "ok"
