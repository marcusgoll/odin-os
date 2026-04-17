#!/usr/bin/env bash
set -euo pipefail

die() {
  echo "$*" >&2
  exit 1
}

require_file() {
  local path="$1"
  [[ -f "$path" ]] || die "missing body file: $path"
}

extract_section() {
  local file="$1"
  local heading="$2"
  awk -v heading="$heading" '
    $0 == heading { in_section = 1; next }
    in_section && /^## / { exit }
    in_section { print }
  ' "$file"
}

require_heading() {
  local file="$1"
  local heading="$2"
  grep -Fxq "$heading" "$file" || die "missing required heading: $heading"
}

has_non_placeholder_bullet() {
  local text="$1"
  shift
  local line trimmed skip

  while IFS= read -r line; do
    [[ "$line" =~ ^[[:space:]]*-[[:space:]]+ ]] || continue
    trimmed="${line#*- }"
    skip=0
    for placeholder in "$@"; do
      if [[ "$trimmed" == "$placeholder" ]]; then
        skip=1
        break
      fi
    done
    if [[ $skip -eq 0 && -n "${trimmed//[[:space:]]/}" ]]; then
      return 0
    fi
  done <<<"$text"

  return 1
}

extract_commands_block() {
  local text="$1"
  awk '
    /^```/ {
      if (in_block) {
        exit
      }
      in_block = 1
      next
    }
    in_block { print }
  ' <<<"$text"
}

has_real_command() {
  local text="$1"
  local line stripped
  while IFS= read -r line; do
    stripped="${line#"${line%%[![:space:]]*}"}"
    [[ -n "$stripped" ]] || continue
    [[ "$stripped" == \#* ]] && continue
    return 0
  done <<<"$text"
  return 1
}

checkbox_checked() {
  local file="$1"
  local label="$2"
  grep -Eq "^[[:space:]]*-[[:space:]]*\\[[xX]\\][[:space:]]+${label}$" "$file"
}

main() {
  [[ $# -eq 1 ]] || die "usage: verify-pr-template.sh <body-file>"
  local body_file="$1"
  local summary proven unproven commands_section commands_block
  local runtime_label='this PR changes user-visible or orchestration-facing behavior'
  local odin_label='if the box above is checked, real `odin` command proof is included below'

  require_file "$body_file"
  [[ -s "$body_file" ]] || die "pull request body is empty"

  require_heading "$body_file" "## Summary"
  require_heading "$body_file" "## Proven"
  require_heading "$body_file" "## Unproven"
  require_heading "$body_file" "## Commands Run"

  summary="$(extract_section "$body_file" "## Summary")"
  proven="$(extract_section "$body_file" "## Proven")"
  unproven="$(extract_section "$body_file" "## Unproven")"
  commands_section="$(extract_section "$body_file" "## Commands Run")"
  commands_block="$(extract_commands_block "$commands_section")"

  has_non_placeholder_bullet "$summary" \
    "describe the change in a few concrete bullets" \
    || die "summary must include at least one non-placeholder bullet"

  has_non_placeholder_bullet "$proven" \
    'list only behaviors directly demonstrated by tests or real `odin` commands' \
    || die "proven must include at least one non-placeholder bullet"

  has_non_placeholder_bullet "$unproven" \
    "list behaviors not exercised" \
    "list external dependencies that remained stubbed or deferred" \
    "list residual risks that still rely on reasoning instead of evidence" \
    || die "unproven must include at least one non-placeholder bullet"

  has_real_command "$commands_block" || die "commands run must include at least one real command"

  if checkbox_checked "$body_file" "$runtime_label"; then
    checkbox_checked "$body_file" "$odin_label" || die "user-visible changes must check the odin proof box"
    grep -Eq '(^|[[:space:]])([^[:space:]]*/)?odin([[:space:]]|$)' <<<"$commands_block" \
      || die "user-visible changes must include a real odin command in commands run"
  fi
}

main "$@"
