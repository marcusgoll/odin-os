#!/usr/bin/env bash
set -euo pipefail

request="$(cat)"
action_kind="$(printf '%s' "$request" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("action_kind",""))')"

if [[ "$action_kind" != "submit_form" ]]; then
  printf '%s' '{"status":"failed","adapter_kind":"huginn_browser_mutation_fixture","action_kind":"'"$action_kind"'","error_code":"unsupported_action","error_message":"fixture driver only supports submit_form"}'
  exit 0
fi

printf '%s' '{"status":"completed","adapter_kind":"huginn_browser_mutation_fixture","action_kind":"submit_form","final_url":"https://example.com/form/complete","summary":"Submitted fixture form with redacted evidence.","evidence":{"pre_action_visible_state":"Fixture form ready","post_action_visible_state":"Fixture form submitted","redaction":"secrets_and_sensitive_values"}}'
