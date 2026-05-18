#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

mkdir -p .odin/e2e
E2E_RUNTIME_ROOT="$ROOT/.odin/e2e/runtime"
rm -rf "$E2E_RUNTIME_ROOT"
mkdir -p "$E2E_RUNTIME_ROOT"
export GOCACHE="${GOCACHE:-$ROOT/.odin/e2e/go-build-cache}"
mkdir -p "$GOCACHE"

: >.odin/e2e/latest.log
: >.odin/e2e/latest.json

cat >.odin/e2e/run-metadata.json <<EOF
{
  "command": "make odin-e2e-local",
  "timestamp": "$(date -u +"%Y-%m-%dT%H:%M:%SZ")",
  "git_sha": "$(git rev-parse HEAD)",
  "git_branch": "$(git branch --show-current)",
  "runner": "${USER:-unknown}",
  "pwd": "$(pwd)",
  "odin_root": "$E2E_RUNTIME_ROOT"
}
EOF

run_logged() {
  echo "$*" | tee -a .odin/e2e/latest.log
  "$@" 2>&1 | tee -a .odin/e2e/latest.log
}

mapfile -t go_packages < <(go list ./... | grep -v '/node_modules/' | grep -v '/.worktrees/')

run_logged go fmt ./...
run_logged go vet "${go_packages[@]}"
run_logged go test "${go_packages[@]}"

mkdir -p bin
run_logged go build -o ./bin/odin ./cmd/odin-os

echo "./bin/odin doctor --json" | tee -a .odin/e2e/latest.log
ODIN_ROOT="$E2E_RUNTIME_ROOT" ODIN_DRY_RUN=true ./bin/odin doctor --json | tee .odin/e2e/doctor.json | tee -a .odin/e2e/latest.log >/dev/null

if ! ./bin/odin e2e --help >/dev/null 2>&1; then
  {
    echo "odin e2e command is unavailable."
    echo "Follow-up ticket: implement a minimal fixture-backed odin e2e command before claiming local E2E enforcement."
  } | tee -a .odin/e2e/latest.log >&2
  exit 1
fi

scenarios=(fixtures/e2e/*.yaml)
if [[ ${#scenarios[@]} -eq 0 ]]; then
  echo "no fixture E2E scenarios found under fixtures/e2e/" | tee -a .odin/e2e/latest.log >&2
  exit 1
fi

for scenario in "${scenarios[@]}"; do
  name="$(basename "$scenario" .yaml)"
  echo "odin e2e $scenario" | tee ".odin/e2e/$name.log" | tee -a .odin/e2e/latest.log
  ./bin/odin e2e --scenario "$scenario" --json | tee ".odin/e2e/$name.json" | tee -a ".odin/e2e/$name.log" | tee -a .odin/e2e/latest.log | tee .odin/e2e/latest.json
done

echo "./bin/odin e2e --scenario fixtures/e2e/software-factory-lane.yaml --json" | tee -a .odin/e2e/latest.log
./bin/odin e2e --scenario fixtures/e2e/software-factory-lane.yaml --json | tee .odin/e2e/software-factory-lane.explicit.json | tee -a .odin/e2e/latest.log | tee .odin/e2e/latest.json
