#!/usr/bin/env bash
set -euo pipefail

repo_root="$(git rev-parse --show-toplevel)"
cd "${repo_root}"

odin_bin="${ODIN_PHONE_RELEASE_ODIN:-${repo_root}/bin/odin}"
artifact_root="${repo_root}/.odin/phone-release-check"
run_id="$(date -u +%Y%m%dT%H%M%SZ)"
run_dir="${artifact_root}/${run_id}"
latest_log="${artifact_root}/latest.log"
latest_json="${artifact_root}/latest.json"
mkdir -p "${run_dir}"
: >"${latest_log}"

tmp_root="$(mktemp -d)"
runtime_root="${tmp_root}/runtime"
home_root="${tmp_root}/home"
serve_log="${run_dir}/serve.log"
serve_pid=""
admin_token="phone-release-check-admin-token"

log() {
  printf '%s\n' "$*" | tee -a "${latest_log}"
}

fail() {
  log "FAIL: $*"
  exit 1
}

cleanup() {
  if [[ -n "${serve_pid}" ]] && kill -0 "${serve_pid}" >/dev/null 2>&1; then
    kill "${serve_pid}" >/dev/null 2>&1 || true
    wait "${serve_pid}" >/dev/null 2>&1 || true
  fi
  if [[ "${ODIN_PHONE_RELEASE_KEEP:-}" != "1" ]]; then
    rm -rf "${tmp_root}"
  else
    log "kept temp root: ${tmp_root}"
  fi
}
trap cleanup EXIT

json_assert() {
  local file="$1"
  local expression="$2"
  local message="$3"
  python3 - "$file" "$expression" "$message" <<'PY'
import json
import sys

path, expression, message = sys.argv[1:4]
with open(path, "r", encoding="utf-8") as handle:
    data = json.load(handle)
if not eval(expression, {"__builtins__": {}, "data": data, "any": any, "all": all, "len": len}, {}):
    raise SystemExit(message)
PY
}

free_port() {
  python3 - <<'PY'
import socket
sock = socket.socket()
sock.bind(("127.0.0.1", 0))
print(sock.getsockname()[1])
sock.close()
PY
}

run_logged() {
  local label="$1"
  shift
  local output="${run_dir}/$(printf '%s' "${label}" | tr '[:upper:] ' '[:lower:]-' | tr -cd '[:alnum:]-').out"
  log ""
  log "## ${label}"
  log "$ $*"
  if "$@" >"${output}" 2>&1; then
    sed 's/^/  /' "${output}" | tee -a "${latest_log}" >/dev/null
  else
    local status=$?
    sed 's/^/  /' "${output}" | tee -a "${latest_log}" >/dev/null
    fail "${label} failed with status ${status}; see ${output}"
  fi
  LAST_OUT="${output}"
}

[[ -x "${odin_bin}" ]] || fail "repo-local odin binary missing at ${odin_bin}; run make build first"
mkdir -p "${runtime_root}" "${home_root}"

log "Odin phone release check"
log "artifacts: ${run_dir}"
log "runtime root: ${runtime_root}"
log "binary_mode=source_local binary=${odin_bin}"
log "external_mutation=none mode=test"

run_logged "Binary source-local help" env HOME="${home_root}" ODIN_ROOT="${runtime_root}" "${odin_bin}" help
grep -Fq "Commands:" "${LAST_OUT}" || fail "repo-local odin help did not render command list"

port="$(free_port)"
base_url="http://127.0.0.1:${port}"
log ""
log "## Health/readiness: source-local serve"
log "$ ODIN_ROOT=${runtime_root} ODIN_HTTP_ADDR=127.0.0.1:${port} ${odin_bin} serve"
env \
  HOME="${home_root}" \
  ODIN_ROOT="${runtime_root}" \
  ODIN_HTTP_ADDR="127.0.0.1:${port}" \
  ODIN_ADMIN_TOKEN="${admin_token}" \
  "${odin_bin}" serve >"${serve_log}" 2>&1 &
serve_pid=$!

for _ in $(seq 1 100); do
  if curl -fsS "${base_url}/healthz" >"${run_dir}/healthz.json" 2>"${run_dir}/healthz.err" &&
     curl -fsS "${base_url}/readyz" >"${run_dir}/readyz.json" 2>"${run_dir}/readyz.err"; then
    json_assert "${run_dir}/healthz.json" "data.get('status') in ('healthy', 'degraded')" "healthz must report health status"
    json_assert "${run_dir}/readyz.json" "data.get('status') in ('healthy', 'degraded')" "readyz must report readiness status"
    break
  fi
  if ! kill -0 "${serve_pid}" >/dev/null 2>&1; then
    sed 's/^/  /' "${serve_log}" | tee -a "${latest_log}" >/dev/null
    fail "odin serve exited before health/readiness passed"
  fi
  sleep 0.1
done
[[ -s "${run_dir}/readyz.json" ]] || fail "readyz did not become available"

run_logged "PWA build/installability" go test ./internal/api/http -run TestPWA -count=1 -v
run_logged "Mobile phone release API proof" go test ./internal/api/http -run 'TestOdinPhoneReleaseCheck|TestMobileShare|TestNotification' -count=1 -v

cat >"${latest_json}" <<JSON
{
  "status": "passed",
  "binary_mode": "source_local",
  "runtime_root": "${runtime_root}",
  "serve_url": "${base_url}",
  "external_mutation": "none",
  "stubbed_labeled": [
    "Huginn browser evidence uses adapter_kind=stub_local in TestOdinPhoneReleaseCheck",
    "notification subscription uses push.example.test fake endpoint"
  ],
  "proofs": [
    "repo-local binary help",
    "healthz",
    "readyz",
    "mobile API session auth and CSRF",
    "PWA build",
    "PWA manifest/service worker/share target",
    "mobile overview",
    "mobile approval decision",
    "raw text intake",
    "image attachment intake",
    "audio attachment intake",
    "share-target route and intake",
    "notification subscription and revoke fake path",
    "Huginn browser evidence visible in mobile review",
    "mobile and canonical audit events"
  ]
}
JSON

log ""
log "Proof matrix:"
log "  [real] source-local binary: passed"
log "  [real] health/readiness: passed"
log "  [real] mobile API auth/session/CSRF: passed"
log "  [real] PWA build/installability artifacts: passed"
log "  [real] overview, approval, text/image/audio/share intake: passed"
log "  [test] notification subscription fake endpoint: passed"
log "  [stub:labeled] Huginn browser evidence adapter_kind=stub_local: passed"
log "  [real] audit event assertions: passed"
log ""
log "PASS: Odin phone release check"
log "latest log: ${latest_log}"
log "latest summary: ${latest_json}"
