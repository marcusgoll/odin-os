#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

fail() {
  echo "test failed: $*" >&2
  exit 1
}

require_file() {
  [[ -f "$1" ]] || fail "missing file: $1"
}

require_contains() {
  local pattern="$1"
  local path="$2"
  rg -q --fixed-strings "$pattern" "$path" || fail "missing pattern '$pattern' in $path"
}

contract="$repo_root/docs/contracts/media-stack-operations.md"
homelab_contract="$repo_root/docs/contracts/homelab-operations.md"
cutover_readiness="$repo_root/docs/operations/cutover-readiness.md"
playbook_root="$repo_root/docs/operations/media-stack"

playbooks=(
  "$playbook_root/README.md"
  "$playbook_root/plex-down.md"
  "$playbook_root/disk-pressure.md"
  "$playbook_root/vpn-downloader.md"
  "$playbook_root/import-failures.md"
  "$playbook_root/mount-mismatch.md"
  "$playbook_root/seedbox-sync.md"
  "$playbook_root/indexer-degradation.md"
)

require_file "$contract"
require_contains "bounded media supervision" "$contract"
require_contains "existing homelab substrate" "$contract"
require_contains "safe automatic actions" "$contract"
require_contains "approval-required actions" "$contract"
require_contains "fail closed" "$contract"
require_contains "approval-required" "$contract"
require_contains "forbidden" "$contract"

for playbook in "${playbooks[@]}"; do
  require_file "$playbook"
done

for playbook in "${playbooks[@]:1}"; do
  require_contains "## Trigger" "$playbook"
  require_contains "## Evidence" "$playbook"
  require_contains "## Safe Actions" "$playbook"
  require_contains "## Approval-Required Actions" "$playbook"
  require_contains "## Rollback Trigger" "$playbook"
  require_contains "## Closeout" "$playbook"
done

require_contains "optional media supervision profile" "$homelab_contract"
require_contains "Media profile" "$cutover_readiness"
require_contains "mount audit passes before any approved media maintenance" "$cutover_readiness"
require_contains "operator has reviewed explicit safe vs unsafe media automation boundaries" "$cutover_readiness"

echo "ok"
