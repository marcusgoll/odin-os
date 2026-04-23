#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SCRIPT="${ROOT_DIR}/scripts/ops/export-live-n8n-odin-targets.sh"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "${TMP_DIR}"' EXIT

cat >"${TMP_DIR}/workflow-array.json" <<'JSON'
[
  {
    "id": 1,
    "name": "Dispatch Envelope",
    "active": true,
    "nodes": [
      {
        "name": "SSH",
        "type": "n8n-nodes-base.executeCommand",
        "parameters": {
          "command": "ssh orchestrator@172.17.0.1 -i /home/node/.ssh/odin_ingress /bin/sh -lc 'printf envelope | base64 -d | odin task create'"
        }
      }
    ]
  },
  {
    "id": 2,
    "name": "Legacy Helper",
    "active": true,
    "nodes": [
      {
        "name": "SSH",
        "type": "n8n-nodes-base.executeCommand",
        "parameters": {
          "command": "ssh orchestrator@172.17.0.1 -i /home/node/.ssh/odin_ingress /bin/sh -lc 'dedup-check ci_failure pbs && nonce-update pbs'"
        }
      }
    ]
  },
  {
    "id": 3,
    "name": "Legacy Script",
    "active": true,
    "nodes": [
      {
        "name": "SSH",
        "type": "n8n-nodes-base.executeCommand",
        "parameters": {
          "command": "ssh orchestrator@172.17.0.1 -i /home/node/.ssh/odin_ingress /bin/sh -lc '/var/odin/scripts/legacy-thing.sh'"
        }
      }
    ]
  },
  {
    "id": 4,
    "name": "Inactive Workflow",
    "active": false,
    "nodes": [
      {
        "name": "SSH",
        "type": "n8n-nodes-base.executeCommand",
        "parameters": {
          "command": "ssh orchestrator@172.17.0.1 -i /home/node/.ssh/odin_ingress /bin/sh -lc 'dedup-check ci_failure pbs'"
        }
      }
    ]
  },
  {
    "id": 5,
    "name": "Metadata Mention Mentions dedup-check",
    "active": true,
    "notes": "legacy helper workflow using nonce-update someday",
    "nodes": [
      {
        "name": "Noop",
        "type": "n8n-nodes-base.set",
        "parameters": {
          "keepOnlySet": true
        }
      }
    ]
  }
]
JSON

expected="$(cat <<'EOF'
1	Dispatch Envelope	dispatch_envelope
2	Legacy Helper	legacy_helper
3	Legacy Script	legacy_script
EOF
)"

assert_output() {
  local input_path="$1"
  local label="$2"
  local output

  output="$(N8N_WORKFLOWS_EXPORT_FILE="${input_path}" "${SCRIPT}" --format tsv)"

  if [[ "${output}" != "${expected}" ]]; then
    printf 'unexpected export output for %s\n' "${label}" >&2
    printf 'expected:\n%s\n' "${expected}" >&2
    printf 'actual:\n%s\n' "${output}" >&2
    exit 1
  fi
}

assert_output "${TMP_DIR}/workflow-array.json" "array root"

for root_key in workflows data items; do
  {
    printf '{ "%s": ' "${root_key}"
    cat "${TMP_DIR}/workflow-array.json"
    printf '}\n'
  } >"${TMP_DIR}/workflow-${root_key}.json"
  assert_output "${TMP_DIR}/workflow-${root_key}.json" "${root_key} root"
done

printf 'export-live-n8n-odin-targets-test: ok\n'
