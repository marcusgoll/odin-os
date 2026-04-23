# n8n SSH Router

`scripts/ops/odin-n8n-ssh-dispatch.sh` is the host-side forced-command entrypoint for the dedicated odin-os pilot ingress key used by cutover-ready n8n workflows.

The recommended staged cutover layout is:

- legacy workflows keep using `/home/node/.ssh/odin_ingress`
- odin-os pilot workflows use `/home/node/.ssh/odin_os_ingress`

This keeps non-cutover workflows on the legacy forced command until their payload shape has been migrated.

Supported commands:

- Empty `SSH_ORIGINAL_COMMAND`: read a normalized intake envelope from stdin and route it to `odin intake enqueue`
- `dedup-check <kind> <project>`: use a file-backed cooldown under `${ODIN_ROOT}/state/n8n-ssh-router/dedup`
- `approval-resolve <approval_id> <approve|deny> <reason...>`: route the Telegram-style approval callback to `odin approvals resolve`

Rejected commands:

- `nonce-update`
- any unknown `SSH_ORIGINAL_COMMAND`

Environment:

- `ODIN_BIN`: path to the `odin` binary. Defaults to `odin`.
- `ODIN_ROOT`: runtime root used for dedup state. Defaults to the current working directory.
- `ODIN_N8N_SSH_DEDUP_COOLDOWN_SECONDS`: dedup cooldown window. Defaults to `300`.
- `ODIN_N8N_SSH_APPROVAL_ACTOR`: value passed to `odin approvals resolve --by`. Defaults to `telegram`.
- `ODIN_N8N_SSH_LOCK_STALE_SECONDS`: reclaim a stale per-key dedup lock after this many seconds. Defaults to `30`.
- `ODIN_N8N_SSH_LOCK_WAIT_SECONDS`: fail a blocked dedup check after this many seconds if the lock cannot be acquired. Defaults to `5`.

Normalized intake envelope:

```json
{
  "schema_version": 1,
  "source": "n8n",
  "type": "ci_failure",
  "project_key": "pbs",
  "title": "Investigate PBS CI failure",
  "action_key": "",
  "dedup_key": "ci_failure:pbs:1234",
  "requested_by": "n8n",
  "payload": {}
}
```

The router forwards only the `payload` object over stdin to `odin intake enqueue --payload-file -`. The envelope metadata is translated into explicit CLI flags.

Operational notes:

- `dedup-check` is intentionally small and file-backed so the router stays shell-native.
- `dedup-check` uses a per-key lock with stale-lock reaping and bounded wait, so a dead writer does not block that key forever.
- `approval-resolve` replaces the old Telegram `nonce-update` callback path.
- `approval-resolve deny ...` is translated to `odin approvals resolve --decision reject` for CLI compatibility.
- Unknown or unsupported commands should fail closed.
