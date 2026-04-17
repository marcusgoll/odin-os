#!/usr/bin/env bash
cat <<'EOF'
{"signals":[
  {"name":"media.mounts","status":"failed","summary":"mount mismatch detected","details":{"path":"/mnt/media/downloads"}},
  {"name":"media.vpn","status":"healthy","summary":"vpn integrity check passed"}
]}
EOF
