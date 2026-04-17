#!/usr/bin/env bash
set -euo pipefail

cat >/dev/null

printf '%s\n' '{"status":"ok","summary":"triage complete","output":{"classification":"triage","next_step":"plan"}}'
