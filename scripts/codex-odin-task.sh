#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 1 ]]; then
  echo "Usage: $0 '<task prompt>'"
  exit 1
fi

TASK="$1"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

mkdir -p .codex-runs

PROMPT_FILE=".codex-runs/current-task.md"
OUTPUT_FILE=".codex-runs/last-message.md"

cat >"$PROMPT_FILE" <<EOF
You are working in the Odin-OS repository.

Before editing:
- Read AGENTS.md.
- Read WORKFLOW.md.
- Inspect existing implementation.
- Reuse before rebuilding.
- Do not create duplicate systems.

Task:
$TASK

Required verification before final answer:
- go fmt ./...
- go vet ./...
- go test ./...
- go build -o ./bin/odin ./cmd/odin-os
- make odin-e2e-local

Required final answer:
- Current State
- Existing Code Reused
- New Code Added
- Files Changed
- Canonical Architecture Decision
- Acceptance Criteria Status
- Tests and Verification
- Behavior Changes
- Security and Safety Notes
- E2E report
- Remaining Risks
- Follow-up Tickets
- Go/No-Go Recommendation
EOF

codex exec \
  --cd "$ROOT" \
  --output-last-message "$OUTPUT_FILE" \
  "$(cat "$PROMPT_FILE")"
