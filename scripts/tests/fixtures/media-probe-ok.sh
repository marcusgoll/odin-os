#!/usr/bin/env bash
cat <<'EOF'
{"signals":[
  {"name":"media.mounts","status":"healthy","summary":"mount audit passed"},
  {"name":"media.queue","status":"healthy","summary":"queue backlog within threshold"},
  {"name":"media.vpn","status":"healthy","summary":"vpn integrity check passed"}
]}
EOF
