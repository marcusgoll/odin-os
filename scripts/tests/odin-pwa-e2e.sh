#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
PORT="$(
  node -e "const net=require('net');const s=net.createServer();s.listen(0,'127.0.0.1',()=>{console.log(s.address().port);s.close();});"
)"
TMP_ROOT="$(mktemp -d)"
LOG_FILE="${TMP_ROOT}/odin-serve.log"
PID=""

cleanup() {
  if [[ -n "${PID}" ]] && kill -0 "${PID}" >/dev/null 2>&1; then
    kill "${PID}" >/dev/null 2>&1 || true
    wait "${PID}" >/dev/null 2>&1 || true
  fi
  rm -rf "${TMP_ROOT}"
}
trap cleanup EXIT

cd "${ROOT}"
if [[ ! -x node_modules/.bin/playwright ]]; then
  npm install --no-audit --no-fund
fi
ODIN_ROOT="${TMP_ROOT}" ODIN_HTTP_ADDR="127.0.0.1:${PORT}" ./bin/odin serve >"${LOG_FILE}" 2>&1 &
PID="$!"

for _ in {1..80}; do
  if curl -fsS "http://127.0.0.1:${PORT}/app/" >/dev/null 2>&1; then
    break
  fi
  if ! kill -0 "${PID}" >/dev/null 2>&1; then
    cat "${LOG_FILE}" >&2
    exit 1
  fi
  sleep 0.25
done

curl -fsS "http://127.0.0.1:${PORT}/app/" >/dev/null
ODIN_PWA_BASE_URL="http://127.0.0.1:${PORT}" npm exec -- playwright test --config=playwright.config.js tests/pwa/odin-pwa.spec.js
