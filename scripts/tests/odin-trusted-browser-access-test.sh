#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
script="$repo_root/scripts/ops/odin-trusted-browser-access.sh"

fail() {
  echo "test failed: $*" >&2
  exit 1
}

assert_contains() {
  local haystack="$1"
  local needle="$2"
  local label="${3:-output}"
  if [[ "$haystack" != *"$needle"* ]]; then
    fail "$label: missing '$needle'"
  fi
}

assert_first_line_eq() {
  local haystack="$1"
  local want="$2"
  local label="${3:-output}"
  local first_line
  first_line="$(printf '%s\n' "$haystack" | sed -n '1p')"
  if [[ "$first_line" != "$want" ]]; then
    fail "$label: first line got '$first_line', want '$want'"
  fi
}

require_file() {
  [[ -f "$1" ]] || fail "missing file: $1"
}

require_file "$script"

tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

stub_bin="$tmpdir/bin"
mkdir -p "$stub_bin"

cat >"$tmpdir/chrome-cdp-start.sh" <<'EOF'
#!/usr/bin/env bash
cdp_start() {
  printf 'cdp_start\n' >>"$ODIN_TEST_ACCESS_TRACE"
  export CHROME_DISPLAY="${ODIN_CHROME_DISPLAY:-:77}"
}
EOF

cat >"$stub_bin/x11vnc" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$@" >"$ODIN_TEST_X11VNC_ARGS"
exec sleep 300
EOF

cat >"$stub_bin/websockify" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$@" >"$ODIN_TEST_WEBSOCKIFY_ARGS"
exec sleep 300
EOF

chmod +x "$tmpdir/chrome-cdp-start.sh" "$stub_bin/x11vnc" "$stub_bin/websockify"

mkdir -p "$tmpdir/noVNC-src/core"
printf 'export default {};\n' >"$tmpdir/noVNC-src/core/rfb.js"

runtime_root="$tmpdir/runtime"
trace_path="$tmpdir/trace.log"
x11vnc_args="$tmpdir/x11vnc.args"
websockify_args="$tmpdir/websockify.args"

run_script() {
  ODIN_ROOT="$runtime_root" \
  ODIN_CHROME_CDP_LIB_PATH="$tmpdir/chrome-cdp-start.sh" \
  ODIN_X11VNC_BIN="$stub_bin/x11vnc" \
  ODIN_WEBSOCKIFY_BIN="$stub_bin/websockify" \
  ODIN_TRUSTED_BROWSER_NOVNC_ASSETS_DIR="$tmpdir/noVNC-src" \
  ODIN_CHROME_DISPLAY=:77 \
  ODIN_TEST_ACCESS_TRACE="$trace_path" \
  ODIN_TEST_X11VNC_ARGS="$x11vnc_args" \
  ODIN_TEST_WEBSOCKIFY_ARGS="$websockify_args" \
  bash "$script" "$@"
}

start_output="$(run_script start)"
assert_first_line_eq "$start_output" "status=ready" "start output"
assert_contains "$start_output" "display=:77" "start output"
assert_contains "$start_output" "chrome_profile=$runtime_root/browser-state/chrome-profile" "start output"
assert_contains "$start_output" "novnc_url=http://127.0.0.1:6080" "start output"
assert_contains "$(cat "$trace_path")" "cdp_start" "cdp trace"
assert_contains "$(tr '\n' ' ' <"$x11vnc_args")" "-display :77 -rfbport 5901 -localhost -forever -shared -nopw -noxdamage" "x11vnc args"
assert_contains "$(tr '\n' ' ' <"$websockify_args")" "--web $runtime_root/browser-state/trusted-browser-access/web 127.0.0.1:6080 127.0.0.1:5901" "websockify args"

web_root="$runtime_root/browser-state/trusted-browser-access/web"
require_file "$web_root/index.html"
[[ -L "$web_root/noVNC-src" ]] || fail "expected noVNC-src symlink"

status_output="$(run_script status)"
assert_first_line_eq "$status_output" "status=ready" "status output"
assert_contains "$status_output" "chrome_profile=$runtime_root/browser-state/chrome-profile" "status output"
assert_contains "$status_output" "x11vnc_status=running" "status output"
assert_contains "$status_output" "websockify_status=running" "status output"

kill "$(cat "$runtime_root/browser-state/trusted-browser-access/x11vnc.pid")" >/dev/null 2>&1 || true
kill "$(cat "$runtime_root/browser-state/trusted-browser-access/websockify.pid")" >/dev/null 2>&1 || true
sleep 1

status_after_kill="$(run_script status)"
assert_first_line_eq "$status_after_kill" "status=stopped" "status after child exit"
assert_contains "$status_after_kill" "chrome_profile=$runtime_root/browser-state/chrome-profile" "status after child exit"

run_script stop >/dev/null

if [[ -f "$runtime_root/browser-state/trusted-browser-access/x11vnc.pid" ]]; then
  fail "x11vnc pid file should be removed on stop"
fi
if [[ -f "$runtime_root/browser-state/trusted-browser-access/websockify.pid" ]]; then
  fail "websockify pid file should be removed on stop"
fi

status_after_stop="$(run_script status)"
assert_first_line_eq "$status_after_stop" "status=stopped" "status after stop"
assert_contains "$status_after_stop" "chrome_profile=$runtime_root/browser-state/chrome-profile" "status after stop"

echo "ok"
