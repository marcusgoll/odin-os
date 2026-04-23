#!/usr/bin/env bash
set -euo pipefail

codex_bin="${ODIN_CODEX_BIN:-codex}"

resolve_codex_exec_args() {
    local sandbox_mode="${ODIN_CODEX_SANDBOX_MODE:-}"
    local -a args=("exec")

    case "${sandbox_mode}" in
        "")
            args+=("--full-auto")
            ;;
        read-only|workspace-write)
            args+=("--sandbox" "${sandbox_mode}")
            ;;
        danger-full-access)
            args+=("--dangerously-bypass-approvals-and-sandbox")
            ;;
        *)
            printf 'unsupported ODIN_CODEX_SANDBOX_MODE: %s\n' "${sandbox_mode}" >&2
            return 1
            ;;
    esac

    printf '%s\0' "${args[@]}"
}

if [[ "${1:-}" == "--health" ]]; then
    if command -v "${codex_bin}" >/dev/null 2>&1; then
        printf '{"status":"healthy","details":"codex CLI available"}\n'
    else
        printf '{"status":"unavailable","details":"codex CLI not found"}\n'
    fi
    exit 0
fi

request_file="$(mktemp)"
prompt_file="$(mktemp)"
command_file="$(mktemp)"
mode_file="$(mktemp)"
stdout_file="$(mktemp)"
stderr_file="$(mktemp)"
message_file="$(mktemp)"
content_workdir=""
cleanup() {
    rm -f "${request_file}" "${prompt_file}" "${command_file}" "${mode_file}" "${stdout_file}" "${stderr_file}" "${message_file}"
    if [[ -n "${content_workdir}" ]]; then
        rm -rf "${content_workdir}"
    fi
}
trap cleanup EXIT

cat >"${request_file}"

legacy_action="$(
    python3 - "${request_file}" <<'PY'
import json
import sys

try:
    with open(sys.argv[1], "r", encoding="utf-8") as handle:
        request = json.load(handle)
except Exception:
    print("")
    raise SystemExit(0)

action = request.get("action")
print(action if isinstance(action, str) else "")
PY
)"

if [[ -n "${legacy_action}" || -n "${ODIN_CODEX_DRIVER_ACTION:-}" ]]; then
    payload="$(cat "${request_file}")"

    if [[ -n "${ODIN_CODEX_DRIVER_TRACE:-}" ]]; then
        printf '%s\n' "${payload}" >"${ODIN_CODEX_DRIVER_TRACE}"
    fi

    PAYLOAD="${payload}" LEGACY_ACTION="${legacy_action:-${ODIN_CODEX_DRIVER_ACTION:-}}" python3 - <<'PY'
import json
import os
import sys

payload = os.environ.get("PAYLOAD", "").strip()
request = json.loads(payload) if payload else {}
action = (os.environ.get("LEGACY_ACTION", "") or request.get("action") or "").strip()

if action == "health":
    response = json.loads(os.environ.get("ODIN_CODEX_DRIVER_HEALTH_RESPONSE", '{"status":"healthy","details":"fixture codex driver healthy"}'))
elif action == "run":
    response = json.loads(os.environ.get("ODIN_CODEX_DRIVER_RUN_RESPONSE", '{"status":"completed","output":"fixture codex driver"}'))
else:
    response = {
        "status": "unavailable",
        "details": f"unknown action: {action}",
    }

metadata = response.setdefault("metadata", {})
metadata.setdefault("driver", "codex_headless_script")
metadata.setdefault("mode", "fixture")
metadata.setdefault("executor_class", "plan_backed_cli")
json.dump(response, sys.stdout)
PY
    exit 0
fi

workdir="$(
    python3 - "${request_file}" "${prompt_file}" "${command_file}" "${mode_file}" <<'PY'
import json
import re
import sys

request_path, prompt_path, command_path, mode_path = sys.argv[1], sys.argv[2], sys.argv[3], sys.argv[4]
with open(request_path, "r", encoding="utf-8") as handle:
    request = json.load(handle)

def pick(mapping, *keys):
    for key in keys:
        value = mapping.get(key)
        if value not in (None, ""):
            return value
    return ""

def extract_exact_command(objective):
    phrases = (
        "execute this exact read-only command from the repo root and return only its stdout",
        "execute this exact read-only command from the repo root and return only its json result plus one sentence interpreting it",
        "execute this exact read-only command from the repo root",
        "execute this exact read-only command",
        "execute this exact command",
        "run this exact command",
        "run the following command",
    )
    lower_objective = objective.lower()
    for phrase in phrases:
        index = lower_objective.find(phrase)
        if index == -1:
            continue
        tail = objective[index + len(phrase):]
        colon_index = tail.find(":")
        if colon_index != -1:
            command = tail[colon_index + 1:].lstrip(" \n\t")
        else:
            command = tail.lstrip(" :\n\t")
        if command:
            return command
    return ""

def extract_registry_key(objective, label):
    match = re.search(rf"^{re.escape(label)}: .* \(([^)]+)\)\s*$", objective, re.MULTILINE)
    return (match.group(1).strip() if match else "")

operation = request.get("operation") or "run_task"
task = request.get("task") or {}
handle = request.get("handle") or {}
packet = request.get("packet") or {}
metadata = task.get("metadata") or task.get("Metadata") or {}
objective = pick(task, "prompt", "Prompt").strip()

workdir = metadata.get("worktree_path") or metadata.get("repo_root") or "."
lower_objective = objective.lower()
exact_command_requested = any(
    phrase in lower_objective
    for phrase in (
        "execute this exact command",
        "execute this exact read-only command",
        "run this exact command",
        "run the following command",
    )
)
exact_command = extract_exact_command(objective) if exact_command_requested else ""
workflow_key = extract_registry_key(objective, "Workflow")
skill_key = extract_registry_key(objective, "Skill")
content_mode = (
    not exact_command_requested
    and (
        workflow_key == "marcus-social-growth-workflow"
        or skill_key.startswith("marcus-")
    )
)

lines = [
    "Operate as the Odin codex_headless driver.",
    f"Operation: {operation}",
    "Do not return a generic completion banner when the task requests a concrete command result.",
]

if content_mode:
    lines.extend(
        [
            "This is a self-contained end-user content task inside Odin, not a repository engineering task.",
            "Do not inspect the repository, read local files, run tests, or invoke software-engineering process skills.",
            "Use the workflow and skill text above only as content, quality, and compliance guidance.",
            "Return only the user-facing deliverable requested in Task Request.",
        ]
    )
    if "draft one primary" in lower_objective:
        lines.append("If Task Request asks for one primary draft, return exactly one primary draft and a short approval checklist.")

if task:
    lines.extend(
        [
            f"Task ID: {pick(task, 'id', 'ID')}",
            f"Kind: {pick(task, 'kind', 'Kind')}",
            f"Scope: {pick(task, 'scope', 'Scope')}",
            "Primary objective:",
            objective,
        ]
    )
    if not content_mode:
        if metadata.get("project_key"):
            lines.append(f"Project: {metadata['project_key']}")
        if metadata.get("branch_name"):
            lines.append(f"Task branch: {metadata['branch_name']}")
        if metadata.get("worktree_path"):
            lines.append(f"Worktree: {metadata['worktree_path']}")
elif handle:
    lines.extend(
        [
            f"Resume external id: {handle.get('external_id', '')}",
            f"Resume summary: {packet.get('summary', '')}",
        ]
    )

if exact_command_requested:
    lines.extend(
        [
            "If the primary objective includes an exact command, execute that command verbatim from the stated worktree.",
            "Honor any requested response format exactly.",
        ]
    )
elif not content_mode:
    lines.append("Return only a concise status summary of what you investigated or changed.")

with open(prompt_path, "w", encoding="utf-8") as handle_out:
    handle_out.write("\n".join(lines).strip() + "\n")

with open(command_path, "w", encoding="utf-8") as handle_out:
    handle_out.write(exact_command)

with open(mode_path, "w", encoding="utf-8") as handle_out:
    handle_out.write("content" if content_mode else "general")

print(workdir)
PY
)"

execution_mode="$(cat "${mode_file}")"
if [[ "${execution_mode}" == "content" ]]; then
    content_workdir="$(mktemp -d)"
    workdir="${content_workdir}"
elif [[ ! -d "${workdir}" ]]; then
    workdir="$(pwd)"
fi

status="completed"
if [[ -s "${command_file}" ]]; then
    exact_command="$(cat "${command_file}")"
    if ! (
        cd "${workdir}" &&
        bash -c "${exact_command}"
    ) >"${stdout_file}" 2>"${stderr_file}"; then
        status="failed"
    fi
else
    if ! command -v "${codex_bin}" >/dev/null 2>&1; then
        printf '{"status":"failed","output":"codex CLI not found","metadata":{"lane":"driver","driver":"codex-headless"}}\n'
        exit 0
    fi

    mapfile -d '' -t codex_args < <(resolve_codex_exec_args)
    extra_codex_args=()
    if [[ "${execution_mode}" == "content" ]]; then
        extra_codex_args+=(--skip-git-repo-check --ignore-rules --ephemeral)
    fi

    if ! "${codex_bin}" "${codex_args[@]}" "${extra_codex_args[@]}" --cd "${workdir}" -o "${message_file}" - <"${prompt_file}" >"${stdout_file}" 2>"${stderr_file}"; then
        status="failed"
    fi
fi

python3 - "${status}" "${message_file}" "${stderr_file}" "${stdout_file}" <<'PY'
import json
import sys

status, message_path, stderr_path, stdout_path = sys.argv[1:5]

summary = ""
for path in (message_path, stderr_path, stdout_path):
    try:
        with open(path, "r", encoding="utf-8") as handle:
            summary = handle.read().strip()
    except FileNotFoundError:
        summary = ""
    if summary:
        break

if not summary:
    summary = "codex driver produced no summary"

print(
    json.dumps(
        {
            "status": status,
            "output": summary,
            "metadata": {
                "lane": "driver",
                "driver": "codex-headless",
            },
        }
    )
)
PY
