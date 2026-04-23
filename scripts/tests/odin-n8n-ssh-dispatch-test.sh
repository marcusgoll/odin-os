#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SCRIPT="${ROOT_DIR}/scripts/ops/odin-n8n-ssh-dispatch.sh"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "${TMP_DIR}"' EXIT

mkdir -p "${TMP_DIR}/bin" "${TMP_DIR}/runtime"

cat >"${TMP_DIR}/bin/odin" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

STATE_DIR="$(cd "$(dirname "$0")/.." && pwd)/state"
mkdir -p "${STATE_DIR}"
COUNT_FILE="${STATE_DIR}/count"
count=0
if [[ -f "${COUNT_FILE}" ]]; then
  count="$(cat "${COUNT_FILE}")"
fi
count=$((count + 1))
printf '%s\n' "${count}" >"${COUNT_FILE}"
printf '%s\n' "$@" >"${STATE_DIR}/call-${count}.args"
cat >"${STATE_DIR}/call-${count}.stdin"
printf '{"status":"ok","call":%d}\n' "${count}"
EOF
chmod +x "${TMP_DIR}/bin/odin"

cat >"${TMP_DIR}/bin/cat" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

if [[ $# -eq 1 && -n "${ODIN_TEST_SLOW_CAT_PATH:-}" && "$1" == "${ODIN_TEST_SLOW_CAT_PATH}" ]]; then
  /bin/cat "$1"
  sleep "${ODIN_TEST_SLOW_CAT_SECONDS:-0}"
  exit 0
fi

exec /bin/cat "$@"
EOF
chmod +x "${TMP_DIR}/bin/cat"

cat >"${TMP_DIR}/envelope.json" <<'JSON'
{
  "schema_version": 1,
  "source": "n8n",
  "type": "ci_failure",
  "project_key": "pbs",
  "title": "Investigate PBS CI failure",
  "action_key": "",
  "dedup_key": "ci_failure:pbs:1234",
  "requested_by": "n8n",
  "payload": {
    "workflow_id": "pbs-ci-1",
    "run_id": "1234"
  }
}
JSON

env \
  ODIN_BIN="${TMP_DIR}/bin/odin" \
  ODIN_ROOT="${TMP_DIR}/runtime" \
  SSH_ORIGINAL_COMMAND="" \
  bash "${SCRIPT}" <"${TMP_DIR}/envelope.json" >"${TMP_DIR}/intake.out"

grep -Fx -- 'intake' "${TMP_DIR}/state/call-1.args" >/dev/null
grep -Fx -- 'enqueue' "${TMP_DIR}/state/call-1.args" >/dev/null
grep -Fx -- '--source' "${TMP_DIR}/state/call-1.args" >/dev/null
grep -Fx -- 'n8n' "${TMP_DIR}/state/call-1.args" >/dev/null
grep -Fx -- '--project' "${TMP_DIR}/state/call-1.args" >/dev/null
grep -Fx -- 'pbs' "${TMP_DIR}/state/call-1.args" >/dev/null
grep -Fx -- '--dedup-key' "${TMP_DIR}/state/call-1.args" >/dev/null
grep -Fx -- 'ci_failure:pbs:1234' "${TMP_DIR}/state/call-1.args" >/dev/null

payload_canonical="$(jq -c '.' "${TMP_DIR}/state/call-1.stdin")"
if [[ "${payload_canonical}" != '{"workflow_id":"pbs-ci-1","run_id":"1234"}' ]]; then
  printf 'unexpected intake payload: %s\n' "${payload_canonical}" >&2
  exit 1
fi

first_dedup_output="$(
  env \
    ODIN_BIN="${TMP_DIR}/bin/odin" \
    ODIN_ROOT="${TMP_DIR}/runtime" \
    ODIN_N8N_SSH_DEDUP_COOLDOWN_SECONDS=300 \
    SSH_ORIGINAL_COMMAND='dedup-check ci_failure pbs' \
    bash "${SCRIPT}"
)"
if [[ "${first_dedup_output}" != "ok" ]]; then
  printf 'unexpected first dedup output: %s\n' "${first_dedup_output}" >&2
  exit 1
fi

second_dedup_output="$(
  env \
    ODIN_BIN="${TMP_DIR}/bin/odin" \
    ODIN_ROOT="${TMP_DIR}/runtime" \
    ODIN_N8N_SSH_DEDUP_COOLDOWN_SECONDS=300 \
    SSH_ORIGINAL_COMMAND='dedup-check ci_failure pbs' \
    bash "${SCRIPT}"
)"
if [[ ! "${second_dedup_output}" =~ ^cooldown:[0-9]+$ ]]; then
  printf 'unexpected second dedup output: %s\n' "${second_dedup_output}" >&2
  exit 1
fi

race_stamp="${TMP_DIR}/runtime/state/n8n-ssh-router/dedup/ci_failure/pbs.stamp"
mkdir -p "$(dirname "${race_stamp}")"
printf '1\n' >"${race_stamp}"

env \
  PATH="${TMP_DIR}/bin:${PATH}" \
  ODIN_BIN="${TMP_DIR}/bin/odin" \
  ODIN_ROOT="${TMP_DIR}/runtime" \
  ODIN_N8N_SSH_DEDUP_COOLDOWN_SECONDS=300 \
  ODIN_TEST_SLOW_CAT_PATH="${race_stamp}" \
  ODIN_TEST_SLOW_CAT_SECONDS=1 \
  SSH_ORIGINAL_COMMAND='dedup-check ci_failure pbs' \
  bash "${SCRIPT}" >"${TMP_DIR}/dedup-race-one.out" &
race_one_pid=$!
sleep 0.1
env \
  PATH="${TMP_DIR}/bin:${PATH}" \
  ODIN_BIN="${TMP_DIR}/bin/odin" \
  ODIN_ROOT="${TMP_DIR}/runtime" \
  ODIN_N8N_SSH_DEDUP_COOLDOWN_SECONDS=300 \
  SSH_ORIGINAL_COMMAND='dedup-check ci_failure pbs' \
  bash "${SCRIPT}" >"${TMP_DIR}/dedup-race-two.out"
wait "${race_one_pid}"

race_one_output="$(/bin/cat "${TMP_DIR}/dedup-race-one.out")"
race_two_output="$(/bin/cat "${TMP_DIR}/dedup-race-two.out")"
if [[ ! ( "${race_one_output}" == "ok" && "${race_two_output}" =~ ^cooldown:[0-9]+$ ) && ! ( "${race_two_output}" == "ok" && "${race_one_output}" =~ ^cooldown:[0-9]+$ ) ]]; then
  printf 'unexpected dedup race outputs: %s / %s\n' "${race_one_output}" "${race_two_output}" >&2
  exit 1
fi

stale_lock_stamp="${TMP_DIR}/runtime/state/n8n-ssh-router/dedup/ci_failure_stale/pbs.stamp"
stale_lock_dir="${stale_lock_stamp}.lock"
mkdir -p "${stale_lock_dir}"
printf '1\n' >"${stale_lock_dir}/acquired_at"

env \
  ODIN_BIN="${TMP_DIR}/bin/odin" \
  ODIN_ROOT="${TMP_DIR}/runtime" \
  ODIN_N8N_SSH_DEDUP_COOLDOWN_SECONDS=300 \
  SSH_ORIGINAL_COMMAND='dedup-check ci_failure_stale pbs' \
  bash "${SCRIPT}" >"${TMP_DIR}/dedup-stale-lock.out" &
stale_lock_pid=$!
sleep 1
if kill -0 "${stale_lock_pid}" 2>/dev/null; then
  kill "${stale_lock_pid}" 2>/dev/null || true
  wait "${stale_lock_pid}" 2>/dev/null || true
  printf 'stale lock recovery hung\n' >&2
  exit 1
fi
wait "${stale_lock_pid}"

stale_lock_output="$(/bin/cat "${TMP_DIR}/dedup-stale-lock.out")"
if [[ "${stale_lock_output}" != "ok" ]]; then
  printf 'unexpected stale lock output: %s\n' "${stale_lock_output}" >&2
  exit 1
fi

missing_meta_stamp="${TMP_DIR}/runtime/state/n8n-ssh-router/dedup/ci_failure_missing_meta/pbs.stamp"
missing_meta_lock_dir="${missing_meta_stamp}.lock"
mkdir -p "${missing_meta_lock_dir}"
touch -d '@1' "${missing_meta_lock_dir}"

env \
  ODIN_BIN="${TMP_DIR}/bin/odin" \
  ODIN_ROOT="${TMP_DIR}/runtime" \
  ODIN_N8N_SSH_DEDUP_COOLDOWN_SECONDS=300 \
  SSH_ORIGINAL_COMMAND='dedup-check ci_failure_missing_meta pbs' \
  bash "${SCRIPT}" >"${TMP_DIR}/dedup-missing-meta-lock.out" &
missing_meta_pid=$!
sleep 1
if kill -0 "${missing_meta_pid}" 2>/dev/null; then
  kill "${missing_meta_pid}" 2>/dev/null || true
  wait "${missing_meta_pid}" 2>/dev/null || true
  printf 'missing-metadata lock recovery hung\n' >&2
  exit 1
fi
wait "${missing_meta_pid}"

missing_meta_output="$(/bin/cat "${TMP_DIR}/dedup-missing-meta-lock.out")"
if [[ "${missing_meta_output}" != "ok" ]]; then
  printf 'unexpected missing-metadata lock output: %s\n' "${missing_meta_output}" >&2
  exit 1
fi

if env \
  ODIN_BIN="${TMP_DIR}/bin/odin" \
  ODIN_ROOT="${TMP_DIR}/runtime" \
  SSH_ORIGINAL_COMMAND='dedup-check ci/failure pbs' \
  bash "${SCRIPT}" >/dev/null 2>"${TMP_DIR}/dedup-invalid.err"; then
  printf 'invalid dedup input unexpectedly succeeded\n' >&2
  exit 1
fi
grep -F 'dedup-check kind and project must contain only' "${TMP_DIR}/dedup-invalid.err" >/dev/null

env \
  ODIN_BIN="${TMP_DIR}/bin/odin" \
  ODIN_ROOT="${TMP_DIR}/runtime" \
  SSH_ORIGINAL_COMMAND='approval-resolve 17 approve confirmed' \
  bash "${SCRIPT}" >"${TMP_DIR}/approval.out"

grep -Fx -- 'approvals' "${TMP_DIR}/state/call-2.args" >/dev/null
grep -Fx -- 'resolve' "${TMP_DIR}/state/call-2.args" >/dev/null
grep -Fx -- '--id' "${TMP_DIR}/state/call-2.args" >/dev/null
grep -Fx -- '17' "${TMP_DIR}/state/call-2.args" >/dev/null
grep -Fx -- '--decision' "${TMP_DIR}/state/call-2.args" >/dev/null
grep -Fx -- 'approve' "${TMP_DIR}/state/call-2.args" >/dev/null
grep -Fx -- '--reason' "${TMP_DIR}/state/call-2.args" >/dev/null
grep -Fx -- 'confirmed' "${TMP_DIR}/state/call-2.args" >/dev/null
grep -Fx -- '--by' "${TMP_DIR}/state/call-2.args" >/dev/null
grep -Fx -- 'telegram' "${TMP_DIR}/state/call-2.args" >/dev/null
grep -Fx -- '--json' "${TMP_DIR}/state/call-2.args" >/dev/null

env \
  ODIN_BIN="${TMP_DIR}/bin/odin" \
  ODIN_ROOT="${TMP_DIR}/runtime" \
  SSH_ORIGINAL_COMMAND='approval-resolve 19 deny blocked-by-policy' \
  bash "${SCRIPT}" >"${TMP_DIR}/approval-deny.out"

grep -Fx -- 'approvals' "${TMP_DIR}/state/call-3.args" >/dev/null
grep -Fx -- 'resolve' "${TMP_DIR}/state/call-3.args" >/dev/null
grep -Fx -- '--id' "${TMP_DIR}/state/call-3.args" >/dev/null
grep -Fx -- '19' "${TMP_DIR}/state/call-3.args" >/dev/null
grep -Fx -- '--decision' "${TMP_DIR}/state/call-3.args" >/dev/null
grep -Fx -- 'reject' "${TMP_DIR}/state/call-3.args" >/dev/null
grep -Fx -- '--reason' "${TMP_DIR}/state/call-3.args" >/dev/null
grep -Fx -- 'blocked-by-policy' "${TMP_DIR}/state/call-3.args" >/dev/null
grep -Fx -- '--by' "${TMP_DIR}/state/call-3.args" >/dev/null
grep -Fx -- 'telegram' "${TMP_DIR}/state/call-3.args" >/dev/null
grep -Fx -- '--json' "${TMP_DIR}/state/call-3.args" >/dev/null

env \
  ODIN_BIN="${TMP_DIR}/bin/odin" \
  ODIN_ROOT="${TMP_DIR}/runtime" \
  SSH_ORIGINAL_COMMAND='approval-resolve 23 approve "blocked by policy"' \
  bash "${SCRIPT}" >"${TMP_DIR}/approval-quoted.out"

grep -Fx -- 'approvals' "${TMP_DIR}/state/call-4.args" >/dev/null
grep -Fx -- 'resolve' "${TMP_DIR}/state/call-4.args" >/dev/null
grep -Fx -- '--id' "${TMP_DIR}/state/call-4.args" >/dev/null
grep -Fx -- '23' "${TMP_DIR}/state/call-4.args" >/dev/null
grep -Fx -- '--decision' "${TMP_DIR}/state/call-4.args" >/dev/null
grep -Fx -- 'approve' "${TMP_DIR}/state/call-4.args" >/dev/null
grep -Fx -- '--reason' "${TMP_DIR}/state/call-4.args" >/dev/null
grep -Fx -- 'blocked by policy' "${TMP_DIR}/state/call-4.args" >/dev/null
grep -Fx -- '--by' "${TMP_DIR}/state/call-4.args" >/dev/null
grep -Fx -- 'telegram' "${TMP_DIR}/state/call-4.args" >/dev/null
grep -Fx -- '--json' "${TMP_DIR}/state/call-4.args" >/dev/null

if env \
  ODIN_BIN="${TMP_DIR}/bin/odin" \
  ODIN_ROOT="${TMP_DIR}/runtime" \
  SSH_ORIGINAL_COMMAND='nonce-update ci_failure pbs' \
  bash "${SCRIPT}" >/dev/null 2>"${TMP_DIR}/nonce.err"; then
  printf 'nonce-update unexpectedly succeeded\n' >&2
  exit 1
fi
grep -F 'nonce-update is not supported' "${TMP_DIR}/nonce.err" >/dev/null

printf 'odin-n8n-ssh-dispatch-test: ok\n'
